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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
)

// podInfo contains runtime information about the pod
type PodInfo struct {
	PodName      string
	PodNamespace string
}

func IsValidService(clientset clientset.Interface, name string) error {
	if name == "" {
		return fmt.Errorf("empty string is not a valid service name")
	}

	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid name format (namespace/name) in service '%v'", name)
	}

	_, err := clientset.CoreV1().Services(parts[0]).Get(parts[1], metav1.GetOptions{})
	return err
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
func loadBalancerIngressesIPEqual(a, b []v1.LoadBalancerIngress) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].IP != b[i].IP {
			return false
		}
	}

	return true
}

// sortLoadBalancerIngress sorts a by IP and Hostname in the ascending order.
func sortLoadBalancerIngress(a []v1.LoadBalancerIngress) {
	sort.Slice(a, func(i, j int) bool {
		return a[i].IP < a[j].IP || (a[i].IP == a[j].IP && a[i].Hostname < a[j].Hostname)
	})
}

// uniqLoadBalancerIngress removes duplicated items from a.  This function assumes a is sorted by sortLoadBalancerIngress.
func uniqLoadBalancerIngress(a []v1.LoadBalancerIngress) []v1.LoadBalancerIngress {
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
func removeAddressFromLoadBalancerIngress(a []v1.LoadBalancerIngress, addr string) []v1.LoadBalancerIngress {
	p := 0
	for i := range a {
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

// podFindPort is copied from
// https://github.com/kubernetes/kubernetes/blob/886e04f1fffbb04faf8a9f9ee141143b2684ae68/pkg/api/v1/pod/util.go#L29 because original
// FindPort requires k8s.io/kubernetes/pkg/api/v1 while we use k8s.io/client-go/pkg/api/v1.

// podFindPort locates the container port for the given pod and portName.  If the
// targetPort is a number, use that.  If the targetPort is a string, look that
// string up in all named ports in all containers in the target pod.  If no
// match is found, fail.
func podFindPort(pod *v1.Pod, svcPort *v1.ServicePort) (int, error) {
	portName := svcPort.TargetPort
	switch portName.Type {
	case intstr.String:
		name := portName.StrVal
		for _, container := range pod.Spec.Containers {
			for _, port := range container.Ports {
				if port.Name == name && port.Protocol == svcPort.Protocol {
					return int(port.ContainerPort), nil
				}
			}
		}
	case intstr.Int:
		return portName.IntValue(), nil
	}

	return 0, fmt.Errorf("no suitable port for manifest: %s", pod.UID)
}
