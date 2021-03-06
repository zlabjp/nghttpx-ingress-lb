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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestFixupPortBackendConfig validates fixupPortBackendConfig corrects invalid input to the correct default value.
func TestFixupPortBackendConfig(t *testing.T) {
	tests := []struct {
		desc                    string
		inProto                 Protocol
		inAffinity              Affinity
		inAffinityCookieSecure  AffinityCookieSecure
		inWeight                uint32
		outProto                Protocol
		outAffinity             Affinity
		outAffinityCookieSecure AffinityCookieSecure
		outWeight               uint32
	}{
		{
			desc:                    "Fixup incorrect input",
			inProto:                 "foo",
			inAffinity:              "bar",
			inAffinityCookieSecure:  "buzz",
			inWeight:                0,
			outProto:                ProtocolH1,
			outAffinity:             AffinityNone,
			outAffinityCookieSecure: AffinityCookieSecureAuto,
			outWeight:               1,
		},
		{
			desc: "Empty input leaves as is.",
		},
		{
			desc:                    "Correct input must be left unchanged.",
			inProto:                 ProtocolH2,
			inAffinity:              AffinityIP,
			inAffinityCookieSecure:  AffinityCookieSecureYes,
			inWeight:                256,
			outProto:                ProtocolH2,
			outAffinity:             AffinityIP,
			outAffinityCookieSecure: AffinityCookieSecureYes,
			outWeight:               256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := &PortBackendConfig{}
			c.SetProto(tt.inProto)
			c.SetAffinity(tt.inAffinity)
			c.SetAffinityCookieSecure(tt.inAffinityCookieSecure)
			FixupPortBackendConfig(c)
			if got, want := c.GetProto(), tt.outProto; got != want {
				t.Errorf("c.GetProto() = %q, want %q", got, want)
			}
			if got, want := c.GetAffinity(), tt.outAffinity; got != want {
				t.Errorf("c.GetAffinity() = %q, want %q", got, want)
			}
			if got, want := c.GetAffinityCookieSecure(), tt.outAffinityCookieSecure; got != want {
				t.Errorf("c.GetAffinityCookieSecure() = %q, want %q", got, want)
			}
		})
	}
}

// TestApplyDefaultPortBackendConfig verifies ApplyDefaultPortBackendConfig.
func TestApplyDefaultPortBackendConfig(t *testing.T) {
	tests := []struct {
		desc        string
		defaultConf *PortBackendConfig
	}{
		{
			desc: "Enable HTTP/2",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetProto(ProtocolH2)
				return a
			}(),
		},
		{
			desc: "Enable TLS",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetTLS(true)
				return a
			}(),
		},
		{
			desc: "Specify SNI",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetSNI("example.com")
				return a
			}(),
		},
		{
			desc: "Enable DNS",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetDNS(true)
				return a
			}(),
		},
		{
			desc: "Enable IP based affinity",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinity(AffinityIP)
				return a
			}(),
		},
		{
			desc: "Set name of affinity cookie",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinityCookieName("lb-cookie")
				return a
			}(),
		},
		{
			desc: "Set path of affinity cookie",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinityCookiePath("/path")
				return a
			}(),
		},
		{
			desc: "Set secure attribute of affinity cookie",
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinityCookieSecure(AffinityCookieSecureNo)
				return a
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			a := &PortBackendConfig{}
			ApplyDefaultPortBackendConfig(a, tt.defaultConf)

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
		})
	}
}

// TestFixupPathConfig validates that FixupPathConfig corrects invalid input to the correct default value.
func TestFixupPathConfig(t *testing.T) {
	tests := []struct {
		desc                    string
		inAffinity              Affinity
		inAffinityCookieSecure  AffinityCookieSecure
		outAffinity             Affinity
		outAffinityCookieSecure AffinityCookieSecure
	}{
		{
			desc:                    "Fixup incorrect input",
			inAffinity:              "bar",
			inAffinityCookieSecure:  "buzz",
			outAffinity:             AffinityNone,
			outAffinityCookieSecure: AffinityCookieSecureAuto,
		},
		{
			desc: "Empty input leaves as is.",
		},
		{
			desc:                    "Correct input must be left unchanged.",
			inAffinity:              AffinityIP,
			inAffinityCookieSecure:  AffinityCookieSecureYes,
			outAffinity:             AffinityIP,
			outAffinityCookieSecure: AffinityCookieSecureYes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := &PathConfig{}
			c.SetAffinity(tt.inAffinity)
			c.SetAffinityCookieSecure(tt.inAffinityCookieSecure)
			FixupPathConfig(c)
			if got, want := c.GetAffinity(), tt.outAffinity; got != want {
				t.Errorf("c.GetAffinity() = %q, want %q", got, want)
			}
			if got, want := c.GetAffinityCookieSecure(), tt.outAffinityCookieSecure; got != want {
				t.Errorf("c.GetAffinityCookieSecure() = %q, want %q", got, want)
			}
		})
	}
}

// TestApplyDefaultPortBackendConfig verifies ApplyDefaultPathConfig.
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
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			a := &PathConfig{}
			ApplyDefaultPathConfig(a, tt.defaultConf)

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
			if got, want := a.GetReadTimeout(), tt.defaultConf.GetReadTimeout(); !((got != nil && want != nil && *got == *want) || (got == nil && want == nil)) {
				t.Errorf("a.GetReadTimeout() = %v, want %v", got, want)
			}
			if got, want := a.GetWriteTimeout(), tt.defaultConf.GetWriteTimeout(); !((got != nil && want != nil && *got == *want) || (got == nil && want == nil)) {
				t.Errorf("a.GetWriteTimeout() = %v, want %v", got, want)
			}
			if got, want := a.GetRedirectIfNotTLS(), tt.defaultConf.GetRedirectIfNotTLS(); got != want {
				t.Errorf("a.GetRedirectIfNotTLS() = %v, want %v", got, want)
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
