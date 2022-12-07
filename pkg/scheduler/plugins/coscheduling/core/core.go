/*
Copyright 2022 The Koordinator Authors.
Copyright 2020 The Kubernetes Authors.

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

package core

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"
	pgclientset "sigs.k8s.io/scheduler-plugins/pkg/generated/clientset/versioned"
	pgformers "sigs.k8s.io/scheduler-plugins/pkg/generated/informers/externalversions"
	pglister "sigs.k8s.io/scheduler-plugins/pkg/generated/listers/scheduling/v1alpha1"

	"github.com/koordinator-sh/koordinator/apis/extension"
	frameworkexthelper "github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext/helper"
)

type Status string

const (
	// PodGroupNotSpecified denotes no PodGroup is specified in the Pod spec.
	PodGroupNotSpecified Status = "PodGroup not specified"
	// PodGroupNotFound denotes the specified PodGroup in the Pod spec is
	// not found in API server.
	PodGroupNotFound Status = "PodGroup not found"
	Success          Status = "Success"
	Wait             Status = "Wait"
)

// Manager defines the interfaces for PodGroup management.
type Manager interface {
	PreFilter(context.Context, *corev1.Pod) error
	Permit(context.Context, *corev1.Pod) (time.Duration, Status)
	PostBind(context.Context, *corev1.Pod, string)
	PostFilter(context.Context, *corev1.Pod, framework.Handle, string) (*framework.PostFilterResult, *framework.Status)
	Unreserve(context.Context, *framework.CycleState, *corev1.Pod, string, framework.Handle, string)
	ActivateSiblings(*corev1.Pod, *framework.CycleState)
	AllowGang(*corev1.Pod, framework.Handle, string)
	GetGang(pod *corev1.Pod) *Gang
	GetCreationTimestamp(*corev1.Pod, time.Time) time.Time
	GetAllPodsFromGang(string) []*corev1.Pod
	GetGangSummary(string) (*GangSummary, bool)
	GetGangSummaries() map[string]*GangSummary
}

// PodGroupManager defines the scheduling operation called
type PodGroupManager struct {
	// pgClient is a PodGroup client
	pgClient pgclientset.Interface
	// pgLister is PodGroup lister
	pgLister pglister.PodGroupLister
	// reserveResourcePercentage is the reserved resource for the max finished group, range (0,100]
	reserveResourcePercentage int32
	// cache stores gang info
	cache *GangCache
}

// NewPodGroupManager creates a new operation object.
func NewPodGroupManager(
	pgClient pgclientset.Interface,
	pgSharedInformerFactory pgformers.SharedInformerFactory,
	sharedInformerFactory informers.SharedInformerFactory,
	scheduleTimeout *metav1.Duration,
) *PodGroupManager {
	defaultWaitTime := DefaultWaitTime
	if scheduleTimeout != nil && scheduleTimeout.Duration > 0 {
		defaultWaitTime = scheduleTimeout.Duration
	}
	pgInformer := pgSharedInformerFactory.Scheduling().V1alpha1().PodGroups()
	podInformer := sharedInformerFactory.Core().V1().Pods()
	gangCache := NewGangCache(defaultWaitTime, podInformer.Lister(), pgInformer.Lister(), pgClient)

	podGroupEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    gangCache.onPodGroupAdd,
		DeleteFunc: gangCache.onPodGroupDelete,
	}
	frameworkexthelper.ForceSyncFromInformer(context.TODO().Done(), pgSharedInformerFactory, pgInformer.Informer(), podGroupEventHandler)

	podEventHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    gangCache.onPodAdd,
		DeleteFunc: gangCache.onPodDelete,
	}
	frameworkexthelper.ForceSyncFromInformer(context.TODO().Done(), sharedInformerFactory, podInformer.Informer(), podEventHandler)
	pgMgr := &PodGroupManager{
		pgClient: pgClient,
		pgLister: pgInformer.Lister(),
		cache:    gangCache,
	}
	return pgMgr
}

// ActivateSiblings stashes the pods belonging to the same Gang of the given pod
// in the given state, with a reserved key "kubernetes.io/pods-to-activate".
func (pgMgr *PodGroupManager) ActivateSiblings(pod *corev1.Pod, state *framework.CycleState) {
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		return
	}

	var toActivePods []*corev1.Pod
	for _, gangFullName := range gang.Spec.Groups {
		g := pgMgr.cache.GetGang(gangFullName)
		if g != nil {
			toActivePods = append(toActivePods, g.getPods()...)
		}
	}

	if len(toActivePods) != 0 {
		if c, err := state.Read(framework.PodsToActivateKey); err == nil {
			if s, ok := c.(*framework.PodsToActivate); ok {
				s.Lock()
				for _, p := range toActivePods {
					if p.UID != pod.UID {
						namespacedName := GetNamespacedName(p)
						s.Map[namespacedName] = p
						klog.V(4).InfoS("ActivateSiblings add pod's key to PodsToActivate map", "pod", namespacedName)
					}
				}
				s.Unlock()
			}
		}
	}
}

// PreFilter filters out a pod if
// - Whether the Gang timed out, was not initialized, or failed to initialize.
// - Whether the total number of Pods in the Gang is less than its minMember.
// - Whether the Gang met the scheduleCycleValid check.
// - Whether the Gang is ResourceSatisfied
func (pgMgr *PodGroupManager) PreFilter(ctx context.Context, pod *corev1.Pod) error {
	klog.V(5).InfoS("Pre-filter", "pod", klog.KObj(pod))
	if !IsPodNeedGang(pod) {
		return nil
	}
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		return fmt.Errorf("pre-filter not found Gang %q", GetGangFullName(pod))
	}
	if !gang.SpecInitialized {
		return fmt.Errorf("pre-filter Gang %q is not yet initialized", gang.Name)
	}
	if gang.Spec.ParseError != nil {
		return fmt.Errorf("pre-filter Gang %q has parse errors %w", gang.Name, gang.Spec.ParseError)
	}

	gang.tryStartNewScheduleCycle()
	scheduleCycle := gang.getScheduleCycle()
	defer gang.setPodScheduleCycle(pod, scheduleCycle)

	if gang.isResourceSatisfied() {
		return nil
	}

	if gang.getPodsTotalNum() < gang.Spec.MinMember {
		return fmt.Errorf("pre-filter Gang %q cannot find enough sibling pods, "+
			"current pods number: %v, minMember of Gang: %v", gang.Name, gang.getPodsTotalNum(), gang.Spec.MinMember)
	}

	if gang.Spec.Mode == extension.GangModeStrict {
		if !gang.isScheduleCycleValid() {
			return fmt.Errorf("pre-filter pod with Gang %q failed in the last cycle, and the new cycle has not started yet", gang.Name)
		}
		podScheduleCycle := gang.getPodScheduleCycle(pod)
		if podScheduleCycle >= scheduleCycle {
			return fmt.Errorf("pre-filter pod's schedule cycle is too large, Gang %q, podScheduleCycle: %v, scheduleCycle: %v",
				gang.Name, podScheduleCycle, scheduleCycle)
		}
	}
	return nil
}

// PostFilter rejects waitingPods if Gang's mode is Strict
func (pgMgr *PodGroupManager) PostFilter(ctx context.Context, pod *corev1.Pod, handle framework.Handle, pluginName string) (*framework.PostFilterResult, *framework.Status) {
	if !IsPodNeedGang(pod) {
		return &framework.PostFilterResult{}, framework.NewStatus(framework.Unschedulable, "")
	}
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		klog.InfoS("Pod does not belong to any gang", "pod", klog.KObj(pod))
		return &framework.PostFilterResult{}, framework.NewStatus(framework.Unschedulable, "can not find gang")
	}
	if gang.isResourceSatisfied() {
		return &framework.PostFilterResult{}, framework.NewStatus(framework.Unschedulable, "")
	}

	if gang.Spec.Mode == extension.GangModeStrict {
		handle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
			if GetGangFullName(waitingPod.GetPod()) == gang.Name {
				klog.InfoS("postFilter rejects the pod", "gang", gang.Name, "pod", klog.KObj(waitingPod.GetPod()))
				waitingPod.Reject(pluginName, "gang rejection in PostFilter")
			}
		})
		gang.setScheduleCycleValid(false)
		return &framework.PostFilterResult{}, framework.NewStatus(framework.Unschedulable,
			fmt.Sprintf("Gang %v gets rejected due to Pod %v is unschedulable even after PostFilter", gang.Name, pod.Name))
	}

	return &framework.PostFilterResult{}, framework.NewStatus(framework.Unschedulable, "")
}

// Permit is the functions invoked by the framework at "Permit" extension point.
func (pgMgr *PodGroupManager) Permit(ctx context.Context, pod *corev1.Pod) (time.Duration, Status) {
	if !IsPodNeedGang(pod) {
		return 0, PodGroupNotSpecified
	}
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		return 0, PodGroupNotFound
	}
	gang.addWaitingPod(pod)

	allowed := true
	if len(gang.Spec.Groups) == 1 {
		allowed = gang.isPermitAllowed()
	} else {
		for _, gangFullName := range gang.Spec.Groups {
			g := pgMgr.cache.GetGang(gangFullName)
			if g == nil || !g.isPermitAllowed() {
				allowed = false
				break
			}
		}
	}
	if !allowed {
		return gang.Spec.WaitTime, Wait
	}
	return 0, Success
}

func (pgMgr *PodGroupManager) AllowGang(pod *corev1.Pod, handle framework.Handle, pluginName string) {
	if !IsPodNeedGang(pod) {
		return
	}
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		klog.InfoS("Pod does not belong to any gang", "pod", klog.KObj(pod))
		return
	}

	gangFullNames := sets.NewString(gang.Spec.Groups...)
	handle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
		waitingGangFullName := GetGangFullName(waitingPod.GetPod())
		if gangFullNames.Has(waitingGangFullName) {
			klog.InfoS("Permit allows pod from gang", "gang", waitingGangFullName, "pod", klog.KObj(waitingPod.GetPod()))
			waitingPod.Allow(pluginName)
		}
	})
}

// Unreserve rejects all other Pods in the PodGroup when one of the pods in the group times out.
func (pgMgr *PodGroupManager) Unreserve(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string, handle framework.Handle, pluginName string) {
	if !IsPodNeedGang(pod) {
		return
	}
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		klog.InfoS("Pod does not belong to any gang", "pod", klog.KObj(pod))
		return
	}
	gang.delWaitingPod(pod)

	if !gang.isResourceSatisfied() && gang.Spec.Mode == extension.GangModeStrict {
		// release resource of all assumed Pods of the gang
		handle.IterateOverWaitingPods(func(waitingPod framework.WaitingPod) {
			if GetGangFullName(waitingPod.GetPod()) == gang.Name {
				klog.InfoS("unReserve rejects the pod from Gang", "gang", gang.Name, "pod", klog.KObj(pod))
				waitingPod.Reject(pluginName, "rejection in Unreserve")
			}
		})
	}
}

// PostBind updates a PodGroup's status.
func (pgMgr *PodGroupManager) PostBind(ctx context.Context, pod *corev1.Pod, nodeName string) {
	if !IsPodNeedGang(pod) {
		return
	}
	gang := pgMgr.GetGang(pod)
	if gang == nil {
		klog.InfoS("Pod does not belong to any gang", "pod", klog.KObj(pod))
		return
	}
	gang.addBoundPod(pod)

	_, pg := pgMgr.GetPodGroup(pod)
	if pg == nil {
		return
	}
	pgCopy := pg.DeepCopy()
	pgCopy.Status.Scheduled = int32(gang.getBoundPodsNum())

	if pgCopy.Status.Scheduled >= pgCopy.Spec.MinMember {
		pgCopy.Status.Phase = v1alpha1.PodGroupScheduled
		klog.InfoS("PostBind has got enough bound child for gang", "gang", gang.Name, "pod", klog.KObj(pod))
	} else {
		pgCopy.Status.Phase = v1alpha1.PodGroupScheduling
		klog.InfoS("PostBind has not got enough bound child for gang", "gang", gang.Name, "pod", klog.KObj(pod))
		if pgCopy.Status.ScheduleStartTime.IsZero() {
			pgCopy.Status.ScheduleStartTime = metav1.Time{Time: time.Now()}
		}
	}
	if pgCopy.Status.Phase != pg.Status.Phase {
		pg, err := pgMgr.pgLister.PodGroups(pgCopy.Namespace).Get(pgCopy.Name)
		if err != nil {
			klog.ErrorS(err, "PosFilter failed to get PodGroup", "podGroup", klog.KObj(pgCopy))
			return
		}
		patch, err := CreateMergePatch(pg, pgCopy)
		if err != nil {
			klog.ErrorS(err, "PostFilter failed to create merge patch", "podGroup", klog.KObj(pg), "podGroup", klog.KObj(pgCopy))
			return
		}
		if err := pgMgr.PatchPodGroup(pg.Name, pg.Namespace, patch); err != nil {
			klog.ErrorS(err, "PostFilter Failed to patch", "podGroup", klog.KObj(pg))
			return
		} else {
			klog.InfoS("PostFilter success to patch podGroup", "podGroup", klog.KObj(pgCopy))
		}
	}
}

// GetGang returns the Gang that a Pod belongs to in cache.
func (pgMgr *PodGroupManager) GetGang(pod *corev1.Pod) *Gang {
	gangFullName := GetGangFullName(pod)
	if gangFullName == "" {
		return nil
	}
	gang := pgMgr.cache.GetGang(gangFullName)
	return gang
}

func (pgMgr *PodGroupManager) GetCreationTimestamp(pod *corev1.Pod, ts time.Time) time.Time {
	if !IsPodNeedGang(pod) {
		return ts
	}
	gang := pgMgr.GetGang(pod)
	if gang != nil && gang.SpecInitialized {
		return gang.CreationTimestamp
	}
	return ts
}

// PatchPodGroup patches a podGroup.
func (pgMgr *PodGroupManager) PatchPodGroup(pgName string, namespace string, patch []byte) error {
	if len(patch) == 0 {
		return nil
	}
	_, err := pgMgr.pgClient.SchedulingV1alpha1().PodGroups(namespace).Patch(context.TODO(), pgName,
		types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// GetPodGroup returns the PodGroup that a Pod belongs to in cache.
func (pgMgr *PodGroupManager) GetPodGroup(pod *corev1.Pod) (string, *v1alpha1.PodGroup) {
	pgName := GetGangName(pod)
	if len(pgName) == 0 {
		return "", nil
	}
	pg, err := pgMgr.pgLister.PodGroups(pod.Namespace).Get(pgName)
	if err != nil {
		return fmt.Sprintf("%v/%v", pod.Namespace, pgName), nil
	}
	return fmt.Sprintf("%v/%v", pod.Namespace, pgName), pg
}

func (pgMgr *PodGroupManager) GetAllPodsFromGang(gangFullName string) []*corev1.Pod {
	gang := pgMgr.cache.GetGang(gangFullName)
	if gang == nil {
		return nil
	}
	return gang.getPods()
}

func (pgMgr *PodGroupManager) GetGangSummary(gangFullName string) (*GangSummary, bool) {
	gang := pgMgr.cache.GetGang(gangFullName)
	if gang == nil {
		return nil, false
	}
	return gang.GetGangSummary(), true
}

func (pgMgr *PodGroupManager) GetGangSummaries() map[string]*GangSummary {
	result := make(map[string]*GangSummary)
	allGangs := pgMgr.cache.GetAllGangs()
	for gangName, gang := range allGangs {
		result[gangName] = gang.GetGangSummary()
	}
	return result
}
