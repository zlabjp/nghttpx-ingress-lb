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

package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/healthz"
	kubectl_util "k8s.io/kubernetes/pkg/kubectl/cmd/util"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/controller"
	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

const (
	healthPort = 10249
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
		`Optional, if this controller is running in a kubernetes cluster, use the
		 pod secrets for creating a Kubernetes client.`)

	resyncPeriod = flags.Duration("sync-period", 30*time.Second,
		`Relist and confirm cloud resources this often.`)

	watchNamespace = flags.String("watch-namespace", api.NamespaceAll,
		`Namespace to watch for Ingress. Default is to watch all namespaces`)

	healthzPort = flags.Int("healthz-port", healthPort, "port for healthz endpoint.")

	buildCfg = flags.Bool("dump-nghttpx-configuration", false, `Deprecated`)

	profiling = flags.Bool("profiling", true, `Enable profiling via web interface host:port/debug/pprof/`)

	allowInternalIP = flags.Bool("allow-internal-ip", false, `Allow to use address of type NodeInternalIP when fetching
                external IP address. This is the workaround for the cluster configuration where NodeExternalIP or
                NodeLegacyHostIP is not assigned or cannot be used.`)

	defaultTLSSecret = flags.String("default-tls-secret", "",
		`Optional, name of the Secret that contains TLS server certificate and secret key to enable TLS by default.`)
)

func main() {
	// We use math/rand to choose interval of resync
	rand.Seed(time.Now().UTC().UnixNano())

	flags.AddGoFlagSet(flag.CommandLine)
	flags.Parse(os.Args)

	clientConfig := kubectl_util.DefaultClientConfig(flags)

	glog.Infof("Using build: %v - %v", gitRepo, version)

	if *buildCfg {
		fmt.Println("dump-nghttpx-configuration was deprecated.")
		os.Exit(0)
	}

	if *defaultSvc == "" {
		glog.Fatalf("Please specify --default-backend-service")
	}

	var err error
	var config *restclient.Config
	if *inCluster {
		config, err = restclient.InClusterConfig()
		if err != nil {
			glog.Fatalf("Could not get clientConfig: %v", err)
		}
	} else {
		config, err = clientConfig.ClientConfig()
		if err != nil {
			glog.Fatalf("error connecting to the client: %v", err)
		}
	}

	clientset, err := internalclientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create clientset: %v", err)
	}

	runtimePodInfo := &controller.PodInfo{NodeIP: "127.0.0.1"}
	if *inCluster {
		runtimePodInfo, err = controller.GetPodDetails(clientset, *allowInternalIP)
		if err != nil {
			glog.Fatalf("unexpected error getting runtime information: %v", err)
		}
	}
	if err := controller.IsValidService(clientset, *defaultSvc); err != nil {
		glog.Fatalf("no service with name %v found: %v", *defaultSvc, err)
	}
	glog.Infof("Validated %v as the default backend", *defaultSvc)

	if *ngxConfigMap != "" {
		if _, _, err := controller.ParseNSName(*ngxConfigMap); err != nil {
			glog.Fatalf("could not parse configmap name %v: %v", *ngxConfigMap, err)
		}
	}

	if *defaultTLSSecret != "" {
		if _, _, err := controller.ParseNSName(*defaultTLSSecret); err != nil {
			glog.Fatalf("could not parse Secret %v: %v", *defaultTLSSecret, err)
		}
	}

	controllerConfig := controller.Config{
		ResyncPeriod:          *resyncPeriod,
		DefaultBackendService: *defaultSvc,
		WatchNamespace:        *watchNamespace,
		NghttpxConfigMap:      *ngxConfigMap,
		DefaultTLSSecret:      *defaultTLSSecret,
	}

	lbc, err := controller.NewLoadBalancerController(clientset, nghttpx.NewManager(), &controllerConfig, runtimePodInfo)
	if err != nil {
		glog.Fatalf("%v", err)
	}

	go registerHandlers(lbc)
	go handleSigterm(lbc)

	lbc.Run()

	for {
		glog.Infof("Waiting for pod deletion...")
		time.Sleep(30 * time.Second)
	}
}

// healthzChecker implements healthz.HealthzChecker interface.
type healthzChecker struct{}

// Name returns the healthcheck name
func (hc healthzChecker) Name() string {
	return "nghttpx"
}

// Check returns if the nghttpx healthz endpoint is returning ok (status code 200)
func (hc healthzChecker) Check(_ *http.Request) error {
	res, err := http.Get("http://127.0.0.1:8080/healthz")
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
	healthz.InstallHandler(mux, &healthzChecker{})

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
	glog.Fatal(server.ListenAndServe())
}

func handleSigterm(lbc *controller.LoadBalancerController) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	glog.Infof("Received SIGTERM, shutting down")

	lbc.Stop()
}
