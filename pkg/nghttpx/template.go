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
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	tmpl, _ := template.New("nghttpx.tmpl").Funcs(funcMap).ParseFiles("./nghttpx.tmpl")
	ngx.template = tmpl

	backendTmpl, _ := template.New("nghttpx-backend.tmpl").Funcs(funcMap).ParseFiles("./nghttpx-backend.tmpl")
	ngx.backendTemplate = backendTmpl
}

const (
	// configuration has not changed
	configNotChanged int = iota
	// main configuration has changed
	mainConfigChanged
	// only backend configuration has changed
	backendConfigChanged
)

func (ngx *Manager) writeCfg(cfg NghttpxConfiguration, ingressCfg IngressConfig) (int, error) {
	conf := make(map[string]interface{})
	conf["upstreams"] = ingressCfg.Upstreams
	conf["server"] = ingressCfg.Server
	conf["cfg"] = cfg

	if glog.V(3) {
		b, err := json.MarshalIndent(conf, "", "  ")
		if err != nil {
			fmt.Println("error:", err)
		}
		glog.Infof("nghttpx configuration: %v", string(b))
	}

	mainConfigBuffer := new(bytes.Buffer)
	if err := ngx.template.Execute(mainConfigBuffer, conf); err != nil {
		glog.Infof("nghttpx error while executing main configuration template: %v", err)
		return configNotChanged, err
	}

	backendConfigBuffer := new(bytes.Buffer)
	if err := ngx.backendTemplate.Execute(backendConfigBuffer, conf); err != nil {
		glog.Infof("nghttpx error while executing backend configuration template: %v", err)
		return configNotChanged, err
	}

	// If main configuration has changed, we need to reload nghttpx
	mainChanged, err := needsReload(ngx.ConfigFile, mainConfigBuffer)
	if err != nil {
		return configNotChanged, err
	}

	// If backend configuration has changed, we need to issue
	// backend replace API to nghttpx
	backendChanged, err := needsReload(ngx.BackendConfigFile, backendConfigBuffer)
	if err != nil {
		return configNotChanged, err
	}

	if mainChanged {
		return mainConfigChanged, nil
	}

	if backendChanged {
		return backendConfigChanged, nil
	}

	return configNotChanged, nil
}
