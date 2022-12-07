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
	"context"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"
	pgclientset "sigs.k8s.io/scheduler-plugins/pkg/generated/clientset/versioned"
	pglister "sigs.k8s.io/scheduler-plugins/pkg/generated/listers/scheduling/v1alpha1"
)

type GangCache struct {
	defaultWaitTime time.Duration
	podLister       listerv1.PodLister
	pgLister        pglister.PodGroupLister
	pgClient        pgclientset.Interface
	lock            *sync.RWMutex
	gangItems       map[string]*Gang
}

func NewGangCache(defaultWaitTime time.Duration, podLister listerv1.PodLister, pgLister pglister.PodGroupLister, client pgclientset.Interface) *GangCache {
	return &GangCache{
		defaultWaitTime: defaultWaitTime,
		podLister:       podLister,
		pgLister:        pgLister,
		pgClient:        client,
		gangItems:       make(map[string]*Gang),
		lock:            new(sync.RWMutex),
	}
}

func (gangCache *GangCache) GetGang(gangFullName string) *Gang {
	gangCache.lock.Lock()
	defer gangCache.lock.Unlock()
	gang := gangCache.gangItems[gangFullName]
	return gang
}

func (gangCache *GangCache) GetAllGangs() map[string]*Gang {
	gangCache.lock.RLock()
	defer gangCache.lock.RUnlock()

	result := make(map[string]*Gang)
	for gangId, gang := range gangCache.gangItems {
		result[gangId] = gang
	}

	return result
}

func (gangCache *GangCache) DeleteGang(gangFullName string) {
	gangCache.lock.Lock()
	defer gangCache.lock.Unlock()

	delete(gangCache.gangItems, gangFullName)
	klog.Infof("delete gang from cache, gang: %v", gangFullName)
}

func (gangCache *GangCache) getOrInitGang(gangFullName string, obj metav1.Object) *Gang {
	gangCache.lock.Lock()
	defer gangCache.lock.Unlock()

	gang := gangCache.gangItems[gangFullName]
	if gang == nil {
		gang = NewGang(gangFullName)
		gangCache.gangItems[gangFullName] = gang
	}
	if !gang.SpecInitialized {
		if pod, ok := obj.(*v1.Pod); !ok || GetPodGroupLabel(pod) == "" {
			gang.CreationTimestamp = obj.GetCreationTimestamp().Time
			gang.Spec = NewGangSpec(obj, gangCache.defaultWaitTime)
			gang.SpecInitialized = true
		}
	}
	return gang
}

func (gangCache *GangCache) onPodAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	gangFullName := GetGangFullName(pod)
	if gangFullName == "" {
		return
	}

	gang := gangCache.getOrInitGang(gangFullName, pod)
	gang.addPod(pod)
	if pod.Spec.NodeName != "" {
		gang.addBoundPod(pod)
		gang.setResourceSatisfied()
	}
	if !gang.SpecInitialized {
		return
	}
	if gang.Spec.ParseError != nil {
		klog.Errorf("Invalid Gang with Pod %v, err: %v", klog.KObj(pod), gang.Spec.ParseError)
		return
	}

	// TODO: we should create the PodGroup asynchronously
	shouldCreatePodGroup := GetPodGroupLabel(pod) == ""
	if shouldCreatePodGroup {
		_, err := gangCache.pgLister.PodGroups(pod.Namespace).Get(GetGangName(pod))
		if errors.IsNotFound(err) {
			pgFromAnnotation := generateNewPodGroup(&gang.Spec, pod)
			err := retry.OnError(
				retry.DefaultRetry,
				errors.IsTooManyRequests,
				func() error {
					_, err := gangCache.pgClient.SchedulingV1alpha1().PodGroups(pod.Namespace).Create(context.TODO(), pgFromAnnotation, metav1.CreateOptions{})
					return err
				})
			if err != nil {
				klog.Errorf("Failed to create PodGroup with Pod %v, err: %v", klog.KObj(pod), err)
			} else {
				klog.Infof("Successfully create PodGroup %s with Pod %v", gangFullName, klog.KObj(pod))
			}
		}
	}
}

func (gangCache *GangCache) onPodDelete(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		pod, _ = t.Obj.(*v1.Pod)
	}
	if pod == nil {
		return
	}

	gangFullName := GetGangFullName(pod)
	if gangFullName == "" {
		return
	}
	gang := gangCache.GetGang(gangFullName)
	if gang == nil {
		return
	}

	shouldDeleteGang := gang.deletePod(pod)
	if shouldDeleteGang {
		gangCache.DeleteGang(gangFullName)
		// delete podGroup
		err := retry.OnError(
			retry.DefaultRetry,
			errors.IsTooManyRequests,
			func() error {
				err := gangCache.pgClient.SchedulingV1alpha1().PodGroups(pod.Namespace).Delete(context.TODO(), GetGangName(pod), metav1.DeleteOptions{})
				return err
			})
		if err != nil {
			klog.Errorf("Delete podGroup by gang's deletion error, gang: %v, error: %v", gangFullName, err)
		} else {
			klog.Infof("Delete podGroup by gang's deletion , gang: %v", gangFullName)
		}
	}
}

func (gangCache *GangCache) onPodGroupAdd(obj interface{}) {
	pg, ok := obj.(*v1alpha1.PodGroup)
	if !ok {
		return
	}

	if pg.Annotations[PodGroupFromPodAnnotation] == "true" {
		return
	}

	gangFullName := GetNamespacedName(pg)
	gang := gangCache.getOrInitGang(gangFullName, pg)
	if gang.Spec.ParseError != nil {
		klog.Errorf("Failed to initGangSpec for PodGroup %s, err: %v", gangFullName, gang.Spec.ParseError)
	}
}

func (gangCache *GangCache) onPodGroupDelete(obj interface{}) {
	var pg *v1alpha1.PodGroup
	switch t := obj.(type) {
	case *v1alpha1.PodGroup:
		pg = t
	case cache.DeletedFinalStateUnknown:
		pg, _ = t.Obj.(*v1alpha1.PodGroup)
	}
	if pg == nil {
		return
	}

	if pg.Annotations[PodGroupFromPodAnnotation] == "true" {
		return
	}

	gangFullName := GetNamespacedName(pg)
	gangCache.DeleteGang(gangFullName)
}

func generateNewPodGroup(gangSpec *GangSpec, pod *v1.Pod) *v1alpha1.PodGroup {
	gangName := GetGangName(pod)
	pg := &v1alpha1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gangName,
			Namespace: pod.Namespace,
			Annotations: map[string]string{
				PodGroupFromPodAnnotation: "true",
			},
		},
		Spec: v1alpha1.PodGroupSpec{
			ScheduleTimeoutSeconds: pointer.Int32(int32(gangSpec.WaitTime / time.Second)),
			MinMember:              int32(gangSpec.MinMember),
		},
		Status: v1alpha1.PodGroupStatus{
			ScheduleStartTime: metav1.Now(),
		},
	}
	return pg
}
