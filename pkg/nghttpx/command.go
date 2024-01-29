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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	kjson "sigs.k8s.io/json"
)

// Start starts a nghttpx process using nghttpx executable at path, and wait.
func (lb *LoadBalancer) Start(ctx context.Context, path, confPath string) error {
	log := klog.FromContext(ctx)

	log.Info("Starting nghttpx process", "path", path, "conf", confPath)
	lb.cmd = exec.Command(path, "--conf", confPath)
	lb.cmd.Stdout = os.Stdout
	lb.cmd.Stderr = os.Stderr
	if err := lb.cmd.Start(); err != nil {
		log.Error(err, "nghttpx did not start successfully")
		return err
	}

	waitCtx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := lb.cmd.Wait(); err != nil {
			log.Error(err, "nghttpx did not finish successfully")
		}
		cancel()
	}()

	select {
	case <-waitCtx.Done():
	case <-ctx.Done():
		log.Info("Sending QUIT signal to nghttpx process to shut down gracefully", "PID", lb.cmd.Process.Pid)
		if err := lb.cmd.Process.Signal(syscall.SIGQUIT); err != nil {
			log.Error(err, "Unable to send signal to nghttpx process", "PID", lb.cmd.Process.Pid)
			cancel()
		}
		<-waitCtx.Done()
	}

	log.Info("nghttpx exited")

	return nil
}

// CheckAndReload checks whether the nghttpx configuration changed and if so, makes nghttpx reload its configuration.
//
// The current running nghttpx master process executes new nghttpx with new configuration.  If its invocation succeeds, current nghttpx is
// going to shutdown gracefully.  The invocation of new process may fail due to invalid configurations.
func (lb *LoadBalancer) CheckAndReload(ctx context.Context, ingressCfg *IngressConfig) (bool, error) {
	log := klog.FromContext(ctx)

	mainConfig, backendConfig, err := lb.generateCfg(ctx, ingressCfg)
	if err != nil {
		return false, err
	}

	changed, err := lb.checkAndWriteCfg(ctx, ingressCfg, mainConfig, backendConfig)
	if err != nil {
		return false, fmt.Errorf("failed to write new nghttpx configuration. Avoiding reload: %w", err)
	}

	if changed == configNotChanged {
		return false, nil
	}

	if log.V(3).Enabled() {
		log.V(3).Info("nghttpx configuration", "configuration", klog.Format(ingressCfg))
	}

	switch changed {
	case mainConfigChanged:
		oldConfRev, err := lb.getNghttpxConfigRevision(ctx)
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
		if err := writeQUICSecretFile(ingressCfg); err != nil {
			return false, err
		}

		log.Info("Change in configuration detected. Reloading...")
		if err := lb.cmd.Process.Signal(syscall.SIGHUP); err != nil {
			return false, fmt.Errorf("failed to send signal to nghttpx process (PID %v): %w", lb.cmd.Process.Pid, err)
		}

		if err := lb.waitUntilConfigRevisionChanges(ctx, oldConfRev); err != nil {
			return false, err
		}

		lb.eventRecorder.Eventf(lb.pod, nil, corev1.EventTypeNormal, "Reload", "Reload", "nghttpx reloaded its configuration")

		log.Info("nghttpx has finished reloading new configuration")

		if err := lb.deleteStaleAssets(ctx, ingressCfg, time.Now()); err != nil {
			log.Error(err, "Unable to delete stale assets")
		}
	case backendConfigChanged:
		if err := writePerPatternMrubyFile(ingressCfg); err != nil {
			return false, err
		}

		if err := lb.issueBackendReplaceRequest(ctx, ingressCfg); err != nil {
			return false, fmt.Errorf("failed to issue backend replace request: %w", err)
		}

		lb.eventRecorder.Eventf(lb.pod, nil, corev1.EventTypeNormal, "ReplaceBackend", "ReplaceBackend", "nghttpx replaced its backend servers")

		if err := lb.deleteStaleMrubyAssets(ctx, ingressCfg, time.Now()); err != nil {
			log.Error(err, "Unable to delete stale assets")
		}
	}

	return true, nil
}

// deleteStaleAssets deletes asset files which are no longer used.
func (lb *LoadBalancer) deleteStaleAssets(ctx context.Context, ingConfig *IngressConfig, t time.Time) error {
	if err := lb.deleteStaleTLSAssets(ctx, ingConfig, t); err != nil {
		return fmt.Errorf("could not delete stale TLS assets: %w", err)
	}
	if err := lb.deleteStaleMrubyAssets(ctx, ingConfig, t); err != nil {
		return fmt.Errorf("could not delete stale mruby assets: %w", err)
	}
	return nil
}

// deleteStaleTLSAssets deletes TLS asset files which are no longer used.
func (lb *LoadBalancer) deleteStaleTLSAssets(ctx context.Context, ingConfig *IngressConfig, t time.Time) error {
	return deleteAssetFiles(ctx, filepath.Join(ingConfig.ConfDir, tlsDir), t, lb.staleAssetsThreshold)
}

// deleteStaleMrubyAssets deletes mruby asset files which are no longer used.
func (lb *LoadBalancer) deleteStaleMrubyAssets(ctx context.Context, ingConfig *IngressConfig, t time.Time) error {
	return deleteAssetFiles(ctx, filepath.Join(ingConfig.ConfDir, mrubyDir), t, lb.staleAssetsThreshold)
}

// deleteAssetFiles deletes stale files under dir.  A file is stale when its modification time + staleAssetsThreshold <= t.
func deleteAssetFiles(ctx context.Context, dir string, t time.Time, staleAssetsThreshold time.Duration) error {
	log := klog.FromContext(ctx)

	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		path := filepath.Join(dir, f.Name())

		info, err := f.Info()
		if err != nil {
			return err
		}

		modTime := info.ModTime()
		if modTime.Add(staleAssetsThreshold).After(t) {
			continue
		}

		log.V(4).Info("Removing stale asset file", "path", path)
		if err := os.Remove(path); err != nil {
			return err
		}
	}

	return nil
}

func (lb *LoadBalancer) issueBackendReplaceRequest(ctx context.Context, ingConfig *IngressConfig) error {
	log := klog.FromContext(ctx)

	log.Info("Issuing API request", "endpoint", lb.backendconfigURI)

	backendConfigPath := BackendConfigPath(ingConfig.ConfDir)

	in, err := os.Open(backendConfigPath)
	if err != nil {
		return fmt.Errorf("could not open backend configuration file %v: %w", backendConfigPath, err)
	}

	defer in.Close()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, lb.backendconfigURI, in)
	if err != nil {
		return fmt.Errorf("could not create API request: %w", err)
	}

	req.Header.Add("Content-Type", "text/plain")

	resp, err := lb.httpClient.Do(req)

	if err != nil {
		return fmt.Errorf("could not issue API request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("backendconfig API endpoint returned unsuccessful status code %v", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read API response body: %w", err)
	}

	if log.V(3).Enabled() {
		log.Info("API request returned response body", "body", string(respBody))
	}

	log.Info("API request has completed successfully")

	return nil
}

// apiResult is an object to store the result of nghttpx API.
type apiResult struct {
	Status string                 `json:"status,omitempty"`
	Code   int32                  `json:"code,omitempty"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// getNghttpxConfigRevision returns the current nghttpx configRevision through configrevision API call.
func (lb *LoadBalancer) getNghttpxConfigRevision(ctx context.Context) (int64, error) {
	log := klog.FromContext(ctx)

	log.V(4).Info("Issuing API request", "endpoint", lb.configrevisionURI)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lb.configrevisionURI, nil)
	if err != nil {
		return 0, fmt.Errorf("could not create API request: %w", err)
	}

	resp, err := lb.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("could not get nghttpx configRevision: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("configrevision API endpoint returned unsuccessful status code %v", resp.StatusCode)
	}

	d := kjson.NewDecoderCaseSensitivePreserveInts(resp.Body)

	var r apiResult
	if err := d.Decode(&r); err != nil {
		return 0, fmt.Errorf("could not parse nghttpx configuration API result: %w", err)
	}

	if r.Data == nil {
		return 0, errors.New("nghttpx configuration API result has nil Data field")
	}

	s := r.Data["configRevision"]
	confRev, ok := s.(int64)
	if !ok {
		return 0, errors.New("nghttpx configuration API result has non int64 configRevision")
	}

	log.V(4).Info("nghttpx configRevision", "configRevision", confRev)

	return confRev, nil
}

// waitUntilConfigRevisionChanges waits for the current nghttpx configuration to change from old value, oldConfRev.
func (lb *LoadBalancer) waitUntilConfigRevisionChanges(ctx context.Context, oldConfRev int64) error {
	log := klog.FromContext(ctx)

	log.Info("Waiting for nghttpx to finish reloading configuration")

	if err := wait.PollUntilContextTimeout(ctx, time.Second, lb.reloadTimeout, false, func(ctx context.Context) (bool, error) {
		newConfRev, err := lb.getNghttpxConfigRevision(ctx)
		if err != nil {
			log.Error(err, "Unable to get new configRevision")
			return false, nil
		}

		if newConfRev == oldConfRev {
			return false, nil
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("could not get new nghttpx configRevision: %w", err)
	}

	return nil
}
