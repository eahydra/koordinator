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
	"k8s.io/apimachinery/pkg/util/sets"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/koordinator-sh/koordinator/apis/extension"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	"github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext"
	reservationutil "github.com/koordinator-sh/koordinator/pkg/util/reservation"
)

func (pl *Plugin) BeforePreFilter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) (*corev1.Pod, bool, error) {
	state, restored, err := pl.prepareMatchReservationState(ctx, cycleState, pod)
	if err != nil {
		return nil, false, err
	}
	cycleState.Write(stateKey, state)
	return pod, restored, nil
}

type reservationMatchState struct {
	nodeName  string
	matched   []*frameworkext.ReservationInfo
	unmatched []*frameworkext.ReservationInfo
}

func (pl *Plugin) prepareMatchReservationState(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) (*stateData, bool, error) {
	allNodes, err := pl.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		return nil, false, fmt.Errorf("cannot list NodeInfo, err: %v", err)
	}

	reservationAffinity, err := reservationutil.GetRequiredReservationAffinity(pod)
	if err != nil {
		klog.ErrorS(err, "Failed to parse reservation affinity", "pod", klog.KObj(pod))
		return nil, false, err
	}

	var stateIndex int32
	allMatchStates := make([]*reservationMatchState, len(allNodes))

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
			nodeName: node.Name,
		}
		for _, rInfo := range rOnNode {
			if !reservationutil.IsReservationAvailable(rInfo.Reservation) {
				continue
			}

			// In this case, the Controller has not yet updated the status of the Reservation to Succeeded,
			// but in fact it can no longer be used for allocation. So it's better to skip first.
			if extension.IsReservationAllocateOnce(rInfo.Reservation) && len(rInfo.Pods) > 0 {
				continue
			}

			if !isReservedPod && matchReservation(pod, node, rInfo.Reservation, reservationAffinity) {
				if err = restoreMatchedReservation(nodeInfo, rInfo, podInfoMap); err != nil {
					errCh.SendErrorWithCancel(err, cancel)
					return
				}

				matchState.matched = append(matchState.matched, rInfo)

			} else if len(rInfo.Pods) > 0 {
				if err = restoreUnmatchedReservations(nodeInfo, rInfo); err != nil {
					errCh.SendErrorWithCancel(err, cancel)
					return
				}

				matchState.unmatched = append(matchState.unmatched, rInfo)
				if !isReservedPod {
					klog.V(6).InfoS("got reservation on node does not match the pod", "reservation", klog.KObj(rInfo.Reservation), "pod", klog.KObj(pod))
				}
			}
		}

		if len(matchState.matched) > 0 || len(matchState.unmatched) > 0 {
			index := atomic.AddInt32(&stateIndex, 1)
			allMatchStates[index-1] = matchState
		}
		klog.V(4).Infof("Pod %v has reservations on node %v, %d matched, %d unmatched", klog.KObj(pod), node.Name, len(matchState.matched), len(matchState.unmatched))
	}
	pl.handle.Parallelizer().Until(parallelCtx, len(allNodes), processNode)
	err = errCh.ReceiveError()
	if err != nil {
		return nil, false, err
	}

	allMatchStates = allMatchStates[:stateIndex]
	state := &stateData{
		matched:   map[string][]*frameworkext.ReservationInfo{},
		unmatched: map[string][]*frameworkext.ReservationInfo{},
	}
	for _, v := range allMatchStates {
		state.matched[v.nodeName] = v.matched
		state.unmatched[v.nodeName] = v.unmatched
	}

	return state, len(allMatchStates) > 0, nil
}

func (pl *Plugin) AfterPreFilter(_ frameworkext.ExtendedHandle, cycleState *framework.CycleState, pod *corev1.Pod) error {
	state := getStateData(cycleState)

	nodeNames := sets.NewString()
	for nodeName := range state.matched {
		nodeNames.Insert(nodeName)
	}
	for nodeName := range state.unmatched {
		nodeNames.Insert(nodeName)
	}

	for nodeName := range nodeNames {
		nodeInfo, err := pl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
		if err != nil {
			continue
		}
		if nodeInfo.Node() == nil {
			continue
		}

		// We should find an appropriate time to return resources allocated by custom plugins held by Reservation,
		// such as fine-grained CPUs(CPU Cores), Devices(e.g. GPU/RDMA/FPGA etc.).
		if extender, ok := pl.handle.(frameworkext.FrameworkExtender); ok {
			status := extender.RunReservationPreFilterExtensionRestoreReservation(context.Background(), cycleState, pod, state.matched[nodeName], state.unmatched[nodeName], nodeInfo)
			if !status.IsSuccess() {
				return status.AsError()
			}
		}
	}
	return nil
}

func restoreUnmatchedReservations(nodeInfo *framework.NodeInfo, rInfo *frameworkext.ReservationInfo) error {
	// Reservations and Pods that consume the Reservations are cumulative in resource accounting.
	// For example, on a 32C machine, ReservationA reserves 8C, and then PodA uses ReservationA to allocate 4C,
	// then the record on NodeInfo is that 12C is allocated. But in fact it should be calculated according to 8C,
	// so we need to return some resources.
	reservePod := reservationutil.NewReservePod(rInfo.Reservation)
	if err := nodeInfo.RemovePod(reservePod); err != nil {
		klog.Errorf("Failed to remove reserve pod %v from node %v, err: %v", klog.KObj(rInfo.Reservation), nodeInfo.Node().Name, err)
		return err
	}
	occupyUnallocatedResources(rInfo, reservePod, nodeInfo)
	return nil
}

func occupyUnallocatedResources(rInfo *frameworkext.ReservationInfo, reservePod *corev1.Pod, nodeInfo *framework.NodeInfo) {
	if len(rInfo.Pods) == 0 {
		nodeInfo.AddPod(reservePod)
	} else {
		for i := range reservePod.Spec.Containers {
			reservePod.Spec.Containers[i].Resources.Requests = corev1.ResourceList{}
		}
		remainedResource := quotav1.SubtractWithNonNegativeResult(rInfo.Allocatable, rInfo.Allocated)
		if !quotav1.IsZero(remainedResource) {
			reservePod.Spec.Containers = append(reservePod.Spec.Containers, corev1.Container{
				Resources: corev1.ResourceRequirements{Requests: remainedResource},
			})
		}
		nodeInfo.AddPod(reservePod)
	}
}

func restoreMatchedReservation(nodeInfo *framework.NodeInfo, rInfo *frameworkext.ReservationInfo, podInfoMap map[types.UID]*framework.PodInfo) error {
	reservePod := reservationutil.NewReservePod(rInfo.Reservation)

	// Retain ports that are not used by other Pods. These ports need to be erased from NodeInfo.UsedPorts,
	// otherwise it may cause Pod port conflicts
	retainReservePodUnusedPorts(reservePod, rInfo.Reservation, podInfoMap)

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
