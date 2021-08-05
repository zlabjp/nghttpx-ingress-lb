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
	"fmt"
	"net/http"
	"os/exec"
	"text/template"
	"time"
)

// Manager ...
type Manager struct {
	// httpClient is used to issue backend API request to nghttpx
	httpClient *http.Client

	// template loaded ready to be used to generate the nghttpx configuration file
	template *template.Template

	// template for backend configuration.  This is a part of
	// nghttpx configuration, and included from main one (template
	// above).  We have separate template for backend to change
	// backend configuration without reloading nghttpx if main
	// configuration has not changed.
	backendTemplate *template.Template
	// backendconfigURI is the nghttpx backendconfig endpoint.
	backendconfigURI string
	// configrevisionURI is the nghttpx configrevision endpoint.
	configrevisionURI string

	// cmd is nghttpx command.
	cmd *exec.Cmd
}

// NewManager ...
func NewManager(apiPort int32) *Manager {
	mgr := &Manager{
		httpClient: &http.Client{
			Timeout: time.Second * 30,
			Transport: &http.Transport{
				// Disable keep alive connection, so that API request does not interfere graceful shutdown of nghttpx.
				DisableKeepAlives: true,
			},
		},
		backendconfigURI:  fmt.Sprintf("http://127.0.0.1:%v/api/v1beta1/backendconfig", apiPort),
		configrevisionURI: fmt.Sprintf("http://127.0.0.1:%v/api/v1beta1/configrevision", apiPort),
	}

	mgr.loadTemplate()

	return mgr
}
