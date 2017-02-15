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
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"syscall"

	"github.com/golang/glog"
)

// Start starts a nghttpx process, and wait.
func (ngx *Manager) Start(stopCh <-chan struct{}) {
	glog.Info("Starting nghttpx process...")
	cmd := exec.Command("/usr/local/bin/nghttpx")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		glog.Errorf("nghttpx didn't started successfully: %v", err)
		return
	}

	waitDoneCh := make(chan struct{})
	go func() {
		if err := cmd.Wait(); err != nil {
			glog.Errorf("nghttpx didn't complete successfully: %v", err)
		}
		close(waitDoneCh)
	}()

	select {
	case <-waitDoneCh:
		glog.Infof("nghttpx exited")
	case <-stopCh:
		glog.Infof("Sending QUIT signal to nghttpx process (PID %v) to shut down gracefully", cmd.Process.Pid)
		if err := cmd.Process.Signal(syscall.SIGQUIT); err != nil {
			glog.Errorf("Could not send signal to nghttpx process (PID %v): %v", cmd.Process.Pid, err)
		}
		<-waitDoneCh
		glog.Infof("nghttpx exited")
	}
}

// CheckAndReload verify if the nghttpx configuration changed and sends a reload
//
// The current running nghttpx master process executes new nghttpx
// with new configuration.  If its invocation succeeds, current
// nghttpx is going to shutdown gracefully.  The invocation of new
// process may fail due to invalid configurations.
func (ngx *Manager) CheckAndReload(ingressCfg *IngressConfig) (bool, error) {
	ngx.reloadLock.Lock()
	defer ngx.reloadLock.Unlock()

	if err := ngx.writeTLSKeyCert(ingressCfg); err != nil {
		return false, err
	}

	changed, err := ngx.writeCfg(ingressCfg)

	if err != nil {
		return false, fmt.Errorf("failed to write new nghttpx configuration. Avoiding reload: %v", err)
	}

	switch changed {
	case mainConfigChanged:
		cmd := "killall"
		args := []string{"-HUP", "nghttpx"}
		glog.Info("change in configuration detected. Reloading...")
		out, err := exec.Command(cmd, args...).CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to execute %v %v: %v", cmd, args, string(out))
		}
		return true, nil
	case backendConfigChanged:
		if err := ngx.issueBackendReplaceRequest(); err != nil {
			return false, fmt.Errorf("failed to issue backend replace request: %v", err)
		}
		return true, nil
	default:
		return false, nil
	}
}

const (
	backendReplaceURI = "http://127.0.0.1:3001/api/v1beta1/backendconfig"
)

func (ngx *Manager) issueBackendReplaceRequest() error {
	glog.Infof("Issuing API request to %v", backendReplaceURI)

	in, err := os.Open(ngx.BackendConfigFile)
	if err != nil {
		return fmt.Errorf("Could not open backend configuration file %v: %v", ngx.BackendConfigFile, err)
	}

	defer in.Close()

	req, err := http.NewRequest("PUT", backendReplaceURI, in)
	if err != nil {
		return fmt.Errorf("Could not create API request %v: %v", backendReplaceURI, err)
	}

	req.Header.Add("Content-Type", "text/plain")

	resp, err := ngx.httpClient.Do(req)

	if err != nil {
		return fmt.Errorf("Could not issue PUT %v: %v", backendReplaceURI, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("%v returned unsuccessful status code %v", backendReplaceURI, resp.StatusCode)
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("Error while reading response body from %v: %v", backendReplaceURI, err)
	}

	if glog.V(3) {
		glog.Infof("API request %v returned response body: %v", backendReplaceURI, string(respBody))
	}

	glog.Infof("API request %v has completed successfully", backendReplaceURI)

	return nil
}
