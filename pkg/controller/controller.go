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
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/api"
	podutil "k8s.io/kubernetes/pkg/api/pod"
	"k8s.io/kubernetes/pkg/apis/extensions"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	unversionedcore "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset/typed/core/internalversion"
	"k8s.io/kubernetes/pkg/client/record"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util/intstr"
	"k8s.io/kubernetes/pkg/util/wait"
	"k8s.io/kubernetes/pkg/util/workqueue"
	"k8s.io/kubernetes/pkg/watch"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

const (
	defServerName            = "_"
	backendConfigAnnotation  = "ingress.zlab.co.jp/backend-config"
	podStoreSyncedPollPeriod = 1 * time.Second
	// Minimum resync period for resources other than Ingress
	minDepResyncPeriod = 12 * time.Hour
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer
type LoadBalancerController struct {
	clientset        internalclientset.Interface
	ingController    *cache.Controller
	epController     *cache.Controller
	svcController    *cache.Controller
	secretController *cache.Controller
	cmController     *cache.Controller
	podController    *cache.Controller
	ingLister        StoreToIngressLister
	svcLister        StoreToServiceLister
	epLister         cache.StoreToEndpointsLister
	secretLister     StoreToSecretLister
	cmLister         StoreToConfigMapLister
	podLister        cache.StoreToPodLister
	nghttpx          nghttpx.Interface
	podInfo          *PodInfo
	defaultSvc       string
	ngxConfigMap     string
	defaultTLSSecret string
	watchNamespace   string

	recorder record.EventRecorder

	syncQueue workqueue.RateLimitingInterface

	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	stopCh   chan struct{}

	// controllersInSyncHandler returns true if all resource controllers have synced.
	controllersInSyncHandler func() bool
}

type Config struct {
	// ResyncPeriod is the duration that Ingress resources are forcibly processed.
	ResyncPeriod time.Duration
	// DefaultBackendService is the default backend service name.
	DefaultBackendService string
	// WatchNamespace is the namespace to watch for Ingress resource updates.
	WatchNamespace string
	// NghttpxConfigMap is the name of ConfigMap resource which contains additional configuration for nghttpx.
	NghttpxConfigMap string
	// DefaultTLSSecret is the default TLS Secret to enable TLS by default.
	DefaultTLSSecret string
}

// NewLoadBalancerController creates a controller for nghttpx loadbalancer
func NewLoadBalancerController(clientset internalclientset.Interface, manager nghttpx.Interface, config *Config, runtimeInfo *PodInfo) *LoadBalancerController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{Interface: clientset.Core().Events(config.WatchNamespace)})

	lbc := LoadBalancerController{
		clientset:        clientset,
		stopCh:           make(chan struct{}),
		podInfo:          runtimeInfo,
		nghttpx:          manager,
		ngxConfigMap:     config.NghttpxConfigMap,
		defaultSvc:       config.DefaultBackendService,
		defaultTLSSecret: config.DefaultTLSSecret,
		watchNamespace:   config.WatchNamespace,
		recorder:         eventBroadcaster.NewRecorder(api.EventSource{Component: "nghttpx-ingress-controller"}),
		syncQueue:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	lbc.ingLister.Store, lbc.ingController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Extensions().Ingresses(config.WatchNamespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Extensions().Ingresses(config.WatchNamespace).Watch(options)
			},
		},
		&extensions.Ingress{},
		config.ResyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addIngressNotification,
			UpdateFunc: lbc.updateIngressNotification,
			DeleteFunc: lbc.deleteIngressNotification,
		},
	)

	lbc.epLister.Store, lbc.epController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Endpoints(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Endpoints(api.NamespaceAll).Watch(options)
			},
		},
		&api.Endpoints{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addEndpointsNotification,
			UpdateFunc: lbc.updateEndpointsNotification,
			DeleteFunc: lbc.deleteEndpointsNotification,
		},
	)

	lbc.svcLister.Store, lbc.svcController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Services(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Services(api.NamespaceAll).Watch(options)
			},
		},
		&api.Service{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{},
	)

	lbc.secretLister.Store, lbc.secretController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Secrets(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Secrets(api.NamespaceAll).Watch(options)
			},
		},
		&api.Secret{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addSecretNotification,
			UpdateFunc: lbc.updateSecretNotification,
			DeleteFunc: lbc.deleteSecretNotification,
		},
	)

	lbc.podLister.Indexer, lbc.podController = cache.NewIndexerInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Pods(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Pods(api.NamespaceAll).Watch(options)
			},
		},
		&api.Pod{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addPodNotification,
			UpdateFunc: lbc.updatePodNotification,
			DeleteFunc: lbc.deletePodNotification,
		},
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	var cmNamespace string
	if lbc.ngxConfigMap != "" {
		ns, _, _ := ParseNSName(lbc.ngxConfigMap)
		cmNamespace = ns
	} else {
		// Just watch runtimeInfo.PodNamespace to make codebase simple
		cmNamespace = runtimeInfo.PodNamespace
	}

	lbc.cmLister.Store, lbc.cmController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().ConfigMaps(cmNamespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().ConfigMaps(cmNamespace).Watch(options)
			},
		},
		&api.ConfigMap{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addConfigMapNotification,
			UpdateFunc: lbc.updateConfigMapNotification,
			DeleteFunc: lbc.deleteConfigMapNotification,
		},
	)

	lbc.controllersInSyncHandler = lbc.controllersInSync

	return &lbc
}

func (lbc *LoadBalancerController) addIngressNotification(obj interface{}) {
	ing := obj.(*extensions.Ingress)
	glog.V(4).Infof("Ingress %v/%v added", ing.Namespace, ing.Name)
	lbc.enqueue(ing)
}

func (lbc *LoadBalancerController) updateIngressNotification(old interface{}, cur interface{}) {
	curIng := cur.(*extensions.Ingress)
	glog.V(4).Infof("Ingress %v/%v updated", curIng.Namespace, curIng.Name)
	lbc.enqueue(curIng)
}

func (lbc *LoadBalancerController) deleteIngressNotification(obj interface{}) {
	ing, ok := obj.(*extensions.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		ing, ok = tombstone.Obj.(*extensions.Ingress)
		if !ok {
			glog.Errorf("Tombstone contained object that is not an Ingress %+v", obj)
			return
		}
	}
	glog.V(4).Infof("Ingress %v/%v deleted", ing.Namespace, ing.Name)
	lbc.enqueue(ing)
}

func (lbc *LoadBalancerController) addEndpointsNotification(obj interface{}) {
	ep := obj.(*api.Endpoints)
	if !lbc.endpointsReferenced(ep) {
		return
	}
	glog.V(4).Infof("Endpoints %v/%v added", ep.Namespace, ep.Name)
	lbc.enqueue(ep)
}

func (lbc *LoadBalancerController) updateEndpointsNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	curEp := cur.(*api.Endpoints)
	if !lbc.endpointsReferenced(curEp) {
		return
	}
	glog.V(4).Infof("Endpoints %v/%v updated", curEp.Namespace, curEp.Name)
	lbc.enqueue(curEp)
}

func (lbc *LoadBalancerController) deleteEndpointsNotification(obj interface{}) {
	ep, ok := obj.(*api.Endpoints)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		ep, ok = tombstone.Obj.(*api.Endpoints)
		if !ok {
			glog.Errorf("Tombstone contained object that is not Endpoints %+v", obj)
			return
		}
	}
	if !lbc.endpointsReferenced(ep) {
		return
	}
	glog.V(4).Infof("Endpoints %v/%v deleted", ep.Namespace, ep.Name)
	lbc.enqueue(ep)
}

// endpointsReferenced returns true if we are interested in ep.
func (lbc *LoadBalancerController) endpointsReferenced(ep *api.Endpoints) bool {
	return lbc.watchNamespace == api.NamespaceAll || lbc.watchNamespace == ep.Namespace || fmt.Sprintf("%v/%v", ep.Namespace, ep.Name) == lbc.defaultSvc
}

func (lbc *LoadBalancerController) addSecretNotification(obj interface{}) {
	s := obj.(*api.Secret)
	if !lbc.secretReferenced(s.Namespace, s.Name) {
		return
	}

	glog.V(4).Infof("Secret %v/%v added", s.Namespace, s.Name)
	lbc.enqueue(s)
}

func (lbc *LoadBalancerController) updateSecretNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	curS := cur.(*api.Secret)
	if !lbc.secretReferenced(curS.Namespace, curS.Name) {
		return
	}

	glog.V(4).Infof("Secret %v/%v updated", curS.Namespace, curS.Name)
	lbc.enqueue(curS)
}

func (lbc *LoadBalancerController) deleteSecretNotification(obj interface{}) {
	s, ok := obj.(*api.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		s, ok = tombstone.Obj.(*api.Secret)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a Secret %+v", obj)
			return
		}
	}
	if !lbc.secretReferenced(s.Namespace, s.Name) {
		return
	}
	glog.V(4).Infof("Secret %v/%v deleted", s.Namespace, s.Name)
	lbc.enqueue(s)
}

func (lbc *LoadBalancerController) addConfigMapNotification(obj interface{}) {
	c := obj.(*api.ConfigMap)
	cKey := fmt.Sprintf("%v/%v", c.Namespace, c.Name)
	if cKey != lbc.ngxConfigMap {
		return
	}
	glog.V(4).Infof("ConfigMap %v added", cKey)
	lbc.enqueue(c)
}

func (lbc *LoadBalancerController) updateConfigMapNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	curC := cur.(*api.ConfigMap)
	cKey := fmt.Sprintf("%v/%v", curC.Namespace, curC.Name)
	// updates to configuration configmaps can trigger an update
	if cKey != lbc.ngxConfigMap {
		return
	}
	glog.V(4).Infof("ConfigMap %v updated", cKey)
	lbc.enqueue(curC)
}

func (lbc *LoadBalancerController) deleteConfigMapNotification(obj interface{}) {
	c, ok := obj.(*api.ConfigMap)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		c, ok = tombstone.Obj.(*api.ConfigMap)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a ConfigMap %+v", obj)
			return
		}
	}
	cKey := fmt.Sprintf("%v/%v", c.Namespace, c.Name)
	if cKey != lbc.ngxConfigMap {
		return
	}
	glog.V(4).Infof("ConfigMap %v deleted", cKey)
	lbc.enqueue(c)
}

func (lbc *LoadBalancerController) addPodNotification(obj interface{}) {
	pod := obj.(*api.Pod)
	if !lbc.podReferenced(pod) {
		return
	}
	glog.V(4).Infof("Pod %v/%v added", pod.Namespace, pod.Name)
	lbc.enqueue(pod)
}

func (lbc *LoadBalancerController) updatePodNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	curPod := cur.(*api.Pod)
	if !lbc.podReferenced(curPod) {
		return
	}
	glog.V(4).Infof("Pod %v/%v updated", curPod.Namespace, curPod.Name)
	lbc.enqueue(curPod)
}

func (lbc *LoadBalancerController) deletePodNotification(obj interface{}) {
	pod, ok := obj.(*api.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*api.Pod)
		if !ok {
			glog.Errorf("Tombstone contained object that is not Pod %+v", obj)
			return
		}
	}
	if !lbc.podReferenced(pod) {
		return
	}
	glog.V(4).Infof("Pod %v/%v deleted", pod.Namespace, pod.Name)
	lbc.enqueue(pod)
}

// podReferenced returns true if we are interested in pod.
func (lbc *LoadBalancerController) podReferenced(pod *api.Pod) bool {
	return lbc.watchNamespace == api.NamespaceAll || lbc.watchNamespace == pod.Namespace || fmt.Sprintf("%v/%v", pod.Namespace, pod.Name) == lbc.defaultSvc
}

func (lbc *LoadBalancerController) enqueue(obj runtime.Object) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		glog.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}

	lbc.syncQueue.Add(key)
}

func (lbc *LoadBalancerController) worker() {
	for {
		func() {
			key, quit := lbc.syncQueue.Get()
			if quit {
				return
			}

			defer lbc.syncQueue.Done(key)
			if err := lbc.sync(key.(string)); err != nil {
				glog.Error(err)
			}
		}()
	}
}

func (lbc *LoadBalancerController) controllersInSync() bool {
	return lbc.ingController.HasSynced() &&
		lbc.svcController.HasSynced() &&
		lbc.epController.HasSynced() &&
		lbc.secretController.HasSynced() &&
		lbc.cmController.HasSynced() &&
		lbc.podController.HasSynced()
}

// getConfigMap returns ConfigMap denoted by cmKey.
func (lbc *LoadBalancerController) getConfigMap(cmKey string) (*api.ConfigMap, error) {
	if cmKey == "" {
		return &api.ConfigMap{}, nil
	}

	obj, exists, err := lbc.cmLister.GetByKey(cmKey)
	if err != nil {
		return nil, err
	} else if !exists {
		glog.V(3).Infof("ConfigMap %v has been deleted", cmKey)
		return &api.ConfigMap{}, nil
	}
	return obj.(*api.ConfigMap), nil
}

func (lbc *LoadBalancerController) sync(key string) error {
	retry := false

	defer func() { lbc.retryOrForget(key, retry) }()

	if !lbc.controllersInSyncHandler() {
		glog.Infof("Deferring sync till endpoints controller has synced")
		retry = true
		time.Sleep(podStoreSyncedPollPeriod)
		return nil
	}

	ings := lbc.ingLister.List()
	upstreams, server, err := lbc.getUpstreamServers(ings)
	if err != nil {
		return err
	}

	cfg, err := lbc.getConfigMap(lbc.ngxConfigMap)
	if err != nil {
		return err
	}

	ngxConfig := nghttpx.ReadConfig(cfg)
	if reloaded, err := lbc.nghttpx.CheckAndReload(ngxConfig, nghttpx.IngressConfig{
		Upstreams: upstreams,
		Server:    server,
	}); err != nil {
		return err
	} else if !reloaded {
		glog.V(4).Infof("No need to reload configuration.")
	}

	return nil
}

func (lbc *LoadBalancerController) getDefaultUpstream() *nghttpx.Upstream {
	upstream := &nghttpx.Upstream{
		Name: lbc.defaultSvc,
	}
	svcKey := lbc.defaultSvc
	svcObj, svcExists, err := lbc.svcLister.GetByKey(svcKey)
	if err != nil {
		glog.Warningf("unexpected error searching the default backend %v: %v", lbc.defaultSvc, err)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
		return upstream
	}

	if !svcExists {
		glog.Warningf("service %v does no exists", svcKey)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
		return upstream
	}

	svc := svcObj.(*api.Service)

	portBackendConfig := nghttpx.DefaultPortBackendConfig()

	eps := lbc.getEndpoints(svc, &svc.Spec.Ports[0], api.ProtocolTCP, &portBackendConfig)
	if len(eps) == 0 {
		glog.Warningf("service %v does no have any active endpoints", svcKey)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
	} else {
		upstream.Backends = append(upstream.Backends, eps...)
	}

	return upstream
}

// in nghttpx terminology, nghttpx.Upstream is backend, nghttpx.Server is frontend
func (lbc *LoadBalancerController) getUpstreamServers(data []interface{}) ([]*nghttpx.Upstream, *nghttpx.Server, error) {
	server := &nghttpx.Server{}

	var (
		upstreams []*nghttpx.Upstream
		pems      []*nghttpx.TLSCred
	)

	if lbc.defaultTLSSecret != "" {
		tlsCred, err := lbc.getTLSCredFromSecret(lbc.defaultTLSSecret)
		if err != nil {
			return nil, nil, err
		}

		server.TLS = true
		server.DefaultTLSCred = tlsCred
	}

	for _, ingIf := range data {
		ing := ingIf.(*extensions.Ingress)

		if ingPems, err := lbc.getTLSCredFromIngress(ing); err != nil {
			glog.Warningf("Ingress %v/%v is disabled because its TLS Secret cannot be processed: %v", ing.Namespace, ing.Name, err)
			continue
		} else {
			pems = append(pems, ingPems...)
		}

		backendConfig := ingressAnnotation(ing.ObjectMeta.Annotations).getBackendConfig()

		for _, rule := range ing.Spec.Rules {
			if rule.IngressRuleValue.HTTP == nil {
				continue
			}

			for _, path := range rule.HTTP.Paths {
				var normalizedPath string
				if path.Path == "" {
					normalizedPath = "/"
				} else if !strings.HasPrefix(path.Path, "/") {
					glog.Infof("Ingress %v/%v, host %v has Path which does not start /: %v", ing.Namespace, ing.Name, rule.Host, path.Path)
					continue
				} else {
					normalizedPath = path.Path
				}
				// The format of upsName is similar to backend option syntax of nghttpx.
				upsName := fmt.Sprintf("%v/%v,%v;%v%v", ing.Namespace, path.Backend.ServiceName, path.Backend.ServicePort.String(), rule.Host, normalizedPath)
				ups := &nghttpx.Upstream{
					Name: upsName,
					Host: rule.Host,
					Path: normalizedPath,
				}

				glog.V(4).Infof("Found rule for upstream name=%v, host=%v, path=%v", upsName, ups.Host, ups.Path)

				svcKey := fmt.Sprintf("%v/%v", ing.Namespace, path.Backend.ServiceName)
				svcObj, svcExists, err := lbc.svcLister.GetByKey(svcKey)
				if err != nil {
					glog.Infof("error getting service %v from the cache: %v", svcKey, err)
					continue
				}

				if !svcExists {
					glog.Warningf("service %v does no exists", svcKey)
					continue
				}

				svc := svcObj.(*api.Service)
				glog.V(3).Infof("obtaining port information for service %v", svcKey)
				bp := path.Backend.ServicePort.String()

				svcBackendConfig := backendConfig[path.Backend.ServiceName]

				for _, servicePort := range svc.Spec.Ports {
					// According to the documentation, servicePort.TargetPort is optional.  If it is omitted, use
					// servicePort.Port.  servicePort.TargetPort could be a string.  This is really messy.
					if strconv.Itoa(int(servicePort.Port)) == bp || servicePort.TargetPort.String() == bp || servicePort.Name == bp {
						portBackendConfig, ok := svcBackendConfig[bp]
						if ok {
							portBackendConfig = nghttpx.FixupPortBackendConfig(portBackendConfig, svcKey, bp)
						} else {
							portBackendConfig = nghttpx.DefaultPortBackendConfig()
						}

						eps := lbc.getEndpoints(svc, &servicePort, api.ProtocolTCP, &portBackendConfig)
						if len(eps) == 0 {
							glog.Warningf("service %v does no have any active endpoints", svcKey)
							break
						}

						ups.Backends = append(ups.Backends, eps...)
						break
					}
				}

				if len(ups.Backends) == 0 {
					glog.Warningf("no backend service port found for service %v", svcKey)
					continue
				}

				upstreams = append(upstreams, ups)
			}
		}
	}

	sort.Sort(nghttpx.TLSCredKeyLess(pems))
	pems = nghttpx.RemoveDuplicatePems(pems)

	if server.DefaultTLSCred != nil {
		// Remove default TLS key pair from pems.
		for i, _ := range pems {
			if server.DefaultTLSCred.Key == pems[i].Key {
				pems = append(pems[:i], pems[i+1:]...)
				break
			}
		}
		server.SubTLSCred = pems
	} else if len(pems) > 0 {
		server.TLS = true
		server.DefaultTLSCred = pems[0]
		server.SubTLSCred = pems[1:]
	}

	// find default backend.  If only it is not found, use default backend.  This is useful to override default backend with ingress.
	defaultUpstreamFound := false

	for _, upstream := range upstreams {
		if upstream.Host == "" && (upstream.Path == "" || upstream.Path == "/") {
			defaultUpstreamFound = true
			break
		}
	}

	if !defaultUpstreamFound {
		upstreams = append(upstreams, lbc.getDefaultUpstream())
	}

	sort.Sort(nghttpx.UpstreamByNameServers(upstreams))

	for _, value := range upstreams {
		sort.Sort(nghttpx.UpstreamServerByAddrPort(value.Backends))

		// remove duplicate UpstreamServer
		uniqBackends := []nghttpx.UpstreamServer{value.Backends[0]}
		for _, sv := range value.Backends[1:] {
			lastBackend := &uniqBackends[len(uniqBackends)-1]

			if lastBackend.Address == sv.Address && lastBackend.Port == sv.Port {
				continue
			}

			uniqBackends = append(uniqBackends, sv)
		}

		value.Backends = uniqBackends
	}

	return upstreams, server, nil
}

// getTLSCredFromSecret returns nghttpx.TLSCred obtained from the Secret denoted by secretKey.
func (lbc *LoadBalancerController) getTLSCredFromSecret(secretKey string) (*nghttpx.TLSCred, error) {
	obj, exists, err := lbc.secretLister.GetByKey(secretKey)
	if err != nil {
		return nil, fmt.Errorf("Could not get TLS secret %v: %v", secretKey, err)
	}
	if !exists {
		return nil, fmt.Errorf("Secret %v has been deleted", secretKey)
	}
	tlsCred, err := lbc.createTLSCredFromSecret(obj.(*api.Secret))
	if err != nil {
		return nil, err
	}
	return tlsCred, nil
}

// getTLSCredFromIngress returns list of nghttpx.TLSCred obtained from Ingress resource.
func (lbc *LoadBalancerController) getTLSCredFromIngress(ing *extensions.Ingress) ([]*nghttpx.TLSCred, error) {
	var pems []*nghttpx.TLSCred

	for _, tls := range ing.Spec.TLS {
		secretKey := fmt.Sprintf("%s/%s", ing.Namespace, tls.SecretName)
		obj, exists, err := lbc.secretLister.GetByKey(secretKey)
		if err != nil {
			return nil, fmt.Errorf("Error retrieving Secret %v for Ingress %v/%v: %v", secretKey, ing.Namespace, ing.Name, err)
		}
		if !exists {
			return nil, fmt.Errorf("Secret %v has been deleted", secretKey)
		}
		tlsCred, err := lbc.createTLSCredFromSecret(obj.(*api.Secret))
		if err != nil {
			return nil, err
		}

		pems = append(pems, tlsCred)
	}

	return pems, nil
}

// createTLSCredFromSecret creates nghttpx.TLSCred from secret.
func (lbc *LoadBalancerController) createTLSCredFromSecret(secret *api.Secret) (*nghttpx.TLSCred, error) {
	cert, ok := secret.Data[api.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("Secret %v/%v has no certificate", secret.Namespace, secret.Name)
	}
	key, ok := secret.Data[api.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("Secret %v/%v has no private key", secret.Namespace, secret.Name)
	}

	if _, err := nghttpx.CommonNames(cert); err != nil {
		return nil, fmt.Errorf("No valid TLS certificate found in Secret %v/%v: %v", secret.Namespace, secret.Name, err)
	}

	if err := nghttpx.CheckPrivateKey(key); err != nil {
		return nil, fmt.Errorf("No valid TLS private key found in Secret %v/%v: %v", secret.Namespace, secret.Name, err)
	}

	tlsCred, err := lbc.nghttpx.AddOrUpdateCertAndKey(nghttpx.TLSCredPrefix(secret), cert, key)
	if err != nil {
		return nil, fmt.Errorf("Could not create private key and certificate files for Secret %v/%v: %v", secret.Namespace, secret.Name, err)
	}

	return tlsCred, nil
}

func (lbc *LoadBalancerController) secretReferenced(namespace, name string) bool {
	if lbc.defaultTLSSecret == fmt.Sprintf("%v/%v", namespace, name) {
		return true
	}

	for _, ingIf := range lbc.ingLister.List() {
		ing := ingIf.(*extensions.Ingress)
		if ing.Namespace != namespace {
			continue
		}
		for _, tls := range ing.Spec.TLS {
			if tls.SecretName == name {
				return true
			}
		}
	}
	return false
}

// getEndpoints returns a list of <endpoint ip>:<port> for a given
// service/target port combination.  portBackendConfig is additional
// per-port configuration for backend, which must not be nil.
func (lbc *LoadBalancerController) getEndpoints(s *api.Service, servicePort *api.ServicePort, proto api.Protocol, portBackendConfig *nghttpx.PortBackendConfig) []nghttpx.UpstreamServer {
	glog.V(3).Infof("getting endpoints for service %v/%v and port %v protocol %v", s.Namespace, s.Name, servicePort.TargetPort.String(), servicePort.Protocol)
	ep, err := lbc.epLister.GetServiceEndpoints(s)
	if err != nil {
		glog.Warningf("unexpected error obtaining service endpoints: %v", err)
		return []nghttpx.UpstreamServer{}
	}

	upsServers := []nghttpx.UpstreamServer{}

	for _, ss := range ep.Subsets {
		for _, epPort := range ss.Ports {
			if epPort.Protocol != proto {
				continue
			}

			var targetPort int32

			switch servicePort.TargetPort.Type {
			case intstr.Int:
				if epPort.Port == servicePort.TargetPort.IntVal {
					targetPort = epPort.Port
				}
			case intstr.String:
				// TODO Is this necessary?
				if servicePort.TargetPort.StrVal == "" {
					break
				}
				var port int32
				if p, err := strconv.Atoi(servicePort.TargetPort.StrVal); err != nil {
					port, err = lbc.getNamedPortFromPod(s, servicePort)
					if err != nil {
						glog.Warningf("Could not find named port %v in Pod spec: %v", servicePort.TargetPort.String(), err)
						continue
					}
				} else {
					port = int32(p)
				}
				if epPort.Port == port {
					targetPort = port
				}
			}

			if targetPort == 0 {
				glog.V(4).Infof("Endpoint port %v does not match Service port %v", epPort.Port, servicePort.TargetPort.String())
				continue
			}

			for _, epAddress := range ss.Addresses {
				ups := nghttpx.UpstreamServer{
					Address:  epAddress.IP,
					Port:     strconv.Itoa(int(targetPort)),
					Protocol: portBackendConfig.Proto,
					TLS:      portBackendConfig.TLS,
					SNI:      portBackendConfig.SNI,
					DNS:      portBackendConfig.DNS,
					Affinity: portBackendConfig.Affinity,
				}
				upsServers = append(upsServers, ups)
			}
		}
	}

	glog.V(3).Infof("endpoints found: %+v", upsServers)
	return upsServers
}

// getNamedPortFromPod returns port number from Pod sharing the same port name with servicePort.
func (lbc *LoadBalancerController) getNamedPortFromPod(svc *api.Service, servicePort *api.ServicePort) (int32, error) {
	pods, err := lbc.podLister.Pods(svc.Namespace).List(labels.Set(svc.Spec.Selector).AsSelector())
	if err != nil {
		return 0, fmt.Errorf("Could not get Pods %v/%v: %v", svc.Namespace, svc.Name, err)
	}

	if len(pods) == 0 {
		return 0, fmt.Errorf("No Pods available for Service %v/%v", svc.Namespace, svc.Name)
	}

	pod := pods[0]
	port, err := podutil.FindPort(pod, servicePort)
	if err != nil {
		return 0, fmt.Errorf("Failed to find port %v from Pod %v/%v: %v", servicePort.TargetPort.String(), pod.Namespace, pod.Name, err)
	}
	return int32(port), nil
}

// Stop commences shutting down the loadbalancer controller.
func (lbc *LoadBalancerController) Stop() {
	// Stop is invoked from the http endpoint.
	lbc.stopLock.Lock()
	defer lbc.stopLock.Unlock()

	// Only try draining the workqueue if we haven't already.
	if lbc.shutdown {
		glog.Infof("Shutting down is already in progress")
		return
	}

	glog.Infof("Commencing shutting down")

	lbc.shutdown = true
	close(lbc.stopCh)
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run() {
	glog.Infof("Starting nghttpx loadbalancer controller")
	go lbc.nghttpx.Start(lbc.stopCh)

	go lbc.ingController.Run(lbc.stopCh)
	go lbc.epController.Run(lbc.stopCh)
	go lbc.svcController.Run(lbc.stopCh)
	go lbc.secretController.Run(lbc.stopCh)
	go lbc.cmController.Run(lbc.stopCh)
	go lbc.podController.Run(lbc.stopCh)

	go wait.Until(lbc.worker, time.Second, lbc.stopCh)

	<-lbc.stopCh

	glog.Infof("Shutting down nghttpx loadbalancer controller")

	lbc.syncQueue.ShutDown()
}

func (lbc *LoadBalancerController) retryOrForget(key interface{}, requeue bool) {
	if !requeue {
		lbc.syncQueue.Forget(key)
		return
	}

	lbc.syncQueue.AddRateLimited(key)
}
