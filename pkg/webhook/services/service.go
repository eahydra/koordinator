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

package services

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	servicesBaseRelativePath = "/apis/v1/"
)

var engine gin.Engine
var registerProviderMap = map[string]WebhookServiceProvider{}

type mux interface {
	Register(path string, handler http.Handler)
}

func InstallAPIHandler(mux mux, isLeader func() bool) {
	e := gin.Default()
	installWebhookServices(e)

	mux.Register(servicesBaseRelativePath, handle(isLeader))
}

func handle(isLeader func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLeader() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"message": "the instance is not leader"}`))
			return
		}
		engine.ServeHTTP(w, r)
	}
}

func installWebhookServices(e *gin.Engine) {
	baseGroup := e.Group(servicesBaseRelativePath)
	for _, v := range registerProviderMap {
		v.RegisterServices(baseGroup)
	}
}

func RegisterWebhookService(name string, provider WebhookServiceProvider) {
	registerProviderMap[name] = provider
}
