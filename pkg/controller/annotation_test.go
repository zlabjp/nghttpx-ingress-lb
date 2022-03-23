package controller

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

// TestGetBackendConfig verifies getBackendConfig.
func TestGetBackendConfig(t *testing.T) {
	tests := []struct {
		desc                    string
		annotationDefaultConfig string
		annotationConfig        string
		wantDefaultConfig       *nghttpx.BackendConfig
		wantConfig              map[string]map[string]*nghttpx.BackendConfig
	}{
		{
			desc:             "Without default config",
			annotationConfig: `{"svc": {"80": {"proto": "h2"}}}`,
			wantConfig: map[string]map[string]*nghttpx.BackendConfig{
				"svc": {
					"80": func() *nghttpx.BackendConfig {
						a := &nghttpx.BackendConfig{}
						a.SetProto(nghttpx.ProtocolH2)
						return a
					}(),
				},
			},
		},
		{
			desc:                    "Just default config",
			annotationDefaultConfig: `{"sni": "example.com"}`,
			wantDefaultConfig: func() *nghttpx.BackendConfig {
				a := &nghttpx.BackendConfig{}
				a.SetSNI("example.com")
				return a
			}(),
		},
		{
			desc:                    "Both config and default config",
			annotationDefaultConfig: `{"sni": "example.com"}`,
			annotationConfig:        `{"svc": {"80": {"proto": "h2"}}}`,
			wantDefaultConfig: func() *nghttpx.BackendConfig {
				a := &nghttpx.BackendConfig{}
				a.SetSNI("example.com")
				return a
			}(),
			wantConfig: map[string]map[string]*nghttpx.BackendConfig{
				"svc": {
					"80": func() *nghttpx.BackendConfig {
						a := &nghttpx.BackendConfig{}
						a.SetProto(nghttpx.ProtocolH2)
						a.SetSNI("example.com")
						return a
					}(),
				},
			},
		},
		{
			desc: "YAML format",
			annotationConfig: `
svc:
  80:
    proto: h2
`,
			wantConfig: map[string]map[string]*nghttpx.BackendConfig{
				"svc": {
					"80": func() *nghttpx.BackendConfig {
						a := &nghttpx.BackendConfig{}
						a.SetProto(nghttpx.ProtocolH2)
						return a
					}(),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ann := ingressAnnotation(map[string]string{
				defaultBackendConfigKey: tt.annotationDefaultConfig,
				backendConfigKey:        tt.annotationConfig,
			})
			defaultConfig, config := ann.getBackendConfig()

			if !reflect.DeepEqual(defaultConfig, tt.wantDefaultConfig) {
				t.Errorf("defaultConfig = %+v, want %+v", defaultConfig, tt.wantDefaultConfig)
			}
			if !reflect.DeepEqual(config, tt.wantConfig) {
				t.Errorf("config = %+v, want %+v", config, tt.wantConfig)
			}
		})
	}
}

// TestGetPathConfig verifies getPathConfig.
func TestGetPathConfig(t *testing.T) {
	d120 := metav1.Duration{Duration: 120 * time.Second}
	rb := "rb"
	tests := []struct {
		desc                    string
		annotationDefaultConfig string
		annotationConfig        string
		wantDefaultConfig       *nghttpx.PathConfig
		wantConfig              map[string]*nghttpx.PathConfig
	}{
		{
			desc:             "Without default config",
			annotationConfig: `{"example.com/alpha": {"readTimeout": "120s"}}`,
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/alpha": {
					ReadTimeout: &d120,
				},
			},
		},
		{
			desc:                    "Both config and default config",
			annotationDefaultConfig: `{"mruby": "rb"}`,
			annotationConfig:        `{"example.com/alpha": {"readTimeout": "120s"}}`,
			wantDefaultConfig: &nghttpx.PathConfig{
				Mruby: &rb,
			},
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/alpha": {
					ReadTimeout: &d120,
					Mruby:       &rb,
				},
			},
		},
		{
			desc: "YAML format",
			annotationConfig: `
example.com/alpha:
  readTimeout: 120s
`,
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/alpha": {
					ReadTimeout: &d120,
				},
			},
		},
		{
			desc:             "JSON format",
			annotationConfig: `{"example.com": {"readTimeout": "120s"}}`,
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/": {
					ReadTimeout: &d120,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ann := ingressAnnotation(map[string]string{
				defaultPathConfigKey: tt.annotationDefaultConfig,
				pathConfigKey:        tt.annotationConfig,
			})
			defaultConfig, config := ann.getPathConfig()

			if !reflect.DeepEqual(defaultConfig, tt.wantDefaultConfig) {
				t.Errorf("defaultConfig = %+v, want %+v", defaultConfig, tt.wantDefaultConfig)
			}
			if !reflect.DeepEqual(config, tt.wantConfig) {
				t.Errorf("config = %+v, want %+v", config, tt.wantConfig)
			}
		})
	}
}
