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
	"reflect"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
)

type cacheManager struct {
	lock            sync.Mutex
	constraintInfos map[string]*constraintInfo
}

func newCacheManager() *cacheManager {
	return &cacheManager{
		constraintInfos: map[string]*constraintInfo{},
	}
}

func (m *cacheManager) getConstraintInfo(namespace, name string) *constraintInfo {
	m.lock.Lock()
	defer m.lock.Unlock()
	namespacedName := types.NamespacedName{Namespace: namespace, Name: name}
	return m.constraintInfos[namespacedName.String()]
}

func (m *cacheManager) updatePod(oldPod, newPod *corev1.Pod) {
	var oldConstraint *apiext.TopologyAwareConstraint
	if oldPod != nil {
		var err error
		oldConstraint, err = apiext.GetTopologyAwareConstraint(oldPod.Annotations)
		if err != nil {
			klog.ErrorS(err, "Failed to GetTopologyAwareConstraint from old pod", "pod", klog.KObj(oldPod))
			return
		}
	}
	constraint, err := apiext.GetTopologyAwareConstraint(newPod.Annotations)
	if err != nil {
		klog.ErrorS(err, "Failed to GetTopologyAwareConstraint from pod", "pod", klog.KObj(newPod))
		return
	}

	if oldConstraint == nil && constraint == nil {
		return
	}

	if oldConstraint != nil && constraint != nil {
		if reflect.DeepEqual(oldConstraint, constraint) {
			return
		}
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	if oldConstraint != nil && validateTopologyAwareConstraint(oldConstraint) {
		m.updateConstraintInfo(oldPod, oldConstraint, false)
	}
	if constraint != nil && validateTopologyAwareConstraint(constraint) {
		m.updateConstraintInfo(newPod, constraint, true)
	}
}

func (m *cacheManager) deletePod(pod *corev1.Pod) {
	constraint, err := apiext.GetTopologyAwareConstraint(pod.Annotations)
	if err != nil || !validateTopologyAwareConstraint(constraint) {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	m.updateConstraintInfo(pod, constraint, false)
}

func (m *cacheManager) updateConstraintInfo(pod *corev1.Pod, constraint *apiext.TopologyAwareConstraint, add bool) {
	podKey, err := framework.GetPodKey(pod)
	if err != nil {
		return
	}

	namespacedName := types.NamespacedName{Namespace: pod.Namespace, Name: constraint.Name}
	constraintInfo := m.constraintInfos[namespacedName.String()]

	if add {
		if constraintInfo == nil {
			constraintInfo = newTopologyAwareConstraintInfo(pod.Namespace, constraint)
			m.constraintInfos[namespacedName.String()] = constraintInfo
		}
		constraintInfo.pods.Insert(podKey)

	} else {
		if constraintInfo != nil {
			constraintInfo.pods.Delete(podKey)
			if constraintInfo.pods.Len() == 0 {
				delete(m.constraintInfos, namespacedName.String())
			}
		}
	}
}

func validateTopologyAwareConstraint(constraint *apiext.TopologyAwareConstraint) bool {
	return constraint != nil && constraint.Name != "" && constraint.Required != nil
}
