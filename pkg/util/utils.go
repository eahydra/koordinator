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

package util

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	jsonpatch "github.com/evanphx/json-patch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	koordinatorclientset "github.com/koordinator-sh/koordinator/pkg/client/clientset/versioned"
)

// MergeCfg returns a merged interface. Value in new will
// override old's when both fields exist.
// It will throw an error if:
//  1. either of the inputs was nil;
//  2. inputs were not a pointer of the same json struct.
func MergeCfg(old, new interface{}) (interface{}, error) {
	if old == nil || new == nil {
		return nil, fmt.Errorf("invalid input, should not be empty")
	}

	if reflect.TypeOf(old).Kind() != reflect.Ptr || reflect.TypeOf(new).Kind() != reflect.Ptr {
		return nil, fmt.Errorf("invalid input, all types must be pointers to structs")
	}
	if reflect.TypeOf(old) != reflect.TypeOf(new) {
		return nil, fmt.Errorf("invalid input, should be the same type")
	}

	if data, err := json.Marshal(new); err != nil {
		return nil, err
	} else if err := json.Unmarshal(data, &old); err != nil {
		return nil, err
	}

	return old, nil
}

func MinInt64(i, j int64) int64 {
	if i < j {
		return i
	}
	return j
}

func MaxInt64(i, j int64) int64 {
	if i > j {
		return i
	}
	return j
}

func RetryOnConflictOrTooManyRequests(fn func() error) error {
	return retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return errors.IsConflict(err) || errors.IsTooManyRequests(err)
	}, fn)
}

func GeneratePodPatch(oldPod, newPod *corev1.Pod) ([]byte, error) {
	oldData, err := json.Marshal(oldPod)
	if err != nil {
		return nil, err
	}

	newData, err := json.Marshal(newPod)
	if err != nil {
		return nil, err
	}
	return strategicpatch.CreateTwoWayMergePatch(oldData, newData, &corev1.Pod{})
}

func PatchPod(ctx context.Context, clientset clientset.Interface, oldPod, newPod *corev1.Pod) (*corev1.Pod, error) {
	// generate patch bytes for the update
	patchBytes, err := GeneratePodPatch(oldPod, newPod)
	if err != nil {
		klog.V(5).InfoS("failed to generate pod patch", "pod", klog.KObj(oldPod), "err", err)
		return nil, err
	}
	if string(patchBytes) == "{}" { // nothing to patch
		return oldPod, nil
	}

	// patch with pod client
	patched, err := clientset.CoreV1().Pods(oldPod.Namespace).
		Patch(ctx, oldPod.Name, apimachinerytypes.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		klog.V(5).InfoS("failed to patch pod", "pod", klog.KObj(oldPod), "patch", string(patchBytes), "err", err)
		return nil, err
	}
	klog.V(6).InfoS("successfully patch pod", "pod", klog.KObj(oldPod), "patch", string(patchBytes))
	return patched, nil
}

func GenerateReservationPatch(oldReservation, newReservation *schedulingv1alpha1.Reservation) ([]byte, error) {
	oldData, err := json.Marshal(oldReservation)
	if err != nil {
		return nil, err
	}

	newData, err := json.Marshal(newReservation)
	if err != nil {
		return nil, err
	}
	return jsonpatch.CreateMergePatch(oldData, newData)
}

func PatchReservation(ctx context.Context, clientset koordinatorclientset.Interface, oldReservation, newReservation *schedulingv1alpha1.Reservation) (*schedulingv1alpha1.Reservation, error) {
	patchBytes, err := GenerateReservationPatch(oldReservation, newReservation)
	if err != nil {
		klog.V(5).InfoS("failed to generate reservation patch", "reservation", klog.KObj(oldReservation), "err", err)
		return nil, err
	}
	if string(patchBytes) == "{}" { // nothing to patch
		return oldReservation, nil
	}

	// NOTE: CRDs do not support strategy merge patch, so here falls back to merge patch.
	// link: https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/#advanced-features-and-flexibility
	patched, err := clientset.SchedulingV1alpha1().Reservations().
		Patch(ctx, oldReservation.Name, apimachinerytypes.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		klog.V(5).InfoS("failed to patch pod", "pod", klog.KObj(oldReservation), "patch", string(patchBytes), "err", err)
		return nil, err
	}
	klog.V(6).InfoS("successfully patch pod", "pod", klog.KObj(oldReservation), "patch", string(patchBytes))
	return patched, nil
}

// Patch is for simply patching arbitrary objects (e.g. pods, koord CRDs).
type Patch struct {
	Clientset      clientset.Interface
	KoordClientset koordinatorclientset.Interface
}

func NewPatch() *Patch {
	return &Patch{}
}

type ClientSetHandle interface {
	ClientSet() clientset.Interface
}

type KoordClientSetHandle interface {
	KoordinatorClientSet() koordinatorclientset.Interface
}

func (p *Patch) WithHandle(handle ClientSetHandle) *Patch {
	p.Clientset = handle.ClientSet()
	// set KoordClientset if ExtendedHandle implemented
	extendedHandle, ok := handle.(KoordClientSetHandle)
	if ok {
		p.KoordClientset = extendedHandle.KoordinatorClientSet()
	}
	return p
}

func (p *Patch) WithClientset(cs clientset.Interface) *Patch {
	p.Clientset = cs
	return p
}

func (p *Patch) WithKoordinatorClientSet(cs koordinatorclientset.Interface) *Patch {
	p.KoordClientset = cs
	return p
}

func (p *Patch) PatchPod(ctx context.Context, oldPod, newPod *corev1.Pod) (*corev1.Pod, error) {
	if p.Clientset == nil || reflect.ValueOf(p.Clientset).IsNil() {
		return nil, fmt.Errorf("missing clientset for pod")
	}

	return PatchPod(ctx, p.Clientset, oldPod, newPod)
}

func (p *Patch) PatchReservation(ctx context.Context, originalReservation, newReservation *schedulingv1alpha1.Reservation) (*schedulingv1alpha1.Reservation, error) {
	if p.KoordClientset == nil || reflect.ValueOf(p.KoordClientset).IsNil() {
		return nil, fmt.Errorf("missing clientset for reservation")
	}

	return PatchReservation(ctx, p.KoordClientset, originalReservation, newReservation)
}

// Patch patches the obj (if the obj is not a reserve pod) or corresponding reservation object (if the
// obj is a reserve pod) with the given patch data.
func (p *Patch) Patch(ctx context.Context, originalObj, newObj metav1.Object) (metav1.Object, error) {
	switch t := originalObj.(type) {
	case *corev1.Pod:
		newPod, _ := newObj.(*corev1.Pod)
		if newPod == nil {
			return nil, fmt.Errorf("the type of newObj is not the expected Pod type")
		}
		return p.PatchPod(ctx, t, newPod)
	case *schedulingv1alpha1.Reservation:
		newReservation, _ := newObj.(*schedulingv1alpha1.Reservation)
		if newReservation == nil {
			return nil, fmt.Errorf("the type of newObj is not the expected Reservation type")
		}
		return p.PatchReservation(ctx, t, newReservation)
	}
	return nil, fmt.Errorf("unsupported Object")
}

func GetNamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
