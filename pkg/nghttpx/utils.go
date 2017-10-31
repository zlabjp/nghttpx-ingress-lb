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
	"encoding/hex"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
)

const (
	// NghttpxExtraConfigKey is a field name of extra nghttpx configuration in ConfigMap.
	NghttpxExtraConfigKey = "nghttpx-conf"
	// NghttpxMrubyFileContentKey is a field name of mruby script in ConfigMap.
	NghttpxMrubyFileContentKey = "nghttpx-mruby-file-content"
)

// ReadConfig obtains the configuration defined by the user merged with the defaults.
func ReadConfig(ingConfig *IngressConfig, config *v1.ConfigMap) {
	ingConfig.ExtraConfig = config.Data[NghttpxExtraConfigKey]
	if mrubyFileContent, ok := config.Data[NghttpxMrubyFileContentKey]; ok {
		b := []byte(mrubyFileContent)
		ingConfig.MrubyFile = &ChecksumFile{
			Path:     NghttpxMrubyRbPath(ingConfig.ConfDir),
			Content:  b,
			Checksum: Checksum(b),
		}
	}
}

// needsReload first checks that configuration is changed.  filename
// is the current configuration file path, and data includes the new
// configuration.  If they differ, we write data into filename, and
// return true.  Otherwise, just return false without altering
// existing file.
func needsReload(filename string, newCfg []byte) (bool, error) {
	in, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}

	oldCfg, err := ioutil.ReadAll(in)
	in.Close()
	if err != nil {
		return false, err
	}

	if bytes.Equal(oldCfg, newCfg) {
		return false, nil
	}

	if glog.V(2) {
		dData, err := diff(oldCfg, newCfg)
		if err != nil {
			glog.Errorf("error computing diff: %s", err)
			return true, nil
		}
		glog.Infof("nghttpx configuration diff a/%s b/%s\n%v", filename, filename, string(dData))
	}

	return true, nil
}

func diff(b1, b2 []byte) (data []byte, err error) {
	f1, err := ioutil.TempFile("", "")
	if err != nil {
		return
	}
	defer func() {
		f1.Close()
		os.Remove(f1.Name())
	}()

	f2, err := ioutil.TempFile("", "")
	if err != nil {
		return
	}
	defer func() {
		f2.Close()
		os.Remove(f2.Name())
	}()

	f1.Write(b1)
	f2.Write(b2)

	data, err = exec.Command("diff", "-u", f1.Name(), f2.Name()).CombinedOutput()
	if len(data) > 0 {
		err = nil
	}
	return
}

// FixupPortBackendConfig validates config, and fixes the invalid values inside it.
func FixupPortBackendConfig(config *PortBackendConfig) {
	switch config.GetProto() {
	case ProtocolH2, ProtocolH1, "":
		// OK
	default:
		glog.Errorf("unrecognized backend protocol %q", config.GetProto())
		config.SetProto(ProtocolH1)
	}
	switch config.GetAffinity() {
	case AffinityNone, AffinityIP, "":
		// OK
	default:
		glog.Errorf("unsupported affinity method %v", config.GetAffinity())
		config.SetAffinity(AffinityNone)
	}
}

// ApplyDefaultPortBackendConfig applies default field value specified in defaultConfig to config if a corresponding field is missing.
func ApplyDefaultPortBackendConfig(config *PortBackendConfig, defaultConfig *PortBackendConfig) {
	glog.V(4).Info("Applying default-backend-config annotation")
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
	if defaultConfig.Affinity != nil && config.Affinity == nil {
		config.SetAffinity(*defaultConfig.Affinity)
	}
}

func WriteFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	tempFile, err := ioutil.TempFile(dir, "nghttpx")
	if err != nil {
		return err
	}
	tempFile.Close()

	if err := ioutil.WriteFile(tempFile.Name(), content, 0644); err != nil {
		os.Remove(tempFile.Name())
		return err
	}
	if err := os.Rename(tempFile.Name(), path); err != nil {
		return err
	}

	return nil
}

// Checksum calculates and returns checksum of b in hex string.
func Checksum(b []byte) string {
	h := sha256.New()
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// NghttpxConfigPath returns the path to nghttpx configuration file.
func NghttpxConfigPath(dir string) string {
	return filepath.Join(dir, "nghttpx.conf")
}

// NghttpxBackendConfigPath returns the path to nghttpx backend configuration file.
func NghttpxBackendConfigPath(dir string) string {
	return filepath.Join(dir, "nghttpx-backend.conf")
}

// NghttpxMrubyRbPath returns the path to nghttpx mruby.rb file.
func NghttpxMrubyRbPath(dir string) string {
	return filepath.Join(dir, "mruby.rb")
}

// MkdirAll creates directory given as path.
func MkdirAll(path string) error {
	if err := os.MkdirAll(path, os.ModeDir); err != nil {
		return err
	}
	return nil
}
