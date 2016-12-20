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
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package nghttpx

// IngressConfig describes an nghttpx configuration
type IngressConfig struct {
	Upstreams []*Upstream
	Server    *Server
}

// Upstream describes an nghttpx upstream
type Upstream struct {
	Name     string
	Host     string
	Path     string
	Backends []UpstreamServer
}

// UpstreamByNameServers sorts upstreams by name
type UpstreamByNameServers []*Upstream

func (c UpstreamByNameServers) Len() int      { return len(c) }
func (c UpstreamByNameServers) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c UpstreamByNameServers) Less(i, j int) bool {
	return c[i].Name < c[j].Name
}

// UpstreamServer describes a server in an nghttpx upstream
type UpstreamServer struct {
	Address  string
	Port     string
	Protocol string
	TLS      bool
	SNI      string
}

// UpstreamServerByAddrPort sorts upstream servers by address and port
type UpstreamServerByAddrPort []UpstreamServer

func (c UpstreamServerByAddrPort) Len() int      { return len(c) }
func (c UpstreamServerByAddrPort) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c UpstreamServerByAddrPort) Less(i, j int) bool {
	iName := c[i].Address
	jName := c[j].Address
	if iName != jName {
		return iName < jName
	}

	iU := c[i].Port
	jU := c[j].Port
	return iU < jU
}

// TLS server private key and certificate file path
type TLSCred struct {
	Key      string
	Cert     string
	Checksum string
}

type TLSCredKeyLess []TLSCred

func (c TLSCredKeyLess) Len() int      { return len(c) }
func (c TLSCredKeyLess) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c TLSCredKeyLess) Less(i, j int) bool {
	return c[i].Key < c[j].Key
}

// Server describes an nghttpx server
type Server struct {
	TLS               bool
	DefaultTLSCred    TLSCred
	SubTLSCred        []TLSCred
	TLSCertificate    string
	TLSCertificateKey string
}

// NewDefaultServer return an UpstreamServer to be use as default server that returns 503.
func NewDefaultServer() UpstreamServer {
	return UpstreamServer{Address: "127.0.0.1", Port: "8181"}
}
