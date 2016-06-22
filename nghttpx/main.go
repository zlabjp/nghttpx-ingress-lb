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

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"text/template"

	"github.com/golang/glog"

	"github.com/fatih/structs"
	"github.com/ghodss/yaml"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/util/flowcontrol"
)

var (
	// Base directory that contains the mounted secrets with SSL certificates, keys and
	sslDirectory = "/etc/nghttpx-ssl"
)

type nghttpxConfiguration struct {
	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-L
	// Set the severity level of log output. <LEVEL> must be one
	// of INFO, NOTICE, WARN, ERROR and FATAL.
	LogLevel string `structs:"log-level,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--backend-read-timeout
	// Specify read timeout for backend connection.
	BackendReadTimeout string `structs:"backend-read-timeout,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--backend-write-timeout
	// Specify write timeout for backend connection.
	BackendWriteTimeout string `structs:"backend-write-timeout,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--ciphers
	// Set allowed cipher list. The format of the string is described in OpenSSL ciphers(1).
	Ciphers string `structs:"ciphers,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--tls-proto-list
	// Comma delimited list of SSL/TLS protocol to be enabled. The
	// following protocols are available: TLSv1.2, TLSv1.1 and
	// TLSv1.0. The name matching is done in case-insensitive
	// manner. The parameter must be delimited by a single comma
	// only and any white spaces are treated as a part of protocol
	// string. If the protocol list advertised by client does not
	// overlap this list, you will receive the error message
	// "unknown protocol".
	TLSProtoList string `structs:"tls-proto-list,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--no-ocsp
	// Disable OCSP stapling.
	NoOCSP bool `structs:"no-ocsp,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--accept-proxy-protocol
	// Accept PROXY protocol version 1 on frontend connection.
	AcceptProxyProtocol bool `structs:"accept-proxy-protocol,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-n
	// Set the number of worker threads.
	Workers string `structs:"workers,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--frontend-http2-window-bits
	// Sets the per-stream initial window size of HTTP/2 SPDY
	// frontend connection. For HTTP/2, the size is 2**<N>-1. For
	// SPDY, the size is 2**<N>.
	FrontendHTTP2WindowBits int `structs:"frontend-http2-window-bits,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--frontend-http2-connection-window-bits
	// Sets the per-connection window size of HTTP/2 and SPDY
	// frontend connection. For HTTP/2, the size is 2**<N>-1. For
	// SPDY, the size is 2**<N>.
	FrontendHTTP2ConnectionWindowBits int `structs:"frontend-http2-connection-window-bits,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--backend-http2-window-bits
	// Sets the initial window size of HTTP/2 backend connection
	// to 2**<N>-1.
	BackendHTTP2WindowBits int `structs:"backend-http2-window-bits,omitempty"`

	// https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--backend-http2-connection-window-bits
	// Sets the per-connection window size of HTTP/2 backend
	// connection to 2**<N>-1.
	BackendHTTP2ConnectionWindowBits int `structs:"backend-http2-connection-window-bits,omitempty"`
}

// Manager ...
type Manager struct {
	// nghttpx main configuration file path
	ConfigFile string
	// nghttpx backend configuration file path
	BackendConfigFile string

	reloadRateLimiter flowcontrol.RateLimiter

	// template loaded ready to be used to generate the nghttpx configuration file
	template *template.Template

	// template for backend configuraiton.  This is a part of
	// nghttpx configuration, and included from main one (template
	// above).  We have separate template for backend to change
	// backend configuration without reloading nghttpx if main
	// configuration has not changed.
	backendTemplate *template.Template

	reloadLock *sync.Mutex
}

// defaultConfiguration returns the default configuration contained
// in the file default-conf.json
func newDefaultNghttpxCfg() nghttpxConfiguration {
	cfg := nghttpxConfiguration{
		LogLevel:                          "NOTICE",
		BackendReadTimeout:                "1m",
		BackendWriteTimeout:               "30s",
		Ciphers:                           "ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256",
		TLSProtoList:                      "TLSv1.2",
		NoOCSP:                            true,
		AcceptProxyProtocol:               false,
		Workers:                           strconv.Itoa(runtime.NumCPU()),
		FrontendHTTP2WindowBits:           16,
		FrontendHTTP2ConnectionWindowBits: 16,
		BackendHTTP2WindowBits:            16,
		BackendHTTP2ConnectionWindowBits:  30,
	}

	if glog.V(5) {
		cfg.LogLevel = "INFO"
	}

	return cfg
}

// NewManager ...
func NewManager(kubeClient *client.Client) *Manager {
	ngx := &Manager{
		ConfigFile:        "/etc/nghttpx/nghttpx.conf",
		BackendConfigFile: "/etc/nghttpx/nghttpx-backend.conf",
		reloadLock:        &sync.Mutex{},
		reloadRateLimiter: flowcontrol.NewTokenBucketRateLimiter(0.1, 1),
	}

	ngx.createCertsDir(sslDirectory)

	ngx.loadTemplate()

	return ngx
}

func (nghttpx *Manager) createCertsDir(base string) {
	if err := os.Mkdir(base, os.ModeDir); err != nil {
		if os.IsExist(err) {
			glog.Infof("%v already exists", err)
			return
		}
		glog.Fatalf("Couldn't create directory %v: %v", base, err)
	}
}

// ConfigMapAsString returns a ConfigMap with the default nghttpx
// configuration to be used a guide to provide a custom configuration
func ConfigMapAsString() string {
	cfg := &api.ConfigMap{}
	cfg.Name = "custom-name"
	cfg.Namespace = "a-valid-namespace"
	cfg.Data = make(map[string]string)

	data := structs.Map(newDefaultNghttpxCfg())
	for k, v := range data {
		cfg.Data[k] = fmt.Sprintf("%v", v)
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		glog.Warningf("Unexpected error creating default configuration: %v", err)
		return ""
	}

	return string(out)
}
