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

package main

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"text/template"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/server/healthz"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/controller"
	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

var (
	// value overwritten during build. This can be used to resolve issues.
	version = "0.5"
	gitRepo = "https://github.com/kubernetes/contrib"

	flags = pflag.NewFlagSet("", pflag.ExitOnError)

	defaultSvc = flags.String("default-backend-service", "",
		`(Required) Service used to serve a 404 page for the default backend. Takes the form
    namespace/name. The controller uses the first node port of this Service for
    the default backend.`)

	ngxConfigMap = flags.String("nghttpx-configmap", "",
		`Name of the ConfigMap that containes the custom nghttpx configuration to use`)

	inCluster = flags.Bool("running-in-cluster", true,
		`Deprecated: Use --kubeconfig to run the controller outside a cluster`)

	kubeconfig = flags.String("kubeconfig", "", `Path to kubeconfig file which overrides in-cluster configuration.`)

	resyncPeriod = flags.Duration("sync-period", 30*time.Second,
		`Relist and confirm cloud resources this often.`)

	watchNamespace = flags.String("watch-namespace", metav1.NamespaceAll,
		`Namespace to watch for Ingress. Default is to watch all namespaces`)

	healthzPort = flags.Int("healthz-port", 11249, "port for healthz endpoint.")

	nghttpxHealthPort = flags.Int("nghttpx-health-port", 10901, "port for nghttpx health monitor endpoint.")

	nghttpxAPIPort = flags.Int("nghttpx-api-port", 10902, "port for nghttpx API endpoint.")

	buildCfg = flags.Bool("dump-nghttpx-configuration", false, `Deprecated`)

	profiling = flags.Bool("profiling", true, `Enable profiling via web interface host:port/debug/pprof/`)

	allowInternalIP = flags.Bool("allow-internal-ip", false, `Allow to use address of type NodeInternalIP when fetching
                external IP address. This is the workaround for the cluster configuration where NodeExternalIP or
                NodeLegacyHostIP is not assigned or cannot be used.`)

	defaultTLSSecret = flags.String("default-tls-secret", "",
		`Optional, name of the Secret that contains TLS server certificate and secret key to enable TLS by default.  For those client connections which are not TLS encrypted, they are redirected to https URI permantently.`)

	ingressClass = flags.String("ingress-class", "nghttpx",
		`Ingress class which this controller is responsible for.`)

	nghttpxConfDir = flags.String("nghttpx-conf-dir", "/etc/nghttpx",
		`Path to the directory which contains nghttpx configuration files.  The controller reads and writes these configuration files.`)

	nghttpxExecPath = flags.String("nghttpx-exec-path", "/usr/local/bin/nghttpx",
		`Path to the nghttpx executable.`)

	nghttpxHTTPPort = flags.Int("nghttpx-http-port", 80,
		`Port to listen to for HTTP (non-TLS) requests.`)

	nghttpxHTTPSPort = flags.Int("nghttpx-https-port", 443,
		`Port to listen to for HTTPS (TLS) requests.`)

	fetchOCSPRespFromSecret = flags.Bool("fetch-ocsp-resp-from-secret", false,
		`Fetch OCSP response from TLS secret.`)

	ocspRespKey = flags.String("ocsp-resp-key", "tls.ocsp-resp", `A key for OCSP response in TLS secret.`)

	configOverrides clientcmd.ConfigOverrides
)

func main() {
	// We use math/rand to choose interval of resync
	rand.Seed(time.Now().UTC().UnixNano())

	flags.AddGoFlagSet(flag.CommandLine)

	clientcmd.BindOverrideFlags(&configOverrides, flags, clientcmd.RecommendedConfigOverrideFlags(""))

	flags.Parse(os.Args)

	glog.Infof("Using build: %v - %v", gitRepo, version)

	if *buildCfg {
		fmt.Println("dump-nghttpx-configuration was deprecated.")
		os.Exit(0)
	}

	if *defaultSvc == "" {
		glog.Exitf("Please specify --default-backend-service")
	}
	if _, _, err := cache.SplitMetaNamespaceKey(*defaultSvc); err != nil {
		glog.Exitf("could not parse default-backend-service %v: %v", *defaultSvc, err)
	}

	var err error
	var config *rest.Config
	if *kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		loadingRules := clientcmd.ClientConfigLoadingRules{
			ExplicitPath: *kubeconfig,
		}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&loadingRules, &configOverrides).ClientConfig()
	}
	if err != nil {
		glog.Exitf("Could not get clientConfig: %v", err)
	}

	clientset, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Exitf("Failed to create clientset: %v", err)
	}

	if err := controller.IsValidService(clientset, *defaultSvc); err != nil {
		glog.Exitf("no service with name %v found: %v", *defaultSvc, err)
	}
	glog.Infof("Validated %v as the default backend", *defaultSvc)

	if *ngxConfigMap != "" {
		if _, _, err := cache.SplitMetaNamespaceKey(*ngxConfigMap); err != nil {
			glog.Exitf("could not parse configmap name %v: %v", *ngxConfigMap, err)
		}
	}

	if *defaultTLSSecret != "" {
		if _, _, err := cache.SplitMetaNamespaceKey(*defaultTLSSecret); err != nil {
			glog.Exitf("could not parse Secret %v: %v", *defaultTLSSecret, err)
		}
	}

	runtimePodInfo := &controller.PodInfo{
		PodName:      os.Getenv("POD_NAME"),
		PodNamespace: os.Getenv("POD_NAMESPACE"),
	}

	if runtimePodInfo.PodName == "" {
		glog.Exit("POD_NAME environment variable cannot be empty.")
	}
	if runtimePodInfo.PodNamespace == "" {
		glog.Exit("POD_NAMESPACE environment variable cannot be empty.")
	}

	controllerConfig := controller.Config{
		ResyncPeriod:            *resyncPeriod,
		DefaultBackendService:   *defaultSvc,
		WatchNamespace:          *watchNamespace,
		NghttpxConfigMap:        *ngxConfigMap,
		NghttpxHealthPort:       *nghttpxHealthPort,
		NghttpxAPIPort:          *nghttpxAPIPort,
		NghttpxConfDir:          *nghttpxConfDir,
		NghttpxExecPath:         *nghttpxExecPath,
		NghttpxHTTPPort:         *nghttpxHTTPPort,
		NghttpxHTTPSPort:        *nghttpxHTTPSPort,
		DefaultTLSSecret:        *defaultTLSSecret,
		IngressClass:            *ingressClass,
		AllowInternalIP:         *allowInternalIP,
		OCSPRespKey:             *ocspRespKey,
		FetchOCSPRespFromSecret: *fetchOCSPRespFromSecret,
	}

	if err := generateDefaultNghttpxConfig(*nghttpxConfDir, *nghttpxHealthPort, *nghttpxAPIPort); err != nil {
		glog.Exit(err)
	}

	lbc := controller.NewLoadBalancerController(clientset, nghttpx.NewManager(*nghttpxAPIPort), &controllerConfig, runtimePodInfo)

	go registerHandlers(lbc)
	go handleSigterm(lbc)

	lbc.Run()
}

// healthzChecker implements healthz.HealthzChecker interface.
type healthzChecker struct {
	// targetURI is the nghttpx health monitor endpoint.
	targetURI string
}

// newHealthChecker returns new healthzChecker.
func newHealthzChecker(healthPort int) *healthzChecker {
	return &healthzChecker{
		targetURI: fmt.Sprintf("http://127.0.0.1:%v/healthz", healthPort),
	}
}

// Name returns the healthcheck name
func (hc healthzChecker) Name() string {
	return "nghttpx"
}

// Check returns if the nghttpx healthz endpoint is returning ok (status code 200)
func (hc healthzChecker) Check(_ *http.Request) error {
	res, err := http.Get(hc.targetURI)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Errorf("nghttpx is unhealthy")
	}

	return nil
}

func registerHandlers(lbc *controller.LoadBalancerController) {
	mux := http.NewServeMux()
	healthz.InstallHandler(mux, newHealthzChecker(*nghttpxHealthPort))

	http.HandleFunc("/build", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "build: %v - %v", gitRepo, version)
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		lbc.Stop()
	})

	if *profiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%v", *healthzPort),
		Handler: mux,
	}
	glog.Exit(server.ListenAndServe())
}

func handleSigterm(lbc *controller.LoadBalancerController) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	glog.Infof("Received SIGTERM, shutting down")

	lbc.Stop()
}

// generateDefaultNghttpxConfig generates default configuration file for nghttpx.
func generateDefaultNghttpxConfig(nghttpxConfDir string, nghttpxHealthPort, nghttpxAPIPort int) error {
	if err := nghttpx.MkdirAll(nghttpxConfDir); err != nil {
		return err
	}

	var buf bytes.Buffer
	t := template.Must(template.New("default.tmpl").ParseFiles("./default.tmpl"))
	if err := t.Execute(&buf, map[string]interface{}{
		"HealthPort": nghttpxHealthPort,
		"APIPort":    nghttpxAPIPort,
	}); err != nil {
		return fmt.Errorf("Could not create default configuration file for nghttpx: %v", err)
	}

	if err := nghttpx.WriteFile(nghttpx.NghttpxConfigPath(nghttpxConfDir), buf.Bytes()); err != nil {
		return fmt.Errorf("Could not create default configuration file for nghttpx: %v", err)
	}

	return nil
}
