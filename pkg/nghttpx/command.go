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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// Start starts a nghttpx process using nghttpx executable at path, and wait.
func (mgr *Manager) Start(ctx context.Context, path, confPath string) error {
	klog.Infof("Starting nghttpx process: %v --conf %v", path, confPath)
	mgr.cmd = exec.Command(path, "--conf", confPath)
	mgr.cmd.Stdout = os.Stdout
	mgr.cmd.Stderr = os.Stderr
	if err := mgr.cmd.Start(); err != nil {
		klog.Errorf("nghttpx did not start successfully: %v", err)
		return err
	}

	waitCtx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := mgr.cmd.Wait(); err != nil {
			klog.Errorf("nghttpx did not finish successfully: %v", err)
		}
		cancel()
	}()

	select {
	case <-waitCtx.Done():
	case <-ctx.Done():
		klog.Infof("Sending QUIT signal to nghttpx process (PID %v) to shut down gracefully", mgr.cmd.Process.Pid)
		if err := mgr.cmd.Process.Signal(syscall.SIGQUIT); err != nil {
			klog.Errorf("Could not send signal to nghttpx process (PID %v): %v", mgr.cmd.Process.Pid, err)
			cancel()
		}
		<-waitCtx.Done()
	}

	klog.Infof("nghttpx exited")

	return nil
}

// CheckAndReload checks whether the nghttpx configuration changed and if so, makes nghttpx reload its configuration.
//
// The current running nghttpx master process executes new nghttpx with new configuration.  If its invocation succeeds, current nghttpx is
// going to shutdown gracefully.  The invocation of new process may fail due to invalid configurations.
func (mgr *Manager) CheckAndReload(ingressCfg *IngressConfig) (bool, error) {
	mainConfig, backendConfig, err := mgr.generateCfg(ingressCfg)
	if err != nil {
		return false, err
	}

	changed, err := mgr.checkAndWriteCfg(ingressCfg, mainConfig, backendConfig)
	if err != nil {
		return false, fmt.Errorf("failed to write new nghttpx configuration. Avoiding reload: %v", err)
	}

	if changed == configNotChanged {
		return false, nil
	}

	if klog.V(3).Enabled() {
		b, err := json.MarshalIndent(ingressCfg, "", "  ")
		if err != nil {
			fmt.Println("error:", err)
		}
		klog.Infof("nghttpx configuration:\n%s", b)
	}

	switch changed {
	case mainConfigChanged:
		oldConfRev, err := mgr.getNghttpxConfigRevision()
		if err != nil {
			return false, err
		}
		if err := writeTLSKeyCert(ingressCfg); err != nil {
			return false, err
		}
		if err := writeMrubyFile(ingressCfg); err != nil {
			return false, err
		}
		if err := writePerPatternMrubyFile(ingressCfg); err != nil {
			return false, err
		}

		klog.Info("change in configuration detected. Reloading...")
		if err := mgr.cmd.Process.Signal(syscall.SIGHUP); err != nil {
			return false, fmt.Errorf("failed to send signal to nghttpx process (PID %v): %v", mgr.cmd.Process.Pid, err)
		}

		if err := mgr.waitUntilConfigRevisionChanges(oldConfRev); err != nil {
			return false, err
		}

		klog.Info("nghttpx has finished reloading new configuration")

		if err := deleteStaleAssets(ingressCfg); err != nil {
			klog.Errorf("Could not delete stale assets: %v", err)
		}
	case backendConfigChanged:
		if err := writePerPatternMrubyFile(ingressCfg); err != nil {
			return false, err
		}

		if err := mgr.issueBackendReplaceRequest(ingressCfg); err != nil {
			return false, fmt.Errorf("failed to issue backend replace request: %v", err)
		}

		if err := deleteStaleMrubyAssets(ingressCfg); err != nil {
			klog.Errorf("Could not delete stale assets: %v", err)
		}
	}

	return true, nil
}

// deleteStaleAssets deletes asset files which are no longer used.
func deleteStaleAssets(ingConfig *IngressConfig) error {
	if err := deleteStaleTLSAssets(ingConfig); err != nil {
		return fmt.Errorf("could not delete stale TLS assets: %v", err)
	}
	if err := deleteStaleMrubyAssets(ingConfig); err != nil {
		return fmt.Errorf("could not delete stale mruby assets: %v", err)
	}
	return nil
}

// deleteStaleTLSAssets deletes TLS asset files which are no longer used.
func deleteStaleTLSAssets(ingConfig *IngressConfig) error {
	keep := make(map[string]bool)
	if ingConfig.DefaultTLSCred != nil {
		gatherTLSAssets(keep, ingConfig.DefaultTLSCred)
	}
	for _, tlsCred := range ingConfig.SubTLSCred {
		gatherTLSAssets(keep, tlsCred)
	}

	return deleteAssetFiles(filepath.Join(ingConfig.ConfDir, tlsDir), keep)
}

// gatherTLSAssets collects file path from tlsCred, and set its associated value to true in dst.
func gatherTLSAssets(dst map[string]bool, tlsCred *TLSCred) {
	dst[tlsCred.Key.Path] = true
	dst[tlsCred.Cert.Path] = true
	if tlsCred.OCSPResp != nil {
		dst[tlsCred.OCSPResp.Path] = true
	}
}

// deleteStaleMrubyAssets deletes mruby asset files which are no longer used.
func deleteStaleMrubyAssets(ingConfig *IngressConfig) error {
	keep := make(map[string]bool)
	for _, upstream := range ingConfig.Upstreams {
		if upstream.Mruby == nil {
			continue
		}
		keep[upstream.Mruby.Path] = true
	}

	if ingConfig.HealthzMruby != nil {
		keep[ingConfig.HealthzMruby.Path] = true
	}

	return deleteAssetFiles(filepath.Join(ingConfig.ConfDir, mrubyDir), keep)
}

// deleteAssetFiles deletes files under dir but keeps files if they are included in keep.
func deleteAssetFiles(dir string, keep map[string]bool) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		path := filepath.Join(dir, f.Name())
		if keep[path] {
			continue
		}
		klog.V(4).Infof("Removing stale asset file %v", path)
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	return nil
}

func (mgr *Manager) issueBackendReplaceRequest(ingConfig *IngressConfig) error {
	klog.Infof("Issuing API request %v", mgr.backendconfigURI)

	backendConfigPath := BackendConfigPath(ingConfig.ConfDir)

	in, err := os.Open(backendConfigPath)
	if err != nil {
		return fmt.Errorf("could not open backend configuration file %v: %v", backendConfigPath, err)
	}

	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mgr.backendconfigURI, in)
	if err != nil {
		return fmt.Errorf("could not create API request: %v", err)
	}

	req.Header.Add("Content-Type", "text/plain")

	resp, err := mgr.httpClient.Do(req)

	if err != nil {
		return fmt.Errorf("could not issue API request: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("backendconfig API endpoint returned unsuccessful status code %v", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read API response body: %v", err)
	}

	if klog.V(3).Enabled() {
		klog.Infof("API request returned response body: %s", respBody)
	}

	klog.Info("API request has completed successfully")

	return nil
}

// apiResult is an object to store the result of nghttpx API.
type apiResult struct {
	Status string                 `json:"status,omitempty"`
	Code   int32                  `json:"code,omitempty"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// getNghttpxConfigRevision returns the current nghttpx configRevision through configrevision API call.
func (mgr *Manager) getNghttpxConfigRevision() (string, error) {
	klog.V(4).Infof("Issuing API request %v", mgr.configrevisionURI)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mgr.configrevisionURI, nil)
	if err != nil {
		return "", fmt.Errorf("could not create API request: %v", err)
	}

	resp, err := mgr.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not get nghttpx configRevision: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("configrevision API endpoint returned unsuccessful status code %v", resp.StatusCode)
	}

	d := json.NewDecoder(resp.Body)
	d.UseNumber()

	var r apiResult
	if err := d.Decode(&r); err != nil {
		return "", fmt.Errorf("could not parse nghttpx configuration API result: %v", err)
	}

	if r.Data == nil {
		return "", fmt.Errorf("nghttpx configuration API result has nil Data field")
	}

	s := r.Data["configRevision"]
	confRev, ok := s.(json.Number)
	if !ok {
		return "", fmt.Errorf("nghttpx configuration API result has non json.Number configRevision")
	}

	klog.V(4).Infof("nghttpx configRevision is %v", confRev)

	return confRev.String(), nil
}

// waitUntilConfigRevisionChanges waits for the current nghttpx configuration to change from old value, oldConfRev.
func (mgr *Manager) waitUntilConfigRevisionChanges(oldConfRev string) error {
	klog.Infof("Waiting for nghttpx to finish reloading configuration")

	if err := wait.Poll(time.Second, 30*time.Second, func() (bool, error) {
		newConfRev, err := mgr.getNghttpxConfigRevision()
		if err != nil {
			klog.Error(err)
			return false, nil
		}

		if newConfRev == oldConfRev {
			return false, nil
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("could not get new nghttpx configRevision: %v", err)
	}

	return nil
}
