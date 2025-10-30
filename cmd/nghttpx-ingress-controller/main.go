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
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/server/healthz"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/events"
	"k8s.io/component-base/cli"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/component-base/featuregate"
	logsapiv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/controller"
	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
	flagsutil "github.com/zlabjp/nghttpx-ingress-lb/pkg/util/flags"
)

var (
	// value overwritten during build. This can be used to resolve issues.
	version = ""
	gitRepo = ""

	// Command-line flags
	defaultSvc               types.NamespacedName
	ngxConfigMap             types.NamespacedName
	kubeconfig               string
	watchNamespace           = metav1.NamespaceAll
	healthzPort              = int32(11249)
	nghttpxHealthPort        = int32(10901)
	nghttpxAPIPort           = int32(10902)
	profiling                = true
	allowInternalIP          = false
	defaultTLSSecret         types.NamespacedName
	ingressClassController   = "zlab.co.jp/nghttpx"
	nghttpxConfDir           = "/etc/nghttpx"
	nghttpxExecPath          = "/usr/local/bin/nghttpx"
	nghttpxHTTPPort          = int32(80)
	nghttpxHTTPSPort         = int32(443)
	fetchOCSPRespFromSecret  = false
	proxyProto               = false
	ocspRespKey              = "tls.ocsp-resp"
	publishSvc               types.NamespacedName
	reloadRate               = 1.0
	reloadBurst              = 1
	noDefaultBackendOverride = false
	deferredShutdownPeriod   time.Duration
	configOverrides          clientcmd.ConfigOverrides
	internalDefaultBackend   = true
	http3                    = false
	nghttpxSecret            = "nghttpx-km"
	shareTLSTicketKey        = false
	reconcileTimeout         = 10 * time.Minute
	leaderElectionConfig     = componentbaseconfig.LeaderElectionConfiguration{
		LeaseDuration: metav1.Duration{Duration: 15 * time.Second},
		RenewDeadline: metav1.Duration{Duration: 10 * time.Second},
		RetryPeriod:   metav1.Duration{Duration: 2 * time.Second},
		ResourceName:  "nghttpx-ingress-lb",
	}
	requireIngressClass                     = false
	nghttpxHealthTimeout                    = 30 * time.Second
	nghttpxWorkers                          = int32(runtime.NumCPU())
	nghttpxWorkerProcessGraceShutdownPeriod = time.Minute
	nghttpxMaxWorkerProcesses               = int32(100)
	reloadTimeout                           = 30 * time.Second
	clientQPS                               = float32(200)
	clientBurst                             = 300
	staleAssetsThreshold                    = time.Hour
	tlsTicketKeyPeriod                      = time.Hour
	quicSecretPeriod                        = 4 * time.Hour
	gatewayAPI                              = false
	gatewayClassController                  = "zlab.co.jp/nghttpx"
)

func main() {
	featureGate := featuregate.NewFeatureGate()
	utilruntime.Must(logsapiv1.AddFeatureGates(featureGate))

	loggingConf := logsapiv1.NewLoggingConfiguration()

	rootCmd := &cobra.Command{
		Use: "nghttpx-ingress-controller",
		Run: func(cmd *cobra.Command, args []string) {
			if err := logsapiv1.ValidateAndApply(loggingConf, featureGate); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				os.Exit(1)
			}

			run(context.Background(), cmd, args)
		},
	}

	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)
	clientcmd.BindOverrideFlags(&configOverrides, rootCmd.Flags(), clientcmd.RecommendedConfigOverrideFlags(""))

	rootCmd.Flags().Var(((*flagsutil.NamespacedName)(&defaultSvc)), "default-backend-service",
		`Service used to serve a 404 page for the default backend.  Takes the form namespace/name.  The controller uses the first port of this Service for the default backend.  This flag must be specified unless --internal-default-backend is given.`)

	rootCmd.Flags().Var((*flagsutil.NamespacedName)(&ngxConfigMap), "nghttpx-configmap",
		`Namespace/name of the ConfigMap that contains the custom nghttpx configuration to use.  Takes the form namespace/name.`)

	rootCmd.Flags().StringVar(&kubeconfig, "kubeconfig", kubeconfig, `Path to kubeconfig file which overrides in-cluster configuration.`)

	rootCmd.Flags().StringVar(&watchNamespace, "watch-namespace", watchNamespace, `Namespace to watch for Ingress.  Default is to watch all namespaces.`)

	rootCmd.Flags().Int32Var(&healthzPort, "healthz-port", healthzPort, "Port for healthz endpoint.")

	rootCmd.Flags().Int32Var(&nghttpxHealthPort, "nghttpx-health-port", nghttpxHealthPort, "Port for nghttpx health monitor endpoint.")

	rootCmd.Flags().Int32Var(&nghttpxAPIPort, "nghttpx-api-port", nghttpxAPIPort, "Port for nghttpx API endpoint.")

	rootCmd.Flags().BoolVar(&profiling, "profiling", profiling, `Enable profiling at the health port.  It exposes /debug/pprof/ endpoint on a port specified by --healthz-port.`)

	rootCmd.Flags().BoolVar(&allowInternalIP, "allow-internal-ip", allowInternalIP,
		`Allow to use address of type NodeInternalIP when fetching external IP address.  This is the workaround for the cluster configuration where NodeExternalIP or NodeLegacyHostIP is not assigned or cannot be used.`)

	rootCmd.Flags().Var((*flagsutil.NamespacedName)(&defaultTLSSecret), "default-tls-secret",
		`Name of the Secret that contains TLS server certificate and secret key to enable TLS by default.  For those client connections which are not TLS encrypted, they are redirected to https URI permanently.  The redirection can be turned off per Ingress basis with redirectIfNotTLS=false in ingress.zlab.co.jp/path-config annotation.`)

	rootCmd.Flags().StringVar(&ingressClassController, "ingress-class-controller", ingressClassController,
		`The name of IngressClass controller for this controller.  This is the value specified in IngressClass.spec.controller.`)

	rootCmd.Flags().StringVar(&nghttpxConfDir, "nghttpx-conf-dir", nghttpxConfDir,
		`Path to the directory which contains nghttpx configuration files.  The controller reads and writes these configuration files.`)

	rootCmd.Flags().StringVar(&nghttpxExecPath, "nghttpx-exec-path", nghttpxExecPath, `Path to the nghttpx executable.`)

	rootCmd.Flags().Int32Var(&nghttpxHTTPPort, "nghttpx-http-port", nghttpxHTTPPort, `Port to listen to for HTTP (non-TLS) requests.  Specifying 0 disables HTTP port.`)

	rootCmd.Flags().Int32Var(&nghttpxHTTPSPort, "nghttpx-https-port", nghttpxHTTPSPort, `Port to listen to for HTTPS (TLS) requests.  Specifying 0 disables HTTPS port.`)

	rootCmd.Flags().BoolVar(&fetchOCSPRespFromSecret, "fetch-ocsp-resp-from-secret", fetchOCSPRespFromSecret, `Fetch OCSP response from TLS secret.`)
	utilruntime.Must(rootCmd.Flags().MarkDeprecated("fetch-ocsp-resp-from-secret", "has been deprecated and has no effect"))

	rootCmd.Flags().BoolVar(&proxyProto, "proxy-proto", proxyProto, `Enable proxyproto for all public-facing frontends (api and health frontends are ignored).`)

	rootCmd.Flags().StringVar(&ocspRespKey, "ocsp-resp-key", ocspRespKey, `A key for OCSP response in TLS secret.`)
	utilruntime.Must(rootCmd.Flags().MarkDeprecated("ocsp-resp-key", "has been deprecated and has no effect"))

	rootCmd.Flags().Var((*flagsutil.NamespacedName)(&publishSvc), "publish-service",
		`Specify namespace/name of Service whose hostnames/IP addresses are set in Ingress resource instead of addresses of Ingress controller Pods.  Takes the form namespace/name.`)

	rootCmd.Flags().Float64Var(&reloadRate, "reload-rate", reloadRate,
		`Rate (QPS) of reloading nghttpx configuration to deal with frequent backend updates in a single batch.`)

	rootCmd.Flags().IntVar(&reloadBurst, "reload-burst", reloadBurst, `Reload burst that can exceed reload-rate.`)

	rootCmd.Flags().BoolVar(&noDefaultBackendOverride, "no-default-backend-override", noDefaultBackendOverride,
		`Ignore any settings or rules in Ingress resources which override default backend service.`)

	rootCmd.Flags().DurationVar(&deferredShutdownPeriod, "deferred-shutdown-period", deferredShutdownPeriod,
		`How long the controller waits before actually starting shutting down.  If nonzero value is given, additional health check endpoint is added to HTTP/HTTPS endpoint.  The health check request path is /nghttpx-healthz.  Normally, it returns 200 HTTP status code, but after entering this period, the endpoint returns 503.`)

	rootCmd.Flags().BoolVar(&internalDefaultBackend, "internal-default-backend", internalDefaultBackend,
		`Use the internal default backend instead of an external service specified by --default-backend-service flag.  The internal default backend responds with 200 status code when /healthz is requested.  It responds with 404 status code to the other requests.  The internal default backend can still be overridden by Ingress resource unless --no-default-backend-override flag is given.`)

	rootCmd.Flags().BoolVar(&http3, "http3", http3, `Enable HTTP/3.  This makes nghttpx listen to UDP port specified by nghttpx-https-port for HTTP/3 traffic.`)

	rootCmd.Flags().StringVar(&nghttpxSecret, "nghttpx-secret", nghttpxSecret, `The name of Secret resource which contains the keying materials for nghttpx.  The resource must belong to the same namespace as the controller Pod.  If it is not found, the controller will create new one.`)

	rootCmd.Flags().BoolVar(&shareTLSTicketKey, "share-tls-ticket-key", shareTLSTicketKey, `Share TLS ticket key among all nghttpx-ingress-controllers.  TLS ticket keys are stored to the Secret specified by nghttpx-secret flag.  If this flag is set to true, TLS ticket keys are generated and rotated by the controller in the interval specified by tls-ticket-key-period flag.  At most 12 latest keys are retained.  TLS tickets are encrypted with AES-128-CBC.`)

	rootCmd.Flags().DurationVar(&reconcileTimeout, "reconcile-timeout", reconcileTimeout,
		`A timeout for a single reconciliation.  It is a safe guard to prevent a reconciliation from getting stuck indefinitely.`)

	rootCmd.Flags().DurationVar(&leaderElectionConfig.LeaseDuration.Duration, "leader-elect-lease-duration", leaderElectionConfig.LeaseDuration.Duration,
		`Duration that non-leader will wait after observing the current leader renewed its leadership until attempting to acquire leadership.`)

	rootCmd.Flags().DurationVar(&leaderElectionConfig.RenewDeadline.Duration, "leader-elect-renew-deadline", leaderElectionConfig.RenewDeadline.Duration,
		`Interval that a leader renews its leadership.`)

	rootCmd.Flags().DurationVar(&leaderElectionConfig.RetryPeriod.Duration, "leader-elect-retry-period", leaderElectionConfig.RetryPeriod.Duration,
		`Duration that client should wait until acquiring or renewing leadership.`)

	rootCmd.Flags().StringVar(&leaderElectionConfig.ResourceName, "leader-elect-resource-name", leaderElectionConfig.ResourceName,
		`Name of leases.coordination.k8s.io resource that is used as a lock.`)

	rootCmd.Flags().BoolVar(&requireIngressClass, "require-ingress-class", requireIngressClass,
		`Ignore Ingress resource which does not specify .spec.ingressClassName.  Historically, nghttpx ingress controller processes Ingress resource which does not have .spec.ingressClassName specified.  It also interprets the default IngressClass in its own way.  If Ingress resource does not have .spec.ingressClassName specified, but the default IngressClass is not nghttpx ingress controller, it does not processes the resource.  If this flag is turned on, nghttpx follows the intended behavior around missing Ingress.spec.ingressClassName, that is ignore those resources that do not have .spec.ingressClassName.`)

	rootCmd.Flags().DurationVar(&nghttpxHealthTimeout, "nghttpx-health-timeout", nghttpxHealthTimeout,
		`Timeout for a request to nghttpx health monitor endpoint.`)

	rootCmd.Flags().Int32Var(&nghttpxWorkers, "nghttpx-workers", nghttpxWorkers,
		`The number of nghttpx worker threads.`)

	rootCmd.Flags().DurationVar(&nghttpxWorkerProcessGraceShutdownPeriod, "nghttpx-worker-process-grace-shutdown-period", nghttpxWorkerProcessGraceShutdownPeriod,
		`The maximum period for an nghttpx worker process to terminate gracefully.  Specifying 0 means no limit.`)

	rootCmd.Flags().Int32Var(&nghttpxMaxWorkerProcesses, "nghttpx-max-worker-processes", nghttpxMaxWorkerProcesses,
		`The maximum number of nghttpx worker processes which are spawned in every configuration reload.`)

	rootCmd.Flags().DurationVar(&reloadTimeout, "reload-timeout", reloadTimeout,
		`Timeout before confirming that nghttpx reloads configuration.`)

	rootCmd.Flags().Float32Var(&clientQPS, "client-qps", clientQPS,
		`QPS for Kubernetes API client request`)

	rootCmd.Flags().IntVar(&clientBurst, "client-burst", clientBurst,
		`Burst for Kubernetes API client request`)

	rootCmd.Flags().DurationVar(&staleAssetsThreshold, "stale-assets-threshold", staleAssetsThreshold,
		`Duration that asset files (e.g., TLS keys and certificates, and mruby files) are considered stale`)

	rootCmd.Flags().DurationVar(&tlsTicketKeyPeriod, "tls-ticket-key-period", tlsTicketKeyPeriod,
		`Duration before TLS ticket keys are rotated and new key is generated.  See share-tls-ticket-key flag.`)

	rootCmd.Flags().DurationVar(&quicSecretPeriod, "quic-secret-period", quicSecretPeriod,
		`Duration before QUIC keying materials are rotated and new key is generated.`)

	rootCmd.Flags().BoolVar(&gatewayAPI, "gateway-api", gatewayAPI, `Enable Gateway API.`)

	rootCmd.Flags().StringVar(&gatewayClassController, "gateway-class-controller", gatewayClassController,
		`The name of GatewayClass controller for this controller.  This is the value specified in GatewayClass.spec.controllerName.`)

	featureGate.AddFlag(rootCmd.Flags())
	logsapiv1.AddFlags(loggingConf, rootCmd.Flags())

	code := cli.Run(rootCmd)
	os.Exit(code)
}

func run(ctx context.Context, _ *cobra.Command, _ []string) {
	log := klog.FromContext(ctx)

	log.Info("Using build", "repository", gitRepo, "version", version)

	var (
		defaultSvcKey       *types.NamespacedName
		defaultTLSSecretKey *types.NamespacedName
		nghttpxConfigMapKey *types.NamespacedName
		publishSvcKey       *types.NamespacedName
	)

	if !internalDefaultBackend {
		if defaultSvc.Name == "" {
			log.Error(nil, "default-backend-service cannot be empty")
			os.Exit(1)
		}

		defaultSvcKey = &defaultSvc
	}

	if publishSvc.Name != "" {
		publishSvcKey = &publishSvc
	}

	if ngxConfigMap.Name != "" {
		nghttpxConfigMapKey = &ngxConfigMap
	}

	if defaultTLSSecret.Name != "" {
		defaultTLSSecretKey = &defaultTLSSecret
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
		log.Error(err, "Unable to get clientConfig")
		os.Exit(1)
	}

	config.QPS = clientQPS
	config.Burst = clientBurst

	clientset, err := clientset.NewForConfig(config)
	if err != nil {
		log.Error(err, "Unable to create clientset")
		os.Exit(1)
	}

	var gatewayClientset gatewayclientset.Interface

	if gatewayAPI {
		gatewayClientset, err = gatewayclientset.NewForConfig(config)
		if err != nil {
			log.Error(err, "Unable to create GatewayClientset")
			os.Exit(1)
		}
	}

	podInfo := types.NamespacedName{Name: os.Getenv("POD_NAME"), Namespace: os.Getenv("POD_NAMESPACE")}

	if podInfo.Name == "" {
		log.Error(nil, "POD_NAME environment variable cannot be empty")
		os.Exit(1)
	}

	if podInfo.Namespace == "" {
		log.Error(nil, "POD_NAMESPACE environment variable cannot be empty")
		os.Exit(1)
	}

	thisPod, err := getThisPod(clientset, podInfo)
	if err != nil {
		log.Error(err, "Unable to get Pod", "pod", podInfo)
		os.Exit(1)
	}

	eventBroadcasterCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	eventBroadcaster := events.NewBroadcaster(&events.EventSinkImpl{Interface: clientset.EventsV1()})
	eventBroadcaster.StartRecordingToSink(eventBroadcasterCtx.Done())
	eventRecorder := eventBroadcaster.NewRecorder(scheme.Scheme, "nghttpx-ingress-controller")

	controllerConfig := controller.Config{
		DefaultBackendService:                   defaultSvcKey,
		WatchNamespace:                          watchNamespace,
		NghttpxConfigMap:                        nghttpxConfigMapKey,
		NghttpxHealthPort:                       nghttpxHealthPort,
		NghttpxAPIPort:                          nghttpxAPIPort,
		NghttpxConfDir:                          nghttpxConfDir,
		NghttpxExecPath:                         nghttpxExecPath,
		NghttpxHTTPPort:                         nghttpxHTTPPort,
		NghttpxHTTPSPort:                        nghttpxHTTPSPort,
		NghttpxWorkers:                          nghttpxWorkers,
		NghttpxWorkerProcessGraceShutdownPeriod: nghttpxWorkerProcessGraceShutdownPeriod,
		NghttpxMaxWorkerProcesses:               nghttpxMaxWorkerProcesses,
		NghttpxSecret:                           types.NamespacedName{Name: nghttpxSecret, Namespace: thisPod.Namespace},
		DefaultTLSSecret:                        defaultTLSSecretKey,
		IngressClassController:                  ingressClassController,
		AllowInternalIP:                         allowInternalIP,
		ProxyProto:                              proxyProto,
		PublishService:                          publishSvcKey,
		ReloadRate:                              reloadRate,
		ReloadBurst:                             reloadBurst,
		NoDefaultBackendOverride:                noDefaultBackendOverride,
		DeferredShutdownPeriod:                  deferredShutdownPeriod,
		HealthzPort:                             healthzPort,
		InternalDefaultBackend:                  internalDefaultBackend,
		HTTP3:                                   http3,
		ShareTLSTicketKey:                       shareTLSTicketKey,
		ReconcileTimeout:                        reconcileTimeout,
		LeaderElectionConfig:                    leaderElectionConfig,
		RequireIngressClass:                     requireIngressClass,
		TLSTicketKeyPeriod:                      tlsTicketKeyPeriod,
		QUICSecretPeriod:                        quicSecretPeriod,
		GatewayAPI:                              gatewayAPI,
		GatewayClassController:                  gatewayv1.GatewayController(gatewayClassController),
		Pod:                                     thisPod,
		EventRecorder:                           eventRecorder,
	}

	lbConfig := nghttpx.LoadBalancerConfig{
		NghttpxHealthPort:    nghttpxHealthPort,
		NghttpxAPIPort:       nghttpxAPIPort,
		NghttpxConfDir:       nghttpxConfDir,
		Pod:                  thisPod,
		EventRecorder:        eventRecorder,
		ReloadTimeout:        reloadTimeout,
		StaleAssetsThreshold: staleAssetsThreshold,
	}

	lb, err := nghttpx.NewLoadBalancer(lbConfig)
	if err != nil {
		log.Error(err, "Unable to create LoadBalancer")
		os.Exit(1)
	}

	lbc, err := controller.NewLoadBalancerController(ctx, clientset, gatewayClientset, lb, controllerConfig)
	if err != nil {
		log.Error(err, "Unable to create LoadBalancerController")
		os.Exit(1)
	}

	ctx, cancel = context.WithCancel(ctx)
	defer cancel()

	go registerHandlers(ctx, cancel, lb)
	go handleSigterm(ctx, cancel)

	lbc.Run(ctx, config)

	eventBroadcaster.Shutdown()
}

// getThisPod returns a Pod denoted by podInfo.
func getThisPod(clientset clientset.Interface, podInfo types.NamespacedName) (*corev1.Pod, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return clientset.CoreV1().Pods(podInfo.Namespace).Get(ctx, podInfo.Name, metav1.GetOptions{})
}

// healthzChecker implements healthz.HealthzChecker interface.
type healthzChecker struct {
	// targetURI is the nghttpx health monitor endpoint.
	targetURI string
}

// newHealthzChecker returns new healthzChecker.
func newHealthzChecker(healthPort int32) *healthzChecker {
	return &healthzChecker{
		targetURI: "http://127.0.0.1:" + strconv.FormatInt(int64(healthPort), 10) + "/healthz",
	}
}

// Name returns the healthcheck name
func (hc healthzChecker) Name() string {
	return "nghttpx"
}

// Check returns if the nghttpx healthz endpoint is returning ok (status code 200)
func (hc healthzChecker) Check(_ *http.Request) error {
	ctx, cancel := context.WithTimeout(context.Background(), nghttpxHealthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hc.targetURI, nil)
	if err != nil {
		return err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return errors.New("nghttpx is unhealthy")
	}

	return nil
}

// readyzChecker checks the readiness of nghttpx.  It verifies that nghttpx has performed at least one configuration reloading.
type readyzChecker struct {
	lb *nghttpx.LoadBalancer
}

func newReadyzChecker(lb *nghttpx.LoadBalancer) *readyzChecker {
	return &readyzChecker{
		lb: lb,
	}
}

func (rc readyzChecker) Name() string {
	return "nghttpx-readyz"
}

func (rc readyzChecker) Check(_ *http.Request) error {
	if rc.lb.GetReloadCounter() == 0 {
		return errors.New("nghttpx has not finished the initial reloading")
	}

	return nil
}

func registerHandlers(ctx context.Context, cancel context.CancelFunc, lb *nghttpx.LoadBalancer) {
	log := klog.FromContext(ctx)

	mux := http.NewServeMux()
	healthz.InstallHandler(mux, newHealthzChecker(nghttpxHealthPort))
	healthz.InstallReadyzHandler(mux, newReadyzChecker(lb))

	http.HandleFunc("/build", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "build: %s - %s", gitRepo, version)
	})

	http.HandleFunc("/stop", func(http.ResponseWriter, *http.Request) {
		cancel()
	})

	if profiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	}

	server := &http.Server{
		Addr:    ":" + strconv.FormatInt(int64(healthzPort), 10),
		Handler: mux,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Error(err, "Internal HTTP server returned error")
		os.Exit(1)
	}
}

func handleSigterm(ctx context.Context, cancel context.CancelFunc) {
	log := klog.FromContext(ctx)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	log.Info("Received SIGTERM, shutting down")

	cancel()
}
