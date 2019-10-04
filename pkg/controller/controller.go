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
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/api/core/v1"
	clientv1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	listerscore "k8s.io/client-go/listers/core/v1"
	listersnetworking "k8s.io/client-go/listers/networking/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

const (
	// Minimum resync period for resources other than Ingress
	minDepResyncPeriod = 2 * time.Minute
	// syncKey is a key to put into the queue.  Since we create load balancer configuration using all available information, it is
	// suffice to queue only one item.  Further, queue is somewhat overkill here, but we just keep using it for simplicity.
	syncKey = "ingress"

	noResyncPeriod = 0
)

// LoadBalancerController watches the kubernetes api and adds/removes services
// from the loadbalancer
type LoadBalancerController struct {
	clientset               clientset.Interface
	ingInformer             cache.SharedIndexInformer
	epInformer              cache.SharedIndexInformer
	svcInformer             cache.SharedIndexInformer
	secretInformer          cache.SharedIndexInformer
	cmInformer              cache.SharedIndexInformer
	podInformer             cache.SharedIndexInformer
	nodeInformer            cache.SharedIndexInformer
	ingLister               listersnetworking.IngressLister
	svcLister               listerscore.ServiceLister
	epLister                listerscore.EndpointsLister
	secretLister            listerscore.SecretLister
	cmLister                listerscore.ConfigMapLister
	podLister               listerscore.PodLister
	nodeLister              listerscore.NodeLister
	nghttpx                 nghttpx.Interface
	podInfo                 *types.NamespacedName
	defaultSvc              types.NamespacedName
	ngxConfigMap            *types.NamespacedName
	nghttpxHealthPort       int
	nghttpxAPIPort          int
	nghttpxConfDir          string
	nghttpxExecPath         string
	nghttpxHTTPPort         int
	nghttpxHTTPSPort        int
	defaultTLSSecret        *types.NamespacedName
	watchNamespace          string
	ingressClass            string
	allowInternalIP         bool
	ocspRespKey             string
	fetchOCSPRespFromSecret bool
	proxyProto              bool
	publishSvc              *types.NamespacedName

	recorder record.EventRecorder

	syncQueue workqueue.Interface

	// stopLock is used to enforce only a single call to Stop is active.
	// Needed because we allow stopping through an http endpoint and
	// allowing concurrent stoppers leads to stack traces.
	stopLock sync.Mutex
	shutdown bool
	stopCh   chan struct{}

	reloadRateLimiter flowcontrol.RateLimiter
}

type Config struct {
	// ResyncPeriod is the duration that Ingress resources are forcibly processed.
	ResyncPeriod time.Duration
	// DefaultBackendService is the default backend service name.
	DefaultBackendService types.NamespacedName
	// WatchNamespace is the namespace to watch for Ingress resource updates.
	WatchNamespace string
	// NghttpxConfigMap is the name of ConfigMap resource which contains additional configuration for nghttpx.
	NghttpxConfigMap *types.NamespacedName
	// NghttpxHealthPort is the port for nghttpx health monitor endpoint.
	NghttpxHealthPort int
	// NghttpxAPIPort is the port for nghttpx API endpoint.
	NghttpxAPIPort int
	// NghttpxConfDir is the directory which contains nghttpx configuration files.
	NghttpxConfDir string
	// NghttpxExecPath is a path to nghttpx executable.
	NghttpxExecPath string
	// NghttpxHTTPPort is a port to listen to for HTTP (non-TLS) requests.
	NghttpxHTTPPort int
	// NghttpxHTTPSPort is a port to listen to for HTTPS (TLS) requests.
	NghttpxHTTPSPort int
	// DefaultTLSSecret is the default TLS Secret to enable TLS by default.
	DefaultTLSSecret *types.NamespacedName
	// IngressClass is the Ingress class this controller is responsible for.
	IngressClass            string
	AllowInternalIP         bool
	OCSPRespKey             string
	FetchOCSPRespFromSecret bool
	// ProxyProto toggles the use of PROXY protocol for all public-facing frontends.
	ProxyProto bool
	// PublishSvc is a namespace/name of Service whose addresses are written in Ingress resource instead of addresses of Ingress
	// controller Pod.
	PublishSvc *types.NamespacedName
}

// NewLoadBalancerController creates a controller for nghttpx loadbalancer
func NewLoadBalancerController(clientset clientset.Interface, manager nghttpx.Interface, config *Config, runtimeInfo *types.NamespacedName) *LoadBalancerController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: clientset.CoreV1().Events(config.WatchNamespace)})

	lbc := LoadBalancerController{
		clientset:               clientset,
		stopCh:                  make(chan struct{}),
		podInfo:                 runtimeInfo,
		nghttpx:                 manager,
		ngxConfigMap:            config.NghttpxConfigMap,
		nghttpxHealthPort:       config.NghttpxHealthPort,
		nghttpxAPIPort:          config.NghttpxAPIPort,
		nghttpxConfDir:          config.NghttpxConfDir,
		nghttpxExecPath:         config.NghttpxExecPath,
		nghttpxHTTPPort:         config.NghttpxHTTPPort,
		nghttpxHTTPSPort:        config.NghttpxHTTPSPort,
		defaultSvc:              config.DefaultBackendService,
		defaultTLSSecret:        config.DefaultTLSSecret,
		watchNamespace:          config.WatchNamespace,
		ingressClass:            config.IngressClass,
		allowInternalIP:         config.AllowInternalIP,
		ocspRespKey:             config.OCSPRespKey,
		fetchOCSPRespFromSecret: config.FetchOCSPRespFromSecret,
		proxyProto:              config.ProxyProto,
		publishSvc:              config.PublishSvc,
		recorder:                eventBroadcaster.NewRecorder(scheme.Scheme, clientv1.EventSource{Component: "nghttpx-ingress-controller"}),
		syncQueue:               workqueue.New(),
		reloadRateLimiter:       flowcontrol.NewTokenBucketRateLimiter(1.0, 1),
	}

	watchNSInformers := informers.NewSharedInformerFactoryWithOptions(lbc.clientset, config.ResyncPeriod, informers.WithNamespace(config.WatchNamespace))

	{
		f := watchNSInformers.Networking().V1beta1().Ingresses()
		lbc.ingLister = f.Lister()
		lbc.ingInformer = f.Informer()
		lbc.ingInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addIngressNotification,
			UpdateFunc: lbc.updateIngressNotification,
			DeleteFunc: lbc.deleteIngressNotification,
		})
	}

	allNSInformers := informers.NewSharedInformerFactory(lbc.clientset, noResyncPeriod)

	{
		f := allNSInformers.Core().V1().Endpoints()
		lbc.epLister = f.Lister()
		lbc.epInformer = f.Informer()
		lbc.epInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addEndpointsNotification,
			UpdateFunc: lbc.updateEndpointsNotification,
			DeleteFunc: lbc.deleteEndpointsNotification,
		})

	}

	{
		f := allNSInformers.Core().V1().Services()
		lbc.svcLister = f.Lister()
		lbc.svcInformer = f.Informer()
	}

	{
		f := allNSInformers.Core().V1().Secrets()
		lbc.secretLister = f.Lister()
		lbc.secretInformer = f.Informer()
		lbc.secretInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addSecretNotification,
			UpdateFunc: lbc.updateSecretNotification,
			DeleteFunc: lbc.deleteSecretNotification,
		})
	}

	{
		f := allNSInformers.Core().V1().Pods()
		lbc.podLister = f.Lister()
		lbc.podInformer = f.Informer()
		lbc.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addPodNotification,
			UpdateFunc: lbc.updatePodNotification,
			DeleteFunc: lbc.deletePodNotification,
		})

	}

	{
		f := allNSInformers.Core().V1().Nodes()
		lbc.nodeLister = f.Lister()
		lbc.nodeInformer = f.Informer()
	}

	var cmNamespace string
	if lbc.ngxConfigMap != nil {
		cmNamespace = lbc.ngxConfigMap.Namespace
	} else {
		// Just watch runtimeInfo.Namespace to make codebase simple
		cmNamespace = runtimeInfo.Namespace
	}

	cmNSInformers := informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod, informers.WithNamespace(cmNamespace))

	{
		f := cmNSInformers.Core().V1().ConfigMaps()
		lbc.cmLister = f.Lister()
		lbc.cmInformer = f.Informer()
		lbc.cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addConfigMapNotification,
			UpdateFunc: lbc.updateConfigMapNotification,
			DeleteFunc: lbc.deleteConfigMapNotification,
		})

	}

	return &lbc
}

func (lbc *LoadBalancerController) addIngressNotification(obj interface{}) {
	ing := obj.(*networking.Ingress)
	if !lbc.validateIngressClass(ing) {
		return
	}
	klog.V(4).Infof("Ingress %v/%v added", ing.Namespace, ing.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) updateIngressNotification(old interface{}, cur interface{}) {
	oldIng := old.(*networking.Ingress)
	curIng := cur.(*networking.Ingress)
	if !lbc.validateIngressClass(oldIng) && !lbc.validateIngressClass(curIng) {
		return
	}
	klog.V(4).Infof("Ingress %v/%v updated", curIng.Namespace, curIng.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) deleteIngressNotification(obj interface{}) {
	ing, ok := obj.(*networking.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		ing, ok = tombstone.Obj.(*networking.Ingress)
		if !ok {
			klog.Errorf("Tombstone contained object that is not an Ingress %+v", obj)
			return
		}
	}
	if !lbc.validateIngressClass(ing) {
		return
	}
	klog.V(4).Infof("Ingress %v/%v deleted", ing.Namespace, ing.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) addEndpointsNotification(obj interface{}) {
	ep := obj.(*v1.Endpoints)
	if !lbc.endpointsReferenced(ep) {
		return
	}
	klog.V(4).Infof("Endpoints %v/%v added", ep.Namespace, ep.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) updateEndpointsNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	oldEp := old.(*v1.Endpoints)
	curEp := cur.(*v1.Endpoints)
	if !lbc.endpointsReferenced(oldEp) && !lbc.endpointsReferenced(curEp) {
		return
	}
	klog.V(4).Infof("Endpoints %v/%v updated", curEp.Namespace, curEp.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) deleteEndpointsNotification(obj interface{}) {
	ep, ok := obj.(*v1.Endpoints)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		ep, ok = tombstone.Obj.(*v1.Endpoints)
		if !ok {
			klog.Errorf("Tombstone contained object that is not Endpoints %+v", obj)
			return
		}
	}
	if !lbc.endpointsReferenced(ep) {
		return
	}
	klog.V(4).Infof("Endpoints %v/%v deleted", ep.Namespace, ep.Name)
	lbc.enqueue(syncKey)
}

// endpointsReferenced returns true if we are interested in ep.
func (lbc *LoadBalancerController) endpointsReferenced(ep *v1.Endpoints) bool {
	if ep.Namespace == lbc.defaultSvc.Namespace && ep.Name == lbc.defaultSvc.Name {
		return true
	}

	ings, err := lbc.ingLister.Ingresses(ep.Namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("Could not list Ingress namespace=%v: %v", ep.Namespace, err)
		return false
	}
	for _, ing := range ings {
		if !lbc.validateIngressClass(ing) {
			continue
		}
		if ing.Spec.Backend != nil && ep.Name == ing.Spec.Backend.ServiceName {
			klog.V(4).Infof("Endpoints %v/%v is referenced by Ingress %v/%v", ep.Namespace, ep.Name, ing.Namespace, ing.Name)
			return true
		}
		for i := range ing.Spec.Rules {
			rule := &ing.Spec.Rules[i]
			if rule.HTTP == nil {
				continue
			}
			for i := range rule.HTTP.Paths {
				path := &rule.HTTP.Paths[i]
				if ep.Name == path.Backend.ServiceName {
					klog.V(4).Infof("Endpoints %v/%v is referenced by Ingress %v/%v", ep.Namespace, ep.Name, ing.Namespace, ing.Name)
					return true
				}
			}
		}
	}
	return false
}

func (lbc *LoadBalancerController) addSecretNotification(obj interface{}) {
	s := obj.(*v1.Secret)
	if !lbc.secretReferenced(s) {
		return
	}

	klog.V(4).Infof("Secret %v/%v added", s.Namespace, s.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) updateSecretNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	oldS := old.(*v1.Secret)
	curS := cur.(*v1.Secret)
	if !lbc.secretReferenced(oldS) && !lbc.secretReferenced(curS) {
		return
	}

	klog.V(4).Infof("Secret %v/%v updated", curS.Namespace, curS.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) deleteSecretNotification(obj interface{}) {
	s, ok := obj.(*v1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		s, ok = tombstone.Obj.(*v1.Secret)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a Secret %+v", obj)
			return
		}
	}
	if !lbc.secretReferenced(s) {
		return
	}
	klog.V(4).Infof("Secret %v/%v deleted", s.Namespace, s.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) addConfigMapNotification(obj interface{}) {
	c := obj.(*v1.ConfigMap)
	if lbc.ngxConfigMap == nil || c.Namespace != lbc.ngxConfigMap.Namespace || c.Name != lbc.ngxConfigMap.Name {
		return
	}
	klog.V(4).Infof("ConfigMap %v/%v added", c.Namespace, c.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) updateConfigMapNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	curC := cur.(*v1.ConfigMap)
	if lbc.ngxConfigMap == nil || curC.Namespace != lbc.ngxConfigMap.Namespace || curC.Name != lbc.ngxConfigMap.Name {
		return
	}
	// updates to configuration configmaps can trigger an update
	klog.V(4).Infof("ConfigMap %v/%v updated", curC.Namespace, curC.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) deleteConfigMapNotification(obj interface{}) {
	c, ok := obj.(*v1.ConfigMap)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		c, ok = tombstone.Obj.(*v1.ConfigMap)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a ConfigMap %+v", obj)
			return
		}
	}
	if lbc.ngxConfigMap == nil || c.Namespace != lbc.ngxConfigMap.Namespace || c.Name != lbc.ngxConfigMap.Name {
		return
	}
	klog.V(4).Infof("ConfigMap %v/%v deleted", c.Namespace, c.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) addPodNotification(obj interface{}) {
	pod := obj.(*v1.Pod)
	if !lbc.podReferenced(pod) {
		return
	}
	klog.V(4).Infof("Pod %v/%v added", pod.Namespace, pod.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) updatePodNotification(old, cur interface{}) {
	if reflect.DeepEqual(old, cur) {
		return
	}

	oldPod := old.(*v1.Pod)
	curPod := cur.(*v1.Pod)
	if !lbc.podReferenced(oldPod) && !lbc.podReferenced(curPod) {
		return
	}
	klog.V(4).Infof("Pod %v/%v updated", curPod.Namespace, curPod.Name)
	lbc.enqueue(syncKey)
}

func (lbc *LoadBalancerController) deletePodNotification(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %+v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			klog.Errorf("Tombstone contained object that is not Pod %+v", obj)
			return
		}
	}
	if !lbc.podReferenced(pod) {
		return
	}
	klog.V(4).Infof("Pod %v/%v deleted", pod.Namespace, pod.Name)
	lbc.enqueue(syncKey)
}

// podReferenced returns true if we are interested in pod.
func (lbc *LoadBalancerController) podReferenced(pod *v1.Pod) bool {
	if svc, err := lbc.svcLister.Services(lbc.defaultSvc.Namespace).Get(lbc.defaultSvc.Name); err == nil {
		if labels.Set(svc.Spec.Selector).AsSelector().Matches(labels.Set(pod.Labels)) {
			klog.V(4).Infof("Pod %v/%v is referenced by default Service %v/%v", pod.Namespace, pod.Name, lbc.defaultSvc.Namespace, lbc.defaultSvc.Name)
			return true
		}
	}

	ings, err := lbc.ingLister.Ingresses(pod.Namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("Could not list Ingress namespace=%v: %v", pod.Namespace, err)
		return false
	}
	for _, ing := range ings {
		if !lbc.validateIngressClass(ing) {
			continue
		}
		if ing.Spec.Backend != nil {
			if svc, err := lbc.svcLister.Services(pod.Namespace).Get(ing.Spec.Backend.ServiceName); err == nil {
				if labels.Set(svc.Spec.Selector).AsSelector().Matches(labels.Set(pod.Labels)) {
					klog.V(4).Infof("Pod %v/%v is referenced by Ingress %v/%v through Service %v/%v",
						pod.Namespace, pod.Name, ing.Namespace, ing.Name, svc.Namespace, svc.Name)
					return true
				}
			}
		}
		for i := range ing.Spec.Rules {
			rule := &ing.Spec.Rules[i]
			if rule.HTTP == nil {
				continue
			}
			for i := range rule.HTTP.Paths {
				path := &rule.HTTP.Paths[i]
				svc, err := lbc.svcLister.Services(pod.Namespace).Get(path.Backend.ServiceName)
				if err != nil {
					continue
				}
				if labels.Set(svc.Spec.Selector).AsSelector().Matches(labels.Set(pod.Labels)) {
					klog.V(4).Infof("Pod %v/%v is referenced by Ingress %v/%v through Service %v/%v",
						pod.Namespace, pod.Name, ing.Namespace, ing.Name, svc.Namespace, svc.Name)
					return true
				}
			}
		}
	}

	return false
}

func (lbc *LoadBalancerController) enqueue(key string) {
	lbc.syncQueue.Add(key)
}

func (lbc *LoadBalancerController) worker() {
	work := func() bool {
		key, quit := lbc.syncQueue.Get()
		if quit {
			return true
		}

		defer lbc.syncQueue.Done(key)
		if err := lbc.sync(key.(string)); err != nil {
			klog.Error(err)
		}

		return false
	}

	for {
		if quit := work(); quit {
			return
		}
	}
}

// getConfigMap returns ConfigMap denoted by cmKey.
func (lbc *LoadBalancerController) getConfigMap(cmKey *types.NamespacedName) (*v1.ConfigMap, error) {
	if cmKey == nil {
		return &v1.ConfigMap{}, nil
	}

	cm, err := lbc.cmLister.ConfigMaps(cmKey.Namespace).Get(cmKey.Name)
	if errors.IsNotFound(err) {
		klog.V(3).Infof("ConfigMap %v has been deleted", cmKey)
		return &v1.ConfigMap{}, nil
	}
	if err != nil {
		return nil, err
	}
	return cm, nil
}

func (lbc *LoadBalancerController) sync(key string) error {
	lbc.reloadRateLimiter.Accept()

	retry := false

	defer func() { lbc.retryOrForget(key, retry) }()

	ings, err := lbc.ingLister.List(labels.Everything())
	if err != nil {
		return err
	}
	ingConfig, err := lbc.getUpstreamServers(ings)
	if err != nil {
		return err
	}

	cm, err := lbc.getConfigMap(lbc.ngxConfigMap)
	if err != nil {
		return err
	}

	nghttpx.ReadConfig(ingConfig, cm)

	if reloaded, err := lbc.nghttpx.CheckAndReload(ingConfig); err != nil {
		return err
	} else if !reloaded {
		klog.V(4).Infof("No need to reload configuration.")
	}

	return nil
}

func (lbc *LoadBalancerController) getDefaultUpstream() *nghttpx.Upstream {
	svcKey := lbc.defaultSvc.String()
	upstream := &nghttpx.Upstream{
		Name:             svcKey,
		RedirectIfNotTLS: lbc.defaultTLSSecret != nil,
		Affinity:         nghttpx.AffinityNone,
	}
	svc, err := lbc.svcLister.Services(lbc.defaultSvc.Namespace).Get(lbc.defaultSvc.Name)
	if errors.IsNotFound(err) {
		klog.Warningf("service %v not found", svcKey)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
		return upstream
	}

	if err != nil {
		klog.Warningf("unexpected error searching the default backend %v: %v", svcKey, err)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
		return upstream
	}

	eps := lbc.getEndpoints(svc, &svc.Spec.Ports[0], v1.ProtocolTCP, &nghttpx.PortBackendConfig{})
	if len(eps) == 0 {
		klog.Warningf("service %v does not have any active endpoints", svcKey)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
	} else {
		upstream.Backends = append(upstream.Backends, eps...)
	}

	return upstream
}

// in nghttpx terminology, nghttpx.Upstream is backend, nghttpx.Server is frontend
func (lbc *LoadBalancerController) getUpstreamServers(ings []*networking.Ingress) (*nghttpx.IngressConfig, error) {
	ingConfig := nghttpx.NewIngressConfig()
	ingConfig.HealthPort = lbc.nghttpxHealthPort
	ingConfig.APIPort = lbc.nghttpxAPIPort
	ingConfig.ConfDir = lbc.nghttpxConfDir
	ingConfig.HTTPPort = lbc.nghttpxHTTPPort
	ingConfig.HTTPSPort = lbc.nghttpxHTTPSPort
	ingConfig.FetchOCSPRespFromSecret = lbc.fetchOCSPRespFromSecret
	ingConfig.ProxyProto = lbc.proxyProto

	var (
		upstreams []*nghttpx.Upstream
		pems      []*nghttpx.TLSCred
	)

	if lbc.defaultTLSSecret != nil {
		tlsCred, err := lbc.getTLSCredFromSecret(lbc.defaultTLSSecret)
		if err != nil {
			return nil, err
		}

		ingConfig.TLS = true
		ingConfig.DefaultTLSCred = tlsCred
	}

	var defaultUpstream *nghttpx.Upstream

	for _, ing := range ings {
		if !lbc.validateIngressClass(ing) {
			klog.V(4).Infof("Skip Ingress %v/%v which has Ingress class %v", ing.Namespace, ing.Name, ingressAnnotation(ing.Annotations).getIngressClass())
			continue
		}

		klog.Infof("Processing Ingress %v/%v", ing.Namespace, ing.Name)

		var requireTLS bool
		if ingPems, err := lbc.getTLSCredFromIngress(ing); err != nil {
			klog.Warningf("Ingress %v/%v is disabled because its TLS Secret cannot be processed: %v", ing.Namespace, ing.Name, err)
			continue
		} else {
			pems = append(pems, ingPems...)
			requireTLS = len(ingPems) > 0
		}

		defaultPortBackendConfig, backendConfig := ingressAnnotation(ing.Annotations).getBackendConfig()
		defaultPathConfig, pathConfig := ingressAnnotation(ing.Annotations).getPathConfig()

		if ing.Spec.Backend != nil {
			// This overrides the default backend specified in command-line.  It is possible that the multiple Ingress resource
			// specifies this.  But specification does not any rules how to deal with it.  Just use the one we meet last.
			if ups, err := lbc.createUpstream(ing, "", "/", ing.Spec.Backend, false, defaultPathConfig, pathConfig, defaultPortBackendConfig, backendConfig); err != nil {
				klog.Errorf("Could not create default backend for Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
			} else {
				defaultUpstream = ups
			}
		}

		for i := range ing.Spec.Rules {
			rule := &ing.Spec.Rules[i]

			if rule.HTTP == nil {
				continue
			}

			for i := range rule.HTTP.Paths {
				path := &rule.HTTP.Paths[i]
				if ups, err := lbc.createUpstream(ing, rule.Host, path.Path, &path.Backend, requireTLS, defaultPathConfig, pathConfig, defaultPortBackendConfig, backendConfig); err != nil {
					klog.Errorf("Could not create backend for Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
					continue
				} else {
					upstreams = append(upstreams, ups)
				}
			}
		}
	}

	sort.Slice(pems, func(i, j int) bool { return pems[i].Key.Path < pems[j].Key.Path })
	pems = nghttpx.RemoveDuplicatePems(pems)

	if ingConfig.DefaultTLSCred != nil {
		// Remove default TLS key pair from pems.
		for i := range pems {
			if ingConfig.DefaultTLSCred.Key.Path == pems[i].Key.Path {
				pems = append(pems[:i], pems[i+1:]...)
				break
			}
		}
		ingConfig.SubTLSCred = pems
	} else if len(pems) > 0 {
		ingConfig.TLS = true
		ingConfig.DefaultTLSCred = pems[0]
		ingConfig.SubTLSCred = pems[1:]
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
		if defaultUpstream == nil {
			defaultUpstream = lbc.getDefaultUpstream()
		}
		upstreams = append(upstreams, defaultUpstream)
	}

	sort.Slice(upstreams, func(i, j int) bool { return upstreams[i].Name < upstreams[j].Name })

	for _, value := range upstreams {
		backends := value.Backends
		sort.Slice(backends, func(i, j int) bool {
			return backends[i].Address < backends[j].Address || (backends[i].Address == backends[j].Address && backends[i].Port < backends[j].Port)
		})

		// remove duplicate UpstreamServer
		uniqBackends := []nghttpx.UpstreamServer{backends[0]}
		for _, sv := range backends[1:] {
			lastBackend := &uniqBackends[len(uniqBackends)-1]

			if lastBackend.Address == sv.Address && lastBackend.Port == sv.Port {
				continue
			}

			uniqBackends = append(uniqBackends, sv)
		}

		value.Backends = uniqBackends
	}

	ingConfig.Upstreams = upstreams

	return ingConfig, nil
}

// createUpstream creates new nghttpx.Upstream for ing, host, path and backend.
func (lbc *LoadBalancerController) createUpstream(ing *networking.Ingress, host, path string, backend *networking.IngressBackend,
	requireTLS bool, defaultPathConfig *nghttpx.PathConfig, pathConfig map[string]*nghttpx.PathConfig, defaultPortBackendConfig *nghttpx.PortBackendConfig, backendConfig map[string]map[string]*nghttpx.PortBackendConfig) (*nghttpx.Upstream, error) {
	var normalizedPath string
	if path == "" {
		normalizedPath = "/"
	} else if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("Host %v has Path which does not start /: %v", host, path)
	} else {
		normalizedPath = path
	}
	pc := nghttpx.ResolvePathConfig(host, normalizedPath, defaultPathConfig, pathConfig)
	// The format of upsName is similar to backend option syntax of nghttpx.
	upsName := fmt.Sprintf("%v/%v,%v;%v%v", ing.Namespace, backend.ServiceName, backend.ServicePort.String(), host, normalizedPath)
	ups := &nghttpx.Upstream{
		Name:                 upsName,
		Host:                 host,
		Path:                 normalizedPath,
		RedirectIfNotTLS:     pc.GetRedirectIfNotTLS() && (requireTLS || lbc.defaultTLSSecret != nil),
		Affinity:             pc.GetAffinity(),
		AffinityCookieName:   pc.GetAffinityCookieName(),
		AffinityCookiePath:   pc.GetAffinityCookiePath(),
		AffinityCookieSecure: pc.GetAffinityCookieSecure(),
		ReadTimeout:          pc.GetReadTimeout(),
		WriteTimeout:         pc.GetWriteTimeout(),
	}

	if mruby := pc.GetMruby(); mruby != "" {
		ups.Mruby = nghttpx.CreatePerPatternMrubyChecksumFile(lbc.nghttpxConfDir, []byte(mruby))
	}

	klog.V(4).Infof("Found rule for upstream name=%v, host=%v, path=%v", upsName, ups.Host, ups.Path)

	svcKey := fmt.Sprintf("%v/%v", ing.Namespace, backend.ServiceName)
	svc, err := lbc.svcLister.Services(ing.Namespace).Get(backend.ServiceName)
	if errors.IsNotFound(err) {
		return nil, fmt.Errorf("service %v not found", svcKey)
	}

	if err != nil {
		return nil, fmt.Errorf("error getting service %v from the cache: %v", svcKey, err)
	}

	klog.V(3).Infof("obtaining port information for service %v", svcKey)
	bp := backend.ServicePort.String()

	svcBackendConfig := backendConfig[backend.ServiceName]

	for i := range svc.Spec.Ports {
		servicePort := &svc.Spec.Ports[i]
		// According to the documentation, servicePort.TargetPort is optional.  If it is omitted, use
		// servicePort.Port.  servicePort.TargetPort could be a string.  This is really messy.
		if strconv.Itoa(int(servicePort.Port)) == bp || servicePort.TargetPort.String() == bp || servicePort.Name == bp {
			portBackendConfig, ok := svcBackendConfig[bp]
			if !ok {
				portBackendConfig = &nghttpx.PortBackendConfig{}
				if defaultPortBackendConfig != nil {
					nghttpx.ApplyDefaultPortBackendConfig(portBackendConfig, defaultPortBackendConfig)
				}
			}

			eps := lbc.getEndpoints(svc, servicePort, v1.ProtocolTCP, portBackendConfig)
			if len(eps) == 0 {
				klog.Warningf("service %v does not have any active endpoints", svcKey)
				break
			}

			ups.Backends = append(ups.Backends, eps...)
			break
		}
	}

	if len(ups.Backends) == 0 {
		return nil, fmt.Errorf("no backend service port found for service %v", svcKey)
	}

	return ups, nil
}

// getTLSCredFromSecret returns nghttpx.TLSCred obtained from the Secret denoted by secretKey.
func (lbc *LoadBalancerController) getTLSCredFromSecret(key *types.NamespacedName) (*nghttpx.TLSCred, error) {
	secret, err := lbc.secretLister.Secrets(key.Namespace).Get(key.Name)
	if errors.IsNotFound(err) {
		return nil, fmt.Errorf("Secret %v has been deleted", key)
	}
	if err != nil {
		return nil, fmt.Errorf("Could not get TLS secret %v: %v", key, err)
	}
	tlsCred, err := lbc.createTLSCredFromSecret(secret)
	if err != nil {
		return nil, err
	}
	return tlsCred, nil
}

// getTLSCredFromIngress returns list of nghttpx.TLSCred obtained from Ingress resource.
func (lbc *LoadBalancerController) getTLSCredFromIngress(ing *networking.Ingress) ([]*nghttpx.TLSCred, error) {
	var pems []*nghttpx.TLSCred

	for i := range ing.Spec.TLS {
		tls := &ing.Spec.TLS[i]
		secret, err := lbc.secretLister.Secrets(ing.Namespace).Get(tls.SecretName)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil, fmt.Errorf("Secret %v/%v has been deleted", ing.Namespace, tls.SecretName)
			}
			return nil, fmt.Errorf("Error retrieving Secret %v/%v for Ingress %v/%v: %v", ing.Namespace, tls.SecretName, ing.Namespace, ing.Name, err)
		}
		tlsCred, err := lbc.createTLSCredFromSecret(secret)
		if err != nil {
			return nil, err
		}

		pems = append(pems, tlsCred)
	}

	return pems, nil
}

// createTLSCredFromSecret creates nghttpx.TLSCred from secret.
func (lbc *LoadBalancerController) createTLSCredFromSecret(secret *v1.Secret) (*nghttpx.TLSCred, error) {
	cert, ok := secret.Data[v1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("Secret %v/%v has no certificate", secret.Namespace, secret.Name)
	}
	key, ok := secret.Data[v1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("Secret %v/%v has no private key", secret.Namespace, secret.Name)
	}

	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return nil, err
	}

	// OCSP response in TLS secret is optional feature.
	ocspResp := secret.Data[lbc.ocspRespKey]

	tlsCred, err := nghttpx.CreateTLSCred(lbc.nghttpxConfDir, nghttpx.TLSCredPrefix(secret), cert, key, ocspResp)
	if err != nil {
		return nil, fmt.Errorf("Could not create private key and certificate files for Secret %v/%v: %v", secret.Namespace, secret.Name, err)
	}

	return tlsCred, nil
}

func (lbc *LoadBalancerController) secretReferenced(s *v1.Secret) bool {
	if lbc.defaultTLSSecret != nil && s.Namespace == lbc.defaultTLSSecret.Namespace && s.Name == lbc.defaultTLSSecret.Name {
		return true
	}

	ings, err := lbc.ingLister.Ingresses(s.Namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("Could not list Ingress namespace=%v: %v", s.Namespace, err)
		return false
	}
	for _, ing := range ings {
		if !lbc.validateIngressClass(ing) {
			continue
		}
		for i := range ing.Spec.TLS {
			tls := &ing.Spec.TLS[i]
			if tls.SecretName == s.Name {
				return true
			}
		}
	}
	return false
}

// getEndpoints returns a list of <endpoint ip>:<port> for a given
// service/target port combination.  portBackendConfig is additional
// per-port configuration for backend, which must not be nil.
func (lbc *LoadBalancerController) getEndpoints(s *v1.Service, servicePort *v1.ServicePort, proto v1.Protocol, portBackendConfig *nghttpx.PortBackendConfig) []nghttpx.UpstreamServer {
	klog.V(3).Infof("getting endpoints for service %v/%v and port %v target port %v protocol %v", s.Namespace, s.Name, servicePort.Port, servicePort.TargetPort.String(), servicePort.Protocol)
	ep, err := lbc.epLister.Endpoints(s.Namespace).Get(s.Name)
	if err != nil {
		klog.Warningf("unexpected error obtaining service endpoints: %v", err)
		return []nghttpx.UpstreamServer{}
	}

	upsServers := []nghttpx.UpstreamServer{}

	for i := range ep.Subsets {
		ss := &ep.Subsets[i]
		for i := range ss.Ports {
			epPort := &ss.Ports[i]
			if epPort.Protocol != proto {
				continue
			}

			var targetPort int32

			if servicePort.TargetPort.String() == "" {
				if epPort.Port == servicePort.Port {
					targetPort = epPort.Port
				}
			} else {
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
							klog.Warningf("Could not find named port %v in Pod spec: %v", servicePort.TargetPort.String(), err)
							continue
						}
					} else {
						port = int32(p)
					}
					if epPort.Port == port {
						targetPort = port
					}
				}
			}

			if targetPort == 0 {
				klog.V(4).Infof("Endpoint port %v does not match Service port %v", epPort.Port, servicePort.TargetPort.String())
				continue
			}

			for i := range ss.Addresses {
				epAddress := &ss.Addresses[i]
				ups := nghttpx.UpstreamServer{
					Address:              epAddress.IP,
					Port:                 strconv.Itoa(int(targetPort)),
					Protocol:             portBackendConfig.GetProto(),
					TLS:                  portBackendConfig.GetTLS(),
					SNI:                  portBackendConfig.GetSNI(),
					DNS:                  portBackendConfig.GetDNS(),
					Affinity:             portBackendConfig.GetAffinity(),
					AffinityCookieName:   portBackendConfig.GetAffinityCookieName(),
					AffinityCookiePath:   portBackendConfig.GetAffinityCookiePath(),
					AffinityCookieSecure: portBackendConfig.GetAffinityCookieSecure(),
					Group:                fmt.Sprintf("%v/%v", s.Namespace, s.Name),
					Weight:               portBackendConfig.GetWeight(),
				}
				// Set Protocol and Affinity here if they are empty.  Template expects them.
				if ups.Protocol == "" {
					ups.Protocol = nghttpx.ProtocolH1
				}
				if ups.Affinity == "" {
					ups.Affinity = nghttpx.AffinityNone
				} else if ups.Affinity == nghttpx.AffinityCookie && ups.AffinityCookieSecure == "" {
					ups.AffinityCookieSecure = nghttpx.AffinityCookieSecureAuto
				}
				upsServers = append(upsServers, ups)
			}
		}
	}

	klog.V(3).Infof("endpoints found: %+v", upsServers)
	return upsServers
}

// getNamedPortFromPod returns port number from Pod sharing the same port name with servicePort.
func (lbc *LoadBalancerController) getNamedPortFromPod(svc *v1.Service, servicePort *v1.ServicePort) (int32, error) {
	pods, err := lbc.podLister.Pods(svc.Namespace).List(labels.Set(svc.Spec.Selector).AsSelector())
	if err != nil {
		return 0, fmt.Errorf("Could not get Pods %v/%v: %v", svc.Namespace, svc.Name, err)
	}

	if len(pods) == 0 {
		return 0, fmt.Errorf("No Pods available for Service %v/%v", svc.Namespace, svc.Name)
	}

	pod := pods[0]
	port, err := podFindPort(pod, servicePort)
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
		klog.Infof("Shutting down is already in progress")
		return
	}

	klog.Infof("Commencing shutting down")

	lbc.shutdown = true
	close(lbc.stopCh)
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run() {
	klog.Infof("Starting nghttpx loadbalancer controller")

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		lbc.nghttpx.Start(lbc.nghttpxExecPath, nghttpx.NghttpxConfigPath(lbc.nghttpxConfDir), lbc.stopCh)
	}()

	go lbc.ingInformer.Run(lbc.stopCh)
	go lbc.epInformer.Run(lbc.stopCh)
	go lbc.svcInformer.Run(lbc.stopCh)
	go lbc.secretInformer.Run(lbc.stopCh)
	go lbc.cmInformer.Run(lbc.stopCh)
	go lbc.podInformer.Run(lbc.stopCh)
	go lbc.nodeInformer.Run(lbc.stopCh)

	if !cache.WaitForCacheSync(
		lbc.stopCh,
		lbc.ingInformer.HasSynced,
		lbc.epInformer.HasSynced,
		lbc.svcInformer.HasSynced,
		lbc.secretInformer.HasSynced,
		lbc.cmInformer.HasSynced,
		lbc.podInformer.HasSynced,
		lbc.nodeInformer.HasSynced,
	) {
		return
	}

	go lbc.worker()

	wg.Add(1)
	go func() {
		defer wg.Done()
		lbc.syncIngress(lbc.stopCh)
	}()

	<-lbc.stopCh

	klog.Infof("Shutting down nghttpx loadbalancer controller")

	lbc.syncQueue.ShutDown()

	wg.Wait()
}

func (lbc *LoadBalancerController) retryOrForget(key interface{}, requeue bool) {
	if requeue {
		lbc.syncQueue.Add(key)
	}
}

// validateIngressClass checks whether this controller should process ing or not.  If ing has "kubernetes.io/ingress.class" annotation, its
// value should be empty or "nghttpx".
func (lbc *LoadBalancerController) validateIngressClass(ing *networking.Ingress) bool {
	switch ingressAnnotation(ing.Annotations).getIngressClass() {
	case "", lbc.ingressClass:
		return true
	default:
		return false
	}
}

// syncIngress udpates Ingress resource status.
func (lbc *LoadBalancerController) syncIngress(stopCh <-chan struct{}) {
	for {
		if err := lbc.getLoadBalancerIngressAndUpdateIngress(); err != nil {
			klog.Errorf("Could not update Ingress status: %v", err)
		}

		select {
		case <-stopCh:
			if err := lbc.removeAddressFromLoadBalancerIngress(); err != nil {
				klog.Error(err)
			}
			return
		case <-time.After(time.Duration(float64(30*time.Second) * (rand.Float64() + 1))):
		}
	}
}

// getLoadBalancerIngressAndUpdateIngress gets addresses to set in Ingress resource, and updates Ingress Status with them.
func (lbc *LoadBalancerController) getLoadBalancerIngressAndUpdateIngress() error {
	var lbIngs []v1.LoadBalancerIngress

	if lbc.publishSvc == nil {
		thisPod, err := lbc.getThisPod()
		if err != nil {
			return err
		}

		selector := labels.Set(thisPod.Labels).AsSelector()
		lbIngs, err = lbc.getLoadBalancerIngress(selector)
		if err != nil {
			return fmt.Errorf("Could not get Node IP of Ingress controller: %v", err)
		}
	} else {
		svc, err := lbc.svcLister.Services(lbc.publishSvc.Namespace).Get(lbc.publishSvc.Name)
		if err != nil {
			return err
		}

		lbIngs = lbc.getLoadBalancerIngressFromService(svc)
	}

	sortLoadBalancerIngress(lbIngs)

	return lbc.updateIngressStatus(uniqLoadBalancerIngress(lbIngs))
}

// getThisPod returns this controller's pod.
func (lbc *LoadBalancerController) getThisPod() (*v1.Pod, error) {
	pod, err := lbc.podLister.Pods(lbc.podInfo.Namespace).Get(lbc.podInfo.Name)
	if err != nil {
		return nil, fmt.Errorf("Could not get Pod %v from lister: %v", lbc.podInfo, err)
	}
	return pod, nil
}

// updateIngressStatus updates LoadBalancerIngress field of all Ingresses.
func (lbc *LoadBalancerController) updateIngressStatus(lbIngs []v1.LoadBalancerIngress) error {

	ings, err := lbc.ingLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("Could not list Ingress: %v", err)
	}

	for _, ing := range ings {
		select {
		case <-lbc.stopCh:
			return nil
		default:
		}

		if !lbc.validateIngressClass(ing) {
			continue
		}

		// Just pass ing.Status.LoadBalancerIngress.Ingress without sorting them.  This is OK since we write sorted
		// LoadBalancerIngress array, and will eventually get sorted one.
		if loadBalancerIngressesIPEqual(ing.Status.LoadBalancer.Ingress, lbIngs) {
			continue
		}

		klog.V(4).Infof("Update Ingress %v/%v .Status.LoadBalancer.Ingress to %q", ing.Namespace, ing.Name, lbIngs)

		newIng := ing.DeepCopy()
		newIng.Status.LoadBalancer.Ingress = lbIngs

		if _, err := lbc.clientset.NetworkingV1beta1().Ingresses(ing.Namespace).UpdateStatus(newIng); err != nil {
			klog.Errorf("Could not update Ingress %v/%v status: %v", ing.Namespace, ing.Name, err)
		}
	}

	return nil
}

// getLoadBalancerIngress creates array of v1.LoadBalancerIngress based on cached Pods and Nodes.
func (lbc *LoadBalancerController) getLoadBalancerIngress(selector labels.Selector) ([]v1.LoadBalancerIngress, error) {
	pods, err := lbc.podLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("Could not list Pods with label %v", selector)
	}

	var lbIngs []v1.LoadBalancerIngress

	for _, pod := range pods {
		externalIP, err := lbc.getPodAddress(pod)
		if err != nil {
			klog.Error(err)
			continue
		}

		lbIng := v1.LoadBalancerIngress{}
		// This is really a messy specification.
		if net.ParseIP(externalIP) != nil {
			lbIng.IP = externalIP
		} else {
			lbIng.Hostname = externalIP
		}
		lbIngs = append(lbIngs, lbIng)
	}

	return lbIngs, nil
}

// getLoadBalancerIngressFromService returns the set of addresses from svc.
func (lbc *LoadBalancerController) getLoadBalancerIngressFromService(svc *v1.Service) []v1.LoadBalancerIngress {
	lbIngs := make([]v1.LoadBalancerIngress, len(svc.Status.LoadBalancer.Ingress)+len(svc.Spec.ExternalIPs))
	copy(lbIngs, svc.Status.LoadBalancer.Ingress)

	i := len(svc.Status.LoadBalancer.Ingress)
	for _, ip := range svc.Spec.ExternalIPs {
		lbIngs[i].IP = ip
		i++
	}

	return lbIngs
}

// getPodAddress returns pod's address.  It prefers external IP.  It may return internal IP if configuration allows it.
func (lbc *LoadBalancerController) getPodAddress(pod *v1.Pod) (string, error) {
	node, err := lbc.nodeLister.Get(pod.Spec.NodeName)
	if errors.IsNotFound(err) {
		return "", fmt.Errorf("Node %v for Pod %v/%v has been deleted", pod.Spec.NodeName, pod.Namespace, pod.Name)
	}
	if err != nil {
		return "", fmt.Errorf("Could not get Node %v for Pod %v/%v from lister: %v", pod.Spec.NodeName, pod.Namespace, pod.Name, err)
	}
	var externalIP string
	for i := range node.Status.Addresses {
		address := &node.Status.Addresses[i]
		if address.Type == v1.NodeExternalIP {
			if address.Address == "" {
				continue
			}
			externalIP = address.Address
			break
		}

		if externalIP == "" && (lbc.allowInternalIP && address.Type == v1.NodeInternalIP) {
			externalIP = address.Address
			// Continue to the next iteration because we may encounter v1.NodeExternalIP later.
		}
	}

	if externalIP == "" {
		return "", fmt.Errorf("Node %v has no external IP", node.Name)
	}

	return externalIP, nil
}

// removeAddressFromLoadBalancerIngress removes this address from all Ingress.Status.LoadBalancer.Ingress.
func (lbc *LoadBalancerController) removeAddressFromLoadBalancerIngress() error {
	klog.Infof("Remove this address from all Ingress.Status.LoadBalancer.Ingress.")

	thisPod, err := lbc.getThisPod()
	if err != nil {
		return fmt.Errorf("Could not remove address from LoadBalancerIngress: %v", err)
	}

	addr, err := lbc.getPodAddress(thisPod)
	if err != nil {
		return fmt.Errorf("Could not remove address from LoadBalancerIngress: %v", err)
	}

	ings, err := lbc.ingLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("Could not remove address from LoadBalancerIngress: %v", err)
	}

	for _, ing := range ings {
		if !lbc.validateIngressClass(ing) {
			continue
		}

		// Time may be short because we should do all the work during Pod graceful shut down period.
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			ing, err := lbc.ingLister.Ingresses(ing.Namespace).Get(ing.Name)
			if err != nil {
				return err
			}

			if len(ing.Status.LoadBalancer.Ingress) == 0 {
				return nil
			}

			lbIngs := removeAddressFromLoadBalancerIngress(ing.Status.LoadBalancer.Ingress, addr)
			if len(ing.Status.LoadBalancer.Ingress) == len(lbIngs) {
				return nil
			}

			newIng := ing.DeepCopy()
			newIng.Status.LoadBalancer.Ingress = lbIngs

			if _, err := lbc.clientset.NetworkingV1beta1().Ingresses(newIng.Namespace).UpdateStatus(newIng); err != nil {
				return err
			}
			return nil
		}); err != nil {
			klog.Errorf("Could not remove address from LoadBalancerIngress from Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
		}
	}

	return nil
}
