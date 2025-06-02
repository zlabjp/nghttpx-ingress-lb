/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
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

// NewBackendConfigMapper returns nghttpx.BackendConfigMapper by reading default-backend-config and backend-config annotations.  This
// function applies default-backend-config to backend-config if it exists.  If invalid value is found, this function replaces them with the
// default value (e.g., nghttpx.ProtocolH1 for proto).
func (ia ingressAnnotation) NewBackendConfigMapper(ctx context.Context) *nghttpx.BackendConfigMapper {
	log := klog.FromContext(ctx)

	data := ia[backendConfigKey]
	// the first key specifies service name, and secondary key specifies port name.
	var config nghttpx.BackendConfigMapping
	if data != "" {
		if err := unmarshal(data, &config); err != nil {
			log.Error(err, "Unexpected error while reading annotation", "annotation", backendConfigKey)
			return nghttpx.NewBackendConfigMapper(nil, nil)
		}
	}

	for _, v := range config {
		for _, vv := range v {
			nghttpx.FixupBackendConfig(ctx, vv)
		}
	}

	data = ia[defaultBackendConfigKey]
	if data == "" {
		log.V(4).Info("Annotation not found", "annotation", defaultBackendConfigKey)
		return nghttpx.NewBackendConfigMapper(nil, config)
	}

	var defaultConfig nghttpx.BackendConfig
	if err := unmarshal(data, &defaultConfig); err != nil {
		log.Error(err, "Unexpected error while reading annotation", "annotation", defaultBackendConfigKey)
		return nghttpx.NewBackendConfigMapper(nil, nil)
	}

	nghttpx.FixupBackendConfig(ctx, &defaultConfig)

	for _, v := range config {
		for _, vv := range v {
			nghttpx.ApplyDefaultBackendConfig(ctx, vv, &defaultConfig)
		}
	}

	return nghttpx.NewBackendConfigMapper(&defaultConfig, config)
}

// NewPathConfigMapper returns nghttpx.PathConfigMapper by reading default-path-config and path-config annotation.  This function applies
// default-path-config to path-config if a value is missing.
func (ia ingressAnnotation) NewPathConfigMapper(ctx context.Context) *nghttpx.PathConfigMapper {
	log := klog.FromContext(ctx)

	data := ia[pathConfigKey]

	var config nghttpx.PathConfigMapping
	if data != "" {
		if err := unmarshal(data, &config); err != nil {
			log.Error(err, "Unexpected error while reading annotation", "annotation", pathConfigKey)
			return nghttpx.NewPathConfigMapper(nil, nil)
		}
	}

	config = normalizePathKey(config)

	for _, v := range config {
		nghttpx.FixupPathConfig(ctx, v)
	}

	data = ia[defaultPathConfigKey]
	if data == "" {
		log.V(4).Info("Annotation not found", "annotation", defaultPathConfigKey)
		return nghttpx.NewPathConfigMapper(nil, config)
	}

	var defaultConfig nghttpx.PathConfig
	if err := unmarshal(data, &defaultConfig); err != nil {
		log.Error(err, "Unexpected error while reading annotation", "annotation", defaultPathConfigKey)
		return nghttpx.NewPathConfigMapper(nil, nil)
	}

	nghttpx.FixupPathConfig(ctx, &defaultConfig)

	for _, v := range config {
		nghttpx.ApplyDefaultPathConfig(ctx, v, &defaultConfig)
	}

	return nghttpx.NewPathConfigMapper(&defaultConfig, config)
}

// normalizePathKey appends "/" if key does not contain "/".
func normalizePathKey(src map[string]*nghttpx.PathConfig) map[string]*nghttpx.PathConfig {
	if len(src) == 0 {
		return src
	}

	dst := make(map[string]*nghttpx.PathConfig, len(src))

	for k, v := range src {
		if !strings.Contains(k, "/") {
			dst[k+"/"] = v
		} else {
			dst[k] = v
		}
	}

	return dst
}

// unmarshal deserializes YAML or JSON data into dest.
func unmarshal(data string, dest any) error {
	return yaml.NewYAMLOrJSONDecoder(strings.NewReader(data), 4096).Decode(dest)
}
