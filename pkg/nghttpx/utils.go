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

	"k8s.io/kubernetes/pkg/api/v1"
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
			Path:     "/etc/nghttpx/mruby.rb",
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

// FixupPortBackendConfig validates config, and fixes the invalid values inside it.  svc and port is service name and port that config is
// associated to.
func FixupPortBackendConfig(config PortBackendConfig, svc, port string) PortBackendConfig {
	glog.Infof("use port backend configuration for service %v: %+v", svc, config)
	switch config.Proto {
	case ProtocolH2, ProtocolH1:
		// OK
	case "":
		config.Proto = ProtocolH1
	default:
		glog.Errorf("unrecognized backend protocol %v for service %v, port %v", config.Proto, svc, port)
		config.Proto = ProtocolH1
	}
	switch config.Affinity {
	case AffinityNone, AffinityIP:
		// OK
	case "":
		config.Affinity = AffinityNone
	default:
		glog.Errorf("unsupported affinity method %v for service %v, port %v", config.Affinity, svc, port)
		config.Affinity = AffinityNone
	}
	return config
}

// DefaultPortBackendConfig returns default PortBackendConfig
func DefaultPortBackendConfig() PortBackendConfig {
	// Update NewDefaultServer() too.
	return PortBackendConfig{
		Proto:    ProtocolH1,
		Affinity: AffinityNone,
	}
}

func writeFile(path string, content []byte) error {
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
