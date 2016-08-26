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
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/pflag"

	"github.com/zlabjp/nghttpx-ingress-lb/nghttpx"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/healthz"
	kubectl_util "k8s.io/kubernetes/pkg/kubectl/cmd/util"
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
		`Service used to serve a 404 page for the default backend. Takes the form
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

	buildCfg = flags.Bool("dump-nghttpx-configuration", false, `Returns a ConfigMap with the default nghttpx conguration.
		This can be used as a guide to create a custom configuration.`)

	profiling = flags.Bool("profiling", true, `Enable profiling via web interface host:port/debug/pprof/`)

	allowInternalIP = flags.Bool("allow-internal-ip", false, `Allow to use address of type NodeInternalIP when fetching
                external IP address. This is the workaround for the cluster configuration where NodeExternalIP or
                NodeLegacyHostIP is not assigned or cannot be used.`)
)

func main() {
	var kubeClient *unversioned.Client
	flags.AddGoFlagSet(flag.CommandLine)
	flags.Parse(os.Args)
	clientConfig := kubectl_util.DefaultClientConfig(flags)

	glog.Infof("Using build: %v - %v", gitRepo, version)

	if *buildCfg {
		fmt.Printf("Example of ConfigMap to customize nghttpx configuration:\n%v", nghttpx.ConfigMapAsString())
		os.Exit(0)
	}

	if *defaultSvc == "" {
		glog.Fatalf("Please specify --default-backend-service")
	}

	var err error
	if *inCluster {
		myconfig, err := restclient.InClusterConfig()
		if err == nil {
			glog.Infof("apiserver host = %v", myconfig.Host)
		}
		kubeClient, err = unversioned.NewInCluster()
	} else {
		config, connErr := clientConfig.ClientConfig()
		if connErr != nil {
			glog.Fatalf("error connecting to the client: %v", err)
		}
		kubeClient, err = unversioned.New(config)
	}
	if err != nil {
		glog.Fatalf("failed to create client: %v", err)
	}

	runtimePodInfo := &podInfo{NodeIP: "127.0.0.1"}
	if *inCluster {
		runtimePodInfo, err = getPodDetails(kubeClient, *allowInternalIP)
		if err != nil {
			glog.Fatalf("unexpected error getting runtime information: %v", err)
		}
	}
	if err := isValidService(kubeClient, *defaultSvc); err != nil {
		glog.Fatalf("no service with name %v found: %v", *defaultSvc, err)
	}
	glog.Infof("Validated %v as the default backend", *defaultSvc)

	if *ngxConfigMap != "" {
		if _, _, err := parseNsName(*ngxConfigMap); err != nil {
			glog.Fatalf("could not parse configmap name %v: %v", *ngxConfigMap, err)
		}
	}

	lbc, err := newLoadBalancerController(kubeClient, *resyncPeriod, *defaultSvc, *watchNamespace, *ngxConfigMap, runtimePodInfo)
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

// podInfo contains runtime information about the pod
type podInfo struct {
	PodName      string
	PodNamespace string
	NodeIP       string
}

func registerHandlers(lbc *loadBalancerController) {
	mux := http.NewServeMux()
	healthz.InstallHandler(mux, lbc.nghttpx)

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

func handleSigterm(lbc *loadBalancerController) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	glog.Infof("Received SIGTERM, shutting down")

	lbc.Stop()
}
