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
)

const (
	AnnotationTopologyAwareConstraint = SchedulingDomainPrefix + "/topology-aware-constraint"
)

type TopologyAwareConstraint struct {
	Name     string              `json:"name,omitempty"`
	Required *TopologyConstraint `json:"required,omitempty"`
}

type TopologyConstraint struct {
	Topologies []TopologyAwareTerm `json:"topologies,omitempty"`
}

type TopologyAwareTerm struct {
	Key string `json:"key,omitempty"`
}

func GetTopologyAwareConstraint(annotations map[string]string) (*TopologyAwareConstraint, error) {
	var selector *TopologyAwareConstraint
	if s := annotations[AnnotationTopologyAwareConstraint]; s != "" {
		selector = &TopologyAwareConstraint{}
		if err := json.Unmarshal([]byte(s), selector); err != nil {
			return nil, err
		}
	}
	return selector, nil
}
