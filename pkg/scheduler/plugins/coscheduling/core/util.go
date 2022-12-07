/*
Copyright 2022 The Koordinator Authors.
Copyright 2020 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/scheduler-plugins/pkg/apis/scheduling/v1alpha1"

	"github.com/koordinator-sh/koordinator/apis/extension"
)

// DefaultWaitTime is 600 if ScheduleTimeoutSeconds is not specified.
const DefaultWaitTime = 600 * time.Second

func IsPodNeedGang(pod *corev1.Pod) bool {
	return GetGangName(pod) != ""
}

func GetGangName(pod *corev1.Pod) string {
	name := GetPodGroupLabel(pod)
	if name == "" {
		name = extension.GetGangName(pod)
	}
	return name
}

// GetPodGroupLabel get pod group from pod annotations
func GetPodGroupLabel(pod *corev1.Pod) string {
	return pod.Labels[v1alpha1.PodGroupLabel]
}

// GetGangFullName get namespaced Gang name from pod annotations
func GetGangFullName(pod *corev1.Pod) string {
	gangName := GetGangName(pod)
	if gangName == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s", pod.Namespace, gangName)
}

// GetNamespacedName returns the namespaced name
func GetNamespacedName(obj metav1.Object) string {
	return fmt.Sprintf("%v/%v", obj.GetNamespace(), obj.GetName())
}

func ParseGangTimeoutSeconds(timeoutSeconds int32) (time.Duration, error) {
	if timeoutSeconds <= 0 {
		return 0, fmt.Errorf("podGroup timeout value is illegal,timeout Value:%v", timeoutSeconds)
	}
	return time.Duration(timeoutSeconds) * time.Second, nil
}

// ParseGangGroups parses s into string slice with Gang full names
func ParseGangGroups(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var gangGroup []string
	err := json.Unmarshal([]byte(s), &gangGroup)
	if err != nil {
		return nil, err
	}
	return gangGroup, nil
}

// CreateMergePatch return patch generated from original and new interfaces
func CreateMergePatch(original, new interface{}) ([]byte, error) {
	pvByte, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	cloneByte, err := json.Marshal(new)
	if err != nil {
		return nil, err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(pvByte, cloneByte, original)
	if err != nil {
		return nil, err
	}
	return patch, nil
}
