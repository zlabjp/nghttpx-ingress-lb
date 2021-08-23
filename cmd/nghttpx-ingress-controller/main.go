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
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/server/healthz"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/controller"
	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

var (
	// value overwritten during build. This can be used to resolve issues.
	version = ""
	gitRepo = ""

	// Command-line flags
	defaultSvc               string
	ngxConfigMap             string
	kubeconfig               string
	watchNamespace           = metav1.NamespaceAll
	healthzPort              = int32(11249)
	nghttpxHealthPort        = int32(10901)
	nghttpxAPIPort           = int32(10902)
	profiling                = true
	allowInternalIP          = false
	defaultTLSSecret         string
	ingressClass             = "nghttpx"
	ingressClassController   = "zlab.co.jp/nghttpx"
	nghttpxConfDir           = "/etc/nghttpx"
	nghttpxExecPath          = "/usr/local/bin/nghttpx"
	nghttpxHTTPPort          = int32(80)
	nghttpxHTTPSPort         = int32(443)
	fetchOCSPRespFromSecret  = false
	proxyProto               = false
	ocspRespKey              = "tls.ocsp-resp"
	publishSvc               string
	endpointSlices           = false
	reloadRate               = 1.0
	reloadBurst              = 1
	noDefaultBackendOverride = false
	deferredShutdownPeriod   time.Duration
	configOverrides          clientcmd.ConfigOverrides
	internalDefaultBackend   = false
)

func main() {
	klog.InitFlags(flag.CommandLine)
	defer klog.Flush()

	// We use math/rand to choose interval of resync
	rand.Seed(time.Now().UTC().UnixNano())

	rootCmd := &cobra.Command{
		Use: "nghttpx-ingress-controller",
		Run: run,
	}

	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)
	clientcmd.BindOverrideFlags(&configOverrides, rootCmd.Flags(), clientcmd.RecommendedConfigOverrideFlags(""))

	rootCmd.Flags().StringVar(&defaultSvc, "default-backend-service", defaultSvc,
		`Service used to serve a 404 page for the default backend.  Takes the form namespace/name.  The controller uses the first node port of this Service for the default backend.  This flag must be specified unless --internal-default-backend is given.`)

	rootCmd.Flags().StringVar(&ngxConfigMap, "nghttpx-configmap", ngxConfigMap,
		`Namespace/name of the ConfigMap that contains the custom nghttpx configuration to use.  Takes the form namespace/name.`)

	rootCmd.Flags().StringVar(&kubeconfig, "kubeconfig", kubeconfig, `Path to kubeconfig file which overrides in-cluster configuration.`)

	rootCmd.Flags().StringVar(&watchNamespace, "watch-namespace", watchNamespace, `Namespace to watch for Ingress.  Default is to watch all namespaces.`)

	rootCmd.Flags().Int32Var(&healthzPort, "healthz-port", healthzPort, "Port for healthz endpoint.")

	rootCmd.Flags().Int32Var(&nghttpxHealthPort, "nghttpx-health-port", nghttpxHealthPort, "Port for nghttpx health monitor endpoint.")

	rootCmd.Flags().Int32Var(&nghttpxAPIPort, "nghttpx-api-port", nghttpxAPIPort, "Port for nghttpx API endpoint.")

	rootCmd.Flags().BoolVar(&profiling, "profiling", profiling, `Enable profiling at the health port.  It exposes /debug/pprof/ endpoint.`)

	rootCmd.Flags().BoolVar(&allowInternalIP, "allow-internal-ip", allowInternalIP,
		`Allow to use address of type NodeInternalIP when fetching external IP address.  This is the workaround for the cluster configuration where NodeExternalIP or NodeLegacyHostIP is not assigned or cannot be used.`)

	rootCmd.Flags().StringVar(&defaultTLSSecret, "default-tls-secret", defaultTLSSecret,
		`Name of the Secret that contains TLS server certificate and secret key to enable TLS by default.  For those client connections which are not TLS encrypted, they are redirected to https URI permanently.`)

	rootCmd.Flags().StringVar(&ingressClass, "ingress-class", ingressClass,
		`(Deprecated) Ingress class which this controller is responsible for.  This is the value of the deprecated "kubernetes.io/ingress.class" annotation.  For Kubernetes v1.18 or later, use ingress-class-controller flag and IngressClass resource.`)

	rootCmd.Flags().StringVar(&ingressClassController, "ingress-class-controller", ingressClassController,
		`The name of IngressClass controller for this controller.  This is the value specified in IngressClass.spec.controller.`)

	rootCmd.Flags().StringVar(&nghttpxConfDir, "nghttpx-conf-dir", nghttpxConfDir,
		`Path to the directory which contains nghttpx configuration files.  The controller reads and writes these configuration files.`)

	rootCmd.Flags().StringVar(&nghttpxExecPath, "nghttpx-exec-path", nghttpxExecPath, `Path to the nghttpx executable.`)

	rootCmd.Flags().Int32Var(&nghttpxHTTPPort, "nghttpx-http-port", nghttpxHTTPPort, `Port to listen to for HTTP (non-TLS) requests.  Specifying 0 disables HTTP port.`)

	rootCmd.Flags().Int32Var(&nghttpxHTTPSPort, "nghttpx-https-port", nghttpxHTTPSPort, `Port to listen to for HTTPS (TLS) requests.  Specifying 0 disables HTTPS port.`)

	rootCmd.Flags().BoolVar(&fetchOCSPRespFromSecret, "fetch-ocsp-resp-from-secret", fetchOCSPRespFromSecret, `Fetch OCSP response from TLS secret.`)

	rootCmd.Flags().BoolVar(&proxyProto, "proxy-proto", proxyProto, `Enable proxyproto for all public-facing frontends (api and health frontends are ignored).`)

	rootCmd.Flags().StringVar(&ocspRespKey, "ocsp-resp-key", ocspRespKey, `A key for OCSP response in TLS secret.`)

	rootCmd.Flags().StringVar(&publishSvc, "publish-service", publishSvc,
		`Specify namespace/name of Service whose hostnames/IP addresses are set in Ingress resource instead of addresses of Ingress controller Pods.  Takes the form namespace/name.`)

	rootCmd.Flags().BoolVar(&endpointSlices, "endpoint-slices", endpointSlices, `Get endpoints from EndpointSlice resource instead of Endpoints resource.`)

	rootCmd.Flags().Float64Var(&reloadRate, "reload-rate", reloadRate,
		`Rate (QPS) of reloading nghttpx configuration to deal with frequent backend updates in a single batch.`)

	rootCmd.Flags().IntVar(&reloadBurst, "reload-burst", reloadBurst, `Reload burst that can exceed reload-rate.`)

	rootCmd.Flags().BoolVar(&noDefaultBackendOverride, "no-default-backend-override", noDefaultBackendOverride,
		`Ignore any settings or rules in Ingress resources which override default backend service.`)

	rootCmd.Flags().DurationVar(&deferredShutdownPeriod, "deferred-shutdown-period", deferredShutdownPeriod,
		`How long the controller waits before actually starting shutting down.  If nonzero value is given, additional health check endpoint is added to HTTP/HTTPS endpoint.  The health check request path is /nghttpx-healthz.  Normally, it returns 200 HTTP status code, but in this period, the endpoint returns 503.`)

	rootCmd.Flags().BoolVar(&internalDefaultBackend, "internal-default-backend", internalDefaultBackend,
		`Use the internal default backend instead of an external service specified by --default-backend-service flag.  The internal default backend responds with 200 status code when /healthz is requested.  It responds with 404 status code to the other requests.  The internal default backend can still be overridden by Ingress resource unless --no-default-backend-override flag is given.`)

	if err := rootCmd.Execute(); err != nil {
		klog.Exitf("Exiting due to command-line error: %v", err)
	}
}

func run(cmd *cobra.Command, args []string) {
	klog.Infof("Using build: %v - %v", gitRepo, version)

	var (
		defaultSvcKey       types.NamespacedName
		defaultTLSSecretKey *types.NamespacedName
		nghttpxConfigMapKey *types.NamespacedName
		publishSvcKey       *types.NamespacedName
	)

	if !internalDefaultBackend {
		if defaultSvc == "" {
			klog.Exitf("default-backend-service cannot be empty")
		}
		if ns, name, err := cache.SplitMetaNamespaceKey(defaultSvc); err != nil {
			klog.Exitf("default-backend-service: invalid Service identifier %v: %v", defaultSvc, err)
		} else {
			defaultSvcKey = types.NamespacedName{
				Namespace: ns,
				Name:      name,
			}
		}
	}

	if publishSvc != "" {
		if ns, name, err := cache.SplitMetaNamespaceKey(publishSvc); err != nil {
			klog.Exitf("publish-service: invalid Service identifier %v: %v", publishSvc, err)
		} else {
			publishSvcKey = &types.NamespacedName{
				Namespace: ns,
				Name:      name,
			}
		}
	}

	if ngxConfigMap != "" {
		if ns, name, err := cache.SplitMetaNamespaceKey(ngxConfigMap); err != nil {
			klog.Exitf("nghttpx-configmap: invalid ConfigMap identifier %v: %v", ngxConfigMap, err)
		} else {
			nghttpxConfigMapKey = &types.NamespacedName{
				Namespace: ns,
				Name:      name,
			}
		}
	}

	if defaultTLSSecret != "" {
		if ns, name, err := cache.SplitMetaNamespaceKey(defaultTLSSecret); err != nil {
			klog.Exitf("default-tls-secret: invalid Secret identifier %v: %v", defaultTLSSecret, err)
		} else {
			defaultTLSSecretKey = &types.NamespacedName{
				Namespace: ns,
				Name:      name,
			}
		}
	}

	var (
		err    error
		config *rest.Config
	)

	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		loadingRules := clientcmd.ClientConfigLoadingRules{
			ExplicitPath: kubeconfig,
		}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&loadingRules, &configOverrides).ClientConfig()
	}
	if err != nil {
		klog.Exitf("Could not get clientConfig: %v", err)
	}

	clientset, err := clientset.NewForConfig(config)
	if err != nil {
		klog.Exitf("Failed to create clientset: %v", err)
	}

	controllerConfig := controller.Config{
		DefaultBackendService:    defaultSvcKey,
		WatchNamespace:           watchNamespace,
		NghttpxConfigMap:         nghttpxConfigMapKey,
		NghttpxHealthPort:        nghttpxHealthPort,
		NghttpxAPIPort:           nghttpxAPIPort,
		NghttpxConfDir:           nghttpxConfDir,
		NghttpxExecPath:          nghttpxExecPath,
		NghttpxHTTPPort:          nghttpxHTTPPort,
		NghttpxHTTPSPort:         nghttpxHTTPSPort,
		DefaultTLSSecret:         defaultTLSSecretKey,
		IngressClassController:   ingressClassController,
		AllowInternalIP:          allowInternalIP,
		OCSPRespKey:              ocspRespKey,
		FetchOCSPRespFromSecret:  fetchOCSPRespFromSecret,
		ProxyProto:               proxyProto,
		PublishSvc:               publishSvcKey,
		EnableEndpointSlice:      endpointSlices,
		ReloadRate:               reloadRate,
		ReloadBurst:              reloadBurst,
		NoDefaultBackendOverride: noDefaultBackendOverride,
		DeferredShutdownPeriod:   deferredShutdownPeriod,
		HealthzPort:              healthzPort,
		InternalDefaultBackend:   internalDefaultBackend,
		PodInfo: types.NamespacedName{
			Name:      os.Getenv("POD_NAME"),
			Namespace: os.Getenv("POD_NAMESPACE"),
		},
	}

	if controllerConfig.PodInfo.Name == "" {
		klog.Exit("POD_NAME environment variable cannot be empty.")
	}
	if controllerConfig.PodInfo.Namespace == "" {
		klog.Exit("POD_NAMESPACE environment variable cannot be empty.")
	}

	mgrConfig := nghttpx.ManagerConfig{
		NghttpxHealthPort: nghttpxHealthPort,
		NghttpxAPIPort:    nghttpxAPIPort,
		NghttpxConfDir:    nghttpxConfDir,
	}

	mgr, err := nghttpx.NewManager(mgrConfig)
	if err != nil {
		klog.Exit(err)
	}

	lbc := controller.NewLoadBalancerController(clientset, mgr, controllerConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go registerHandlers(cancel)
	go handleSigterm(cancel)

	lbc.Run(ctx)
}

// healthzChecker implements healthz.HealthzChecker interface.
type healthzChecker struct {
	// targetURI is the nghttpx health monitor endpoint.
	targetURI string
}

// newHealthzChecker returns new healthzChecker.
func newHealthzChecker(healthPort int32) *healthzChecker {
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

func registerHandlers(cancel context.CancelFunc) {
	mux := http.NewServeMux()
	healthz.InstallHandler(mux, newHealthzChecker(nghttpxHealthPort))

	http.HandleFunc("/build", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "build: %v - %v", gitRepo, version)
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		cancel()
	})

	if profiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%v", healthzPort),
		Handler: mux,
	}
	klog.Exit(server.ListenAndServe())
}

func handleSigterm(cancel context.CancelFunc) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	klog.Infof("Received SIGTERM, shutting down")

	cancel()
}
