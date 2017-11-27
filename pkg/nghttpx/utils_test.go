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
)

// TestFixupPortBackendConfig validates fixupPortBackendConfig corrects invalid input to the correct default value.
func TestFixupPortBackendConfig(t *testing.T) {
	tests := []struct {
		inProto                 Protocol
		inAffinity              Affinity
		inAffinityCookieSecure  AffinityCookieSecure
		outProto                Protocol
		outAffinity             Affinity
		outAffinityCookieSecure AffinityCookieSecure
	}{
		// 0
		{
			inProto:                 "foo",
			inAffinity:              "bar",
			inAffinityCookieSecure:  "buzz",
			outProto:                ProtocolH1,
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
			inProto:                 ProtocolH2,
			inAffinity:              AffinityIP,
			inAffinityCookieSecure:  AffinityCookieSecureYes,
			outProto:                ProtocolH2,
			outAffinity:             AffinityIP,
			outAffinityCookieSecure: AffinityCookieSecureYes,
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
