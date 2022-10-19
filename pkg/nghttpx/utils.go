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
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

const (
	// NghttpxExtraConfigKey is a field name of extra nghttpx configuration in ConfigMap.
	NghttpxExtraConfigKey = "nghttpx-conf"
	// NghttpxMrubyFileContentKey is a field name of mruby script in ConfigMap.
	NghttpxMrubyFileContentKey = "nghttpx-mruby-file-content"
)

// ReadConfig obtains the configuration defined by the user merged with the defaults.
func ReadConfig(ingConfig *IngressConfig, config *corev1.ConfigMap) {
	ingConfig.ExtraConfig = config.Data[NghttpxExtraConfigKey]
	if mrubyFileContent, ok := config.Data[NghttpxMrubyFileContentKey]; ok {
		b := []byte(mrubyFileContent)
		ingConfig.MrubyFile = &ChecksumFile{
			Path:     MrubyRbPath(ingConfig.ConfDir),
			Content:  b,
			Checksum: Checksum(b),
		}
	}
}

// needsReload first checks that configuration is changed.  filename is the current configuration file path, and data includes the new
// configuration.  If they differ, we write data into filename, and return true.  Otherwise, just return false without altering existing
// file.
func needsReload(filename string, newCfg []byte) (bool, error) {
	in, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	oldCfg, err := io.ReadAll(in)
	in.Close()
	if err != nil {
		return false, err
	}

	if bytes.Equal(oldCfg, newCfg) {
		return false, nil
	}

	if klog.V(2).Enabled() {
		dData, err := diff(oldCfg, newCfg)
		if err != nil {
			klog.Errorf("error computing diff: %s", err)
			return true, nil
		}
		klog.Infof("nghttpx configuration diff %s\n%v", filename, dData)
	}

	return true, nil
}

func diff(b1, b2 []byte) (string, error) {
	d := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(b1)),
		B:        difflib.SplitLines(string(b2)),
		FromFile: "currnet",
		ToFile:   "new",
		Context:  3,
	}
	return difflib.GetUnifiedDiffString(d)
}

// FixupBackendConfig validates config, and fixes the invalid values inside it.
func FixupBackendConfig(config *BackendConfig) {
	switch config.GetProto() {
	case ProtocolH2, ProtocolH1:
		// OK
	default:
		klog.Errorf("unrecognized backend protocol %q", config.GetProto())
		config.SetProto(ProtocolH1)
	}
	if config.Weight != nil {
		weight := config.GetWeight()
		switch {
		case weight < 1:
			klog.Errorf("invalid weight %v.  It must be [1, 256], inclusive", weight)
			config.SetWeight(1)
		case weight > 256:
			klog.Errorf("invalid weight %v.  It must be [1, 256], inclusive", weight)
			config.SetWeight(256)
		}
	}
}

// ApplyDefaultBackendConfig applies default field value specified in defaultConfig to config if a corresponding field is missing.
func ApplyDefaultBackendConfig(config *BackendConfig, defaultConfig *BackendConfig) {
	klog.V(4).Info("Applying default-backend-config annotation")
	if defaultConfig.Proto != nil && config.Proto == nil {
		config.SetProto(*defaultConfig.Proto)
	}
	if defaultConfig.TLS != nil && config.TLS == nil {
		config.SetTLS(*defaultConfig.TLS)
	}
	if defaultConfig.SNI != nil && config.SNI == nil {
		config.SetSNI(*defaultConfig.SNI)
	}
	if defaultConfig.DNS != nil && config.DNS == nil {
		config.SetDNS(*defaultConfig.DNS)
	}
	if defaultConfig.Weight != nil && config.Weight == nil {
		config.SetWeight(*defaultConfig.Weight)
	}
}

// FixupPathConfig validates config and fixes the invalid values inside it.
func FixupPathConfig(config *PathConfig) {
	switch config.GetAffinity() {
	case AffinityNone, AffinityIP, AffinityCookie:
		// OK
	default:
		klog.Errorf("unsupported affinity method %v", config.GetAffinity())
		config.SetAffinity(AffinityNone)
	}
	switch config.GetAffinityCookieSecure() {
	case AffinityCookieSecureAuto, AffinityCookieSecureYes, AffinityCookieSecureNo:
		// OK
	default:
		klog.Errorf("unsupported affinity cookie secure %v", config.GetAffinityCookieSecure())
		config.SetAffinityCookieSecure(AffinityCookieSecureAuto)
	}
	switch config.GetAffinityCookieStickiness() {
	case AffinityCookieStickinessLoose, AffinityCookieStickinessStrict:
		// OK
	default:
		klog.Errorf("unsupported affinity cookie stickiness %v", config.GetAffinityCookieStickiness())
		config.SetAffinityCookieStickiness(AffinityCookieStickinessLoose)
	}
}

func ApplyDefaultPathConfig(config *PathConfig, defaultConfig *PathConfig) {
	klog.V(4).Info("Applying default-path-config annotation")
	if defaultConfig.Mruby != nil && config.Mruby == nil {
		config.SetMruby(*defaultConfig.Mruby)
	}
	if defaultConfig.Affinity != nil && config.Affinity == nil {
		config.SetAffinity(*defaultConfig.Affinity)
	}
	if defaultConfig.AffinityCookieName != nil && config.AffinityCookieName == nil {
		config.SetAffinityCookieName(*defaultConfig.AffinityCookieName)
	}
	if defaultConfig.AffinityCookiePath != nil && config.AffinityCookiePath == nil {
		config.SetAffinityCookiePath(*defaultConfig.AffinityCookiePath)
	}
	if defaultConfig.AffinityCookieSecure != nil && config.AffinityCookieSecure == nil {
		config.SetAffinityCookieSecure(*defaultConfig.AffinityCookieSecure)
	}
	if defaultConfig.AffinityCookieStickiness != nil && config.AffinityCookieStickiness == nil {
		config.SetAffinityCookieStickiness(*defaultConfig.AffinityCookieStickiness)
	}
	if defaultConfig.ReadTimeout != nil && config.ReadTimeout == nil {
		config.SetReadTimeout(*defaultConfig.ReadTimeout)
	}
	if defaultConfig.WriteTimeout != nil && config.WriteTimeout == nil {
		config.SetWriteTimeout(*defaultConfig.WriteTimeout)
	}
	if defaultConfig.RedirectIfNotTLS != nil && config.RedirectIfNotTLS == nil {
		config.SetRedirectIfNotTLS(*defaultConfig.RedirectIfNotTLS)
	}
	if defaultConfig.DoNotForward != nil && config.DoNotForward == nil {
		config.SetDoNotForward(*defaultConfig.DoNotForward)
	}
}

// ResolvePathConfig returns a PathConfig which should be used for the pattern denoted by host and path.
func ResolvePathConfig(host, path string, defaultPathConfig *PathConfig, pathConfig PathConfigMapping) *PathConfig {
	key := host + path
	if c, ok := pathConfig[key]; ok {
		return c
	}
	return defaultPathConfig
}

func WriteFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, "nghttpx")
	if err != nil {
		return err
	}

	if _, err := tempFile.Write(content); err != nil {
		tempFile.Close()
		os.Remove(tempFile.Name())
		return err
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempFile.Name())
		return err
	}

	return os.Rename(tempFile.Name(), path)
}

// Checksum calculates and returns checksum of b in hex string.
func Checksum(b []byte) []byte {
	h := sha256.New()
	h.Write(b) // nolint: errcheck
	return h.Sum(nil)
}

// ConfigPath returns the path to nghttpx configuration file.
func ConfigPath(dir string) string {
	return filepath.Join(dir, "nghttpx.conf")
}

// BackendConfigPath returns the path to nghttpx backend configuration file.
func BackendConfigPath(dir string) string {
	return filepath.Join(dir, "nghttpx-backend.conf")
}

// MrubyRbPath returns the path to nghttpx mruby.rb file.
func MrubyRbPath(dir string) string {
	return filepath.Join(dir, "mruby.rb")
}

// MkdirAll creates directory given as path.
func MkdirAll(path string) error {
	return os.MkdirAll(path, os.ModeDir)
}

// nghttpxDuration serializes d in nghttpx DURATION format.  Currently, it formats d in milliseconds if it has fraction of a second.
// Otherwise, it formats d in seconds.
func nghttpxDuration(d time.Duration) string {
	d = d.Round(time.Millisecond)
	msec := d.Nanoseconds() / int64(time.Millisecond)
	if msec%1000 == 0 {
		return strconv.FormatInt(msec/1000, 10)
	}
	return fmt.Sprintf("%vms", msec)
}
