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
	"runtime"
	"strconv"
)

// Interface is the API to update underlying load balancer.
type Interface interface {
	// Start starts a nghttpx process using executable at path with configuration file at confPath, and wait.  If stopCh becomes
	// readable, kill nghttpx process, and return.
	Start(path, confPath string, stopCh <-chan struct{})
	// CheckAndReload checks whether the nghttpx configuration changed, and if so, make nghttpx reload its configuration.  If reloading
	// is required, and it successfully issues reloading, returns true.  If there is no need to reloading, it returns false.  On error,
	// it returns false, and non-nil error.
	CheckAndReload(ingressCfg *IngressConfig) (bool, error)
}

// IngressConfig describes an nghttpx configuration
type IngressConfig struct {
	Upstreams      []*Upstream
	TLS            bool
	DefaultTLSCred *TLSCred
	SubTLSCred     []*TLSCred
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-n
	// Set the number of worker threads.
	Workers string
	// ExtraConfig is the extra configurations in a format that nghttpx accepts in --conf.
	ExtraConfig string
	// MrubyFileContent is the extra mruby script.  It is saved in the container disk space, and will be referenced by mruby-file from
	// configuration file.
	MrubyFile *ChecksumFile
	// HealthPort is the port for health monitor endpoint.
	HealthPort int
	// APIPort is the port for API endpoint.
	APIPort int
	// ConfDir is the path to the directory which includes nghttpx configuration files.
	ConfDir string
	// HTTPPort is the port to listen to for HTTP (non-TLS) request.
	HTTPPort int
	// HTTPSPort is the port to listen to for HTTPS (TLS) request.
	HTTPSPort int
	// FetchOCSPRespFromSecret is true if OCSP response is fetched from TLS secret.
	FetchOCSPRespFromSecret bool
	// ProxyProto toggles the use of PROXY protocol for all public-facing frontends.
	ProxyProto bool
}

// NewIngressConfig returns new IngressConfig.  Workers is initialized as the number of CPU cores.
func NewIngressConfig() *IngressConfig {
	return &IngressConfig{
		Workers: strconv.Itoa(runtime.NumCPU()),
	}
}

// Upstream describes an nghttpx upstream
type Upstream struct {
	Name             string
	Host             string
	Path             string
	Backends         []UpstreamServer
	RedirectIfNotTLS bool
}

type Affinity string

const (
	AffinityNone   Affinity = "none"
	AffinityIP     Affinity = "ip"
	AffinityCookie Affinity = "cookie"
)

type AffinityCookieSecure string

const (
	AffinityCookieSecureAuto AffinityCookieSecure = "auto"
	AffinityCookieSecureYes  AffinityCookieSecure = "yes"
	AffinityCookieSecureNo   AffinityCookieSecure = "no"
)

type Protocol string

const (
	// HTTP/2 protocol
	ProtocolH2 Protocol = "h2"
	// HTTP/1.1 protocol
	ProtocolH1 Protocol = "http/1.1"
)

// UpstreamServer describes a server in an nghttpx upstream
type UpstreamServer struct {
	Address              string
	Port                 string
	Protocol             Protocol
	TLS                  bool
	SNI                  string
	DNS                  bool
	Affinity             Affinity
	AffinityCookieName   string
	AffinityCookiePath   string
	AffinityCookieSecure AffinityCookieSecure
	Mruby                *ChecksumFile
}

// TLS server private key, certificate file path, and optionally OCSP response.  OCSP response must be DER encoded byte string.
type TLSCred struct {
	Key      ChecksumFile
	Cert     ChecksumFile
	OCSPResp ChecksumFile
}

// NewDefaultServer return an UpstreamServer to be use as default server that returns 503.
func NewDefaultServer() UpstreamServer {
	return UpstreamServer{
		Address:  "127.0.0.1",
		Port:     "8181",
		Protocol: ProtocolH1,
		Affinity: AffinityNone,
	}
}

// backend configuration obtained from ingress annotation, specified per service port
type PortBackendConfig struct {
	// backend application protocol.  At the moment, this should be either ProtocolH2 or ProtocolH1.
	Proto *Protocol `json:"proto,omitempty"`
	// true if backend connection requires TLS
	TLS *bool `json:"tls,omitempty"`
	// SNI hostname for backend TLS connection
	SNI *string `json:"sni,omitempty"`
	// DNS is true if backend hostname is resolved dynamically rather than start up or configuration reloading.
	DNS *bool `json:"dns,omitempty"`
	// Affinity is session affinity method nghttpx supports.  See affinity parameter in backend option of nghttpx.
	Affinity *Affinity `json:"affinity,omitempty"`
	// AffinityCookieName is a name of cookie to use for cookie-based session affinity.
	AffinityCookieName *string `json:"affinityCookieName,omitempty"`
	// AffinityCookiePath is a path of cookie for cookie-based session affinity.
	AffinityCookiePath *string `json:"affinityCookiePath,omitempty"`
	// AffinityCookieSecure controls whether Secure attribute is added to session affinity cookie.
	AffinityCookieSecure *AffinityCookieSecure `json:"affinityCookieSecure,omitempty"`
	// Mruby is mruby script
	Mruby *string `json:"mruby,omitempty"`
}

func (pbc *PortBackendConfig) GetProto() Protocol {
	if pbc.Proto == nil {
		return ""
	}
	return *pbc.Proto
}

func (pbc *PortBackendConfig) SetProto(proto Protocol) {
	pbc.Proto = new(Protocol)
	*pbc.Proto = proto
}

func (pbc *PortBackendConfig) GetTLS() bool {
	if pbc.TLS == nil {
		return false
	}
	return *pbc.TLS
}

func (pbc *PortBackendConfig) SetTLS(tls bool) {
	pbc.TLS = new(bool)
	*pbc.TLS = tls
}

func (pbc *PortBackendConfig) GetSNI() string {
	if pbc.SNI == nil {
		return ""
	}
	return *pbc.SNI
}

func (pbc *PortBackendConfig) SetSNI(sni string) {
	pbc.SNI = new(string)
	*pbc.SNI = sni
}

func (pbc *PortBackendConfig) GetDNS() bool {
	if pbc.DNS == nil {
		return false
	}
	return *pbc.DNS
}

func (pbc *PortBackendConfig) SetDNS(dns bool) {
	pbc.DNS = new(bool)
	*pbc.DNS = dns
}

func (pbc *PortBackendConfig) GetAffinity() Affinity {
	if pbc.Affinity == nil {
		return AffinityNone
	}
	return *pbc.Affinity
}

func (pbc *PortBackendConfig) SetAffinity(affinity Affinity) {
	pbc.Affinity = new(Affinity)
	*pbc.Affinity = affinity
}

func (pbc *PortBackendConfig) GetAffinityCookieName() string {
	if pbc.AffinityCookieName == nil {
		return ""
	}
	return *pbc.AffinityCookieName
}

func (pbc *PortBackendConfig) SetAffinityCookieName(affinityCookieName string) {
	pbc.AffinityCookieName = new(string)
	*pbc.AffinityCookieName = affinityCookieName
}

func (pbc *PortBackendConfig) GetAffinityCookiePath() string {
	if pbc.AffinityCookiePath == nil {
		return ""
	}
	return *pbc.AffinityCookiePath
}

func (pbc *PortBackendConfig) SetAffinityCookiePath(affinityCookiePath string) {
	pbc.AffinityCookiePath = new(string)
	*pbc.AffinityCookiePath = affinityCookiePath
}

func (pbc *PortBackendConfig) GetAffinityCookieSecure() AffinityCookieSecure {
	if pbc.AffinityCookieSecure == nil {
		return ""
	}
	return *pbc.AffinityCookieSecure
}

func (pbc *PortBackendConfig) SetAffinityCookieSecure(affinityCookieSecure AffinityCookieSecure) {
	pbc.AffinityCookieSecure = new(AffinityCookieSecure)
	*pbc.AffinityCookieSecure = affinityCookieSecure
}

func (pbc *PortBackendConfig) SetMruby(mruby string) {
	pbc.Mruby = new(string)
	*pbc.Mruby = mruby
}

func (pbc *PortBackendConfig) GetMruby() string {
	if pbc.Mruby == nil {
		return ""
	}
	return *pbc.Mruby
}

// ChecksumFile represents a file with path, its arbitrary content, and its checksum.
type ChecksumFile struct {
	Path     string
	Content  []byte
	Checksum string
}
