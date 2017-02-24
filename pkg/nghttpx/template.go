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
	"regexp"
	"text/template"

	"github.com/golang/glog"
)

var (
	camelRegexp = regexp.MustCompile("[0-9A-Za-z]+")

	funcMap = template.FuncMap{
		"empty": func(input interface{}) bool {
			check, ok := input.(string)
			if ok {
				return len(check) == 0
			}

			return true
		},
	}
)

func (ngx *Manager) loadTemplate() {
	ngx.template = template.Must(template.New("nghttpx.tmpl").Funcs(funcMap).ParseFiles("./nghttpx.tmpl"))
	ngx.backendTemplate = template.Must(template.New("nghttpx-backend.tmpl").Funcs(funcMap).ParseFiles("./nghttpx-backend.tmpl"))
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
func (ngx *Manager) generateCfg(ingConfig *IngressConfig) ([]byte, []byte, error) {
	mainConfigBuffer := new(bytes.Buffer)
	if err := ngx.template.Execute(mainConfigBuffer, ingConfig); err != nil {
		glog.Infof("nghttpx error while executing main configuration template: %v", err)
		return nil, nil, err
	}

	backendConfigBuffer := new(bytes.Buffer)
	if err := ngx.backendTemplate.Execute(backendConfigBuffer, ingConfig); err != nil {
		glog.Infof("nghttpx error while executing backend configuration template: %v", err)
		return nil, nil, err
	}

	return mainConfigBuffer.Bytes(), backendConfigBuffer.Bytes(), nil
}

func (ngx *Manager) checkAndWriteCfg(mainConfig, backendConfig []byte) (int, error) {
	// If main configuration has changed, we need to reload nghttpx
	mainChanged, err := needsReload(ngx.ConfigFile, mainConfig)
	if err != nil {
		return configNotChanged, err
	}

	// If backend configuration has changed, we need to issue
	// backend replace API to nghttpx
	backendChanged, err := needsReload(ngx.BackendConfigFile, backendConfig)
	if err != nil {
		return configNotChanged, err
	}

	if mainChanged {
		if err := writeFile(ngx.ConfigFile, mainConfig); err != nil {
			return configNotChanged, err
		}
	}

	if backendChanged {
		if err := writeFile(ngx.BackendConfigFile, backendConfig); err != nil {
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
