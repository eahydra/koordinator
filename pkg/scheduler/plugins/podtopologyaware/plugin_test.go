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
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	schedulertesting "k8s.io/kubernetes/pkg/scheduler/testing"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
)

func TestPreFilter(t *testing.T) {
	var nodes []*corev1.Node
	var topologies []*topology
	for j := 0; j < 3; j++ {
		az := fmt.Sprintf("az-%d", j)
		topo := topology{
			uniqueName: az,
			labels: map[string]string{
				"az-topology": az,
			},
			nodes: sets.NewString(),
		}
		for i := 0; i < 5; i++ {
			node := schedulertesting.MakeNode().
				Name(fmt.Sprintf("node-%d%d", j, i)).
				Label("az-topology", az).
				Obj()
			nodes = append(nodes, node)
			topo.nodes.Insert(node.Name)
		}
		topologies = append(topologies, &topo)
	}

	constraint := &apiext.TopologyAwareConstraint{
		Name: "test-constraint",
		Required: &apiext.TopologyConstraint{
			Topologies: []apiext.TopologyAwareTerm{
				{
					Key: "az-topology",
				},
			},
		},
	}
	data, err := json.Marshal(constraint)
	assert.NoError(t, err)

	tests := []struct {
		name          string
		pod           *corev1.Pod
		constraint    *apiext.TopologyAwareConstraint
		wantStateData *stateData
		wantStatus    *framework.Status
	}{
		{
			name:          "non topology-aware constraint",
			pod:           &corev1.Pod{},
			wantStateData: &stateData{skip: true},
			wantStatus:    nil,
		},
		{
			name: "has topology-aware constraint but no synced",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "123456",
					Annotations: map[string]string{
						apiext.AnnotationTopologyAwareConstraint: string(data),
					},
				},
			},
			wantStateData: &stateData{skip: true},
			wantStatus:    framework.NewStatus(framework.Unschedulable, ErrReasonMissingConstraintCache),
		},
		{
			name: "has topology-aware constraint and ready for filtering",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "123456",
					Annotations: map[string]string{
						apiext.AnnotationTopologyAwareConstraint: string(data),
					},
				},
			},
			constraint: constraint,
			wantStateData: &stateData{
				skip: false,
				constraintInfo: &constraintInfo{
					namespace:  "default",
					name:       constraint.Name,
					pods:       sets.NewString("123456"),
					constraint: constraint,
					index:      0,
					topologies: topologies,
				},
				currentTopology: topologies[0],
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := newTestSharedLister(nodes)
			fh, err := schedulertesting.NewFramework(
				[]schedulertesting.RegisterPluginFunc{
					schedulertesting.RegisterBindPlugin(defaultbinder.Name, defaultbinder.New),
					schedulertesting.RegisterQueueSortPlugin(queuesort.Name, queuesort.New),
				},
				"koord-scheduler",
				frameworkruntime.WithSnapshotSharedLister(snapshot))
			assert.NoError(t, err)

			pl := &Plugin{
				handle:       fh,
				cacheManager: newCacheManager(),
			}

			if tt.constraint != nil {
				pl.cacheManager.updatePod(nil, tt.pod)
			}

			cycleState := framework.NewCycleState()
			status := pl.PreFilter(context.TODO(), cycleState, tt.pod)
			assert.Equal(t, tt.wantStatus, status)
			assert.Equal(t, tt.wantStateData, getStateData(cycleState))
		})
	}
}

func TestFilter(t *testing.T) {
	tests := []struct {
		name       string
		node       *corev1.Node
		sd         *stateData
		wantStatus *framework.Status
	}{
		{
			name: "no constraint",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
				},
			},
			sd:         &stateData{skip: true},
			wantStatus: nil,
		},
		{
			name: "has constraint but unmatched current topology",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
				},
			},
			sd: &stateData{
				skip: false,
				currentTopology: &topology{
					uniqueName: "test-1",
					nodes:      sets.NewString("other-node"),
				},
			},
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable, fmt.Sprintf("%s %s", ErrReasonNodeUnmatchedTopology, "test-1")),
		},
		{
			name: "has constraint and matched current topology",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
				},
			},
			sd: &stateData{
				skip: false,
				currentTopology: &topology{
					uniqueName: "test-1",
					nodes:      sets.NewString("test-node-1"),
				},
			},
			wantStatus: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl := &Plugin{}
			nodeInfo := framework.NewNodeInfo()
			nodeInfo.SetNode(tt.node)
			cycleState := framework.NewCycleState()
			cycleState.Write(Name, tt.sd)
			status := pl.Filter(context.TODO(), cycleState, &corev1.Pod{}, nodeInfo)
			assert.Equal(t, tt.wantStatus, status)
		})
	}
}

func TestPostFilter(t *testing.T) {
	tests := []struct {
		name          string
		sd            *stateData
		wantStatus    *framework.Status
		wantNextIndex int
	}{
		{
			name:       "no constraint",
			sd:         &stateData{skip: true},
			wantStatus: framework.NewStatus(framework.Unschedulable),
		},
		{
			name: "has constraint",
			sd: &stateData{
				skip:            false,
				constraintInfo:  &constraintInfo{},
				currentTopology: &topology{uniqueName: "test"},
			},
			wantNextIndex: 1,
			wantStatus:    framework.NewStatus(framework.Success),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl := &Plugin{}
			cycleState := framework.NewCycleState()
			cycleState.Write(Name, tt.sd)
			_, status := pl.PostFilter(context.TODO(), cycleState, &corev1.Pod{}, nil)
			assert.Equal(t, tt.wantStatus, status)
			if tt.sd.constraintInfo != nil {
				assert.Equal(t, tt.wantNextIndex, tt.sd.constraintInfo.index)
			}
		})
	}
}
