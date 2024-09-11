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
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	listersnetworkingv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewaylistersv1 "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1"

	slicesutil "github.com/zlabjp/nghttpx-ingress-lb/pkg/util/slices"
)

// loadBalancerIngressesIPEqual compares a and b, and if their IP fields are equal, returns true.  a and b might not be sorted in the
// particular order.  They just compared from first to last, and if there is a difference, this function returns false.
func loadBalancerIngressesIPEqual(a, b []networkingv1.IngressLoadBalancerIngress) bool {
	return slicesutil.EqualPtrFunc(a, b, func(a, b *networkingv1.IngressLoadBalancerIngress) bool {
		return a.IP == b.IP
	})
}

// sortLoadBalancerIngress sorts a by IP and Hostname in the ascending order.
func sortLoadBalancerIngress(lbIngs []networkingv1.IngressLoadBalancerIngress) {
	slices.SortFunc(lbIngs, func(a, b networkingv1.IngressLoadBalancerIngress) int {
		return cmp.Or(
			cmp.Compare(a.IP, b.IP),
			cmp.Compare(a.Hostname, b.Hostname),
		)
	})
}

// uniqLoadBalancerIngress removes duplicated items from a.  This function assumes a is sorted by sortLoadBalancerIngress.
func uniqLoadBalancerIngress(a []networkingv1.IngressLoadBalancerIngress) []networkingv1.IngressLoadBalancerIngress {
	return slices.CompactFunc(a, func(a, b networkingv1.IngressLoadBalancerIngress) bool {
		return a.IP == b.IP && a.Hostname == b.Hostname
	})
}

// podFindPort is copied from
// https://github.com/kubernetes/kubernetes/blob/886e04f1fffbb04faf8a9f9ee141143b2684ae68/pkg/api/v1/pod/util.go#L29 because original
// FindPort requires k8s.io/kubernetes/pkg/api/v1 while we use k8s.io/client-go/pkg/api/v1.

// podFindPort locates the container port for the given pod and portName.  If the targetPort is a number, use that.  If the targetPort is a
// string, look that string up in all named ports in all containers in the target pod.  If no match is found, fail.
func podFindPort(pod *corev1.Pod, svcPort *corev1.ServicePort) (int32, error) {
	portName := svcPort.TargetPort
	switch portName.Type {
	case intstr.String:
		name := portName.StrVal

		for i := range pod.Spec.Containers {
			container := &pod.Spec.Containers[i]
			i := slicesutil.IndexPtrFunc(container.Ports, func(port *corev1.ContainerPort) bool {
				// port.Name must be unique inside Pod.
				return port.Name == name
			})

			if i != -1 {
				port := &container.Ports[i]
				if port.Protocol == svcPort.Protocol {
					return port.ContainerPort, nil
				}

				break
			}
		}
	case intstr.Int:
		return int32(portName.IntValue()), nil
	}

	return 0, fmt.Errorf("no suitable port for manifest: %s", pod.UID)
}

// podLabelSelector returns labels.Selector from labelSet.
func podLabelSelector(labelSet map[string]string) labels.Selector {
	l := make(map[string]string)
	// Remove labels which represent pod template hash, revision, or generation.
	for k, v := range labelSet {
		switch k {
		case appsv1.ControllerRevisionHashLabelKey:
		case "pod-template-generation": // Used by DaemonSet
		case appsv1.DefaultDeploymentUniqueLabelKey:
			continue
		}

		l[k] = v
	}

	return labels.ValidatedSetSelector(l)
}

// validateIngressClass checks whether this controller should process ing or not.
func validateIngressClass(ctx context.Context, ing *networkingv1.Ingress, ingressClassController string, ingClassLister listersnetworkingv1.IngressClassLister,
	requireIngressClass bool) bool {
	log := klog.FromContext(ctx)

	if ing.Spec.IngressClassName != nil {
		ingClass, err := ingClassLister.Get(*ing.Spec.IngressClassName)
		if err != nil {
			log.Error(err, "Unable to get IngressClass", "ingressClass", ing.Spec.IngressClassName)
			return false
		}

		if ingClass.Spec.Controller != ingressClassController {
			log.V(4).Info("Skip Ingress", "ingress", klog.KObj(ing), "ingressClass", klog.KObj(ingClass),
				"controller", ingClass.Spec.Controller)
			return false
		}

		return true
	}

	if requireIngressClass {
		// Requiring IngressClass is the intended behavior of Ingress resource.  But historically, we do this differently.
		return false
	}

	// Check defaults

	ingClasses, err := ingClassLister.List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list IngressClass")
		return false
	}

	for _, ingClass := range ingClasses {
		if ingClass.Annotations[networkingv1.AnnotationIsDefaultIngressClass] != "true" {
			continue
		}

		if ingClass.Spec.Controller != ingressClassController {
			log.V(4).Info("Skip Ingress because of default IngressClass", "ingress", klog.KObj(ing),
				"ingressClass", klog.KObj(ingClass), "controller", ingClass.Spec.Controller)
			return false
		}

		return true
	}

	// If there is no default IngressClass, process the Ingress.
	return true
}

func validateGatewayGatewayClass(ctx context.Context, gtw *gatewayv1.Gateway, gatewayClassController gatewayv1.GatewayController,
	gatewayClassLister gatewaylistersv1.GatewayClassLister) bool {
	log := klog.FromContext(ctx)

	log = klog.LoggerWithValues(log, "gateway", klog.KObj(gtw))

	gc, err := gatewayClassLister.Get(string(gtw.Spec.GatewayClassName))
	if err != nil {
		log.Error(err, "Unable to get GatewayClass")

		return false
	}

	return gc.Spec.ControllerName == gatewayClassController
}

// parentGateway returns true if paRef refers to a Gateway that exists in namespace.
func parentGateway(paRef *gatewayv1.ParentReference, namespace string) bool {
	return ptr.Deref(paRef.Group, gatewayv1.GroupName) == gatewayv1.GroupName &&
		ptr.Deref(paRef.Kind, "Gateway") == "Gateway" &&
		ptr.Deref(paRef.Namespace, gatewayv1.Namespace(namespace)) == gatewayv1.Namespace(namespace)
}

func validateHTTPRouteGatewayClass(ctx context.Context, httpRoute *gatewayv1.HTTPRoute, gatewayClassController gatewayv1.GatewayController,
	gatewayClassLister gatewaylistersv1.GatewayClassLister, gatewayLister gatewaylistersv1.GatewayLister) bool {
	log := klog.FromContext(ctx)

	log = klog.LoggerWithValues(log, "httpRoute", klog.KObj(httpRoute))

	for i := range httpRoute.Spec.ParentRefs {
		paRef := &httpRoute.Spec.ParentRefs[i]

		if !parentGateway(paRef, httpRoute.Namespace) {
			continue
		}

		gtw, err := gatewayLister.Gateways(httpRoute.Namespace).Get(string(paRef.Name))
		if err != nil {
			log.Error(err, "Unable to get Gateway")
			continue
		}

		if validateGatewayGatewayClass(ctx, gtw, gatewayClassController, gatewayClassLister) {
			return true
		}
	}

	return false
}

func ingressLoadBalancerIngressFromService(svc *corev1.Service) []networkingv1.IngressLoadBalancerIngress {
	l := len(svc.Status.LoadBalancer.Ingress) + len(svc.Spec.ExternalIPs)
	if l == 0 {
		return nil
	}

	lbIngs := make([]networkingv1.IngressLoadBalancerIngress, l)

	for i := range svc.Status.LoadBalancer.Ingress {
		dst := &lbIngs[i]
		src := &svc.Status.LoadBalancer.Ingress[i]

		dst.IP = src.IP
		dst.Hostname = src.Hostname
		dst.Ports = ingressPortStatusFromPortStatus(src.Ports)
	}

	i := len(svc.Status.LoadBalancer.Ingress)

	for _, ip := range svc.Spec.ExternalIPs {
		lbIngs[i].IP = ip
		i++
	}

	return lbIngs
}

func ingressPortStatusFromPortStatus(ports []corev1.PortStatus) []networkingv1.IngressPortStatus {
	if len(ports) == 0 {
		return nil
	}

	ingPorts := make([]networkingv1.IngressPortStatus, len(ports))

	for i := range ports {
		dst := &ingPorts[i]
		src := &ports[i]

		dst.Port = src.Port
		dst.Protocol = src.Protocol
		dst.Error = src.Error
	}

	return ingPorts
}

func createCertCacheKey(s *corev1.Secret) types.NamespacedName {
	return namespacedName(s)
}

func calculateCertificateHash(cert, key []byte) []byte {
	h := sha256.New()
	h.Write(cert)
	h.Write(key)

	return h.Sum(nil)
}

// parentRefEqual returns true if a and b are equal.  namespace is the defaulted namespace.
func parentRefEqual(a, b *gatewayv1.ParentReference, namespace string) bool {
	return ptr.Deref(a.Group, gatewayv1.GroupName) == ptr.Deref(b.Group, gatewayv1.GroupName) &&
		ptr.Deref(a.Kind, "Gateway") == ptr.Deref(b.Kind, "Gateway") &&
		ptr.Deref(a.Namespace, gatewayv1.Namespace(namespace)) == ptr.Deref(b.Namespace, gatewayv1.Namespace(namespace)) &&
		a.Name == b.Name &&
		ptr.Deref(a.SectionName, "*") == ptr.Deref(b.SectionName, "*") &&
		ptr.Deref(a.Port, 65536) == ptr.Deref(b.Port, 65536)
}

// findCondition returns a pointer to the first metav1.Condition whose Type is condType in conditions.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	i := slices.IndexFunc(conditions, func(cond metav1.Condition) bool {
		return cond.Type == condType
	})
	if i == -1 {
		return nil
	}

	return &conditions[i]
}

// appendCondition appends cond to conditions, and returns the updated slice and the pointer to the appended condition.
func appendCondition(conditions []metav1.Condition, cond metav1.Condition) ([]metav1.Condition, *metav1.Condition) {
	conditions = append(conditions, cond)
	return conditions, &conditions[len(conditions)-1]
}

// findHTTPRouteParentStatus returns a pointer to the first gatewayv1.RouteParentStatus which includes paRef in httpRoute.Status.Parents.
func findHTTPRouteParentStatus(httpRoute *gatewayv1.HTTPRoute, paRef *gatewayv1.ParentReference) *gatewayv1.RouteParentStatus {
	i := slices.IndexFunc(httpRoute.Status.Parents, func(s gatewayv1.RouteParentStatus) bool {
		return parentRefEqual(&s.ParentRef, paRef, httpRoute.Namespace)
	})
	if i == -1 {
		return nil
	}

	return &httpRoute.Status.Parents[i]
}

// hostnameMatch returns true if pattern matches hostname.
func hostnameMatch(pattern, hostname gatewayv1.Hostname) bool {
	if strings.HasPrefix(string(pattern), "*.") {
		return len(hostname) >= len(pattern) && strings.HasSuffix(string(hostname), string(pattern[1:]))
	}

	return pattern == hostname
}
