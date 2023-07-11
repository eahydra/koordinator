//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Device) DeepCopyInto(out *Device) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Device.
func (in *Device) DeepCopy() *Device {
	if in == nil {
		return nil
	}
	out := new(Device)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Device) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceAllocation) DeepCopyInto(out *DeviceAllocation) {
	*out = *in
	if in.Entries != nil {
		in, out := &in.Entries, &out.Entries
		*out = make([]DeviceAllocationItem, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceAllocation.
func (in *DeviceAllocation) DeepCopy() *DeviceAllocation {
	if in == nil {
		return nil
	}
	out := new(DeviceAllocation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceAllocationItem) DeepCopyInto(out *DeviceAllocationItem) {
	*out = *in
	if in.Minors != nil {
		in, out := &in.Minors, &out.Minors
		*out = make([]int32, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceAllocationItem.
func (in *DeviceAllocationItem) DeepCopy() *DeviceAllocationItem {
	if in == nil {
		return nil
	}
	out := new(DeviceAllocationItem)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceInfo) DeepCopyInto(out *DeviceInfo) {
	*out = *in
	if in.Minor != nil {
		in, out := &in.Minor, &out.Minor
		*out = new(int32)
		**out = **in
	}
	if in.ModuleID != nil {
		in, out := &in.ModuleID, &out.ModuleID
		*out = new(int32)
		**out = **in
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceInfo.
func (in *DeviceInfo) DeepCopy() *DeviceInfo {
	if in == nil {
		return nil
	}
	out := new(DeviceInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceList) DeepCopyInto(out *DeviceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Device, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceList.
func (in *DeviceList) DeepCopy() *DeviceList {
	if in == nil {
		return nil
	}
	out := new(DeviceList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DeviceList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceSpec) DeepCopyInto(out *DeviceSpec) {
	*out = *in
	if in.Devices != nil {
		in, out := &in.Devices, &out.Devices
		*out = make([]DeviceInfo, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceSpec.
func (in *DeviceSpec) DeepCopy() *DeviceSpec {
	if in == nil {
		return nil
	}
	out := new(DeviceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DeviceStatus) DeepCopyInto(out *DeviceStatus) {
	*out = *in
	if in.Allocations != nil {
		in, out := &in.Allocations, &out.Allocations
		*out = make([]DeviceAllocation, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DeviceStatus.
func (in *DeviceStatus) DeepCopy() *DeviceStatus {
	if in == nil {
		return nil
	}
	out := new(DeviceStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrateReservationOptions) DeepCopyInto(out *PodMigrateReservationOptions) {
	*out = *in
	if in.ReservationRef != nil {
		in, out := &in.ReservationRef, &out.ReservationRef
		*out = new(v1.ObjectReference)
		**out = **in
	}
	if in.Template != nil {
		in, out := &in.Template, &out.Template
		*out = new(ReservationTemplateSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.PreemptionOptions != nil {
		in, out := &in.PreemptionOptions, &out.PreemptionOptions
		*out = new(PodMigrationJobPreemptionOptions)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrateReservationOptions.
func (in *PodMigrateReservationOptions) DeepCopy() *PodMigrateReservationOptions {
	if in == nil {
		return nil
	}
	out := new(PodMigrateReservationOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJob) DeepCopyInto(out *PodMigrationJob) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJob.
func (in *PodMigrationJob) DeepCopy() *PodMigrationJob {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PodMigrationJob) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJobCondition) DeepCopyInto(out *PodMigrationJobCondition) {
	*out = *in
	in.LastProbeTime.DeepCopyInto(&out.LastProbeTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJobCondition.
func (in *PodMigrationJobCondition) DeepCopy() *PodMigrationJobCondition {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJobCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJobList) DeepCopyInto(out *PodMigrationJobList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PodMigrationJob, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJobList.
func (in *PodMigrationJobList) DeepCopy() *PodMigrationJobList {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJobList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PodMigrationJobList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJobPreemptedReservation) DeepCopyInto(out *PodMigrationJobPreemptedReservation) {
	*out = *in
	if in.PreemptedPodRef != nil {
		in, out := &in.PreemptedPodRef, &out.PreemptedPodRef
		*out = new(v1.ObjectReference)
		**out = **in
	}
	if in.PodsRef != nil {
		in, out := &in.PodsRef, &out.PodsRef
		*out = make([]v1.ObjectReference, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJobPreemptedReservation.
func (in *PodMigrationJobPreemptedReservation) DeepCopy() *PodMigrationJobPreemptedReservation {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJobPreemptedReservation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJobPreemptionOptions) DeepCopyInto(out *PodMigrationJobPreemptionOptions) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJobPreemptionOptions.
func (in *PodMigrationJobPreemptionOptions) DeepCopy() *PodMigrationJobPreemptionOptions {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJobPreemptionOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJobSpec) DeepCopyInto(out *PodMigrationJobSpec) {
	*out = *in
	if in.TTL != nil {
		in, out := &in.TTL, &out.TTL
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.PodRef != nil {
		in, out := &in.PodRef, &out.PodRef
		*out = new(v1.ObjectReference)
		**out = **in
	}
	if in.ReservationOptions != nil {
		in, out := &in.ReservationOptions, &out.ReservationOptions
		*out = new(PodMigrateReservationOptions)
		(*in).DeepCopyInto(*out)
	}
	if in.DeleteOptions != nil {
		in, out := &in.DeleteOptions, &out.DeleteOptions
		*out = new(metav1.DeleteOptions)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJobSpec.
func (in *PodMigrationJobSpec) DeepCopy() *PodMigrationJobSpec {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJobSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodMigrationJobStatus) DeepCopyInto(out *PodMigrationJobStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]PodMigrationJobCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.PodRef != nil {
		in, out := &in.PodRef, &out.PodRef
		*out = new(v1.ObjectReference)
		**out = **in
	}
	if in.PreemptedPodsRef != nil {
		in, out := &in.PreemptedPodsRef, &out.PreemptedPodsRef
		*out = make([]v1.ObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.PreemptedPodsReservations != nil {
		in, out := &in.PreemptedPodsReservations, &out.PreemptedPodsReservations
		*out = make([]PodMigrationJobPreemptedReservation, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodMigrationJobStatus.
func (in *PodMigrationJobStatus) DeepCopy() *PodMigrationJobStatus {
	if in == nil {
		return nil
	}
	out := new(PodMigrationJobStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Reservation) DeepCopyInto(out *Reservation) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Reservation.
func (in *Reservation) DeepCopy() *Reservation {
	if in == nil {
		return nil
	}
	out := new(Reservation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Reservation) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationCondition) DeepCopyInto(out *ReservationCondition) {
	*out = *in
	in.LastProbeTime.DeepCopyInto(&out.LastProbeTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationCondition.
func (in *ReservationCondition) DeepCopy() *ReservationCondition {
	if in == nil {
		return nil
	}
	out := new(ReservationCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationControllerReference) DeepCopyInto(out *ReservationControllerReference) {
	*out = *in
	in.OwnerReference.DeepCopyInto(&out.OwnerReference)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationControllerReference.
func (in *ReservationControllerReference) DeepCopy() *ReservationControllerReference {
	if in == nil {
		return nil
	}
	out := new(ReservationControllerReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationList) DeepCopyInto(out *ReservationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Reservation, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationList.
func (in *ReservationList) DeepCopy() *ReservationList {
	if in == nil {
		return nil
	}
	out := new(ReservationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ReservationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationOwner) DeepCopyInto(out *ReservationOwner) {
	*out = *in
	if in.Object != nil {
		in, out := &in.Object, &out.Object
		*out = new(v1.ObjectReference)
		**out = **in
	}
	if in.Controller != nil {
		in, out := &in.Controller, &out.Controller
		*out = new(ReservationControllerReference)
		(*in).DeepCopyInto(*out)
	}
	if in.LabelSelector != nil {
		in, out := &in.LabelSelector, &out.LabelSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationOwner.
func (in *ReservationOwner) DeepCopy() *ReservationOwner {
	if in == nil {
		return nil
	}
	out := new(ReservationOwner)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationSpec) DeepCopyInto(out *ReservationSpec) {
	*out = *in
	if in.Template != nil {
		in, out := &in.Template, &out.Template
		*out = new(v1.PodTemplateSpec)
		(*in).DeepCopyInto(*out)
	}
	if in.Owners != nil {
		in, out := &in.Owners, &out.Owners
		*out = make([]ReservationOwner, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TTL != nil {
		in, out := &in.TTL, &out.TTL
		*out = new(metav1.Duration)
		**out = **in
	}
	if in.Expires != nil {
		in, out := &in.Expires, &out.Expires
		*out = (*in).DeepCopy()
	}
	if in.AllocateOnce != nil {
		in, out := &in.AllocateOnce, &out.AllocateOnce
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationSpec.
func (in *ReservationSpec) DeepCopy() *ReservationSpec {
	if in == nil {
		return nil
	}
	out := new(ReservationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationStatus) DeepCopyInto(out *ReservationStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]ReservationCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.CurrentOwners != nil {
		in, out := &in.CurrentOwners, &out.CurrentOwners
		*out = make([]v1.ObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.Allocatable != nil {
		in, out := &in.Allocatable, &out.Allocatable
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.Allocated != nil {
		in, out := &in.Allocated, &out.Allocated
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationStatus.
func (in *ReservationStatus) DeepCopy() *ReservationStatus {
	if in == nil {
		return nil
	}
	out := new(ReservationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ReservationTemplateSpec) DeepCopyInto(out *ReservationTemplateSpec) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ReservationTemplateSpec.
func (in *ReservationTemplateSpec) DeepCopy() *ReservationTemplateSpec {
	if in == nil {
		return nil
	}
	out := new(ReservationTemplateSpec)
	in.DeepCopyInto(out)
	return out
}
