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
	"context"
	"sort"
	"strings"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
)

type constraintInfo struct {
	namespace  string
	name       string
	pods       sets.String
	constraint *apiext.TopologyAwareConstraint

	index      int
	topologies []*topology
}

type topology struct {
	uniqueName string
	labels     map[string]string
	nodes      sets.String
	anchor     string
}

func newTopologyAwareConstraintInfo(namespace string, constraint *apiext.TopologyAwareConstraint) *constraintInfo {
	return &constraintInfo{
		namespace:  namespace,
		name:       constraint.Name,
		pods:       sets.NewString(),
		constraint: constraint,
	}
}

func (s *constraintInfo) shouldRefreshTopologies() bool {
	return len(s.topologies) == 0 || s.index == len(s.topologies)
}

func (s *constraintInfo) changeToNextTopology() {
	s.index++
}

func (s *constraintInfo) getCurrentTopology() *topology {
	if s.index < len(s.topologies) {
		return s.topologies[s.index]
	}
	return nil
}

func partitionTopologies(ctx context.Context, handle framework.Handle, constraintInfo *constraintInfo) error {
	allNodeInfos, err := handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		return err
	}
	topologies := make([]*topology, len(allNodeInfos))
	var nodeIndex int32
	partitionFn := func(piece int) {
		nodeInfo := allNodeInfos[piece]
		node := nodeInfo.Node()
		if node == nil {
			return
		}
		var topology topology
		values := make([]string, 0, len(constraintInfo.constraint.Required.Topologies))
		for _, term := range constraintInfo.constraint.Required.Topologies {
			topologyKey := term.Key
			topologyValue, ok := node.Labels[topologyKey]
			if !ok {
				return
			}
			if topology.labels == nil {
				topology.labels = map[string]string{}
			}
			topology.labels[topologyKey] = topologyValue
			values = append(values, topologyValue)
		}
		if len(values) == 0 {
			return
		}
		topology.uniqueName = strings.Join(values, "#")
		topology.anchor = node.Name
		index := atomic.AddInt32(&nodeIndex, 1)
		topologies[index-1] = &topology
	}
	handle.Parallelizer().Until(ctx, len(allNodeInfos), partitionFn)
	topologies = topologies[:nodeIndex]
	if len(topologies) == 0 {
		return nil
	}

	merged := map[string]*topology{}
	for _, topo := range topologies {
		r := merged[topo.uniqueName]
		if r == nil {
			r = &topology{
				uniqueName: topo.uniqueName,
				labels:     topo.labels,
				nodes:      sets.NewString(),
			}
			merged[topo.uniqueName] = r
		}
		r.nodes.Insert(topo.anchor)
	}

	result := make([]*topology, 0, len(merged))
	for _, v := range merged {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].uniqueName < result[j].uniqueName
	})
	constraintInfo.topologies = result
	constraintInfo.index = 0
	return nil
}
