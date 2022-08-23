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

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/koordinator-sh/koordinator/apis/extension"
	sev1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
)

const (
	indexPodUUID      = "migration.pod.uuid"
	indexPodNamespace = "migration.pod.namespace"
)

const (
	LabelMigrateFromNode = extension.SchedulingDomainPrefix + "/migrate-from-node"
)

func registerIndex(c cache.Cache) {
	err := c.IndexField(context.Background(), &sev1alpha1.PodMigrationJob{}, indexPodUUID, func(obj client.Object) []string {
		migrationJob, ok := obj.(*sev1alpha1.PodMigrationJob)
		if !ok {
			return []string{}
		}
		if migrationJob.Spec.PodRef == nil {
			return []string{}
		}
		return []string{string(migrationJob.Spec.PodRef.UID)}
	})
	if err != nil {
		klog.Fatalf("Failed to register field index %s, err: %v", indexPodUUID, err)
	}
	err = c.IndexField(context.Background(), &sev1alpha1.PodMigrationJob{}, indexPodNamespace, func(obj client.Object) []string {
		migrationJob, ok := obj.(*sev1alpha1.PodMigrationJob)
		if !ok {
			return []string{}
		}
		if migrationJob.Spec.PodRef == nil {
			return []string{}
		}
		return []string{migrationJob.Spec.PodRef.Namespace}
	})
	if err != nil {
		klog.Fatalf("Failed to register field index %s, err: %v", indexPodNamespace, err)
	}
}
