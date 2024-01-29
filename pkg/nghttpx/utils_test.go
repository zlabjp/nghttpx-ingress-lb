/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"context"
	"testing"
	"time"

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
			FixupBackendConfig(context.Background(), c)
			if got, want := c.GetProto(), tt.outProto; got != want {
				t.Errorf("c.GetProto() = %q, want %q", got, want)
			}
			if got, want := c.GetWeight(), tt.outWeight; got != want {
				t.Errorf("c.GetWeight() = %v, want %v", got, want)
			}
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
			ApplyDefaultBackendConfig(context.Background(), a, tt.defaultConf)

			if got, want := a.GetProto(), tt.defaultConf.GetProto(); got != want {
				t.Errorf("a.GetProto() = %v, want %v", got, want)
			}
			if got, want := a.GetTLS(), tt.defaultConf.GetTLS(); got != want {
				t.Errorf("a.GetTLS() = %v, want %v", got, want)
			}
			if got, want := a.GetSNI(), tt.defaultConf.GetSNI(); got != want {
				t.Errorf("a.GetSNI() = %v, want %v", got, want)
			}
			if got, want := a.GetDNS(), tt.defaultConf.GetDNS(); got != want {
				t.Errorf("a.GetDNS() = %v, want %v", got, want)
			}
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
			FixupPathConfig(context.Background(), c)
			if got, want := c.GetAffinity(), tt.outAffinity; got != want {
				t.Errorf("c.GetAffinity() = %q, want %q", got, want)
			}
			if got, want := c.GetAffinityCookieSecure(), tt.outAffinityCookieSecure; got != want {
				t.Errorf("c.GetAffinityCookieSecure() = %q, want %q", got, want)
			}
			if got, want := c.GetAffinityCookieStickiness(), tt.outAffinityCookieStickiness; got != want {
				t.Errorf("c.GetAffinityCookieStickiness() = %q, want %q", got, want)
			}
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
			ApplyDefaultPathConfig(context.Background(), a, tt.defaultConf)

			if got, want := a.GetMruby(), tt.defaultConf.GetMruby(); got != want {
				t.Errorf("a.GetMruby() = %v, want %v", got, want)
			}
			if got, want := a.GetAffinity(), tt.defaultConf.GetAffinity(); got != want {
				t.Errorf("a.GetAffinity() = %v, want %v", got, want)
			}
			if got, want := a.GetAffinityCookieName(), tt.defaultConf.GetAffinityCookieName(); got != want {
				t.Errorf("a.GetAffinityCookieName() = %v, want %v", got, want)
			}
			if got, want := a.GetAffinityCookiePath(), tt.defaultConf.GetAffinityCookiePath(); got != want {
				t.Errorf("a.GetAffinityCookiePath() = %v, want %v", got, want)
			}
			if got, want := a.GetAffinityCookieSecure(), tt.defaultConf.GetAffinityCookieSecure(); got != want {
				t.Errorf("a.GetAffinityCookieSecure() = %v, want %v", got, want)
			}
			if got, want := a.GetAffinityCookieStickiness(), tt.defaultConf.GetAffinityCookieStickiness(); got != want {
				t.Errorf("a.GetAffinityCookieStickiness() = %v, want %v", got, want)
			}
			if got, want := a.GetReadTimeout(), tt.defaultConf.GetReadTimeout(); !((got != nil && want != nil && *got == *want) || (got == nil && want == nil)) {
				t.Errorf("a.GetReadTimeout() = %v, want %v", got, want)
			}
			if got, want := a.GetWriteTimeout(), tt.defaultConf.GetWriteTimeout(); !((got != nil && want != nil && *got == *want) || (got == nil && want == nil)) {
				t.Errorf("a.GetWriteTimeout() = %v, want %v", got, want)
			}
			if got, want := a.GetRedirectIfNotTLS(), tt.defaultConf.GetRedirectIfNotTLS(); got != want {
				t.Errorf("a.GetRedirectIfNotTLS() = %v, want %v", got, want)
			}
			if got, want := a.GetDoNotForward(), tt.defaultConf.GetDoNotForward(); got != want {
				t.Errorf("a.GetDoNotForward() = %v, want %v", got, want)
			}
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
			d:    0,
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
			if got, want := nghttpxDuration(tt.d), tt.want; got != want {
				t.Errorf("nghttpxDuration(%v) = %v, want %v", tt.d, got, want)
			}
		})
	}
}
