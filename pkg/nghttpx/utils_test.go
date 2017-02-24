/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
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
		in  PortBackendConfig
		out PortBackendConfig
	}{
		{
			in: PortBackendConfig{
				Proto:    "foo",
				Affinity: "bar",
			},
			out: PortBackendConfig{
				Proto:    ProtocolH1,
				Affinity: AffinityNone,
			},
		},
		{
			// Empty Proto and Affinity should be replaced with default values.
			out: PortBackendConfig{
				Proto:    ProtocolH1,
				Affinity: AffinityNone,
			},
		},
		{
			// Correct input must be left unchanged.
			in: PortBackendConfig{
				Proto:    ProtocolH2,
				Affinity: AffinityIP,
			},
			out: PortBackendConfig{
				Proto:    ProtocolH2,
				Affinity: AffinityIP,
			},
		},
	}

	for i, tt := range tests {
		if got, want := FixupPortBackendConfig(tt.in, "svc", "port"), tt.out; got != want {
			t.Errorf("#%v: fixupPortBackendConfig(%+v) = %+v, want %+v", i, tt.in, got, want)
		}
	}
}
