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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// HealthzMruby is the mruby script to setup healthz endpoint.  It is only enabled when deferred shutdown period is configured.
	HealthzMruby *ChecksumFile
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
	// HealthzPort is the port of backend for mruby script healthz endpoint.  mruby script returns response, therefore this backend is
	// not used, but nghttpx needs functional backend for this purpose.
	HealthzPort int
}

// NewIngressConfig returns new IngressConfig.  Workers is initialized as the number of CPU cores.
func NewIngressConfig() *IngressConfig {
	return &IngressConfig{
		Workers: strconv.Itoa(runtime.NumCPU()),
	}
}

// Upstream describes an nghttpx upstream
type Upstream struct {
	Name                 string
	Host                 string
	Path                 string
	Backends             []UpstreamServer
	RedirectIfNotTLS     bool
	Mruby                *ChecksumFile
	Affinity             Affinity
	AffinityCookieName   string
	AffinityCookiePath   string
	AffinityCookieSecure AffinityCookieSecure
	ReadTimeout          *metav1.Duration
	WriteTimeout         *metav1.Duration
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
	Address  string
	Port     string
	Protocol Protocol
	TLS      bool
	SNI      string
	DNS      bool
	// Deprecated.  Use Upstream instead.
	Affinity Affinity
	// Deprecated.  Use Upstream instead.
	AffinityCookieName string
	// Deprecated.  Use Upstream instead.
	AffinityCookiePath string
	// Deprecated.  Use Upstream instead.
	AffinityCookieSecure AffinityCookieSecure
	Group                string
	Weight               uint32
}

// TLS server private key, certificate file path, and optionally OCSP response.  OCSP response must be DER encoded byte string.
type TLSCred struct {
	Key      ChecksumFile
	Cert     ChecksumFile
	OCSPResp *ChecksumFile
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
	// (Deprecated) Affinity is session affinity method nghttpx supports.  See affinity parameter in backend option of nghttpx.  This
	// field is deprecated.  Use PathConfig instead.
	Affinity *Affinity `json:"affinity,omitempty"`
	// (Deprecated) AffinityCookieName is a name of cookie to use for cookie-based session affinity.  This field is deprecated.  Use
	// PathConfig instead.
	AffinityCookieName *string `json:"affinityCookieName,omitempty"`
	// (Deprecated) AffinityCookiePath is a path of cookie for cookie-based session affinity.  This field is deprecated.  Use PathConfig
	// instead.
	AffinityCookiePath *string `json:"affinityCookiePath,omitempty"`
	// (Deprecated) AffinityCookieSecure controls whether Secure attribute is added to session affinity cookie.  This field is deleted.
	// Use PathConfig instead.
	AffinityCookieSecure *AffinityCookieSecure `json:"affinityCookieSecure,omitempty"`
	// Weight is a weight of backend selection.
	Weight *uint32 `json:"weight,omitempty"`
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

func (pbc *PortBackendConfig) GetWeight() uint32 {
	if pbc == nil || pbc.Weight == nil {
		return 0
	}
	return *pbc.Weight
}

func (pbc *PortBackendConfig) SetWeight(weight uint32) {
	pbc.Weight = new(uint32)
	*pbc.Weight = weight
}

// PathConfig is per-pattern configuration obtained from Ingress annotation, specified per host and path pattern.
type PathConfig struct {
	// Mruby is mruby script
	Mruby *string `json:"mruby,omitempty"`
	// Affinity is session affinity method nghttpx supports.  See affinity parameter in backend option of nghttpx.
	Affinity *Affinity `json:"affinity,omitempty"`
	// AffinityCookieName is a name of cookie to use for cookie-based session affinity.
	AffinityCookieName *string `json:"affinityCookieName,omitempty"`
	// AffinityCookiePath is a path of cookie for cookie-based session affinity.
	AffinityCookiePath *string `json:"affinityCookiePath,omitempty"`
	// AffinityCookieSecure controls whether Secure attribute is added to session affinity cookie.
	AffinityCookieSecure *AffinityCookieSecure `json:"affinityCookieSecure,omitempty"`
	// ReadTimeout is a read timeout when this path is selected.
	ReadTimeout *metav1.Duration `json:"readTimeout,omitempty"`
	// WriteTimeout is a write timeout when this path is selected.
	WriteTimeout *metav1.Duration `json:"writeTimeout,omitempty"`
	// RedirectIfNotTLS, if set to true, redirects cleartext HTTP to HTTPS.
	RedirectIfNotTLS *bool `json:"redirectIfNotTLS,omitempty"`
}

func (pc *PathConfig) GetMruby() string {
	if pc == nil || pc.Mruby == nil {
		return ""
	}
	return *pc.Mruby
}

func (pc *PathConfig) SetMruby(mruby string) {
	pc.Mruby = new(string)
	*pc.Mruby = mruby
}

func (pc *PathConfig) GetAffinity() Affinity {
	if pc == nil || pc.Affinity == nil {
		return AffinityNone
	}
	return *pc.Affinity
}

func (pc *PathConfig) SetAffinity(affinity Affinity) {
	pc.Affinity = new(Affinity)
	*pc.Affinity = affinity
}

func (pc *PathConfig) GetAffinityCookieName() string {
	if pc == nil || pc.AffinityCookieName == nil {
		return ""
	}
	return *pc.AffinityCookieName
}

func (pc *PathConfig) SetAffinityCookieName(affinityCookieName string) {
	pc.AffinityCookieName = new(string)
	*pc.AffinityCookieName = affinityCookieName
}

func (pc *PathConfig) GetAffinityCookiePath() string {
	if pc == nil || pc.AffinityCookiePath == nil {
		return ""
	}
	return *pc.AffinityCookiePath
}

func (pc *PathConfig) SetAffinityCookiePath(affinityCookiePath string) {
	pc.AffinityCookiePath = new(string)
	*pc.AffinityCookiePath = affinityCookiePath
}

func (pc *PathConfig) GetAffinityCookieSecure() AffinityCookieSecure {
	if pc == nil || pc.AffinityCookieSecure == nil {
		return AffinityCookieSecureAuto
	}
	return *pc.AffinityCookieSecure
}

func (pc *PathConfig) SetAffinityCookieSecure(affinityCookieSecure AffinityCookieSecure) {
	pc.AffinityCookieSecure = new(AffinityCookieSecure)
	*pc.AffinityCookieSecure = affinityCookieSecure
}

func (pc *PathConfig) GetReadTimeout() *metav1.Duration {
	if pc == nil {
		return nil
	}
	return pc.ReadTimeout
}

func (pc *PathConfig) SetReadTimeout(readTimeout metav1.Duration) {
	pc.ReadTimeout = new(metav1.Duration)
	*pc.ReadTimeout = readTimeout
}

func (pc *PathConfig) GetWriteTimeout() *metav1.Duration {
	if pc == nil {
		return nil
	}
	return pc.WriteTimeout
}

func (pc *PathConfig) SetWriteTimeout(writeTimeout metav1.Duration) {
	pc.WriteTimeout = new(metav1.Duration)
	*pc.WriteTimeout = writeTimeout
}

func (pc *PathConfig) GetRedirectIfNotTLS() bool {
	if pc == nil || pc.RedirectIfNotTLS == nil {
		return true
	}
	return *pc.RedirectIfNotTLS
}

func (pc *PathConfig) SetRedirectIfNotTLS(b bool) {
	pc.RedirectIfNotTLS = new(bool)
	*pc.RedirectIfNotTLS = b
}

// ChecksumFile represents a file with path, its arbitrary content, and its checksum.
type ChecksumFile struct {
	Path     string
	Content  []byte
	Checksum string
}
