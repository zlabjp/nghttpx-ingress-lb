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
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	networking "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

type fixture struct {
	t *testing.T

	clientset *fake.Clientset

	lbc *LoadBalancerController

	ingStore      []*networking.Ingress
	ingClassStore []*networking.IngressClass
	epStore       []*v1.Endpoints
	svcStore      []*v1.Service
	secretStore   []*v1.Secret
	cmStore       []*v1.ConfigMap
	podStore      []*v1.Pod
	nodeStore     []*v1.Node
	epSliceStore  []*discovery.EndpointSlice

	objects []runtime.Object

	actions []core.Action

	enableEndpointSlice bool
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
)

// prepare performs setup necessary for test run.
func (f *fixture) prepare() {
	f.clientset = fake.NewSimpleClientset(f.objects...)
	config := Config{
		DefaultBackendService:  types.NamespacedName{Namespace: defaultBackendNamespace, Name: defaultBackendName},
		WatchNamespace:         defaultIngNamespace,
		NghttpxConfigMap:       &types.NamespacedName{Namespace: defaultConfigMapNamespace, Name: defaultConfigMapName},
		NghttpxConfDir:         defaultConfDir,
		IngressClassController: defaultIngressClassController,
		EnableEndpointSlice:    f.enableEndpointSlice,
		ReloadRate:             1.0,
		ReloadBurst:            1,
		PodInfo:                defaultRuntimeInfo,
	}
	f.lbc = NewLoadBalancerController(f.clientset, newFakeManager(), config)
}

func (f *fixture) run() {
	f.setupStore()

	if err := f.lbc.sync(syncKey); err != nil {
		f.t.Errorf("Failed to sync: %v", err)
	}

	f.verifyActions()
}

func (f *fixture) runShouldFail() {
	f.setupStore()

	if err := f.lbc.sync(syncKey); err == nil {
		f.t.Errorf("sync should fail")
	}

	f.verifyActions()
}

func (f *fixture) setupStore() {
	for _, ing := range f.ingStore {
		if err := f.lbc.ingInformer.GetIndexer().Add(ing); err != nil {
			panic(err)
		}
	}
	for _, ingClass := range f.ingClassStore {
		if err := f.lbc.ingClassInformer.GetIndexer().Add(ingClass); err != nil {
			panic(err)
		}
	}
	if f.enableEndpointSlice {
		for _, es := range f.epSliceStore {
			if err := f.lbc.epSliceInformer.GetIndexer().Add(es); err != nil {
				panic(err)
			}
		}
	} else {
		for _, ep := range f.epStore {
			if err := f.lbc.epInformer.GetIndexer().Add(ep); err != nil {
				panic(err)
			}
		}
	}
	for _, svc := range f.svcStore {
		if err := f.lbc.svcInformer.GetIndexer().Add(svc); err != nil {
			panic(err)
		}
	}
	for _, secret := range f.secretStore {
		if err := f.lbc.secretInformer.GetIndexer().Add(secret); err != nil {
			panic(err)
		}
	}
	for _, cm := range f.cmStore {
		if err := f.lbc.cmInformer.GetIndexer().Add(cm); err != nil {
			panic(err)
		}
	}
	for _, pod := range f.podStore {
		if err := f.lbc.podInformer.GetIndexer().Add(pod); err != nil {
			panic(err)
		}
	}
	for _, node := range f.nodeStore {
		if err := f.lbc.nodeInformer.GetIndexer().Add(node); err != nil {
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
func (f *fixture) expectUpdateIngAction(ing *networking.Ingress) {
	f.actions = append(f.actions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "ingresses"}, ing.Namespace, ing))
}

// newFakeManager implements nghttpx.Interface.
type fakeManager struct {
	checkAndReloadHandler func(ingConfig *nghttpx.IngressConfig) (bool, error)

	ingConfig *nghttpx.IngressConfig
}

// newFakeManager creates new fakeManager.
func newFakeManager() *fakeManager {
	fm := &fakeManager{}
	fm.checkAndReloadHandler = fm.defaultCheckAndReload
	return fm
}

func (fm *fakeManager) Start(ctx context.Context, path, confPath string) {}

func (fm *fakeManager) CheckAndReload(ingConfig *nghttpx.IngressConfig) (bool, error) {
	return fm.checkAndReloadHandler(ingConfig)
}

func (fm *fakeManager) defaultCheckAndReload(ingConfig *nghttpx.IngressConfig) (bool, error) {
	fm.ingConfig = ingConfig
	return true, nil
}

func stringPtr(s string) *string {
	return &s
}

func int32Ptr(n int32) *int32 {
	return &n
}

// newEmptyConfigMap returns empty ConfigMap.
func newEmptyConfigMap() *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultConfigMapName,
			Namespace: defaultConfigMapNamespace,
		},
		Data: make(map[string]string),
	}
}

// newDefaultBackend returns Service and Endpoints for default backend.
func newDefaultBackend() (*v1.Service, *v1.Endpoints, []*discovery.EndpointSlice) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:       8081,
					TargetPort: intstr.FromInt(8080),
					Protocol:   v1.ProtocolTCP,
				},
			},
		},
	}
	eps := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP: "192.168.100.1",
						TargetRef: &v1.ObjectReference{
							Kind:      "Pod",
							Name:      defaultBackendName + "-pod-1",
							Namespace: defaultBackendNamespace,
						},
					},
					{
						IP: "192.168.100.2",
						TargetRef: &v1.ObjectReference{
							Kind:      "Pod",
							Name:      defaultBackendName + "-pod-2",
							Namespace: defaultBackendNamespace,
						},
					},
				},
				Ports: []v1.EndpointPort{
					{
						Protocol: v1.ProtocolTCP,
						Port:     8081,
					},
					{
						Protocol: v1.ProtocolTCP,
						Port:     8080,
					},
				},
			},
		},
	}

	ess := []*discovery.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaultBackendName + "-a",
				Namespace: defaultBackendNamespace,
				Labels: map[string]string{
					discovery.LabelServiceName: defaultBackendName,
				},
			},
			AddressType: "IPv4",
			Ports: []discovery.EndpointPort{
				{
					Port: int32Ptr(8081),
				},
				{
					Port: int32Ptr(8080),
				},
			},
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{
						"192.168.100.1",
					},
					TargetRef: &v1.ObjectReference{
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
					discovery.LabelServiceName: defaultBackendName,
				},
			},
			AddressType: "IPv4",
			Ports: []discovery.EndpointPort{
				{
					Port: int32Ptr(8081),
				},
				{
					Port: int32Ptr(8080),
				},
			},
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{
						"192.168.100.2",
					},
					TargetRef: &v1.ObjectReference{
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
			Ports: []discovery.EndpointPort{
				{
					Port: int32Ptr(8081),
				},
				{
					Port: int32Ptr(8080),
				},
			},
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{
						"192.168.100.3",
					},
				},
				{
					Addresses: []string{
						"192.168.100.4",
					},
					TargetRef: &v1.ObjectReference{
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

func newBackend(namespace, name string, addrs []string) (*v1.Service, *v1.Endpoints, *discovery.EndpointSlice) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Port:       81,
					TargetPort: intstr.FromInt(80),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"k8s-app": "test",
			},
		},
	}
	eps := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Ports: []v1.EndpointPort{
					{
						Protocol: v1.ProtocolTCP,
						Port:     81,
					},
					{
						Protocol: v1.ProtocolTCP,
						Port:     80,
					},
				},
			},
		},
	}

	endpointAddrs := make([]v1.EndpointAddress, len(addrs))
	for i, addr := range addrs {
		endpointAddrs[i] = v1.EndpointAddress{
			IP: addr,
			TargetRef: &v1.ObjectReference{
				Kind:      "Pod",
				Name:      fmt.Sprintf("%v-pod-%v", name, i+1),
				Namespace: namespace,
			},
		}
	}

	eps.Subsets[0].Addresses = endpointAddrs

	proto := v1.ProtocolTCP

	es := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-aaaa",
			Namespace: namespace,
			Labels: map[string]string{
				discovery.LabelServiceName: name,
			},
		},
		AddressType: "IPv4",
		Ports: []discovery.EndpointPort{
			{
				Protocol: &proto,
				Port:     int32Ptr(81),
			},
			{
				Protocol: &proto,
				Port:     int32Ptr(80),
			},
		},
	}

	for i, addr := range addrs {
		es.Endpoints = append(es.Endpoints, discovery.Endpoint{
			Addresses: []string{addr},
			TargetRef: &v1.ObjectReference{
				Kind:      "Pod",
				Name:      fmt.Sprintf("%v-pod-%v", name, i+1),
				Namespace: namespace,
			},
		})
	}

	return svc, eps, es
}

func newIngressTLS(namespace, name, svcName string, svcPort networking.ServiceBackendPort, tlsSecretName string) *networking.Ingress {
	ing := newIngress(namespace, name, svcName, svcPort)
	ing.Spec.TLS = []networking.IngressTLS{
		{SecretName: tlsSecretName},
	}
	return ing
}

func serviceBackendPortNumber(port int32) networking.ServiceBackendPort {
	return networking.ServiceBackendPort{
		Number: port,
	}
}

func serviceBackendPortName(name string) networking.ServiceBackendPort {
	return networking.ServiceBackendPort{
		Name: name,
	}
}

func newIngress(namespace, name, svcName string, svcPort networking.ServiceBackendPort) *networking.Ingress {
	return &networking.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networking.IngressSpec{
			Rules: []networking.IngressRule{
				{
					Host: fmt.Sprintf("%v.%v.test", name, namespace),
					IngressRuleValue: networking.IngressRuleValue{
						HTTP: &networking.HTTPIngressRuleValue{
							Paths: []networking.HTTPIngressPath{
								{
									Path: "/",
									Backend: networking.IngressBackend{
										Service: &networking.IngressServiceBackend{
											Name: svcName,
											Port: svcPort,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newTLSSecret(namespace, name string, tlsCrt, tlsKey []byte) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			v1.TLSCertKey:       tlsCrt,
			v1.TLSPrivateKeyKey: tlsKey,
		},
	}
}

// TestSyncDefaultBackend verifies that controller creates configuration for default service backend.
func TestSyncDefaultBackend(t *testing.T) {
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

			cm := newEmptyConfigMap()
			cm.Data[nghttpx.NghttpxExtraConfigKey] = "Test"
			const mrubyContent = "mruby"
			cm.Data[nghttpx.NghttpxMrubyFileContentKey] = mrubyContent
			svc, eps, ess := newDefaultBackend()

			f.cmStore = append(f.cmStore, cm)
			f.svcStore = append(f.svcStore, svc)
			f.epStore = append(f.epStore, eps)
			f.epSliceStore = append(f.epSliceStore, ess...)

			f.objects = append(f.objects, cm, svc, eps)

			f.prepare()
			f.run()

			fm := f.lbc.nghttpx.(*fakeManager)
			ingConfig := fm.ingConfig

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
			}

			if got, want := fm.ingConfig.ExtraConfig, cm.Data[nghttpx.NghttpxExtraConfigKey]; got != want {
				t.Errorf("fm.cfg.ExtraConfig = %v, want %v", got, want)
			}
			if got, want := fm.ingConfig.MrubyFile, (&nghttpx.ChecksumFile{
				Path:     nghttpx.MrubyRbPath(defaultConfDir),
				Content:  []byte(mrubyContent),
				Checksum: nghttpx.Checksum([]byte(mrubyContent)),
			}); !reflect.DeepEqual(got, want) {
				t.Errorf("fm.ingConfig.MrubyFile = %q, want %q", got, want)
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

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := ingConfig.TLS, true; got != want {
		t.Errorf("ingConfig.TLS = %v, want %v", got, want)
	}

	prefix := nghttpx.TLSCredPrefix(tlsSecret)
	if got, want := ingConfig.DefaultTLSCred.Key.Path, nghttpx.CreateTLSKeyPath(defaultConfDir, prefix); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Key.Path = %v, want %v", got, want)
	}
	if got, want := ingConfig.DefaultTLSCred.Cert.Path, nghttpx.CreateTLSCertPath(defaultConfDir, prefix); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Cert.Path = %v, want %v", got, want)
	}
	if got, want := ingConfig.DefaultTLSCred.Key.Checksum, nghttpx.Checksum(dKey); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Key.Checksum = %v, want %v", got, want)
	}
	if got, want := ingConfig.DefaultTLSCred.Cert.Checksum, nghttpx.Checksum(dCrt); got != want {
		t.Errorf("ingConfig.DefaultTLSCred.Cert.Checksum = %v, want %v", got, want)
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
	ing1 := newIngressTLS(metav1.NamespaceDefault, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port), tlsSecret.Name)

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

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := ingConfig.TLS, true; got != want {
		t.Errorf("ingConfig.TLS = %v, want %v", got, want)
	}

	prefix := nghttpx.TLSCredPrefix(tlsSecret)
	if got, want := ingConfig.DefaultTLSCred.Key.Path, nghttpx.CreateTLSKeyPath(defaultConfDir, prefix); got != want {
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
	ing1 := newIngressTLS(metav1.NamespaceDefault, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port), tlsSecret.Name)

	f.secretStore = append(f.secretStore, tlsSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)

	f.objects = append(f.objects, tlsSecret, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run()

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

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

	upstream := ingConfig.Upstreams[0]
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
			bs1.Spec.Ports[0] = v1.ServicePort{
				Port:       1234,
				TargetPort: intstr.FromString("my-port"),
				Protocol:   v1.ProtocolTCP,
			}
			ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port))

			bp1 := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alpha-pod-1",
					Namespace: bs1.Namespace,
					Labels:    bs1.Spec.Selector,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Ports: []v1.ContainerPort{
								{
									Name:          "my-port",
									ContainerPort: 80,
									Protocol:      v1.ProtocolTCP,
								},
							},
						},
					},
				},
			}

			bp2 := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alpha-pod-2",
					Namespace: bs1.Namespace,
					Labels:    bs1.Spec.Selector,
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Ports: []v1.ContainerPort{
								{
									Name:          "my-port",
									ContainerPort: 81,
									Protocol:      v1.ProtocolTCP,
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

			fm := f.lbc.nghttpx.(*fakeManager)
			ingConfig := fm.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			if got, want := len(ingConfig.Upstreams[0].Backends), 2; got != want {
				t.Errorf("len(ingConfig.Upstreams[0].Backends) = %v, want %v", got, want)
			} else {
				for i, port := range []string{"80", "81"} {
					backend := ingConfig.Upstreams[0].Backends[i]
					if got, want := backend.Port, port; got != want {
						t.Errorf("backends[i].Port = %v, want %v", got, want)
					}
				}
			}
		})
	}
}

// TestSyncNumericTargetPort verifies that if target port is numeric, it is compared to endpoint port directly.
func TestSyncNumericTargetPort(t *testing.T) {
	tests := []struct {
		desc                string
		enableEndpointSlice bool
	}{
		{
			desc: "With Endpoint",
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

			bs1, be1, bes1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
			bs1.Spec.Ports[0] = v1.ServicePort{
				Port:       8080,
				TargetPort: intstr.FromString("80"),
				Protocol:   v1.ProtocolTCP,
			}
			ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port))

			f.svcStore = append(f.svcStore, svc, bs1)
			f.epStore = append(f.epStore, eps, be1)
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.ingStore = append(f.ingStore, ing1)

			f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

			f.prepare()
			f.run()

			fm := f.lbc.nghttpx.(*fakeManager)
			ingConfig := fm.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			backend := ingConfig.Upstreams[0].Backends[0]
			if got, want := backend.Port, "80"; got != want {
				t.Errorf("backend.Port = %v, want %v", got, want)
			}
		})
	}
}

// TestSyncEmptyTargetPort verifies that if target port is empty, port is used instead.  In practice, target port is always filled out.
func TestSyncEmptyTargetPort(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = v1.ServicePort{
		Port:       80,
		TargetPort: intstr.FromString(""),
		Protocol:   v1.ProtocolTCP,
	}
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port))

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run()

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	backend := ingConfig.Upstreams[0].Backends[0]
	if got, want := backend.Port, "80"; got != want {
		t.Errorf("backend.Port = %v, want %v", got, want)
	}
}

// TestValidateIngressClass verifies validateIngressClass.
func TestValidateIngressClass(t *testing.T) {
	tests := []struct {
		desc     string
		ing      networking.Ingress
		ingClass *networking.IngressClass
		want     bool
	}{
		{
			desc: "no IngressClass",
			ing: networking.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			want: true,
		},
		{
			desc: "IngressClass targets this controller",
			ing: networking.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: networking.IngressSpec{
					IngressClassName: stringPtr("bar"),
				},
			},
			ingClass: &networking.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
				Spec: networking.IngressClassSpec{
					Controller: defaultIngressClassController,
				},
			},
			want: true,
		},
		{
			desc: "IngressClass does not target this controller",
			ing: networking.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: networking.IngressSpec{
					IngressClassName: stringPtr("bar"),
				},
			},
			ingClass: &networking.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
				},
				Spec: networking.IngressClassSpec{
					Controller: "example.com/ingress",
				},
			},
		},
		{
			desc: "The specified IngressClass is not found",
			ing: networking.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: networking.IngressSpec{
					IngressClassName: stringPtr("bar"),
				},
			},
		},
		{
			desc: "IngressClass which targets this controller is marked default",
			ing: networking.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			ingClass: &networking.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
					Annotations: map[string]string{
						annotationIsDefaultIngressClass: "true",
					},
				},
				Spec: networking.IngressClassSpec{
					Controller: defaultIngressClassController,
				},
			},
			want: true,
		},
		{
			desc: "IngressClass which does not target this controller is marked default",
			ing: networking.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
			},
			ingClass: &networking.IngressClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "bar",
					Annotations: map[string]string{
						annotationIsDefaultIngressClass: "true",
					},
				},
				Spec: networking.IngressClassSpec{
					Controller: "example.com/ingress",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			f.ingStore = append(f.ingStore, &tt.ing)
			if tt.ingClass != nil {
				f.ingClassStore = append(f.ingClassStore, tt.ingClass)
			}

			f.prepare()
			f.setupStore()

			if got, want := f.lbc.validateIngressClass(&tt.ing), tt.want; got != want {
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
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port))
	ing1.Spec.DefaultBackend = &networking.IngressBackend{
		Service: &networking.IngressServiceBackend{
			Name: "bravo",
			Port: serviceBackendPortNumber(bs2.Spec.Ports[0].Port),
		},
	}

	f.svcStore = append(f.svcStore, svc, bs1, bs2)
	f.epStore = append(f.epStore, eps, be1, be2)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1, bs2, be2)

	f.prepare()
	f.run()

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	var found bool
	for _, upstream := range ingConfig.Upstreams {
		if upstream.Name == "default/bravo,81;/" {
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
		ing      *networking.Ingress
		wantName string
	}{
		{
			desc: ".Spec.DefaultBackend must be ignored",
			ing: func() *networking.Ingress {
				ing := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port))
				ing.Spec.DefaultBackend = &networking.IngressBackend{
					Service: &networking.IngressServiceBackend{
						Name: bs2.Name,
						Port: serviceBackendPortNumber(bs2.Spec.Ports[0].Port),
					},
				}
				return ing
			}(),
		},
		{
			desc: "Any rules which override default backend must be ignored",
			ing: func() *networking.Ingress {
				ing := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortNumber(bs1.Spec.Ports[0].Port))
				ing.Spec.Rules = append(ing.Spec.Rules,
					networking.IngressRule{
						IngressRuleValue: networking.IngressRuleValue{
							HTTP: &networking.HTTPIngressRuleValue{
								Paths: []networking.HTTPIngressPath{
									{
										Path: "/",
										Backend: networking.IngressBackend{
											Service: &networking.IngressServiceBackend{
												Name: bs2.Name,
												Port: serviceBackendPortNumber(bs2.Spec.Ports[0].Port),
											},
										},
									},
								},
							},
						},
					},
				)
				return ing
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)

			svc, eps, ess := newDefaultBackend()

			f.svcStore = append(f.svcStore, svc, bs1.DeepCopyObject().(*v1.Service), bs2.DeepCopyObject().(*v1.Service))
			f.epStore = append(f.epStore, eps, be1.DeepCopyObject().(*v1.Endpoints), be2.DeepCopyObject().(*v1.Endpoints))
			f.epSliceStore = append(f.epSliceStore, ess...)
			f.ingStore = append(f.ingStore, tt.ing)

			f.prepare()
			f.lbc.noDefaultBackendOverride = true
			f.run()

			fm := f.lbc.nghttpx.(*fakeManager)
			ingConfig := fm.ingConfig

			if got, want := len(ingConfig.Upstreams), 2; got != want {
				t.Fatalf("len(ingConfig.Upstreams) = %v, want %v", got, want)
			}

			if got, want := ingConfig.Upstreams[1].Name, f.lbc.defaultSvc.String(); got != want {
				t.Errorf("ingConfig.Upstreams[1].Name = %v, want %v", got, want)
			}
		})
	}
}

// newIngPod creates Ingress controller pod.
func newIngPod(name, nodeName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultRuntimeInfo.Namespace,
			Labels:    defaultIngPodLables,
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Containers: []v1.Container{
				{
					Ports: []v1.ContainerPort{
						{
							Name:          "my-port",
							ContainerPort: 80,
							Protocol:      v1.ProtocolTCP,
						},
					},
				},
			},
		},
	}
}

// newNode creates new Node.
func newNode(name string, addrs ...v1.NodeAddress) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: v1.NodeStatus{
			Addresses: addrs,
		},
	}
}

// TestGetLoadBalancerIngress verifies that it collects node IPs from cache.
func TestGetLoadBalancerIngress(t *testing.T) {
	f := newFixture(t)

	po1 := newIngPod(defaultRuntimeInfo.Name, "alpha.test")
	node1 := newNode("alpha.test", v1.NodeAddress{Type: v1.NodeExternalIP, Address: "192.168.0.1"})

	po2 := newIngPod("bravo", "bravo.test")
	node2 := newNode("bravo.test", v1.NodeAddress{Type: v1.NodeInternalIP, Address: "10.0.0.1"}, v1.NodeAddress{Type: v1.NodeExternalIP, Address: "192.168.0.2"})

	f.podStore = append(f.podStore, po1, po2)
	f.nodeStore = append(f.nodeStore, node1, node2)

	f.objects = append(f.objects, po1, po2, node1, node2)

	f.prepare()
	f.setupStore()

	lbIngs, err := f.lbc.getLoadBalancerIngress(labels.Set(defaultIngPodLables).AsSelector())

	f.verifyActions()

	if err != nil {
		t.Fatalf("f.lbc.getLoadBalancerIngress() returned unexpected error %v", err)
	}

	if got, want := len(lbIngs), 2; got != want {
		t.Errorf("len(lbIngs) = %v, want %v", got, want)
	}

	sortLoadBalancerIngress(lbIngs)

	ans := []v1.LoadBalancerIngress{
		{IP: "192.168.0.1"}, {IP: "192.168.0.2"},
	}

	if got, want := lbIngs, ans; !reflect.DeepEqual(got, want) {
		t.Errorf("lbIngs = %+v, want %+v", got, want)
	}
}

// TestUpdateIngressStatus verifies that Ingress resources are updated with the given lbIngs.
func TestUpdateIngressStatus(t *testing.T) {
	f := newFixture(t)

	lbIngs := []v1.LoadBalancerIngress{{IP: "192.168.0.1"}, {IP: "192.168.0.2"}}

	ing1 := newIngress(metav1.NamespaceDefault, "delta-ing", "delta", serviceBackendPortNumber(80))
	ing3 := newIngress(metav1.NamespaceDefault, "foxtrot-ing", "foxtrot", serviceBackendPortNumber(80))
	ing3.Spec.IngressClassName = stringPtr("not-nghttpx")
	ing3.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: "192.168.0.100"}, {IP: "192.168.0.101"}}
	ing4 := newIngress(metav1.NamespaceDefault, "golf-ing", "golf", serviceBackendPortNumber(80))
	ing4.Status.LoadBalancer.Ingress = lbIngs
	ing2 := newIngress(metav1.NamespaceDefault, "echo-ing", "echo", serviceBackendPortNumber(80))

	f.ingStore = append(f.ingStore, ing1, ing2, ing3, ing4)

	f.objects = append(f.objects, ing1, ing2, ing3, ing4)

	f.expectUpdateIngAction(ing1)
	f.expectUpdateIngAction(ing2)

	f.prepare()
	f.setupStore()

	err := f.lbc.updateIngressStatus(context.Background(), lbIngs)

	f.verifyActions()

	if err != nil {
		t.Fatalf("f.lbc.updateIngressStatus(lbIngs) returned unexpected error %v", err)
	}

	if updatedIng, err := f.clientset.NetworkingV1().Ingresses(ing1.Namespace).Get(context.TODO(), ing1.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing1.Namespace, ing1.Name, err)
	} else if got, want := updatedIng.Status.LoadBalancer.Ingress, lbIngs; !reflect.DeepEqual(got, want) {
		t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
	}
	if updatedIng, err := f.clientset.NetworkingV1().Ingresses(ing2.Namespace).Get(context.TODO(), ing2.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing2.Namespace, ing2.Name, err)
	} else if got, want := updatedIng.Status.LoadBalancer.Ingress, lbIngs; !reflect.DeepEqual(got, want) {
		t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
	}
	if updatedIng, err := f.clientset.NetworkingV1().Ingresses(ing3.Namespace).Get(context.TODO(), ing3.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing2.Namespace, ing2.Name, err)
	} else {
		if got, want := updatedIng.Status.LoadBalancer.Ingress, ing3.Status.LoadBalancer.Ingress; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}
}

// TestRemoveAddressFromLoadBalancerIngress verifies that removeAddressFromLoadBalancerIngress clears Ingress.Status.LoadBalancer.Ingress.
func TestRemoveAddressFromLoadBalancerIngress(t *testing.T) {
	f := newFixture(t)

	po := newIngPod(defaultRuntimeInfo.Name, "alpha.test")
	node := newNode("alpha.test", v1.NodeAddress{Type: v1.NodeExternalIP, Address: "192.168.0.1"})

	lbIngs := []v1.LoadBalancerIngress{{IP: "192.168.0.1"}, {IP: "192.168.0.2"}}

	ing1 := newIngress(metav1.NamespaceDefault, "delta-ing", "delta", serviceBackendPortNumber(80))
	ing1.Status.LoadBalancer.Ingress = lbIngs

	ing2 := newIngress(metav1.NamespaceDefault, "echo-ing", "echo", serviceBackendPortNumber(80))
	ing2.Status.LoadBalancer.Ingress = lbIngs

	ing3 := newIngress(metav1.NamespaceDefault, "foxtrot-ing", "foxtrot", serviceBackendPortNumber(80))
	ing3.Spec.IngressClassName = stringPtr("not-nghttpx")
	ing3.Status.LoadBalancer.Ingress = lbIngs

	ing4 := newIngress(metav1.NamespaceDefault, "golf-ing", "golf", serviceBackendPortNumber(80))
	ing4.Status.LoadBalancer.Ingress = lbIngs[1:]

	f.podStore = append(f.podStore, po)
	f.nodeStore = append(f.nodeStore, node)
	f.ingStore = append(f.ingStore, ing1, ing2, ing3, ing4)

	f.objects = append(f.objects, po, node, ing1, ing2, ing3, ing4)

	f.prepare()
	f.setupStore()

	err := f.lbc.removeAddressFromLoadBalancerIngress()

	if err != nil {
		t.Fatalf("f.lbc.removeAddressFromLoadBalancerIngress() returned unexpected error %v", err)
	}

	if updatedIng, err := f.lbc.clientset.NetworkingV1().Ingresses(ing1.Namespace).Get(context.TODO(), ing1.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing1.Namespace, ing1.Name, err)
	} else {
		ans := []v1.LoadBalancerIngress{{IP: "192.168.0.2"}}
		if got, want := updatedIng.Status.LoadBalancer.Ingress, ans; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}

	if updatedIng, err := f.lbc.clientset.NetworkingV1().Ingresses(ing4.Namespace).Get(context.TODO(), ing4.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing4.Namespace, ing4.Name, err)
	} else {
		ans := []v1.LoadBalancerIngress{{IP: "192.168.0.2"}}
		if got, want := updatedIng.Status.LoadBalancer.Ingress, ans; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}

	if updatedIng, err := f.lbc.clientset.NetworkingV1().Ingresses(ing3.Namespace).Get(context.TODO(), ing3.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing3.Namespace, ing3.Name, err)
	} else {
		if got, want := updatedIng.Status.LoadBalancer.Ingress, ing3.Status.LoadBalancer.Ingress; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}
}

// TestGetLoadBalancerIngressFromService verifies getLoadBalancerIngressFromService.
func TestGetLoadBalancerIngressFromService(t *testing.T) {
	f := newFixture(t)

	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			ExternalIPs: []string{
				"192.168.0.2",
				"192.168.0.1",
				"192.168.0.3",
			},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
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

	want := []v1.LoadBalancerIngress{
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

	f.prepare()
	f.lbc.publishSvc = &types.NamespacedName{
		Namespace: "alpha",
		Name:      "bravo",
	}

	got := f.lbc.getLoadBalancerIngressFromService(svc)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("f.lbc.getLoadBalancerIngressFromService(...) = %#v, want %#v", got, want)
	}
}

// TestSyncNamedServicePort verifies that if a named service port is given in Ingress, a service port is looked up by the name.
func TestSyncNamedServicePort(t *testing.T) {
	f := newFixture(t)

	svc, eps, _ := newDefaultBackend()

	bs1, be1, _ := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = v1.ServicePort{
		Name:     "namedport",
		Port:     80,
		Protocol: v1.ProtocolTCP,
	}
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, serviceBackendPortName(bs1.Spec.Ports[0].Name))

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run()

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	backend := ingConfig.Upstreams[0].Backends[0]
	if got, want := backend.Port, "80"; got != want {
		t.Errorf("backend.Port = %v, want %v", got, want)
	}
}
