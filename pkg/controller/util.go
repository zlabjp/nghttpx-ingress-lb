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

package controller

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/api"
	apierrs "k8s.io/kubernetes/pkg/api/errors"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/util/wait"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

// StoreToIngressLister makes a Store that lists Ingress.
type StoreToIngressLister struct {
	cache.Store
}

// StoreToSecrLister makes a Store that lists Secrets.
type StoreToSecretLister struct {
	cache.Store
}

// StoreToMapLister makes a Store that lists Configmaps.
type StoreToMapLister struct {
	cache.Store
}

// StoreToServiceLister makes a Store that lists Services.
type StoreToServiceLister struct {
	cache.Store
}

// podInfo contains runtime information about the pod
type PodInfo struct {
	PodName      string
	PodNamespace string
	NodeIP       string
}

// GetPodDetails  returns runtime information about the pod: name, namespace and IP of the node
func GetPodDetails(clientset internalclientset.Interface, allowInternalIP bool) (*PodInfo, error) {
	podName := os.Getenv("POD_NAME")
	podNs := os.Getenv("POD_NAMESPACE")

	err := waitForPodRunning(clientset, podNs, podName, time.Millisecond*200, time.Second*30)
	if err != nil {
		return nil, err
	}

	pod, _ := clientset.Core().Pods(podNs).Get(podName)
	if pod == nil {
		return nil, fmt.Errorf("Unable to get POD information")
	}

	node, err := clientset.Core().Nodes().Get(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}

	var externalIP string
	for _, address := range node.Status.Addresses {
		if address.Type == api.NodeExternalIP {
			if address.Address != "" {
				externalIP = address.Address
				break
			}
		}

		if externalIP == "" && address.Type == api.NodeInternalIP && allowInternalIP {
			externalIP = address.Address
			continue
		}

		if externalIP == "" && address.Type == api.NodeLegacyHostIP {
			externalIP = address.Address
		}
	}

	if externalIP == "" {
		return nil, fmt.Errorf("no external IP found")
	}

	return &PodInfo{
		PodName:      podName,
		PodNamespace: podNs,
		NodeIP:       externalIP,
	}, nil
}

func IsValidService(clientset internalclientset.Interface, name string) error {
	if name == "" {
		return fmt.Errorf("empty string is not a valid service name")
	}

	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid name format (namespace/name) in service '%v'", name)
	}

	_, err := clientset.Core().Services(parts[0]).Get(parts[1])
	return err
}

func ParseNSName(input string) (string, string, error) {
	nsName := strings.Split(input, "/")
	if len(nsName) != 2 {
		return "", "", fmt.Errorf("invalid format (namespace/name) found in '%v'", input)
	}

	return nsName[0], nsName[1], nil
}

func waitForPodRunning(clientset internalclientset.Interface, ns, podName string, interval, timeout time.Duration) error {
	condition := func(pod *api.Pod) (bool, error) {
		if pod.Status.Phase == api.PodRunning {
			return true, nil
		}
		return false, nil
	}

	glog.Infof("ns=%v, podName=%v", ns, podName)

	return waitForPodCondition(clientset, ns, podName, condition, interval, timeout)
}

// waitForPodCondition waits for a pod in state defined by a condition (func)
func waitForPodCondition(clientset internalclientset.Interface, ns, podName string, condition func(pod *api.Pod) (bool, error),
	interval, timeout time.Duration) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		pod, err := clientset.Core().Pods(ns).Get(podName)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return false, err
			}
			return false, nil
		}

		done, err := condition(pod)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}

		return false, nil
	})
}

// fixupBackendConfig validates config, and fixes the invalid values inside it.  svc and port is service name and port that config is
// associated to.
func fixupBackendConfig(config nghttpx.PortBackendConfig, svc, port string) nghttpx.PortBackendConfig {
	glog.Infof("use port backend configuration for service %v: %+v", svc, config)
	switch config.Proto {
	case nghttpx.ProtocolH2, nghttpx.ProtocolH1:
		// OK
	case "":
		config.Proto = nghttpx.ProtocolH1
	default:
		glog.Errorf("unrecognized backend protocol %v for service %v, port %v", config.Proto, svc, port)
		config.Proto = nghttpx.ProtocolH1
	}
	switch config.Affinity {
	case "", nghttpx.AffinityNone, nghttpx.AffinityIP:
		// OK
	default:
		glog.Errorf("unsupported affinity method %v for service %v, port %v", config.Affinity, svc, port)
		config.Affinity = ""
	}
	return config
}
