package core

import (
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
)

type GangSummary struct {
	Name               string         `json:"name,omitempty"`
	CreationTimestamp  time.Time      `json:"creationTimestamp"`
	Spec               GangSpec       `json:"spec"`
	SpecInitialized    bool           `json:"specInitialized"`
	Pods               sets.String    `json:"pods,omitempty"`
	WaitingPods        sets.String    `json:"waitingPods,omitempty"`
	BoundPods          sets.String    `json:"boundPods,omitempty"`
	ResourceSatisfied  bool           `json:"resourceSatisfied,omitempty"`
	ScheduleCycleValid bool           `json:"scheduleCycleValid,omitempty"`
	ScheduleCycle      int            `json:"scheduleCycle,omitempty"`
	PodScheduleCycles  map[string]int `json:"podScheduleCycles,omitempty"`
}

func (gang *Gang) GetGangSummary() *GangSummary {
	summary := &GangSummary{
		Pods:              sets.NewString(),
		WaitingPods:       sets.NewString(),
		BoundPods:         sets.NewString(),
		PodScheduleCycles: make(map[string]int),
	}

	if gang == nil {
		return summary
	}

	gang.lock.Lock()
	defer gang.lock.Unlock()

	summary.Name = gang.Name
	summary.Spec = gang.Spec
	summary.SpecInitialized = gang.SpecInitialized
	summary.CreationTimestamp = gang.CreationTimestamp
	summary.ResourceSatisfied = gang.resourceSatisfied
	summary.ScheduleCycleValid = gang.scheduleCycleValid
	summary.ScheduleCycle = gang.scheduleCycle

	for podName := range gang.pods {
		summary.Pods.Insert(podName)
	}
	for podName := range gang.waitingPods {
		summary.WaitingPods.Insert(podName)
	}
	for podName := range gang.boundPods {
		summary.BoundPods.Insert(podName)
	}
	for key, value := range gang.podScheduleCycles {
		summary.PodScheduleCycles[key] = value
	}

	return summary
}
