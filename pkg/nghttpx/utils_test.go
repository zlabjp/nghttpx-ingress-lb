/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestFixupBackendConfig validates fixupBackendConfig corrects invalid input to the correct default value.
func TestFixupBackendConfig(t *testing.T) {
	tests := []struct {
		desc      string
		inProto   Protocol
		inWeight  uint32
		outProto  Protocol
		outWeight uint32
	}{
		{
			desc:      "Fixup incorrect input",
			inProto:   "foo",
			inWeight:  257,
			outProto:  ProtocolH1,
			outWeight: 256,
		},
		{
			desc:      "Empty input",
			outWeight: 1,
			outProto:  ProtocolH1,
		},
		{
			desc:      "Correct input must be left unchanged.",
			inProto:   ProtocolH2,
			inWeight:  256,
			outProto:  ProtocolH2,
			outWeight: 256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := &BackendConfig{}
			c.SetProto(tt.inProto)
			c.SetWeight(tt.inWeight)
			FixupBackendConfig(t.Context(), c)

			assert.Equal(t, tt.outProto, c.GetProto())
			assert.Equal(t, tt.outWeight, c.GetWeight())
		})
	}
}

// TestApplyDefaultBackendConfig verifies ApplyDefaultBackendConfig.
func TestApplyDefaultBackendConfig(t *testing.T) {
	tests := []struct {
		desc        string
		defaultConf *BackendConfig
	}{
		{
			desc: "Enable HTTP/2",
			defaultConf: func() *BackendConfig {
				a := &BackendConfig{}
				a.SetProto(ProtocolH2)

				return a
			}(),
		},
		{
			desc: "Enable TLS",
			defaultConf: func() *BackendConfig {
				a := &BackendConfig{}
				a.SetTLS(true)

				return a
			}(),
		},
		{
			desc: "Specify SNI",
			defaultConf: func() *BackendConfig {
				a := &BackendConfig{}
				a.SetSNI("example.com")

				return a
			}(),
		},
		{
			desc: "Enable DNS",
			defaultConf: func() *BackendConfig {
				a := &BackendConfig{}
				a.SetDNS(true)

				return a
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			a := &BackendConfig{}
			ApplyDefaultBackendConfig(t.Context(), a, tt.defaultConf)

			assert.Equal(t, tt.defaultConf.GetProto(), a.GetProto())
			assert.Equal(t, tt.defaultConf.GetTLS(), a.GetTLS())
			assert.Equal(t, tt.defaultConf.GetSNI(), a.GetSNI())
			assert.Equal(t, tt.defaultConf.GetDNS(), a.GetDNS())
		})
	}
}

// TestFixupPathConfig validates that FixupPathConfig corrects invalid input to the correct default value.
func TestFixupPathConfig(t *testing.T) {
	tests := []struct {
		desc                        string
		inAffinity                  Affinity
		inAffinityCookieSecure      AffinityCookieSecure
		inAffinityCookieStickiness  AffinityCookieStickiness
		outAffinity                 Affinity
		outAffinityCookieSecure     AffinityCookieSecure
		outAffinityCookieStickiness AffinityCookieStickiness
	}{
		{
			desc:                        "Fixup incorrect input",
			inAffinity:                  "bar",
			inAffinityCookieSecure:      "buzz",
			inAffinityCookieStickiness:  "foo",
			outAffinity:                 AffinityNone,
			outAffinityCookieSecure:     AffinityCookieSecureAuto,
			outAffinityCookieStickiness: AffinityCookieStickinessLoose,
		},
		{
			desc:                        "Empty input",
			outAffinity:                 AffinityNone,
			outAffinityCookieSecure:     AffinityCookieSecureAuto,
			outAffinityCookieStickiness: AffinityCookieStickinessLoose,
		},
		{
			desc:                        "Correct input must be left unchanged.",
			inAffinity:                  AffinityIP,
			inAffinityCookieSecure:      AffinityCookieSecureYes,
			inAffinityCookieStickiness:  AffinityCookieStickinessStrict,
			outAffinity:                 AffinityIP,
			outAffinityCookieSecure:     AffinityCookieSecureYes,
			outAffinityCookieStickiness: AffinityCookieStickinessStrict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := &PathConfig{}
			c.SetAffinity(tt.inAffinity)
			c.SetAffinityCookieSecure(tt.inAffinityCookieSecure)
			c.SetAffinityCookieStickiness(tt.inAffinityCookieStickiness)
			FixupPathConfig(t.Context(), c)

			assert.Equal(t, tt.outAffinity, c.GetAffinity())
			assert.Equal(t, tt.outAffinityCookieSecure, c.GetAffinityCookieSecure())
			assert.Equal(t, tt.outAffinityCookieStickiness, c.GetAffinityCookieStickiness())
		})
	}
}

// TestApplyDefaultBackendConfig verifies ApplyDefaultPathConfig.
func TestApplyDefaultPathConfig(t *testing.T) {
	tests := []struct {
		desc        string
		defaultConf *PathConfig
	}{
		{
			desc: "Specify mruby",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetMruby("hello mruby")

				return a
			}(),
		},
		{
			desc: "Specify IP based affinity",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinity(AffinityIP)

				return a
			}(),
		},
		{
			desc: "Set name of affinity cookie",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookieName("lb-cookie")

				return a
			}(),
		},
		{
			desc: "Set path of affinity cookie",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookiePath("/path")

				return a
			}(),
		},
		{
			desc: "Set secure attribute of affinity cookie",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookieSecure(AffinityCookieSecureNo)

				return a
			}(),
		},
		{
			desc: "Set affinity cookie stickiness",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookieStickiness(AffinityCookieStickinessStrict)

				return a
			}(),
		},
		{
			desc: "Set read timeout",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetReadTimeout(metav1.Duration{Duration: 5 * time.Minute})

				return a
			}(),
		},
		{
			desc: "Set write timeout",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetWriteTimeout(metav1.Duration{Duration: 10 * time.Second})

				return a
			}(),
		},
		{
			desc: "Set redirect if not TLS",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetRedirectIfNotTLS(false)

				return a
			}(),
		},
		{
			desc: "Set do not forward",
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetDoNotForward(true)

				return a
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			a := &PathConfig{}
			ApplyDefaultPathConfig(t.Context(), a, tt.defaultConf)

			assert.Equal(t, tt.defaultConf.GetMruby(), a.GetMruby())
			assert.Equal(t, tt.defaultConf.GetAffinity(), a.GetAffinity())
			assert.Equal(t, tt.defaultConf.GetAffinityCookieName(), a.GetAffinityCookieName())
			assert.Equal(t, tt.defaultConf.GetAffinityCookiePath(), a.GetAffinityCookiePath())
			assert.Equal(t, tt.defaultConf.GetAffinityCookieSecure(), a.GetAffinityCookieSecure())
			assert.Equal(t, tt.defaultConf.GetAffinityCookieStickiness(), a.GetAffinityCookieStickiness())
			assert.Equal(t, tt.defaultConf.GetReadTimeout(), a.GetReadTimeout())
			assert.Equal(t, tt.defaultConf.GetWriteTimeout(), a.GetWriteTimeout())
			assert.Equal(t, tt.defaultConf.GetRedirectIfNotTLS(), a.GetRedirectIfNotTLS())
			assert.Equal(t, tt.defaultConf.GetDoNotForward(), a.GetDoNotForward())
		})
	}
}

// TestNghttpxDuration verifies nghttpxDuration.
func TestNghttpxDuration(t *testing.T) {
	tests := []struct {
		desc string
		d    time.Duration
		want string
	}{
		{
			desc: "Zero",
			want: "0",
		},
		{
			desc: "Maximum value less than a second in milliseconds",
			d:    999 * time.Millisecond,
			want: "999ms",
		},
		{
			desc: "Seconds do not have a unit",
			d:    time.Second,
			want: "1",
		},
		{
			desc: "Use ms if it has fraction of seconds even if it is greater than a second",
			d:    1001 * time.Millisecond,
			want: "1001ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, nghttpxDuration(tt.d))
		})
	}
}
