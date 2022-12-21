/**
 * Copyright 2017, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

// TestSortLoadBalancerIngress verifies that sortLoadBalancerIngress sorts given items.
func TestSortLoadBalancerIngress(t *testing.T) {
	input := []networkingv1.IngressLoadBalancerIngress{
		{IP: "delta", Hostname: "alpha"},
		{IP: "alpha", Hostname: "delta"},
		{IP: "alpha", Hostname: "charlie"},
		{IP: "bravo", Hostname: ""},
	}

	ans := []networkingv1.IngressLoadBalancerIngress{
		{IP: "alpha", Hostname: "charlie"},
		{IP: "alpha", Hostname: "delta"},
		{IP: "bravo", Hostname: ""},
		{IP: "delta", Hostname: "alpha"},
	}

	sortLoadBalancerIngress(input)

	if got, want := input, ans; !reflect.DeepEqual(got, want) {
		t.Errorf("sortLoadBalancerIngress(...) = %+v, want %+v", got, want)
	}
}

// TestUniqLoadBalancerIngress verifies that uniqLoadBalancerIngress removes duplicated items.
func TestUniqLoadBalancerIngress(t *testing.T) {
	tests := []struct {
		desc  string
		input []networkingv1.IngressLoadBalancerIngress
		ans   []networkingv1.IngressLoadBalancerIngress
	}{
		{
			desc: "Emptry input",
		},
		{
			desc: "With duplicates",
			input: []networkingv1.IngressLoadBalancerIngress{
				{IP: "alpha", Hostname: "bravo"},
				{IP: "alpha", Hostname: "bravo"},
				{IP: "bravo", Hostname: "alpha"},
				{IP: "delta", Hostname: ""},
				{IP: "delta", Hostname: ""},
			},
			ans: []networkingv1.IngressLoadBalancerIngress{
				{IP: "alpha", Hostname: "bravo"},
				{IP: "bravo", Hostname: "alpha"},
				{IP: "delta", Hostname: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := uniqLoadBalancerIngress(tt.input), tt.ans; !reflect.DeepEqual(got, want) {
				t.Errorf("uniqLoadBalancerIngress(...) = %+v, want %+v", got, want)
			}
		})
	}
}

// TestIngressLoadBalancerIngressFromService verifies ingressLoadBalancerIngressFromService.
func TestIngressLoadBalancerIngressFromService(t *testing.T) {
	svc := &corev1.Service{
		Spec: corev1.ServiceSpec{
			ExternalIPs: []string{
				"192.168.0.2",
				"192.168.0.1",
				"192.168.0.3",
			},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						Hostname: "charlie.example.com",
					},
					{
						IP: "10.0.0.1",
					},
				},
			},
		},
	}

	want := []networkingv1.IngressLoadBalancerIngress{
		{
			Hostname: "charlie.example.com",
		},
		{
			IP: "10.0.0.1",
		},
		{
			IP: "192.168.0.2",
		},
		{
			IP: "192.168.0.1",
		},
		{
			IP: "192.168.0.3",
		},
	}

	if got, want := ingressLoadBalancerIngressFromService(svc), want; !reflect.DeepEqual(got, want) {
		t.Errorf("ingressLoadBalancerIngressFromService(...) = %#v, want %#v", got, want)
	}
}
