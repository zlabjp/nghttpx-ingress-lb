package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

// TestIngressAnnotationNewBackendConfigMapper verifies ingressAnnotation.NewBackendConfigMapper.
func TestIngressAnnotationNewBackendConfigMapper(t *testing.T) {
	tests := []struct {
		desc                    string
		annotationDefaultConfig string
		annotationConfig        string
		wantDefaultConfig       *nghttpx.BackendConfig
		wantConfig              nghttpx.BackendConfigMapping
	}{
		{
			desc:             "Without default config",
			annotationConfig: `{"svc": {"80": {"proto": "h2"}}}`,
			wantConfig: nghttpx.BackendConfigMapping{
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
			wantConfig: nghttpx.BackendConfigMapping{
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
			wantConfig: nghttpx.BackendConfigMapping{
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
			bcm := ann.NewBackendConfigMapper(context.Background())

			assert.Equal(t, tt.wantDefaultConfig, bcm.DefaultBackendConfig)
			assert.Equal(t, tt.wantConfig, bcm.BackendConfigMapping)
		})
	}
}

// TestIngressAnnotationNewPathConfigMapper verifies ingressAnnotation.NewPathConfigMapper.
func TestIngresssAnnotationNewPathConfigMapper(t *testing.T) {
	d120 := metav1.Duration{Duration: 120 * time.Second}
	rb := "rb"
	tests := []struct {
		desc                    string
		annotationDefaultConfig string
		annotationConfig        string
		wantDefaultConfig       *nghttpx.PathConfig
		wantConfig              nghttpx.PathConfigMapping
	}{
		{
			desc:             "Without default config",
			annotationConfig: `{"example.com/alpha": {"readTimeout": "120s"}}`,
			wantConfig: nghttpx.PathConfigMapping{
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
			wantConfig: nghttpx.PathConfigMapping{
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
			wantConfig: nghttpx.PathConfigMapping{
				"example.com/alpha": {
					ReadTimeout: &d120,
				},
			},
		},
		{
			desc:             "JSON format",
			annotationConfig: `{"example.com": {"readTimeout": "120s"}}`,
			wantConfig: nghttpx.PathConfigMapping{
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
			pcm := ann.NewPathConfigMapper(context.Background())

			assert.Equal(t, tt.wantDefaultConfig, pcm.DefaultPathConfig)
			assert.Equal(t, tt.wantConfig, pcm.PathConfigMapping)
		})
	}
}
