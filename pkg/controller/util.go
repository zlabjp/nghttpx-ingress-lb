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

package controller

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	extensionslisters "k8s.io/kubernetes/pkg/client/listers/extensions/internalversion"
)

// ingressLister makes a Store that lists Ingresses.
type ingressLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	extensionslisters.IngressLister
}

// secrLister makes a Store that lists Secrets.
type secretLister struct {
	cache.Store
}

// configMapLister makes a Store that lists ConfigMaps.
type configMapLister struct {
	cache.Store
}

// serviceLister makes a Store that lists Services.
type serviceLister struct {
	cache.Store
}

// podInfo contains runtime information about the pod
type PodInfo struct {
	PodName      string
	PodNamespace string
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

// depResyncPeriod returns duration between resync for resources other than Ingress.
//
// Inspired by Kubernetes apiserver: k8s.io/kubernetes/cmd/kube-controller-manager/app/controllermanager.go
func depResyncPeriod() time.Duration {
	factor := rand.Float64() + 1
	return time.Duration(float64(minDepResyncPeriod.Nanoseconds()) * factor)
}

// loadBalancerIngressesIPEqual compares a and b, and if their IP fields are equal, returns true.  a and b might not be sorted in the
// particular order.  They just compared from first to last, and if there is a difference, this function returns false.
func loadBalancerIngressesIPEqual(a, b []api.LoadBalancerIngress) bool {
	if len(a) != len(b) {
		return false
	}

	for i, _ := range a {
		if a[i].IP != b[i].IP {
			return false
		}
	}

	return true
}

// sortLoadBalancerIngress sorts a by IP and Hostname in the ascending order.
func sortLoadBalancerIngress(a []api.LoadBalancerIngress) {
	sort.Slice(a, func(i, j int) bool {
		return a[i].IP < a[j].IP || (a[i].IP == a[j].IP && a[i].Hostname < a[j].Hostname)
	})
}

// uniqLoadBalancerIngress removes duplicated items from a.  This function assumes a is sorted by sortLoadBalancerIngress.
func uniqLoadBalancerIngress(a []api.LoadBalancerIngress) []api.LoadBalancerIngress {
	if len(a) == 0 {
		return a
	}
	p := 0
	for i := 1; i < len(a); i++ {
		if a[p] == a[i] {
			continue
		}
		p++
		if p != i {
			a[p] = a[i]
		}
	}

	return a[:p+1]
}

// removeAddressFromLoadBalancerIngress removes addr from a.  addr may match IP or Hostname.
func removeAddressFromLoadBalancerIngress(a []api.LoadBalancerIngress, addr string) []api.LoadBalancerIngress {
	p := 0
	for i, _ := range a {
		if a[i].IP == addr || a[i].Hostname == addr {
			continue
		}
		if p != i {
			a[p] = a[i]
		}
		p++
	}
	return a[:p]
}
