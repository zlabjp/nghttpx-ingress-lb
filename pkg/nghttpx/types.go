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
	"runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
)

// Interface is the API to update underlying load balancer.
type Interface interface {
	// Start starts a nghttpx process using executable at path with configuration file at confPath, and waits for the process to finish.
	// If ctx is canceled, kill nghttpx process, and return.
	Start(ctx context.Context, path, confPath string) error
	// CheckAndReload checks whether the nghttpx configuration changed, and if so, make nghttpx reload its configuration.  If reloading
	// is required, and it successfully issues reloading, returns true.  If there is no need to reloading, it returns false.  On error,
	// it returns false, and non-nil error.
	CheckAndReload(ctx context.Context, ingressCfg *IngressConfig) (bool, error)
}

// IngressConfig describes an nghttpx configuration
type IngressConfig struct {
	Upstreams      []*Upstream
	TLS            bool
	DefaultTLSCred *TLSCred
	SubTLSCred     []*TLSCred
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-n
	// Set the number of worker threads.
	Workers int32
	// ExtraConfig is the extra configurations in a format that nghttpx accepts in --conf.
	ExtraConfig string
	// MrubyFileContent is the extra mruby script.  It is saved in the container disk space, and will be referenced by mruby-file from
	// configuration file.
	MrubyFile *ChecksumFile
	// HealthzMruby is the mruby script to setup healthz endpoint.  It is only enabled when deferred shutdown period is configured.
	HealthzMruby *ChecksumFile
	// HealthPort is the port for health monitor endpoint.
	HealthPort int32
	// APIPort is the port for API endpoint.
	APIPort int32
	// ConfDir is the path to the directory which includes nghttpx configuration files.
	ConfDir string
	// HTTPPort is the port to listen to for HTTP (non-TLS) request.
	HTTPPort int32
	// HTTPSPort is the port to listen to for HTTPS (TLS) request.
	HTTPSPort int32
	// FetchOCSPRespFromSecret is true if OCSP response is fetched from TLS secret.
	FetchOCSPRespFromSecret bool
	// ProxyProto toggles the use of PROXY protocol for all public-facing frontends.
	ProxyProto bool
	// HTTP3 enables HTTP/3.
	HTTP3 bool
	// QUICSecretFile is the file which contains QUIC keying materials.
	QUICSecretFile *PrivateChecksumFile
}

// NewIngressConfig returns new IngressConfig.  Workers is initialized as the number of CPU cores.
func NewIngressConfig() *IngressConfig {
	return &IngressConfig{
		Workers: int32(runtime.NumCPU()),
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
	DoNotForward         bool
}

type Affinity string

const (
	// AffinityNone indicates no session affinity.
	AffinityNone Affinity = "none"
	// AffinityIP indicates client IP address based session affinity.
	AffinityIP Affinity = "ip"
	// AffinityCookie indicates cookie based session affinity.
	AffinityCookie Affinity = "cookie"
)

type AffinityCookieSecure string

const (
	// AffinityCookieSecureAuto indicates that secure attribute is set based on underlying protocol.
	AffinityCookieSecureAuto AffinityCookieSecure = "auto"
	// AffinityCookieSecureYes indicates that secure attribute is set.
	AffinityCookieSecureYes AffinityCookieSecure = "yes"
	// AffinityCookieSecureNo indicates that secure attribute is not set.
	AffinityCookieSecureNo AffinityCookieSecure = "no"
)

type Protocol string

const (
	// ProtocolH2 indicates HTTP/2 protocol.
	ProtocolH2 Protocol = "h2"
	// ProtocolH1 indicates HTTP/1.1 protocol.
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
	Group    string
	Weight   uint32
}

// TLSCred stores TLS server private key, certificate file path, and optionally OCSP response.  OCSP response must be DER encoded byte
// string.
type TLSCred struct {
	Key      PrivateChecksumFile
	Cert     ChecksumFile
	OCSPResp *ChecksumFile
}

// NewDefaultServer return an UpstreamServer to be use as default server that returns 503.
func NewDefaultServer() UpstreamServer {
	return UpstreamServer{
		Address:  "127.0.0.1",
		Port:     "8181",
		Protocol: ProtocolH1,
	}
}

// PortBackendConfig is a backend configuration obtained from ingress annotation, specified per service port
type PortBackendConfig struct {
	// backend application protocol.  At the moment, this should be either ProtocolH2 or ProtocolH1.
	Proto *Protocol `json:"proto,omitempty"`
	// true if backend connection requires TLS
	TLS *bool `json:"tls,omitempty"`
	// SNI hostname for backend TLS connection
	SNI *string `json:"sni,omitempty"`
	// DNS is true if backend hostname is resolved dynamically rather than start up or configuration reloading.
	DNS *bool `json:"dns,omitempty"`
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
	// DoNotForward, if set to true, does not forward a request to a backend.
	DoNotForward *bool `json:"doNotForward,omitempty"`
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

func (pc *PathConfig) GetDoNotForward() bool {
	if pc == nil || pc.DoNotForward == nil {
		return false
	}
	return *pc.DoNotForward
}

func (pc *PathConfig) SetDoNotForward(b bool) {
	pc.DoNotForward = new(bool)
	*pc.DoNotForward = b
}

// ChecksumFile represents a file with path, its arbitrary content, and its checksum.
type ChecksumFile struct {
	Path     string
	Content  []byte
	Checksum []byte
}

// PrivateChecksumFile is a kind of ChecksumFile and it contains private data which should not be spilled out into log.
type PrivateChecksumFile ChecksumFile

func (c PrivateChecksumFile) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer

	b.WriteString(`{"Path":`)

	a, err := json.Marshal(c.Path)
	if err != nil {
		return nil, err
	}

	b.Write(a)
	b.WriteString(`,"Content":"[redacted]","Checksum":`)

	a, err = json.Marshal(c.Checksum)
	if err != nil {
		return nil, err
	}

	b.Write(a)
	b.WriteString(`}`)

	return b.Bytes(), nil
}
