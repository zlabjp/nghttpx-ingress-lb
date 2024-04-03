/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	defaultConfDir = "dir"
)

func TestCreateTLSCred(t *testing.T) {
	// openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=echoheaders/O=echoheaders"
	tlsCrt := "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURhakNDQWxLZ0F3SUJBZ0lKQUxHUXR5VVBKTFhYTUEwR0NTcUdTSWIzRFFFQkJRVUFNQ3d4RkRBU0JnTlYKQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3RWdZRFZRUUtFd3RsWTJodmFHVmhaR1Z5Y3pBZUZ3MHhOakF6TXpFeQpNekU1TkRoYUZ3MHhOekF6TXpFeU16RTVORGhhTUN3eEZEQVNCZ05WQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3CkVnWURWUVFLRXd0bFkyaHZhR1ZoWkdWeWN6Q0NBU0l3RFFZSktvWklodmNOQVFFQkJRQURnZ0VQQURDQ0FRb0MKZ2dFQkFONzVmS0N5RWwxanFpMjUxTlNabDYzeGQweG5HMHZTVjdYL0xxTHJveVNraW5nbnI0NDZZWlE4UEJWOAo5TUZzdW5RRGt1QVoyZzA3NHM1YWhLSm9BRGJOMzhld053RXNsVDJkRzhRTUw0TktrTUNxL1hWbzRQMDFlWG1PCmkxR2txZFA1ZUExUHlPZCtHM3gzZmxPN2xOdmtJdHVHYXFyc0tvMEhtMHhqTDVtRUpwWUlOa0tGSVhsWWVLZS8KeHRDR25CU2tLVHFMTG0yeExKSGFFcnJpaDZRdkx4NXF5U2gzZTU2QVpEcTlkTERvcWdmVHV3Z2IzekhQekc2NwppZ0E0dkYrc2FRNHpZUE1NMHQyU1NiVkx1M2pScWNvL3lxZysrOVJBTTV4bjRubnorL0hUWFhHKzZ0RDBaeGI1CmVVRDNQakVhTnlXaUV2dTN6UFJmdysyNURMY0NBd0VBQWFPQmpqQ0JpekFkQmdOVkhRNEVGZ1FVcktMZFhHeUUKNUlEOGRvd2lZNkdzK3dNMHFKc3dYQVlEVlIwakJGVXdVNEFVcktMZFhHeUU1SUQ4ZG93aVk2R3Mrd00wcUp1aApNS1F1TUN3eEZEQVNCZ05WQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3RWdZRFZRUUtFd3RsWTJodmFHVmhaR1Z5CmM0SUpBTEdRdHlVUEpMWFhNQXdHQTFVZEV3UUZNQU1CQWY4d0RRWUpLb1pJaHZjTkFRRUZCUUFEZ2dFQkFNZVMKMHFia3VZa3Z1enlSWmtBeE1PdUFaSDJCK0Evb3N4ODhFRHB1ckV0ZWN5RXVxdnRvMmpCSVdCZ2RkR3VBYU5jVQorUUZDRm9NakJOUDVWVUxIWVhTQ3VaczN2Y25WRDU4N3NHNlBaLzhzbXJuYUhTUjg1ZVpZVS80bmFyNUErdWErClIvMHJrSkZnOTlQSmNJd3JmcWlYOHdRcWdJVVlLNE9nWEJZcUJRL0VZS2YvdXl6UFN3UVZYRnVJTTZTeDBXcTYKTUNML3d2RlhLS0FaWDBqb3J4cHRjcldkUXNCcmYzWVRnYmx4TE1sN20zL2VuR1drcEhDUHdYeVRCOC9rRkw3SApLL2ZHTU1NWGswUkVSbGFPM1hTSUhrZUQ2SXJiRnRNV3R1RlJwZms2ZFA2TXlMOHRmTmZ6a3VvUHVEWUFaWllWCnR1NnZ0c0FRS0xWb0pGaGV0b1k9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K"
	tlsKey := "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBM3ZsOG9MSVNYV09xTGJuVTFKbVhyZkYzVEdjYlM5Slh0Zjh1b3V1akpLU0tlQ2V2CmpqcGhsRHc4Rlh6MHdXeTZkQU9TNEJuYURUdml6bHFFb21nQU5zM2Z4N0EzQVN5VlBaMGJ4QXd2ZzBxUXdLcjkKZFdqZy9UVjVlWTZMVWFTcDAvbDREVS9JNTM0YmZIZCtVN3VVMitRaTI0WnFxdXdxalFlYlRHTXZtWVFtbGdnMgpRb1VoZVZoNHA3L0cwSWFjRktRcE9vc3ViYkVza2RvU3V1S0hwQzh2SG1ySktIZDdub0JrT3IxMHNPaXFCOU83CkNCdmZNYy9NYnJ1S0FEaThYNnhwRGpOZzh3elMzWkpKdFV1N2VOR3B5ai9LcUQ3NzFFQXpuR2ZpZWZQNzhkTmQKY2I3cTBQUm5Gdmw1UVBjK01SbzNKYUlTKzdmTTlGL0Q3YmtNdHdJREFRQUJBb0lCQUViNmFEL0hMNjFtMG45bgp6bVkyMWwvYW83MUFmU0h2dlZnRCtWYUhhQkY4QjFBa1lmQUdpWlZrYjBQdjJRSFJtTERoaWxtb0lROWhadHVGCldQOVIxKythTFlnbGdmenZzanBBenR2amZTUndFaEFpM2pnSHdNY1p4S2Q3UnNJZ2hxY2huS093S0NYNHNNczQKUnBCbEFBZlhZWGs4R3F4NkxUbGptSDRDZk42QzZHM1EwTTlLMUxBN2lsck1Na3hwcngxMnBlVTNkczZMVmNpOQptOFdBL21YZ2I0c3pEbVNaWVpYRmNZMEhYNTgyS3JKRHpQWEVJdGQwZk5wd3I0eFIybzdzMEwvK2RnZCtqWERjCkh2SDBKZ3NqODJJaTIxWGZGM2tST3FxR3BKNmhVcncxTUZzVWRyZ29GL3pFck0vNWZKMDdVNEhodGFlalVzWTIKMFJuNXdpRUNnWUVBKzVUTVRiV084Wkg5K2pIdVQwc0NhZFBYcW50WTZYdTZmYU04Tm5CZWNoeTFoWGdlQVN5agpSWERlZGFWM1c0SjU5eWxIQ3FoOVdseVh4cDVTWWtyQU41RnQ3elFGYi91YmorUFIyWWhMTWZpYlBSYlYvZW1MCm5YaGF6MmtlNUUxT1JLY0x6QUVwSmpuZGQwZlZMZjdmQzFHeStnS2YyK3hTY1hjMHJqRE5iNGtDZ1lFQTR1UVEKQk91TlJQS3FKcDZUZS9zUzZrZitHbEpjQSs3RmVOMVlxM0E2WEVZVm9ydXhnZXQ4a2E2ZEo1QjZDOWtITGtNcQpwdnFwMzkxeTN3YW5uWC9ONC9KQlU2M2RxZEcyd1BWRUQ0REduaE54Qm1oaWZpQ1I0R0c2ZnE4MUV6ZE1vcTZ4CklTNHA2RVJaQnZkb1RqNk9pTHl6aUJMckpxeUhIMWR6c0hGRlNqOENnWUVBOWlSSEgyQ2JVazU4SnVYak8wRXcKUTBvNG4xdS9TZkQ4TFNBZ01VTVBwS1hpRTR2S0Qyd1U4a1BUNDFiWXlIZUh6UUpkdDFmU0RTNjZjR0ZHU1ZUSgphNVNsOG5yN051ejg3bkwvUmMzTGhFQ3Y0YjBOOFRjbW1oSy9CbDdiRXBOd0dFczNoNGs3TVdNOEF4QU15c3VxCmZmQ1pJM0tkNVJYNk0zbGwyV2QyRjhFQ2dZQlQ5RU9oTG0vVmhWMUVjUVR0cVZlMGJQTXZWaTVLSGozZm5UZkUKS0FEUVIvYVZncElLR3RLN0xUdGxlbVpPbi8yeU5wUS91UnpHZ3pDUUtldzNzU1RFSmMzYVlzbFVudzdhazJhZAp2ZTdBYXowMU84YkdHTk1oamNmdVBIS05LN2Nsc3pKRHJzcys4SnRvb245c0JHWEZYdDJuaWlpTTVPWVN5TTg4CkNJMjFEUUtCZ0hEQVRZbE84UWlDVWFBQlVqOFBsb1BtMDhwa3cyc1VmQW0xMzJCY00wQk9BN1hqYjhtNm1ManQKOUlteU5kZ2ZiM080UjlKVUxTb1pZSTc1dUxIL3k2SDhQOVlpWHZOdzMrTXl6VFU2b2d1YU8xSTNya2pna29NeAo5cU5pYlJFeGswS1A5MVZkckVLSEdHZEFwT05ES1N4VzF3ektvbUxHdmtYSTVKV05KRXFkCi0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg=="
	tlsOCSPResp := "sample-ocsp-response"

	dCrt, err := base64.StdEncoding.DecodeString(tlsCrt)
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
		return
	}

	dKey, err := base64.StdEncoding.DecodeString(tlsKey)
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}

	tlsCred := CreateTLSCred(defaultConfDir, "tls", dCrt, dKey, []byte(tlsOCSPResp))

	if got, want := tlsCred.Name, "tls"; got != want {
		t.Errorf("tlsCred.Name = %v, want %v", got, want)
	}

	if got, want := tlsCred.Key.Path, filepath.Join(defaultConfDir, tlsDir, hex.EncodeToString(Checksum(dKey))+".key"); got != want {
		t.Errorf("tlsCred.Key.Path = %v, want %v", got, want)
	}

	if got, want := tlsCred.Cert.Path, filepath.Join(defaultConfDir, tlsDir, hex.EncodeToString(Checksum(dCrt))+".crt"); got != want {
		t.Errorf("tlsCred.Cert.Path = %v, want %v", got, want)
	}

	if got, want := tlsCred.OCSPResp.Path, filepath.Join(defaultConfDir, tlsDir, hex.EncodeToString(Checksum([]byte(tlsOCSPResp)))+".ocsp-resp"); got != want {
		t.Errorf("tlsCred.OCSPResp.Path = %v, want %v", got, want)
	}

	if _, err := tls.X509KeyPair(dCrt, dKey); err != nil {
		t.Fatalf("unexpected error parsing TLS key pair: %v", err)
	}
}

// TestSortTLSCred tests SortTLSCred.
func TestSortTLSCred(t *testing.T) {
	tests := []struct {
		desc string
		in   []*TLSCred
		out  []*TLSCred
	}{
		{
			desc: "Empty input",
		},
		{
			desc: "Sort TLSCred",
			in: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0021"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0011"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0020"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00A0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
			},
			out: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0011"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0020"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0021"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00A0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			SortTLSCred(tt.in)

			if got, want := tt.in, tt.out; !reflect.DeepEqual(got, want) {
				t.Errorf("tt.in = %v, want %v", got, want)
			}
		})
	}
}

// TestRemoveDuplicateTLSCred tests RemoveDuplicateTLSCred function.  We make sure that duplicates are removed from supplied input array.
func TestRemoveDuplicateTLSCred(t *testing.T) {
	tests := []struct {
		desc string
		in   []*TLSCred
		out  []*TLSCred
	}{
		{
			desc: "Empty input",
		},
		{
			desc: "Single entry",
			in: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
			},
			out: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
			},
		},
		{
			desc: "Multiple entries and no duplicates",
			in: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0011"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0020"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0021"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00A0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
			},
			out: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0011"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0020"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0021"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00A0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
			},
		},
		{
			desc: "Duplicates must be removed",
			in: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0011"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0020"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0021"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00A0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
			},
			out: []*TLSCred{
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0010"}},
				{Key: PrivateChecksumFile{Path: "alpha"}, Cert: ChecksumFile{Path: "0011"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0020"}},
				{Key: PrivateChecksumFile{Path: "bravo"}, Cert: ChecksumFile{Path: "0021"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00A0"}},
				{Key: PrivateChecksumFile{Path: "charlie"}, Cert: ChecksumFile{Path: "0030"}, OCSPResp: &ChecksumFile{Path: "00B0"}},
				{Key: PrivateChecksumFile{Path: "delta"}, Cert: ChecksumFile{Path: "0040"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := RemoveDuplicateTLSCred(tt.in), tt.out; !reflect.DeepEqual(got, want) {
				t.Errorf("RemoveDuplicateTLSCred(%v) = %v, want %v", tt.in, got, want)
			}
		})
	}
}

// TestVerifyCertificate verifies VerifyCertificate.
func TestVerifyCertificate(t *testing.T) {
	tests := []struct {
		desc    string
		crt     string
		wantErr string
	}{
		{
			desc: "Valid Certificate with SHA256",
			// openssl req -x509 -nodes -days 36500 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=echoheaders/O=echoheaders"
			crt: "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURPekNDQWlPZ0F3SUJBZ0lVY0VUU3p3d1lFWEI0YUpQdlBFak5JQWppaWc4d0RRWUpLb1pJaHZjTkFRRUwKQlFBd0xERVVNQklHQTFVRUF3d0xaV05vYjJobFlXUmxjbk14RkRBU0JnTlZCQW9NQzJWamFHOW9aV0ZrWlhKegpNQ0FYRFRJeU1ETXlNakF4TkRBeE5Gb1lEekl4TWpJd01qSTJNREUwTURFMFdqQXNNUlF3RWdZRFZRUUREQXRsClkyaHZhR1ZoWkdWeWN6RVVNQklHQTFVRUNnd0xaV05vYjJobFlXUmxjbk13Z2dFaU1BMEdDU3FHU0liM0RRRUIKQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURrOWMzenk2T0NVNUN1WEFTREh2WFJOLy9CbGJicXdPS0w2ODBnT2pLbQpsTU14L3hrdko0dURvWS90bUpmWkhneW9KelNNTFhzMldyNHFXNTlrL0NSalFYbndzT0tUTjR4MzJNQmRMamVzCjBKVHF3cEp6U2Vad1djNDJtREVhUkpCazEyS3dCc3VTTTJQSDVDRW9vcWllalVCenpNNGl3bWcvR0sxK3N6VTAKbjJmZ0xtcDdCOTZyRHFza3o4QlEyWi93SHQ5MVhHOXVEbnh4T3FrMkRFYVFGMjJrdFhIUGYrTEk1dytKbEpYMQpxeUpLdEtrRDd6bmc4YU8rb3FTMG9UZGxRb0d0V2g0ZTJMUHZ6M3dDMVZjRVRCdEtwOHEyMWJMb3Z5T1ZxQ1JkCkVRTzByMkp3MlBScDdwS0lhMkU5akViMUJqbjNGUEQ5WUlLQkV1Y3owazhoQWdNQkFBR2pVekJSTUIwR0ExVWQKRGdRV0JCUmlxNWprU2xVdXlNRHZxREtGWkNIa0xGWEVkekFmQmdOVkhTTUVHREFXZ0JSaXE1amtTbFV1eU1EdgpxREtGWkNIa0xGWEVkekFQQmdOVkhSTUJBZjhFQlRBREFRSC9NQTBHQ1NxR1NJYjNEUUVCQ3dVQUE0SUJBUURHCmJWejB1YzFGbThtbmxGOTZFZk1oVUZ5VW5XQWZSNkkyMnJpcVNHQ0xRK1l6czlsRFYrZ1U3dUtYL2RjdlFYR2UKWHdkMzBOY29YNCtzaytIUzZpY1oxc0VhYmkyWk0wSFJFSE5STHdIRkh3VTBoM2kzUXA2Y0NUOVIwVUF5dGZiKwo3SU5DN1RtSlU0UTk5UXRGSmZ1amVXcmlLbCt4cXpOVUZEWUJSV0Q0d3daMG1EK0pLK2NNY1h6UlIyL1gxQzVPCnovU1ZiZXJKZGQ2dGdCd2lqblMvbVZqM1FFYW1vMU5BQ0NQL0lraEEvUnJQRVY0T2xWNm8xV3k1a3FrVit1V2gKaDNVU2Q3aFgyY1FvSFJFeHhqREp2Y250Z21TVFpSQitEOXN0NjBWWW05WW50THZyOFVPL1NsTlVlajRHM0xHcQo2bXQ4THdwd0hDeFZWSzg1SEtMSwotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
		},
		{
			desc: "Expired certificate with SHA256",
			// openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=echoheaders/O=echoheaders"
			crt:     "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURPVENDQWlHZ0F3SUJBZ0lVRm0wbFJHM3ZGZ1p3UlBmTWFJbUI0NGtpU0tnd0RRWUpLb1pJaHZjTkFRRUwKQlFBd0xERVVNQklHQTFVRUF3d0xaV05vYjJobFlXUmxjbk14RkRBU0JnTlZCQW9NQzJWamFHOW9aV0ZrWlhKegpNQjRYRFRJd01Ea3dOekF5TXpjek1Wb1hEVEl4TURrd056QXlNemN6TVZvd0xERVVNQklHQTFVRUF3d0xaV05vCmIyaGxZV1JsY25NeEZEQVNCZ05WQkFvTUMyVmphRzlvWldGa1pYSnpNSUlCSWpBTkJna3Foa2lHOXcwQkFRRUYKQUFPQ0FROEFNSUlCQ2dLQ0FRRUF2dnFGQVJ1QmZIYTNrMlZKMTY4ZWpOS2w3RS96ZXRLb3BnKzRGZ0J0V3M1OApLZDN4N3p2TzVMQWhBODJrWUZVbE9pWlZIWUdibldZSTdBR3dWUXdwRHlZdnVMeGtqUWN0TG9SanU2bzFDTW9LClJXbWg2VXN3RGVyb2Vpdy9zQU9jTXAyUWxPVXBsQWZyK1U4d2ZKaVRrcWVMMVJ0d0pwQ05Ca0l2MnUxand0YTQKVmMvU1JEVjI3LzYwTEtMTHFUdmo3L2NRNzNWK2trUlFPSWdGRDQyUFF5V2xnMHZyVmhDczVSd3Y1NmtHV1RZbQp4K1RLWlRwZ3BTQWg1b1hIVXVISmlsUUpKNmZQMC93T2NERkpkVzBLNnU2VVBSSktrVE1LSmlxcGExbjN6NGoyCi9UL3BObFprNkFocWNNekFVT25iTWJQSEsxTkd5MFdUTXg1bUt6ZlltUUlEQVFBQm8xTXdVVEFkQmdOVkhRNEUKRmdRVTFGVHRnM0JNZnc3R0FCVTl5cFZ3aFlXdUUwUXdId1lEVlIwakJCZ3dGb0FVMUZUdGczQk1mdzdHQUJVOQp5cFZ3aFlXdUUwUXdEd1lEVlIwVEFRSC9CQVV3QXdFQi96QU5CZ2txaGtpRzl3MEJBUXNGQUFPQ0FRRUF2V3lSCmYvd2JjdDA5WG1jY3pOWlVZbFhOcm51aXRBRU1rODVIaUFMNWU5bDBiaEFyakRYUjEybzNWNGtlNE5VekUzYkgKaW1jNkxLdmVXc3NRdzZlRmtmMXlBbWcxMDRTZ1pjVGFVVytUNFRGeERLM2Q5c2c3dUpRUk9qK3c2V0hMb0VCYgpBV0xkc09keko1S3hVWGdPcHVqWC9TcDRNdWhyc3NxWjd2MXBvQVBPQTJaRzgrekhGK0J4TnFRa1ZnVlEwL3hECjNNc3VHL1lOTDk3c05sSndMMkVYelhXNldIVGc5bXhLcWp4OVI0dWdvblZkc05MZnE5WlVYSTlXZGVielRoVlEKVThoOW5JY09wSHNSTzYyQUVvY3ByWGg5N2ZQTUtrRVY5c082WHVwREs0VFJtY0NwcU84ck01cGN4Zzd0WjIzRgo3MEhwbDlJWXdYOWwvbFZaeXc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
			wantErr: "certificate has expired or is not valid at the given moment of time",
		},
		{
			desc: "Certificate with SHA1WithRSA",
			// openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=echoheaders/O=echoheaders" -sha1
			crt:     "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURPVENDQWlHZ0F3SUJBZ0lVREVzS2JIQUlza3Z4VUVLMDhmWVNDNDE5Ujlrd0RRWUpLb1pJaHZjTkFRRUYKQlFBd0xERVVNQklHQTFVRUF3d0xaV05vYjJobFlXUmxjbk14RkRBU0JnTlZCQW9NQzJWamFHOW9aV0ZrWlhKegpNQjRYRFRJd01Ea3dOekF5TXprek5Gb1hEVEl4TURrd056QXlNemt6TkZvd0xERVVNQklHQTFVRUF3d0xaV05vCmIyaGxZV1JsY25NeEZEQVNCZ05WQkFvTUMyVmphRzlvWldGa1pYSnpNSUlCSWpBTkJna3Foa2lHOXcwQkFRRUYKQUFPQ0FROEFNSUlCQ2dLQ0FRRUFxd00yeXpaZkFxTkxjOW51a3BMcnQxVTgrSnhlRTVMTEt2bHN5N04ydG1lSQpyOVMrSHp1OUkvajByOUpvN051UkhvOE1TVXNaZ1VFMFUxdlRIT1g4UDZETVdMS1dZVmtkNWlBbmJGYUhJeHY3CldJQUxCUHFQYWhJNmxhNGp0ejVXVDBZeHlIQ0JUQnN1M2owMVIvcFhIMDB4bzlDNFZSQU9CRHVjenR6ci9RRTgKcmFLUkJwa05pcFRvQ1BLdXNrQ1ltTDk5KzQ5UndoT1ovOEpnRmJQV2JwQVFxV3g0UWc3cllNbmxUTU5pSnd5QwpLdTFIT2hHa0dXMDRpM1VNMSswTFVpRGFYSVVmME9vcHlWRW1XbHkxZUVKQnJjM2l3bnFzMERwdmlOZ2w1ZUxHCmpPUzY5OVM3SXU1VExzY1gzekRmZloxeHRiT21oaU1lUjNFWWVKL1lvd0lEQVFBQm8xTXdVVEFkQmdOVkhRNEUKRmdRVXFoTDJpNjVwL3F1eXdQTHkwL1dpTFRNb0lBb3dId1lEVlIwakJCZ3dGb0FVcWhMMmk2NXAvcXV5d1BMeQowL1dpTFRNb0lBb3dEd1lEVlIwVEFRSC9CQVV3QXdFQi96QU5CZ2txaGtpRzl3MEJBUVVGQUFPQ0FRRUFBdTI0Cm16Tzd0MzAwSWx4Z3laWEorL0lMWEplbzRrQWIyL0VzbDhTNk1CSVZyeldyUVd2cW96Y1lOQjNybkxCMXlNeUEKbThHRk5MbUZtajh0WWFET01vWXliSmhLeTVWajhXQlNkWmVqMW9mOUpkNklWVXpoUnhTdUJ5YWhYVjhPWDErMApxKy9sUkZZOW1OYzhoRG4yd0hZM2xEUVY0Nng0T01Dbjh3c0xBZ0JTOGlYZVhndTdCUlhoY1E3Q3VoeE05QWppCnNJdzlGVkpjd0hUOEZZRHNtN29Qa1dMN0FRVko3cVpJK1JIbENvdG9NZFhpL3c3VExVOWEzaHllNU10eU1VWnUKVTdnVUtwWjRWUVh2YzcyMkQzbkV5ZVJIR3ZHcHV5S3V2MzZlMXkyOXFUYnZJZU5Iam5FbVowMVFCVEd1VlprZwpaWm1wb2lNZVdCMkdTWERSZHc9PQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==",
			wantErr: "unsupported signature algorithm: SHA1-RSA",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			certPEM, err := base64.StdEncoding.DecodeString(tt.crt)
			if err != nil {
				panic(err)
			}

			cert, err := ReadLeafCertificate(certPEM)
			if err != nil {
				panic(err)
			}

			var errMsg string
			if err := VerifyCertificate(context.Background(), cert, time.Now()); err != nil {
				errMsg = err.Error()
			}

			if got, want := errMsg, tt.wantErr; got != want {
				t.Errorf("errMsg = %v, want %v", got, want)
			}
		})
	}
}

// TestNormalizePEM verifies NormalizePEM.
func TestNormalizePEM(t *testing.T) {
	const normPEM = `-----BEGIN CERTIFICATE-----
MIIDkzCCAnugAwIBAgIURHf7IiRPSCwzvqGcjHrL7daI1LgwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MB4X
DTIxMDEyMjAwMjYwMFoXDTIyMDEyMjAwMjYwMFowWTELMAkGA1UEBhMCQVUxEzAR
BgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5
IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAvjAdhKGhqic8As5c+cTJRoyXo1VqThrr2pUWnRfp3xy2NkzRjYYJ
zPWdbfVABPBktG1E6RrktNfkDmDqOzr9yZULuny6IBsjphGlqloXTLPjXHi/9wWy
CdSZBKmCCpHxqv0ACWD2DCAUkEOM774O/xIhJTJzLIrt0DpzN3fgjQI4IIbV+DNE
x7yM58MvOQfU79eeE9p67JLFpP5Um1C/svQPdD701Ho262pEWfl2h20ZfG+ACfVp
BiuR+WNhC8SkABZLXal9DRtnTM7U5vh+/sSTf5hZ3rp0XclHEE/dz6oyHkdmQ+UW
FuyQkCDAYBTMyzUml8xwVgI3o/3m/E8GLwIDAQABo1MwUTAdBgNVHQ4EFgQUDEx/
wU/IzXILpB0k07aJgWm0pqswHwYDVR0jBBgwFoAUDEx/wU/IzXILpB0k07aJgWm0
pqswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAheB+XDZZCQW1
E/hS4SJo+aY7+MPuhf6UsBNaT8svGlbSpL7Hdgox5WgTrCu2JxupwBNpF8F4aCXU
8aL3y66JSKfPnmXqDdXJg2ONzlrpzoaNzU0ysHwpvwTD7NzSLOgXOH6kNP//u+Ie
dlfNUx10LpM1tzs3/+yyA1sDX/WXOiewOtN8Ik4ckBa6iPi6LLJ2586BFtSFVdVi
oaV4iMr9vFevRNEbwzFu5e0ChRxHavVnCNndpEkK/rVb4/pHqJ1G+w3ep1L4rPah
WBGtG3/5DLn9GGMIPZyEtsfsFA5+b3DNthRdxlWL3MmvOkdxo5DX60ZC+A1YDcdj
fyQkcb8UhQ==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIDkzCCAnugAwIBAgIUZQvh/qn1NZyp2g4RXN4PBWyt+RcwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCSlAxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MB4X
DTIxMDEyMjAwMjcxNVoXDTIyMDEyMjAwMjcxNVowWTELMAkGA1UEBhMCSlAxEzAR
BgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5
IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAsDZS4ihNX9buUrlVF1Lmh7hoVn/JrtYQpQ9/e9vQTKzh0PL80T3f
EQJ8ZE2oB1cVSUN5OavcsnfMC7YRf+kZ5TY6F7YUl2pJbuH5zt1e4iclIMto8X5V
VBa31XG+cocS7bpCMu6GQQ+Ohi0/9nTNjAbkcZaP4oLnHPHFdtkYM9Os6l4mcQ2U
38oHrsBRBTetXS8gZMxFw+cUce/3VipzWcR19ATRFSfHPH7hGsTOI9InRz1rCzu/
2C3BsKeWEPcHsdnYDl1TAz4DmyoFDH2zH2OQWAM+bug47LngxOw4aOcQM1DYOOn0
rPfnb2zatiUXdHhDn5VeHxnmZflEx+HxPwIDAQABo1MwUTAdBgNVHQ4EFgQUic1T
7zOqdOXXBjL4UgmN7U99+X0wHwYDVR0jBBgwFoAUic1T7zOqdOXXBjL4UgmN7U99
+X0wDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAbHmhDw8xkp8t
ETqKV3KuMNiTqJLIxx/si08P/3UZQpeLiXqwLCpjj2udIRqHkUPMEstFc4AYK2Wl
ec/XQxh4hJsWhdaD6JBb4Gn+6Z9uYUTCTxDbwgiUqpjd458sIX0EgcQBEXJdT+nu
kNfaTl4t/BczrXQf3h+2K7VEnF+theB0VthrehFbDvaD3kp95oFPZglgX7DpsB2g
MrkG09abOHnYi25ziYtiQwbnjmi3NG1nflqnpIhGWmor0U2AMaE+4of/evugJTwy
r1K7N2unJBaH84CjJpejcuLfzCvLCthdsu3CqXbwMNesL82+niOAyJETd2m5IlgW
3Y1Vfys94A==
-----END CERTIFICATE-----
`

	tests := []struct {
		desc string
		pem  string
		want string
	}{
		{
			desc: "Already normalized PEM",
			pem:  normPEM,
			want: normPEM,
		},
		{
			desc: "Badly formatted PEM",
			pem: `
-----BEGIN CERTIFICATE-----

MIIDkzCCAnugAwIBAgIURHf7IiRPSCwzvqGcjHrL7daI1LgwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MB4X
DTIxMDEyMjAwMjYwMFoXDTIyMDEyMjAwMjYwMFowWTELMAkGA1UEBhMCQVUxEzAR
BgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5
IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAvjAdhKGhqic8As5c+cTJRoyXo1VqThrr2pUWnRfp3xy2NkzRjYYJ
zPWdbfVABPBktG1E6RrktNfkDmDqOzr9yZULuny6IBsjphGlqloXTLPjXHi/9wWy
  CdSZBKmCCpHxqv0ACWD2DCAUkEOM774O/xIhJTJzLIrt0DpzN3fgjQI4IIbV+DNE
  x7yM58MvOQfU79eeE9p67JLFpP5Um1C/svQPdD701Ho262pEWfl2h20ZfG+ACfVp
BiuR+WNhC8SkABZLXal9DRtnTM7U5vh+/sSTf5hZ3rp0XclHEE/dz6oyHkdmQ+UW
     FuyQkCDAYBTMyzUml8xwVgI3o/3m/E8GLwIDAQABo1MwUTAdBgNVHQ4EFgQUDEx/
wU/IzXILpB0k07aJgWm0pqswHwYDVR0jBBgwFoAUDEx/wU/IzXILpB0k07aJgWm0
pqswDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAheB+XDZZCQW1
   E/hS4SJo+aY7+MPuhf6UsBNaT8svGlbSpL7Hdgox5WgTrCu2JxupwBNpF8F4aCXU
8aL3y66JSKfPnmXqDdXJg2ONzlrpzoaNzU0ysHwpvwTD7NzSLOgXOH6kNP//u+Ie
dlfNUx10LpM1tzs3/+yyA1sDX/WXOiewOtN8Ik4ckBa6iPi6LLJ2586BFtSFVdVi
oaV4iMr9vFevRNEbwzFu5e0ChRxHavVnCNndpEkK/rVb4/pHqJ1G+w3ep1L4rPah
WBGtG3/5DLn9GGMIPZyEtsfsFA5+b3DNthRdxlWL3MmvOkdxo5DX60ZC+A1YDcdj
fyQkcb8UhQ==
-----END CERTIFICATE-----

-----BEGIN CERTIFICATE-----

  MIIDkzCCAnugAwIBAgIUZQvh/qn1NZyp2g4RXN4PBWyt+RcwDQYJKoZIhvcNAQEL
BQAwWTELMAkGA1UEBhMCSlAxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MB4X
DTIxMDEyMjAwMjcxNVoXDTIyMDEyMjAwMjcxNVowWTELMAkGA1UEBhMCSlAxEzAR
    BgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5
IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAsDZS4ihNX9buUrlVF1Lmh7hoVn/JrtYQpQ9/e9vQTKzh0PL80T3f
EQJ8ZE2oB1cVSUN5OavcsnfMC7YRf+kZ5TY6F7YUl2pJbuH5zt1e4iclIMto8X5V
VBa31XG+cocS7bpCMu6GQQ+Ohi0/9nTNjAbkcZaP4oLnHPHFdtkYM9Os6l4mcQ2U
38oHrsBRBTetXS8gZMxFw+cUce/3VipzWcR19ATRFSfHPH7hGsTOI9InRz1rCzu/
2C3BsKeWEPcHsdnYDl1TAz4DmyoFDH2zH2OQWAM+bug47LngxOw4aOcQM1DYOOn0
rPfnb2zatiUXdHhDn5VeHxnmZflEx+HxPwIDAQABo1MwUTAdBgNVHQ4EFgQUic1T
7zOqdOXXBjL4UgmN7U99+X0wHwYDVR0jBBgwFoAUic1T7zOqdOXXBjL4UgmN7U99
   +X0wDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAbHmhDw8xkp8t
ETqKV3KuMNiTqJLIxx/si08P/3UZQpeLiXqwLCpjj2udIRqHkUPMEstFc4AYK2Wl
ec/XQxh4hJsWhdaD6JBb4Gn+6Z9uYUTCTxDbwgiUqpjd458sIX0EgcQBEXJdT+nu
kNfaTl4t/BczrXQf3h+2K7VEnF+theB0VthrehFbDvaD3kp95oFPZglgX7DpsB2g
MrkG09abOHnYi25ziYtiQwbnjmi3NG1nflqnpIhGWmor0U2AMaE+4of/evugJTwy
r1K7N2unJBaH84CjJpejcuLfzCvLCthdsu3CqXbwMNesL82+niOAyJETd2m5IlgW
3Y1Vfys94A==

-----END CERTIFICATE-----
`,
			want: normPEM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			p, err := NormalizePEM([]byte(tt.pem))
			if err != nil {
				t.Fatalf("NormalizePEM(...): %v", err)
			}

			if got, want := string(p), tt.want; got != want {
				t.Errorf("p = %v, want %v", got, want)
			}
		})
	}
}

func TestNewTLSTicketKey(t *testing.T) {
	ticketKey, err := NewTLSTicketKey()
	if err != nil {
		t.Fatalf("NewTLSTicketKey: %v", err)
	}

	if got, want := len(ticketKey), TLSTicketKeySize; got != want {
		t.Errorf("len(ticketKey) = %v, want %v", got, want)
	}
}

func TestVerifyTLSTicketKey(t *testing.T) {
	tests := []struct {
		desc      string
		ticketKey []byte
		wantErr   bool
	}{
		{
			desc:    "Empty key",
			wantErr: true,
		},
		{
			desc: "Good key",
			ticketKey: []byte("" +
				"012345678901234567890123456789012345678901234567" +
				"012345678901234567890123456789012345678901234567",
			),
		},
		{
			desc: "Malformed key",
			ticketKey: []byte("" +
				"012345678901234567890123456789012345678901234567" +
				"0123456789012345678901234567890123456789012345678",
			),
			wantErr: true,
		},
		{
			desc:      "Single key",
			ticketKey: []byte("012345678901234567890123456789012345678901234567"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := VerifyTLSTicketKey(tt.ticketKey)
			if err != nil {
				if tt.wantErr {
					return
				}

				t.Fatalf("VerifyTLSTicketKey: %v", err)
			}

			if tt.wantErr {
				t.Fatal("VerifyTLSTicketKey should fail")
			}
		})
	}
}

type tlsKeyGenerator struct {
	seq int
}

func (g *tlsKeyGenerator) generateKey() ([]byte, error) {
	prefix := "new" + strconv.Itoa(g.seq)
	g.seq++

	return []byte(prefix + strings.Repeat(".", TLSTicketKeySize-len(prefix))), nil
}

func TestUpdateTLSTicketKeyFunc(t *testing.T) {
	tests := []struct {
		desc      string
		ticketKey []byte
		want      []byte
	}{
		{
			desc: "Key is rotated and new key is appended",
			ticketKey: []byte("" +
				"old0............................................" +
				"old1............................................",
			),
			want: []byte("" +
				"old1............................................" +
				"old0............................................" +
				"new0............................................",
			),
		},
		{
			desc: "oldest key is discarded",
			ticketKey: []byte("" +
				"old10..........................................." +
				"old9............................................" +
				"old8............................................" +
				"old7............................................" +
				"old6............................................" +
				"old5............................................" +
				"old4............................................" +
				"old3............................................" +
				"old2............................................" +
				"old1............................................" +
				"old0............................................" +
				"old11...........................................",
			),
			want: []byte("" +
				"old11..........................................." +
				"old10..........................................." +
				"old9............................................" +
				"old8............................................" +
				"old7............................................" +
				"old6............................................" +
				"old5............................................" +
				"old4............................................" +
				"old3............................................" +
				"old2............................................" +
				"old1............................................" +
				"new0............................................",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			var g tlsKeyGenerator

			newTicketKey, err := UpdateTLSTicketKeyFunc(tt.ticketKey, g.generateKey)
			if err != nil {
				t.Fatalf("UpdateTLSTicketKeyFunc: %v", err)
			}

			if got, want := newTicketKey, tt.want; !bytes.Equal(got, want) {
				t.Errorf("newTicketKey = %s, want %s", got, want)
			}
		})
	}
}

func TestCreateTLSTicketKeyFiles(t *testing.T) {
	ticketKey := []byte("" +
		"old1............................................" +
		"old0............................................" +
		"new0............................................",
	)

	files := CreateTLSTicketKeyFiles("/foo/bar", ticketKey)

	if got, want := len(files), len(ticketKey)/TLSTicketKeySize; got != want {
		t.Fatalf("len(files) = %v, want %v", got, want)
	}

	wantFiles := []*PrivateChecksumFile{
		{
			Path:     "/foo/bar/tls-ticket-key/key-0",
			Content:  []byte("old1............................................"),
			Checksum: hexMustDecodeString("caba3c7923d7e867d0fd00665fbcfbe9b1d73925097aa0bc0f220590df5a7fc4"),
		},
		{
			Path:     "/foo/bar/tls-ticket-key/key-1",
			Content:  []byte("old0............................................"),
			Checksum: hexMustDecodeString("a86b67f250b9a6bba17908f0c448d7dc63f0c66964229a2341b73d7008d35cf3"),
		},
		{
			Path:     "/foo/bar/tls-ticket-key/key-2",
			Content:  []byte("new0............................................"),
			Checksum: hexMustDecodeString("c2816f6b0278c715029a1de1487c14fb6daad4655e8149becda7259b42cc1fc5"),
		},
	}

	for i, f := range files {
		if got, want := f, wantFiles[i]; !reflect.DeepEqual(got, want) {
			t.Errorf("files[%v] = %q, want %q", i, got, want)
		}
	}
}
