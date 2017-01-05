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
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/api"
)

// ReadConfig obtains the configuration defined by the user merged with the defaults.
func (ngx *Manager) ReadConfig(config *api.ConfigMap) nghttpxConfiguration {
	cfg := newDefaultNghttpxCfg()
	cfg.ExtraConfig = config.Data["nghttpx-conf"]
	return cfg
}

// needsReload first checks that configuration is changed.  filename
// is the current configuration file path, and data includes the new
// configuration.  If they differ, we write data into filename, and
// return true.  Otherwise, just return false without altering
// existing file.
func (ngx *Manager) needsReload(filename string, data *bytes.Buffer) (bool, error) {
	in, err := os.Open(filename)
	if err != nil {
		return false, err
	}

	src, err := ioutil.ReadAll(in)
	in.Close()
	if err != nil {
		return false, err
	}

	res := data.Bytes()
	if !bytes.Equal(src, res) {
		// First write into temporary file in the same
		// directory of filename.  Then replace filename with
		// temporary file.  In Linux, this is atomic
		// operation.
		dir := filepath.Dir(filename)
		tempFile, err := ioutil.TempFile(dir, "nghttpx")
		if err != nil {
			return false, err
		}
		tempFile.Close()
		defer func() {
			os.Remove(tempFile.Name())
		}()
		if err := ioutil.WriteFile(tempFile.Name(), res, 0644); err != nil {
			return false, err
		}
		if err := os.Rename(tempFile.Name(), filename); err != nil {
			return false, err
		}
		dData, err := diff(src, res)
		if err != nil {
			glog.Errorf("error computing diff: %s", err)
			return true, nil
		}

		if glog.V(2) {
			glog.Infof("nghttpx configuration diff a/%s b/%s\n", filename, filename)
			glog.Infof("%v", string(dData))
		}

		return len(dData) > 0, nil
	}

	return false, nil
}

func diff(b1, b2 []byte) (data []byte, err error) {
	f1, err := ioutil.TempFile("", "")
	if err != nil {
		return
	}
	defer os.Remove(f1.Name())
	defer f1.Close()

	f2, err := ioutil.TempFile("", "")
	if err != nil {
		return
	}
	defer os.Remove(f2.Name())
	defer f2.Close()

	f1.Write(b1)
	f2.Write(b2)

	data, err = exec.Command("diff", "-u", f1.Name(), f2.Name()).CombinedOutput()
	if len(data) > 0 {
		err = nil
	}
	return
}
