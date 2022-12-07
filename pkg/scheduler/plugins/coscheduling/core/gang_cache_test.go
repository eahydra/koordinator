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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"
	fakepgclientset "sigs.k8s.io/scheduler-plugins/pkg/generated/clientset/versioned/fake"
	pgformers "sigs.k8s.io/scheduler-plugins/pkg/generated/informers/externalversions"

	"github.com/koordinator-sh/koordinator/apis/extension"
)

var fakeTimeNowFn = func() time.Time {
	t := time.Time{}
	t.Add(100 * time.Second)
	return t
}

func TestGangCache_OnPodAdd(t *testing.T) {
	tests := []struct {
		name               string
		pods               []*corev1.Pod
		podGroups          []*v1alpha1.PodGroup
		wantCache          map[string]*Gang
		wantPodGroupMap    map[string]*v1alpha1.PodGroup
		wantGetPodGroupErr bool
		wantParseErr       bool
	}{
		{
			name:      "add invalid pod",
			pods:      []*corev1.Pod{{}},
			wantCache: map[string]*Gang{},
		},
		{
			name: "add invalid pod2",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Labels:      map[string]string{"test": "gang"},
						Annotations: map[string]string{"test": "gang"},
					},
				},
			},
			wantCache: map[string]*Gang{},
		},
		{
			name: "add pod announcing Gang in CRD way before CRD created,gang should be created but not initialized",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "crdPod",
						Namespace: "default",
						Labels:    map[string]string{v1alpha1.PodGroupLabel: "test"},
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/test": {
					Name:               "default/test",
					CreationTimestamp:  fakeTimeNowFn(),
					scheduleCycleValid: true,
					scheduleCycle:      1,
					pods: map[string]*corev1.Pod{
						"default/crdPod": {
							ObjectMeta: metav1.ObjectMeta{
								Name:      "crdPod",
								Namespace: "default",
								Labels:    map[string]string{v1alpha1.PodGroupLabel: "test"},
							},
						},
					},
					waitingPods:       map[string]*corev1.Pod{},
					boundPods:         map[string]*corev1.Pod{},
					podScheduleCycles: map[string]int{},
				},
			},
		},
		{
			name: "add pod announcing Gang in Annotation way",
			pods: []*corev1.Pod{
				// pod1 announce GangA
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod1",
						Annotations: map[string]string{
							extension.AnnotationGangName:     "ganga",
							extension.AnnotationGangMinNum:   "2",
							extension.AnnotationGangWaitTime: "30s",
							extension.AnnotationGangMode:     extension.GangModeNonStrict,
							extension.AnnotationGangGroups:   "[\"default/ganga\",\"default/gangb\"]",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "nba",
					},
				},
				// pod2 also announce GangA but with different annotations after pod1's announcing
				// so gangA in cache should only be created with pod1's Annotations
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod2",
						Annotations: map[string]string{
							extension.AnnotationGangName:     "ganga",
							extension.AnnotationGangMinNum:   "7",
							extension.AnnotationGangWaitTime: "3000s",
							extension.AnnotationGangGroups:   "[\"default/gangc\",\"default/gangd\"]",
						},
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/ganga": {
					Name: "default/ganga",
					Spec: GangSpec{
						Mode:        extension.GangModeNonStrict,
						MinMember:   2,
						TotalMember: 2,
						Groups:      []string{"default/ganga", "default/gangb"},
						WaitTime:    30 * time.Second,
						GangFrom:    GangFromPodAnnotation,
					},
					SpecInitialized:   true,
					CreationTimestamp: fakeTimeNowFn(),
					pods: map[string]*corev1.Pod{
						"default/pod1": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod1",
								Annotations: map[string]string{
									extension.AnnotationGangName:     "ganga",
									extension.AnnotationGangMinNum:   "2",
									extension.AnnotationGangWaitTime: "30s",
									extension.AnnotationGangMode:     extension.GangModeNonStrict,
									extension.AnnotationGangGroups:   "[\"default/ganga\",\"default/gangb\"]",
								},
							},
							Spec: corev1.PodSpec{
								NodeName: "nba",
							},
						},
						"default/pod2": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod2",
								Annotations: map[string]string{
									extension.AnnotationGangName:     "ganga",
									extension.AnnotationGangMinNum:   "7",
									extension.AnnotationGangWaitTime: "3000s",
									extension.AnnotationGangGroups:   "[\"default/gangc\",\"default/gangd\"]",
								},
							},
						},
					},
					waitingPods: map[string]*corev1.Pod{},
					boundPods: map[string]*corev1.Pod{
						"default/pod1": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod1",
								Annotations: map[string]string{
									extension.AnnotationGangName:     "ganga",
									extension.AnnotationGangMinNum:   "2",
									extension.AnnotationGangWaitTime: "30s",
									extension.AnnotationGangMode:     extension.GangModeNonStrict,
									extension.AnnotationGangGroups:   "[\"default/ganga\",\"default/gangb\"]",
								},
							},
							Spec: corev1.PodSpec{
								NodeName: "nba",
							},
						},
					},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					resourceSatisfied:  true,
					podScheduleCycles:  map[string]int{},
				},
			},
			podGroups: []*v1alpha1.PodGroup{
				// default/gangA pg has already existed, so pod1's pg will not create
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ganga",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: fakeTimeNowFn()},
						Annotations: map[string]string{
							PodGroupFromPodAnnotation: "true",
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						ScheduleTimeoutSeconds: pointer.Int32(30),
						MinMember:              int32(10),
					},
				},
			},
			wantPodGroupMap: map[string]*v1alpha1.PodGroup{
				"ganga": {
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ganga",
						Namespace:         "default",
						CreationTimestamp: metav1.Time{Time: fakeTimeNowFn()},
						Annotations: map[string]string{
							PodGroupFromPodAnnotation: "true",
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						ScheduleTimeoutSeconds: pointer.Int32(30),
						MinMember:              int32(10),
					},
				},
			},
		},
		{
			name: "add pods announcing Gang in Annotation way,but with illegal args A",
			pods: []*corev1.Pod{
				// pod3 announce GangB with illegal minNum,
				// so that gangA's info depends on the next pod's Annotations
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod3",
						Annotations: map[string]string{
							extension.AnnotationGangName:   "gangb",
							extension.AnnotationGangMinNum: "xxx",
						},
					},
				},
				// pod4 also announce GangA but with legal minNum,illegal remaining args
				// so gangA in cache should only be created with pod4's Annotations(illegal args set by default)
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod4",
						Annotations: map[string]string{
							extension.AnnotationGangName:     "gangb",
							extension.AnnotationGangMinNum:   "2",
							extension.AnnotationGangTotalNum: "1",
							extension.AnnotationGangMode:     "UnsupportedMode",
							extension.AnnotationGangWaitTime: "InvalidWaitTime",
							extension.AnnotationGangGroups:   "ganga,gangx",
						},
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/gangb": {
					Name:              "default/gangb",
					CreationTimestamp: fakeTimeNowFn(),
					Spec: GangSpec{
						Mode:        extension.GangModeStrict,
						MinMember:   0,
						TotalMember: 0,
						Groups:      []string{"default/gangb"},
						WaitTime:    0,
						ParseError:  nil,
						GangFrom:    GangFromPodAnnotation,
					},
					SpecInitialized: true,
					pods: map[string]*corev1.Pod{
						"default/pod3": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod3",
								Annotations: map[string]string{
									extension.AnnotationGangName:   "gangb",
									extension.AnnotationGangMinNum: "xxx",
								},
							},
						},
						"default/pod4": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod4",
								Annotations: map[string]string{
									extension.AnnotationGangName:     "gangb",
									extension.AnnotationGangMinNum:   "2",
									extension.AnnotationGangTotalNum: "1",
									extension.AnnotationGangMode:     "UnsupportedMode",
									extension.AnnotationGangWaitTime: "InvalidWaitTime",
									extension.AnnotationGangGroups:   "ganga,gangx",
								},
							},
						},
					},
					waitingPods:        map[string]*corev1.Pod{},
					boundPods:          map[string]*corev1.Pod{},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					podScheduleCycles:  map[string]int{},
				},
			},
			wantPodGroupMap:    nil,
			wantGetPodGroupErr: true,
			wantParseErr:       true,
		},
		{
			name: "add pods announcing Gang in Annotation way,but with illegal args",
			pods: []*corev1.Pod{
				// pod1 announce GangA with illegal AnnotationGangWaitTime,
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod5",
						Annotations: map[string]string{
							extension.AnnotationGangName:     "gangc",
							extension.AnnotationGangMinNum:   "0",
							extension.AnnotationGangWaitTime: "0",
							extension.AnnotationGangGroups:   "[a,b]",
						},
					},
				},
				// pod2 announce GangB with illegal AnnotationGangWaitTime,
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod6",
						Annotations: map[string]string{
							extension.AnnotationGangName:     "gangd",
							extension.AnnotationGangMinNum:   "0",
							extension.AnnotationGangWaitTime: "-20s",
							extension.AnnotationGangGroups:   "[a,b]",
						},
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/gangc": {
					Name:              "default/gangc",
					CreationTimestamp: fakeTimeNowFn(),
					Spec: GangSpec{
						Mode:     extension.GangModeStrict,
						GangFrom: GangFromPodAnnotation,
						Groups:   []string{"default/gangc"},
					},
					SpecInitialized: true,
					pods: map[string]*corev1.Pod{
						"default/pod5": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod5",
								Annotations: map[string]string{
									extension.AnnotationGangName:     "gangc",
									extension.AnnotationGangMinNum:   "0",
									extension.AnnotationGangWaitTime: "0",
									extension.AnnotationGangGroups:   "[a,b]",
								},
							},
						},
					},
					waitingPods:        map[string]*corev1.Pod{},
					boundPods:          map[string]*corev1.Pod{},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					podScheduleCycles:  map[string]int{},
				},
				"default/gangd": {
					Name:              "default/gangd",
					CreationTimestamp: fakeTimeNowFn(),
					Spec: GangSpec{
						Mode:     extension.GangModeStrict,
						GangFrom: GangFromPodAnnotation,
						Groups:   []string{"default/gangd"},
					},
					SpecInitialized: true,
					pods: map[string]*corev1.Pod{
						"default/pod6": {
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "pod6",
								Annotations: map[string]string{
									extension.AnnotationGangName:     "gangd",
									extension.AnnotationGangMinNum:   "0",
									extension.AnnotationGangWaitTime: "-20s",
									extension.AnnotationGangGroups:   "[a,b]",
								},
							},
						},
					},
					waitingPods:        map[string]*corev1.Pod{},
					boundPods:          map[string]*corev1.Pod{},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					podScheduleCycles:  map[string]int{},
				},
			},
			wantPodGroupMap:    nil,
			wantGetPodGroupErr: true,
			wantParseErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgClientSet := fakepgclientset.NewSimpleClientset()
			pgInformerFactory := pgformers.NewSharedInformerFactory(pgClientSet, 0)
			pgInformer := pgInformerFactory.Scheduling().V1alpha1().PodGroups()
			pglister := pgInformer.Lister()
			gangCache := NewGangCache(0, nil, pglister, pgClientSet)
			for _, pg := range tt.podGroups {
				err := retry.OnError(
					retry.DefaultRetry,
					errors.IsTooManyRequests,
					func() error {
						var err error
						pg, err = pgClientSet.SchedulingV1alpha1().PodGroups("default").Create(context.TODO(), pg, metav1.CreateOptions{})
						return err
					})
				if err != nil {
					t.Errorf("retry pgClient create PodGroup err: %v", err)
				}
			}
			for _, pod := range tt.pods {
				gangCache.onPodAdd(pod)
			}

			var parseErrs []error
			for _, v := range gangCache.gangItems {
				if v.SpecInitialized && v.Spec.ParseError != nil {
					parseErrs = append(parseErrs, v.Spec.ParseError)
					v.Spec.ParseError = nil
				}
			}
			if (parseErrs != nil) != tt.wantParseErr {
				t.Errorf("expect wantParseErr=%v but got %v", tt.wantParseErr, utilerrors.NewAggregate(parseErrs))
			}
			assert.Equal(t, tt.wantCache, gangCache.gangItems)

			for pgKey, targetPg := range tt.wantPodGroupMap {
				var pg *v1alpha1.PodGroup
				err := retry.OnError(
					retry.DefaultRetry,
					errors.IsTooManyRequests,
					func() error {
						var err error
						pg, err = pgClientSet.SchedulingV1alpha1().PodGroups("default").Get(context.TODO(), pgKey, metav1.GetOptions{})
						return err
					})
				if (err != nil) != tt.wantGetPodGroupErr {
					t.Errorf("expect wantGetPodGroupErr: %v, but got %v", tt.wantGetPodGroupErr, err)
				} else {
					targetPg.Status.ScheduleStartTime = metav1.Time{}
					pg.Status.ScheduleStartTime = metav1.Time{}
					assert.Equal(t, targetPg, pg)
				}
			}
		})
	}
}

func TestGangCache_OnPodDelete(t *testing.T) {
	tests := []struct {
		name         string
		podGroups    []*v1alpha1.PodGroup
		pods         []*corev1.Pod
		wantCache    map[string]*Gang
		wantPodGroup map[string]*v1alpha1.PodGroup
	}{
		{
			name: "delete invalid pod,has no gang",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod1",
					},
				},
			},
			wantCache: map[string]*Gang{},
		},
		{
			name: "delete invalid pod2,gang has not find",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "pod2",
						Namespace:   "wenshiqi",
						Labels:      map[string]string{"test": "gang"},
						Annotations: map[string]string{"test": "gang"},
					},
				},
			},
			wantCache: map[string]*Gang{},
		},
		{
			name: "delete gangA's pods one by one,finally gangA should be deleted",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod3",
						Annotations: map[string]string{
							extension.AnnotationGangName: "gangA",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod4",
						Annotations: map[string]string{
							extension.AnnotationGangName: "gangA",
						},
					},
				},
			},
			wantCache: map[string]*Gang{},
			wantPodGroup: map[string]*v1alpha1.PodGroup{
				"gangA": nil,
			},
		},
		{
			name: "delete gangB's pods one by one,but gangB is created by CRD",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod5",
						Labels: map[string]string{
							v1alpha1.PodGroupLabel: "GangB",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "pod6",
						Labels: map[string]string{
							v1alpha1.PodGroupLabel: "GangB",
						},
					},
				},
			},
			podGroups: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "GangB",
					},
					Spec: v1alpha1.PodGroupSpec{
						MinMember:              4,
						ScheduleTimeoutSeconds: pointer.Int32(10),
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/GangB": {
					Name: "default/GangB",
					Spec: GangSpec{
						Mode:        extension.GangModeStrict,
						WaitTime:    10 * time.Second,
						MinMember:   4,
						TotalMember: 4,
						Groups:      []string{"default/GangB"},
						GangFrom:    GangFromPodGroup,
					},
					SpecInitialized:    true,
					CreationTimestamp:  fakeTimeNowFn(),
					pods:               map[string]*corev1.Pod{},
					waitingPods:        map[string]*corev1.Pod{},
					boundPods:          map[string]*corev1.Pod{},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					podScheduleCycles:  map[string]int{},
				},
			},
			wantPodGroup: map[string]*v1alpha1.PodGroup{
				"GangB": {
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "GangB",
					},
					Spec: v1alpha1.PodGroupSpec{
						MinMember:              4,
						ScheduleTimeoutSeconds: pointer.Int32(10),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgClient := fakepgclientset.NewSimpleClientset()
			pgInformerFactory := pgformers.NewSharedInformerFactory(pgClient, 0)
			pgInformer := pgInformerFactory.Scheduling().V1alpha1().PodGroups()
			pglister := pgInformer.Lister()
			gangCache := NewGangCache(0, nil, pglister, pgClient)
			for _, pg := range tt.podGroups {
				err := retry.OnError(
					retry.DefaultRetry,
					errors.IsTooManyRequests,
					func() error {
						var err error
						pg, err = pgClient.SchedulingV1alpha1().PodGroups("default").Create(context.TODO(), pg, metav1.CreateOptions{})
						return err
					})
				if err != nil {
					t.Errorf("retry pgClient create PodGroup err: %v", err)
				}
				gangCache.onPodGroupAdd(pg)
			}
			for _, pod := range tt.pods {
				gangCache.onPodAdd(pod)
			}

			// start deleting pods
			for _, pod := range tt.pods {
				gangCache.onPodDelete(pod)
			}
			assert.Equal(t, tt.wantCache, gangCache.gangItems)

			for pgKey, pgT := range tt.wantPodGroup {
				var pg *v1alpha1.PodGroup
				err := retry.OnError(
					retry.DefaultRetry,
					errors.IsTooManyRequests,
					func() error {
						var err error
						pg, err = pgClient.SchedulingV1alpha1().PodGroups("default").Get(context.TODO(), pgKey, metav1.GetOptions{})
						return err
					})
				// pgT ==nil, we can not get the pg from the cluster,error should be nil
				if pgT == nil {
					if err == nil {
						t.Error()
					}
				} else {
					if err != nil {
						t.Errorf("retry pgClient Get PodGroup err: %v", err)
					} else {
						assert.Equal(t, pgT, pg)
					}
				}
			}
		})
	}
}

func TestGangCache_OnPodGroupAdd(t *testing.T) {
	waitTime := int32(300)
	tests := []struct {
		name         string
		pgs          []*v1alpha1.PodGroup
		wantCache    map[string]*Gang
		wantParseErr bool
	}{
		{
			name: "update podGroup with annotations that created from annotations",
			pgs: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      " gangb",
						Annotations: map[string]string{
							PodGroupFromPodAnnotation: "true",
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						MinMember:              2,
						ScheduleTimeoutSeconds: &waitTime,
					},
				},
			},
			wantCache: map[string]*Gang{},
		},
		{
			name: "update podGroup with annotations",
			pgs: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "gangA",
						Annotations: map[string]string{
							extension.AnnotationGangMode:   extension.GangModeNonStrict,
							extension.AnnotationGangGroups: "[\"default/gangA\",\"default/gangB\"]",
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						MinMember:              2,
						ScheduleTimeoutSeconds: &waitTime,
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/gangA": {
					Name:              "default/gangA",
					CreationTimestamp: fakeTimeNowFn(),
					Spec: GangSpec{
						Mode:        extension.GangModeNonStrict,
						WaitTime:    300 * time.Second,
						MinMember:   2,
						TotalMember: 2,
						Groups:      []string{"default/gangA", "default/gangB"},
						GangFrom:    GangFromPodGroup,
					},
					SpecInitialized:    true,
					pods:               map[string]*corev1.Pod{},
					waitingPods:        map[string]*corev1.Pod{},
					boundPods:          map[string]*corev1.Pod{},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					podScheduleCycles:  map[string]int{},
				},
			},
		},
		{
			name: "update podGroup with illegal annotations",
			pgs: []*v1alpha1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Name:      "gangA",
						Annotations: map[string]string{
							extension.AnnotationGangMode:     "UnsupportedMode",
							extension.AnnotationGangGroups:   "a,b",
							extension.AnnotationGangTotalNum: "2",
						},
					},
					Spec: v1alpha1.PodGroupSpec{
						MinMember:              4,
						ScheduleTimeoutSeconds: &waitTime,
					},
				},
			},
			wantCache: map[string]*Gang{
				"default/gangA": {
					Name:              "default/gangA",
					CreationTimestamp: fakeTimeNowFn(),
					Spec: GangSpec{
						WaitTime:    300 * time.Second,
						Mode:        extension.GangModeStrict,
						MinMember:   4,
						TotalMember: 4,
						Groups:      []string{"default/gangA"},
						GangFrom:    GangFromPodGroup,
					},
					SpecInitialized:    true,
					pods:               map[string]*corev1.Pod{},
					waitingPods:        map[string]*corev1.Pod{},
					boundPods:          map[string]*corev1.Pod{},
					scheduleCycleValid: true,
					scheduleCycle:      1,
					podScheduleCycles:  map[string]int{},
				},
			},
			wantParseErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pgClient := fakepgclientset.NewSimpleClientset()
			gangCache := NewGangCache(0, nil, nil, pgClient)
			for _, pg := range tt.pgs {
				gangCache.onPodGroupAdd(pg)
			}
			var parseErrs []error
			for _, v := range gangCache.gangItems {
				if v.SpecInitialized && v.Spec.ParseError != nil {
					parseErrs = append(parseErrs, v.Spec.ParseError)
					v.Spec.ParseError = nil
				}
			}
			if (parseErrs != nil) != tt.wantParseErr {
				t.Errorf("expect wantParseErr=%v but got %v", tt.wantParseErr, utilerrors.NewAggregate(parseErrs))
			}
			assert.Equal(t, tt.wantCache, gangCache.gangItems)
		})
	}
}

func TestGangCache_OnGangDelete(t *testing.T) {
	pgClient := fakepgclientset.NewSimpleClientset()

	pgInformerFactory := pgformers.NewSharedInformerFactory(pgClient, 0)
	pgInformer := pgInformerFactory.Scheduling().V1alpha1().PodGroups()
	pglister := pgInformer.Lister()
	cache := NewGangCache(0, nil, pglister, pgClient)

	// case1: pg that created by crd,delete pg then will delete the gang
	podGroup := &v1alpha1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "ganga",
		},
	}
	gangFullName := fmt.Sprintf("%s/%s", "default", "ganga")
	cache.GetGang(gangFullName)
	cache.onPodGroupDelete(podGroup)
	assert.Equal(t, 0, len(cache.gangItems))

	// case2: pg that created by annotations,pg deleted will do nothing

	podToCreatePg := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod1",
			Annotations: map[string]string{
				extension.AnnotationGangName:     "gangb",
				extension.AnnotationGangMinNum:   "2",
				extension.AnnotationGangWaitTime: "30s",
				extension.AnnotationGangMode:     extension.GangModeNonStrict,
				extension.AnnotationGangGroups:   "[\"default/gangA\",\"default/gangB\"]",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "nba",
		},
	}

	cache.onPodAdd(podToCreatePg)
	var pg *v1alpha1.PodGroup
	err := retry.OnError(
		retry.DefaultRetry,
		errors.IsTooManyRequests,
		func() error {
			var err error
			pg, err = pgClient.SchedulingV1alpha1().PodGroups("default").Get(context.TODO(), "gangb", metav1.GetOptions{})
			return err
		})
	if err != nil {
		t.Errorf("pgLister get pg err: %v", err)
	}
	cache.onPodGroupDelete(pg)
	wantedGang := &Gang{
		Name: "default/gangb",
		Spec: GangSpec{
			Mode:        extension.GangModeNonStrict,
			MinMember:   2,
			TotalMember: 2,
			Groups:      []string{"default/gangA", "default/gangB"},
			WaitTime:    30 * time.Second,
			ParseError:  nil,
			GangFrom:    GangFromPodAnnotation,
		},
		SpecInitialized:   true,
		CreationTimestamp: fakeTimeNowFn(),
		pods: map[string]*corev1.Pod{
			"default/pod1": {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "pod1",
					Annotations: map[string]string{
						extension.AnnotationGangName:     "gangb",
						extension.AnnotationGangMinNum:   "2",
						extension.AnnotationGangWaitTime: "30s",
						extension.AnnotationGangMode:     extension.GangModeNonStrict,
						extension.AnnotationGangGroups:   "[\"default/gangA\",\"default/gangB\"]",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "nba",
				},
			},
		},
		waitingPods: map[string]*corev1.Pod{},
		boundPods: map[string]*corev1.Pod{
			"default/pod1": {
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "pod1",
					Annotations: map[string]string{
						extension.AnnotationGangName:     "gangb",
						extension.AnnotationGangMinNum:   "2",
						extension.AnnotationGangWaitTime: "30s",
						extension.AnnotationGangMode:     extension.GangModeNonStrict,
						extension.AnnotationGangGroups:   "[\"default/gangA\",\"default/gangB\"]",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "nba",
				},
			},
		},
		scheduleCycleValid: true,
		scheduleCycle:      1,
		resourceSatisfied:  true,
		podScheduleCycles:  map[string]int{},
	}
	cacheGang := cache.GetGang("default/gangb")
	assert.Equal(t, wantedGang, cacheGang)

}
