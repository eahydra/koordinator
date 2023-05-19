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

package extension

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"

	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
)

const (
	// LabelPodOperatingMode describes the mode of operation for Pod.
	LabelPodOperatingMode = SchedulingDomainPrefix + "/operating-mode"

	// LabelReservationOrder controls the preference logic for Reservation.
	// Reservation with lower order is preferred to be selected before Reservation with higher order.
	// But if it is 0, Reservation will be selected according to the capacity score.
	LabelReservationOrder = SchedulingDomainPrefix + "/reservation-order"

	// AnnotationReservationAllocated represents the reservation allocated by the pod.
	AnnotationReservationAllocated = SchedulingDomainPrefix + "/reservation-allocated"

	// AnnotationReservationAffinity represents the constraints of Pod selection Reservation
	AnnotationReservationAffinity = SchedulingDomainPrefix + "/reservation-affinity"

	// AnnotationReservationOwners indicates the owner specification which can allocate reserved resources
	AnnotationReservationOwners = SchedulingDomainPrefix + "/reservation-owners"

	// AnnotationReservationCurrentOwner indicates current resource owners which allocated the reservation resources.
	AnnotationReservationCurrentOwner = SchedulingDomainPrefix + "/reservation-current-owner"
)

// Drawing on the design proposal being discussed in the community
// Doc: https://docs.google.com/document/d/1sbFUA_9qWtorJkcukNULr12FKX6lMvISiINxAURHNFo/edit#heading=h.xgjl2srtytjt

type PodOperatingMode string

const (
	// RunnablePodOperatingMode represents the original pod behavior, it is the default mode where the
	// pod’s containers are executed by Kubelet when the pod is assigned a node.
	RunnablePodOperatingMode PodOperatingMode = "Runnable"

	// ReservationPodOperatingMode means the pod represents a scheduling and resource reservation unit
	ReservationPodOperatingMode PodOperatingMode = "Reservation"
)

type ReservationAllocated struct {
	Name string    `json:"name,omitempty"`
	UID  types.UID `json:"uid,omitempty"`
}

// ReservationAffinity represents the constraints of Pod selection Reservation
type ReservationAffinity struct {
	// If the affinity requirements specified by this field are not met at
	// scheduling time, the pod will not be scheduled onto the node.
	// If the affinity requirements specified by this field cease to be met
	// at some point during pod execution (e.g. due to an update), the system
	// may or may not try to eventually evict the pod from its node.
	RequiredDuringSchedulingIgnoredDuringExecution *ReservationAffinitySelector `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty"`
	// ReservationSelector is a selector which must be true for the pod to fit on a reservation.
	// Selector which must match a reservation's labels for the pod to be scheduled on that node.
	ReservationSelector map[string]string `json:"reservationSelector,omitempty"`
}

// ReservationAffinitySelector represents the union of the results of one or more label queries
// over a set of reservations; that is, it represents the OR of the selectors represented
// by the reservation selector terms.
type ReservationAffinitySelector struct {
	// Required. A list of reservation selector terms. The terms are ORed.
	// Reuse corev1.NodeSelectorTerm to avoid defining too many repeated definitions.
	ReservationSelectorTerms []corev1.NodeSelectorTerm `json:"reservationSelectorTerms,omitempty"`
}

func GetReservationAllocated(pod *corev1.Pod) (*ReservationAllocated, error) {
	if pod.Annotations == nil {
		return nil, nil
	}
	data, ok := pod.Annotations[AnnotationReservationAllocated]
	if !ok {
		return nil, nil
	}
	reservationAllocated := &ReservationAllocated{}
	err := json.Unmarshal([]byte(data), reservationAllocated)
	if err != nil {
		return nil, err
	}
	return reservationAllocated, nil
}

func SetReservationAllocated(pod *corev1.Pod, r metav1.Object) {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	reservationAllocated := &ReservationAllocated{
		Name: r.GetName(),
		UID:  r.GetUID(),
	}
	data, _ := json.Marshal(reservationAllocated) // assert no error
	pod.Annotations[AnnotationReservationAllocated] = string(data)
}

func IsReservationAllocateOnce(r *schedulingv1alpha1.Reservation) bool {
	return pointer.BoolDeref(r.Spec.AllocateOnce, true)
}

func GetReservationAffinity(annotations map[string]string) (*ReservationAffinity, error) {
	var affinity ReservationAffinity
	if s := annotations[AnnotationReservationAffinity]; s != "" {
		if err := json.Unmarshal([]byte(s), &affinity); err != nil {
			return nil, err
		}
	}
	return &affinity, nil
}

func IsReservationOperatingMode(pod *corev1.Pod) bool {
	return pod.Labels[LabelPodOperatingMode] == string(ReservationPodOperatingMode)
}

func GetReservationOwners(annotations map[string]string) ([]schedulingv1alpha1.ReservationOwner, error) {
	var owners []schedulingv1alpha1.ReservationOwner
	if s := annotations[AnnotationReservationOwners]; s != "" {
		err := json.Unmarshal([]byte(s), &owners)
		if err != nil {
			return nil, err
		}
	}
	return owners, nil
}

func GetReservationCurrentOwner(annotations map[string]string) (*corev1.ObjectReference, error) {
	var owner corev1.ObjectReference
	s := annotations[AnnotationReservationCurrentOwner]
	if s == "" {
		return nil, nil
	}
	err := json.Unmarshal([]byte(s), &owner)
	if err != nil {
		return nil, err
	}
	return &owner, nil
}

func SetReservationCurrentOwner(annotations map[string]string, owner *corev1.ObjectReference) error {
	if owner == nil {
		return nil
	}
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	annotations[AnnotationReservationCurrentOwner] = string(data)
	return nil
}

func RemoveReservationCurrentOwner(annotations map[string]string) {
	delete(annotations, AnnotationReservationCurrentOwner)
}
