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
	"context"
	_ "embed"
	"encoding/hex"
	"fmt"
	"text/template"
	"time"

	"k8s.io/klog/v2"
)

var (
	funcMap = template.FuncMap{
		"duration": func(d time.Duration) string {
			return nghttpxDuration(d)
		},
		"encodeHex": func(b []byte) string {
			return hex.EncodeToString(b)
		},
	}
)

var (
	//go:embed nghttpx.tmpl
	nghttpxTmpl string
	//go:embed nghttpx-backend.tmpl
	nghttpxBackendTmpl string
	//go:embed default.tmpl
	defaultTmpl string
)

func (lb *LoadBalancer) loadTemplate() {
	lb.template = template.Must(template.New("nghttpx.tmpl").Funcs(funcMap).Parse(nghttpxTmpl))
	lb.backendTemplate = template.Must(template.New("nghttpx-backend.tmpl").Funcs(funcMap).Parse(nghttpxBackendTmpl))
}

// configStatus is the status of configuration files.
type configStatus int

const (
	// configuration has not changed
	configNotChanged configStatus = iota
	// main configuration has changed
	mainConfigChanged
	// only backend configuration has changed
	backendConfigChanged
)

// generateCfg generates nghttpx's main and backend configurations.
func (lb *LoadBalancer) generateCfg(ctx context.Context, ingConfig *IngressConfig) ([]byte, []byte, error) {
	log := klog.FromContext(ctx)

	mainConfigBuffer := new(bytes.Buffer)
	if err := lb.template.Execute(mainConfigBuffer, ingConfig); err != nil {
		log.Error(err, "nghttpx error while executing main configuration template")
		return nil, nil, err
	}

	backendConfigBuffer := new(bytes.Buffer)
	if err := lb.backendTemplate.Execute(backendConfigBuffer, ingConfig); err != nil {
		log.Error(err, "nghttpx error while executing backend configuration template")
		return nil, nil, err
	}

	return mainConfigBuffer.Bytes(), backendConfigBuffer.Bytes(), nil
}

func (lb *LoadBalancer) checkAndWriteCfg(ctx context.Context, ingConfig *IngressConfig, mainConfig, backendConfig []byte) (configStatus, error) {
	configPath := ConfigPath(ingConfig.ConfDir)
	backendConfigPath := BackendConfigPath(ingConfig.ConfDir)

	if err := MkdirAll(ingConfig.ConfDir); err != nil {
		return configNotChanged, err
	}

	// If main configuration has changed, we need to reload nghttpx
	mainChanged, err := needsReload(ctx, configPath, mainConfig)
	if err != nil {
		return configNotChanged, err
	}

	// If backend configuration has changed, we need to issue
	// backend replace API to nghttpx
	backendChanged, err := needsReload(ctx, backendConfigPath, backendConfig)
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

// writeDefaultNghttpxConfig writes default configuration file for nghttpx.
func (lb *LoadBalancer) writeDefaultNghttpxConfig(nghttpxConfDir string, nghttpxHealthPort, nghttpxAPIPort int32) error {
	if err := MkdirAll(nghttpxConfDir); err != nil {
		return err
	}

	var buf bytes.Buffer
	t := template.Must(template.New("default.tmpl").Parse(defaultTmpl))
	if err := t.Execute(&buf, map[string]interface{}{
		"HealthPort": nghttpxHealthPort,
		"APIPort":    nghttpxAPIPort,
	}); err != nil {
		return fmt.Errorf("unable to not create default configuration file for nghttpx: %w", err)
	}

	if err := WriteFile(ConfigPath(nghttpxConfDir), buf.Bytes()); err != nil {
		return fmt.Errorf("unable to write default configuration file for nghttpx: %w", err)
	}

	return nil
}
