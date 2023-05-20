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

package hijack

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/api/v1/resource"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
	"github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext"
	"github.com/koordinator-sh/koordinator/pkg/util"
)

const (
	Name = "Hijack"
)

const (
	LabelPodHijackable = apiext.SchedulingDomainPrefix + "/hijackable"
)

var _ framework.PreFilterPlugin = &Plugin{}
var _ framework.ReservePlugin = &Plugin{}
var _ framework.PreBindPlugin = &Plugin{}
var _ framework.BindPlugin = &Plugin{}

// Plugin hijacks the assumed Pod to reserve pod or inplace update pod, and must set the first order.
type Plugin struct {
	extendedHandle frameworkext.ExtendedHandle
}

func New(args runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	extendedHandle := handle.(frameworkext.ExtendedHandle)
	return &Plugin{
		extendedHandle: extendedHandle,
	}, nil
}

func (pl *Plugin) Name() string {
	return Name
}

type stateData struct {
	originalAssumedPod *corev1.Pod
	originalPod        *corev1.Pod
	currentPod         *corev1.Pod
}

func (s *stateData) Clone() framework.StateData {
	return s
}

func (pl *Plugin) PreFilter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) *framework.Status {
	if pod.Annotations["hijacked"] == "true" {
		return framework.NewStatus(framework.Error, "pod has been hijacked...")
	}
	return nil
}

func (pl *Plugin) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func (pl *Plugin) Reserve(ctx context.Context, cycleState *framework.CycleState, assumedPod *corev1.Pod, nodeName string) *framework.Status {
	if assumedPod.Labels[LabelPodHijackable] != "true" {
		return nil
	}
	klog.V(4).InfoS("Pod is hijackable, we will hijack it!", "pod", klog.KObj(assumedPod))

	nominatedReservation := frameworkext.GetNominatedReservation(cycleState)
	if nominatedReservation == nil {
		klog.V(4).InfoS("There is no nominated reservation to cooperate with hijacking, skip it", "pod", klog.KObj(assumedPod))
		return framework.AsStatus(fmt.Errorf("no nominated reservation"))
	}

	reservePod := nominatedReservation.GetReservePod()
	if !apiext.IsReservationOperatingMode(reservePod) {
		klog.V(4).InfoS("Target Pod is not operating pod, stop hijacking", "pod", klog.KObj(assumedPod), "target", klog.KObj(nominatedReservation))
		return nil
	}

	assumedPodRequests, _ := resource.PodRequestsAndLimits(assumedPod)
	assumedPodRequests = quotav1.Mask(assumedPodRequests, nominatedReservation.ResourceNames)
	remained := quotav1.SubtractWithNonNegativeResult(assumedPodRequests, nominatedReservation.Allocatable)
	if quotav1.IsZero(remained) {
		klog.V(4).InfoS("AssumedPod don't need to be hijack since the resources is satisfied", "pod", klog.KObj(assumedPod))
		return nil
	}

	reservePod = reservePod.DeepCopy()
	originalReservePod := reservePod.DeepCopy()
	originalAssumedPod := assumedPod.DeepCopy()

	// scale out the pod
	for i := range reservePod.Spec.Containers {
		container := &reservePod.Spec.Containers[i]
		var targetContainer *corev1.Container
		for j := range assumedPod.Spec.Containers {
			if assumedPod.Spec.Containers[j].Name == container.Name {
				targetContainer = &assumedPod.Spec.Containers[i]
				break
			}
		}
		if targetContainer == nil {
			continue
		}
		if equality.Semantic.DeepEqual(container.Resources, targetContainer.Resources) {
			continue
		}
		container.Resources = targetContainer.Resources
	}
	if err := pl.extendedHandle.Scheduler().GetCache().UpdatePod(originalReservePod, reservePod); err != nil {
		klog.ErrorS(err, "Failed to UpdatePod in SchedulerCache", "hijackedPod", klog.KObj(assumedPod), "pod", klog.KObj(originalReservePod))
		return framework.AsStatus(err)
	}

	// remove the pod from the scheduler cache to ensure the NodeInfo.Requested is correctly.
	err := pl.extendedHandle.Scheduler().GetCache().ForgetPod(assumedPod)
	if err != nil {
		klog.ErrorS(err, "Failed to forget assumed pod", "pod", klog.KObj(assumedPod))
		return framework.AsStatus(err)
	}
	assumedPod.Name = reservePod.Name
	assumedPod.Namespace = reservePod.Namespace
	assumedPod.UID = reservePod.UID
	cycleState.Write(Name, &stateData{
		originalAssumedPod: originalAssumedPod,
		originalPod:        originalReservePod,
		currentPod:         reservePod,
	})
	klog.InfoS("Pod is hijacked by target Pod", "hijackedPod", klog.KObj(originalAssumedPod), "pod", klog.KObj(reservePod))
	return nil
}

func (pl *Plugin) Unreserve(ctx context.Context, cycleState *framework.CycleState, assumedPod *corev1.Pod, nodeName string) {
	s, err := cycleState.Read(Name)
	if err != nil {
		return
	}
	state := s.(*stateData)

	if err := pl.extendedHandle.Scheduler().GetCache().UpdatePod(state.currentPod, state.originalPod); err != nil {
		klog.ErrorS(err, "Failed to rollback the Pod", "pod", klog.KObj(state.originalPod))
	}

	assumedPod.Name = state.originalAssumedPod.Name
	assumedPod.Namespace = state.originalAssumedPod.Namespace
	assumedPod.UID = state.originalAssumedPod.UID
	if err := pl.extendedHandle.Scheduler().GetCache().AssumePod(assumedPod); err != nil {
		klog.ErrorS(err, "Failed rollback to assume pod", "pod", klog.KObj(assumedPod))
	}
}

func (pl *Plugin) PreBind(ctx context.Context, cycleState *framework.CycleState, assumedPod *corev1.Pod, nodeName string) *framework.Status {
	s, err := cycleState.Read(Name)
	if err != nil {
		return nil
	}
	state := s.(*stateData)
	assumedPod.Name = state.originalAssumedPod.Name
	assumedPod.Namespace = state.originalAssumedPod.Namespace
	assumedPod.UID = state.originalAssumedPod.UID
	return nil
}

func (pl *Plugin) Bind(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, nodeName string) *framework.Status {
	s, err := cycleState.Read(Name)
	if err != nil {
		return framework.NewStatus(framework.Skip)
	}
	state := s.(*stateData)

	err = util.RetryOnConflictOrTooManyRequests(func() error {
		_, err := util.NewPatch().WithClientset(pl.extendedHandle.ClientSet()).PatchPod(ctx, state.originalPod, state.currentPod)
		return err
	})
	if err != nil {
		klog.ErrorS(err, "Failed to update target pod", "targetPod", klog.KObj(state.originalPod))
		return framework.AsStatus(err)
	}

	assumedPod := state.originalAssumedPod.DeepCopy()
	if assumedPod.Annotations == nil {
		assumedPod.Annotations = map[string]string{}
	}
	assumedPod.Annotations["hijacked"] = "true"

	err = util.RetryOnConflictOrTooManyRequests(func() error {
		_, err := util.NewPatch().WithClientset(pl.extendedHandle.ClientSet()).PatchPod(ctx, state.originalAssumedPod, assumedPod)
		return err
	})
	if err != nil {
		klog.ErrorS(err, "Failed to patch hijacked annotation to pod", "pod", klog.KObj(assumedPod))
		return framework.AsStatus(err)
	}

	return framework.NewStatus(framework.Success)
}
