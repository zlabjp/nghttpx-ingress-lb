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
	// ingressClassKey is a key to annotation in order to run multiple Ingress controllers.
	ingressClassKey = "kubernetes.io/ingress.class"
)

type ingressAnnotation map[string]string

func (ia ingressAnnotation) getBackendConfig() map[string]map[string]nghttpx.PortBackendConfig {
	data := ia[backendConfigKey]
	// the first key specifies service name, and secondary key specifies port name.
	var config map[string]map[string]nghttpx.PortBackendConfig
	if data == "" {
		return config
	}
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		glog.Errorf("unexpected error reading %v annotation: %v", backendConfigKey, err)
		return config
	}

	return config
}

// getIngressClass returns Ingress class from annotation.
func (ia ingressAnnotation) getIngressClass() string {
	return ia[ingressClassKey]
}
