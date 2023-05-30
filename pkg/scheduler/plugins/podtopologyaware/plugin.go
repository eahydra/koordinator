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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	apiext "github.com/koordinator-sh/koordinator/apis/extension"
	"github.com/koordinator-sh/koordinator/pkg/scheduler/frameworkext"
)

const (
	Name = "PodTopologyAware"

	ErrReasonMissingConstraintCache = "missing topology aware constraint cache"
	ErrReasonNodeUnmatchedTopology  = "node(s) unmatched topology"
)

var _ framework.PreFilterPlugin = &Plugin{}
var _ framework.FilterPlugin = &Plugin{}
var _ framework.PostFilterPlugin = &Plugin{}

type Plugin struct {
	handle       framework.Handle
	cacheManager *cacheManager
}

func New(args runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	cacheManager := newCacheManager()
	extendedHandle, ok := handle.(frameworkext.ExtendedHandle)
	if !ok {
		return nil, fmt.Errorf("expect handle to be type frameworkext.ExtendedHandle, got %T", handle)
	}
	registerPodEventHandler(cacheManager, handle.SharedInformerFactory(), extendedHandle.KoordinatorSharedInformerFactory())
	pl := &Plugin{
		handle:       handle,
		cacheManager: cacheManager,
	}
	return pl, nil
}

func (pl *Plugin) Name() string {
	return Name
}

type stateData struct {
	skip            bool
	constraintInfo  *constraintInfo
	currentTopology *topology
}

func (s *stateData) Clone() framework.StateData {
	return s
}

func getStateData(cycleState *framework.CycleState) *stateData {
	val, _ := cycleState.Read(Name)
	s, _ := val.(*stateData)
	if s == nil {
		s = &stateData{
			skip: true,
		}
	}
	return s
}

func (pl *Plugin) PreFilter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod) *framework.Status {
	constraint, err := apiext.GetTopologyAwareConstraint(pod.Annotations)
	if err != nil || constraint == nil {
		klog.V(5).InfoS("pod skip topology-aware constraint", "pod", klog.KObj(pod))
		cycleState.Write(Name, &stateData{skip: true})
		return nil
	}

	constraintInfo := pl.cacheManager.getConstraintInfo(pod.Namespace, constraint.Name)
	if constraintInfo == nil {
		return framework.NewStatus(framework.Unschedulable, ErrReasonMissingConstraintCache)
	}
	if constraintInfo.shouldRefreshTopologies() {
		if err := partitionTopologies(ctx, pl.handle, constraintInfo); err != nil {
			klog.ErrorS(err, "Failed refresh topologies by topology-aware constraint", "pod", klog.KObj(pod))
			return framework.AsStatus(err)
		}
	}
	currentTopology := constraintInfo.getCurrentTopology()
	if currentTopology == nil {
		klog.Errorf("unexpected things happened, missing current topology in topology-aware constraint", "pod", klog.KObj(pod))
		return framework.NewStatus(framework.Error, "missing current topology")
	}
	klog.V(4).InfoS("pod try to filter in topology",
		"pod", klog.KObj(pod), "constraintName", constraintInfo.name, "topologyName", currentTopology.uniqueName)

	sd := &stateData{
		skip:            false,
		constraintInfo:  constraintInfo,
		currentTopology: currentTopology,
	}
	cycleState.Write(Name, sd)
	return nil
}

func (pl *Plugin) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func (pl *Plugin) Filter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	sd := getStateData(cycleState)
	if sd.skip {
		return nil
	}

	node := nodeInfo.Node()
	if node == nil {
		return framework.NewStatus(framework.Error, "node not found")
	}

	if !sd.currentTopology.nodes.Has(node.Name) {
		return framework.NewStatus(framework.UnschedulableAndUnresolvable, fmt.Sprintf("%s %s", ErrReasonNodeUnmatchedTopology, sd.currentTopology.uniqueName))
	}

	return nil
}

func (pl *Plugin) PostFilter(ctx context.Context, cycleState *framework.CycleState, pod *corev1.Pod, filteredNodeStatusMap framework.NodeToStatusMap) (*framework.PostFilterResult, *framework.Status) {
	sd := getStateData(cycleState)
	if sd.skip {
		return nil, framework.NewStatus(framework.Unschedulable)
	}
	sd.constraintInfo.changeToNextTopology()
	klog.V(4).InfoS("topology-aware constraint should change to next topology",
		"pod", klog.KObj(pod), "constraintName", sd.constraintInfo.name, "failedTopology", sd.currentTopology.uniqueName)
	return nil, framework.NewStatus(framework.Success)
}
