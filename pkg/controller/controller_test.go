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
)

// prepare performs setup necessary for test run.
func (f *fixture) prepare() {
	f.clientset = fake.NewSimpleClientset(f.objects...)
	config := Config{
		ResyncPeriod:              defaultResyncPeriod,
		DefaultBackendServiceName: fmt.Sprintf("%v/%v", defaultBackendNamespace, defaultBackendName),
		WatchNamespace:            defaultIngNamespace,
		NghttpxConfigMapName:      fmt.Sprintf("%v/%v", defaultConfigMapNamespace, defaultConfigMapName),
	}
	if lbc, err := NewLoadBalancerController(f.clientset, newFakeManager(), &config, nil); err != nil {
		f.t.Fatalf("Could not create LoadBalancerController: %v", err)
	} else {
		f.lbc = lbc
	}
	f.lbc.controllersInSyncHandler = func() bool { return true }
}

func (f *fixture) run(ingKey string) {
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

	if err := f.lbc.sync(ingKey); err != nil {
		f.t.Errorf("Failed to sync %v: %v", ingKey, err)
	}

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
	addOrUpdateCertAndKeyHandler func(name string, cert []byte, key []byte) (nghttpx.TLSCred, error)

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

func (fm *fakeManager) AddOrUpdateCertAndKey(name string, cert, key []byte) (nghttpx.TLSCred, error) {
	return fm.AddOrUpdateCertAndKey(name, cert, key)
}

func (fm *fakeManager) defaultCheckAndReload(cfg nghttpx.NghttpxConfiguration, ingressCfg nghttpx.IngressConfig) (bool, error) {
	fm.cfg = cfg
	fm.ingressCfg = ingressCfg
	return true, nil
}

func (fm *fakeManager) defaultAddOrUpdateCertAndKey(name string, cert []byte, key []byte) (nghttpx.TLSCred, error) {
	fm.certs = append(fm.certs, keyPair{
		name: name,
		cert: cert,
		key:  key,
	})
	return nghttpx.TLSCred{
		Key:      fmt.Sprintf("%v.key", name),
		Cert:     fmt.Sprintf("%v.crt", name),
		Checksum: "deadbeef",
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
	svc, eps := newDefaultBackend()

	f.mapStore = append(f.mapStore, cm)
	f.svcStore = append(f.svcStore, svc)
	f.endpStore = append(f.endpStore, eps)

	f.objects = append(f.objects, cm, svc, eps)

	f.expectGetCMAction(cm)

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
}
