package controller

import (
	"reflect"
	"testing"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

// TestGetBackendConfig verifies getBackendConfig.
func TestGetBackendConfig(t *testing.T) {
	tests := []struct {
		annotationDefaultConfig string
		annotationConfig        string
		wantDefaultConfig       *nghttpx.PortBackendConfig
		wantConfig              map[string]map[string]*nghttpx.PortBackendConfig
	}{
		// 0
		{
			annotationConfig: `{"svc": {"80": {"proto": "h2"}}}`,
			wantConfig: map[string]map[string]*nghttpx.PortBackendConfig{
				"svc": {
					"80": func() *nghttpx.PortBackendConfig {
						a := &nghttpx.PortBackendConfig{}
						a.SetProto(nghttpx.ProtocolH2)
						return a
					}(),
				},
			},
		},
		// 1
		{
			annotationDefaultConfig: `{"affinity": "ip"}`,
			wantDefaultConfig: func() *nghttpx.PortBackendConfig {
				a := &nghttpx.PortBackendConfig{}
				a.SetAffinity(nghttpx.AffinityIP)
				return a
			}(),
		},
		// 2
		{
			annotationDefaultConfig: `{"affinity": "ip"}`,
			annotationConfig:        `{"svc": {"80": {"proto": "h2"}}}`,
			wantDefaultConfig: func() *nghttpx.PortBackendConfig {
				a := &nghttpx.PortBackendConfig{}
				a.SetAffinity(nghttpx.AffinityIP)
				return a
			}(),
			wantConfig: map[string]map[string]*nghttpx.PortBackendConfig{
				"svc": {
					"80": func() *nghttpx.PortBackendConfig {
						a := &nghttpx.PortBackendConfig{}
						a.SetProto(nghttpx.ProtocolH2)
						a.SetAffinity(nghttpx.AffinityIP)
						return a
					}(),
				},
			},
		},
	}

	for i, tt := range tests {
		ann := ingressAnnotation(map[string]string{
			defaultBackendConfigKey: tt.annotationDefaultConfig,
			backendConfigKey:        tt.annotationConfig,
		})
		defaultConfig, config := ann.getBackendConfig()

		if !reflect.DeepEqual(defaultConfig, tt.wantDefaultConfig) {
			t.Errorf("#%v: defaultConfig = %+v, want %+v", i, defaultConfig, tt.wantDefaultConfig)
		}
		if !reflect.DeepEqual(config, tt.wantConfig) {
			t.Errorf("#%v: config = %+v, want %+v", i, config, tt.wantConfig)
		}
	}
}
