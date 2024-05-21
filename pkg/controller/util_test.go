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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
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
			desc: "Empty input",
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

func TestParentGateway(t *testing.T) {
	tests := []struct {
		desc      string
		namespace string
		parentRef gatewayv1.ParentReference
		want      bool
	}{
		{
			desc:      "Just defaults",
			namespace: "ns",
			want:      true,
		},
		{
			desc:      "Gateway with all fields",
			namespace: "ns",
			parentRef: gatewayv1.ParentReference{
				Group:     ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
				Kind:      ptr.To(gatewayv1.Kind("Gateway")),
				Namespace: ptr.To(gatewayv1.Namespace("ns")),
			},
			want: true,
		},
		{
			desc:      "Gateway with another namespace",
			namespace: "ns",
			parentRef: gatewayv1.ParentReference{
				Namespace: ptr.To(gatewayv1.Namespace("another-ns")),
			},
		},
		{
			desc:      "ConfigMap with all fields",
			namespace: "ns",
			parentRef: gatewayv1.ParentReference{
				Kind:      ptr.To(gatewayv1.Kind("ConfigMap")),
				Namespace: ptr.To(gatewayv1.Namespace("ns")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := parentGateway(&tt.parentRef, tt.namespace), tt.want; got != want {
				t.Errorf("parentGateway(...) = %v, want %v", got, want)
			}
		})
	}
}

func TestParentRefEqual(t *testing.T) {
	tests := []struct {
		desc      string
		a, b      gatewayv1.ParentReference
		namespace string
		want      bool
	}{
		{
			desc: "Just names",
			a: gatewayv1.ParentReference{
				Name: "foo",
			},
			b: gatewayv1.ParentReference{
				Name: "foo",
			},
			namespace: "ns",
			want:      true,
		},
		{
			desc: "Just names but do not match",
			a: gatewayv1.ParentReference{
				Name: "foo",
			},
			b: gatewayv1.ParentReference{
				Name: "bar",
			},
			namespace: "ns",
		},
		{
			desc: "Full fields",
			a: gatewayv1.ParentReference{
				Group:       ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
				Kind:        ptr.To(gatewayv1.Kind("Gateway")),
				Namespace:   ptr.To(gatewayv1.Namespace("ns")),
				Name:        "foo",
				SectionName: ptr.To(gatewayv1.SectionName("https")),
				Port:        ptr.To(gatewayv1.PortNumber(443)),
			},
			b: gatewayv1.ParentReference{
				Group:       ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
				Kind:        ptr.To(gatewayv1.Kind("Gateway")),
				Namespace:   ptr.To(gatewayv1.Namespace("ns")),
				Name:        "foo",
				SectionName: ptr.To(gatewayv1.SectionName("https")),
				Port:        ptr.To(gatewayv1.PortNumber(443)),
			},
			namespace: "ns",
			want:      true,
		},
		{
			desc: "Full fields vs omitting Group, Kind, and Namespace",
			a: gatewayv1.ParentReference{
				Group:       ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
				Kind:        ptr.To(gatewayv1.Kind("Gateway")),
				Namespace:   ptr.To(gatewayv1.Namespace("ns")),
				Name:        "foo",
				SectionName: ptr.To(gatewayv1.SectionName("https")),
				Port:        ptr.To(gatewayv1.PortNumber(443)),
			},
			b: gatewayv1.ParentReference{
				Name:        "foo",
				SectionName: ptr.To(gatewayv1.SectionName("https")),
				Port:        ptr.To(gatewayv1.PortNumber(443)),
			},
			namespace: "ns",
			want:      true,
		},
		{
			desc: "Groups do not match",
			a: gatewayv1.ParentReference{
				Group: ptr.To(gatewayv1.Group(gatewayv1.GroupName)),
				Name:  "foo",
			},
			b: gatewayv1.ParentReference{
				Group: ptr.To(gatewayv1.Group("")),
				Name:  "foo",
			},
			namespace: "ns",
		},
		{
			desc: "Kinds do not match",
			a: gatewayv1.ParentReference{
				Kind: ptr.To(gatewayv1.Kind("Gateway")),
				Name: "foo",
			},
			b: gatewayv1.ParentReference{
				Kind: ptr.To(gatewayv1.Kind("")),
				Name: "foo",
			},
			namespace: "ns",
		},
		{
			desc: "Namespaces do not match",
			a: gatewayv1.ParentReference{
				Namespace: ptr.To(gatewayv1.Namespace("ns")),
				Name:      "foo",
			},
			b: gatewayv1.ParentReference{
				Namespace: ptr.To(gatewayv1.Namespace("")),
				Name:      "foo",
			},
			namespace: "ns",
		},
		{
			desc: "Defaulted namespace does not match",
			a: gatewayv1.ParentReference{
				Namespace: ptr.To(gatewayv1.Namespace("bar")),
				Name:      "foo",
			},
			b: gatewayv1.ParentReference{
				Name: "foo",
			},
			namespace: "ns",
		},
		{
			desc: "SectionNames do not match",
			a: gatewayv1.ParentReference{
				Name:        "foo",
				SectionName: ptr.To(gatewayv1.SectionName("https")),
			},
			b: gatewayv1.ParentReference{
				Name: "foo",
			},
			namespace: "ns",
		},
		{
			desc: "Ports do not match",
			a: gatewayv1.ParentReference{
				Name: "foo",
				Port: ptr.To(gatewayv1.PortNumber(443)),
			},
			b: gatewayv1.ParentReference{
				Name: "foo",
			},
			namespace: "ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := parentRefEqual(&tt.a, &tt.b, tt.namespace), tt.want; got != want {
				t.Errorf("parentRefEqual(...) = %v, want %v", got, want)
			}
		})
	}
}

func TestFindCondition(t *testing.T) {
	tests := []struct {
		desc          string
		conditions    []metav1.Condition
		conditionType string
		want          int
	}{
		{
			desc:          "Empty conditions",
			conditionType: "ready",
			want:          -1,
		},
		{
			desc: "Condition found",
			conditions: []metav1.Condition{
				{
					Type: "accepted",
				},
				{
					Type: "ready",
				},
			},
			conditionType: "ready",
			want:          1,
		},
		{
			desc: "Condition not found",
			conditions: []metav1.Condition{
				{
					Type: "accepted",
				},
				{
					Type: "ready",
				},
			},
			conditionType: "programmed",
			want:          -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := findCondition(tt.conditions, tt.conditionType)

			var want *metav1.Condition

			if tt.want != -1 {
				want = &tt.conditions[tt.want]
			}

			if got != want {
				t.Errorf("findCondition(...) = %#v, want %#v", got, want)
			}
		})
	}
}

func TestFindHTTPRouteParentStatus(t *testing.T) {
	tests := []struct {
		desc      string
		httpRoute gatewayv1.HTTPRoute
		parentRef gatewayv1.ParentReference
		want      int
	}{
		{
			desc: "Empty status",
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns",
				},
			},
			parentRef: gatewayv1.ParentReference{
				Name: "foo",
			},
			want: -1,
		},
		{
			desc: "Status includes reference",
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns",
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "foo",
								},
							},
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "bar",
								},
							},
						},
					},
				},
			},
			parentRef: gatewayv1.ParentReference{
				Name: "foo",
			},
		},
		{
			desc: "Status does not include reference",
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns",
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "foo",
								},
							},
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "bar",
								},
							},
						},
					},
				},
			},
			parentRef: gatewayv1.ParentReference{
				Name: "alpha",
			},
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := findHTTPRouteParentStatus(&tt.httpRoute, &tt.parentRef)

			var want *gatewayv1.RouteParentStatus

			if tt.want != -1 {
				want = &tt.httpRoute.Status.Parents[tt.want]
			}

			if got != want {
				t.Errorf("findHTTPRouteParentStatus(...) = %#v, want %#v", got, want)
			}
		})
	}
}
