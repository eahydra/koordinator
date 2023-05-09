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

package deviceshare

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/api/v1/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	"github.com/koordinator-sh/koordinator/pkg/scheduler/apis/config"
	"github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext"
	"github.com/koordinator-sh/koordinator/pkg/util"
	reservationutil "github.com/koordinator-sh/koordinator/pkg/util/reservation"
)

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = "DeviceShare"

	// stateKey is the key in CycleState to pre-computed data.
	stateKey = Name

	// ErrMissingDevice when node does not have Device.
	ErrMissingDevice = "node(s) missing Device"

	// ErrInsufficientDevices when node can't satisfy Pod's requested resource.
	ErrInsufficientDevices = "Insufficient Devices"
)

var (
	_ framework.PreFilterPlugin = &Plugin{}
	_ framework.FilterPlugin    = &Plugin{}
	_ framework.ReservePlugin   = &Plugin{}
	_ framework.PreBindPlugin   = &Plugin{}

	_ frameworkext.ReservationPreFilterExtension = &Plugin{}
	_ frameworkext.ReservationFilterPlugin       = &Plugin{}
	_ frameworkext.ReservationPreBindPlugin      = &Plugin{}
)

type Plugin struct {
	handle          framework.Handle
	nodeDeviceCache *nodeDeviceCache
	allocator       Allocator
}

type preFilterState struct {
	skip                  bool
	allocationResult      apiext.DeviceAllocations
	podRequests           corev1.ResourceList
	preemptibleDevices    map[string]map[schedulingv1alpha1.DeviceType]deviceResources
	matchedReservations   map[string][]*frameworkext.ReservationInfo
	unmatchedReservations map[string][]reservationAlloc
}

type reservationAlloc struct {
	rInfo    *frameworkext.ReservationInfo
	remained map[schedulingv1alpha1.DeviceType]deviceResources
}

func (s *preFilterState) Clone() framework.StateData {
	ns := &preFilterState{
		skip:                  s.skip,
		allocationResult:      s.allocationResult,
		podRequests:           s.podRequests,
		matchedReservations:   s.matchedReservations,
		unmatchedReservations: s.unmatchedReservations,
	}

	preemptibleDevices := map[string]map[schedulingv1alpha1.DeviceType]deviceResources{}
	for nodeName, returnedDevices := range s.preemptibleDevices {
		devices := preemptibleDevices[nodeName]
		if devices == nil {
			devices = map[schedulingv1alpha1.DeviceType]deviceResources{}
			preemptibleDevices[nodeName] = devices
		}
		for k, v := range returnedDevices {
			devices[k] = v.DeepCopy()
		}
	}
	ns.preemptibleDevices = preemptibleDevices
	return ns
}

func (p *Plugin) Name() string {
	return Name
}

func getPreFilterState(cycleState *framework.CycleState) (*preFilterState, *framework.Status) {
	value, err := cycleState.Read(stateKey)
	if err != nil {
		return nil, framework.AsStatus(err)
	}
	state := value.(*preFilterState)
	return state, nil
}

func (p *Plugin) PreFilter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) *framework.Status {
	state := &preFilterState{
		skip:                  true,
		podRequests:           make(corev1.ResourceList),
		preemptibleDevices:    map[string]map[schedulingv1alpha1.DeviceType]deviceResources{},
		matchedReservations:   map[string][]*frameworkext.ReservationInfo{},
		unmatchedReservations: map[string][]reservationAlloc{},
	}

	podRequests, _ := resource.PodRequestsAndLimits(pod)
	podRequests = apiext.TransformDeprecatedDeviceResources(podRequests)

	for deviceType := range DeviceResourceNames {
		switch deviceType {
		case schedulingv1alpha1.GPU:
			if !hasDeviceResource(podRequests, deviceType) {
				break
			}
			combination, err := ValidateGPURequest(podRequests)
			if err != nil {
				return framework.NewStatus(framework.Error, err.Error())
			}
			state.podRequests = quotav1.Add(state.podRequests, ConvertGPUResource(podRequests, combination))
			state.skip = false
		case schedulingv1alpha1.RDMA, schedulingv1alpha1.FPGA:
			if !hasDeviceResource(podRequests, deviceType) {
				break
			}
			if err := validateCommonDeviceRequest(podRequests, deviceType); err != nil {
				return framework.NewStatus(framework.Error, err.Error())
			}
			state.podRequests = quotav1.Add(state.podRequests, convertCommonDeviceResource(podRequests, deviceType))
			state.skip = false
		default:
			klog.Warningf("device type %v is not supported yet, pod: %v", deviceType, klog.KObj(pod))
		}
	}

	cycleState.Write(stateKey, state)
	return nil
}

func (p *Plugin) PreFilterExtensions() framework.PreFilterExtensions {
	return p
}

func (p *Plugin) AddPod(ctx context.Context, cycleState *framework.CycleState, podToSchedule *corev1.Pod, podInfoToAdd *framework.PodInfo, nodeInfo *framework.NodeInfo) *framework.Status {
	if reservationutil.IsReservePod(podInfoToAdd.Pod) {
		return nil
	}

	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	nd := p.nodeDeviceCache.getNodeDevice(podInfoToAdd.Pod.Spec.NodeName, false)
	if nd == nil {
		return nil
	}

	nd.lock.RLock()
	defer nd.lock.RUnlock()

	podAllocated := nd.getUsed(podInfoToAdd.Pod.Namespace, podInfoToAdd.Pod.Name)
	if len(podAllocated) == 0 {
		return nil
	}

	klog.V(5).Infof("DeviceShare.AddPod: podToSchedule %v, add podInfoToAdd: %v on node %s, allocatedDevices: %v",
		klog.KObj(podToSchedule), klog.KObj(podInfoToAdd.Pod), nodeInfo.Node().Name, podAllocated)

	preemptible := subtractAllocated(state.preemptibleDevices[podInfoToAdd.Pod.Spec.NodeName], podAllocated, true)
	if len(preemptible) == 0 {
		delete(state.preemptibleDevices, podInfoToAdd.Pod.Spec.NodeName)
	}
	return nil
}

func (p *Plugin) RemovePod(ctx context.Context, cycleState *framework.CycleState, podToSchedule *corev1.Pod, podInfoToRemove *framework.PodInfo, nodeInfo *framework.NodeInfo) *framework.Status {
	if reservationutil.IsReservePod(podInfoToRemove.Pod) {
		return nil
	}

	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	nd := p.nodeDeviceCache.getNodeDevice(podInfoToRemove.Pod.Spec.NodeName, false)
	if nd == nil {
		return nil
	}

	nd.lock.RLock()
	defer nd.lock.RUnlock()

	podAllocated := nd.getUsed(podInfoToRemove.Pod.Namespace, podInfoToRemove.Pod.Name)
	if len(podAllocated) == 0 {
		return nil
	}

	klog.V(5).Infof("DeviceShare.RemovePod: podToSchedule %v, remove podInfoToRemove: %v on node %s, allocatedDevices: %v",
		klog.KObj(podToSchedule), klog.KObj(podInfoToRemove.Pod), nodeInfo.Node().Name, podAllocated)

	preemptibleDevices := state.preemptibleDevices[podInfoToRemove.Pod.Spec.NodeName]
	state.preemptibleDevices[podInfoToRemove.Pod.Spec.NodeName] = appendAllocatedDevices(preemptibleDevices, podAllocated)
	return nil
}

func (p *Plugin) RestoreReservation(ctx context.Context, cycleState *framework.CycleState, podToSchedule *corev1.Pod, matched []*frameworkext.ReservationInfo, unmatched []*frameworkext.ReservationInfo, nodeInfo *framework.NodeInfo) *framework.Status {
	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	nodeName := nodeInfo.Node().Name
	nd := p.nodeDeviceCache.getNodeDevice(nodeName, false)
	if nd == nil {
		return nil
	}

	nd.lock.RLock()
	defer nd.lock.RUnlock()

	var filteredMatched []*frameworkext.ReservationInfo
	var filteredUnmatched []reservationAlloc
	for _, rInfo := range matched {
		namespacedName := reservationutil.GetReservationNamespacedName(rInfo.Reservation)
		allocated := nd.getUsed(namespacedName.Namespace, namespacedName.Name)
		if len(allocated) == 0 {
			continue
		}
		filteredMatched = append(filteredMatched, rInfo)
	}
	for _, rInfo := range unmatched {
		namespacedName := reservationutil.GetReservationNamespacedName(rInfo.Reservation)
		allocated := nd.getUsed(namespacedName.Namespace, namespacedName.Name)
		if len(allocated) == 0 {
			continue
		}
		for _, podRequirement := range rInfo.Pods {
			podAllocated := nd.getUsed(podRequirement.Namespace, podRequirement.Name)
			if len(podAllocated) == 0 {
				continue
			}
			allocated = subtractAllocated(allocated, podAllocated, true)
		}
		filteredUnmatched = append(filteredUnmatched, reservationAlloc{
			rInfo:    rInfo,
			remained: allocated,
		})
	}
	if len(filteredMatched) > 0 {
		state.matchedReservations[nodeName] = filteredMatched
	}
	if len(filteredUnmatched) > 0 {
		state.unmatchedReservations[nodeName] = filteredUnmatched
	}
	return nil
}

func (p *Plugin) restoreUnmatchedReservations(nodeName string, nodeDeviceInfo *nodeDevice, state *preFilterState) *nodeDevice {
	unmatched := state.unmatchedReservations[nodeName]
	if len(unmatched) > 0 {
		nodeDeviceInfo = nodeDeviceInfo.clone()
		for _, alloc := range unmatched {
			namespacedName := reservationutil.GetReservationNamespacedName(alloc.rInfo.Reservation)
			used := nodeDeviceInfo.getUsed(namespacedName.Namespace, namespacedName.Name)
			if len(used) == 0 {
				continue
			}
			nodeDeviceInfo.updateUsed(used, namespacedName.Namespace, namespacedName.Name, false)
			if len(alloc.remained) > 0 {
				nodeDeviceInfo.updateUsed(alloc.remained, namespacedName.Namespace, namespacedName.Name, true)
			}
		}
	}
	return nodeDeviceInfo
}

func (p *Plugin) Filter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}

	nodeDeviceInfo := p.nodeDeviceCache.getNodeDevice(node.Name, false)
	if nodeDeviceInfo == nil {
		return framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrMissingDevice)
	}

	originalNodeDeviceInfo := nodeDeviceInfo

	originalNodeDeviceInfo.lock.RLock()
	defer originalNodeDeviceInfo.lock.RUnlock()

	nodeDeviceInfo = p.restoreUnmatchedReservations(node.Name, originalNodeDeviceInfo, state)

	if matchedReservations := state.matchedReservations[node.Name]; len(matchedReservations) > 0 {
		for _, rInfo := range matchedReservations {
			namespacedName := reservationutil.GetReservationNamespacedName(rInfo.Reservation)
			preemptibleDevices := nodeDeviceInfo.getUsed(namespacedName.Namespace, namespacedName.Name)
			preemptibleDevices = appendAllocatedDevices(preemptibleDevices, state.preemptibleDevices[node.Name])
			allocateResult, err := p.allocator.Allocate(node.Name, pod, state.podRequests, nodeDeviceInfo, preemptibleDevices)
			if len(allocateResult) > 0 && err == nil {
				return nil
			}
		}
	}
	allocateResult, err := p.allocator.Allocate(node.Name, pod, state.podRequests, nodeDeviceInfo, state.preemptibleDevices[node.Name])
	if len(allocateResult) != 0 && err == nil {
		return nil
	}
	return framework.NewStatus(framework.Unschedulable, ErrInsufficientDevices)
}

func (p *Plugin) FilterReservation(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, reservation *schedulingv1alpha1.Reservation, nodeName string) *framework.Status {
	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	nodeDeviceInfo := p.nodeDeviceCache.getNodeDevice(nodeName, false)
	if nodeDeviceInfo == nil {
		return framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrMissingDevice)
	}

	originalNodeDeviceInfo := nodeDeviceInfo

	originalNodeDeviceInfo.lock.RLock()
	defer originalNodeDeviceInfo.lock.RUnlock()

	nodeDeviceInfo = p.restoreUnmatchedReservations(nodeName, originalNodeDeviceInfo, state)

	namespacedName := reservationutil.GetReservationNamespacedName(reservation)
	preemptibleDevices := nodeDeviceInfo.getUsed(namespacedName.Namespace, namespacedName.Name)
	preemptibleDevices = appendAllocatedDevices(preemptibleDevices, state.preemptibleDevices[nodeName])
	allocateResult, err := p.allocator.Allocate(nodeName, pod, state.podRequests, nodeDeviceInfo, preemptibleDevices)
	if len(allocateResult) == 0 || err != nil {
		return framework.NewStatus(framework.Unschedulable, ErrInsufficientDevices)
	}
	return nil
}

func (p *Plugin) Reserve(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, nodeName string) *framework.Status {
	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	nodeDeviceInfo := p.nodeDeviceCache.getNodeDevice(nodeName, false)
	if nodeDeviceInfo == nil {
		return framework.NewStatus(framework.UnschedulableAndUnresolvable, ErrMissingDevice)
	}

	originalNodeDeviceInfo := nodeDeviceInfo

	originalNodeDeviceInfo.lock.Lock()
	defer originalNodeDeviceInfo.lock.Unlock()

	nodeDeviceInfo = p.restoreUnmatchedReservations(nodeName, originalNodeDeviceInfo, state)

	reservedDevices := p.getReservationReservedDevices(cycleState, state, pod, nodeDeviceInfo, nodeName)
	if len(reservedDevices) > 0 {
		reservedDevices = appendAllocatedDevices(reservedDevices, state.preemptibleDevices[nodeName])
	} else {
		if matchedReservations := state.matchedReservations[nodeName]; len(matchedReservations) > 0 {
			for _, rInfo := range matchedReservations {
				namespacedName := reservationutil.GetReservationNamespacedName(rInfo.Reservation)
				reserved := nodeDeviceInfo.getUsed(namespacedName.Namespace, namespacedName.Name)
				reservedDevices = appendAllocatedDevices(reservedDevices, reserved)
			}
		}
		reservedDevices = appendAllocatedDevices(reservedDevices, state.preemptibleDevices[nodeName])
	}
	allocateResult, err := p.allocator.Allocate(nodeName, pod, state.podRequests, nodeDeviceInfo, reservedDevices)
	if err != nil || len(allocateResult) == 0 {
		return framework.NewStatus(framework.Unschedulable, ErrInsufficientDevices)
	}

	p.allocator.Reserve(pod, originalNodeDeviceInfo, allocateResult)
	state.allocationResult = allocateResult
	return nil
}

func (p *Plugin) getReservationReservedDevices(cycleState *framework.CycleState, state *preFilterState, pod *corev1.Pod, nodeDeviceInfo *nodeDevice, nodeName string) map[schedulingv1alpha1.DeviceType]deviceResources {
	if reservationutil.IsReservePod(pod) {
		return nil
	}

	reservation := frameworkext.GetNominatedReservation(cycleState)
	if reservation == nil {
		return nil
	}

	namespacedName := reservationutil.GetReservationNamespacedName(reservation)
	reservedDevices := nodeDeviceInfo.getUsed(namespacedName.Namespace, namespacedName.Name)
	return reservedDevices
}

func (p *Plugin) Unreserve(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, nodeName string) {
	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return
	}
	if state.skip {
		return
	}

	nodeDeviceInfo := p.nodeDeviceCache.getNodeDevice(nodeName, false)
	if nodeDeviceInfo == nil {
		return
	}

	nodeDeviceInfo.lock.Lock()
	defer nodeDeviceInfo.lock.Unlock()

	p.allocator.Unreserve(pod, nodeDeviceInfo, state.allocationResult)
	state.allocationResult = nil
}

func (p *Plugin) PreBind(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, nodeName string) *framework.Status {
	return p.preBindObject(ctx, cycleState, pod, nodeName)
}

func (p *Plugin) PreBindReservation(ctx context.Context, cycleState *framework.CycleState, reservation *schedulingv1alpha1.Reservation, nodeName string) *framework.Status {
	return p.preBindObject(ctx, cycleState, reservation, nodeName)
}

func (p *Plugin) preBindObject(ctx context.Context, cycleState *framework.CycleState, object runtime.Object, nodeName string) *framework.Status {
	state, status := getPreFilterState(cycleState)
	if !status.IsSuccess() {
		return status
	}
	if state.skip {
		return nil
	}

	originalObj := object.DeepCopyObject()
	metaObject := object.(metav1.Object)
	if err := apiext.SetDeviceAllocations(metaObject, state.allocationResult); err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}

	// NOTE: APIServer won't allow the following modification. Error: pod updates may not change fields other than
	// `spec.containers[*].image`, `spec.initContainers[*].image`, `spec.activeDeadlineSeconds`,
	// `spec.tolerations` (only additions to existing tolerations) or `spec.terminationGracePeriodSeconds`

	// podRequest := state.convertedDeviceResource
	// if _, ok := allocResult[schedulingv1alpha1.GPU]; ok {
	// 	patchContainerGPUResource(newPod, podRequest)
	// }

	// patch pod or reservation (if the pod is a reserve pod) with new annotations
	err := util.RetryOnConflictOrTooManyRequests(func() error {
		_, err1 := util.NewPatch().WithHandle(p.handle).AddAnnotations(metaObject.GetAnnotations()).Patch(ctx, originalObj.(metav1.Object))
		return err1
	})
	if err != nil {
		klog.V(3).ErrorS(err, "Failed to preBind %T with DeviceShare", object, klog.KObj(metaObject), "Devices", state.allocationResult, "node", nodeName)
		return framework.NewStatus(framework.Error, err.Error())
	}
	klog.V(4).Infof("Successfully preBind %T %v", object, klog.KObj(metaObject))

	return nil
}

func (p *Plugin) getNodeDeviceSummary(nodeName string) (*NodeDeviceSummary, bool) {
	return p.nodeDeviceCache.getNodeDeviceSummary(nodeName)
}

func (p *Plugin) getAllNodeDeviceSummary() map[string]*NodeDeviceSummary {
	return p.nodeDeviceCache.getAllNodeDeviceSummary()
}

func New(obj runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	args, ok := obj.(*config.DeviceShareArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type DeviceShareArgs, got %T", obj)
	}

	extendedHandle, ok := handle.(frameworkext.ExtendedHandle)
	if !ok {
		return nil, fmt.Errorf("expect handle to be type frameworkext.ExtendedHandle, got %T", handle)
	}

	deviceCache := newNodeDeviceCache()
	registerDeviceEventHandler(deviceCache, extendedHandle.KoordinatorSharedInformerFactory())
	registerPodEventHandler(deviceCache, handle.SharedInformerFactory(), extendedHandle.KoordinatorSharedInformerFactory())

	allocatorOpts := AllocatorOptions{
		SharedInformerFactory:      extendedHandle.SharedInformerFactory(),
		KoordSharedInformerFactory: extendedHandle.KoordinatorSharedInformerFactory(),
	}
	allocator := NewAllocator(args.Allocator, allocatorOpts)

	return &Plugin{
		handle:          handle,
		nodeDeviceCache: deviceCache,
		allocator:       allocator,
	}, nil
}
