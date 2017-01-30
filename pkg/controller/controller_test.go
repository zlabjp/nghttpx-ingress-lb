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
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/apis/extensions"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset/fake"
	"k8s.io/kubernetes/pkg/client/testing/core"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/intstr"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

type fixture struct {
	t *testing.T

	clientset *fake.Clientset

	lbc *LoadBalancerController

	ingStore    []*extensions.Ingress
	endpStore   []*api.Endpoints
	svcStore    []*api.Service
	secretStore []*api.Secret
	mapStore    []*api.ConfigMap

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
	defaultIngNamespace       = api.NamespaceAll
	defaultConfigMapName      = "ing-config"
	defaultConfigMapNamespace = "kube-system"

	// openssl req -x509 -nodes -days 365 -newkey rsa:2048 -keyout /tmp/tls.key -out /tmp/tls.crt -subj "/CN=echoheaders/O=echoheaders"
	tlsCrt = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURhakNDQWxLZ0F3SUJBZ0lKQUxHUXR5VVBKTFhYTUEwR0NTcUdTSWIzRFFFQkJRVUFNQ3d4RkRBU0JnTlYKQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3RWdZRFZRUUtFd3RsWTJodmFHVmhaR1Z5Y3pBZUZ3MHhOakF6TXpFeQpNekU1TkRoYUZ3MHhOekF6TXpFeU16RTVORGhhTUN3eEZEQVNCZ05WQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3CkVnWURWUVFLRXd0bFkyaHZhR1ZoWkdWeWN6Q0NBU0l3RFFZSktvWklodmNOQVFFQkJRQURnZ0VQQURDQ0FRb0MKZ2dFQkFONzVmS0N5RWwxanFpMjUxTlNabDYzeGQweG5HMHZTVjdYL0xxTHJveVNraW5nbnI0NDZZWlE4UEJWOAo5TUZzdW5RRGt1QVoyZzA3NHM1YWhLSm9BRGJOMzhld053RXNsVDJkRzhRTUw0TktrTUNxL1hWbzRQMDFlWG1PCmkxR2txZFA1ZUExUHlPZCtHM3gzZmxPN2xOdmtJdHVHYXFyc0tvMEhtMHhqTDVtRUpwWUlOa0tGSVhsWWVLZS8KeHRDR25CU2tLVHFMTG0yeExKSGFFcnJpaDZRdkx4NXF5U2gzZTU2QVpEcTlkTERvcWdmVHV3Z2IzekhQekc2NwppZ0E0dkYrc2FRNHpZUE1NMHQyU1NiVkx1M2pScWNvL3lxZysrOVJBTTV4bjRubnorL0hUWFhHKzZ0RDBaeGI1CmVVRDNQakVhTnlXaUV2dTN6UFJmdysyNURMY0NBd0VBQWFPQmpqQ0JpekFkQmdOVkhRNEVGZ1FVcktMZFhHeUUKNUlEOGRvd2lZNkdzK3dNMHFKc3dYQVlEVlIwakJGVXdVNEFVcktMZFhHeUU1SUQ4ZG93aVk2R3Mrd00wcUp1aApNS1F1TUN3eEZEQVNCZ05WQkFNVEMyVmphRzlvWldGa1pYSnpNUlF3RWdZRFZRUUtFd3RsWTJodmFHVmhaR1Z5CmM0SUpBTEdRdHlVUEpMWFhNQXdHQTFVZEV3UUZNQU1CQWY4d0RRWUpLb1pJaHZjTkFRRUZCUUFEZ2dFQkFNZVMKMHFia3VZa3Z1enlSWmtBeE1PdUFaSDJCK0Evb3N4ODhFRHB1ckV0ZWN5RXVxdnRvMmpCSVdCZ2RkR3VBYU5jVQorUUZDRm9NakJOUDVWVUxIWVhTQ3VaczN2Y25WRDU4N3NHNlBaLzhzbXJuYUhTUjg1ZVpZVS80bmFyNUErdWErClIvMHJrSkZnOTlQSmNJd3JmcWlYOHdRcWdJVVlLNE9nWEJZcUJRL0VZS2YvdXl6UFN3UVZYRnVJTTZTeDBXcTYKTUNML3d2RlhLS0FaWDBqb3J4cHRjcldkUXNCcmYzWVRnYmx4TE1sN20zL2VuR1drcEhDUHdYeVRCOC9rRkw3SApLL2ZHTU1NWGswUkVSbGFPM1hTSUhrZUQ2SXJiRnRNV3R1RlJwZms2ZFA2TXlMOHRmTmZ6a3VvUHVEWUFaWllWCnR1NnZ0c0FRS0xWb0pGaGV0b1k9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K"
	tlsKey = "LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBM3ZsOG9MSVNYV09xTGJuVTFKbVhyZkYzVEdjYlM5Slh0Zjh1b3V1akpLU0tlQ2V2CmpqcGhsRHc4Rlh6MHdXeTZkQU9TNEJuYURUdml6bHFFb21nQU5zM2Z4N0EzQVN5VlBaMGJ4QXd2ZzBxUXdLcjkKZFdqZy9UVjVlWTZMVWFTcDAvbDREVS9JNTM0YmZIZCtVN3VVMitRaTI0WnFxdXdxalFlYlRHTXZtWVFtbGdnMgpRb1VoZVZoNHA3L0cwSWFjRktRcE9vc3ViYkVza2RvU3V1S0hwQzh2SG1ySktIZDdub0JrT3IxMHNPaXFCOU83CkNCdmZNYy9NYnJ1S0FEaThYNnhwRGpOZzh3elMzWkpKdFV1N2VOR3B5ai9LcUQ3NzFFQXpuR2ZpZWZQNzhkTmQKY2I3cTBQUm5Gdmw1UVBjK01SbzNKYUlTKzdmTTlGL0Q3YmtNdHdJREFRQUJBb0lCQUViNmFEL0hMNjFtMG45bgp6bVkyMWwvYW83MUFmU0h2dlZnRCtWYUhhQkY4QjFBa1lmQUdpWlZrYjBQdjJRSFJtTERoaWxtb0lROWhadHVGCldQOVIxKythTFlnbGdmenZzanBBenR2amZTUndFaEFpM2pnSHdNY1p4S2Q3UnNJZ2hxY2huS093S0NYNHNNczQKUnBCbEFBZlhZWGs4R3F4NkxUbGptSDRDZk42QzZHM1EwTTlLMUxBN2lsck1Na3hwcngxMnBlVTNkczZMVmNpOQptOFdBL21YZ2I0c3pEbVNaWVpYRmNZMEhYNTgyS3JKRHpQWEVJdGQwZk5wd3I0eFIybzdzMEwvK2RnZCtqWERjCkh2SDBKZ3NqODJJaTIxWGZGM2tST3FxR3BKNmhVcncxTUZzVWRyZ29GL3pFck0vNWZKMDdVNEhodGFlalVzWTIKMFJuNXdpRUNnWUVBKzVUTVRiV084Wkg5K2pIdVQwc0NhZFBYcW50WTZYdTZmYU04Tm5CZWNoeTFoWGdlQVN5agpSWERlZGFWM1c0SjU5eWxIQ3FoOVdseVh4cDVTWWtyQU41RnQ3elFGYi91YmorUFIyWWhMTWZpYlBSYlYvZW1MCm5YaGF6MmtlNUUxT1JLY0x6QUVwSmpuZGQwZlZMZjdmQzFHeStnS2YyK3hTY1hjMHJqRE5iNGtDZ1lFQTR1UVEKQk91TlJQS3FKcDZUZS9zUzZrZitHbEpjQSs3RmVOMVlxM0E2WEVZVm9ydXhnZXQ4a2E2ZEo1QjZDOWtITGtNcQpwdnFwMzkxeTN3YW5uWC9ONC9KQlU2M2RxZEcyd1BWRUQ0REduaE54Qm1oaWZpQ1I0R0c2ZnE4MUV6ZE1vcTZ4CklTNHA2RVJaQnZkb1RqNk9pTHl6aUJMckpxeUhIMWR6c0hGRlNqOENnWUVBOWlSSEgyQ2JVazU4SnVYak8wRXcKUTBvNG4xdS9TZkQ4TFNBZ01VTVBwS1hpRTR2S0Qyd1U4a1BUNDFiWXlIZUh6UUpkdDFmU0RTNjZjR0ZHU1ZUSgphNVNsOG5yN051ejg3bkwvUmMzTGhFQ3Y0YjBOOFRjbW1oSy9CbDdiRXBOd0dFczNoNGs3TVdNOEF4QU15c3VxCmZmQ1pJM0tkNVJYNk0zbGwyV2QyRjhFQ2dZQlQ5RU9oTG0vVmhWMUVjUVR0cVZlMGJQTXZWaTVLSGozZm5UZkUKS0FEUVIvYVZncElLR3RLN0xUdGxlbVpPbi8yeU5wUS91UnpHZ3pDUUtldzNzU1RFSmMzYVlzbFVudzdhazJhZAp2ZTdBYXowMU84YkdHTk1oamNmdVBIS05LN2Nsc3pKRHJzcys4SnRvb245c0JHWEZYdDJuaWlpTTVPWVN5TTg4CkNJMjFEUUtCZ0hEQVRZbE84UWlDVWFBQlVqOFBsb1BtMDhwa3cyc1VmQW0xMzJCY00wQk9BN1hqYjhtNm1ManQKOUlteU5kZ2ZiM080UjlKVUxTb1pZSTc1dUxIL3k2SDhQOVlpWHZOdzMrTXl6VFU2b2d1YU8xSTNya2pna29NeAo5cU5pYlJFeGswS1A5MVZkckVLSEdHZEFwT05ES1N4VzF3ektvbUxHdmtYSTVKV05KRXFkCi0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg=="
)

var (
	defaultRuntimeInfo = PodInfo{
		PodName:      "nghttpx-ingress-controller",
		PodNamespace: "kube-system",
		NodeIP:       "192.168.0.1",
	}
)

// prepare performs setup necessary for test run.
func (f *fixture) prepare() {
	f.clientset = fake.NewSimpleClientset(f.objects...)
	config := Config{
		ResyncPeriod:          defaultResyncPeriod,
		DefaultBackendService: fmt.Sprintf("%v/%v", defaultBackendNamespace, defaultBackendName),
		WatchNamespace:        defaultIngNamespace,
		NghttpxConfigMap:      fmt.Sprintf("%v/%v", defaultConfigMapNamespace, defaultConfigMapName),
	}
	if lbc, err := NewLoadBalancerController(f.clientset, newFakeManager(), &config, &defaultRuntimeInfo); err != nil {
		f.t.Fatalf("Could not create LoadBalancerController: %v", err)
	} else {
		f.lbc = lbc
	}
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
		f.lbc.ingLister.Add(ing)
	}
	for _, endp := range f.endpStore {
		f.lbc.endpLister.Add(endp)
	}
	for _, svc := range f.svcStore {
		f.lbc.svcLister.Add(svc)
	}
	for _, secret := range f.secretStore {
		f.lbc.secretLister.Add(secret)
	}
	for _, cm := range f.mapStore {
		f.lbc.mapLister.Add(cm)
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
func (f *fixture) expectGetCMAction(cm *api.ConfigMap) {
	f.actions = append(f.actions, core.NewGetAction(unversioned.GroupVersionResource{Resource: "configmaps"}, cm.Namespace, cm.Name))
}

// newFakeManager implements nghttpx.Interface.
type fakeManager struct {
	checkAndReloadHandler        func(cfg nghttpx.NghttpxConfiguration, ingressCfg nghttpx.IngressConfig) (bool, error)
	addOrUpdateCertAndKeyHandler func(name string, cert []byte, key []byte) (*nghttpx.TLSCred, error)

	cfg        nghttpx.NghttpxConfiguration
	ingressCfg nghttpx.IngressConfig
	certs      []keyPair
}

// newFakeManager creates new fakeManager.
func newFakeManager() *fakeManager {
	fm := &fakeManager{}
	fm.checkAndReloadHandler = fm.defaultCheckAndReload
	fm.addOrUpdateCertAndKeyHandler = fm.defaultAddOrUpdateCertAndKey
	return fm
}

func (fm *fakeManager) Start(stopCh <-chan struct{}) {}

func (fm *fakeManager) CheckAndReload(cfg nghttpx.NghttpxConfiguration, ingressCfg nghttpx.IngressConfig) (bool, error) {
	return fm.checkAndReloadHandler(cfg, ingressCfg)
}

func (fm *fakeManager) AddOrUpdateCertAndKey(name string, cert, key []byte) (*nghttpx.TLSCred, error) {
	return fm.addOrUpdateCertAndKeyHandler(name, cert, key)
}

func (fm *fakeManager) defaultCheckAndReload(cfg nghttpx.NghttpxConfiguration, ingressCfg nghttpx.IngressConfig) (bool, error) {
	fm.cfg = cfg
	fm.ingressCfg = ingressCfg
	return true, nil
}

func (fm *fakeManager) defaultAddOrUpdateCertAndKey(name string, cert []byte, key []byte) (*nghttpx.TLSCred, error) {
	fm.certs = append(fm.certs, keyPair{
		name: name,
		cert: cert,
		key:  key,
	})
	return &nghttpx.TLSCred{
		Key:      fmt.Sprintf("%v.key", name),
		Cert:     fmt.Sprintf("%v.crt", name),
		Checksum: nghttpx.TLSCertKeyChecksum(cert, key),
	}, nil
}

// keyPair contains certificate key, and cert, and their name.
type keyPair struct {
	name string
	cert []byte
	key  []byte
}

// newEmptyConfigMap returns empty ConfigMap.
func newEmptyConfigMap() *api.ConfigMap {
	return &api.ConfigMap{
		ObjectMeta: api.ObjectMeta{
			Name:      defaultConfigMapName,
			Namespace: defaultConfigMapNamespace,
		},
		Data: make(map[string]string),
	}
}

// newDefaultBackend returns Service and Endpoints for default backend.
func newDefaultBackend() (*api.Service, *api.Endpoints) {
	svc := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Spec: api.ServiceSpec{
			Ports: []api.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
	eps := &api.Endpoints{
		ObjectMeta: api.ObjectMeta{
			Name:      defaultBackendName,
			Namespace: defaultBackendNamespace,
		},
		Subsets: []api.EndpointSubset{
			{
				Addresses: []api.EndpointAddress{
					{IP: "192.168.100.1"},
					{IP: "192.168.100.2"},
				},
				Ports: []api.EndpointPort{
					{
						Protocol: api.ProtocolTCP,
						Port:     8080,
					},
				},
			},
		},
	}

	return svc, eps
}

func newBackend(namespace, name string, addrs []string) (*api.Service, *api.Endpoints) {
	svc := &api.Service{
		ObjectMeta: api.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: api.ServiceSpec{
			Ports: []api.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}
	eps := &api.Endpoints{
		ObjectMeta: api.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []api.EndpointSubset{
			{
				Ports: []api.EndpointPort{
					{
						Protocol: api.ProtocolTCP,
						Port:     8080,
					},
				},
			},
		},
	}

	var endpointAddrs []api.EndpointAddress
	for _, addr := range addrs {
		endpointAddrs = append(endpointAddrs, api.EndpointAddress{IP: addr})
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
		ObjectMeta: api.ObjectMeta{
			Name:      name,
			Namespace: namespace,
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

func newTLSSecret(namespace, name string, tlsCrt, tlsKey []byte) *api.Secret {
	return &api.Secret{
		ObjectMeta: api.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			api.TLSCertKey:       tlsCrt,
			api.TLSPrivateKeyKey: tlsKey,
		},
	}
}

func getKey(obj runtime.Object, t *testing.T) string {
	if key, err := controller.KeyFunc(obj); err != nil {
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
	cm.Data[nghttpx.NghttpxExtraConfigFieldName] = "Test"
	svc, eps := newDefaultBackend()

	f.mapStore = append(f.mapStore, cm)
	f.svcStore = append(f.svcStore, svc)
	f.endpStore = append(f.endpStore, eps)

	f.objects = append(f.objects, cm, svc, eps)

	f.prepare()
	f.run(getKey(svc, t))

	fm := f.lbc.nghttpx.(*fakeManager)
	ingressCfg := fm.ingressCfg

	if got, want := ingressCfg.Server.TLS, false; got != want {
		t.Errorf("ingressCfg.Server.TLS = %v, want %v", got, want)
	}

	if got, want := len(ingressCfg.Upstreams), 1; got != want {
		t.Errorf("len(ingressCfg.Upstreams) = %v, want %v", got, want)
	} else {
		upstream := ingressCfg.Upstreams[0]
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

	if got, want := fm.cfg.ExtraConfig, cm.Data[nghttpx.NghttpxExtraConfigFieldName]; got != want {
		t.Errorf("fm.cfg.ExtraConfig = %v, want %v", got, want)
	}
}

// TestSyncDefaultTLSSecretNotFound verifies that sync must fail if default TLS Secret is not found.
func TestSyncDefaultTLSSecretNotFound(t *testing.T) {
	f := newFixture(t)

	svc, eps := newDefaultBackend()

	f.svcStore = append(f.svcStore, svc)
	f.endpStore = append(f.endpStore, eps)

	f.objects = append(f.objects, svc, eps)

	f.prepare()
	f.lbc.defaultTLSSecret = "kube-system/default-tls"
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
	f.endpStore = append(f.endpStore, eps)

	f.objects = append(f.objects, tlsSecret, svc, eps)

	f.prepare()
	f.lbc.defaultTLSSecret = fmt.Sprintf("%v/%v", tlsSecret.Namespace, tlsSecret.Name)
	f.run(getKey(svc, t))

	fm := f.lbc.nghttpx.(*fakeManager)
	server := fm.ingressCfg.Server

	if got, want := server.TLS, true; got != want {
		t.Errorf("server.TLS = %v, want %v", got, want)
	}

	prefix := nghttpx.TLSCredPrefix(tlsSecret)
	if got, want := server.DefaultTLSCred.Key, fmt.Sprintf("%v.key", prefix); got != want {
		t.Errorf("server.DefaultTLSCred.Key = %v, want %v", got, want)
	}
	if got, want := server.DefaultTLSCred.Cert, fmt.Sprintf("%v.crt", prefix); got != want {
		t.Errorf("server.DefaultTLSCred.Crt = %v, want %v", got, want)
	}
	if got, want := server.DefaultTLSCred.Checksum, nghttpx.TLSCertKeyChecksum(dCrt, dKey); got != want {
		t.Errorf("server.DefaultTLSCred.Checksum = %v, want %v", got, want)
	}
}

// TestSyncDupDefaultSecret verifies that duplicated default TLS secret is removed.
func TestSyncDupDefaultSecret(t *testing.T) {
	f := newFixture(t)

	dCrt, _ := base64.StdEncoding.DecodeString(tlsCrt)
	dKey, _ := base64.StdEncoding.DecodeString(tlsKey)
	tlsSecret := newTLSSecret("kube-system", "default-tls", dCrt, dKey)
	svc, eps := newDefaultBackend()

	bs1, be1 := newBackend(api.NamespaceDefault, "alpha", []string{"192.168.10.1"})
	ing1 := newIngressTLS(api.NamespaceDefault, "alpha-ing", bs1.Name, bs1.Spec.Ports[0].TargetPort.String(), tlsSecret.Name)

	f.secretStore = append(f.secretStore, tlsSecret)
	f.ingStore = append(f.ingStore, ing1)
	f.svcStore = append(f.svcStore, svc, bs1)
	f.endpStore = append(f.endpStore, eps, be1)

	f.objects = append(f.objects, tlsSecret, svc, eps, bs1, be1, ing1)

	f.prepare()
	f.lbc.defaultTLSSecret = fmt.Sprintf("%v/%v", tlsSecret.Namespace, tlsSecret.Name)
	f.run(getKey(svc, t))

	fm := f.lbc.nghttpx.(*fakeManager)
	server := fm.ingressCfg.Server

	if got, want := server.TLS, true; got != want {
		t.Errorf("server.TLS = %v, want %v", got, want)
	}

	prefix := nghttpx.TLSCredPrefix(tlsSecret)
	if got, want := server.DefaultTLSCred.Key, fmt.Sprintf("%v.key", prefix); got != want {
		t.Errorf("server.DefaultTLSCred.Key = %v, want %v", got, want)
	}
	if got, want := len(server.SubTLSCred), 0; got != want {
		t.Errorf("len(server.SubTLSCred) = %v, want %v", got, want)
	}
}
