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

package migration

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sev1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
)

func (r *Reconciler) filterExistingPodMigrationJob(pod *corev1.Pod) bool {
	opts := &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(indexPodUUID, string(pod.UID))}
	existing := false
	r.forEachAvailableMigrationJobs(opts, func(job *sev1alpha1.PodMigrationJob) bool {
		existing = true
		return false
	})
	return !existing
}

func (r *Reconciler) filterMaxMigratingPerNode(pod *corev1.Pod) bool {
	if pod.Spec.NodeName == "" || r.args.MaxMigratingPerNode == nil || *r.args.MaxMigratingPerNode <= 0 {
		return true
	}

	opts := &client.ListOptions{LabelSelector: labels.SelectorFromSet(map[string]string{
		LabelMigrateFromNode: pod.Spec.NodeName,
	})}
	count := 0
	r.forEachAvailableMigrationJobs(opts, func(job *sev1alpha1.PodMigrationJob) bool {
		count++
		return true
	})

	return count < int(*r.args.MaxMigratingPerNode)
}

func (r *Reconciler) filterMaxMigratingPerNamespace(pod *corev1.Pod) bool {
	if r.args.MaxMigratingPerNamespace == nil || *r.args.MaxMigratingPerNamespace <= 0 {
		return true
	}

	opts := &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(indexPodNamespace, pod.Namespace)}
	count := 0
	r.forEachAvailableMigrationJobs(opts, func(job *sev1alpha1.PodMigrationJob) bool {
		count++
		return true
	})
	return count < int(*r.args.MaxMigratingPerNamespace)
}

func (r *Reconciler) forEachAvailableMigrationJobs(listOpts *client.ListOptions, handler func(job *sev1alpha1.PodMigrationJob) bool) {
	jobList := &sev1alpha1.PodMigrationJobList{}
	err := r.Client.List(context.TODO(), jobList, listOpts)
	if err != nil {
		klog.Errorf("failed to get PodMigrationJobList, err: %v", err)
		return
	}

	for i := range jobList.Items {
		job := &jobList.Items[i]
		phase := job.Status.Phase
		if phase == "" || phase == sev1alpha1.PodMigrationJobPending || phase == sev1alpha1.PodMigrationJobRunning {
			if !handler(job) {
				break
			}
		}
	}
	return
}
