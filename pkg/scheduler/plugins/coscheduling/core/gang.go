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

package core

import (
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	GangFromPodGroup          string = "GangFromPodGroup"
	GangFromPodAnnotation     string = "GangFromPodAnnotation"
	PodGroupFromPodAnnotation string = "PodGroupFromPodAnnotation"
)

// Gang is an abstraction of PodGroup and custom Gang protocol, recording GangSpec and current state
type Gang struct {
	// These exported fields are read-only.
	Name              string
	Spec              GangSpec
	SpecInitialized   bool
	CreationTimestamp time.Time

	// The following fields record Gang's current state data and are modifiable
	lock sync.Mutex
	// Pods records all associated Pods
	pods map[string]*corev1.Pod
	// waitingPods that Pods have already assumed(waiting in Permit stage)
	waitingPods map[string]*corev1.Pod
	// boundPods that Pods have already bound
	boundPods map[string]*corev1.Pod
	// resourceSatisfied indicates whether the Gang has ever reached the ResourceSatisfied state.
	// Once this variable is set true, it is irreversible.
	resourceSatisfied bool

	// These fields used to count the cycle
	// For example, at the beginning, `scheduleCycle` is 1, and each pod's cycle in `podScheduleCycles` is 0. When each pod comes to PreFilter,
	// we will check if the pod's value in `podScheduleCycles` is smaller than Gang's `scheduleCycle`, If result is positive,
	// we set the pod's cycle in `podScheduleCycles` equal with `scheduleCycle` and pass the check. If result is negative, means
	// the pod has been scheduled in this cycle, so we should reject it. With `totalChildrenNum`'s help, when the last pod comes to make all
	// `podScheduleCycles`'s values equal to `scheduleCycle`, Gang's `scheduleCycle` will be added by 1, which means a new schedule cycle.
	scheduleCycleValid bool
	scheduleCycle      int
	podScheduleCycles  map[string]int
}

func NewGang(gangFullName string) *Gang {
	return &Gang{
		Name:               gangFullName,
		pods:               map[string]*corev1.Pod{},
		waitingPods:        map[string]*corev1.Pod{},
		boundPods:          map[string]*corev1.Pod{},
		scheduleCycleValid: true,
		scheduleCycle:      1,
		podScheduleCycles:  map[string]int{},
	}
}

func (gang *Gang) isPermitAllowed() bool {
	gang.lock.Lock()
	defer gang.lock.Unlock()
	return gang.resourceSatisfied || len(gang.waitingPods) >= gang.Spec.MinMember
}

func (gang *Gang) isResourceSatisfied() bool {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	return gang.resourceSatisfied
}

func (gang *Gang) setResourceSatisfied() {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	if !gang.resourceSatisfied {
		gang.resourceSatisfied = true
		klog.Infof("Gang ResourceSatisfied, gangName: %v", gang.Name)
	}
}

func (gang *Gang) getPodsTotalNum() int {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	return len(gang.pods)
}

func (gang *Gang) getPods() (pods []*corev1.Pod) {
	gang.lock.Lock()
	defer gang.lock.Unlock()
	pods = make([]*corev1.Pod, 0, len(gang.pods))
	for _, pod := range gang.pods {
		pods = append(pods, pod)
	}
	return
}

func (gang *Gang) addPod(pod *corev1.Pod) {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	podNamespacedName := GetNamespacedName(pod)
	if _, ok := gang.pods[podNamespacedName]; !ok {
		gang.pods[podNamespacedName] = pod
		klog.Infof("SetChild, gangName: %v, childName: %v", gang.Name, podNamespacedName)
	}
}

func (gang *Gang) deletePod(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}

	podNamespacedName := GetNamespacedName(pod)
	klog.Infof("Delete pod from gang: %v, podName: %v", gang.Name, podNamespacedName)

	gang.lock.Lock()
	defer gang.lock.Unlock()

	delete(gang.pods, podNamespacedName)
	delete(gang.waitingPods, podNamespacedName)
	delete(gang.boundPods, podNamespacedName)
	delete(gang.podScheduleCycles, podNamespacedName)
	if gang.Spec.GangFrom == GangFromPodAnnotation {
		if len(gang.pods) == 0 {
			return true
		}
	}
	return false
}

func (gang *Gang) addWaitingPod(pod *corev1.Pod) {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	podNamespacedName := GetNamespacedName(pod)
	if _, ok := gang.waitingPods[podNamespacedName]; !ok {
		gang.waitingPods[podNamespacedName] = pod
		klog.Infof("AddAssumedPod, gangName: %v, podName: %v", gang.Name, podNamespacedName)
	}
}

func (gang *Gang) delWaitingPod(pod *corev1.Pod) {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	podNamespacedName := GetNamespacedName(pod)
	if _, ok := gang.waitingPods[podNamespacedName]; ok {
		delete(gang.waitingPods, podNamespacedName)
		klog.Infof("delWaitingPod, gangName: %v, podName: %v", gang.Name, podNamespacedName)
	}
}

func (gang *Gang) getBoundPodsNum() int {
	gang.lock.Lock()
	defer gang.lock.Unlock()
	return len(gang.boundPods)
}

func (gang *Gang) addBoundPod(pod *corev1.Pod) {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	podNamespacedName := GetNamespacedName(pod)
	delete(gang.waitingPods, podNamespacedName)
	gang.boundPods[podNamespacedName] = pod

	klog.Infof("AddBoundPod, gangName: %v, podName: %v", gang.Name, podNamespacedName)
	if len(gang.boundPods) >= gang.Spec.MinMember {
		gang.resourceSatisfied = true
		klog.Infof("Gang ResourceSatisfied due to addBoundPod, gangName: %v", gang.Name)
	}
}

func (gang *Gang) getScheduleCycle() int {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	return gang.scheduleCycle
}

func (gang *Gang) tryStartNewScheduleCycle() {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	count := 0
	for _, cycle := range gang.podScheduleCycles {
		if cycle == gang.scheduleCycle {
			count++
		}
	}

	if count == gang.Spec.TotalMember {
		gang.scheduleCycleValid = true
		gang.scheduleCycle += 1
		klog.Infof("Gang %q start new schedule cycle. scheduleCycle: %v, scheduleCycleValid: %v",
			gang.Name, gang.scheduleCycle, gang.scheduleCycleValid)
	}
}

func (gang *Gang) setPodScheduleCycle(pod *corev1.Pod, childCycle int) {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	podNamespacedName := GetNamespacedName(pod)
	gang.podScheduleCycles[podNamespacedName] = childCycle
	klog.Infof("setPodScheduleCycle, pod: %v, childCycle: %v", podNamespacedName, childCycle)
}

func (gang *Gang) getPodScheduleCycle(pod *corev1.Pod) int {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	podId := GetNamespacedName(pod)
	return gang.podScheduleCycles[podId]
}

func (gang *Gang) isScheduleCycleValid() bool {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	return gang.scheduleCycleValid
}

func (gang *Gang) setScheduleCycleValid(valid bool) {
	gang.lock.Lock()
	defer gang.lock.Unlock()

	gang.scheduleCycleValid = valid
	klog.Infof("SetScheduleCycleValid, gangName: %v, valid: %v", gang.Name, valid)
}
