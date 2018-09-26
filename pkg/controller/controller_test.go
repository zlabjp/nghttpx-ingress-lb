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
	"encoding/base64"
	"fmt"
	"reflect"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

type fixture struct {
	t *testing.T

	clientset *fake.Clientset

	lbc *LoadBalancerController

	ingStore    []*extensions.Ingress
	epStore     []*v1.Endpoints
	svcStore    []*v1.Service
	secretStore []*v1.Secret
	cmStore     []*v1.ConfigMap
	podStore    []*v1.Pod
	nodeStore   []*v1.Node

	objects []runtime.Object

	actions []core.Action
}

func newFixture(t *testing.T) *fixture {
	return &fixture{
		t:       t,
		objects: []runtime.Object{},
	}
}

const (
	defaultResyncPeriod       = 30 * time.Second
	defaultBackendName        = "default-http-backend"
	defaultBackendNamespace   = "kube-system"
	defaultIngNamespace       = metav1.NamespaceAll
	defaultConfigMapName      = "ing-config"
	defaultConfigMapNamespace = "kube-system"
	defaultIngressClass       = "nghttpx"
	defaultConfDir            = "conf"

	// openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=echoheaders/O=echoheaders"
	tlsCrt = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURhakNDQWxLZ0F3SUJBZ0lKQUxHUXR5VVBKTFhYTUEwR0NTcUdTSWIzRFFFQkJRVUFNQ3d4RkRBU0JnTlYKQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3RWdZRFZRUUtFd3RsWTJodmFHVmhaR1Z5Y3pBZUZ3MHhOakF6TXpFeQpNekU1TkRoYUZ3MHhOekF6TXpFeU16RTVORGhhTUN3eEZEQVNCZ05WQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3CkVnWURWUVFLRXd0bFkyaHZhR1ZoWkdWeWN6Q0NBU0l3RFFZSktvWklodmNOQVFFQkJRQURnZ0VQQURDQ0FRb0MKZ2dFQkFONzVmS0N5RWwxanFpMjUxTlNabDYzeGQweG5HMHZTVjdYL0xxTHJveVNraW5nbnI0NDZZWlE4UEJWOAo5TUZzdW5RRGt1QVoyZzA3NHM1YWhLSm9BRGJOMzhld053RXNsVDJkRzhRTUw0TktrTUNxL1hWbzRQMDFlWG1PCmkxR2txZFA1ZUExUHlPZCtHM3gzZmxPN2xOdmtJdHVHYXFyc0tvMEhtMHhqTDVtRUpwWUlOa0tGSVhsWWVLZS8KeHRDR25CU2tLVHFMTG0yeExKSGFFcnJpaDZRdkx4NXF5U2gzZTU2QVpEcTlkTERvcWdmVHV3Z2IzekhQekc2NwppZ0E0dkYrc2FRNHpZUE1NMHQyU1NiVkx1M2pScWNvL3lxZysrOVJBTTV4bjRubnorL0hUWFhHKzZ0RDBaeGI1CmVVRDNQakVhTnlXaUV2dTN6UFJmdysyNURMY0NBd0VBQWFPQmpqQ0JpekFkQmdOVkhRNEVGZ1FVcktMZFhHeUUKNUlEOGRvd2lZNkdzK3dNMHFKc3dYQVlEVlIwakJGVXdVNEFVcktMZFhHeUU1SUQ4ZG93aVk2R3Mrd00wcUp1aApNS1F1TUN3eEZEQVNCZ05WQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3RWdZRFZRUUtFd3RsWTJodmFHVmhaR1Z5CmM0SUpBTEdRdHlVUEpMWFhNQXdHQTFVZEV3UUZNQU1CQWY4d0RRWUpLb1pJaHZjTkFRRUZCUUFEZ2dFQkFNZVMKMHFia3VZa3Z1enlSWmtBeE1PdUFaSDJCK0Evb3N4ODhFRHB1ckV0ZWN5RXVxdnRvMmpCSVdCZ2RkR3VBYU5jVQorUUZDRm9NakJOUDVWVUxIWVhTQ3VaczN2Y25WRDU4N3NHNlBaLzhzbXJuYUhTUjg1ZVpZVS80bmFyNUErdWErClIvMHJrSkZnOTlQSmNJd3JmcWlYOHdRcWdJVVlLNE9nWEJZcUJRL0VZS2YvdXl6UFN3UVZYRnVJTTZTeDBXcTYKTUNML3d2RlhLS0FaWDBqb3J4cHRjcldkUXNCcmYzWVRnYmx4TE1sN20zL2VuR1drcEhDUHdYeVRCOC9rRkw3SApLL2ZHTU1NWGswUkVSbGFPM1hTSUhrZUQ2SXJiRnRNV3R1RlJwZms2ZFA2TXlMOHRmTmZ6a3VvUHVEWUFaWllWCnR1NnZ0c0FRS0xWb0pGaGV0b1k9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K"
	tlsKey = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBM3ZsOG9MSVNYV09xTGJuVTFKbVhyZkYzVEdjYlM5Slh0Zjh1b3V1akpLU0tlQ2V2CmpqcGhsRHc4Rlh6MHdXeTZkQU9TNEJuYURUdml6bHFFb21nQU5zM2Z4N0EzQVN5VlBaMGJ4QXd2ZzBxUXdLcjkKZFdqZy9UVjVlWTZMVWFTcDAvbDREVS9JNTM0YmZIZCtVN3VVMitRaTI0WnFxdXdxalFlYlRHTXZtWVFtbGdnMgpRb1VoZVZoNHA3L0cwSWFjRktRcE9vc3ViYkVza2RvU3V1S0hwQzh2SG1ySktIZDdub0JrT3IxMHNPaXFCOU83CkNCdmZNYy9NYnJ1S0FEaThYNnhwRGpOZzh3elMzWkpKdFV1N2VOR3B5ai9LcUQ3NzFFQXpuR2ZpZWZQNzhkTmQKY2I3cTBQUm5Gdmw1UVBjK01SbzNKYUlTKzdmTTlGL0Q3YmtNdHdJREFRQUJBb0lCQUViNmFEL0hMNjFtMG45bgp6bVkyMWwvYW83MUFmU0h2dlZnRCtWYUhhQkY4QjFBa1lmQUdpWlZrYjBQdjJRSFJtTERoaWxtb0lROWhadHVGCldQOVIxKythTFlnbGdmenZzanBBenR2amZTUndFaEFpM2pnSHdNY1p4S2Q3UnNJZ2hxY2huS093S0NYNHNNczQKUnBCbEFBZlhZWGs4R3F4NkxUbGptSDRDZk42QzZHM1EwTTlLMUxBN2lsck1Na3hwcngxMnBlVTNkczZMVmNpOQptOFdBL21YZ2I0c3pEbVNaWVpYRmNZMEhYNTgyS3JKRHpQWEVJdGQwZk5wd3I0eFIybzdzMEwvK2RnZCtqWERjCkh2SDBKZ3NqODJJaTIxWGZGM2tST3FxR3BKNmhVcncxTUZzVWRyZ29GL3pFck0vNWZKMDdVNEhodGFlalVzWTIKMFJuNXdpRUNnWUVBKzVUTVRiV084Wkg5K2pIdVQwc0NhZFBYcW50WTZYdTZmYU04Tm5CZWNoeTFoWGdlQVN5agpSWERlZGFWM1c0SjU5eWxIQ3FoOVdseVh4cDVTWWtyQU41RnQ3elFGYi91YmorUFIyWWhMTWZpYlBSYlYvZW1MCm5YaGF6MmtlNUUxT1JLY0x6QUVwSmpuZGQwZlZMZjdmQzFHeStnS2YyK3hTY1hjMHJqRE5iNGtDZ1lFQTR1UVEKQk91TlJQS3FKcDZUZS9zUzZrZitHbEpjQSs3RmVOMVlxM0E2WEVZVm9ydXhnZXQ4a2E2ZEo1QjZDOWtITGtNcQpwdnFwMzkxeTN3YW5uWC9ONC9KQlU2M2RxZEcyd1BWRUQ0REduaE54Qm1oaWZpQ1I0R0c2ZnE4MUV6ZE1vcTZ4CklTNHA2RVJaQnZkb1RqNk9pTHl6aUJMckpxeUhIMWR6c0hGRlNqOENnWUVBOWlSSEgyQ2JVazU4SnVYak8wRXcKUTBvNG4xdS9TZkQ4TFNBZ01VTVBwS1hpRTR2S0Qyd1U4a1BUNDFiWXlIZUh6UUpkdDFmU0RTNjZjR0ZHU1ZUSgphNVNsOG5yN051ejg3bkwvUmMzTGhFQ3Y0YjBOOFRjbW1oSy9CbDdiRXBOd0dFczNoNGs3TVdNOEF4QU15c3VxCmZmQ1pJM0tkNVJYNk0zbGwyV2QyRjhFQ2dZQlQ5RU9oTG0vVmhWMUVjUVR0cVZlMGJQTXZWaTVLSGozZm5UZkUKS0FEUVIvYVZncElLR3RLN0xUdGxlbVpPbi8yeU5wUS91UnpHZ3pDUUtldzNzU1RFSmMzYVlzbFVudzdhazJhZAp2ZTdBYXowMU84YkdHTk1oamNmdVBIS05LN2Nsc3pKRHJzcys4SnRvb245c0JHWEZYdDJuaWlpTTVPWVN5TTg4CkNJMjFEUUtCZ0hEQVRZbE84UWlDVWFBQlVqOFBsb1BtMDhwa3cyc1VmQW0xMzJCY00wQk9BN1hqYjhtNm1ManQKOUlteU5kZ2ZiM080UjlKVUxTb1pZSTc1dUxIL3k2SDhQOVlpWHZOdzMrTXl6VFU2b2d1YU8xSTNya2pna29NeAo5cU5pYlJFeGswS1A5MVZkckVLSEdHZEFwT05ES1N4VzF3ektvbUxHdmtYSTVKV05KRXFkCi0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg=="
)

var (
	defaultRuntimeInfo = PodInfo{
		PodName:      "nghttpx-ingress-controller",
		PodNamespace: "kube-system",
	}

	defaultIngPodLables = map[string]string{
		"k8s-app": "ingress",
	}
)

// prepare performs setup necessary for test run.
func (f *fixture) prepare() {
	f.clientset = fake.NewSimpleClientset(f.objects...)
	config := Config{
		ResyncPeriod:          defaultResyncPeriod,
		DefaultBackendService: MetaNamespaceKey{Namespace: defaultBackendNamespace, Name: defaultBackendName},
		WatchNamespace:        defaultIngNamespace,
		NghttpxConfigMap:      fmt.Sprintf("%v/%v", defaultConfigMapNamespace, defaultConfigMapName),
		NghttpxConfDir:        defaultConfDir,
		IngressClass:          defaultIngressClass,
	}
	f.lbc = NewLoadBalancerController(f.clientset, newFakeManager(), &config, &defaultRuntimeInfo)
	f.lbc.controllersInSyncHandler = func() bool { return true }
}

func (f *fixture) run(ingKey string) {
	f.setupStore()

	if err := f.lbc.sync(ingKey); err != nil {
		f.t.Errorf("Failed to sync %v: %v", ingKey, err)
	}

	f.verifyActions()
}

func (f *fixture) runShouldFail(ingKey string) {
	f.setupStore()

	if err := f.lbc.sync(ingKey); err == nil {
		f.t.Errorf("sync should fail")
	}

	f.verifyActions()
}

func (f *fixture) setupStore() {
	for _, ing := range f.ingStore {
		f.lbc.ingLister.indexer.Add(ing)
	}
	for _, ep := range f.epStore {
		f.lbc.epLister.indexer.Add(ep)
	}
	for _, svc := range f.svcStore {
		f.lbc.svcLister.indexer.Add(svc)
	}
	for _, secret := range f.secretStore {
		f.lbc.secretLister.indexer.Add(secret)
	}
	for _, cm := range f.cmStore {
		f.lbc.cmLister.indexer.Add(cm)
	}
	for _, pod := range f.podStore {
		f.lbc.podLister.indexer.Add(pod)
	}
	for _, node := range f.nodeStore {
		f.lbc.nodeLister.indexer.Add(node)
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

// expectGetCMAction adds expectation that Get for cm should occur.
func (f *fixture) expectGetCMAction(cm *v1.ConfigMap) {
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "configmaps"}, cm.Namespace, cm.Name))
}

// expectGetIngAction adds an expectation that get for ing should occur.
func (f *fixture) expectGetIngAction(ing *extensions.Ingress) {
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "ingresses"}, ing.Namespace, ing.Name))
}

// expectUpdateIngAction adds an expectation that update for ing should occur.
func (f *fixture) expectUpdateIngAction(ing *extensions.Ingress) {
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

func (fm *fakeManager) Start(path, confPath string, stopCh <-chan struct{}) {}

func (fm *fakeManager) CheckAndReload(ingConfig *nghttpx.IngressConfig) (bool, error) {
	return fm.checkAndReloadHandler(ingConfig)
}

func (fm *fakeManager) defaultCheckAndReload(ingConfig *nghttpx.IngressConfig) (bool, error) {
	fm.ingConfig = ingConfig
	return true, nil
}

// keyPair contains certificate key, and cert, and their name.
type keyPair struct {
	name string
	cert []byte
	key  []byte
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
func newDefaultBackend() (*v1.Service, *v1.Endpoints) {
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
					{IP: "192.168.100.1"},
					{IP: "192.168.100.2"},
				},
				Ports: []v1.EndpointPort{
					{
						Protocol: v1.ProtocolTCP,
						Port:     8080,
					},
				},
			},
		},
	}

	return svc, eps
}

func newBackend(namespace, name string, addrs []string) (*v1.Service, *v1.Endpoints) {
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
						Port:     80,
					},
				},
			},
		},
	}

	var endpointAddrs []v1.EndpointAddress
	for _, addr := range addrs {
		endpointAddrs = append(endpointAddrs, v1.EndpointAddress{IP: addr})
	}

	eps.Subsets[0].Addresses = endpointAddrs

	return svc, eps
}

func newIngressTLS(namespace, name, svcName, svcPort, tlsSecretName string) *extensions.Ingress {
	ing := newIngress(namespace, name, svcName, svcPort)
	ing.Spec.TLS = []extensions.IngressTLS{
		{SecretName: tlsSecretName},
	}
	return ing
}

func newIngress(namespace, name, svcName, svcPort string) *extensions.Ingress {
	return &extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				ingressClassKey: defaultIngressClass,
			},
		},
		Spec: extensions.IngressSpec{
			Rules: []extensions.IngressRule{
				{
					Host: fmt.Sprintf("%v.%v.test", name, namespace),
					IngressRuleValue: extensions.IngressRuleValue{
						HTTP: &extensions.HTTPIngressRuleValue{
							Paths: []extensions.HTTPIngressPath{
								{
									Path: "/",
									Backend: extensions.IngressBackend{
										ServiceName: svcName,
										ServicePort: intstr.FromString(svcPort),
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

func getKey(obj runtime.Object, t *testing.T) string {
	if key, err := cache.MetaNamespaceKeyFunc(obj); err != nil {
		t.Fatalf("Could not get key for %+v: %v", obj, err)
		return ""
	} else {
		return key
	}
}

// TestSyncDefaultBackend verifies that controller creates configuration for default service backend.
func TestSyncDefaultBackend(t *testing.T) {
	f := newFixture(t)

	cm := newEmptyConfigMap()
	cm.Data[nghttpx.NghttpxExtraConfigKey] = "Test"
	const mrubyContent = "mruby"
	cm.Data[nghttpx.NghttpxMrubyFileContentKey] = mrubyContent
	svc, eps := newDefaultBackend()

	f.cmStore = append(f.cmStore, cm)
	f.svcStore = append(f.svcStore, svc)
	f.epStore = append(f.epStore, eps)

	f.objects = append(f.objects, cm, svc, eps)

	f.prepare()
	f.run(getKey(svc, t))

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
		Path:     nghttpx.NghttpxMrubyRbPath(defaultConfDir),
		Content:  []byte(mrubyContent),
		Checksum: nghttpx.Checksum([]byte(mrubyContent)),
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("fm.ingConfig.MrubyFile = %q, want %q", got, want)
	}
}

// TestSyncDefaultTLSSecretNotFound verifies that sync must fail if default TLS Secret is not found.
func TestSyncDefaultTLSSecretNotFound(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	f.svcStore = append(f.svcStore, svc)
	f.epStore = append(f.epStore, eps)

	f.objects = append(f.objects, svc, eps)

	f.prepare()
	f.lbc.defaultTLSSecret.Namespace = "kube-system"
	f.lbc.defaultTLSSecret.Name = "default-tls"
	f.runShouldFail(getKey(svc, t))
}

// TestSyncDefaultSecret verifies that default TLS secret is loaded.
func TestSyncDefaultSecret(t *testing.T) {
	f := newFixture(t)

	dCrt, _ := base64.StdEncoding.DecodeString(tlsCrt)
	dKey, _ := base64.StdEncoding.DecodeString(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	svc, eps := newDefaultBackend()

	f.secretStore = append(f.secretStore, tlsSecret)
	f.svcStore = append(f.svcStore, svc)
	f.epStore = append(f.epStore, eps)

	f.objects = append(f.objects, tlsSecret, svc, eps)

	f.prepare()
	f.lbc.defaultTLSSecret.Namespace = tlsSecret.Namespace
	f.lbc.defaultTLSSecret.Name = tlsSecret.Name
	f.run(getKey(svc, t))

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

	dCrt, _ := base64.StdEncoding.DecodeString(tlsCrt)
	dKey, _ := base64.StdEncoding.DecodeString(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	ing1 := newIngressTLS(metav1.NamespaceDefault, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String(), tlsSecret.Name)

	f.secretStore = append(f.secretStore, tlsSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)

	f.objects = append(f.objects, tlsSecret, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.lbc.defaultTLSSecret.Namespace = tlsSecret.Namespace
	f.lbc.defaultTLSSecret.Name = tlsSecret.Name
	f.run(getKey(svc, t))

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

// TestSyncStringNamedPort verifies that if service target port is a named port, it is looked up from Pod spec.
func TestSyncStringNamedPort(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = v1.ServicePort{
		TargetPort: intstr.FromString("my-port"),
		Protocol:   v1.ProtocolTCP,
	}
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String())

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

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)
	f.podStore = append(f.podStore, bp1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1, bp1)

	f.prepare()
	f.run(getKey(svc, t))

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

// TestSyncNumericTargetPort verifies that if target port is numeric, it is compared to endpoint port directly.
func TestSyncNumericTargetPort(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = v1.ServicePort{
		TargetPort: intstr.FromString("80"),
		Protocol:   v1.ProtocolTCP,
	}
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String())

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run(getKey(svc, t))

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

// TestSyncEmptyTargetPort verifies that if target port is empty, port is used instead.  In practice, target port is always filled out.
func TestSyncEmptyTargetPort(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs1.Spec.Ports[0] = v1.ServicePort{
		Port:       80,
		TargetPort: intstr.FromString(""),
		Protocol:   v1.ProtocolTCP,
	}
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String())

	f.svcStore = append(f.svcStore, svc, bs1)
	f.epStore = append(f.epStore, eps, be1)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.run(getKey(svc, t))

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

// TestSyncIngressClass validates that Ingress resource bearing Ingress class "foo" is not processed.
func TestSyncIngressClass(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String())

	bs2, be2 := newBackend(metav1.NamespaceDefault, "beta", []string{"192.168.10.2"})
	ing2 := newIngress(bs2.Namespace, "beta-ing", bs2.Name, bs2.Spec.Ports[0].TargetPort.String())
	ing2.Annotations[ingressClassKey] = "foo"

	f.svcStore = append(f.svcStore, svc, bs1, bs2)
	f.epStore = append(f.epStore, eps, be1, be2)
	f.ingStore = append(f.ingStore, ing1, ing2)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1, bs2, be2, ing2)

	f.prepare()
	f.run(getKey(svc, t))

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	backend := ingConfig.Upstreams[0].Backends[0]
	if got, want := backend.Address, "192.168.10.1"; got != want {
		t.Errorf("backend.Address = %v, want %v", got, want)
	}
}

// TestSyncIngressDefaultBackend verfies that Ingress.Spec.Backend is considered.
func TestSyncIngressDefaultBackend(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(metav1.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	bs2, be2 := newBackend(metav1.NamespaceDefault, "bravo", []string{"192.168.10.2"})
	ing1 := newIngress(bs1.Namespace, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String())
	ing1.Spec.Backend = &extensions.IngressBackend{
		ServiceName: "bravo",
		ServicePort: bs2.Spec.Ports[0].TargetPort,
	}

	f.svcStore = append(f.svcStore, svc, bs1, bs2)
	f.epStore = append(f.epStore, eps, be1, be2)
	f.ingStore = append(f.ingStore, ing1)

	f.objects = append(f.objects, svc, eps, bs1, be1, ing1, bs2, be2)

	f.prepare()
	f.run(getKey(svc, t))

	fm := f.lbc.nghttpx.(*fakeManager)
	ingConfig := fm.ingConfig

	if got, want := len(ingConfig.Upstreams), 2; got != want {
		t.Errorf("len(ingConfig.Upstreams) = %v, want %v", got, want)
	}

	var found bool
	for _, upstream := range ingConfig.Upstreams {
		if upstream.Name == "default/bravo,80;/" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Ingress default backend is not found")
	}
}

// newIngPod creates Ingress controller pod.
func newIngPod(name, nodeName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaultRuntimeInfo.PodNamespace,
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

	po1 := newIngPod(defaultRuntimeInfo.PodName, "alpha.test")
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

	ing1 := newIngress(metav1.NamespaceDefault, "delta-ing", "delta", "80")
	ing3 := newIngress(metav1.NamespaceDefault, "foxtrot-ing", "foxtrot", "80")
	ing3.Annotations[ingressClassKey] = "not-nghttpx"
	ing4 := newIngress(metav1.NamespaceDefault, "golf-ing", "golf", "80")
	ing4.Status.LoadBalancer.Ingress = lbIngs
	ing2 := newIngress(metav1.NamespaceDefault, "echo-ing", "echo", "80")

	f.ingStore = append(f.ingStore, ing1, ing2, ing3, ing4)

	f.objects = append(f.objects, ing1, ing2, ing3, ing4)

	f.expectUpdateIngAction(ing1)
	f.expectUpdateIngAction(ing2)

	f.prepare()
	f.setupStore()

	err := f.lbc.updateIngressStatus(lbIngs)

	f.verifyActions()

	if err != nil {
		t.Fatalf("f.lbc.updateIngressStatus(lbIngs) returned unexpected error %v", err)
	}

	if updatedIng, err := f.clientset.ExtensionsV1beta1().Ingresses(ing1.Namespace).Get(ing1.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing1.Namespace, ing1.Name, err)
	} else {
		if got, want := updatedIng.Status.LoadBalancer.Ingress, lbIngs; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}
	if updatedIng, err := f.clientset.ExtensionsV1beta1().Ingresses(ing2.Namespace).Get(ing2.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing2.Namespace, ing2.Name, err)
	} else {
		if got, want := updatedIng.Status.LoadBalancer.Ingress, lbIngs; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}
}

// TestRemoveAddressFromLoadBalancerIngress verifies that removeAddressFromLoadBalancerIngress clears Ingress.Status.LoadBalancer.Ingress.
func TestRemoveAddressFromLoadBalancerIngress(t *testing.T) {
	f := newFixture(t)

	po := newIngPod(defaultRuntimeInfo.PodName, "alpha.test")
	node := newNode("alpha.test", v1.NodeAddress{Type: v1.NodeExternalIP, Address: "192.168.0.1"})

	lbIngs := []v1.LoadBalancerIngress{{IP: "192.168.0.1"}, {IP: "192.168.0.2"}}

	ing1 := newIngress(metav1.NamespaceDefault, "delta-ing", "delta", "80")
	ing1.Status.LoadBalancer.Ingress = lbIngs

	ing2 := newIngress(metav1.NamespaceDefault, "echo-ing", "echo", "80")
	ing2.Status.LoadBalancer.Ingress = lbIngs

	ing3 := newIngress(metav1.NamespaceDefault, "foxtrot-ing", "foxtrot", "80")
	ing3.Annotations[ingressClassKey] = "not-nghttpx"
	ing3.Status.LoadBalancer.Ingress = lbIngs

	ing4 := newIngress(metav1.NamespaceDefault, "golf-ing", "golf", "80")
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

	if updatedIng, err := f.lbc.clientset.ExtensionsV1beta1().Ingresses(ing1.Namespace).Get(ing1.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing1.Namespace, ing1.Name, err)
	} else {
		ans := []v1.LoadBalancerIngress{{IP: "192.168.0.2"}}
		if got, want := updatedIng.Status.LoadBalancer.Ingress, ans; !reflect.DeepEqual(got, want) {
			t.Errorf("updatedIng.Status.LoadBalancer.Ingress = %+v, want %+v", got, want)
		}
	}

	if updatedIng, err := f.lbc.clientset.ExtensionsV1beta1().Ingresses(ing4.Namespace).Get(ing4.Name, metav1.GetOptions{}); err != nil {
		t.Errorf("Could not get Ingress %v/%v: %v", ing4.Namespace, ing4.Name, err)
	} else {
		ans := []v1.LoadBalancerIngress{{IP: "192.168.0.2"}}
		if got, want := updatedIng.Status.LoadBalancer.Ingress, ans; !reflect.DeepEqual(got, want) {
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
	f.lbc.publishSvc = "alpha/bravo"

	got := f.lbc.getLoadBalancerIngressFromService(svc)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("f.lbc.getLoadBalancerIngressFromService(...) = %q, want %q", got, want)
	}
}
