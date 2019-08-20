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
		inProto                 Protocol
		inAffinity              Affinity
		inAffinityCookieSecure  AffinityCookieSecure
		inWeight                uint32
		outProto                Protocol
		outAffinity             Affinity
		outAffinityCookieSecure AffinityCookieSecure
		outWeight               uint32
	}{
		// 0
		{
			inProto:                 "foo",
			inAffinity:              "bar",
			inAffinityCookieSecure:  "buzz",
			inWeight:                0,
			outProto:                ProtocolH1,
			outAffinity:             AffinityNone,
			outAffinityCookieSecure: AffinityCookieSecureAuto,
			outWeight:               1,
		},
		// 1
		{
			// Empty input leaves as is.
		},
		// 2
		{
			// Correct input must be left unchanged.
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

	for i, tt := range tests {
		c := &PortBackendConfig{}
		c.SetProto(tt.inProto)
		c.SetAffinity(tt.inAffinity)
		c.SetAffinityCookieSecure(tt.inAffinityCookieSecure)
		FixupPortBackendConfig(c)
		if got, want := c.GetProto(), tt.outProto; got != want {
			t.Errorf("#%v: c.GetProto() = %q, want %q", i, got, want)
		}
		if got, want := c.GetAffinity(), tt.outAffinity; got != want {
			t.Errorf("#%v: c.GetAffinity() = %q, want %q", i, got, want)
		}
		if got, want := c.GetAffinityCookieSecure(), tt.outAffinityCookieSecure; got != want {
			t.Errorf("#%v: c.GetAffinityCookieSecure() = %q, want %q", i, got, want)
		}
	}
}

// TestApplyDefaultPortBackendConfig verifies ApplyDefaultPortBackendConfig.
func TestApplyDefaultPortBackendConfig(t *testing.T) {
	tests := []struct {
		defaultConf *PortBackendConfig
	}{
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetProto(ProtocolH2)
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetTLS(true)
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetSNI("example.com")
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetDNS(true)
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinity(AffinityIP)
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinityCookieName("lb-cookie")
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinityCookiePath("/path")
				return a
			}(),
		},
		{
			defaultConf: func() *PortBackendConfig {
				a := &PortBackendConfig{}
				a.SetAffinityCookieSecure(AffinityCookieSecureNo)
				return a
			}(),
		},
	}

	for i, tt := range tests {
		a := &PortBackendConfig{}
		ApplyDefaultPortBackendConfig(a, tt.defaultConf)

		if got, want := a.GetProto(), tt.defaultConf.GetProto(); got != want {
			t.Errorf("#%v: a.GetProto() = %v, want %v", i, got, want)
		}
		if got, want := a.GetTLS(), tt.defaultConf.GetTLS(); got != want {
			t.Errorf("#%v: a.GetTLS() = %v, want %v", i, got, want)
		}
		if got, want := a.GetSNI(), tt.defaultConf.GetSNI(); got != want {
			t.Errorf("#%v: a.GetSNI() = %v, want %v", i, got, want)
		}
		if got, want := a.GetDNS(), tt.defaultConf.GetDNS(); got != want {
			t.Errorf("#%v: a.GetDNS() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinity(), tt.defaultConf.GetAffinity(); got != want {
			t.Errorf("#%v: a.GetAffinity() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinityCookieName(), tt.defaultConf.GetAffinityCookieName(); got != want {
			t.Errorf("#%v: a.GetAffinityCookieName() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinityCookiePath(), tt.defaultConf.GetAffinityCookiePath(); got != want {
			t.Errorf("#%v: a.GetAffinityCookiePath() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinityCookieSecure(), tt.defaultConf.GetAffinityCookieSecure(); got != want {
			t.Errorf("#%v: a.GetAffinityCookieSecure() = %v, want %v", i, got, want)
		}
	}
}

// TestFixupPathConfig validates that FixupPathConfig corrects invalid input to the correct default value.
func TestFixupPathConfig(t *testing.T) {
	tests := []struct {
		inAffinity              Affinity
		inAffinityCookieSecure  AffinityCookieSecure
		outAffinity             Affinity
		outAffinityCookieSecure AffinityCookieSecure
	}{
		// 0
		{
			inAffinity:              "bar",
			inAffinityCookieSecure:  "buzz",
			outAffinity:             AffinityNone,
			outAffinityCookieSecure: AffinityCookieSecureAuto,
		},
		// 1
		{
			// Empty input leaves as is.
		},
		// 2
		{
			// Correct input must be left unchanged.
			inAffinity:              AffinityIP,
			inAffinityCookieSecure:  AffinityCookieSecureYes,
			outAffinity:             AffinityIP,
			outAffinityCookieSecure: AffinityCookieSecureYes,
		},
	}

	for i, tt := range tests {
		c := &PathConfig{}
		c.SetAffinity(tt.inAffinity)
		c.SetAffinityCookieSecure(tt.inAffinityCookieSecure)
		FixupPathConfig(c)
		if got, want := c.GetAffinity(), tt.outAffinity; got != want {
			t.Errorf("#%v: c.GetAffinity() = %q, want %q", i, got, want)
		}
		if got, want := c.GetAffinityCookieSecure(), tt.outAffinityCookieSecure; got != want {
			t.Errorf("#%v: c.GetAffinityCookieSecure() = %q, want %q", i, got, want)
		}
	}
}

// TestApplyDefaultPortBackendConfig verifies ApplyDefaultPathConfig.
func TestApplyDefaultPathConfig(t *testing.T) {
	tests := []struct {
		defaultConf *PathConfig
	}{
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetMruby("hello mruby")
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinity(AffinityIP)
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookieName("lb-cookie")
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookiePath("/path")
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetAffinityCookieSecure(AffinityCookieSecureNo)
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetReadTimeout(metav1.Duration{Duration: 5 * time.Minute})
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetWriteTimeout(metav1.Duration{Duration: 10 * time.Second})
				return a
			}(),
		},
		{
			defaultConf: func() *PathConfig {
				a := &PathConfig{}
				a.SetRedirectIfNotTLS(false)
				return a
			}(),
		},
	}

	for i, tt := range tests {
		a := &PathConfig{}
		ApplyDefaultPathConfig(a, tt.defaultConf)

		if got, want := a.GetMruby(), tt.defaultConf.GetMruby(); got != want {
			t.Errorf("#%v: a.GetMruby() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinity(), tt.defaultConf.GetAffinity(); got != want {
			t.Errorf("#%v: a.GetAffinity() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinityCookieName(), tt.defaultConf.GetAffinityCookieName(); got != want {
			t.Errorf("#%v: a.GetAffinityCookieName() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinityCookiePath(), tt.defaultConf.GetAffinityCookiePath(); got != want {
			t.Errorf("#%v: a.GetAffinityCookiePath() = %v, want %v", i, got, want)
		}
		if got, want := a.GetAffinityCookieSecure(), tt.defaultConf.GetAffinityCookieSecure(); got != want {
			t.Errorf("#%v: a.GetAffinityCookieSecure() = %v, want %v", i, got, want)
		}
		if got, want := a.GetReadTimeout(), tt.defaultConf.GetReadTimeout(); !((got != nil && want != nil && *got == *want) || (got == nil && want == nil)) {
			t.Errorf("#%v: a.GetReadTimeout() = %v, want %v", i, got, want)
		}
		if got, want := a.GetWriteTimeout(), tt.defaultConf.GetWriteTimeout(); !((got != nil && want != nil && *got == *want) || (got == nil && want == nil)) {
			t.Errorf("#%v: a.GetWriteTimeout() = %v, want %v", i, got, want)
		}
		if got, want := a.GetRedirectIfNotTLS(), tt.defaultConf.GetRedirectIfNotTLS(); got != want {
			t.Errorf("#%v: a.GetRedirectIfNotTLS() = %v, want %v", i, got, want)
		}
	}
}

// TestNghttpxDuration verifies nghttpxDuration.
func TestNghttpxDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		// 0
		{
			d:    0,
			want: "0",
		},
		// 1
		{
			d:    999 * time.Millisecond,
			want: "999ms",
		},
		// 2
		{
			d:    time.Second,
			want: "1",
		},
		// 3
		{
			d:    1001 * time.Millisecond,
			want: "1001ms",
		},
	}

	for i, tt := range tests {
		if got, want := nghttpxDuration(tt.d), tt.want; got != want {
			t.Errorf("#%v: nghttpxDuration(%v) = %v, want %v", i, tt.d, got, want)
		}
	}
}
