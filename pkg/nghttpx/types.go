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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
)

// IngressConfig describes an nghttpx configuration
type IngressConfig struct {
	Upstreams      []*Upstream
	TLS            bool
	DefaultTLSCred *TLSCred
	SubTLSCred     []*TLSCred
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-n
	// Set the number of worker threads.
	Workers int32
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-worker-process-grace-shutdown-period
	// WorkerProcessGraceShutdownPeriod is the maximum period for an nghttpx worker process to terminate gracefully.
	WorkerProcessGraceShutdownPeriod time.Duration
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-max-worker-processes
	// MaxWorkerProcesses is the maximum number of nghttpx worker processes which are spawned in every configuration reload.
	MaxWorkerProcesses int32
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
	// ProxyProto toggles the use of PROXY protocol for all public-facing frontends.
	ProxyProto bool
	// HTTP3 enables HTTP/3.
	HTTP3 bool
	// QUICSecretFile is the file which contains QUIC keying materials.
	QUICSecretFile *PrivateChecksumFile
	// ShareTLSTicketKey, if true, shares TLS ticket key among ingress controllers via Secret.
	ShareTLSTicketKey bool
	// TLSTicketKeyFiles is the list of files that contain TLS ticket key.
	TLSTicketKeyFiles []*PrivateChecksumFile
}

// Upstream describes an nghttpx upstream
type Upstream struct {
	Name                     string
	GroupVersionKind         schema.GroupVersionKind
	Source                   types.NamespacedName
	Host                     string
	Path                     string
	Backends                 []Backend
	RedirectIfNotTLS         bool
	Mruby                    *ChecksumFile
	Affinity                 Affinity
	AffinityCookieName       string
	AffinityCookiePath       string
	AffinityCookieSecure     AffinityCookieSecure
	AffinityCookieStickiness AffinityCookieStickiness
	ReadTimeout              *metav1.Duration
	WriteTimeout             *metav1.Duration
	DoNotForward             bool
}

func (ups *Upstream) String() string {
	if ups == nil {
		return "(nil)"
	}

	return ups.Name
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

type AffinityCookieStickiness string

const (
	// AffinityCookieStickinessLoose indicates loose affinity cookie stickiness.
	AffinityCookieStickinessLoose AffinityCookieStickiness = "loose"
	// AffinityCookieStickinessStrict indicates strict affinity cookie stickiness.
	AffinityCookieStickinessStrict AffinityCookieStickiness = "strict"
)

type Protocol string

const (
	// ProtocolH2 indicates HTTP/2 protocol.
	ProtocolH2 Protocol = "h2"
	// ProtocolH1 indicates HTTP/1.1 protocol.
	ProtocolH1 Protocol = "http/1.1"
)

// Backend describes a server in an nghttpx upstream
type Backend struct {
	Address  string
	Port     string
	Protocol Protocol
	TLS      bool
	SNI      string
	DNS      bool
	Group    string
	Weight   uint32
}

// TLSCred stores TLS server private key and certificate file path.
type TLSCred struct {
	Name string
	Key  PrivateChecksumFile
	Cert ChecksumFile
}

// NewDefaultBackend return a Backend to be use as default server that returns 503.
func NewDefaultBackend() Backend {
	return Backend{
		Address:  "127.0.0.1",
		Port:     "8181",
		Protocol: ProtocolH1,
	}
}

// BackendConfigMapper is a convenient object for querying BackendConfig for given service and port.
type BackendConfigMapper struct {
	DefaultBackendConfig *BackendConfig
	BackendConfigMapping BackendConfigMapping
}

// NewBackendConfigMapper returns new BackendConfigMapper.
func NewBackendConfigMapper(defaultBackendConfig *BackendConfig, backendConfigMapping BackendConfigMapping) *BackendConfigMapper {
	return &BackendConfigMapper{
		DefaultBackendConfig: defaultBackendConfig,
		BackendConfigMapping: backendConfigMapping,
	}
}

// ConfigFor returns BackendConfig for given svc and port.  svc is Service name, and port is either a named Service port or a numeric port
// number.
func (bcm *BackendConfigMapper) ConfigFor(ctx context.Context, svc, port string) *BackendConfig {
	c := bcm.BackendConfigMapping[svc][port]
	if c != nil {
		return c
	}

	c = new(BackendConfig)

	if bcm.DefaultBackendConfig != nil {
		ApplyDefaultBackendConfig(ctx, c, bcm.DefaultBackendConfig)
	}

	return c
}

type BackendConfigMapping map[string]map[string]*BackendConfig

// BackendConfig is a backend configuration obtained from ingress annotation, specified per service port
type BackendConfig struct {
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

func (pbc *BackendConfig) GetProto() Protocol {
	if pbc.Proto == nil {
		return ProtocolH1
	}

	return *pbc.Proto
}

func (pbc *BackendConfig) SetProto(proto Protocol) {
	pbc.Proto = new(Protocol)
	*pbc.Proto = proto
}

func (pbc *BackendConfig) GetTLS() bool {
	if pbc.TLS == nil {
		return false
	}

	return *pbc.TLS
}

func (pbc *BackendConfig) SetTLS(tls bool) {
	pbc.TLS = new(bool)
	*pbc.TLS = tls
}

func (pbc *BackendConfig) GetSNI() string {
	if pbc.SNI == nil {
		return ""
	}

	return *pbc.SNI
}

func (pbc *BackendConfig) SetSNI(sni string) {
	pbc.SNI = new(string)
	*pbc.SNI = sni
}

func (pbc *BackendConfig) GetDNS() bool {
	if pbc.DNS == nil {
		return false
	}

	return *pbc.DNS
}

func (pbc *BackendConfig) SetDNS(dns bool) {
	pbc.DNS = new(bool)
	*pbc.DNS = dns
}

func (pbc *BackendConfig) GetWeight() uint32 {
	if pbc == nil || pbc.Weight == nil {
		return 0
	}

	return *pbc.Weight
}

func (pbc *BackendConfig) SetWeight(weight uint32) {
	pbc.Weight = new(uint32)
	*pbc.Weight = weight
}

// PathConfigMapper is a convenient object for querying PathConfig for given host and path.
type PathConfigMapper struct {
	DefaultPathConfig *PathConfig
	PathConfigMapping PathConfigMapping
}

// NewPathConfigMapper returns new PathConfigMapper.
func NewPathConfigMapper(defaultPathConfig *PathConfig, pathConfigMapping PathConfigMapping) *PathConfigMapper {
	return &PathConfigMapper{
		DefaultPathConfig: defaultPathConfig,
		PathConfigMapping: pathConfigMapping,
	}
}

// ConfigFor returns PathConfig for given host and path.
func (pcm *PathConfigMapper) ConfigFor(host, path string) *PathConfig {
	return ResolvePathConfig(host, path, pcm.DefaultPathConfig, pcm.PathConfigMapping)
}

type PathConfigMapping map[string]*PathConfig

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
	// AffinityCookieStickiness controls the stickiness of affinity cookie.
	AffinityCookieStickiness *AffinityCookieStickiness `json:"affinityCookieStickiness,omitempty"`
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

func (pc *PathConfig) GetAffinityCookieStickiness() AffinityCookieStickiness {
	if pc == nil || pc.AffinityCookieStickiness == nil {
		return AffinityCookieStickinessLoose
	}

	return *pc.AffinityCookieStickiness
}

func (pc *PathConfig) SetAffinityCookieStickiness(affinityCookieStickiness AffinityCookieStickiness) {
	pc.AffinityCookieStickiness = new(AffinityCookieStickiness)
	*pc.AffinityCookieStickiness = affinityCookieStickiness
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

func (c *ChecksumFile) GetPath() string {
	if c == nil {
		return ""
	}

	return c.Path
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
