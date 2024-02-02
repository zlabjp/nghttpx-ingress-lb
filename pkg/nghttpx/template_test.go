package nghttpx

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func hexMustDecodeString(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// TestLoadBalancerGenerateCfg verifies LoadBalancer.generateCfg.
func TestLoadBalancerGenerateCfg(t *testing.T) {
	tests := []struct {
		desc              string
		ingConfig         *IngressConfig
		wantMainConfig    string
		wantBackendConfig string
	}{
		{
			desc: "With just HTTP port",
			ingConfig: &IngressConfig{
				HTTPPort: 80,
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With HTTP and HTTPS ports",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# just listen 443 to gain port 443, so that we can always bind that address.
frontend=*,443;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With default TLS certificate",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				TLS:       true,
				DefaultTLSCred: &TLSCred{
					Key: PrivateChecksumFile{
						Path:     "/tls/server.key",
						Content:  []byte("key"),
						Checksum: hexMustDecodeString("2c70e12b7a0646f92279f427c7b38e7334d8e5389cff167a1dc30e73f826b683"),
					},
					Cert: ChecksumFile{
						Path:     "/tls/server.crt",
						Content:  []byte("cert"),
						Checksum: hexMustDecodeString("06298432e8066b29e2223bcc23aa9504b56ae508fabf3435508869b9c3190e22"),
					},
				},
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# HTTPS port
frontend=*,443
# Default TLS credential
private-key-file=/tls/server.key
certificate-file=/tls/server.crt
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With extra TLS certificates",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				TLS:       true,
				DefaultTLSCred: &TLSCred{
					Key: PrivateChecksumFile{
						Path:     "/tls/server.key",
						Content:  []byte("key"),
						Checksum: hexMustDecodeString("2c70e12b7a0646f92279f427c7b38e7334d8e5389cff167a1dc30e73f826b683"),
					},
					Cert: ChecksumFile{
						Path:     "/tls/server.crt",
						Content:  []byte("cert"),
						Checksum: hexMustDecodeString("06298432e8066b29e2223bcc23aa9504b56ae508fabf3435508869b9c3190e22"),
					},
				},
				SubTLSCred: []*TLSCred{
					{
						Key: PrivateChecksumFile{
							Path:     "/tls/server2.key",
							Content:  []byte("key2"),
							Checksum: hexMustDecodeString("b10253764c8b233fb37542e23401c7b450e5a6f9751f3b5a014f6f67e8bc999d"),
						},
						Cert: ChecksumFile{
							Path:     "/tls/server2.crt",
							Content:  []byte("cert2"),
							Checksum: hexMustDecodeString("cdf9e092139ce78806e2fbabc732bd2d322e964fa93a10a8e0155e673aa96737"),
						},
					},
					{
						Key: PrivateChecksumFile{
							Path:     "/tls/server3.key",
							Content:  []byte("key3"),
							Checksum: hexMustDecodeString("f576104eebeab09651d83acffc77c8b8c6eaa4b767aeab24d7da80f83f51d865"),
						},
						Cert: ChecksumFile{
							Path:     "/tls/server3.crt",
							Content:  []byte("cert3"),
							Checksum: hexMustDecodeString("9bffb43d004b76efd8c8bb6086d37b28737b85b455021ba0f20210062dda59ed"),
						},
					},
				},
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# HTTPS port
frontend=*,443
# Default TLS credential
private-key-file=/tls/server.key
certificate-file=/tls/server.crt
subcert=/tls/server2.key:/tls/server2.crt
subcert=/tls/server3.key:/tls/server3.crt
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With ExtraConfig",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				ExtraConfig: `log-level=INFO
foo=bar`,
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# just listen 443 to gain port 443, so that we can always bind that address.
frontend=*,443;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# ExtraConfig
log-level=INFO
foo=bar
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With MrubyFile",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				MrubyFile: &ChecksumFile{
					Path:     "/mruby.rb",
					Content:  []byte("mruby"),
					Checksum: hexMustDecodeString("a3b3c8e58869776c6ab92b545454eea9e8db3b460bc1b6b9684323e3bb70a005"),
				},
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# just listen 443 to gain port 443, so that we can always bind that address.
frontend=*,443;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# mruby file
# checksum: a3b3c8e58869776c6ab92b545454eea9e8db3b460bc1b6b9684323e3bb70a005
mruby-file=/mruby.rb
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With FetchOCSPRespFromSecret",
			ingConfig: &IngressConfig{
				HTTPPort:                80,
				HTTPSPort:               443,
				FetchOCSPRespFromSecret: true,
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# just listen 443 to gain port 443, so that we can always bind that address.
frontend=*,443;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/cat-ocsp-resp
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With HTTP/3",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				TLS:       true,
				DefaultTLSCred: &TLSCred{
					Key: PrivateChecksumFile{
						Path:     "/tls/server.key",
						Content:  []byte("key"),
						Checksum: hexMustDecodeString("2c70e12b7a0646f92279f427c7b38e7334d8e5389cff167a1dc30e73f826b683"),
					},
					Cert: ChecksumFile{
						Path:     "/tls/server.crt",
						Content:  []byte("cert"),
						Checksum: hexMustDecodeString("06298432e8066b29e2223bcc23aa9504b56ae508fabf3435508869b9c3190e22"),
					},
				},
				HTTP3: true,
				QUICSecretFile: &PrivateChecksumFile{
					Path:     "/quic-secrets.txt",
					Content:  []byte("quic-secrets"),
					Checksum: hexMustDecodeString("d1a7d1680092bbbcbfed9bf40ddfbde8434dcff43d47f717c9dc36a2624c5dbf"),
				},
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# HTTPS port
frontend=*,443
# HTTP/3
frontend=*,443;quic
# checksum: d1a7d1680092bbbcbfed9bf40ddfbde8434dcff43d47f717c9dc36a2624c5dbf
frontend-quic-secret-file=/quic-secrets.txt
altsvc=h3,443,,,ma=3600
altsvc=h3-29,443,,,ma=3600
http2-altsvc=h3,443,,,ma=3600
http2-altsvc=h3-29,443,,,ma=3600
# Default TLS credential
private-key-file=/tls/server.key
certificate-file=/tls/server.crt
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With HTTP/3 but missing QUICSecretFile",
			ingConfig: &IngressConfig{
				HTTPPort:  80,
				HTTPSPort: 443,
				TLS:       true,
				DefaultTLSCred: &TLSCred{
					Key: PrivateChecksumFile{
						Path:     "/tls/server.key",
						Content:  []byte("key"),
						Checksum: hexMustDecodeString("2c70e12b7a0646f92279f427c7b38e7334d8e5389cff167a1dc30e73f826b683"),
					},
					Cert: ChecksumFile{
						Path:     "/tls/server.crt",
						Content:  []byte("cert"),
						Checksum: hexMustDecodeString("06298432e8066b29e2223bcc23aa9504b56ae508fabf3435508869b9c3190e22"),
					},
				},
				HTTP3: true,
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# HTTP port
frontend=*,80;no-tls
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# HTTPS port
frontend=*,443
# HTTP/3
frontend=*,443;quic
altsvc=h3,443,,,ma=3600
altsvc=h3-29,443,,,ma=3600
http2-altsvc=h3,443,,,ma=3600
http2-altsvc=h3-29,443,,,ma=3600
# Default TLS credential
private-key-file=/tls/server.key
certificate-file=/tls/server.crt
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
`,
		},
		{
			desc: "With HealthzMruby",
			ingConfig: &IngressConfig{
				HealthzMruby: &ChecksumFile{
					Path:     "/healthz.rb",
					Content:  []byte("healthz"),
					Checksum: hexMustDecodeString("2320e78d12f94b53ac43a3ffaac709c6e9c16907a4a37264ab94fe34163af0a9"),
				},
				Upstreams: []*Upstream{
					{
						Name:     "foo",
						Host:     "example.com",
						Path:     "/",
						Affinity: AffinityNone,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;affinity=none
backend=192.168.0.2,80;example.com/;proto=h2;affinity=none
# healthz
backend=127.0.0.1,9999;/nghttpx-healthz;mruby=/healthz.rb;dnf
`,
		},
		{
			desc: "With all backend options",
			ingConfig: &IngressConfig{
				Upstreams: []*Upstream{
					{
						Name:                     "foo",
						Host:                     "example.com",
						Path:                     "/",
						Affinity:                 AffinityCookie,
						AffinityCookieName:       "sticky",
						AffinityCookiePath:       "/",
						AffinityCookieSecure:     AffinityCookieSecureAuto,
						AffinityCookieStickiness: AffinityCookieStickinessStrict,
						RedirectIfNotTLS:         true,
						Mruby: &ChecksumFile{
							Path:     "/mruby.rb",
							Content:  []byte("mruby"),
							Checksum: hexMustDecodeString("a3b3c8e58869776c6ab92b545454eea9e8db3b460bc1b6b9684323e3bb70a005"),
						},
						ReadTimeout:  &metav1.Duration{Duration: 3 * time.Minute},
						WriteTimeout: &metav1.Duration{Duration: 5 * time.Minute},
						DoNotForward: true,
						Backends: []Backend{
							{
								Address:  "192.168.0.1",
								Port:     "8080",
								Protocol: ProtocolH2,
								TLS:      true,
								SNI:      "foo.example.com",
								DNS:      true,
								Group:    "group1",
								Weight:   100,
							},
							{
								Address:  "192.168.0.2",
								Port:     "80",
								Protocol: ProtocolH2,
							},
						},
					},
				},
				Workers:                          8,
				WorkerProcessGraceShutdownPeriod: 30 * time.Second,
				MaxWorkerProcesses:               111,
			},
			wantMainConfig: `accesslog-file=/dev/stdout
include=/nghttpx-backend.conf
# API endpoint
frontend=127.0.0.1,0;api;no-tls
# for health check
frontend=127.0.0.1,0;healthmon;no-tls
# default configuration by controller
workers=8
worker-process-grace-shutdown-period=30
max-worker-processes=111
# OCSP
fetch-ocsp-response-file=/fetch-ocsp-response
`,
			wantBackendConfig: `# foo
backend=192.168.0.1,8080;example.com/;proto=h2;tls;sni=foo.example.com;dns;group=group1;group-weight=100;affinity=cookie;affinity-cookie-name=sticky;affinity-cookie-path=/;affinity-cookie-secure=auto;affinity-cookie-stickiness=strict;redirect-if-not-tls;mruby=/mruby.rb;read-timeout=180;write-timeout=300;dnf
backend=192.168.0.2,80;example.com/;proto=h2;affinity=cookie;affinity-cookie-name=sticky;affinity-cookie-path=/;affinity-cookie-secure=auto;affinity-cookie-stickiness=strict;redirect-if-not-tls;mruby=/mruby.rb;read-timeout=180;write-timeout=300;dnf
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			lb := &LoadBalancer{}
			lb.loadTemplate()

			mainConfig, backendConfig, err := lb.generateCfg(context.Background(), tt.ingConfig)
			if err != nil {
				t.Fatalf("lb.generateCfg: %v", err)
			}

			if got, want := string(mainConfig), tt.wantMainConfig; got != want {
				t.Errorf("mainConfig =\n%v, want\n%v", got, want)
			}

			if got, want := string(backendConfig), tt.wantBackendConfig; got != want {
				t.Errorf("backendConfig =\n%v, want\n%v", got, want)
			}
		})
	}
}
