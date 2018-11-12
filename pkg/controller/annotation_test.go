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
		// 3
		{
			annotationConfig: `
svc:
  80:
    proto: h2
`,
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

// TestGetPathConfig verifies getPathConfig.
func TestGetPathConfig(t *testing.T) {
	d120 := metav1.Duration{120 * time.Second}
	rb := "rb"
	tests := []struct {
		annotationDefaultConfig string
		annotationConfig        string
		wantDefaultConfig       *nghttpx.PathConfig
		wantConfig              map[string]*nghttpx.PathConfig
	}{
		// 0
		{
			annotationConfig: `{"example.com/alpha": {"readTimeout": "120s"}}`,
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/alpha": &nghttpx.PathConfig{
					ReadTimeout: &d120,
				},
			},
		},
		// 1
		{
			annotationDefaultConfig: `{"mruby": "rb"}`,
			annotationConfig:        `{"example.com/alpha": {"readTimeout": "120s"}}`,
			wantDefaultConfig: &nghttpx.PathConfig{
				Mruby: &rb,
			},
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/alpha": &nghttpx.PathConfig{
					ReadTimeout: &d120,
					Mruby:       &rb,
				},
			},
		},
		// 3
		{
			annotationConfig: `
example.com/alpha:
  readTimeout: 120s
`,
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/alpha": &nghttpx.PathConfig{
					ReadTimeout: &d120,
				},
			},
		},
		// 4
		{
			annotationConfig: `{"example.com": {"readTimeout": "120s"}}`,
			wantConfig: map[string]*nghttpx.PathConfig{
				"example.com/": &nghttpx.PathConfig{
					ReadTimeout: &d120,
				},
			},
		},
	}

	for i, tt := range tests {
		ann := ingressAnnotation(map[string]string{
			defaultPathConfigKey: tt.annotationDefaultConfig,
			pathConfigKey:        tt.annotationConfig,
		})
		defaultConfig, config := ann.getPathConfig()

		if !reflect.DeepEqual(defaultConfig, tt.wantDefaultConfig) {
			t.Errorf("#%v: defaultConfig = %+v, want %+v", i, defaultConfig, tt.wantDefaultConfig)
		}
		if !reflect.DeepEqual(config, tt.wantConfig) {
			t.Errorf("#%v: config = %+v, want %+v", i, config, tt.wantConfig)
		}
	}
}
