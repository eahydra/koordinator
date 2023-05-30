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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	schedulertesting "k8s.io/kubernetes/pkg/scheduler/testing"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
)

var _ framework.SharedLister = &testSharedLister{}

type testSharedLister struct {
	nodes       []*corev1.Node
	nodeInfos   []*framework.NodeInfo
	nodeInfoMap map[string]*framework.NodeInfo
}

func newTestSharedLister(nodes []*corev1.Node) *testSharedLister {
	nodeInfoMap := make(map[string]*framework.NodeInfo)
	nodeInfos := make([]*framework.NodeInfo, 0)
	for _, node := range nodes {
		if _, ok := nodeInfoMap[node.Name]; !ok {
			nodeInfoMap[node.Name] = framework.NewNodeInfo()
		}
		nodeInfoMap[node.Name].SetNode(node)
	}

	for _, v := range nodeInfoMap {
		nodeInfos = append(nodeInfos, v)
	}

	return &testSharedLister{
		nodes:       nodes,
		nodeInfos:   nodeInfos,
		nodeInfoMap: nodeInfoMap,
	}
}

func (f *testSharedLister) NodeInfos() framework.NodeInfoLister {
	return f
}

func (f *testSharedLister) List() ([]*framework.NodeInfo, error) {
	return f.nodeInfos, nil
}

func (f *testSharedLister) HavePodsWithAffinityList() ([]*framework.NodeInfo, error) {
	return nil, nil
}

func (f *testSharedLister) HavePodsWithRequiredAntiAffinityList() ([]*framework.NodeInfo, error) {
	return nil, nil
}

func (f *testSharedLister) Get(nodeName string) (*framework.NodeInfo, error) {
	return f.nodeInfoMap[nodeName], nil
}

func Test_partitionTopologies(t *testing.T) {
	constraint := &apiext.TopologyAwareConstraint{
		Name: "test-constraint",
		Required: &apiext.TopologyConstraint{
			Topologies: []apiext.TopologyAwareTerm{
				{
					Key: "az-topology",
				},
				{
					Key: "custom-topology",
				},
			},
		},
	}

	var nodes []*corev1.Node
	var topologies []*topology
	for j := 0; j < 3; j++ {
		az := fmt.Sprintf("az-%d", j)
		custom := fmt.Sprintf("t%d", j)
		topo := topology{
			uniqueName: strings.Join([]string{az, custom}, "#"),
			labels: map[string]string{
				"az-topology":     az,
				"custom-topology": custom,
			},
			nodes: sets.NewString(),
		}
		for i := 0; i < 5; i++ {
			node := schedulertesting.MakeNode().
				Name(fmt.Sprintf("node-%d%d", j, i)).
				Label("az-topology", az).
				Label("custom-topology", custom).
				Obj()
			nodes = append(nodes, node)
			topo.nodes.Insert(node.Name)
		}
		topologies = append(topologies, &topo)
	}
	for i := 0; i < 5; i++ {
		node := schedulertesting.MakeNode().Name(fmt.Sprintf("unsatisfied-node-%d", i)).Label("az-topology", "az-4").Obj()
		nodes = append(nodes, node)
	}

	snapshot := newTestSharedLister(nodes)

	fh, err := schedulertesting.NewFramework(
		[]schedulertesting.RegisterPluginFunc{
			schedulertesting.RegisterBindPlugin(defaultbinder.Name, defaultbinder.New),
			schedulertesting.RegisterQueueSortPlugin(queuesort.Name, queuesort.New),
		},
		"koord-scheduler",
		frameworkruntime.WithSnapshotSharedLister(snapshot))
	assert.NoError(t, err)

	cInfo := newTopologyAwareConstraintInfo("default", constraint)
	err = partitionTopologies(context.TODO(), fh, cInfo)
	assert.NoError(t, err)

	expectedConstraintInfo := &constraintInfo{
		namespace:  "default",
		name:       constraint.Name,
		pods:       sets.NewString(),
		constraint: constraint,
		index:      0,
		topologies: topologies,
	}
	assert.Equal(t, expectedConstraintInfo, cInfo)
}

func Test_refresh_and_change(t *testing.T) {
	constraint := &apiext.TopologyAwareConstraint{
		Name: "test",
	}
	cInfo := newTopologyAwareConstraintInfo("default", constraint)
	assert.True(t, cInfo.shouldRefreshTopologies())
	cInfo.topologies = []*topology{{uniqueName: "a"}, {uniqueName: "b"}}
	assert.False(t, cInfo.shouldRefreshTopologies())
	topo := cInfo.getCurrentTopology()
	assert.Equal(t, "a", topo.uniqueName)
	cInfo.changeToNextTopology()
	topo = cInfo.getCurrentTopology()
	assert.Equal(t, "b", topo.uniqueName)
	assert.False(t, cInfo.shouldRefreshTopologies())
	cInfo.changeToNextTopology()
	assert.True(t, cInfo.shouldRefreshTopologies())
}
