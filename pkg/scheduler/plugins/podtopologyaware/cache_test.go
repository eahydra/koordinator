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

package podtopologyaware

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
)

func TestCacheManagerUpdate(t *testing.T) {
	m := newCacheManager()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod-1",
			Namespace:   "default",
			UID:         "123456",
			Annotations: map[string]string{},
		},
	}
	constraint := &apiext.TopologyAwareConstraint{
		Name: "test-topology",
		Required: &apiext.TopologyConstraint{
			Topologies: []apiext.TopologyAwareTerm{
				{
					Key: "topology.kubernetes.io/zone",
				},
			},
		},
	}
	data, err := json.Marshal(constraint)
	assert.NoError(t, err)
	pod.Annotations[apiext.AnnotationTopologyAwareConstraint] = string(data)

	m.updatePod(nil, pod)
	cInfo := m.getConstraintInfo(pod.Namespace, constraint.Name)
	expectedConstraintInfo := &constraintInfo{
		namespace:  pod.Namespace,
		name:       "test-topology",
		pods:       sets.NewString(string(pod.UID)),
		constraint: constraint,
	}
	assert.Equal(t, expectedConstraintInfo, cInfo)

	m.updatePod(pod, pod)
	cInfo = m.getConstraintInfo(pod.Namespace, constraint.Name)
	assert.Equal(t, expectedConstraintInfo, cInfo)

	m.deletePod(pod)
	cInfo = m.getConstraintInfo(pod.Namespace, constraint.Name)
	assert.Nil(t, cInfo)
}
