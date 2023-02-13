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

package noderesource

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/koordinator-sh/koordinator/apis/extension"
	schedulingv1alpha1 "github.com/koordinator-sh/koordinator/apis/scheduling/v1alpha1"
	slov1alpha1 "github.com/koordinator-sh/koordinator/apis/slo/v1alpha1"
	"github.com/koordinator-sh/koordinator/pkg/slo-controller/config"
	"github.com/koordinator-sh/koordinator/pkg/util"
)

type nodeBEResource struct {
	IsColocationAvailable bool

	MilliCPU *resource.Quantity
	Memory   *resource.Quantity

	Reason  string
	Message string
}

type SyncContext struct {
	lock       sync.RWMutex
	contextMap map[string]time.Time
}

func NewSyncContext() SyncContext {
	return SyncContext{
		contextMap: map[string]time.Time{},
	}
}

func (s *SyncContext) Load(key string) (time.Time, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	value, ok := s.contextMap[key]
	return value, ok
}

func (s *SyncContext) Store(key string, value time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.contextMap[key] = value
}

func (s *SyncContext) Delete(key string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.contextMap, key)
}

func (r *NodeResourceReconciler) isColocationCfgDisabled(node *corev1.Node) bool {
	cfg := r.cfgCache.GetCfgCopy()
	if cfg.Enable == nil || !*cfg.Enable {
		return true
	}
	strategy := config.GetNodeColocationStrategy(cfg, node)
	if strategy == nil || strategy.Enable == nil {
		return true
	}
	return !(*strategy.Enable)
}

func (r *NodeResourceReconciler) isDegradeNeeded(nodeMetric *slov1alpha1.NodeMetric, node *corev1.Node) bool {
	if nodeMetric == nil || nodeMetric.Status.UpdateTime == nil {
		klog.Warningf("invalid NodeMetric: %v, need degradation", nodeMetric)
		return true
	}

	strategy := config.GetNodeColocationStrategy(r.cfgCache.GetCfgCopy(), node)

	if r.Clock.Now().After(nodeMetric.Status.UpdateTime.Add(time.Duration(*strategy.DegradeTimeMinutes) * time.Minute)) {
		klog.Warningf("timeout NodeMetric: %v, current timestamp: %v, metric last update timestamp: %v",
			nodeMetric.Name, r.Clock.Now(), nodeMetric.Status.UpdateTime)
		return true
	}

	return false
}

func (r *NodeResourceReconciler) resetNodeBEResource(node *corev1.Node, reason, message string) error {
	beResource := &nodeBEResource{
		IsColocationAvailable: false,
		MilliCPU:              nil,
		Memory:                nil,
		Reason:                reason,
		Message:               message,
	}
	return r.updateNodeBEResource(node, beResource)
}

func (r *NodeResourceReconciler) isGPUResourceNeedSync(new, old *corev1.Node) bool {
	strategy := config.GetNodeColocationStrategy(r.cfgCache.GetCfgCopy(), new)

	lastUpdatedTime, ok := r.GPUSyncContext.Load(util.GenerateNodeKey(&new.ObjectMeta))
	if !ok || r.Clock.Since(lastUpdatedTime) > time.Duration(*strategy.UpdateTimeThresholdSeconds)*time.Second {
		klog.V(4).Infof("node %v resource expired, need sync", new.Name)
		return true
	}

	for _, resourceName := range []corev1.ResourceName{extension.ResourceGPUCore, extension.ResourceGPUMemoryRatio, extension.ResourceGPUMemory, extension.ResourceGPU} {
		if util.IsResourceDiff(old.Status.Allocatable, new.Status.Allocatable, resourceName, *strategy.ResourceDiffThreshold) {
			klog.V(4).Infof("node %v resource diff bigger than %v, need sync", resourceName, *strategy.ResourceDiffThreshold)
			return true
		}
	}
	return false
}

func (r *NodeResourceReconciler) isGPULabelNeedSync(new, old map[string]string) bool {
	return new[extension.LabelGPUModel] != old[extension.LabelGPUModel] ||
		new[extension.LabelGPUDriverVersion] != old[extension.LabelGPUDriverVersion]
}

func (r *NodeResourceReconciler) updateGPUNodeResource(node *corev1.Node, device *schedulingv1alpha1.Device) error {
	if device == nil {
		return nil
	}
	gpuResources := make(corev1.ResourceList)
	totalKoordGPU := resource.NewQuantity(0, resource.DecimalSI)
	hasGPUDevice := false
	for _, device := range device.Spec.Devices {
		if device.Type != schedulingv1alpha1.GPU || !device.Health {
			continue
		}
		hasGPUDevice = true
		resources := extension.TransformDeprecatedDeviceResources(device.Resources)
		util.AddResourceList(gpuResources, resources)
		totalKoordGPU.Add(resources[extension.ResourceGPUCore])
	}
	gpuResources[extension.ResourceGPU] = *totalKoordGPU

	if !hasGPUDevice {
		return nil
	}

	copyNode := node.DeepCopy()
	util.AddResourceList(copyNode.Status.Allocatable, gpuResources)
	if !r.isGPUResourceNeedSync(copyNode, node) {
		return nil
	}

	err := util.RetryOnConflictOrTooManyRequests(func() error {
		updateNode := &corev1.Node{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: node.Name}, updateNode); err != nil {
			klog.Errorf("failed to get node %v, error: %v", node.Name, err)
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		updateNode = updateNode.DeepCopy() // avoid overwriting the cache
		util.AddResourceList(updateNode.Status.Capacity, gpuResources)
		util.AddResourceList(updateNode.Status.Allocatable, gpuResources)

		if err := r.Client.Status().Update(context.TODO(), updateNode); err != nil {
			klog.Errorf("failed to update node gpu resource, %v, error: %v", updateNode.Name, err)
			return err
		}
		return nil
	})
	if err == nil {
		r.GPUSyncContext.Store(util.GenerateNodeKey(&node.ObjectMeta), r.Clock.Now())
	}
	return err
}

func (r *NodeResourceReconciler) updateGPUDriverAndModel(node *corev1.Node, device *schedulingv1alpha1.Device) error {
	if device == nil || device.Labels == nil {
		return nil
	}

	if !r.isGPULabelNeedSync(device.Labels, node.Labels) {
		return nil
	}

	err := util.RetryOnConflictOrTooManyRequests(func() error {
		updateNode := &corev1.Node{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: node.Name}, updateNode); err != nil {
			klog.Errorf("failed to get node %v, error: %v", node.Name, err)
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		updateNodeNew := updateNode.DeepCopy()
		if updateNodeNew.Labels == nil {
			updateNodeNew.Labels = make(map[string]string)
		}
		updateNodeNew.Labels[extension.LabelGPUModel] = device.Labels[extension.LabelGPUModel]
		updateNodeNew.Labels[extension.LabelGPUDriverVersion] = device.Labels[extension.LabelGPUDriverVersion]

		patch := client.MergeFrom(updateNode)
		if err := r.Client.Patch(context.Background(), updateNodeNew, patch); err != nil {
			klog.Errorf("failed to patch node gpu model and version, err:%v", err)
			return err
		} else {
			klog.Infof("Success to patch node:%v gpu model:%v and version:%v",
				node.Name, device.Labels[extension.LabelGPUModel], device.Labels[extension.LabelGPUDriverVersion])
		}
		return nil
	})
	if err == nil {
		r.GPUSyncContext.Store(util.GenerateNodeKey(&node.ObjectMeta), r.Clock.Now())
	}

	return nil
}

func (r *NodeResourceReconciler) updateNodeBEResource(node *corev1.Node, beResource *nodeBEResource) error {
	nodeCopy := node.DeepCopy() // avoid overwriting the cache

	r.prepareNodeResource(nodeCopy, beResource)

	if needSync := r.isBEResourceSyncNeeded(node, nodeCopy); !needSync {
		return nil
	}

	return util.RetryOnConflictOrTooManyRequests(func() error {
		nodeCopy = &corev1.Node{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: node.Name}, nodeCopy); err != nil {
			if errors.IsNotFound(err) {
				klog.V(4).Infof("aborted to update node %v because error: %v", nodeCopy.Name, err)
				return nil
			}
			klog.Errorf("failed to get node %v, error: %v", node.Name, err)
			return err
		}

		nodeCopy = nodeCopy.DeepCopy() // avoid overwriting the cache
		r.prepareNodeResource(nodeCopy, beResource)

		if err := r.Client.Status().Update(context.TODO(), nodeCopy); err != nil {
			klog.Errorf("failed to update node %v, error: %v", nodeCopy.Name, err)
			return err
		}
		r.BESyncContext.Store(util.GenerateNodeKey(&node.ObjectMeta), r.Clock.Now())
		klog.V(5).Infof("update node %v successfully, detail %+v", nodeCopy.Name, nodeCopy)
		return nil
	})
}

func (r *NodeResourceReconciler) isBEResourceSyncNeeded(old, new *corev1.Node) bool {
	if new == nil || new.Status.Allocatable == nil || new.Status.Capacity == nil {
		klog.Errorf("invalid input, node should not be nil")
		return false
	}

	strategy := config.GetNodeColocationStrategy(r.cfgCache.GetCfgCopy(), new)

	// scenario 1: update time gap is bigger than UpdateTimeThresholdSeconds
	lastUpdatedTime, ok := r.BESyncContext.Load(util.GenerateNodeKey(&new.ObjectMeta))
	if !ok || r.Clock.Since(lastUpdatedTime) > time.Duration(*strategy.UpdateTimeThresholdSeconds)*time.Second {
		klog.V(4).Infof("node %v resource expired, need sync", new.Name)
		return true
	}

	// scenario 2: resource diff is bigger than ResourceDiffThreshold
	resourcesToDiff := []corev1.ResourceName{extension.BatchCPU, extension.BatchMemory}
	for _, resourceName := range resourcesToDiff {
		if util.IsResourceDiff(old.Status.Allocatable, new.Status.Allocatable, resourceName, *strategy.ResourceDiffThreshold) {
			klog.V(4).Infof("node %v resource %v diff bigger than %v, need sync", new.Name, resourceName, *strategy.ResourceDiffThreshold)
			return true
		}
	}

	// scenario 3: all good, do nothing
	klog.V(4).Infof("all good, no need to sync for node %v", new.Name)
	return false
}

func (r *NodeResourceReconciler) prepareNodeResource(node *corev1.Node, beResource *nodeBEResource) {
	if beResource.MilliCPU == nil {
		delete(node.Status.Capacity, extension.BatchCPU)
		delete(node.Status.Allocatable, extension.BatchCPU)
	} else {
		// NOTE: extended resource would be validated as an integer, so beResource should be checked before the update
		if _, ok := beResource.MilliCPU.AsInt64(); !ok {
			klog.V(2).Infof("batch cpu quantity is not int64 type and will be rounded, original value %v",
				*beResource.MilliCPU)
			beResource.MilliCPU.Set(beResource.MilliCPU.Value())
		}
		node.Status.Capacity[extension.BatchCPU] = *beResource.MilliCPU
		node.Status.Allocatable[extension.BatchCPU] = *beResource.MilliCPU
	}

	if beResource.Memory == nil {
		delete(node.Status.Capacity, extension.BatchMemory)
		delete(node.Status.Allocatable, extension.BatchMemory)
	} else {
		// NOTE: extended resource would be validated as an integer, so beResource should be checked before the update
		if _, ok := beResource.Memory.AsInt64(); !ok {
			klog.V(2).Infof("batch memory quantity is not int64 type and will be rounded, original value %v",
				*beResource.Memory)
			beResource.Memory.Set(beResource.Memory.Value())
		}
		node.Status.Capacity[extension.BatchMemory] = *beResource.Memory
		node.Status.Allocatable[extension.BatchMemory] = *beResource.Memory
	}

	strategy := config.GetNodeColocationStrategy(r.cfgCache.GetCfgCopy(), node)
	runNodePrepareExtenders(strategy, node)
}
