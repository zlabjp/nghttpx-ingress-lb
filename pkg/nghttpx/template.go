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
	"bytes"
	_ "embed"
	"text/template"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var (
	funcMap = template.FuncMap{
		"empty": func(input interface{}) bool {
			check, ok := input.(string)
			if ok {
				return len(check) == 0
			}

			return true
		},
		"duration": func(d *metav1.Duration) string {
			return nghttpxDuration(d.Duration)
		},
	}
)

var (
	//go:embed nghttpx.tmpl
	nghttpxTmpl string
	//go:embed nghttpx-backend.tmpl
	nghttpxBackendTmpl string
)

func (mgr *Manager) loadTemplate() {
	mgr.template = template.Must(template.New("nghttpx.tmpl").Funcs(funcMap).Parse(nghttpxTmpl))
	mgr.backendTemplate = template.Must(template.New("nghttpx-backend.tmpl").Funcs(funcMap).Parse(nghttpxBackendTmpl))
}

const (
	// configuration has not changed
	configNotChanged int = iota
	// main configuration has changed
	mainConfigChanged
	// only backend configuration has changed
	backendConfigChanged
)

// generateCfg generates nghttpx's main and backend configurations.
func (mgr *Manager) generateCfg(ingConfig *IngressConfig) ([]byte, []byte, error) {
	mainConfigBuffer := new(bytes.Buffer)
	if err := mgr.template.Execute(mainConfigBuffer, ingConfig); err != nil {
		klog.Infof("nghttpx error while executing main configuration template: %v", err)
		return nil, nil, err
	}

	backendConfigBuffer := new(bytes.Buffer)
	if err := mgr.backendTemplate.Execute(backendConfigBuffer, ingConfig); err != nil {
		klog.Infof("nghttpx error while executing backend configuration template: %v", err)
		return nil, nil, err
	}

	return mainConfigBuffer.Bytes(), backendConfigBuffer.Bytes(), nil
}

func (mgr *Manager) checkAndWriteCfg(ingConfig *IngressConfig, mainConfig, backendConfig []byte) (int, error) {
	configPath := ConfigPath(ingConfig.ConfDir)
	backendConfigPath := BackendConfigPath(ingConfig.ConfDir)

	if err := MkdirAll(ingConfig.ConfDir); err != nil {
		return configNotChanged, err
	}

	// If main configuration has changed, we need to reload nghttpx
	mainChanged, err := needsReload(configPath, mainConfig)
	if err != nil {
		return configNotChanged, err
	}

	// If backend configuration has changed, we need to issue
	// backend replace API to nghttpx
	backendChanged, err := needsReload(backendConfigPath, backendConfig)
	if err != nil {
		return configNotChanged, err
	}

	if mainChanged {
		if err := WriteFile(configPath, mainConfig); err != nil {
			return configNotChanged, err
		}
	}

	if backendChanged {
		if err := WriteFile(backendConfigPath, backendConfig); err != nil {
			return configNotChanged, err
		}
	}

	if mainChanged {
		return mainConfigChanged, nil
	}

	if backendChanged {
		return backendConfigChanged, nil
	}

	return configNotChanged, nil
}
