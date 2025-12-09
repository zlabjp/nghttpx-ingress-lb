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
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/go-cmp/cmp"
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
func needsReload(ctx context.Context, filename string, newCfg []byte) (bool, error) {
	log := klog.FromContext(ctx)

	oldCfg, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}

		return false, err
	}

	if bytes.Equal(oldCfg, newCfg) {
		return false, nil
	}

	if log := log.V(2); log.Enabled() {
		log.Info("nghttpx configuration diff", "path", filename, "diff", cmp.Diff(oldCfg, newCfg))
	}

	return true, nil
}

// FixupBackendConfig validates config, and fixes the invalid values inside it.
func FixupBackendConfig(ctx context.Context, config *BackendConfig) {
	log := klog.FromContext(ctx)

	switch config.GetProto() {
	case ProtocolH2, ProtocolH1:
		// OK
	default:
		log.Error(nil, "Unrecognized backend protocol", "protocol", config.GetProto())
		config.SetProto(ProtocolH1)
	}

	if config.Weight != nil {
		weight := config.GetWeight()

		switch {
		case weight < 1:
			log.Error(nil, "Invalid weight.  It must be [1, 256], inclusive", "weight", weight)
			config.SetWeight(1)
		case weight > 256:
			log.Error(nil, "Invalid weight.  It must be [1, 256], inclusive", "weight", weight)
			config.SetWeight(256)
		}
	}
}

// ApplyDefaultBackendConfig applies default field value specified in defaultConfig to config if a corresponding field is missing.
func ApplyDefaultBackendConfig(ctx context.Context, config *BackendConfig, defaultConfig *BackendConfig) {
	log := klog.FromContext(ctx)

	log.V(4).Info("Applying default-backend-config annotation")

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
func FixupPathConfig(ctx context.Context, config *PathConfig) {
	log := klog.FromContext(ctx)

	switch config.GetAffinity() {
	case AffinityNone, AffinityIP, AffinityCookie:
		// OK
	default:
		log.Error(nil, "Unsupported affinity method", "affinity", config.GetAffinity())
		config.SetAffinity(AffinityNone)
	}

	switch config.GetAffinityCookieSecure() {
	case AffinityCookieSecureAuto, AffinityCookieSecureYes, AffinityCookieSecureNo:
		// OK
	default:
		log.Error(nil, "Unsupported affinity cookie secure", "cookieSecure", config.GetAffinityCookieSecure())
		config.SetAffinityCookieSecure(AffinityCookieSecureAuto)
	}

	switch config.GetAffinityCookieStickiness() {
	case AffinityCookieStickinessLoose, AffinityCookieStickinessStrict:
		// OK
	default:
		log.Error(nil, "Unsupported affinity cookie stickiness", "cookieStickiness", config.GetAffinityCookieStickiness())
		config.SetAffinityCookieStickiness(AffinityCookieStickinessLoose)
	}
}

func ApplyDefaultPathConfig(ctx context.Context, config *PathConfig, defaultConfig *PathConfig) {
	log := klog.FromContext(ctx)

	log.V(4).Info("Applying default-path-config annotation")

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

	if err := os.Rename(tempFile.Name(), path); err != nil {
		os.Remove(tempFile.Name())

		return err
	}

	return nil
}

// Checksum calculates and returns checksum of b in hex string.
func Checksum(b []byte) []byte {
	h := sha256.New()
	h.Write(b)

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

	return strconv.FormatInt(msec, 10) + "ms"
}

// GenerateCryptoKey generates cryptographic key of length keyLength. SHA-256 is used as a hash function.  info is an optional context
// information.  IKM (Input Keying Material) and salt are generated from crypto/rand.Read.
func GenerateCryptoKey(info string, keyLength int) ([]byte, error) {
	if keyLength == 0 {
		return nil, nil
	}

	const ikmLen = 8

	ikmSalt := make([]byte, ikmLen+sha256.Size)
	if _, err := rand.Read(ikmSalt); err != nil {
		return nil, err
	}

	ikm := ikmSalt[:ikmLen]
	salt := ikmSalt[ikmLen:]

	return hkdf.Key(sha256.New, ikm, salt, info, keyLength)
}
