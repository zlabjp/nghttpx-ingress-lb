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

	"github.com/golang/glog"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

const (
	// backendConfigKey is a key to annotation for extra backend configuration.
	backendConfigKey = "ingress.zlab.co.jp/backend-config"
	// defaultBackendConfigKey is a key to annotation for default backend configuration which applies to all entries in a Ingress
	// resource.
	defaultBackendConfigKey = "ingress.zlab.co.jp/default-backend-config"
	// ingressClassKey is a key to annotation in order to run multiple Ingress controllers.
	ingressClassKey = "kubernetes.io/ingress.class"
)

type ingressAnnotation map[string]string

// getBackendConfig returns default-backend-config, and backend-config.  This function applies default-backend-config to backend-config if
// it exists.  If invalid value is found, this function replaces them with the default value (e.g., nghttpx.ProtocolH1 for proto).
func (ia ingressAnnotation) getBackendConfig() (*nghttpx.PortBackendConfig, map[string]map[string]*nghttpx.PortBackendConfig) {
	data := ia[backendConfigKey]
	// the first key specifies service name, and secondary key specifies port name.
	var config map[string]map[string]*nghttpx.PortBackendConfig
	if data != "" {
		if err := json.Unmarshal([]byte(data), &config); err != nil {
			glog.Errorf("unexpected error reading %v annotation: %v", backendConfigKey, err)
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
		glog.V(4).Infof("%v annotation not found", defaultBackendConfigKey)
		return nil, config
	}

	var defaultConfig nghttpx.PortBackendConfig
	if err := json.Unmarshal([]byte(data), &defaultConfig); err != nil {
		glog.Errorf("unexpected error reading %v annotation: %v", defaultBackendConfigKey, err)
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

// getIngressClass returns Ingress class from annotation.
func (ia ingressAnnotation) getIngressClass() string {
	return ia[ingressClassKey]
}
