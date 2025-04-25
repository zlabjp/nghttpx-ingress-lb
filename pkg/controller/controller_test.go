/*
Copyright 2015 The Kubernetes Authors.

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
	"context"
	"encoding/hex"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/events"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

type fixture struct {
	t *testing.T

	clientset        *fake.Clientset
	gatewayClientset *gatewayfake.Clientset

	lbc *LoadBalancerController
	lc  *LeaderController

	ingStore          []*networkingv1.Ingress
	ingClassStore     []*networkingv1.IngressClass
	svcStore          []*corev1.Service
	secretStore       []*corev1.Secret
	cmStore           []*corev1.ConfigMap
	podStore          []*corev1.Pod
	nodeStore         []*corev1.Node
	epSliceStore      []*discoveryv1.EndpointSlice
	gatewayClassStore []*gatewayv1.GatewayClass
	gatewayStore      []*gatewayv1.Gateway
	httpRouteStore    []*gatewayv1.HTTPRoute

	objects        []runtime.Object
	gatewayObjects []runtime.Object

	actions        []core.Action
	gatewayActions []core.Action

	http3               bool
	shareTLSTicketKey   bool
	publishService      *types.NamespacedName
	requireIngressClass bool
	gatewayAPI          bool
	currentTime         metav1.Time
}

func newFixture(t *testing.T) *fixture {
	return &fixture{
		t:       t,
		objects: []runtime.Object{},
	}
}

const (
	defaultBackendName            = "default-http-backend"
	defaultBackendNamespace       = "kube-system"
	defaultIngNamespace           = metav1.NamespaceAll
	defaultConfigMapName          = "ing-config"
	defaultConfigMapNamespace     = "kube-system"
	defaultIngressClassController = "zlab.co.jp/nghttpx"
	defaultGatewayClassController = "zlab.co.jp/nghttpx"
	defaultConfDir                = "conf"
	defaultTLSTicketKeyPeriod     = time.Hour
	defaultQUICSecretPeriod       = time.Hour

	// openssl ecparam -name prime256v1 -genkey -noout -out tls.key
	tlsKey = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIR3RHN766OwG5SPYdCDHaolfkVS0bpqTHVUj1Tkw++CoAoGCCqGSM49
AwEHoUQDQgAE5qxIb/FFAeAdsOVqAeAlKnXwHwTL+mxRAr2QZ63A7SdYgqOB+pz3
Qu6PQqBCMaMh3xbmq1M9OwKwW/NwU0GW7w==
-----END EC PRIVATE KEY-----
`
	// openssl req -new -key tls.key -x509 -nodes -days 3650 -out tls.crt
	tlsCrt = `-----BEGIN CERTIFICATE-----
MIICCDCCAa2gAwIBAgIUDRVDeW3iI7AYMWugEE0LU9mxM54wCgYIKoZIzj0EAwIw
WTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGElu
dGVybmV0IFdpZGdpdHMgUHR5IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTE5
MTAwOTAxNTA1MFoXDTI5MTAwNjAxNTA1MFowWTELMAkGA1UEBhMCQVUxEzARBgNV
BAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0
ZDESMBAGA1UEAwwJbG9jYWxob3N0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE
5qxIb/FFAeAdsOVqAeAlKnXwHwTL+mxRAr2QZ63A7SdYgqOB+pz3Qu6PQqBCMaMh
3xbmq1M9OwKwW/NwU0GW76NTMFEwHQYDVR0OBBYEFCXLwcdXXbjMX4BipAWB3B/k
8iUvMB8GA1UdIwQYMBaAFCXLwcdXXbjMX4BipAWB3B/k8iUvMA8GA1UdEwEB/wQF
MAMBAf8wCgYIKoZIzj0EAwIDSQAwRgIhAMxuNNoDOpZ8XjA/VaFg1kSaqnyKRLVZ
N7YC0GGs9cugAiEAyE3qpDKBvSoRSAaOwQvba22Wo3qI/mhioHdt7Xm4jkI=
-----END CERTIFICATE-----
`
)

var (
	defaultRuntimeInfo = types.NamespacedName{
		Name:      "nghttpx-ingress-controller",
		Namespace: "kube-system",
	}

	defaultIngPodLables = map[string]string{
		"k8s-app": "ingress",
	}

	defaultNghttpxSecret = types.NamespacedName{
		Name:      "nghttpx-km",
		Namespace: "kube-system",
	}
)

// prepare performs setup necessary for test run.
func (f *fixture) prepare() {
	f.preparePod(newIngPod(defaultRuntimeInfo.Name, "zulu.test"))
}

func (f *fixture) preparePod(pod *corev1.Pod) {
	f.clientset = fake.NewSimpleClientset(f.objects...)

	f.gatewayClientset = gatewayfake.NewSimpleClientset()

	// Needs special handling for Gateway due to https://github.com/kubernetes/client-go/issues/1082
	gatewayv1GVR := gatewayv1.SchemeGroupVersion.WithResource("gateways")

	for _, o := range f.gatewayObjects {
		if gtw, ok := o.(*gatewayv1.Gateway); ok {
			if err := f.gatewayClientset.Tracker().Create(gatewayv1GVR, gtw, gtw.Namespace); err != nil {
				panic(err)
			}
		} else {
			if err := f.gatewayClientset.Tracker().Add(o); err != nil {
				panic(err)
			}
		}
	}

	config := Config{
		DefaultBackendService:  &types.NamespacedName{Namespace: defaultBackendNamespace, Name: defaultBackendName},
		WatchNamespace:         defaultIngNamespace,
		NghttpxConfigMap:       &types.NamespacedName{Namespace: defaultConfigMapNamespace, Name: defaultConfigMapName},
		NghttpxConfDir:         defaultConfDir,
		NghttpxSecret:          defaultNghttpxSecret,
		IngressClassController: defaultIngressClassController,
		GatewayClassController: defaultGatewayClassController,
		ReloadRate:             1.0,
		ReloadBurst:            1,
		HTTP3:                  f.http3,
		ShareTLSTicketKey:      f.shareTLSTicketKey,
		PublishService:         f.publishService,
		RequireIngressClass:    f.requireIngressClass,
		TLSTicketKeyPeriod:     defaultTLSTicketKeyPeriod,
		QUICSecretPeriod:       defaultQUICSecretPeriod,
		GatewayAPI:             f.gatewayAPI,
		Pod:                    pod,
		EventRecorder:          &events.FakeRecorder{},
	}

	lbc, err := NewLoadBalancerController(context.Background(), f.clientset, f.gatewayClientset, newFakeLoadBalancer(), config)
	require.NoError(f.t, err)

	lc, err := NewLeaderController(context.Background(), lbc)
	require.NoError(f.t, err)

	if !f.currentTime.IsZero() {
		lc.timeNow = func() metav1.Time {
			return f.currentTime
		}
	}

	f.lbc = lbc
	f.lc = lc
}

func (f *fixture) run() {
	f.setupStore()

	require.NoError(f.t, f.lbc.sync(context.Background(), syncKey))

	f.verifyActions()
}

func (f *fixture) runShouldFail() {
	f.setupStore()

	require.Error(f.t, f.lbc.sync(context.Background(), syncKey))

	f.verifyActions()
}

func (f *fixture) setupStore() {
	for _, ing := range f.ingStore {
		require.NoError(f.t, f.lbc.ingIndexer.Add(ing))
		require.NoError(f.t, f.lc.ingIndexer.Add(ing))
	}

	for _, ingClass := range f.ingClassStore {
		require.NoError(f.t, f.lbc.ingClassIndexer.Add(ingClass))
		require.NoError(f.t, f.lc.ingClassIndexer.Add(ingClass))
	}

	for _, es := range f.epSliceStore {
		require.NoError(f.t, f.lbc.epSliceIndexer.Add(es))
	}

	for _, svc := range f.svcStore {
		require.NoError(f.t, f.lbc.svcIndexer.Add(svc))

		if f.lc.svcIndexer != nil {
			require.NoError(f.t, f.lc.svcIndexer.Add(svc))
		}
	}

	for _, secret := range f.secretStore {
		require.NoError(f.t, f.lbc.secretIndexer.Add(secret))

		if f.lc.secretIndexer != nil {
			require.NoError(f.t, f.lc.secretIndexer.Add(secret))
		}
	}

	if f.lbc.cmIndexer != nil {
		for _, cm := range f.cmStore {
			require.NoError(f.t, f.lbc.cmIndexer.Add(cm))
		}
	}

	for _, pod := range f.podStore {
		require.NoError(f.t, f.lbc.podIndexer.Add(pod))
		require.NoError(f.t, f.lc.podIndexer.Add(pod))
	}

	for _, node := range f.nodeStore {
		require.NoError(f.t, f.lc.nodeIndexer.Add(node))
	}

	for _, gc := range f.gatewayClassStore {
		require.NoError(f.t, f.lbc.gatewayClassIndexer.Add(gc))
		require.NoError(f.t, f.lc.gatewayClassIndexer.Add(gc))
	}

	for _, gtw := range f.gatewayStore {
		require.NoError(f.t, f.lbc.gatewayIndexer.Add(gtw))
		require.NoError(f.t, f.lc.gatewayIndexer.Add(gtw))
	}

	for _, httpRoute := range f.httpRouteStore {
		require.NoError(f.t, f.lbc.httpRouteIndexer.Add(httpRoute))
		require.NoError(f.t, f.lc.httpRouteIndexer.Add(httpRoute))
	}
}

func (f *fixture) verifyActions() {
	actions := f.clientset.Actions()
	for i, action := range actions {
		require.GreaterOrEqualf(f.t, len(f.actions), i+1, "unexpected action: %+v", actions[i:])

		expectedAction := f.actions[i]
		assert.True(f.t, expectedAction.Matches(action.GetVerb(), action.GetResource().Resource),
			"Expected\n\t%+v\ngot\n\t%+v", expectedAction, action)
	}

	assert.Len(f.t, actions, len(f.actions), "additional expected actions: %+v", f.actions[len(actions):])

	actions = f.gatewayClientset.Actions()
	for i, action := range actions {
		require.GreaterOrEqualf(f.t, len(f.gatewayActions), i+1, "unexpected action: %+v", actions[i:])

		expectedAction := f.gatewayActions[i]
		assert.True(f.t, expectedAction.Matches(action.GetVerb(), action.GetResource().Resource),
			"Expected\n\t%+v\ngot\n\t%+v", expectedAction, action)
	}

	assert.Len(f.t, actions, len(f.gatewayActions), "additional expected actions: %+v", f.gatewayActions[len(actions):])
}

// expectUpdateIngAction adds an expectation that update for ing should occur.
func (f *fixture) expectUpdateIngAction(ing *networkingv1.Ingress) {
	f.actions = append(f.actions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "ingresses"}, ing.Namespace, ing))
}

func (f *fixture) expectUpdateStatusGatewayClassAction(gc *gatewayv1.GatewayClass) {
	gvr := gatewayv1.SchemeGroupVersion.WithResource("gatewayclasses")
	f.gatewayActions = append(f.gatewayActions, core.NewUpdateSubresourceAction(gvr, "status", "", gc))
}

func (f *fixture) expectUpdateStatusGatewayAction(gtw *gatewayv1.Gateway) {
	gvr := gatewayv1.SchemeGroupVersion.WithResource("gateways")
	f.gatewayActions = append(f.gatewayActions, core.NewUpdateSubresourceAction(gvr, "status", "", gtw))
}

func (f *fixture) expectUpdateStatusHTTPRouteAction(httpRoute *gatewayv1.HTTPRoute) {
	gvr := gatewayv1.SchemeGroupVersion.WithResource("httproutes")
	f.gatewayActions = append(f.gatewayActions, core.NewUpdateSubresourceAction(gvr, "status", "", httpRoute))
}

// fakeLoadBalancer implements serverReloader.
type fakeLoadBalancer struct {
	checkAndReloadHandler func(ingConfig *nghttpx.IngressConfig) (bool, error)

	ingConfig *nghttpx.IngressConfig
}

var _ serverReloader = &fakeLoadBalancer{}

// newFakeLoadBalancer creates new fakeLoadBalancer.
func newFakeLoadBalancer() *fakeLoadBalancer {
	flb := &fakeLoadBalancer{}
	flb.checkAndReloadHandler = flb.defaultCheckAndReload

	return flb
}

func (flb *fakeLoadBalancer) Start(context.Context, string, string) error {
	return nil
}

func (flb *fakeLoadBalancer) CheckAndReload(_ context.Context, ingConfig *nghttpx.IngressConfig) (bool, error) {
	return flb.checkAndReloadHandler(ingConfig)
}

func (flb *fakeLoadBalancer) defaultCheckAndReload(ingConfig *nghttpx.IngressConfig) (bool, error) {
	flb.ingConfig = ingConfig
	return true, nil
}

func toPtr[T any](v T) *T {
	return &v
}

// newEmptyConfigMap returns empty ConfigMap.
func newEmptyConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultConfigMapName,
			Namespace: defaultConfigMapNamespace,
		},
		Data: make(map[string]string),
	}
}

// newDefaultBackend returns Service and EndpointSlices for default backend.
func newDefaultBackend() (*corev1.Service, []*discoveryv1.EndpointSlice) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name": defaultBackendName,
			},
			Ports: []corev1.ServicePort{
				{
					Port:       8181,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	ess := []*discoveryv1.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBackendName + "-a",
				Namespace: defaultBackendNamespace,
				Labels: map[string]string{
					discoveryv1.LabelServiceName: defaultBackendName,
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{
				{
					Port: toPtr(int32(8081)),
				},
				{
					Port: toPtr(int32(8080)),
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{
						"192.168.100.1",
					},
					TargetRef: &corev1.ObjectReference{
						Kind:      "Pod",
						Name:      defaultBackendName + "-pod-1",
						Namespace: defaultBackendNamespace,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBackendName + "-b",
				Namespace: defaultBackendNamespace,
				Labels: map[string]string{
					discoveryv1.LabelServiceName: defaultBackendName,
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{
				{
					Port: toPtr(int32(8081)),
				},
				{
					Port: toPtr(int32(8080)),
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{
						"192.168.100.2",
					},
					TargetRef: &corev1.ObjectReference{
						Kind:      "Pod",
						Name:      defaultBackendName + "-pod-2",
						Namespace: defaultBackendNamespace,
					},
				},
			},
		},
		// This must be ignored.
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBackendName + "-c",
				Namespace: defaultBackendNamespace,
			},
			AddressType: "FQDN",
			Ports: []discoveryv1.EndpointPort{
				{
					Port: toPtr(int32(8081)),
				},
				{
					Port: toPtr(int32(8080)),
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{
						"192.168.100.3",
					},
				},
				{
					Addresses: []string{
						"192.168.100.4",
					},
					TargetRef: &corev1.ObjectReference{
						Kind:      "Foo",
						Name:      "something",
						Namespace: defaultBackendNamespace,
					},
				},
			},
		},
	}

	return svc, ess
}

// newDefaultBackendWithoutSelectors returns Service and EndpointSlices for default backend without Service selectors.
func newDefaultBackendWithoutSelectors() (*corev1.Service, []*discoveryv1.EndpointSlice) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       8181,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	ess := []*discoveryv1.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBackendName + "-a",
				Namespace: defaultBackendNamespace,
				Labels: map[string]string{
					discoveryv1.LabelServiceName: defaultBackendName,
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{
				{
					Port: toPtr(int32(8081)),
				},
				{
					Port: toPtr(int32(8080)),
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{
						"192.168.100.1",
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBackendName + "-b",
				Namespace: defaultBackendNamespace,
				Labels: map[string]string{
					discoveryv1.LabelServiceName: defaultBackendName,
				},
			},
			AddressType: discoveryv1.AddressTypeIPv4,
			Ports: []discoveryv1.EndpointPort{
				{
					Port: toPtr(int32(8081)),
				},
				{
					Port: toPtr(int32(8080)),
				},
			},
			Endpoints: []discoveryv1.Endpoint{
				{
					Addresses: []string{
						"192.168.100.2",
					},
				},
			},
		},
	}

	return svc, ess
}

func newBackend(name string, addrs []string) (*corev1.Service, *discoveryv1.EndpointSlice) {
	const namespace = metav1.NamespaceDefault

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       8281,
					TargetPort: intstr.FromInt(80),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"k8s-app": "test",
			},
		},
	}

	proto := corev1.ProtocolTCP

	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-aaaa",
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: name,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Protocol: &proto,
				Port:     toPtr(int32(81)),
			},
			{
				Protocol: &proto,
				Port:     toPtr(int32(80)),
			},
		},
	}

	for i, addr := range addrs {
		es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{
			Addresses: []string{addr},
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      name + "-pod-" + strconv.Itoa(i+1),
				Namespace: namespace,
			},
		})
	}

	return svc, es
}

func newBackendWithoutSelectors(name string, addrs []string) (*corev1.Service, *discoveryv1.EndpointSlice) {
	const namespace = metav1.NamespaceDefault

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:       8281,
					TargetPort: intstr.FromInt(80),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	proto := corev1.ProtocolTCP

	es := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-aaaa",
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: name,
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports: []discoveryv1.EndpointPort{
			{
				Protocol: &proto,
				Port:     toPtr(int32(81)),
			},
			{
				Protocol: &proto,
				Port:     toPtr(int32(80)),
			},
		},
	}

	for _, addr := range addrs {
		es.Endpoints = append(es.Endpoints, discoveryv1.Endpoint{
			Addresses: []string{addr},
		})
	}

	return svc, es
}

type ingressBuilder struct {
	*networkingv1.Ingress
}

func newIngressBuilder(namespace, name string) *ingressBuilder {
	return &ingressBuilder{
		Ingress: &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

func (b *ingressBuilder) Complete() *networkingv1.Ingress {
	return b.Ingress
}

func (b *ingressBuilder) WithAnnotations(annotations map[string]string) *ingressBuilder {
	b.Annotations = annotations

	return b
}

func (b *ingressBuilder) WithDefaultRule(svc string, port networkingv1.ServiceBackendPort) *ingressBuilder {
	rule := networkingv1.IngressRule{
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{
					{
						Path: "/",
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: svc,
								Port: port,
							},
						},
					},
				},
			},
		},
	}

	b.Spec.Rules = append(b.Spec.Rules, rule)

	return b
}

func (b *ingressBuilder) WithRule(path, svc string, port networkingv1.ServiceBackendPort) *ingressBuilder {
	return b.WithRuleHost(b.Name+"."+b.Namespace+".test", path, svc, port)
}

func (b *ingressBuilder) WithRuleHost(host, path, svc string, port networkingv1.ServiceBackendPort) *ingressBuilder {
	rule := networkingv1.IngressRule{
		Host: host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{
					{
						Path: path,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: svc,
								Port: port,
							},
						},
					},
				},
			},
		},
	}

	b.Spec.Rules = append(b.Spec.Rules, rule)

	return b
}

func (b *ingressBuilder) WithDefaultBackend(svc string, port networkingv1.ServiceBackendPort) *ingressBuilder {
	b.Spec.DefaultBackend = &networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: svc,
			Port: port,
		},
	}

	return b
}

func (b *ingressBuilder) WithTLS(tlsSecret string) *ingressBuilder {
	b.Spec.TLS = append(b.Spec.TLS, networkingv1.IngressTLS{
		SecretName: tlsSecret,
	})

	return b
}

func (b *ingressBuilder) WithIngressClass(ingressClass string) *ingressBuilder {
	b.Spec.IngressClassName = toPtr(ingressClass)

	return b
}

func (b *ingressBuilder) WithLoadBalancerIngress(lbIngs []networkingv1.IngressLoadBalancerIngress) *ingressBuilder {
	b.Status.LoadBalancer.Ingress = lbIngs

	return b
}

func serviceBackendPortNumber(port int32) networkingv1.ServiceBackendPort {
	return networkingv1.ServiceBackendPort{
		Number: port,
	}
}

func serviceBackendPortName(name string) networkingv1.ServiceBackendPort {
	return networkingv1.ServiceBackendPort{
		Name: name,
	}
}

func newTLSSecret(namespace, name string, tlsCrt, tlsKey []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       tlsCrt,
			corev1.TLSPrivateKeyKey: tlsKey,
		},
	}
}

func newNghttpxSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultNghttpxSecret.Name,
			Namespace: defaultNghttpxSecret.Namespace,
		},
	}
}

func newChecksumFile(path string) *nghttpx.ChecksumFile {
	return &nghttpx.ChecksumFile{
		Path: path,
	}
}

// TestSyncDefaultBackend verifies that controller creates configuration for default service backend.
func TestSyncDefaultBackend(t *testing.T) {
	tests := []struct {
		desc             string
		withoutSelectors bool
	}{
		{
			desc: "With selectors",
		},
		{
			desc:             "Without selectors",
			withoutSelectors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			cm := newEmptyConfigMap()
			cm.Data[nghttpx.NghttpxExtraConfigKey] = "Test"

			const mrubyContent = "mruby"

			cm.Data[nghttpx.NghttpxMrubyFileContentKey] = mrubyContent

			var (
				svc *corev1.Service
				ess []*discoveryv1.EndpointSlice
			)

			if tt.withoutSelectors {
				svc, ess = newDefaultBackendWithoutSelectors()
			} else {
				svc, ess = newDefaultBackend()
			}

			nghttpxSecret := newNghttpxSecret()

			f.cmStore = append(f.cmStore, cm)
			f.svcStore = append(f.svcStore, svc)
			f.secretStore = append(f.secretStore, nghttpxSecret)
			f.epSliceStore = append(f.epSliceStore, ess...)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			assert.False(t, ingConfig.TLS)
			require.Len(t, ingConfig.Upstreams, 1)

			upstream := ingConfig.Upstreams[0]
			assert.Empty(t, upstream.Path)

			backends := upstream.Backends
			require.Len(t, backends, 2)

			us := backends[0]
			assert.Equal(t, "192.168.100.1", us.Address)
			assert.Equal(t, "8080", us.Port)
			assert.Equal(t, cm.Data[nghttpx.NghttpxExtraConfigKey], flb.ingConfig.ExtraConfig)
			assert.Equal(t, &nghttpx.ChecksumFile{
				Path:     nghttpx.MrubyRbPath(defaultConfDir),
				Content:  []byte(mrubyContent),
				Checksum: nghttpx.Checksum([]byte(mrubyContent)),
			}, flb.ingConfig.MrubyFile)
			assert.Empty(t, flb.ingConfig.TLSTicketKeyFiles)
		})
	}
}

// TestSyncDefaultTLSSecretNotFound verifies that sync must fail if default TLS Secret is not found.
func TestSyncDefaultTLSSecretNotFound(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	f.svcStore = append(f.svcStore, svc)
	f.epSliceStore = append(f.epSliceStore, ess...)

	f.prepare()
	f.lbc.defaultTLSSecret = &types.NamespacedName{
		Namespace: "kube-system",
		Name:      "default-tls",
	}
	f.runShouldFail()
}

// TestSyncDefaultSecret verifies that default TLS secret is loaded.
func TestSyncDefaultSecret(t *testing.T) {
	f := newFixture(t)

	dCrt := []byte(tlsCrt)
	dKey := []byte(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	nghttpxSecret := newNghttpxSecret()
	svc, ess := newDefaultBackend()

	f.secretStore = append(f.secretStore, tlsSecret, nghttpxSecret)
	f.svcStore = append(f.svcStore, svc)
	f.epSliceStore = append(f.epSliceStore, ess...)

	f.prepare()
	f.lbc.defaultTLSSecret = &types.NamespacedName{
		Namespace: tlsSecret.Namespace,
		Name:      tlsSecret.Name,
	}
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	assert.True(t, ingConfig.TLS)

	dKeyChecksum := nghttpx.Checksum(dKey)
	dCrtChecksum := nghttpx.Checksum(dCrt)

	assert.Equal(t, nghttpx.CreateTLSKeyPath(defaultConfDir, hex.EncodeToString(dKeyChecksum)), ingConfig.DefaultTLSCred.Key.Path)
	assert.Equal(t, nghttpx.CreateTLSCertPath(defaultConfDir, hex.EncodeToString(dCrtChecksum)), ingConfig.DefaultTLSCred.Cert.Path)
	assert.Equal(t, dKeyChecksum, ingConfig.DefaultTLSCred.Key.Checksum)
	assert.Equal(t, dCrtChecksum, ingConfig.DefaultTLSCred.Cert.Checksum)
	assert.True(t, ingConfig.Upstreams[0].RedirectIfNotTLS)
}

// TestSyncDupDefaultSecret verifies that duplicated default TLS secret is removed.
func TestSyncDupDefaultSecret(t *testing.T) {
	f := newFixture(t)

	dCrt := []byte(tlsCrt)
	dKey := []byte(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	nghttpxSecret := newNghttpxSecret()
	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithTLS(tlsSecret.Name).
		Complete()

	f.secretStore = append(f.secretStore, tlsSecret, nghttpxSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1)

	f.prepare()
	f.lbc.defaultTLSSecret = &types.NamespacedName{
		Namespace: tlsSecret.Namespace,
		Name:      tlsSecret.Name,
	}
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	assert.True(t, ingConfig.TLS)
	assert.Equal(t, nghttpx.CreateTLSKeyPath(defaultConfDir, hex.EncodeToString(nghttpx.Checksum(dKey))), ingConfig.DefaultTLSCred.Key.Path)
	assert.Empty(t, ingConfig.SubTLSCred)

	for i := range ingConfig.Upstreams {
		assert.True(t, ingConfig.Upstreams[i].RedirectIfNotTLS)
	}
}

// TestSyncNormalizePEM verifies that badly formatted PEM in TLS secret is normalized.
func TestSyncNormalizePEM(t *testing.T) {
	const (
		badlyFormattedTLSKey = `-----BEGIN EC PRIVATE KEY-----

MHcCAQEEIIR3RHN766OwG5SPYdCDHaolfkVS0bpqTHVUj1Tkw++CoAoGCCqGSM49
  AwEHoUQDQgAE5qxIb/FFAeAdsOVqAeAlKnXwHwTL+mxRAr2QZ63A7SdYgqOB+pz3
Qu6PQqBCMaMh3xbmq1M9OwKwW/NwU0GW7w==

-----END EC PRIVATE KEY-----
`
		badlyFormattedTLSCrt = `
-----BEGIN CERTIFICATE-----
  MIICCDCCAa2gAwIBAgIUDRVDeW3iI7AYMWugEE0LU9mxM54wCgYIKoZIzj0EAwIw
  WTELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGElu
  dGVybmV0IFdpZGdpdHMgUHR5IEx0ZDESMBAGA1UEAwwJbG9jYWxob3N0MB4XDTE5
  MTAwOTAxNTA1MFoXDTI5MTAwNjAxNTA1MFowWTELMAkGA1UEBhMCQVUxEzARBgNV
  BAgMClNvbWUtU3RhdGUxITAfBgNVBAoMGEludGVybmV0IFdpZGdpdHMgUHR5IEx0
  ZDESMBAGA1UEAwwJbG9jYWxob3N0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE
  5qxIb/FFAeAdsOVqAeAlKnXwHwTL+mxRAr2QZ63A7SdYgqOB+pz3Qu6PQqBCMaMh
  3xbmq1M9OwKwW/NwU0GW76NTMFEwHQYDVR0OBBYEFCXLwcdXXbjMX4BipAWB3B/k
  8iUvMB8GA1UdIwQYMBaAFCXLwcdXXbjMX4BipAWB3B/k8iUvMA8GA1UdEwEB/wQF
  MAMBAf8wCgYIKoZIzj0EAwIDSQAwRgIhAMxuNNoDOpZ8XjA/VaFg1kSaqnyKRLVZ
  N7YC0GGs9cugAiEAyE3qpDKBvSoRSAaOwQvba22Wo3qI/mhioHdt7Xm4jkI=
-----END CERTIFICATE-----
`
	)

	f := newFixture(t)

	dCrt := []byte(badlyFormattedTLSCrt)
	dKey := []byte(badlyFormattedTLSKey)
	tlsSecret := newTLSSecret(metav1.NamespaceDefault, "tls", dCrt, dKey)
	nghttpxSecret := newNghttpxSecret()
	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithTLS(tlsSecret.Name).
		Complete()

	f.secretStore = append(f.secretStore, tlsSecret, nghttpxSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	require.NotNil(t, ingConfig.DefaultTLSCred)

	tlsCred := ingConfig.DefaultTLSCred
	assert.Equal(t, tlsCrt, string(tlsCred.Cert.Content))
	assert.Equal(t, tlsKey, string(tlsCred.Key.Content))
	assert.Empty(t, ingConfig.SubTLSCred)
	require.Len(t, ingConfig.Upstreams, 2)

	upstream := ingConfig.Upstreams[1]
	assert.Equal(t, namespacedName(ing1), upstream.Source)
	assert.True(t, upstream.RedirectIfNotTLS)
}

// TestSyncStringNamedPort verifies that if service target port is a named port, it is looked up from Pod spec.
func TestSyncStringNamedPort(t *testing.T) {
	tests := []struct {
		desc string
	}{
		{
			desc: "With EndpointSlice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			svc, ess := newDefaultBackend()

			bs1, bes1 := newBackend("alpha", []string{"192.168.10.1", "192.168.10.2"})
			bs1.Spec.Ports[0] = corev1.ServicePort{
				Port:       1234,
				TargetPort: intstr.FromString("my-port"),
				Protocol:   corev1.ProtocolTCP,
			}
			ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
				WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				Complete()

			bp1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alpha-pod-1",
					Namespace: bs1.Namespace,
					Labels:    bs1.Spec.Selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{
									Name:          "my-port",
									ContainerPort: 80,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			}

			bp2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alpha-pod-2",
					Namespace: bs1.Namespace,
					Labels:    bs1.Spec.Selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{
									Name:          "my-port",
									ContainerPort: 81,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			}

			nghttpxSecret := newNghttpxSecret()

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.ingStore = append(f.ingStore, ing1)
			f.podStore = append(f.podStore, bp1, bp2)
			f.secretStore = append(f.secretStore, nghttpxSecret)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			require.Len(t, ingConfig.Upstreams, 2)
			assert.Equal(t, namespacedName(ing1), ingConfig.Upstreams[1].Source)
			require.Len(t, ingConfig.Upstreams[1].Backends, 2)

			for i, port := range []string{"80", "81"} {
				backend := ingConfig.Upstreams[1].Backends[i]
				assert.Equal(t, port, backend.Port)
			}
		})
	}
}

// TestSyncEmptyTargetPort verifies that if target port is empty, port is used instead.  In practice, target port is always filled out.
func TestSyncEmptyTargetPort(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = corev1.ServicePort{
		Port:       80,
		TargetPort: intstr.FromString(""),
		Protocol:   corev1.ProtocolTCP,
	}
	ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		Complete()
	nghttpxSecret := newNghttpxSecret()

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1)
	f.ingStore = append(f.ingStore, ing1)
	f.secretStore = append(f.secretStore, nghttpxSecret)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	require.Len(t, ingConfig.Upstreams, 2)
	assert.Equal(t, namespacedName(ing1), ingConfig.Upstreams[1].Source)

	backend := ingConfig.Upstreams[1].Backends[0]
	assert.Equal(t, "80", backend.Port)
}

// TestSyncWithoutSelectors verifies that the controller deals with Service without selectors.
func TestSyncWithoutSelectors(t *testing.T) {
	tests := []struct {
		desc string
	}{
		{
			desc: "With EndpointSlice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			svc, ess := newDefaultBackend()

			bs1, bes1 := newBackendWithoutSelectors("alpha", []string{"192.168.10.1"})
			ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
				WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				Complete()
			nghttpxSecret := newNghttpxSecret()

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.ingStore = append(f.ingStore, ing1)
			f.secretStore = append(f.secretStore, nghttpxSecret)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			require.Len(t, ingConfig.Upstreams, 2)
			assert.Equal(t, namespacedName(ing1), ingConfig.Upstreams[1].Source)

			backend := ingConfig.Upstreams[1].Backends[0]
			assert.Equal(t, "80", backend.Port)
		})
	}
}

// TestValidateIngressClass verifies validateIngressClass.
func TestValidateIngressClass(t *testing.T) {
	tests := []struct {
		desc            string
		ing             *networkingv1.Ingress
		ingClass        *networkingv1.IngressClass
		requireIngClass bool
		want            bool
	}{
		{
			desc: "no IngressClass",
			ing:  newIngressBuilder("default", "foo").Complete(),
			want: true,
		},
		{
			desc: "IngressClass targets this controller",
			ing:  newIngressBuilder("default", "foo").WithIngressClass("bar").Complete(),
			ingClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: defaultIngressClassController,
				},
			},
			want: true,
		},
		{
			desc: "IngressClass does not target this controller",
			ing:  newIngressBuilder("default", "foo").WithIngressClass("bar").Complete(),
			ingClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "example.com/ingress",
				},
			},
		},
		{
			desc: "The specified IngressClass is not found",
			ing:  newIngressBuilder("default", "foo").WithIngressClass("bar").Complete(),
		},
		{
			desc: "IngressClass which targets this controller is marked default",
			ing:  newIngressBuilder("default", "foo").Complete(),
			ingClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
					Annotations: map[string]string{
						networkingv1.AnnotationIsDefaultIngressClass: "true",
					},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: defaultIngressClassController,
				},
			},
			want: true,
		},
		{
			desc: "IngressClass which does not target this controller is marked default",
			ing:  newIngressBuilder("default", "foo").Complete(),
			ingClass: &networkingv1.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
					Annotations: map[string]string{
						networkingv1.AnnotationIsDefaultIngressClass: "true",
					},
				},
				Spec: networkingv1.IngressClassSpec{
					Controller: "example.com/ingress",
				},
			},
		},
		{
			desc:            "Ingress without IngressClass must be ignored",
			ing:             newIngressBuilder("default", "foo").Complete(),
			requireIngClass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.requireIngressClass = tt.requireIngClass

			f.ingStore = append(f.ingStore, tt.ing)
			if tt.ingClass != nil {
				f.ingClassStore = append(f.ingClassStore, tt.ingClass)
			}

			f.prepare()
			f.setupStore()

			assert.Equal(t, tt.want, f.lbc.validateIngressClass(t.Context(), tt.ing))
		})
	}
}

// TestSyncIngressDefaultBackend verfies that Ingress.Spec.DefaultBackend is considered.
func TestSyncIngressDefaultBackend(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
	bs2, bes2 := newBackend("bravo", []string{"192.168.10.2"})
	ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithDefaultBackend("bravo", serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
		Complete()
	nghttpxSecret := newNghttpxSecret()

	f.svcStore = append(f.svcStore, svc, bs1, bs2)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1, bes2)
	f.ingStore = append(f.ingStore, ing1)
	f.secretStore = append(f.secretStore, nghttpxSecret)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	assert.Len(t, ingConfig.Upstreams, 2)
	assert.True(t, slices.ContainsFunc(ingConfig.Upstreams, func(u *nghttpx.Upstream) bool {
		return u.Name == "networking.k8s.io/v1/Ingress:default/bravo,8281;/"
	}))
}

// TestSyncIngressNoDefaultBackendOverride verifies that any settings or rules which override default backend are ignored.
func TestSyncIngressNoDefaultBackendOverride(t *testing.T) {
	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
	bs2, bes2 := newBackend("bravo", []string{"192.168.10.2"})

	tests := []struct {
		desc     string
		ing      *networkingv1.Ingress
		wantName string
	}{
		{
			desc: ".Spec.DefaultBackend must be ignored",
			ing: newIngressBuilder(bs1.Namespace, "alpha-ing").
				WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				WithDefaultBackend(bs2.Name, serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
				Complete(),
		},
		{
			desc: "Any rules which override default backend must be ignored",
			ing: newIngressBuilder(bs1.Namespace, "alpha-ing").
				WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				WithDefaultRule(bs2.Name, serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
				Complete(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			svc, ess := newDefaultBackend()
			nghttpxSecret := newNghttpxSecret()

			f.svcStore = append(f.svcStore, svc, bs1.DeepCopyObject().(*corev1.Service), bs2.DeepCopyObject().(*corev1.Service))
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1.DeepCopyObject().(*discoveryv1.EndpointSlice), bes2.DeepCopyObject().(*discoveryv1.EndpointSlice))
			f.ingStore = append(f.ingStore, tt.ing)
			f.secretStore = append(f.secretStore, nghttpxSecret)

			f.prepare()
			f.lbc.noDefaultBackendOverride = true
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			require.Len(t, ingConfig.Upstreams, 2)
			assert.Empty(t, ingConfig.Upstreams[0].Source)
			assert.Equal(t, f.lbc.defaultSvc.String(), ingConfig.Upstreams[0].Name)
		})
	}
}

// newIngPod creates Ingress controller pod.
func newIngPod(name, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultRuntimeInfo.Namespace,
			Labels:    defaultIngPodLables,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{
							Name:          "my-port",
							ContainerPort: 80,
							Protocol:      corev1.ProtocolTCP,
						},
					},
				},
			},
		},
	}
}

// newNode creates new Node.
func newNode(name string, addrs ...corev1.NodeAddress) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Addresses: addrs,
		},
	}
}

// TestGetLoadBalancerIngressSelector verifies that it collects node IPs from cache.
func TestGetLoadBalancerIngressSelector(t *testing.T) {
	tests := []struct {
		desc        string
		hostNetwork bool
	}{
		{
			desc:        "Use host network",
			hostNetwork: true,
		},
		{
			desc: "Do not use host network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			po1 := newIngPod(defaultRuntimeInfo.Name, "alpha.test")
			po1.Spec.HostNetwork = tt.hostNetwork

			var node1 *corev1.Node

			if tt.hostNetwork {
				node1 = newNode("alpha.test", corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "192.168.0.1"})
			} else {
				po1.Status.PodIP = "192.168.0.1"
			}

			po2 := newIngPod("bravo", "bravo.test")
			po2.Spec.HostNetwork = true
			node2 := newNode("bravo.test",
				corev1.NodeAddress{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
				corev1.NodeAddress{Type: corev1.NodeExternalIP, Address: "192.168.0.2"})

			f.podStore = append(f.podStore, po1, po2)
			f.nodeStore = append(f.nodeStore, node2)

			if tt.hostNetwork {
				f.nodeStore = append(f.nodeStore, node1)
			}

			f.preparePod(po1)
			f.setupStore()

			lbIngs, err := f.lc.getLoadBalancerIngressSelector(t.Context(), labels.ValidatedSetSelector(defaultIngPodLables))

			require.NoError(t, err)

			f.verifyActions()

			assert.Len(t, lbIngs, 2)

			sortLoadBalancerIngress(lbIngs)

			ans := []networkingv1.IngressLoadBalancerIngress{
				{IP: "192.168.0.1"}, {IP: "192.168.0.2"},
			}

			assert.Equal(t, ans, lbIngs)
		})
	}
}

// TestSyncIngress verifies that Ingress resources are updated with the given lbIngs.
func TestSyncIngress(t *testing.T) {
	ingPo1 := newIngPod(defaultRuntimeInfo.Name, "alpha.test")
	ingPo1.Status.PodIP = "192.168.0.1"
	ingPo2 := newIngPod("bravo", "bravo.test")
	ingPo2.Status.PodIP = "192.168.0.2"

	tests := []struct {
		desc                      string
		ingress                   *networkingv1.Ingress
		wantLoadBalancerIngresses []networkingv1.IngressLoadBalancerIngress
	}{
		{
			desc: "Update Ingress status",
			ingress: newIngressBuilder(metav1.NamespaceDefault, "delta-ing").
				WithRule("/", "delta", serviceBackendPortNumber(80)).
				Complete(),
			wantLoadBalancerIngresses: []networkingv1.IngressLoadBalancerIngress{{IP: "192.168.0.1"}, {IP: "192.168.0.2"}},
		},
		{
			desc: "Not Ingress controlled by the controller",
			ingress: newIngressBuilder(metav1.NamespaceDefault, "foxtrot-ing").
				WithRule("/", "foxtrot", serviceBackendPortNumber(80)).
				WithIngressClass("not-nghttpx").
				WithLoadBalancerIngress([]networkingv1.IngressLoadBalancerIngress{{IP: "192.168.0.100"}, {IP: "192.168.0.101"}}).
				Complete(),
			wantLoadBalancerIngresses: []networkingv1.IngressLoadBalancerIngress{{IP: "192.168.0.100"}, {IP: "192.168.0.101"}},
		},
		{
			desc: "Ingress already has correct load balancer addresses",
			ingress: newIngressBuilder(metav1.NamespaceDefault, "golf-ing").
				WithRule("/", "golf", serviceBackendPortNumber(80)).
				WithLoadBalancerIngress([]networkingv1.IngressLoadBalancerIngress{{IP: "192.168.0.1"}, {IP: "192.168.0.2"}}).
				Complete(),
			wantLoadBalancerIngresses: []networkingv1.IngressLoadBalancerIngress{{IP: "192.168.0.1"}, {IP: "192.168.0.2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			f.ingStore = append(f.ingStore, tt.ingress)
			f.podStore = append(f.podStore, ingPo1, ingPo2)
			f.objects = append(f.objects, tt.ingress)

			if !reflect.DeepEqual(tt.wantLoadBalancerIngresses, tt.ingress.Status.LoadBalancer.Ingress) {
				f.expectUpdateIngAction(tt.ingress)
			}

			f.prepare()
			f.setupStore()

			require.NoError(t, f.lc.syncIngress(t.Context(), namespacedName(tt.ingress)))

			f.verifyActions()

			updatedIng, err := f.clientset.NetworkingV1().Ingresses(tt.ingress.Namespace).
				Get(t.Context(), tt.ingress.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.wantLoadBalancerIngresses, updatedIng.Status.LoadBalancer.Ingress)
		})
	}
}

// TestSyncNamedServicePort verifies that if a named service port is given in Ingress, a service port is looked up by the name.
func TestSyncNamedServicePort(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = corev1.ServicePort{
		Name:     "namedport",
		Port:     80,
		Protocol: corev1.ProtocolTCP,
	}
	ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortName(bs1.Spec.Ports[0].Name)).
		Complete()
	nghttpxSecret := newNghttpxSecret()

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1)
	f.ingStore = append(f.ingStore, ing1)
	f.secretStore = append(f.secretStore, nghttpxSecret)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	require.Len(t, ingConfig.Upstreams, 2)

	assert.Equal(t, namespacedName(ing1), ingConfig.Upstreams[1].Source)

	backend := ingConfig.Upstreams[1].Backends[0]
	assert.Equal(t, "80", backend.Port)
}

// TestSyncInternalDefaultBackend verifies that controller creates configuration for the internal default backend.
func TestSyncInternalDefaultBackend(t *testing.T) {
	tests := []struct {
		desc string
	}{
		{
			desc: "Create internal default backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			nghttpxSecret := newNghttpxSecret()

			f.secretStore = append(f.secretStore, nghttpxSecret)

			f.prepare()

			f.lbc.internalDefaultBackend = true

			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			require.Len(t, ingConfig.Upstreams, 1)

			upstream := ingConfig.Upstreams[0]
			assert.Empty(t, upstream.Path)

			backends := upstream.Backends
			require.Len(t, backends, 1)

			us := backends[0]
			assert.Equal(t, "127.0.0.1", us.Address)
			assert.True(t, upstream.DoNotForward)
		})
	}
}

// TestSyncDoNotForward verifies that service endpoints are ignored if doNotForward path-config is used.
func TestSyncDoNotForward(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
		WithRule("/", "alpha-svc", serviceBackendPortNumber(80)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha-ing.default.test/:
  doNotForward: true
  mruby: foo
`,
		}).
		Complete()

	nghttpxSecret := newNghttpxSecret()

	f.svcStore = append(f.svcStore, svc)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.ingStore = append(f.ingStore, ing1)
	f.secretStore = append(f.secretStore, nghttpxSecret)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	require.Len(t, ingConfig.Upstreams, 2)

	upstream := ingConfig.Upstreams[1]

	assert.Equal(t, namespacedName(ing1), upstream.Source)
	assert.True(t, upstream.DoNotForward)

	backend := upstream.Backends[0]
	assert.Equal(t, "8181", backend.Port)
}

// TestSyncNormalizePath verifies that a substring which starts with '#' or '?' is removed from path.
func TestSyncNormalizePath(t *testing.T) {
	tests := []struct {
		desc string
		path string
		want string
	}{
		{
			desc: "Path includes neither # nor ?",
			path: "/foo",
			want: "/foo",
		},
		{
			desc: "Path includes #",
			path: "/foo#bar#bar",
			want: "/foo",
		},
		{
			desc: "Path includes ?",
			path: "/baz?bar?bar",
			want: "/baz",
		},
		{
			desc: "Path includes both # and ?",
			path: "/foo?bar#bar",
			want: "/foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			svc, ess := newDefaultBackend()

			bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})
			ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
				WithRule(tt.path, bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				Complete()
			nghttpxSecret := newNghttpxSecret()

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.ingStore = append(f.ingStore, ing1)
			f.secretStore = append(f.secretStore, nghttpxSecret)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			require.Len(t, ingConfig.Upstreams, 2)

			upstream := ingConfig.Upstreams[1]

			assert.Equal(t, namespacedName(ing1), upstream.Source)
			assert.Equal(t, tt.want, upstream.Path)
		})
	}
}

// TestSyncSecretQUIC verifies syncSecret for QUIC keying materials.
func TestSyncSecretQUIC(t *testing.T) {
	now := time.Now().Round(time.Second)
	expiredTimestamp := now.Add(-defaultQUICSecretPeriod)
	notExpiredTimestamp := now.Add(-defaultQUICSecretPeriod + time.Second)

	tests := []struct {
		desc              string
		secret            *corev1.Secret
		wantKeepTimestamp bool
	}{
		{
			desc: "No existing QUIC keying materials secret",
		},
		{
			desc: "QUIC secret is up to date",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNghttpxSecret.Name,
					Namespace: defaultNghttpxSecret.Namespace,
					Annotations: map[string]string{
						quicKeyingMaterialsUpdateTimestampKey: notExpiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxQUICKeyingMaterialsSecretKey: []byte("" +
						"80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
						"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
					),
				},
			},
			wantKeepTimestamp: true,
		},
		{
			desc: "QUIC secret has been expired",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNghttpxSecret.Name,
					Namespace: defaultNghttpxSecret.Namespace,
					Annotations: map[string]string{
						quicKeyingMaterialsUpdateTimestampKey: expiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxQUICKeyingMaterialsSecretKey: []byte("" +
						"80112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233\n" +
						"c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233",
					),
				},
			},
		},
		{
			desc: "QUIC secret timestamp is not expired, but data is malformed",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNghttpxSecret.Name,
					Namespace: defaultNghttpxSecret.Namespace,
					Annotations: map[string]string{
						quicKeyingMaterialsUpdateTimestampKey: notExpiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxQUICKeyingMaterialsSecretKey: []byte(`foo`),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.http3 = true

			if tt.secret != nil {
				f.secretStore = append(f.secretStore, tt.secret)

				f.objects = append(f.objects, tt.secret)
			}

			f.prepare()
			f.setupStore()

			f.lbc.nghttpxSecret = defaultNghttpxSecret

			require.NoError(t, f.lc.syncSecret(t.Context(), defaultNghttpxSecret, now))

			updatedSecret, err := f.clientset.CoreV1().Secrets(defaultNghttpxSecret.Namespace).Get(t.Context(),
				defaultNghttpxSecret.Name, metav1.GetOptions{})
			require.NoError(t, err)

			if tt.wantKeepTimestamp {
				assert.Equal(t, tt.secret.Annotations[quicKeyingMaterialsUpdateTimestampKey], updatedSecret.Annotations[quicKeyingMaterialsUpdateTimestampKey])
				assert.Equal(t, tt.secret.Data[nghttpxQUICKeyingMaterialsSecretKey], updatedSecret.Data[nghttpxQUICKeyingMaterialsSecretKey])
			} else {
				assert.Equal(t, now.Format(time.RFC3339), updatedSecret.Annotations[quicKeyingMaterialsUpdateTimestampKey])

				km := updatedSecret.Data[nghttpxQUICKeyingMaterialsSecretKey]

				assert.False(t, len(km) < nghttpx.QUICKeyingMaterialsEncodedSize ||
					len(km) != len(km)/nghttpx.QUICKeyingMaterialsEncodedSize*nghttpx.QUICKeyingMaterialsEncodedSize+(len(km)/nghttpx.QUICKeyingMaterialsEncodedSize-1))

				if tt.secret != nil {
					assert.NotEqual(t, tt.secret.Data[nghttpxQUICKeyingMaterialsSecretKey], km)
				}

				require.NoError(t, nghttpx.VerifyQUICKeyingMaterials(km))
			}
		})
	}
}

// TestSyncSecretTLSTicketKey verifies syncSecret for TLS ticket key.
func TestSyncSecretTLSTicketKey(t *testing.T) {
	now := time.Now().Round(time.Second)
	expiredTimestamp := now.Add(-defaultTLSTicketKeyPeriod)
	notExpiredTimestamp := now.Add(-defaultTLSTicketKeyPeriod + time.Second)

	tests := []struct {
		desc              string
		secret            *corev1.Secret
		wantKeepTimestamp bool
	}{
		{
			desc: "No existing TLS ticket key secret",
		},
		{
			desc: "TLS ticket key secret is up to date",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNghttpxSecret.Name,
					Namespace: defaultNghttpxSecret.Namespace,
					Annotations: map[string]string{
						tlsTicketKeyUpdateTimestampKey: notExpiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxTLSTicketKeySecretKey: []byte("" +
						"................................................" +
						"................................................"),
				},
			},
			wantKeepTimestamp: true,
		},
		{
			desc: "TLS ticket key secret has been expired",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNghttpxSecret.Name,
					Namespace: defaultNghttpxSecret.Namespace,
					Annotations: map[string]string{
						tlsTicketKeyUpdateTimestampKey: expiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxTLSTicketKeySecretKey: []byte("" +
						"................................................" +
						"................................................",
					),
				},
			},
		},
		{
			desc: "TLS ticket key secret timestamp is not expired, but data is malformed",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultNghttpxSecret.Name,
					Namespace: defaultNghttpxSecret.Namespace,
					Annotations: map[string]string{
						tlsTicketKeyUpdateTimestampKey: notExpiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxTLSTicketKeySecretKey: []byte("foo"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.shareTLSTicketKey = true

			if tt.secret != nil {
				f.secretStore = append(f.secretStore, tt.secret)

				f.objects = append(f.objects, tt.secret)
			}

			f.prepare()
			f.setupStore()

			f.lbc.nghttpxSecret = defaultNghttpxSecret

			require.NoError(t, f.lc.syncSecret(t.Context(), defaultNghttpxSecret, now))

			updatedSecret, err := f.clientset.CoreV1().Secrets(defaultNghttpxSecret.Namespace).Get(t.Context(),
				defaultNghttpxSecret.Name, metav1.GetOptions{})
			require.NoError(t, err)

			if tt.wantKeepTimestamp {
				assert.Equal(t, tt.secret.Annotations[tlsTicketKeyUpdateTimestampKey], updatedSecret.Annotations[tlsTicketKeyUpdateTimestampKey])
				assert.Equal(t, tt.secret.Data[nghttpxTLSTicketKeySecretKey], updatedSecret.Data[nghttpxTLSTicketKeySecretKey])
			} else {
				assert.Equal(t, now.Format(time.RFC3339), updatedSecret.Annotations[tlsTicketKeyUpdateTimestampKey])

				if tt.secret != nil {
					assert.NotEqual(t, tt.secret.Data[nghttpxTLSTicketKeySecretKey], updatedSecret.Data[nghttpxTLSTicketKeySecretKey])
				}

				key := updatedSecret.Data[nghttpxTLSTicketKeySecretKey]
				require.NoError(t, nghttpx.VerifyTLSTicketKey(key))
			}
		})
	}
}

// TestRemoveUpstreamsWithInconsistentBackendParams verifies removeUpstreamsWithInconsistentBackendParams.
func TestRemoveUpstreamsWithInconsistentBackendParams(t *testing.T) {
	tests := []struct {
		desc      string
		upstreams []*nghttpx.Upstream
		want      []*nghttpx.Upstream
	}{
		{
			desc: "Empty upstreams",
		},
		{
			desc: "Nothing to remove",
			upstreams: []*nghttpx.Upstream{
				{
					Name:     "alpha0",
					Host:     "alpha",
					Path:     "/",
					Mruby:    newChecksumFile("mruby1"),
					Affinity: nghttpx.AffinityNone,
				},
				{
					Name:                     "alpha1",
					Host:                     "alpha",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookiePath:       "/foo",
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureAuto,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
				{
					Name:        "alpha2",
					Host:        "alpha",
					Path:        "/",
					Affinity:    nghttpx.AffinityNone,
					ReadTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
				{
					Name:         "alpha3",
					Host:         "alpha",
					Path:         "/",
					Affinity:     nghttpx.AffinityNone,
					WriteTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name:     "alpha0",
					Host:     "alpha",
					Path:     "/",
					Mruby:    newChecksumFile("mruby1"),
					Affinity: nghttpx.AffinityNone,
				},
				{
					Name:                     "alpha1",
					Host:                     "alpha",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookiePath:       "/foo",
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureAuto,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
				{
					Name:        "alpha2",
					Host:        "alpha",
					Path:        "/",
					Affinity:    nghttpx.AffinityNone,
					ReadTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
				{
					Name:         "alpha3",
					Host:         "alpha",
					Path:         "/",
					Affinity:     nghttpx.AffinityNone,
					WriteTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
			},
		},
		{
			desc: "Remove upstreams with inconsistent mruby",
			upstreams: []*nghttpx.Upstream{
				{
					Name:  "alpha0",
					Host:  "alpha",
					Path:  "/",
					Mruby: newChecksumFile("mruby1"),
				},
				{
					Name:  "alpha1",
					Host:  "alpha",
					Path:  "/",
					Mruby: newChecksumFile("mruby1"),
				},
				{
					Name:  "bravo0",
					Host:  "bravo",
					Path:  "/",
					Mruby: newChecksumFile("mruby2"),
				},
				{
					Name:  "bravo1",
					Host:  "bravo",
					Path:  "/",
					Mruby: newChecksumFile("mruby3"),
				},
				{
					Name: "charlie0",
					Host: "charlie",
					Path: "/",
				},
				{
					Name:  "charlie1",
					Host:  "charlie",
					Path:  "/",
					Mruby: newChecksumFile("mruby4"),
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name:  "alpha0",
					Host:  "alpha",
					Path:  "/",
					Mruby: newChecksumFile("mruby1"),
				},
				{
					Name:  "alpha1",
					Host:  "alpha",
					Path:  "/",
					Mruby: newChecksumFile("mruby1"),
				},
				{
					Name: "charlie0",
					Host: "charlie",
					Path: "/",
				},
				{
					Name:  "charlie1",
					Host:  "charlie",
					Path:  "/",
					Mruby: newChecksumFile("mruby4"),
				},
			},
		},
		{
			desc: "Remove upstreams with inconsistent affinity",
			upstreams: []*nghttpx.Upstream{
				{
					Name:               "alpha0",
					Host:               "alpha",
					Path:               "/",
					Affinity:           nghttpx.AffinityCookie,
					AffinityCookieName: "foo",
				},
				{
					Name:               "alpha1",
					Host:               "alpha",
					Path:               "/",
					Affinity:           nghttpx.AffinityCookie,
					AffinityCookieName: "foo",
				},
				{
					Name:               "alpha2",
					Host:               "alpha",
					Path:               "/",
					Affinity:           nghttpx.AffinityCookie,
					AffinityCookieName: "bar",
				},
				{
					Name:               "bravo0",
					Host:               "bravo",
					Path:               "/",
					Affinity:           nghttpx.AffinityCookie,
					AffinityCookieName: "foo",
				},
				{
					Name:               "bravo1",
					Host:               "bravo",
					Path:               "/",
					Affinity:           nghttpx.AffinityCookie,
					AffinityCookieName: "foo",
					AffinityCookiePath: "/",
				},
				{
					Name:                 "charlie0",
					Host:                 "charlie",
					Path:                 "/",
					Affinity:             nghttpx.AffinityCookie,
					AffinityCookieName:   "foo",
					AffinityCookieSecure: nghttpx.AffinityCookieSecureAuto,
				},
				{
					Name:                 "charlie1",
					Host:                 "charlie",
					Path:                 "/",
					Affinity:             nghttpx.AffinityCookie,
					AffinityCookieName:   "foo",
					AffinityCookieSecure: nghttpx.AffinityCookieSecureYes,
				},
				{
					Name:                     "delta0",
					Host:                     "delta",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
				{
					Name:                     "delta1",
					Host:                     "delta",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessStrict,
				},
				{
					Name:     "echo0",
					Host:     "echo",
					Path:     "/",
					Affinity: nghttpx.AffinityNone,
				},
				{
					Name:                     "echo1",
					Host:                     "echo",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookiePath:       "/foo",
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureYes,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessStrict,
				},
				{
					Name:     "echo2",
					Host:     "echo",
					Path:     "/",
					Affinity: nghttpx.AffinityNone,
				},
				{
					Name:                     "echo3",
					Host:                     "echo",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookiePath:       "/foo",
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureYes,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessStrict,
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name:     "echo0",
					Host:     "echo",
					Path:     "/",
					Affinity: nghttpx.AffinityNone,
				},
				{
					Name:                     "echo1",
					Host:                     "echo",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookiePath:       "/foo",
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureYes,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessStrict,
				},
				{
					Name:     "echo2",
					Host:     "echo",
					Path:     "/",
					Affinity: nghttpx.AffinityNone,
				},
				{
					Name:                     "echo3",
					Host:                     "echo",
					Path:                     "/",
					Affinity:                 nghttpx.AffinityCookie,
					AffinityCookieName:       "foo",
					AffinityCookiePath:       "/foo",
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureYes,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessStrict,
				},
			},
		},
		{
			desc: "Remove upstreams with inconsistent readTimeout",
			upstreams: []*nghttpx.Upstream{
				{
					Name:        "alpha0",
					Host:        "alpha",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
				{
					Name:        "alpha1",
					Host:        "alpha",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Minute},
				},
				{
					Name:        "bravo0",
					Host:        "bravo",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
				{
					Name:        "bravo1",
					Host:        "bravo",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: 10 * time.Second},
				},
				{
					Name:        "charlie0",
					Host:        "charlie",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
				{
					Name:        "charlie1",
					Host:        "charlie",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
				{
					Name: "charlie2",
					Host: "charlie",
					Path: "/",
				},
				{
					Name:        "delta0",
					Host:        "delta",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
				{
					Name:        "delta1",
					Host:        "delta",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name:        "charlie0",
					Host:        "charlie",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
				{
					Name:        "charlie1",
					Host:        "charlie",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
				{
					Name: "charlie2",
					Host: "charlie",
					Path: "/",
				},
			},
		},
		{
			desc: "Remove upstreams with inconsistent writeTimeout",
			upstreams: []*nghttpx.Upstream{
				{
					Name: "alpha0",
					Host: "alpha",
					Path: "/",
				},
				{
					Name:        "alpha1",
					Host:        "alpha",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Minute},
				},
				{
					Name: "alpha2",
					Host: "alpha",
					Path: "/",
				},
				{
					Name:        "bravo0",
					Host:        "bravo",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: 30 * time.Second},
				},
				{
					Name:        "bravo1",
					Host:        "bravo",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Hour},
				},
				{
					Name:        "charlie0",
					Host:        "charlie",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name: "alpha0",
					Host: "alpha",
					Path: "/",
				},
				{
					Name:        "alpha1",
					Host:        "alpha",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Minute},
				},
				{
					Name: "alpha2",
					Host: "alpha",
					Path: "/",
				},
				{
					Name:        "charlie0",
					Host:        "charlie",
					Path:        "/",
					ReadTimeout: &metav1.Duration{Duration: time.Second},
				},
			},
		},
		{
			desc: "Host with different Path",
			upstreams: []*nghttpx.Upstream{
				{
					Name:  "alpha0",
					Host:  "alpha",
					Path:  "/",
					Mruby: newChecksumFile("foo"),
				},
				{
					Name:  "alpha1",
					Host:  "alpha",
					Path:  "/",
					Mruby: newChecksumFile("bar"),
				},
				{
					Name:  "alpha2",
					Host:  "alpha",
					Path:  "/a",
					Mruby: newChecksumFile("baz"),
				},
				{
					Name:  "alpha2",
					Host:  "alpha",
					Path:  "/b",
					Mruby: newChecksumFile("foo"),
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name:  "alpha2",
					Host:  "alpha",
					Path:  "/a",
					Mruby: newChecksumFile("baz"),
				},
				{
					Name:  "alpha2",
					Host:  "alpha",
					Path:  "/b",
					Mruby: newChecksumFile("foo"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			assert.Equal(t, tt.want, removeUpstreamsWithInconsistentBackendParams(t.Context(), tt.upstreams))
		})
	}
}

// TestSyncIgnoreUpstreamsWithInconsistentBackendParams verifies that upstreams which have inconsistent backend parameters are ignored.
func TestSyncIgnoreUpstreamsWithInconsistentBackendParams(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha1", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha1").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha1.default.test/:
  mruby: foo
`,
		}).
		Complete()

	bs2, bes2 := newBackend("alpha2", []string{"192.168.10.2"})
	ing2 := newIngressBuilder(metav1.NamespaceDefault, "alpha2").
		WithRuleHost("alpha1.default.test", "/", bs2.Name, serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha1.default.test/:
  mruby: bar
`,
		}).
		Complete()

	bs3, bes3 := newBackend("alpha3", []string{"192.168.10.3"})
	ing3 := newIngressBuilder(metav1.NamespaceDefault, "alpha3").
		WithRule("/examples", bs3.Name, serviceBackendPortNumber(bs3.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha3.default.test/examples:
  mruby: foo
`,
		}).
		Complete()
	nghttpxSecret := newNghttpxSecret()

	f.svcStore = append(f.svcStore, svc, bs1, bs2, bs3)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1, bes2, bes3)
	f.ingStore = append(f.ingStore, ing1, ing2, ing3)
	f.secretStore = append(f.secretStore, nghttpxSecret)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	require.Len(t, ingConfig.Upstreams, 2)
	assert.Equal(t, f.lbc.defaultSvc.String(), ingConfig.Upstreams[0].Name)

	upstream := ingConfig.Upstreams[1]

	assert.Equal(t, namespacedName(ing3), upstream.Source)
}

// TestSyncEmptyAffinityCookieName verifies that an upstream which has empty affinity cookie name should be ignored.
func TestSyncEmptyAffinityCookieName(t *testing.T) {
	f := newFixture(t)

	svc, ess := newDefaultBackend()

	bs1, bes1 := newBackend("alpha1", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha1").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha1.default.test/:
  affinity: cookie
`,
		}).
		Complete()

	bs2, bes2 := newBackend("alpha2", []string{"192.168.10.2"})
	ing2 := newIngressBuilder(metav1.NamespaceDefault, "alpha2").
		WithRule("/", bs2.Name, serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha2.default.test/:
  affinity: cookie
  affinityCookieName: ""
`,
		}).
		Complete()

	bs3, bes3 := newBackend("alpha3", []string{"192.168.10.3"})
	ing3 := newIngressBuilder(metav1.NamespaceDefault, "alpha3").
		WithRule("/", bs3.Name, serviceBackendPortNumber(bs3.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha3.default.test/:
  affinity: cookie
  affinityCookieName: "foo"
`,
		}).
		Complete()
	nghttpxSecret := newNghttpxSecret()

	f.svcStore = append(f.svcStore, svc, bs1, bs2, bs3)
	f.epSliceStore = append(f.epSliceStore, ess...)
	f.epSliceStore = append(f.epSliceStore, bes1, bes2, bes3)
	f.ingStore = append(f.ingStore, ing1, ing2, ing3)
	f.secretStore = append(f.secretStore, nghttpxSecret)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	require.Len(t, ingConfig.Upstreams, 2)
	assert.Equal(t, f.lbc.defaultSvc.String(), ingConfig.Upstreams[0].Name)

	upstream := ingConfig.Upstreams[1]

	assert.Equal(t, namespacedName(ing3), upstream.Source)
}

func TestCreateTLSCredFromSecret(t *testing.T) {
	f := newFixture(t)
	f.prepare()

	s := newTLSSecret("ns", "cert", []byte(tlsCrt), []byte(tlsKey))

	_, err := f.lbc.createTLSCredFromSecret(t.Context(), s)
	require.NoError(t, err)

	cacheKey := createCertCacheKey(s)

	ent := f.lbc.certCache[cacheKey]
	require.NotNil(t, ent)

	certHash := calculateCertificateHash(s.Data[corev1.TLSCertKey], s.Data[corev1.TLSPrivateKeyKey])

	assert.Equal(t, certHash, ent.certificateHash)
	assert.Equal(t, s.Data[corev1.TLSCertKey], ent.certificate)
	assert.Equal(t, s.Data[corev1.TLSPrivateKeyKey], ent.key)

	// Should use cache.
	_, err = f.lbc.createTLSCredFromSecret(t.Context(), s)
	require.NoError(t, err)
	assert.Equal(t, ent, f.lbc.certCache[cacheKey])
}

func TestSyncWithTLSTicketKey(t *testing.T) {
	f := newFixture(t)
	f.shareTLSTicketKey = true

	dCrt := []byte(tlsCrt)
	dKey := []byte(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	nghttpxSecret := newNghttpxSecret()

	ticketKey, err := nghttpx.NewInitialTLSTicketKey()
	require.NoError(t, err)

	nghttpxSecret.Data = map[string][]byte{
		nghttpxTLSTicketKeySecretKey: ticketKey,
	}
	svc, ess := newDefaultBackend()

	f.secretStore = append(f.secretStore, tlsSecret, nghttpxSecret)
	f.svcStore = append(f.svcStore, svc)
	f.epSliceStore = append(f.epSliceStore, ess...)

	f.prepare()
	f.lbc.defaultTLSSecret = &types.NamespacedName{
		Namespace: tlsSecret.Namespace,
		Name:      tlsSecret.Name,
	}
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	assert.Len(t, ingConfig.TLSTicketKeyFiles, 2)
}
