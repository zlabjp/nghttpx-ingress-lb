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
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/events"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

type fixture struct {
	t *testing.T

	clientset *fake.Clientset

	lbc *LoadBalancerController
	lc  *LeaderController

	ingStore      []*networkingv1.Ingress
	ingClassStore []*networkingv1.IngressClass
	epStore       []*corev1.Endpoints
	svcStore      []*corev1.Service
	secretStore   []*corev1.Secret
	cmStore       []*corev1.ConfigMap
	podStore      []*corev1.Pod
	nodeStore     []*corev1.Node
	epSliceStore  []*discoveryv1.EndpointSlice

	objects []runtime.Object

	actions []core.Action

	enableEndpointSlice bool
	http3               bool
	publishService      *types.NamespacedName
	requireIngressClass bool
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
	defaultConfDir                = "conf"

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

	defaultQUICSecret = types.NamespacedName{
		Name:      "nghttpx-quic-secret",
		Namespace: "kube-system",
	}
)

// prepare performs setup necessary for test run.
func (f *fixture) prepare() {
	f.preparePod(newIngPod(defaultRuntimeInfo.Name, "zulu.test"))
}

func (f *fixture) preparePod(pod *corev1.Pod) {
	f.clientset = fake.NewSimpleClientset(f.objects...)
	config := Config{
		DefaultBackendService:  &types.NamespacedName{Namespace: defaultBackendNamespace, Name: defaultBackendName},
		WatchNamespace:         defaultIngNamespace,
		NghttpxConfigMap:       &types.NamespacedName{Namespace: defaultConfigMapNamespace, Name: defaultConfigMapName},
		NghttpxConfDir:         defaultConfDir,
		IngressClassController: defaultIngressClassController,
		EnableEndpointSlice:    f.enableEndpointSlice,
		ReloadRate:             1.0,
		ReloadBurst:            1,
		HTTP3:                  f.http3,
		PublishService:         f.publishService,
		RequireIngressClass:    f.requireIngressClass,
		Pod:                    pod,
		EventRecorder:          &events.FakeRecorder{},
	}

	if f.http3 {
		config.QUICKeyingMaterialsSecret = &types.NamespacedName{
			Name:      defaultQUICSecret.Name,
			Namespace: defaultQUICSecret.Namespace,
		}
	}

	lbc, err := NewLoadBalancerController(f.clientset, newFakeLoadBalancer(), config)
	if err != nil {
		f.t.Fatalf("NewLoadBalancerController: %v", err)
	}

	lc, err := NewLeaderController(lbc)
	if err != nil {
		f.t.Fatalf("NewLeaderController: %v", err)
	}

	f.lbc = lbc
	f.lc = lc
}

func (f *fixture) run() {
	f.setupStore()

	if err := f.lbc.sync(context.Background(), syncKey); err != nil {
		f.t.Errorf("Failed to sync: %v", err)
	}

	f.verifyActions()
}

func (f *fixture) runShouldFail() {
	f.setupStore()

	if err := f.lbc.sync(context.Background(), syncKey); err == nil {
		f.t.Errorf("sync should fail")
	}

	f.verifyActions()
}

func (f *fixture) setupStore() {
	for _, ing := range f.ingStore {
		if err := f.lbc.ingIndexer.Add(ing); err != nil {
			panic(err)
		}
		if err := f.lc.ingIndexer.Add(ing); err != nil {
			panic(err)
		}
	}
	for _, ingClass := range f.ingClassStore {
		if err := f.lbc.ingClassIndexer.Add(ingClass); err != nil {
			panic(err)
		}
		if err := f.lc.ingClassIndexer.Add(ingClass); err != nil {
			panic(err)
		}
	}
	if f.enableEndpointSlice {
		for _, es := range f.epSliceStore {
			if err := f.lbc.epSliceIndexer.Add(es); err != nil {
				panic(err)
			}
		}
	} else {
		for _, ep := range f.epStore {
			if err := f.lbc.epIndexer.Add(ep); err != nil {
				panic(err)
			}
		}
	}
	for _, svc := range f.svcStore {
		if err := f.lbc.svcIndexer.Add(svc); err != nil {
			panic(err)
		}
		if f.lc.svcIndexer != nil {
			if err := f.lc.svcIndexer.Add(svc); err != nil {
				panic(err)
			}
		}
	}
	for _, secret := range f.secretStore {
		if err := f.lbc.secretIndexer.Add(secret); err != nil {
			panic(err)
		}
		if f.lc.secretIndexer != nil {
			if err := f.lc.secretIndexer.Add(secret); err != nil {
				panic(err)
			}
		}
	}
	if f.lbc.cmIndexer != nil {
		for _, cm := range f.cmStore {
			if err := f.lbc.cmIndexer.Add(cm); err != nil {
				panic(err)
			}
		}
	}
	for _, pod := range f.podStore {
		if err := f.lbc.podIndexer.Add(pod); err != nil {
			panic(err)
		}
		if err := f.lc.podIndexer.Add(pod); err != nil {
			panic(err)
		}
	}
	for _, node := range f.nodeStore {
		if err := f.lc.nodeIndexer.Add(node); err != nil {
			panic(err)
		}
	}
}

func (f *fixture) verifyActions() {
	actions := f.clientset.Actions()
	for i, action := range actions {
		if len(f.actions) < i+1 {
			f.t.Errorf("%v unexpected action: %+v", len(actions)-len(f.actions), actions[i:])
			break
		}
		expectedAction := f.actions[i]
		if !expectedAction.Matches(action.GetVerb(), action.GetResource().Resource) {
			f.t.Errorf("Expected\n\t%+v\ngot\n\t%+v", expectedAction, action)
		}
	}
	if len(f.actions) > len(actions) {
		f.t.Errorf("%v additional expected actions: %+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}
}

// expectUpdateIngAction adds an expectation that update for ing should occur.
func (f *fixture) expectUpdateIngAction(ing *networkingv1.Ingress) {
	f.actions = append(f.actions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "ingresses"}, ing.Namespace, ing))
}

// fakeLoadBalancer implements nghttpx.ServerReloader.
type fakeLoadBalancer struct {
	checkAndReloadHandler func(ingConfig *nghttpx.IngressConfig) (bool, error)

	ingConfig *nghttpx.IngressConfig
}

// newFakeLoadBalancer creates new fakeLoadBalancer.
func newFakeLoadBalancer() *fakeLoadBalancer {
	flb := &fakeLoadBalancer{}
	flb.checkAndReloadHandler = flb.defaultCheckAndReload
	return flb
}

func (flb *fakeLoadBalancer) Start(ctx context.Context, path, confPath string) error {
	return nil
}

func (flb *fakeLoadBalancer) CheckAndReload(ctx context.Context, ingConfig *nghttpx.IngressConfig) (bool, error) {
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

// newDefaultBackend returns Service and Endpoints for default backend.
func newDefaultBackend() (*corev1.Service, *corev1.Endpoints, []*discoveryv1.EndpointSlice) {
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
	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "192.168.100.1",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Name:      defaultBackendName + "-pod-1",
							Namespace: defaultBackendNamespace,
						},
					},
					{
						IP: "192.168.100.2",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Name:      defaultBackendName + "-pod-2",
							Namespace: defaultBackendNamespace,
						},
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     8081,
					},
					{
						Protocol: corev1.ProtocolTCP,
						Port:     8080,
					},
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

	return svc, eps, ess
}

// newDefaultBackendWithoutSelectors returns Service and Endpoints for default backend without Service selectors.
func newDefaultBackendWithoutSelectors() (*corev1.Service, *corev1.Endpoints, []*discoveryv1.EndpointSlice) {
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
	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "192.168.100.1",
					},
					{
						IP: "192.168.100.2",
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     8081,
					},
					{
						Protocol: corev1.ProtocolTCP,
						Port:     8080,
					},
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

	return svc, eps, ess
}

func newBackend(namespace, name string, addrs []string) (*corev1.Service, *corev1.Endpoints, *discoveryv1.EndpointSlice) {
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
	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Ports: []corev1.EndpointPort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     81,
					},
					{
						Protocol: corev1.ProtocolTCP,
						Port:     80,
					},
				},
			},
		},
	}

	endpointAddrs := make([]corev1.EndpointAddress, len(addrs))
	for i, addr := range addrs {
		endpointAddrs[i] = corev1.EndpointAddress{
			IP: addr,
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      fmt.Sprintf("%v-pod-%v", name, i+1),
				Namespace: namespace,
			},
		}
	}

	eps.Subsets[0].Addresses = endpointAddrs

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
				Name:      fmt.Sprintf("%v-pod-%v", name, i+1),
				Namespace: namespace,
			},
		})
	}

	return svc, eps, es
}

func newBackendWithoutSelectors(namespace, name string, addrs []string) (*corev1.Service, *corev1.Endpoints, *discoveryv1.EndpointSlice) {
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
	eps := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Ports: []corev1.EndpointPort{
					{
						Protocol: corev1.ProtocolTCP,
						Port:     81,
					},
					{
						Protocol: corev1.ProtocolTCP,
						Port:     80,
					},
				},
			},
		},
	}

	endpointAddrs := make([]corev1.EndpointAddress, len(addrs))
	for i, addr := range addrs {
		endpointAddrs[i] = corev1.EndpointAddress{
			IP: addr,
		}
	}

	eps.Subsets[0].Addresses = endpointAddrs

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

	return svc, eps, es
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
	return b.WithRuleHost(fmt.Sprintf("%v.%v.test", b.Name, b.Namespace), path, svc, port)
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

func newChecksumFile(path string) *nghttpx.ChecksumFile {
	return &nghttpx.ChecksumFile{
		Path: path,
	}
}

// TestSyncDefaultBackend verifies that controller creates configuration for default service backend.
func TestSyncDefaultBackend(t *testing.T) {
	tests := []struct {
		desc                string
		enableEndpointSlice bool
		withoutSelectors    bool
	}{
		{
			desc: "With Endpoints",
		},
		{
			desc:                "With EndpointSlice",
			enableEndpointSlice: true,
		},
		{
			desc:             "Without selectors",
			withoutSelectors: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.enableEndpointSlice = tt.enableEndpointSlice

			cm := newEmptyConfigMap()
			cm.Data[nghttpx.NghttpxExtraConfigKey] = "Test"
			const mrubyContent = "mruby"
			cm.Data[nghttpx.NghttpxMrubyFileContentKey] = mrubyContent
			var (
				svc *corev1.Service
				eps *corev1.Endpoints
				ess []*discoveryv1.EndpointSlice
			)
			if tt.withoutSelectors {
				svc, eps, ess = newDefaultBackendWithoutSelectors()
			} else {
				svc, eps, ess = newDefaultBackend()
			}

			f.cmStore = append(f.cmStore, cm)
			f.svcStore = append(f.svcStore, svc)
			f.epStore = append(f.epStore, eps)
			f.epSliceStore = append(f.epSliceStore, ess...)

			f.objects = append(f.objects, cm, svc, eps)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			if got, want := ingConfig.TLS, false; got != want {
				t.Errorf("ingConfig.TLS = %v, want %v", got, want)
			}

			if got, want := len(ingConfig.Upstreams), 1; got != want {
				t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			} else {
				upstream := ingConfig.Upstreams[0]
				if got, want := upstream.Path, ""; got != want {
					t.Errorf("upstream.Path = %v, want %v", got, want)
				}
				backends := upstream.Backends
				if got, want := len(backends), 2; got != want {
					t.Errorf("len(backends) = %v, want %v", got, want)
				}
				us := backends[0]
				if got, want := us.Address, "192.168.100.1"; got != want {
					t.Errorf("0: us.Address = %v, want %v", got, want)
				}
				if got, want := us.Port, "8080"; got != want {
					t.Errorf("0: us.Port = %v, want %v", got, want)
				}
			}

			if got, want := flb.ingConfig.ExtraConfig, cm.Data[nghttpx.NghttpxExtraConfigKey]; got != want {
				t.Errorf("flb.cfg.ExtraConfig = %v, want %v", got, want)
			}
			if got, want := flb.ingConfig.MrubyFile, (&nghttpx.ChecksumFile{
				Path:     nghttpx.MrubyRbPath(defaultConfDir),
				Content:  []byte(mrubyContent),
				Checksum: nghttpx.Checksum([]byte(mrubyContent)),
			}); !reflect.DeepEqual(got, want) {
				t.Errorf("flb.ingConfig.MrubyFile = %q, want %q", got, want)
			}
		})
	}
}

// TestSyncDefaultTLSSecretNotFound verifies that sync must fail if default TLS Secret is not found.
func TestSyncDefaultTLSSecretNotFound(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	f.svcStore = append(f.svcStore, svc)
	f.epStore = append(f.epStore, eps)

	f.objects = append(f.objects, svc, eps)

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
	svc, eps, _ := newDefaultBackend()

	f.secretStore = append(f.secretStore, tlsSecret)
	f.svcStore = append(f.svcStore, svc)
	f.epStore = append(f.epStore, eps)

	f.objects = append(f.objects, tlsSecret, svc, eps)

	f.prepare()
	f.lbc.defaultTLSSecret = &types.NamespacedName{
		Namespace: tlsSecret.Namespace,
		Name:      tlsSecret.Name,
	}
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := ingConfig.TLS, true; got != want {
		t.Errorf("ingConfig.TLS = %v, want %v", got, want)
	}

	dKeyChecksum := nghttpx.Checksum(dKey)
	dCrtChecksum := nghttpx.Checksum(dCrt)

	if got, want := ingConfig.DefaultTLSCred.Key.Path, nghttpx.CreateTLSKeyPath(defaultConfDir, hex.EncodeToString(dKeyChecksum)); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Key.Path = %v, want %v", got, want)
	}
	if got, want := ingConfig.DefaultTLSCred.Cert.Path, nghttpx.CreateTLSCertPath(defaultConfDir, hex.EncodeToString(dCrtChecksum)); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Cert.Path = %v, want %v", got, want)
	}
	if got, want := ingConfig.DefaultTLSCred.Key.Checksum, dKeyChecksum; !bytes.Equal(got, want) {
		t.Errorf("ingConfig.DefaultTLSCred.Key.Checksum = %x, want %x", got, want)
	}
	if got, want := ingConfig.DefaultTLSCred.Cert.Checksum, dCrtChecksum; !bytes.Equal(got, want) {
		t.Errorf("ingConfig.DefaultTLSCred.Cert.Checksum = %v, want %x", got, want)
	}

	if got, want := ingConfig.Upstreams[0].RedirectIfNotTLS, true; got != want {
		t.Errorf("ingConfig.RedirectIfNotTLS = %v, want %v", got, want)
	}
}

// TestSyncDupDefaultSecret verifies that duplicated default TLS secret is removed.
func TestSyncDupDefaultSecret(t *testing.T) {
	f := newFixture(t)

	dCrt := []byte(tlsCrt)
	dKey := []byte(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithTLS(tlsSecret.Name).
		Complete()

	f.secretStore = append(f.secretStore, tlsSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)

	f.objects = append(f.objects, tlsSecret, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.lbc.defaultTLSSecret = &types.NamespacedName{
		Namespace: tlsSecret.Namespace,
		Name:      tlsSecret.Name,
	}
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := ingConfig.TLS, true; got != want {
		t.Errorf("ingConfig.TLS = %v, want %v", got, want)
	}

	if got, want := ingConfig.DefaultTLSCred.Key.Path, nghttpx.CreateTLSKeyPath(defaultConfDir, hex.EncodeToString(nghttpx.Checksum(dKey))); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Key.Path = %v, want %v", got, want)
	}
	if got, want := len(ingConfig.SubTLSCred), 0; got != want {
		t.Errorf("len(ingConfig.SubTLSCred) = %v, want %v", got, want)
	}

	for i := range ingConfig.Upstreams {
		if got, want := ingConfig.Upstreams[i].RedirectIfNotTLS, true; got != want {
			t.Errorf("ingConfig.Upstreams[%v].RedirectIfNotTLS = %v, want %v", i, got, want)
		}
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
	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithTLS(tlsSecret.Name).
		Complete()

	f.secretStore = append(f.secretStore, tlsSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)

	f.objects = append(f.objects, tlsSecret, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if ingConfig.DefaultTLSCred == nil {
		t.Fatal("ingConfig.DefaultTLSCred should not be nil")
	}

	tlsCred := ingConfig.DefaultTLSCred
	if got, want := string(tlsCred.Cert.Content), tlsCrt; got != want {
		t.Errorf("tlsCred.Cert.Content = %v, want %v", got, want)
	}
	if got, want := string(tlsCred.Key.Content), tlsKey; got != want {
		t.Errorf("tlsCred.Key.Content = %v, want %v", got, want)
	}

	if got, want := len(ingConfig.SubTLSCred), 0; got != want {
		t.Errorf("len(ingConfig.SubTLSCred) = %v, want %v", got, want)
	}

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Fatalf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	upstream := ingConfig.Upstreams[1]
	if got, want := upstream.Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
		t.Errorf("upstream.Ingress = %v, want %v", got, want)
	}
	if got, want := upstream.RedirectIfNotTLS, true; got != want {
		t.Errorf("upstream.RedirectIfNotTLS = %v, want %v", got, want)
	}
}

// TestSyncStringNamedPort verifies that if service target port is a named port, it is looked up from Pod spec.
func TestSyncStringNamedPort(t *testing.T) {
	tests := []struct {
		desc                string
		enableEndpointSlice bool
	}{
		{
			desc: "With Endpoints",
		},
		{
			desc:                "With EndpointSlice",
			enableEndpointSlice: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.enableEndpointSlice = tt.enableEndpointSlice

			svc, eps, ess := newDefaultBackend()

			bs1, be1, bes1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1", "192.168.10.2"})
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

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epStore = append(f.epStore, eps, be1)
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.ingStore = append(f.ingStore, ing1)
			f.podStore = append(f.podStore, bp1, bp2)

			f.objects = append(f.objects, svc, eps, bs1, be1, ing1, bp1, bp2)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			if got, want := ingConfig.Upstreams[1].Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
				t.Errorf("ingConfig.Upstreams[1].Ingress = %v, want %v", got, want)
			}

			if got, want := len(ingConfig.Upstreams[1].Backends), 2; got != want {
				t.Errorf("len(ingConfig.Upstreams[0].Backends) = %v, want %v", got, want)
			} else {
				for i, port := range []string{"80", "81"} {
					backend := ingConfig.Upstreams[1].Backends[i]
					if got, want := backend.Port, port; got != want {
						t.Errorf("backends[i].Port = %v, want %v", got, want)
					}
				}
			}
		})
	}
}

// TestSyncEmptyTargetPort verifies that if target port is empty, port is used instead.  In practice, target port is always filled out.
func TestSyncEmptyTargetPort(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = corev1.ServicePort{
		Port:       80,
		TargetPort: intstr.FromString(""),
		Protocol:   corev1.ProtocolTCP,
	}
	ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		Complete()

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	if got, want := ingConfig.Upstreams[1].Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
		t.Errorf("ingConfig.Upstreams[1].Ingress = %v, want %v", got, want)
	}

	backend := ingConfig.Upstreams[1].Backends[0]
	if got, want := backend.Port, "80"; got != want {
		t.Errorf("backend.Port = %v, want %v", got, want)
	}
}

// TestSyncWithoutSelectors verifies that the controller deals with Service without selectors.
func TestSyncWithoutSelectors(t *testing.T) {
	tests := []struct {
		desc                string
		enableEndpointSlice bool
	}{
		{
			desc: "With Endpoints",
		},
		{
			desc:                "With EndpointSlice",
			enableEndpointSlice: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.enableEndpointSlice = tt.enableEndpointSlice

			svc, eps, ess := newDefaultBackend()

			bs1, be1, bes1 := newBackendWithoutSelectors(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
			ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
				WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				Complete()

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epStore = append(f.epStore, eps, be1)
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.ingStore = append(f.ingStore, ing1)

			f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			if got, want := ingConfig.Upstreams[1].Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
				t.Errorf("ingConfig.Upstreams[1].Ingress = %v, want %v", got, want)
			}

			backend := ingConfig.Upstreams[1].Backends[0]
			if got, want := backend.Port, "80"; got != want {
				t.Errorf("backend.Port = %v, want %v", got, want)
			}
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

			if got, want := f.lbc.validateIngressClass(tt.ing), tt.want; got != want {
				t.Errorf("f.lbc.validateIngressClass(...) = %v, want %v", got, want)
			}
		})
	}
}

// TestSyncIngressDefaultBackend verfies that Ingress.Spec.DefaultBackend is considered.
func TestSyncIngressDefaultBackend(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs2, be2, _ := newBackend(metav1.NamespaceDefault, "bravo", []string{"192.168.10.2"})
	ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithDefaultBackend("bravo", serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
		Complete()

	f.svcStore = append(f.svcStore, svc, bs1, bs2)
	f.epStore = append(f.epStore, eps, be1, be2)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1, bs2, be2)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	var found bool
	for _, upstream := range ingConfig.Upstreams {
		if upstream.Name == "default/bravo,8281;/" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Ingress default backend is not found")
	}
}

// TestSyncIngressNoDefaultBackendOverride verifies that any settings or rules which override default backend are ignored.
func TestSyncIngressNoDefaultBackendOverride(t *testing.T) {
	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs2, be2, _ := newBackend(metav1.NamespaceDefault, "bravo", []string{"192.168.10.2"})

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

			svc, eps, ess := newDefaultBackend()

			f.svcStore = append(f.svcStore, svc, bs1.DeepCopyObject().(*corev1.Service), bs2.DeepCopyObject().(*corev1.Service))
			f.epStore = append(f.epStore, eps, be1.DeepCopyObject().(*corev1.Endpoints), be2.DeepCopyObject().(*corev1.Endpoints))
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.ingStore = append(f.ingStore, tt.ing)

			f.prepare()
			f.lbc.noDefaultBackendOverride = true
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Fatalf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			if got, want := ingConfig.Upstreams[0].Ingress, (types.NamespacedName{}); got != want {
				t.Errorf("ingConfig.Upstreams[0].Ingress = %v, want %v", got, want)
			}

			if got, want := ingConfig.Upstreams[0].Name, f.lbc.defaultSvc.String(); got != want {
				t.Errorf("ingConfig.Upstreams[0].Name = %v, want %v", got, want)
			}
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

			f.objects = append(f.objects, po1, po2, node2)

			if tt.hostNetwork {
				f.nodeStore = append(f.nodeStore, node1)
				f.objects = append(f.objects, node1)
			}

			f.preparePod(po1)
			f.setupStore()

			lbIngs, err := f.lc.getLoadBalancerIngressSelector(labels.Set(defaultIngPodLables).AsSelector())

			f.verifyActions()

			if err != nil {
				t.Fatalf("f.lc.getLoadBalancerIngressSelector() returned unexpected error %v", err)
			}

			if got, want := len(lbIngs), 2; got != want {
				t.Errorf("len(lbIngs) = %v, want %v", got, want)
			}

			sortLoadBalancerIngress(lbIngs)

			ans := []networkingv1.IngressLoadBalancerIngress{
				{IP: "192.168.0.1"}, {IP: "192.168.0.2"},
			}

			if got, want := lbIngs, ans; !reflect.DeepEqual(got, want) {
				t.Errorf("lbIngs = %+v, want %+v", got, want)
			}
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
			f.objects = append(f.objects, tt.ingress, ingPo1, ingPo2)

			if !reflect.DeepEqual(tt.wantLoadBalancerIngresses, tt.ingress.Status.LoadBalancer.Ingress) {
				f.expectUpdateIngAction(tt.ingress)
			}

			f.prepare()
			f.setupStore()

			key := types.NamespacedName{Name: tt.ingress.Name, Namespace: tt.ingress.Namespace}.String()
			err := f.lc.syncIngress(context.Background(), key)
			if err != nil {
				t.Fatalf("f.lc.syncIngress(...): %v", err)
			}

			f.verifyActions()

			updatedIng, err := f.clientset.NetworkingV1().Ingresses(tt.ingress.Namespace).
				Get(context.Background(), tt.ingress.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Could not get Ingress %v/%v: %v", tt.ingress.Namespace, tt.ingress.Name, err)
			}

			if got, want := updatedIng.Status.LoadBalancer.Ingress, tt.wantLoadBalancerIngresses; !reflect.DeepEqual(got, want) {
				t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
			}
		})
	}
}

// TestSyncNamedServicePort verifies that if a named service port is given in Ingress, a service port is looked up by the name.
func TestSyncNamedServicePort(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = corev1.ServicePort{
		Name:     "namedport",
		Port:     80,
		Protocol: corev1.ProtocolTCP,
	}
	ing1 := newIngressBuilder(bs1.Namespace, "alpha-ing").
		WithRule("/", bs1.Name, serviceBackendPortName(bs1.Spec.Ports[0].Name)).
		Complete()

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	if got, want := ingConfig.Upstreams[1].Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
		t.Errorf("ingConfig.Upstreams[1].Ingress = %v, want %v", got, want)
	}

	backend := ingConfig.Upstreams[1].Backends[0]
	if got, want := backend.Port, "80"; got != want {
		t.Errorf("backend.Port = %v, want %v", got, want)
	}
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

			f.prepare()

			f.lbc.internalDefaultBackend = true

			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			if got, want := len(ingConfig.Upstreams), 1; got != want {
				t.Fatalf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			upstream := ingConfig.Upstreams[0]
			if got, want := upstream.Path, ""; got != want {
				t.Errorf("upstream.Path = %v, want %v", got, want)
			}

			backends := upstream.Backends
			if got, want := len(backends), 1; got != want {
				t.Errorf("len(backends) = %v, want %v", got, want)
			}

			us := backends[0]
			if got, want := us.Address, "127.0.0.1"; got != want {
				t.Errorf("0: us.Address = %v, want %v", got, want)
			}

			if got, want := upstream.DoNotForward, true; got != want {
				t.Errorf("upstream.DoNotForward = %v, want %v", got, want)
			}
		})
	}
}

// TestSyncDoNotForward verifies that service endpoints are ignored if doNotForward path-config is used.
func TestSyncDoNotForward(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
		WithRule("/", "alpha-svc", serviceBackendPortNumber(80)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha-ing.default.test/:
  doNotForward: true
  mruby: foo
`}).
		Complete()

	f.svcStore = append(f.svcStore, svc)
	f.epStore = append(f.epStore, eps)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, ing1)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	upstream := ingConfig.Upstreams[1]

	if got, want := upstream.Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
		t.Errorf("upstream.Ingress = %v, want %v", got, want)
	}

	if got, want := upstream.DoNotForward, true; got != want {
		t.Errorf("upstream.DoNotForward = %v, want %v", got, want)
	}

	backend := upstream.Backends[0]
	if got, want := backend.Port, "8181"; got != want {
		t.Errorf("backend.Port = %v, want %v", got, want)
	}
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

			svc, eps, _ := newDefaultBackend()

			bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
			ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha-ing").
				WithRule(tt.path, bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
				Complete()

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epStore = append(f.epStore, eps, be1)
			f.ingStore = append(f.ingStore, ing1)

			f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

			f.prepare()
			f.run()

			flb := f.lbc.nghttpx.(*fakeLoadBalancer)
			ingConfig := flb.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Fatalf("len(ingConfig.Upstream) = %v want %v", got, want)
			}

			upstream := ingConfig.Upstreams[1]

			if got, want := upstream.Ingress, (types.NamespacedName{Name: ing1.Name, Namespace: ing1.Namespace}); got != want {
				t.Errorf("upstream.Ingress = %v, want %v", got, want)
			}

			if got, want := upstream.Path, tt.want; got != want {
				t.Errorf("upstream.Path = %v, want %v", got, want)
			}
		})
	}
}

// TestSyncQUICKeyingMaterials verifies syncQUICKeyingMaterials.
func TestSyncQUICKeyingMaterials(t *testing.T) {
	now := time.Now().Round(time.Second)
	expiredTimestamp := now.Add(-quicSecretTimeout)
	notExpiredTimestamp := now.Add(-quicSecretTimeout + time.Second)

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
					Name:      defaultQUICSecret.Name,
					Namespace: defaultQUICSecret.Namespace,
					Annotations: map[string]string{
						quicKeyingMaterialsUpdateTimestampKey: notExpiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxQUICKeyingMaterialsSecretKey: []byte(`c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
				},
			},
			wantKeepTimestamp: true,
		},
		{
			desc: "QUIC secret has been expired",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultQUICSecret.Name,
					Namespace: defaultQUICSecret.Namespace,
					Annotations: map[string]string{
						quicKeyingMaterialsUpdateTimestampKey: expiredTimestamp.Format(time.RFC3339),
					},
				},
				Data: map[string][]byte{
					nghttpxQUICKeyingMaterialsSecretKey: []byte(`c0112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00112233`),
				},
			},
		},
		{
			desc: "QUIC secret timestamp is not expired, but data is malformed",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaultQUICSecret.Name,
					Namespace: defaultQUICSecret.Namespace,
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

			f.lbc.quicKeyingMaterialsSecret = &defaultQUICSecret

			err := f.lc.syncQUICKeyingMaterials(context.Background(), now)
			if err != nil {
				t.Fatalf("f.lc.syncQUICKeyingMaterials(...): %v", err)
			}

			updatedSecret, err := f.clientset.CoreV1().Secrets(defaultQUICSecret.Namespace).Get(context.Background(), defaultQUICSecret.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Could not get Secret %v/%v: %v", defaultQUICSecret.Namespace, defaultQUICSecret.Name, err)
			}

			if tt.wantKeepTimestamp {
				if got, want := updatedSecret.Annotations[quicKeyingMaterialsUpdateTimestampKey], tt.secret.Annotations[quicKeyingMaterialsUpdateTimestampKey]; got != want {
					t.Errorf("updatedSecret.Annotations[%q] = %v, want %v", quicKeyingMaterialsUpdateTimestampKey, got, want)
				}

				if got, want := updatedSecret.Data[nghttpxQUICKeyingMaterialsSecretKey], tt.secret.Data[nghttpxQUICKeyingMaterialsSecretKey]; !bytes.Equal(got, want) {
					t.Errorf("updatedSecret.Data[%q] = %s, want %s", nghttpxQUICKeyingMaterialsSecretKey, got, want)
				}
			} else {
				if got, want := updatedSecret.Annotations[quicKeyingMaterialsUpdateTimestampKey], now.Format(time.RFC3339); got != want {
					t.Errorf("updatedSecret.Annotations[%q] = %v, want %v", quicKeyingMaterialsUpdateTimestampKey, got, want)
				}

				km := updatedSecret.Data[nghttpxQUICKeyingMaterialsSecretKey]
				if len(km) < nghttpx.QUICKeyingMaterialsEncodedSize ||
					len(km) != len(km)/nghttpx.QUICKeyingMaterialsEncodedSize*nghttpx.QUICKeyingMaterialsEncodedSize+(len(km)/nghttpx.QUICKeyingMaterialsEncodedSize-1) {
					t.Fatal("updatedSecret does not contain QUIC keying materials", len(km))
				}

				if err := nghttpx.VerifyQUICKeyingMaterials(km); err != nil {
					t.Fatalf("verifyQUICKeyingMaterials(...): %v", err)
				}
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
			if got, want := removeUpstreamsWithInconsistentBackendParams(tt.upstreams), tt.want; !equality.Semantic.DeepEqual(got, want) {
				t.Errorf("removeUpstreamsWithInconsistentBackendParams(...) = %v, want %v", got, want)
			}
		})
	}
}

// TestSyncIgnoreUpstreamsWithInconsistentBackendParams verifies that upstreams which have inconsistent backend parameters are ignored.
func TestSyncIgnoreUpstreamsWithInconsistentBackendParams(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha1", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha1").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha1.default.test/:
  mruby: foo
`}).
		Complete()

	bs2, be2, _ := newBackend(metav1.NamespaceDefault, "alpha2", []string{"192.168.10.2"})
	ing2 := newIngressBuilder(metav1.NamespaceDefault, "alpha2").
		WithRuleHost("alpha1.default.test", "/", bs2.Name, serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha1.default.test/:
  mruby: bar
`}).
		Complete()

	bs3, be3, _ := newBackend(metav1.NamespaceDefault, "alpha3", []string{"192.168.10.3"})
	ing3 := newIngressBuilder(metav1.NamespaceDefault, "alpha3").
		WithRule("/examples", bs3.Name, serviceBackendPortNumber(bs3.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha3.default.test/examples:
  mruby: foo
`}).
		Complete()

	f.svcStore = append(f.svcStore, svc, bs1, bs2, bs3)
	f.epStore = append(f.epStore, eps, be1, be2, be3)
	f.ingStore = append(f.ingStore, ing1, ing2, ing3)

	f.objects = append(f.objects, svc, bs1, bs2, bs3, eps, be1, be2, be3, ing1, ing2, ing3)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	if got, want := ingConfig.Upstreams[0].Name, f.lbc.defaultSvc.String(); got != want {
		t.Errorf("ingConfig.Upstreams[0].Name = %v, want %v", got, want)
	}

	upstream := ingConfig.Upstreams[1]

	if got, want := upstream.Ingress, (types.NamespacedName{Name: ing3.Name, Namespace: ing3.Namespace}); got != want {
		t.Errorf("upstream.Ingress = %v, want %v", got, want)
	}
}

// TestSyncEmptyAffinityCookieName verifies that an upstream which has empty affinity cookie name should be ignored.
func TestSyncEmptyAffinityCookieName(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha1", []string{"192.168.10.1"})
	ing1 := newIngressBuilder(metav1.NamespaceDefault, "alpha1").
		WithRule("/", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha1.default.test/:
  affinity: cookie
`}).
		Complete()

	bs2, be2, _ := newBackend(metav1.NamespaceDefault, "alpha2", []string{"192.168.10.2"})
	ing2 := newIngressBuilder(metav1.NamespaceDefault, "alpha2").
		WithRule("/", bs2.Name, serviceBackendPortNumber(bs2.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha2.default.test/:
  affinity: cookie
  affinityCookieName: ""
`}).
		Complete()

	bs3, be3, _ := newBackend(metav1.NamespaceDefault, "alpha3", []string{"192.168.10.3"})
	ing3 := newIngressBuilder(metav1.NamespaceDefault, "alpha3").
		WithRule("/", bs3.Name, serviceBackendPortNumber(bs3.Spec.Ports[0].Port)).
		WithAnnotations(map[string]string{
			pathConfigKey: `alpha3.default.test/:
  affinity: cookie
  affinityCookieName: "foo"
`}).
		Complete()

	f.svcStore = append(f.svcStore, svc, bs1, bs2, bs3)
	f.epStore = append(f.epStore, eps, be1, be2, be3)
	f.ingStore = append(f.ingStore, ing1, ing2, ing3)

	f.objects = append(f.objects, svc, bs1, bs2, bs3, eps, be1, be2, be3, ing1, ing2, ing3)

	f.prepare()
	f.run()

	flb := f.lbc.nghttpx.(*fakeLoadBalancer)
	ingConfig := flb.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	if got, want := ingConfig.Upstreams[0].Name, f.lbc.defaultSvc.String(); got != want {
		t.Errorf("ingConfig.Upstreams[0].Name = %v, want %v", got, want)
	}

	upstream := ingConfig.Upstreams[1]

	if got, want := upstream.Ingress, (types.NamespacedName{Name: ing3.Name, Namespace: ing3.Namespace}); got != want {
		t.Errorf("upstream.Ingress = %v, want %v", got, want)
	}
}
