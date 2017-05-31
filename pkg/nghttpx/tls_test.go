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
	"encoding/base64"
	"fmt"
	"path/filepath"
	"reflect"
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
	tlsOCSPResp := "dummy-ocsp-response"

	dCrt, err := base64.StdEncoding.DecodeString(tlsCrt)
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
		return
	}

	dKey, err := base64.StdEncoding.DecodeString(tlsKey)
	if err != nil {
		t.Fatalf("Unexpected error: %+v", err)
	}

	name := fmt.Sprintf("test-%v", time.Now().UnixNano())
	tlsCred, err := CreateTLSCred(defaultConfDir, name, dCrt, dKey, []byte(tlsOCSPResp))
	if err != nil {
		t.Fatalf("unexpected error writing TLS key pair: %v", err)
	}

	if got, want := tlsCred.Key.Path, filepath.Join(defaultConfDir, tlsDir, fmt.Sprintf("%v.key", name)); got != want {
		t.Errorf("tlsCred.Key = %v, want %v", got, want)
	}
	if got, want := tlsCred.Cert.Path, filepath.Join(defaultConfDir, tlsDir, fmt.Sprintf("%v.crt", name)); got != want {
		t.Errorf("tlsCred.Crt = %v, want %v", got, want)
	}
	if got, want := tlsCred.OCSPResp.Path, filepath.Join(defaultConfDir, tlsDir, fmt.Sprintf("%v.ocsp-resp", name)); got != want {
		t.Errorf("tlsCred.OCSPResp.Path = %v, want %v", got, want)
	}

	if err := CheckPrivateKey(dKey); err != nil {
		t.Fatalf("unexpected error checking TLS private key: %v", err)
	}

	cnames, err := CommonNames(dCrt)
	if len(cnames) == 0 {
		t.Fatalf("expected at least one cname but none returned")
	}

	if cnames[0] != "echoheaders" {
		t.Fatalf("expected cname echoheaders but %v returned", cnames[0])
	}
}

// TestRemoveDuplicatePems tests RemoveDuplicatePems function.  We make sure that duplicates are removed from supplied input array.
func TestRemoveDuplicatePems(t *testing.T) {
	tests := []struct {
		in  []*TLSCred
		out []*TLSCred
	}{
		{},
		{
			in: []*TLSCred{
				{Key: ChecksumFile{Path: "alpha"}},
			},
			out: []*TLSCred{
				{Key: ChecksumFile{Path: "alpha"}},
			},
		},
		{
			in: []*TLSCred{
				{Key: ChecksumFile{Path: "alpha"}}, {Key: ChecksumFile{Path: "bravo"}}, {Key: ChecksumFile{Path: "charlie"}}, {Key: ChecksumFile{Path: "delta"}},
			},
			out: []*TLSCred{
				{Key: ChecksumFile{Path: "alpha"}}, {Key: ChecksumFile{Path: "bravo"}}, {Key: ChecksumFile{Path: "charlie"}}, {Key: ChecksumFile{Path: "delta"}},
			},
		},
		{
			in: []*TLSCred{
				{Key: ChecksumFile{Path: "alpha"}}, {Key: ChecksumFile{Path: "alpha"}}, {Key: ChecksumFile{Path: "bravo"}}, {Key: ChecksumFile{Path: "charlie"}},
				{Key: ChecksumFile{Path: "charlie"}}, {Key: ChecksumFile{Path: "delta"}}, {Key: ChecksumFile{Path: "delta"}},
			},
			out: []*TLSCred{
				{Key: ChecksumFile{Path: "alpha"}}, {Key: ChecksumFile{Path: "bravo"}}, {Key: ChecksumFile{Path: "charlie"}}, {Key: ChecksumFile{Path: "delta"}},
			},
		},
	}

	for i, tt := range tests {
		if got, want := RemoveDuplicatePems(tt.in), tt.out; !reflect.DeepEqual(got, want) {
			t.Errorf("#%v: RemoveDuplicatePems(%q) = %q, want %q", i, tt.in, got, want)
		}
	}
}
