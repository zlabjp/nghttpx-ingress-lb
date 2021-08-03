/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ghodss/yaml"
	"k8s.io/klog/v2"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

const (
	// backendConfigKey is a key to annotation for extra backend configuration.
	backendConfigKey = "ingress.zlab.co.jp/backend-config"
	// defaultBackendConfigKey is a key to annotation for default backend configuration which applies to all entries in a Ingress
	// resource.
	defaultBackendConfigKey = "ingress.zlab.co.jp/default-backend-config"
	// pathConfigKey is a key to annotation for extra path configuration.
	pathConfigKey = "ingress.zlab.co.jp/path-config"
	// defaultPathConfigKey is a key to annotation for default path configuration which applies to all entries in a Ingress resource.
	defaultPathConfigKey = "ingress.zlab.co.jp/default-path-config"
)

type ingressAnnotation map[string]string

// getBackendConfig returns default-backend-config, and backend-config.  This function applies default-backend-config to backend-config if
// it exists.  If invalid value is found, this function replaces them with the default value (e.g., nghttpx.ProtocolH1 for proto).
func (ia ingressAnnotation) getBackendConfig() (*nghttpx.PortBackendConfig, map[string]map[string]*nghttpx.PortBackendConfig) {
	data := ia[backendConfigKey]
	// the first key specifies service name, and secondary key specifies port name.
	var config map[string]map[string]*nghttpx.PortBackendConfig
	if data != "" {
		if err := unmarshal([]byte(data), &config); err != nil {
			klog.Errorf("unexpected error reading %v annotation: %v", backendConfigKey, err)
			return nil, nil
		}
	}

	for _, v := range config {
		for _, vv := range v {
			nghttpx.FixupPortBackendConfig(vv)
		}
	}

	data = ia[defaultBackendConfigKey]
	if data == "" {
		klog.V(4).Infof("%v annotation not found", defaultBackendConfigKey)
		return nil, config
	}

	var defaultConfig nghttpx.PortBackendConfig
	if err := unmarshal([]byte(data), &defaultConfig); err != nil {
		klog.Errorf("unexpected error reading %v annotation: %v", defaultBackendConfigKey, err)
		return nil, nil
	}
	nghttpx.FixupPortBackendConfig(&defaultConfig)

	for _, v := range config {
		for _, vv := range v {
			nghttpx.ApplyDefaultPortBackendConfig(vv, &defaultConfig)
		}
	}

	return &defaultConfig, config
}

// getPathConfig returns default-path-config and path-config.  This function applies default-path-config to path-config if a value is
// missing.
func (ia ingressAnnotation) getPathConfig() (*nghttpx.PathConfig, map[string]*nghttpx.PathConfig) {
	data := ia[pathConfigKey]
	var config map[string]*nghttpx.PathConfig
	if data != "" {
		if err := unmarshal([]byte(data), &config); err != nil {
			klog.Errorf("unexpected error reading %v annotation: %v", pathConfigKey, err)
			return nil, nil
		}
	}

	config = normalizePathKey(config)

	for _, v := range config {
		nghttpx.FixupPathConfig(v)
	}

	data = ia[defaultPathConfigKey]
	if data == "" {
		klog.V(4).Infof("%v annotation not found", defaultPathConfigKey)
		return nil, config
	}

	var defaultConfig nghttpx.PathConfig
	if err := unmarshal([]byte(data), &defaultConfig); err != nil {
		klog.Errorf("unexpected error reading %v annotation: %v", defaultPathConfigKey, err)
		return nil, nil
	}
	nghttpx.FixupPathConfig(&defaultConfig)

	for _, v := range config {
		nghttpx.ApplyDefaultPathConfig(v, &defaultConfig)
	}

	return &defaultConfig, config
}

// normalizePathKey appends "/" if key does not contain "/".
func normalizePathKey(src map[string]*nghttpx.PathConfig) map[string]*nghttpx.PathConfig {
	if len(src) == 0 {
		return src
	}

	dst := make(map[string]*nghttpx.PathConfig)
	for k, v := range src {
		if !strings.Contains(k, "/") {
			dst[k+"/"] = v
		} else {
			dst[k] = v
		}
	}

	return dst
}

// unmarshal deserializes data into dest.  This function first tries YAML and then JSON.
func unmarshal(data []byte, dest interface{}) error {
	err := yaml.Unmarshal(data, dest)
	if err == nil {
		return nil
	}

	klog.Infof("Could not unmarshal YAML string; fall back to JSON: %v", err)

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("could not unmarshal JSON string: %v", err)
	}

	return nil
}
