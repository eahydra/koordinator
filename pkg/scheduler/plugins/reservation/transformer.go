/*
Copyright 2022 The Koordinator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reservation

import (
	"context"
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/koordinator-sh/koordinator/apis/extension"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	"github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext"
	reservationutil "github.com/koordinator-sh/koordinator/pkg/util/reservation"
)

func (pl *Plugin) BeforePreFilter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) (*corev1.Pod, bool, error) {
	state, err := pl.prepareMatchReservationState(ctx, cycleState, pod)
	if err != nil {
		klog.Warningf("BeforePreFilter failed to get matched reservations, err: %v", err)
		return nil, false, err
	}
	cycleState.Write(stateKey, state)

	klog.V(4).Infof("Pod %v has %d matched reservations and %d unmatched reservations before PreFilter", klog.KObj(pod), len(state.matched), len(state.unmatched))
	return pod, len(state.matched) > 0 || len(state.unmatched) > 0, nil
}

type reservationMatchState struct {
	nodeName          string
	matched           []*reservationInfo
	unmatched         []*reservationInfo
	restoredMatched   map[string][]interface{}
	restoredUnmatched map[string][]interface{}
}

func (pl *Plugin) prepareMatchReservationState(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) (*stateData, error) {
	allNodes, err := pl.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		return nil, fmt.Errorf("cannot list NodeInfo, err: %v", err)
	}

	reservationAffinity, err := reservationutil.GetRequiredReservationAffinity(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to parse reservation affinity", "pod", klog.KObj(pod))
		return nil, err
	}

	var stateIndex int32
	reservationMatchStates := make([]*reservationMatchState, len(allNodes))

	isReservedPod := reservationutil.IsReservePod(pod)
	parallelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := NewErrorChannel()

	processNode := func(i int) {
		nodeInfo := allNodes[i]
		node := nodeInfo.Node()
		if node == nil {
			klog.V(4).InfoS("BeforePreFilter failed to get node", "pod", klog.KObj(pod), "nodeInfo", nodeInfo)
			return
		}

		rOnNode := pl.reservationCache.listReservationInfosOnNode(node.Name)
		if len(rOnNode) == 0 {
			return
		}

		podInfoMap := make(map[types.UID]*framework.PodInfo)
		for _, podInfo := range nodeInfo.Pods {
			if !reservationutil.IsReservePod(podInfo.Pod) {
				podInfoMap[podInfo.Pod.UID] = podInfo
			}
		}

		matchState := &reservationMatchState{
			nodeName:          node.Name,
			restoredMatched:   map[string][]interface{}{},
			restoredUnmatched: map[string][]interface{}{},
		}
		for _, rInfo := range rOnNode {
			if !reservationutil.IsReservationAvailable(rInfo.reservation) {
				continue
			}

			// In this case, the Controller has not yet updated the status of the Reservation to Succeeded,
			// but in fact it can no longer be used for allocation. So it's better to skip first.
			if extension.IsReservationAllocateOnce(rInfo.reservation) && len(rInfo.pods) > 0 {
				continue
			}

			if !isReservedPod && matchReservation(pod, node, rInfo.reservation, reservationAffinity) {
				if err = restoreMatchedReservation(nodeInfo, rInfo, podInfoMap); err != nil {
					errCh.SendErrorWithCancel(err, cancel)
					return
				}

				matchState.matched = append(matchState.matched, rInfo)

			} else if len(rInfo.pods) > 0 {
				if err = restoreUnmatchedReservations(nodeInfo, rInfo); err != nil {
					errCh.SendErrorWithCancel(err, cancel)
					return
				}

				matchState.unmatched = append(matchState.unmatched, rInfo)
				if !isReservedPod {
					klog.V(6).InfoS("got reservation on node does not match the pod", "reservation", klog.KObj(rInfo.reservation), "pod", klog.KObj(pod))
				}
			}
		}

		if len(matchState.matched) > 0 || len(matchState.unmatched) > 0 {
			index := atomic.AddInt32(&stateIndex, 1)
			reservationMatchStates[index-1] = matchState
		}
		if len(matchState.matched) == 0 {
			return
		}

		klog.V(4).Infof("Pod %v has %d matched reservations before PreFilter, %d unmatched reservations on node %v", klog.KObj(pod), len(matchState.matched), len(matchState.unmatched), node.Name)
	}
	pl.handle.Parallelizer().Until(parallelCtx, len(allNodes), processNode)
	err = errCh.ReceiveError()

	reservationMatchStates = reservationMatchStates[:stateIndex]
	restoredMatched := map[string][]interface{}{}
	restoredUnmatched := map[string][]interface{}{}
	state := &stateData{
		matched:   map[string][]*reservationInfo{},
		unmatched: map[string][]*reservationInfo{},
	}
	for _, v := range reservationMatchStates {
		state.matched[v.nodeName] = v.matched
		state.unmatched[v.nodeName] = v.unmatched
		for _, vv := range v.restoredMatched {
			restoredMatched[v.nodeName] = append(restoredMatched[v.nodeName], vv)
		}
		for _, vv := range v.restoredUnmatched {
			restoredUnmatched[v.nodeName] = append(restoredUnmatched[v.nodeName], vv)
		}
	}

	return state, err
}

func (pl *Plugin) AfterPreFilter(_ frameworkext.ExtendedHandle, cycleState *framework.CycleState, pod *corev1.Pod) error {
	state := getStateData(cycleState)
	if !reservationutil.IsReservePod(pod) {
		for nodeName, reservationInfos := range state.matched {
			nodeInfo, err := pl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
			if err != nil {
				continue
			}
			if nodeInfo.Node() == nil {
				continue
			}

			podInfoMap := make(map[types.UID]*framework.PodInfo)
			for _, podInfo := range nodeInfo.Pods {
				if !reservationutil.IsReservePod(podInfo.Pod) {
					podInfoMap[podInfo.Pod.UID] = podInfo
				}
			}

			// NOTE: After PreFilter, all reserved resources need to be returned to the corresponding NodeInfo,
			// and each plugin is triggered to adjust its own StateData through RemovePod, RemoveReservation and AddPodInReservation,
			// and adjust the state data related to Reservation and Pod
			for _, rInfo := range reservationInfos {
				err := restoreReservedResourcesViaPlugins(pl.handle, cycleState, pod, rInfo, nodeInfo, podInfoMap)
				if err != nil {
					return err
				}

			}
		}
	}
	return nil
}

func restoreUnmatchedReservations(nodeInfo *framework.NodeInfo, rInfo *reservationInfo) error {
	// Reservations and Pods that consume the Reservations are cumulative in resource accounting.
	// For example, on a 32C machine, ReservationA reserves 8C, and then PodA uses ReservationA to allocate 4C,
	// then the record on NodeInfo is that 12C is allocated. But in fact it should be calculated according to 8C,
	// so we need to return some resources.
	reservePod := reservationutil.NewReservePod(rInfo.reservation)
	if err := nodeInfo.RemovePod(reservePod); err != nil {
		klog.Errorf("Failed to remove reserve pod %v from node %v, err: %v", klog.KObj(rInfo.reservation), nodeInfo.Node().Name, err)
		return err
	}
	occupyUnallocatedResources(rInfo, reservePod, nodeInfo)
	return nil
}

func occupyUnallocatedResources(rInfo *reservationInfo, reservePod *corev1.Pod, nodeInfo *framework.NodeInfo) {
	if len(rInfo.pods) == 0 {
		nodeInfo.AddPod(reservePod)
	} else {
		for i := range reservePod.Spec.Containers {
			reservePod.Spec.Containers[i].Resources.Requests = corev1.ResourceList{}
		}
		remainedResource := quotav1.SubtractWithNonNegativeResult(rInfo.allocatable, rInfo.allocated)
		if !quotav1.IsZero(remainedResource) {
			reservePod.Spec.Containers = append(reservePod.Spec.Containers, corev1.Container{
				Resources: corev1.ResourceRequirements{Requests: remainedResource},
			})
		}
		nodeInfo.AddPod(reservePod)
	}
}

func restoreMatchedReservation(nodeInfo *framework.NodeInfo, rInfo *reservationInfo, podInfoMap map[types.UID]*framework.PodInfo) error {
	reservePod := reservationutil.NewReservePod(rInfo.reservation)

	// Retain ports that are not used by other Pods. These ports need to be erased from NodeInfo.UsedPorts,
	// otherwise it may cause Pod port conflicts
	retainReservePodUnusedPorts(reservePod, rInfo.reservation, podInfoMap)

	// When AllocateOnce is disabled, some resources may have been allocated,
	// and an additional resource record will be accumulated at this time.
	// Even if the Reservation is not bound by the Pod (e.g. Reservation is enabled with AllocateOnce),
	// these resources held by the Reservation need to be returned, so as to ensure that
	// the Pod can pass through each filter plugin during scheduling.
	// The returned resources include scalar resources such as CPU/Memory, ports etc..
	if err := nodeInfo.RemovePod(reservePod); err != nil {
		return err
	}

	return nil
}

func restoreReservedResourcesViaPlugins(handle frameworkext.ExtendedHandle, cycleState *framework.CycleState, pod *corev1.Pod, rInfo *reservationInfo, nodeInfo *framework.NodeInfo, podInfoMap map[types.UID]*framework.PodInfo) error {
	// We should find an appropriate time to return resources allocated by custom plugins held by Reservation,
	// such as fine-grained CPUs(CPU Cores), Devices(e.g. GPU/RDMA/FPGA etc.).
	if extender, ok := handle.(frameworkext.FrameworkExtender); ok {
		status := extender.RunReservationPreFilterExtensionRemoveReservation(context.Background(), cycleState, pod, rInfo.reservation, nodeInfo)
		if !status.IsSuccess() {
			return status.AsError()
		}

		for _, assignedPod := range rInfo.pods {
			if assignedPodInfo, ok := podInfoMap[assignedPod.uid]; ok {
				status := extender.RunReservationPreFilterExtensionAddPodInReservation(context.Background(), cycleState, pod, assignedPodInfo, rInfo.reservation, nodeInfo)
				if !status.IsSuccess() {
					return status.AsError()
				}
			}
		}
	}
	return nil
}

func retainReservePodUnusedPorts(reservePod *corev1.Pod, reservation *schedulingv1alpha1.Reservation, podInfoMap map[types.UID]*framework.PodInfo) {
	port := reservationutil.ReservePorts(reservation)
	if len(port) == 0 {
		return
	}

	// TODO(joseph): maybe we can record allocated Ports by Pods in Reservation.Status
	portReserved := framework.HostPortInfo{}
	for ip, protocolPortMap := range port {
		for protocolPort := range protocolPortMap {
			portReserved.Add(ip, protocolPort.Protocol, protocolPort.Port)
		}
	}

	removed := false
	for _, assignedPodInfo := range podInfoMap {
		for i := range assignedPodInfo.Pod.Spec.Containers {
			container := &assignedPodInfo.Pod.Spec.Containers[i]
			for j := range container.Ports {
				podPort := &container.Ports[j]
				portReserved.Remove(podPort.HostIP, string(podPort.Protocol), podPort.HostPort)
				removed = true
			}
		}
	}
	if !removed {
		return
	}

	for i := range reservePod.Spec.Containers {
		container := &reservePod.Spec.Containers[i]
		if len(container.Ports) > 0 {
			container.Ports = nil
		}
	}

	container := &reservePod.Spec.Containers[0]
	for ip, protocolPortMap := range portReserved {
		for ports := range protocolPortMap {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				HostPort: ports.Port,
				Protocol: corev1.Protocol(ports.Protocol),
				HostIP:   ip,
			})
		}
	}
}

func matchReservation(pod *corev1.Pod, node *corev1.Node, reservation *schedulingv1alpha1.Reservation, reservationAffinity *reservationutil.RequiredReservationAffinity) bool {
	if !reservationutil.MatchReservationOwners(pod, reservation) {
		return false
	}

	if reservationAffinity != nil {
		// NOTE: There are some special scenarios.
		// For example, the AZ where the Pod wants to select the Reservation is cn-hangzhou, but the Reservation itself
		// does not have this information, so it needs to perceive the label of the Node when Matching Affinity.
		fakeNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   reservation.Name,
				Labels: map[string]string{},
			},
		}
		for k, v := range node.Labels {
			fakeNode.Labels[k] = v
		}
		for k, v := range reservation.Labels {
			fakeNode.Labels[k] = v
		}
		return reservationAffinity.Match(fakeNode)
	}
	return true
}
