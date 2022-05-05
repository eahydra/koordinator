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

package statesinformer

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/koordinator-sh/koordinator/pkg/util"
)

type KubeletStub interface {
	GetAllPods() (corev1.PodList, error)
}

type kubeletStub struct {
	ipAddr         string
	httpPort       int
	timeoutSeconds int
}

func NewKubeletStub(ip string, port, timeoutSeconds int) KubeletStub {
	return &kubeletStub{
		ipAddr:         ip,
		httpPort:       port,
		timeoutSeconds: timeoutSeconds,
	}
}

func (k *kubeletStub) GetAllPods() (corev1.PodList, error) {
	podList := corev1.PodList{}
	result, err := util.DoHTTPGet("pods", k.ipAddr, k.httpPort, k.timeoutSeconds)
	if err != nil {
		return podList, err
	}
	// parse json data
	err = json.Unmarshal(result, &podList)
	if err != nil {
		return podList, fmt.Errorf("parse kubelet pod list failed, err: %v", err)
	}
	return podList, nil
}
