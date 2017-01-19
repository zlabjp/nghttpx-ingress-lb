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
	"encoding/json"
	"fmt"
	"math/rand"
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
	namedPortAnnotation      = "ingress.kubernetes.io/named-ports"
	backendConfigAnnotation  = "ingress.zlab.co.jp/backend-config"
	podStoreSyncedPollPeriod = 1 * time.Second
	// Minimum resync period for resources other than Ingress
	minDepResyncPeriod = 12 * time.Hour
)

type serviceAnnotation map[string]string

// getPort returns the port defined in a named port
func (npm serviceAnnotation) getPort(name string) (string, bool) {
	val, ok := npm.getPortMappings()[name]
	return val, ok
}

// getPortMappings returns the map containing the
// mapping of named port names and the port number
func (npm serviceAnnotation) getPortMappings() map[string]string {
	data := npm[namedPortAnnotation]
	var mapping map[string]string
	if data == "" {
		return mapping
	}
	if err := json.Unmarshal([]byte(data), &mapping); err != nil {
		glog.Errorf("unexpected error reading annotations: %v", err)
	}

	return mapping
}

type ingressAnnotation map[string]string

func (ia ingressAnnotation) getBackendConfig() map[string]map[string]nghttpx.PortBackendConfig {
	data := ia[backendConfigAnnotation]
	// the first key specifies service name, and secondary key specifies port name.
	var config map[string]map[string]nghttpx.PortBackendConfig
	if data == "" {
		return config
	}
	if err := json.Unmarshal([]byte(data), &config); err != nil {
		glog.Errorf("unexpected error reading %v annotation: %v", backendConfigAnnotation, err)
		return config
	}

	return config
}

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer
type LoadBalancerController struct {
	clientset        internalclientset.Interface
	ingController    *cache.Controller
	endpController   *cache.Controller
	svcController    *cache.Controller
	secretController *cache.Controller
	mapController    *cache.Controller
	ingLister        StoreToIngressLister
	svcLister        StoreToServiceLister
	endpLister       cache.StoreToEndpointsLister
	secretLister     StoreToSecretLister
	mapLister        StoreToMapLister
	nghttpx          *nghttpx.Manager
	podInfo          *PodInfo
	defaultSvc       string
	ngxConfigMap     string

	recorder record.EventRecorder

	syncQueue workqueue.RateLimitingInterface
	ingQueue  workqueue.RateLimitingInterface

	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	stopCh   chan struct{}
}

// NewLoadBalancerController creates a controller for nghttpx loadbalancer
func NewLoadBalancerController(clientset internalclientset.Interface, resyncPeriod time.Duration, defaultSvc,
	namespace, ngxConfigMapName string, runtimeInfo *PodInfo) (*LoadBalancerController, error) {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&unversionedcore.EventSinkImpl{Interface: clientset.Core().Events(namespace)})

	lbc := LoadBalancerController{
		clientset:    clientset,
		stopCh:       make(chan struct{}),
		podInfo:      runtimeInfo,
		nghttpx:      nghttpx.NewManager(),
		ngxConfigMap: ngxConfigMapName,
		defaultSvc:   defaultSvc,
		recorder:     eventBroadcaster.NewRecorder(api.EventSource{Component: "nghttpx-ingress-controller"}),
		syncQueue:    workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		ingQueue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	lbc.ingLister.Store, lbc.ingController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Extensions().Ingresses(namespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Extensions().Ingresses(namespace).Watch(options)
			},
		},
		&extensions.Ingress{},
		resyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addIngressNotification,
			UpdateFunc: lbc.updateIngressNotification,
			DeleteFunc: lbc.deleteIngressNotification,
		},
	)

	lbc.endpLister.Store, lbc.endpController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Endpoints(namespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Endpoints(namespace).Watch(options)
			},
		},
		&api.Endpoints{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addEndpointNotification,
			UpdateFunc: lbc.updateEndpointNotification,
			DeleteFunc: lbc.deleteEndpointNotification,
		},
	)

	lbc.svcLister.Store, lbc.svcController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Services(namespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Services(namespace).Watch(options)
			},
		},
		&api.Service{},
		depResyncPeriod(),
		cache.ResourceEventHandlerFuncs{},
	)

	lbc.secretLister.Store, lbc.secretController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().Secrets(namespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().Secrets(namespace).Watch(options)
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

	lbc.mapLister.Store, lbc.mapController = cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return lbc.clientset.Core().ConfigMaps(namespace).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return lbc.clientset.Core().ConfigMaps(namespace).Watch(options)
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

	return &lbc, nil
}

func (lbc *LoadBalancerController) addIngressNotification(obj interface{}) {
	ing := obj.(*extensions.Ingress)
	glog.V(4).Infof("Ingress %v/%v added", ing.Namespace, ing.Name)
	lbc.enqueueIngress(ing)
	lbc.enqueue(ing)
}

func (lbc *LoadBalancerController) updateIngressNotification(old interface{}, cur interface{}) {
	curIng := cur.(*extensions.Ingress)
	glog.V(4).Infof("Ingress %v/%v updated", curIng.Namespace, curIng.Name)
	lbc.enqueueIngress(curIng)
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
	lbc.enqueueIngress(ing)
	lbc.enqueue(ing)
}

func (lbc *LoadBalancerController) addEndpointNotification(obj interface{}) {
	ep := obj.(*api.Endpoints)
	glog.V(4).Infof("Endpoints %v/%v added", ep.Namespace, ep.Name)
	lbc.enqueue(ep)
}

func (lbc *LoadBalancerController) updateEndpointNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	curEp := cur.(*api.Endpoints)
	glog.V(4).Infof("Endpoints %v/%v updated", curEp.Namespace, curEp.Name)
	lbc.enqueue(curEp)
}

func (lbc *LoadBalancerController) deleteEndpointNotification(obj interface{}) {
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
	glog.V(4).Infof("Endpoints %v/%v deleted", ep.Namespace, ep.Name)
	lbc.enqueue(ep)
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

func (lbc *LoadBalancerController) enqueue(obj runtime.Object) {
	key, err := controller.KeyFunc(obj)
	if err != nil {
		glog.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}

	lbc.syncQueue.Add(key)
}

func (lbc *LoadBalancerController) enqueueIngress(ing *extensions.Ingress) {
	key, err := controller.KeyFunc(ing)
	if err != nil {
		glog.Errorf("Couldn't get key for object %+v: %v", ing, err)
		return
	}

	lbc.ingQueue.Add(key)
}

func (lbc *LoadBalancerController) worker() {
	for {
		func() {
			key, quit := lbc.syncQueue.Get()
			if quit {
				return
			}

			defer lbc.syncQueue.Done(key)
			lbc.sync(key.(string))
		}()
	}
}

func (lbc *LoadBalancerController) ingressWorker() {
	for {
		func() {
			key, quit := lbc.ingQueue.Get()
			if quit {
				return
			}

			defer lbc.ingQueue.Done(key)
			lbc.updateIngressStatus(key.(string))
		}()
	}
}

func (lbc *LoadBalancerController) controllersInSync() bool {
	return lbc.ingController.HasSynced() &&
		lbc.svcController.HasSynced() &&
		lbc.endpController.HasSynced() &&
		lbc.secretController.HasSynced() &&
		lbc.mapController.HasSynced()
}

func (lbc *LoadBalancerController) getConfigMap(cfgName string) *api.ConfigMap {
	if cfgName == "" {
		return &api.ConfigMap{}
	}

	ns, name, _ := ParseNSName(cfgName)
	// TODO: check why lbc.mapLister.Store.GetByKey(mapKey) is not stable (random content)
	if cfg, err := lbc.clientset.Core().ConfigMaps(ns).Get(name); err != nil {
		glog.V(3).Infof("configmap not found %v : %v", cfgName, err)
		return &api.ConfigMap{}
	} else {
		return cfg
	}
}

// checkSvcForUpdate verifies if one of the running pods for a service contains
// named port. If the annotation in the service does not exists or is not equals
// to the port mapping obtained from the pod the service must be updated to reflect
// the current state
func (lbc *LoadBalancerController) checkSvcForUpdate(svc *api.Service) (map[string]string, error) {
	// get the pods associated with the service
	// TODO: switch this to a watch
	pods, err := lbc.clientset.Core().Pods(svc.Namespace).List(api.ListOptions{
		LabelSelector: labels.Set(svc.Spec.Selector).AsSelector(),
	})

	namedPorts := map[string]string{}
	if err != nil {
		return namedPorts, fmt.Errorf("error searching service pods %v/%v: %v", svc.Namespace, svc.Name, err)
	}

	if len(pods.Items) == 0 {
		return namedPorts, nil
	}

	// we need to check only one pod searching for named ports
	pod := &pods.Items[0]
	glog.V(4).Infof("checking pod %v/%v for named port information", pod.Namespace, pod.Name)
	for i := range svc.Spec.Ports {
		servicePort := &svc.Spec.Ports[i]
		_, err := strconv.Atoi(servicePort.TargetPort.StrVal)
		if err != nil {
			portNum, err := podutil.FindPort(pod, servicePort)
			if err != nil {
				glog.V(4).Infof("failed to find port for service %s/%s: %v", svc.Namespace, svc.Name, err)
				continue
			}

			if servicePort.TargetPort.StrVal == "" {
				continue
			}

			namedPorts[servicePort.TargetPort.StrVal] = fmt.Sprintf("%v", portNum)
		}
	}

	if svc.ObjectMeta.Annotations == nil {
		svc.ObjectMeta.Annotations = map[string]string{}
	}

	curNamedPort := svc.ObjectMeta.Annotations[namedPortAnnotation]
	if len(namedPorts) > 0 && !reflect.DeepEqual(curNamedPort, namedPorts) {
		data, _ := json.Marshal(namedPorts)

		newSvc, err := lbc.clientset.Core().Services(svc.Namespace).Get(svc.Name)
		if err != nil {
			return namedPorts, fmt.Errorf("error getting service %v/%v: %v", svc.Namespace, svc.Name, err)
		}

		if newSvc.ObjectMeta.Annotations == nil {
			newSvc.ObjectMeta.Annotations = map[string]string{}
		}

		newSvc.ObjectMeta.Annotations[namedPortAnnotation] = string(data)
		glog.Infof("updating service %v with new named port mappings", svc.Name)
		_, err = lbc.clientset.Core().Services(svc.Namespace).Update(newSvc)
		if err != nil {
			return namedPorts, fmt.Errorf("error syncing service %v/%v: %v", svc.Namespace, svc.Name, err)
		}

		return newSvc.ObjectMeta.Annotations, nil
	}

	return namedPorts, nil
}

func (lbc *LoadBalancerController) sync(key string) {
	retry := false

	defer func() { retryOrForget(lbc.syncQueue, key, retry) }()

	if !lbc.controllersInSync() {
		glog.Infof("Deferring sync till endpoints controller has synced")
		retry = true
		time.Sleep(podStoreSyncedPollPeriod)
		return
	}

	ings := lbc.ingLister.Store.List()
	upstreams, server := lbc.getUpstreamServers(ings)

	cfg := lbc.getConfigMap(lbc.ngxConfigMap)

	ngxConfig := lbc.nghttpx.ReadConfig(cfg)
	lbc.nghttpx.CheckAndReload(ngxConfig, nghttpx.IngressConfig{
		Upstreams: upstreams,
		Server:    server,
	})
}

func (lbc *LoadBalancerController) updateIngressStatus(key string) {
	retry := false

	defer func() { retryOrForget(lbc.ingQueue, key, retry) }()

	if !lbc.controllersInSync() {
		glog.Infof("Deferring sync till endpoints controller has synced")
		retry = true
		time.Sleep(podStoreSyncedPollPeriod)
		return
	}

	obj, ingExists, err := lbc.ingLister.Store.GetByKey(key)
	if err != nil {
		glog.Errorf("Couldn't get ingress %v: %v", key, err)
		retry = true
		return
	}

	if !ingExists {
		glog.Infof("Ingress %v has been deleted", key)
		return
	}

	ing := obj.(*extensions.Ingress)

	ingClient := lbc.clientset.Extensions().Ingresses(ing.Namespace)

	curIng, err := ingClient.Get(ing.Name)
	if err != nil {
		glog.Errorf("unexpected error searching Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
		retry = true
		return
	}

	lbIPs := ing.Status.LoadBalancer.Ingress
	if !lbc.isStatusIPDefined(lbIPs) {
		glog.Infof("Updating loadbalancer %v/%v with IP %v", ing.Namespace, ing.Name, lbc.podInfo.NodeIP)
		curIng.Status.LoadBalancer.Ingress = append(curIng.Status.LoadBalancer.Ingress, api.LoadBalancerIngress{
			IP: lbc.podInfo.NodeIP,
		})
		if _, err := ingClient.UpdateStatus(curIng); err != nil {
			glog.Errorf("Couldn't update ingress %v: %v", key, err)
			retry = true
			return
		}
	}
}

func (lbc *LoadBalancerController) isStatusIPDefined(lbings []api.LoadBalancerIngress) bool {
	for _, lbing := range lbings {
		if lbing.IP == lbc.podInfo.NodeIP {
			return true
		}
	}

	return false
}

func (lbc *LoadBalancerController) getDefaultUpstream() *nghttpx.Upstream {
	upstream := &nghttpx.Upstream{
		Name: lbc.defaultSvc,
	}
	svcKey := lbc.defaultSvc
	svcObj, svcExists, err := lbc.svcLister.Store.GetByKey(svcKey)
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

	endps := lbc.getEndpoints(svc, svc.Spec.Ports[0].TargetPort, api.ProtocolTCP, &portBackendConfig)
	if len(endps) == 0 {
		glog.Warningf("service %v does no have any active endpoints", svcKey)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
	} else {
		upstream.Backends = append(upstream.Backends, endps...)
	}

	return upstream
}

// in nghttpx terminology, nghttpx.Upstream is backend, nghttpx.Server is frontend
func (lbc *LoadBalancerController) getUpstreamServers(data []interface{}) ([]*nghttpx.Upstream, *nghttpx.Server) {
	pems := lbc.getPemsFromIngress(data)

	server := &nghttpx.Server{}

	if len(pems) > 0 {
		server.TLS = true
		server.DefaultTLSCred = pems[0]
		server.SubTLSCred = pems[1:]
	}

	upstreams := []*nghttpx.Upstream{}

	for _, ingIf := range data {
		ing := ingIf.(*extensions.Ingress)

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
				svcObj, svcExists, err := lbc.svcLister.Store.GetByKey(svcKey)
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
					// targetPort could be a string, use the name or the port (int)
					if strconv.Itoa(int(servicePort.Port)) == bp || servicePort.TargetPort.String() == bp || servicePort.Name == bp {
						portBackendConfig, ok := svcBackendConfig[bp]
						if ok {
							portBackendConfig = nghttpx.FixupPortBackendConfig(portBackendConfig, svcKey, bp)
						} else {
							portBackendConfig = nghttpx.DefaultPortBackendConfig()
						}

						endps := lbc.getEndpoints(svc, servicePort.TargetPort, api.ProtocolTCP, &portBackendConfig)
						if len(endps) == 0 {
							glog.Warningf("service %v does no have any active endpoints", svcKey)
							break
						}

						ups.Backends = append(ups.Backends, endps...)
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

	return upstreams, server
}

func (lbc *LoadBalancerController) getPemsFromIngress(data []interface{}) []nghttpx.TLSCred {
	pems := []nghttpx.TLSCred{}

	for _, ingIf := range data {
		ing := ingIf.(*extensions.Ingress)

		for _, tls := range ing.Spec.TLS {
			secretName := tls.SecretName
			secretKey := fmt.Sprintf("%s/%s", ing.Namespace, secretName)
			secretInterface, exists, err := lbc.secretLister.Store.GetByKey(secretKey)
			if err != nil {
				glog.Warningf("Error retriveing secret %v for ing %v: %v", secretKey, ing.Name, err)
				continue
			}
			if !exists {
				glog.Warningf("Secret %v does not exist", secretKey)
				continue
			}
			secret := secretInterface.(*api.Secret)
			cert, ok := secret.Data[api.TLSCertKey]
			if !ok {
				glog.Warningf("Secret %v has no cert", secretKey)
				continue
			}
			key, ok := secret.Data[api.TLSPrivateKeyKey]
			if !ok {
				glog.Warningf("Secret %v has no private key", secretKey)
				continue
			}

			if _, err := nghttpx.CommonNames(cert); err != nil {
				glog.Errorf("No valid TLS certificate found in secret %v: %v", secretKey, err)
				continue
			}

			if err := nghttpx.CheckPrivateKey(key); err != nil {
				glog.Errorf("No valid TLS private key found in secret %v: %v", secretKey, err)
				continue
			}

			tlsCred, err := lbc.nghttpx.AddOrUpdateCertAndKey(fmt.Sprintf("%v_%v", ing.Namespace, secretName), cert, key)
			if err != nil {
				glog.Errorf("Could not create private key and certificate files %v: %v", secretKey, err)
				continue
			}

			pems = append(pems, tlsCred)
		}
	}

	sort.Sort(nghttpx.TLSCredKeyLess(pems))

	return nghttpx.RemoveDuplicatePems(pems)
}

func (lbc *LoadBalancerController) secretReferenced(namespace, name string) bool {
	for _, ingIf := range lbc.ingLister.Store.List() {
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
func (lbc *LoadBalancerController) getEndpoints(s *api.Service, servicePort intstr.IntOrString, proto api.Protocol, portBackendConfig *nghttpx.PortBackendConfig) []nghttpx.UpstreamServer {
	glog.V(3).Infof("getting endpoints for service %v/%v and port %v", s.Namespace, s.Name, servicePort.String())
	ep, err := lbc.endpLister.GetServiceEndpoints(s)
	if err != nil {
		glog.Warningf("unexpected error obtaining service endpoints: %v", err)
		return []nghttpx.UpstreamServer{}
	}

	upsServers := []nghttpx.UpstreamServer{}

	for _, ss := range ep.Subsets {
		for _, epPort := range ss.Ports {

			if !reflect.DeepEqual(epPort.Protocol, proto) {
				continue
			}

			var targetPort int
			switch servicePort.Type {
			case intstr.Int:
				if int(epPort.Port) == servicePort.IntValue() {
					targetPort = int(epPort.Port)
				}
			case intstr.String:
				val, ok := serviceAnnotation(s.ObjectMeta.Annotations).getPort(servicePort.StrVal)
				if ok {
					port, err := strconv.Atoi(val)
					if err != nil {
						glog.Warningf("%v is not valid as a port", val)
						continue
					}

					targetPort = port
				} else {
					newnp, err := lbc.checkSvcForUpdate(s)
					if err != nil {
						glog.Warningf("error mapping service ports: %v", err)
						continue
					}
					val, ok := serviceAnnotation(newnp).getPort(servicePort.StrVal)
					if ok {
						port, err := strconv.Atoi(val)
						if err != nil {
							glog.Warningf("%v is not valid as a port", val)
							continue
						}

						targetPort = port
					}
				}
			}

			if targetPort == 0 {
				continue
			}

			for _, epAddress := range ss.Addresses {
				ups := nghttpx.UpstreamServer{
					Address:  epAddress.IP,
					Port:     fmt.Sprintf("%v", targetPort),
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

func (lbc *LoadBalancerController) removeFromIngress(ings []interface{}) {
	glog.Infof("Updating %v Ingress rule(s)", len(ings))
	for _, obj := range ings {
		ing := obj.(*extensions.Ingress)

		ingClient := lbc.clientset.Extensions().Ingresses(ing.Namespace)
		curIng, err := ingClient.Get(ing.Name)
		if err != nil {
			glog.Errorf("unexpected error searching Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
			continue
		}

		updated := false
		for idx, lbStatus := range curIng.Status.LoadBalancer.Ingress {
			if lbStatus.IP != lbc.podInfo.NodeIP {
				continue
			}

			glog.Infof("Updating loadbalancer %v/%v. Removing IP %v", ing.Namespace, ing.Name, lbc.podInfo.NodeIP)

			curIng.Status.LoadBalancer.Ingress = append(curIng.Status.LoadBalancer.Ingress[:idx],
				curIng.Status.LoadBalancer.Ingress[idx+1:]...)
			updated = true
			break
		}

		if !updated {
			continue
		}

		if _, err := ingClient.UpdateStatus(curIng); err != nil {
			glog.Errorf("Couldn't update Ingress %+v: %v", curIng, err)
		}
	}
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run() {
	glog.Infof("Starting nghttpx loadbalancer controller")
	go lbc.nghttpx.Start(lbc.stopCh)

	go lbc.ingController.Run(lbc.stopCh)
	go lbc.endpController.Run(lbc.stopCh)
	go lbc.svcController.Run(lbc.stopCh)
	go lbc.secretController.Run(lbc.stopCh)
	go lbc.mapController.Run(lbc.stopCh)

	go wait.Until(lbc.worker, time.Second, lbc.stopCh)
	go wait.Until(lbc.ingressWorker, time.Second, lbc.stopCh)

	<-lbc.stopCh

	glog.Infof("Shutting down nghttpx loadbalancer controller")

	lbc.syncQueue.ShutDown()
	lbc.ingQueue.ShutDown()

	glog.Infof("Removing IP address %v from ingress rules", lbc.podInfo.NodeIP)
	ings := lbc.ingLister.Store.List()
	lbc.removeFromIngress(ings)
}

func (lbc *LoadBalancerController) Nghttpx() *nghttpx.Manager {
	return lbc.nghttpx
}

func retryOrForget(q workqueue.RateLimitingInterface, key interface{}, requeue bool) {
	if !requeue {
		q.Forget(key)
		return
	}

	q.AddRateLimited(key)
}

// depResyncPeriod returns duration between resync for resources other than Ingress.
//
// Inspired by Kubernetes apiserver: k8s.io/kubernetes/cmd/kube-controller-manager/app/controllermanager.go
func depResyncPeriod() time.Duration {
	factor := rand.Float64() + 1
	return time.Duration(float64(minDepResyncPeriod.Nanoseconds()) * factor)
}
