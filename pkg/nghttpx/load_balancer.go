/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"net/http"
	"os/exec"
	"strconv"
	"sync/atomic"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
)

type LoadBalancerConfig struct {
	NghttpxHealthPort    int32
	NghttpxAPIPort       int32
	NghttpxConfDir       string
	Pod                  *corev1.Pod
	EventRecorder        events.EventRecorder
	ReloadTimeout        time.Duration
	StaleAssetsThreshold time.Duration
}

// LoadBalancer starts nghttpx and reloads its configuration on demand.
type LoadBalancer struct {
	// httpClient is used to issue backend API request to nghttpx
	httpClient    *http.Client
	pod           *corev1.Pod
	eventRecorder events.EventRecorder

	// template loaded ready to be used to generate the nghttpx configuration file
	template *template.Template

	// template for backend configuration.  This is a part of nghttpx configuration, and included from main one (template above).  We
	// have separate template for backend to change backend configuration without reloading nghttpx if main configuration has not
	// changed.
	backendTemplate *template.Template
	// backendconfigURI is the nghttpx backendconfig endpoint.
	backendconfigURI string
	// configrevisionURI is the nghttpx configrevision endpoint.
	configrevisionURI string
	// reloadTimeout is the timeout before controller gives up to wait for configRevision to change.
	reloadTimeout time.Duration
	// staleAssetsThreshold is the duration that asset files are considered stale.
	staleAssetsThreshold time.Duration

	// cmd is nghttpx command.
	cmd *exec.Cmd

	// reloadCounter is the number of the successful configuration reload.
	reloadCounter atomic.Uint64
}

// NewLoadBalancer creates new LoadBalancer.
func NewLoadBalancer(config LoadBalancerConfig) (*LoadBalancer, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// Disable keep alive connection, so that API request does not interfere graceful shutdown of nghttpx.
	tr.DisableKeepAlives = true
	apiURI := "http://127.0.0.1:" + strconv.FormatInt(int64(config.NghttpxAPIPort), 10)

	lb := &LoadBalancer{
		httpClient: &http.Client{
			Transport: tr,
		},
		pod:                  config.Pod,
		eventRecorder:        config.EventRecorder,
		backendconfigURI:     apiURI + "/api/v1beta1/backendconfig",
		configrevisionURI:    apiURI + "/api/v1beta1/configrevision",
		reloadTimeout:        config.ReloadTimeout,
		staleAssetsThreshold: config.StaleAssetsThreshold,
	}

	lb.loadTemplate()

	if err := lb.writeDefaultNghttpxConfig(config.NghttpxConfDir, config.NghttpxHealthPort, config.NghttpxAPIPort); err != nil {
		return nil, err
	}

	return lb, nil
}

func (lb *LoadBalancer) GetReloadCounter() uint64 {
	return lb.reloadCounter.Load()
}
