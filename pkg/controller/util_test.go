/**
 * Copyright 2017, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// TestSortLoadBalancerIngress verifies that sortLoadBalancerIngress sorts given items.
func TestSortLoadBalancerIngress(t *testing.T) {
	input := []networkingv1.IngressLoadBalancerIngress{
		{IP: "delta", Hostname: "alpha"},
		{IP: "alpha", Hostname: "delta"},
		{IP: "alpha", Hostname: "charlie"},
		{IP: "bravo"},
	}

	ans := []networkingv1.IngressLoadBalancerIngress{
		{IP: "alpha", Hostname: "charlie"},
		{IP: "alpha", Hostname: "delta"},
		{IP: "bravo"},
		{IP: "delta", Hostname: "alpha"},
	}

	sortLoadBalancerIngress(input)

	assert.Equal(t, ans, input)
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
				{IP: "delta"},
				{IP: "delta"},
			},
			ans: []networkingv1.IngressLoadBalancerIngress{
				{IP: "alpha", Hostname: "bravo"},
				{IP: "bravo", Hostname: "alpha"},
				{IP: "delta"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.ans, uniqLoadBalancerIngress(tt.input))
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

	assert.Equal(t, want, ingressLoadBalancerIngressFromService(svc))
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
			assert.Equal(t, tt.want, parentGateway(&tt.parentRef, tt.namespace))
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
			assert.Equal(t, tt.want, parentRefEqual(&tt.a, &tt.b, tt.namespace))
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

			assert.Equal(t, want, got)
		})
	}
}

func TestAppendCondition(t *testing.T) {
	tests := []struct {
		desc       string
		conditions []metav1.Condition
		cond       metav1.Condition
		want       []metav1.Condition
	}{
		{
			desc: "Empty conditions",
			cond: metav1.Condition{Type: "foo"},
			want: []metav1.Condition{{Type: "foo"}},
		},
		{
			desc:       "Non-empty conditions",
			conditions: []metav1.Condition{{Type: "foo"}},
			cond:       metav1.Condition{Type: "bar"},
			want:       []metav1.Condition{{Type: "foo"}, {Type: "bar"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			conditions, cond := appendCondition(tt.conditions, tt.cond)

			assert.Equal(t, tt.want, conditions)
			assert.Equal(t, &conditions[len(conditions)-1], cond)
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

			assert.Equal(t, want, got)
		})
	}
}

func TestHostnameMatch(t *testing.T) {
	tests := []struct {
		desc     string
		pattern  gatewayv1.Hostname
		hostname gatewayv1.Hostname
		want     bool
	}{
		{
			desc:     "Exact match",
			pattern:  "example.com",
			hostname: "example.com",
			want:     true,
		},
		{
			desc:     "Exact match failure",
			pattern:  "www.example.com",
			hostname: "example.com",
		},
		{
			desc:     "Suffix match",
			pattern:  "*.example.com",
			hostname: "www.example.com",
			want:     true,
		},
		{
			desc:     "Suffix no match",
			pattern:  "*.example.com",
			hostname: "www.example.net",
		},
		{
			desc:     "Must match at least one label",
			pattern:  "*.example.com",
			hostname: ".example.com",
		},
		{
			desc:     "Must match extra label",
			pattern:  "*.example.com",
			hostname: "example.com",
		},
		{
			desc:     "Match with wildcards in both hostnames",
			pattern:  "*.example.com",
			hostname: "*.example.com",
			want:     true,
		},
		{
			desc:     "No match with wildcards in both hostnames",
			pattern:  "*.www.example.com",
			hostname: "*.example.com",
		},
		{
			desc:     "Pattern has no wildcard but hostname has one",
			pattern:  "www.example.com",
			hostname: "*.example.com",
		},
		{
			desc:     "Match multiple labels",
			pattern:  "*.example.com",
			hostname: "www.sample.example.com",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, hostnameMatch(tt.pattern, tt.hostname))
		})
	}
}

func TestPodFindPort(t *testing.T) {
	tests := []struct {
		desc        string
		containers  []corev1.Container
		servicePort corev1.ServicePort
		wantPort    int32
		wantErr     bool
	}{
		{
			desc: "Numeric port",
			servicePort: corev1.ServicePort{
				TargetPort: intstr.FromInt32(8080),
			},
			wantPort: 8080,
		},
		{
			desc: "Numeric port (string)",
			containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8080,
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				TargetPort: intstr.FromString("8080"),
			},
			wantErr: true,
		},
		{
			desc: "Named port and empty containers",
			servicePort: corev1.ServicePort{
				TargetPort: intstr.FromString("https"),
			},
			wantErr: true,
		},
		{
			desc: "Named port and non-empty containers; port not found",
			containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{
							Name: "http",
						},
						{
							Name: "ftp",
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				TargetPort: intstr.FromString("https"),
			},
			wantErr: true,
		},
		{
			desc: "Named port",
			containers: []corev1.Container{
				{},
				{
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 80,
							Protocol:      corev1.ProtocolTCP,
						},
						{
							Name:          "https",
							ContainerPort: 443,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				TargetPort: intstr.FromString("https"),
				Protocol:   corev1.ProtocolTCP,
			},
			wantPort: 443,
		},
		{
			desc: "Protocols do not match",
			containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 80,
							Protocol:      corev1.ProtocolTCP,
						},
						{
							Name:          "https",
							ContainerPort: 443,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
			servicePort: corev1.ServicePort{
				TargetPort: intstr.FromString("https"),
				Protocol:   corev1.ProtocolUDP,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			po := &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: tt.containers,
				},
			}

			port, err := podFindPort(po, &tt.servicePort)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantPort, port)
		})
	}
}

func TestDeletedObjectAs(t *testing.T) {
	t.Run("Direct conversion", func(t *testing.T) {
		s := &corev1.Pod{}

		po, err := deletedObjectAs[*corev1.Pod](s)
		require.NoError(t, err)
		assert.Equal(t, s, po)
	})

	t.Run("Via cache.DeletedFinalStateUnknown", func(t *testing.T) {
		s := &corev1.Pod{}
		d := cache.DeletedFinalStateUnknown{
			Obj: s,
		}

		po, err := deletedObjectAs[*corev1.Pod](d)
		require.NoError(t, err)
		assert.Equal(t, s, po)
	})

	t.Run("Failed conversion", func(t *testing.T) {
		s := &corev1.ConfigMap{}

		po, err := deletedObjectAs[*corev1.Pod](s)
		require.Error(t, err)
		assert.Nil(t, po)
	})

	t.Run("Failed conversion via cache.DeletedFinalStateUnknown", func(t *testing.T) {
		s := &corev1.ConfigMap{}
		d := cache.DeletedFinalStateUnknown{
			Obj: s,
		}

		po, err := deletedObjectAs[*corev1.Pod](d)
		require.Error(t, err)
		assert.Nil(t, po)
	})
}
