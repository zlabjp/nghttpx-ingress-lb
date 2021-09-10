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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/api/core/v1"
	clientv1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1beta1"
	networking "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	listerscore "k8s.io/client-go/listers/core/v1"
	listersdiscovery "k8s.io/client-go/listers/discovery/v1beta1"
	listersnetworking "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

const (
	// syncKey is a key to put into the queue.  Since we create load balancer configuration using all available information, it is
	// suffice to queue only one item.  Further, queue is somewhat overkill here, but we just keep using it for simplicity.
	syncKey = "ingress"

	noResyncPeriod = 0

	annotationIsDefaultIngressClass = "ingressclass.kubernetes.io/is-default-class"
)

// LoadBalancerController watches the kubernetes api and adds/removes services from the loadbalancer
type LoadBalancerController struct {
	clientset                clientset.Interface
	ingInformer              cache.SharedIndexInformer
	ingClassInformer         cache.SharedIndexInformer
	epInformer               cache.SharedIndexInformer
	epSliceInformer          cache.SharedIndexInformer
	svcInformer              cache.SharedIndexInformer
	secretInformer           cache.SharedIndexInformer
	cmInformer               cache.SharedIndexInformer
	podInformer              cache.SharedIndexInformer
	nodeInformer             cache.SharedIndexInformer
	ingLister                listersnetworking.IngressLister
	ingClassLister           listersnetworking.IngressClassLister
	svcLister                listerscore.ServiceLister
	epLister                 listerscore.EndpointsLister
	epSliceLister            listersdiscovery.EndpointSliceLister
	secretLister             listerscore.SecretLister
	cmLister                 listerscore.ConfigMapLister
	podLister                listerscore.PodLister
	nodeLister               listerscore.NodeLister
	nghttpx                  nghttpx.Interface
	podInfo                  types.NamespacedName
	defaultSvc               *types.NamespacedName
	nghttpxConfigMap         *types.NamespacedName
	defaultTLSSecret         *types.NamespacedName
	publishSvc               *types.NamespacedName
	nghttpxHealthPort        int32
	nghttpxAPIPort           int32
	nghttpxConfDir           string
	nghttpxExecPath          string
	nghttpxHTTPPort          int32
	nghttpxHTTPSPort         int32
	watchNamespace           string
	ingressClassController   string
	allowInternalIP          bool
	ocspRespKey              string
	fetchOCSPRespFromSecret  bool
	proxyProto               bool
	noDefaultBackendOverride bool
	deferredShutdownPeriod   time.Duration
	healthzPort              int32
	internalDefaultBackend   bool
	reloadRateLimiter        flowcontrol.RateLimiter
	recorder                 record.EventRecorder
	syncQueue                workqueue.Interface

	// shutdownMu protects shutdown from the concurrent read/write.
	shutdownMu sync.RWMutex
	shutdown   bool
}

type Config struct {
	// DefaultBackendService is the default backend service name.  It must be specified if InternalDefaultBackend == false.
	DefaultBackendService *types.NamespacedName
	// WatchNamespace is the namespace to watch for Ingress resource updates.
	WatchNamespace string
	// NghttpxConfigMap is the name of ConfigMap resource which contains additional configuration for nghttpx.
	NghttpxConfigMap *types.NamespacedName
	// NghttpxHealthPort is the port for nghttpx health monitor endpoint.
	NghttpxHealthPort int32
	// NghttpxAPIPort is the port for nghttpx API endpoint.
	NghttpxAPIPort int32
	// NghttpxConfDir is the directory which contains nghttpx configuration files.
	NghttpxConfDir string
	// NghttpxExecPath is a path to nghttpx executable.
	NghttpxExecPath string
	// NghttpxHTTPPort is a port to listen to for HTTP (non-TLS) requests.
	NghttpxHTTPPort int32
	// NghttpxHTTPSPort is a port to listen to for HTTPS (TLS) requests.
	NghttpxHTTPSPort int32
	// DefaultTLSSecret is the default TLS Secret to enable TLS by default.
	DefaultTLSSecret *types.NamespacedName
	// IngressClassController is the name of IngressClass controller for this controller.
	IngressClassController  string
	AllowInternalIP         bool
	OCSPRespKey             string
	FetchOCSPRespFromSecret bool
	// ProxyProto toggles the use of PROXY protocol for all public-facing frontends.
	ProxyProto bool
	// PublishSvc is a namespace/name of Service whose addresses are written in Ingress resource instead of addresses of Ingress
	// controller Pod.
	PublishSvc *types.NamespacedName
	// EnableEndpointSlice tells controller to use EndpointSlices instead of Endpoints.
	EnableEndpointSlice bool
	// ReloadRate is a rate (QPS) of reloading nghttpx configuration.
	ReloadRate float64
	// ReloadBurst is the number of reload burst that can exceed ReloadRate.
	ReloadBurst int
	// NoDefaultBackendOverride, if set to true, ignores settings or rules in Ingress resource which override default backend service.
	NoDefaultBackendOverride bool
	// DeferredShutdownPeriod is a period before the controller starts shutting down when it receives shutdown signal.
	DeferredShutdownPeriod time.Duration
	// HealthzPort is a port for healthz endpoint.
	HealthzPort int32
	// InternalDefaultBackend, if true, instructs the controller to use internal default backend instead of an external one.
	InternalDefaultBackend bool
	// PodInfo is the Pod where this controller runs.
	PodInfo types.NamespacedName
}

// NewLoadBalancerController creates a controller for nghttpx loadbalancer
func NewLoadBalancerController(clientset clientset.Interface, manager nghttpx.Interface, config Config) *LoadBalancerController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: clientset.CoreV1().Events(config.WatchNamespace)})

	lbc := LoadBalancerController{
		clientset:                clientset,
		podInfo:                  config.PodInfo,
		nghttpx:                  manager,
		nghttpxConfigMap:         config.NghttpxConfigMap,
		nghttpxHealthPort:        config.NghttpxHealthPort,
		nghttpxAPIPort:           config.NghttpxAPIPort,
		nghttpxConfDir:           config.NghttpxConfDir,
		nghttpxExecPath:          config.NghttpxExecPath,
		nghttpxHTTPPort:          config.NghttpxHTTPPort,
		nghttpxHTTPSPort:         config.NghttpxHTTPSPort,
		defaultSvc:               config.DefaultBackendService,
		defaultTLSSecret:         config.DefaultTLSSecret,
		watchNamespace:           config.WatchNamespace,
		ingressClassController:   config.IngressClassController,
		allowInternalIP:          config.AllowInternalIP,
		ocspRespKey:              config.OCSPRespKey,
		fetchOCSPRespFromSecret:  config.FetchOCSPRespFromSecret,
		proxyProto:               config.ProxyProto,
		publishSvc:               config.PublishSvc,
		noDefaultBackendOverride: config.NoDefaultBackendOverride,
		deferredShutdownPeriod:   config.DeferredShutdownPeriod,
		healthzPort:              config.HealthzPort,
		internalDefaultBackend:   config.InternalDefaultBackend,
		recorder:                 eventBroadcaster.NewRecorder(scheme.Scheme, clientv1.EventSource{Component: "nghttpx-ingress-controller"}),
		syncQueue:                workqueue.New(),
		reloadRateLimiter:        flowcontrol.NewTokenBucketRateLimiter(float32(config.ReloadRate), config.ReloadBurst),
	}

	watchNSInformers := informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod, informers.WithNamespace(config.WatchNamespace))

	{
		f := watchNSInformers.Networking().V1().Ingresses()
		lbc.ingLister = f.Lister()
		lbc.ingInformer = f.Informer()
		lbc.ingInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addIngressNotification,
			UpdateFunc: lbc.updateIngressNotification,
			DeleteFunc: lbc.deleteIngressNotification,
		})
	}

	allNSInformers := informers.NewSharedInformerFactory(lbc.clientset, noResyncPeriod)

	if !config.EnableEndpointSlice {
		f := allNSInformers.Core().V1().Endpoints()
		lbc.epLister = f.Lister()
		lbc.epInformer = f.Informer()
		lbc.epInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addEndpointsNotification,
			UpdateFunc: lbc.updateEndpointsNotification,
			DeleteFunc: lbc.deleteEndpointsNotification,
		})

	} else {
		epSliceInformers := informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod,
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.LabelSelector =
					discovery.LabelManagedBy + "=endpointslice-controller.k8s.io," + discovery.LabelServiceName
			}))

		f := epSliceInformers.Discovery().V1beta1().EndpointSlices()
		lbc.epSliceLister = f.Lister()
		lbc.epSliceInformer = f.Informer()
		lbc.epSliceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addEndpointSliceNotification,
			UpdateFunc: lbc.updateEndpointSliceNotification,
			DeleteFunc: lbc.deleteEndpointSliceNotification,
		})
	}

	{
		f := allNSInformers.Core().V1().Services()
		lbc.svcLister = f.Lister()
		lbc.svcInformer = f.Informer()
		lbc.svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addServiceNotification,
			UpdateFunc: lbc.updateServiceNotification,
			DeleteFunc: lbc.deleteServiceNotification,
		})
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

	{
		f := allNSInformers.Networking().V1().IngressClasses()
		lbc.ingClassLister = f.Lister()
		lbc.ingClassInformer = f.Informer()
		lbc.ingClassInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    lbc.addIngressClassNotification,
			UpdateFunc: lbc.updateIngressClassNotification,
			DeleteFunc: lbc.deleteIngressClassNotification,
		})
	}

	var cmNamespace string
	if lbc.nghttpxConfigMap != nil {
		cmNamespace = lbc.nghttpxConfigMap.Namespace
	} else {
		// Just watch config.PodInfo.Namespace to make codebase simple
		cmNamespace = config.PodInfo.Namespace
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
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateIngressNotification(old interface{}, cur interface{}) {
	oldIng := old.(*networking.Ingress)
	curIng := cur.(*networking.Ingress)
	if !lbc.validateIngressClass(oldIng) && !lbc.validateIngressClass(curIng) {
		return
	}
	klog.V(4).Infof("Ingress %v/%v updated", curIng.Namespace, curIng.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteIngressNotification(obj interface{}) {
	ing, ok := obj.(*networking.Ingress)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
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
	lbc.enqueue()
}

func (lbc *LoadBalancerController) addIngressClassNotification(obj interface{}) {
	ingClass := obj.(*networking.IngressClass)
	klog.V(4).Infof("IngressClass %v added", ingClass.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateIngressClassNotification(old interface{}, cur interface{}) {
	ingClass := cur.(*networking.IngressClass)
	klog.V(4).Infof("IngressClass %v updated", ingClass.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteIngressClassNotification(obj interface{}) {
	ingClass, ok := obj.(*networking.IngressClass)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
			return
		}
		ingClass, ok = tombstone.Obj.(*networking.IngressClass)
		if !ok {
			klog.Errorf("Tombstone contained object that is not IngressClass %+v", obj)
			return
		}
	}
	klog.V(4).Infof("IngressClass %v deleted", ingClass.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) addEndpointsNotification(obj interface{}) {
	ep := obj.(*v1.Endpoints)
	if !lbc.endpointsReferenced(ep) {
		return
	}
	klog.V(4).Infof("Endpoints %v/%v added", ep.Namespace, ep.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateEndpointsNotification(old, cur interface{}) {
	oldEp := old.(*v1.Endpoints)
	curEp := cur.(*v1.Endpoints)
	if !lbc.endpointsReferenced(oldEp) && !lbc.endpointsReferenced(curEp) {
		return
	}
	klog.V(4).Infof("Endpoints %v/%v updated", curEp.Namespace, curEp.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteEndpointsNotification(obj interface{}) {
	ep, ok := obj.(*v1.Endpoints)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
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
	lbc.enqueue()
}

// endpointsReferenced returns true if we are interested in ep.
func (lbc *LoadBalancerController) endpointsReferenced(ep *v1.Endpoints) bool {
	return lbc.serviceReferenced(types.NamespacedName{Name: ep.Name, Namespace: ep.Namespace})
}

func (lbc *LoadBalancerController) addEndpointSliceNotification(obj interface{}) {
	es := obj.(*discovery.EndpointSlice)
	if !lbc.endpointSliceReferenced(es) {
		return
	}
	klog.V(4).Infof("EndpointSlice %v/%v added", es.Namespace, es.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateEndpointSliceNotification(old, cur interface{}) {
	oldES := old.(*discovery.EndpointSlice)
	curES := cur.(*discovery.EndpointSlice)
	if !lbc.endpointSliceReferenced(oldES) && !lbc.endpointSliceReferenced(curES) {
		return
	}
	klog.V(4).Infof("EndpointSlice %v/%v updated", curES.Namespace, curES.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteEndpointSliceNotification(obj interface{}) {
	es, ok := obj.(*discovery.EndpointSlice)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
			return
		}
		es, ok = tombstone.Obj.(*discovery.EndpointSlice)
		if !ok {
			klog.Errorf("Tombstone contained object that is not EndpointSlice %+v", obj)
			return
		}
	}
	if !lbc.endpointSliceReferenced(es) {
		return
	}
	klog.V(4).Infof("EndpointSlice %v/%v deleted", es.Namespace, es.Name)
	lbc.enqueue()
}

// endpointSliceReferenced returns true if we are interested in es.
func (lbc *LoadBalancerController) endpointSliceReferenced(es *discovery.EndpointSlice) bool {
	svcName := es.Labels[discovery.LabelServiceName]
	if svcName == "" {
		return false
	}

	return lbc.serviceReferenced(types.NamespacedName{Name: svcName, Namespace: es.Namespace})
}

func (lbc *LoadBalancerController) addServiceNotification(obj interface{}) {
	svc := obj.(*v1.Service)
	if !lbc.serviceReferenced(namespacedName(svc)) {
		return
	}
	klog.V(4).Infof("Service %v/%v added", svc.Namespace, svc.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateServiceNotification(old, cur interface{}) {
	oldSvc := old.(*v1.Service)
	curSvc := cur.(*v1.Service)
	if !lbc.serviceReferenced(namespacedName(oldSvc)) && !lbc.serviceReferenced(namespacedName(curSvc)) {
		return
	}
	klog.V(4).Infof("Service %v/%v updated", curSvc.Namespace, curSvc.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteServiceNotification(obj interface{}) {
	svc, ok := obj.(*v1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
			return
		}
		svc, ok = tombstone.Obj.(*v1.Service)
		if !ok {
			klog.Errorf("Tombstone contained object that is not Service %+v", obj)
			return
		}
	}
	if !lbc.serviceReferenced(namespacedName(svc)) {
		return
	}
	klog.V(4).Infof("Service %v/%v deleted", svc.Namespace, svc.Name)
	lbc.enqueue()
}

func namespacedName(obj metav1.Object) types.NamespacedName {
	return types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
}

// serviceReferenced returns true if we are interested in svc.
func (lbc *LoadBalancerController) serviceReferenced(svc types.NamespacedName) bool {
	if !lbc.internalDefaultBackend && svc.Namespace == lbc.defaultSvc.Namespace && svc.Name == lbc.defaultSvc.Name {
		return true
	}

	ings, err := lbc.ingLister.Ingresses(svc.Namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("Could not list Ingress namespace=%v: %v", svc.Namespace, err)
		return false
	}
	for _, ing := range ings {
		if !lbc.validateIngressClass(ing) {
			continue
		}
		if !lbc.noDefaultBackendOverride {
			if isb := getDefaultBackendService(ing); isb != nil && svc.Name == isb.Name {
				klog.V(4).Infof("Service %v/%v is referenced by Ingress %v/%v", svc.Namespace, svc.Name, ing.Namespace, ing.Name)
				return true
			}
		}
		for i := range ing.Spec.Rules {
			rule := &ing.Spec.Rules[i]
			if rule.HTTP == nil {
				continue
			}
			for i := range rule.HTTP.Paths {
				path := &rule.HTTP.Paths[i]
				if isb := path.Backend.Service; isb != nil && svc.Name == isb.Name {
					klog.V(4).Infof("Service %v/%v is referenced by Ingress %v/%v", svc.Namespace, svc.Name, ing.Namespace, ing.Name)
					return true
				}
			}
		}
	}
	return false
}

func getDefaultBackendService(ing *networking.Ingress) *networking.IngressServiceBackend {
	if ing.Spec.DefaultBackend == nil {
		return nil
	}
	return ing.Spec.DefaultBackend.Service
}

func (lbc *LoadBalancerController) addSecretNotification(obj interface{}) {
	s := obj.(*v1.Secret)
	if !lbc.secretReferenced(s) {
		return
	}

	klog.V(4).Infof("Secret %v/%v added", s.Namespace, s.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateSecretNotification(old, cur interface{}) {
	oldS := old.(*v1.Secret)
	curS := cur.(*v1.Secret)
	if !lbc.secretReferenced(oldS) && !lbc.secretReferenced(curS) {
		return
	}

	klog.V(4).Infof("Secret %v/%v updated", curS.Namespace, curS.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteSecretNotification(obj interface{}) {
	s, ok := obj.(*v1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
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
	lbc.enqueue()
}

func (lbc *LoadBalancerController) addConfigMapNotification(obj interface{}) {
	c := obj.(*v1.ConfigMap)
	if lbc.nghttpxConfigMap == nil || c.Namespace != lbc.nghttpxConfigMap.Namespace || c.Name != lbc.nghttpxConfigMap.Name {
		return
	}
	klog.V(4).Infof("ConfigMap %v/%v added", c.Namespace, c.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateConfigMapNotification(old, cur interface{}) {
	curC := cur.(*v1.ConfigMap)
	if lbc.nghttpxConfigMap == nil || curC.Namespace != lbc.nghttpxConfigMap.Namespace || curC.Name != lbc.nghttpxConfigMap.Name {
		return
	}
	// updates to configuration configmaps can trigger an update
	klog.V(4).Infof("ConfigMap %v/%v updated", curC.Namespace, curC.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteConfigMapNotification(obj interface{}) {
	c, ok := obj.(*v1.ConfigMap)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
			return
		}
		c, ok = tombstone.Obj.(*v1.ConfigMap)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a ConfigMap %+v", obj)
			return
		}
	}
	if lbc.nghttpxConfigMap == nil || c.Namespace != lbc.nghttpxConfigMap.Namespace || c.Name != lbc.nghttpxConfigMap.Name {
		return
	}
	klog.V(4).Infof("ConfigMap %v/%v deleted", c.Namespace, c.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) addPodNotification(obj interface{}) {
	pod := obj.(*v1.Pod)
	if !lbc.podReferenced(pod) {
		return
	}
	klog.V(4).Infof("Pod %v/%v added", pod.Namespace, pod.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) updatePodNotification(old, cur interface{}) {
	oldPod := old.(*v1.Pod)
	curPod := cur.(*v1.Pod)
	if !lbc.podReferenced(oldPod) && !lbc.podReferenced(curPod) {
		return
	}
	klog.V(4).Infof("Pod %v/%v updated", curPod.Namespace, curPod.Name)
	lbc.enqueue()
}

func (lbc *LoadBalancerController) deletePodNotification(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not get object from tombstone %+v", obj)
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
	lbc.enqueue()
}

// podReferenced returns true if we are interested in pod.
func (lbc *LoadBalancerController) podReferenced(pod *v1.Pod) bool {
	if !lbc.internalDefaultBackend {
		if svc, err := lbc.svcLister.Services(lbc.defaultSvc.Namespace).Get(lbc.defaultSvc.Name); err == nil {
			if labels.Set(svc.Spec.Selector).AsSelector().Matches(labels.Set(pod.Labels)) {
				klog.V(4).Infof("Pod %v/%v is referenced by default Service %v/%v", pod.Namespace, pod.Name, lbc.defaultSvc.Namespace, lbc.defaultSvc.Name)
				return true
			}
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
		if !lbc.noDefaultBackendOverride {
			if isb := getDefaultBackendService(ing); isb != nil {
				if svc, err := lbc.svcLister.Services(pod.Namespace).Get(isb.Name); err == nil {
					if labels.Set(svc.Spec.Selector).AsSelector().Matches(labels.Set(pod.Labels)) {
						klog.V(4).Infof("Pod %v/%v is referenced by Ingress %v/%v through Service %v/%v",
							pod.Namespace, pod.Name, ing.Namespace, ing.Name, svc.Namespace, svc.Name)
						return true
					}
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
				isb := path.Backend.Service
				if isb == nil {
					continue
				}
				svc, err := lbc.svcLister.Services(pod.Namespace).Get(isb.Name)
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

func (lbc *LoadBalancerController) enqueue() {
	lbc.syncQueue.Add(syncKey)
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
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(3).Infof("ConfigMap %v has been deleted", cmKey)
			return &v1.ConfigMap{}, nil
		}

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

	cm, err := lbc.getConfigMap(lbc.nghttpxConfigMap)
	if err != nil {
		return err
	}

	nghttpx.ReadConfig(ingConfig, cm)

	reloaded, err := lbc.nghttpx.CheckAndReload(ingConfig)
	if err != nil {
		return err
	}

	if !reloaded {
		klog.V(4).Infof("No need to reload configuration.")
	}

	return nil
}

func (lbc *LoadBalancerController) getDefaultUpstream() *nghttpx.Upstream {
	if lbc.internalDefaultBackend {
		script := []byte(`
class App
  def on_req(env)
    req = env.req
    resp = env.resp
    resp.add_header 'content-type', 'text/plain; charset=utf-8'
    if req.path == '/healthz' || req.path.start_with?('/healthz?')
      resp.status = 200
      resp.return 'ok'
      return
    end
    resp.status = 404
    resp.return 'default backend - 404'
  end
end

App.new
`)
		return &nghttpx.Upstream{
			Name:             "internal-default-backend",
			RedirectIfNotTLS: lbc.defaultTLSSecret != nil,
			Affinity:         nghttpx.AffinityNone,
			Mruby:            nghttpx.CreatePerPatternMrubyChecksumFile(lbc.nghttpxConfDir, script),
			DoNotForward:     true,
			Backends: []nghttpx.UpstreamServer{
				{
					Address:  "127.0.0.1",
					Port:     "9999",
					Protocol: nghttpx.ProtocolH1,
					Affinity: nghttpx.AffinityNone,
				},
			},
		}
	}

	svcKey := lbc.defaultSvc.String()
	upstream := &nghttpx.Upstream{
		Name:             svcKey,
		RedirectIfNotTLS: lbc.defaultTLSSecret != nil,
		Affinity:         nghttpx.AffinityNone,
	}
	svc, err := lbc.svcLister.Services(lbc.defaultSvc.Namespace).Get(lbc.defaultSvc.Name)
	if err != nil {
		klog.Warningf("unable to get Service %v: %v", svcKey, err)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
		return upstream
	}

	if len(svc.Spec.Ports) == 0 {
		klog.Warningf("Service %v/%v has no ports", svc.Namespace, svc.Name)
		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultServer())
		return upstream
	}

	eps := lbc.getEndpoints(svc, &svc.Spec.Ports[0], &nghttpx.PortBackendConfig{})
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

		if !lbc.noDefaultBackendOverride {
			if isb := getDefaultBackendService(ing); isb != nil {
				// This overrides the default backend specified in command-line.  It is possible that the multiple Ingress
				// resource specifies this.  But specification does not any rules how to deal with it.  Just use the one we
				// meet last.
				if ups, err := lbc.createUpstream(ing, "", "/", isb, false,
					defaultPathConfig, pathConfig, defaultPortBackendConfig, backendConfig); err != nil {
					klog.Errorf("Could not create default backend for Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
				} else {
					defaultUpstream = ups
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

				if lbc.noDefaultBackendOverride && (rule.Host == "" && (path.Path == "" || path.Path == "/")) {
					klog.Warningf("Ignore rule in Ingress %v/%v which overrides default backend", ing.Namespace, ing.Name)
					continue
				}

				isb := path.Backend.Service
				if isb == nil {
					klog.Warningf("No Service is set for path")
					continue
				}

				if ups, err := lbc.createUpstream(ing, rule.Host, path.Path, isb, requireTLS,
					defaultPathConfig, pathConfig, defaultPortBackendConfig, backendConfig); err != nil {
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

	if lbc.deferredShutdownPeriod != 0 {
		ingConfig.HealthzMruby = nghttpx.CreatePerPatternMrubyChecksumFile(lbc.nghttpxConfDir, lbc.createHealthzMruby())
	}

	return ingConfig, nil
}

func (lbc *LoadBalancerController) createHealthzMruby() []byte {
	var code int
	if lbc.ShutdownCommenced() {
		code = 503
	} else {
		code = 200
	}

	return []byte(fmt.Sprintf(`
class App
  def on_req(env)
    resp = env.resp
    resp.status = %v
    resp.return ""
  end
end

App.new
`, code))
}

// createUpstream creates new nghttpx.Upstream for ing, host, path and isb.
func (lbc *LoadBalancerController) createUpstream(ing *networking.Ingress, host, path string, isb *networking.IngressServiceBackend,
	requireTLS bool, defaultPathConfig *nghttpx.PathConfig, pathConfig map[string]*nghttpx.PathConfig, defaultPortBackendConfig *nghttpx.PortBackendConfig, backendConfig map[string]map[string]*nghttpx.PortBackendConfig) (*nghttpx.Upstream, error) {
	var normalizedPath string
	switch {
	case path == "":
		normalizedPath = "/"
	case !strings.HasPrefix(path, "/"):
		return nil, fmt.Errorf("host %v has Path which does not start /: %v", host, path)
	default:
		// nghttpx requires ':' to be percent-encoded.  Otherwise, ':' is recognized as pattern separator.
		normalizedPath = strings.ReplaceAll(path, ":", "%3A")
	}
	var portStr string
	if isb.Port.Name != "" {
		portStr = isb.Port.Name
	} else {
		portStr = strconv.FormatInt(int64(isb.Port.Number), 10)
	}
	pc := nghttpx.ResolvePathConfig(host, normalizedPath, defaultPathConfig, pathConfig)
	// The format of upsName is similar to backend option syntax of nghttpx.
	upsName := fmt.Sprintf("%v/%v,%v;%v%v", ing.Namespace, isb.Name, portStr, host, normalizedPath)
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

	svcKey := strings.Join([]string{ing.Namespace, isb.Name}, "/")
	svc, err := lbc.svcLister.Services(ing.Namespace).Get(isb.Name)
	if err != nil {
		return nil, fmt.Errorf("error getting Service %v from the cache: %v", svcKey, err)
	}

	klog.V(3).Infof("obtaining port information for Service %v", svcKey)

	svcBackendConfig := backendConfig[isb.Name]

	for i := range svc.Spec.Ports {
		servicePort := &svc.Spec.Ports[i]
		// According to the documentation, servicePort.TargetPort is optional.  If it is omitted, use servicePort.Port.
		// servicePort.TargetPort could be a string.  This is really messy.

		var key string
		switch {
		case isb.Port.Name != "":
			if isb.Port.Name != servicePort.Name {
				continue
			}
			key = isb.Port.Name
		case isb.Port.Number == servicePort.Port:
			key = strconv.FormatInt(int64(isb.Port.Number), 10)
		default:
			continue
		}

		portBackendConfig, ok := svcBackendConfig[key]
		if !ok {
			portBackendConfig = &nghttpx.PortBackendConfig{}
			if defaultPortBackendConfig != nil {
				nghttpx.ApplyDefaultPortBackendConfig(portBackendConfig, defaultPortBackendConfig)
			}
		}

		eps := lbc.getEndpoints(svc, servicePort, portBackendConfig)
		if len(eps) == 0 {
			klog.Warningf("Service %v does not have any active endpoints", svcKey)
			break
		}

		ups.Backends = append(ups.Backends, eps...)
		break
	}

	if len(ups.Backends) == 0 {
		return nil, fmt.Errorf("no backend service port found for Service %v", svcKey)
	}

	return ups, nil
}

// getTLSCredFromSecret returns nghttpx.TLSCred obtained from the Secret denoted by secretKey.
func (lbc *LoadBalancerController) getTLSCredFromSecret(key *types.NamespacedName) (*nghttpx.TLSCred, error) {
	secret, err := lbc.secretLister.Secrets(key.Namespace).Get(key.Name)
	if err != nil {
		return nil, fmt.Errorf("could not get TLS secret %v: %v", key, err)
	}
	tlsCred, err := lbc.createTLSCredFromSecret(secret)
	if err != nil {
		return nil, err
	}
	return tlsCred, nil
}

// getTLSCredFromIngress returns list of nghttpx.TLSCred obtained from Ingress resource.
func (lbc *LoadBalancerController) getTLSCredFromIngress(ing *networking.Ingress) ([]*nghttpx.TLSCred, error) {
	if len(ing.Spec.TLS) == 0 {
		return nil, nil
	}

	pems := make([]*nghttpx.TLSCred, len(ing.Spec.TLS))

	for i := range ing.Spec.TLS {
		tls := &ing.Spec.TLS[i]
		secret, err := lbc.secretLister.Secrets(ing.Namespace).Get(tls.SecretName)
		if err != nil {
			return nil, fmt.Errorf("could not retrieve Secret %v/%v for Ingress %v/%v: %v", ing.Namespace, tls.SecretName, ing.Namespace, ing.Name, err)
		}
		tlsCred, err := lbc.createTLSCredFromSecret(secret)
		if err != nil {
			return nil, err
		}

		pems[i] = tlsCred
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

	var err error

	cert, err = nghttpx.NormalizePEM(cert)
	if err != nil {
		return nil, fmt.Errorf("could not normalize certificate in Secret %v/%v: %v", secret.Namespace, secret.Name, err)
	}

	key, err = nghttpx.NormalizePEM(key)
	if err != nil {
		return nil, fmt.Errorf("could not normalize private key in Secret %v/%v: %v", secret.Namespace, secret.Name, err)
	}

	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return nil, err
	}

	if err := nghttpx.VerifyCertificate(cert); err != nil {
		return nil, err
	}

	// OCSP response in TLS secret is optional feature.
	return nghttpx.CreateTLSCred(lbc.nghttpxConfDir, nghttpx.TLSCredPrefix(secret), cert, key, secret.Data[lbc.ocspRespKey]), nil
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

// getEndpoints returns a list of UpstreamServer for a given service.  portBackendConfig is additional per-port configuration for backend,
// which must not be nil.
func (lbc *LoadBalancerController) getEndpoints(svc *v1.Service, svcPort *v1.ServicePort, portBackendConfig *nghttpx.PortBackendConfig) []nghttpx.UpstreamServer {
	if svcPort.Protocol != "" && svcPort.Protocol != v1.ProtocolTCP {
		klog.Infof("Service %v/%v has unsupported protocol %v", svc.Namespace, svc.Name, svcPort.Protocol)
		return nil
	}

	klog.V(3).Infof("getting endpoints for Service %v/%v and port %v target port %v",
		svc.Namespace, svc.Name, svcPort.Port, svcPort.TargetPort.String())

	switch {
	case lbc.epLister != nil:
		return lbc.getEndpointsFromEndpoints(svc, svcPort, portBackendConfig)
	case lbc.epSliceLister != nil:
		return lbc.getEndpointsFromEndpointSlice(svc, svcPort, portBackendConfig)
	default:
		panic("unreachable")
	}
}

func (lbc *LoadBalancerController) getEndpointsFromEndpoints(svc *v1.Service, svcPort *v1.ServicePort, portBackendConfig *nghttpx.PortBackendConfig) []nghttpx.UpstreamServer {
	ep, err := lbc.epLister.Endpoints(svc.Namespace).Get(svc.Name)
	if err != nil {
		klog.Warningf("unexpected error obtaining service endpoints: %v", err)
		return nil
	}

	var upsServers []nghttpx.UpstreamServer

	for i := range ep.Subsets {
		ss := &ep.Subsets[i]
		for i := range ss.Ports {
			port := &ss.Ports[i]

			if port.Protocol != "" && port.Protocol != v1.ProtocolTCP {
				continue
			}

			epPort := &discovery.EndpointPort{
				Port: &port.Port,
			}

			for i := range ss.Addresses {
				epAddr := &ss.Addresses[i]
				ref := epAddr.TargetRef
				if ref == nil || ref.Kind != "Pod" {
					continue
				}

				targetPort, err := lbc.resolveTargetPort(svcPort, epPort, ref)
				if err != nil {
					klog.Warningf("unable to get target port from Pod %v/%v for ServicePort %v and EndpointPort %v: %v",
						ref.Namespace, ref.Name, svcPort, epPort, err)
					continue
				}

				upsServers = append(upsServers, lbc.createUpstreamServer(svc, epAddr.IP, targetPort, portBackendConfig))
			}
		}
	}

	klog.V(3).Infof("endpoints found: %+v", upsServers)
	return upsServers
}

func (lbc *LoadBalancerController) getEndpointsFromEndpointSlice(svc *v1.Service, svcPort *v1.ServicePort, portBackendConfig *nghttpx.PortBackendConfig) []nghttpx.UpstreamServer {
	ess, err := lbc.epSliceLister.EndpointSlices(svc.Namespace).List(newEndpointSliceSelector(svc))
	if err != nil {
		klog.Warningf("unexpected error obtaining EndpointSlice: %v", err)
		return nil
	}

	var upsServers []nghttpx.UpstreamServer

	for _, es := range ess {
		switch es.AddressType {
		case "IPv4", "IPv6":
		default:
			klog.Warningf("EndpointSlice %v/%v has unsupported address type %v", es.Namespace, es.Name, es.AddressType)
			continue
		}

		if len(es.Ports) == 0 {
			klog.Warningf("EndpointSlice %v/%v has no port defined", es.Namespace, es.Name)
			continue
		}

		for i := range es.Ports {
			epPort := &es.Ports[i]

			if epPort.Protocol != nil && *epPort.Protocol != v1.ProtocolTCP {
				continue
			}

			for i := range es.Endpoints {
				ep := &es.Endpoints[i]
				if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
					continue
				}
				ref := ep.TargetRef
				if ref == nil || ref.Kind != "Pod" {
					continue
				}

				targetPort, err := lbc.resolveTargetPort(svcPort, epPort, ref)
				if err != nil {
					klog.Warningf("unable to get target port from Pod %v/%v for ServicePort %v and EndpointPort %v: %v",
						ref.Namespace, ref.Name, svcPort, epPort, err)
					continue
				}

				// TODO We historically added all addresses in Endpoints code.  Not sure we should just pick one here
				// instead.
				for _, addr := range ep.Addresses {
					upsServers = append(upsServers, lbc.createUpstreamServer(svc, addr, targetPort, portBackendConfig))
				}
			}
		}
	}

	klog.V(3).Infof("endpoints found: %+v", upsServers)
	return upsServers
}

func newEndpointSliceSelector(svc *v1.Service) labels.Selector {
	return labels.SelectorFromSet(labels.Set{
		discovery.LabelServiceName: svc.Name,
	})
}

func (lbc *LoadBalancerController) createUpstreamServer(svc *v1.Service, address string, targetPort int32, portBackendConfig *nghttpx.PortBackendConfig) nghttpx.UpstreamServer {
	ups := nghttpx.UpstreamServer{
		Address:  address,
		Port:     strconv.Itoa(int(targetPort)),
		Protocol: portBackendConfig.GetProto(),
		TLS:      portBackendConfig.GetTLS(),
		SNI:      portBackendConfig.GetSNI(),
		DNS:      portBackendConfig.GetDNS(),
		Group:    strings.Join([]string{svc.Namespace, svc.Name}, "/"),
		Weight:   portBackendConfig.GetWeight(),
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

	return ups
}

// resolveTargetPort returns endpoint port.  This function verifies that endpoint port given in epPort matches the svcPort.  If svcPort is
// not a number, a port is looked up by referencing Pod denoted by ref.
func (lbc *LoadBalancerController) resolveTargetPort(svcPort *v1.ServicePort, epPort *discovery.EndpointPort, ref *v1.ObjectReference) (int32, error) {
	if epPort.Port == nil {
		return 0, errors.New("EndpointPort has no port defined")
	}

	switch {
	case svcPort.TargetPort.IntVal != 0:
		if *epPort.Port == svcPort.TargetPort.IntVal {
			return *epPort.Port, nil
		}
	case svcPort.TargetPort.StrVal != "":
		port, err := lbc.getNamedPortFromPod(ref, svcPort)
		if err != nil {
			return 0, fmt.Errorf("could not find named port %v in Pod spec: %v", svcPort.TargetPort.String(), err)
		}
		if *epPort.Port == port {
			return *epPort.Port, nil
		}
	default:
		// svcPort.TargetPort is not specified.
		if *epPort.Port == svcPort.Port {
			return *epPort.Port, nil
		}
	}

	return 0, errors.New("no matching port found")
}

// getNamedPortFromPod returns port number from Pod denoted by ref which shares the same port name with servicePort.
func (lbc *LoadBalancerController) getNamedPortFromPod(ref *v1.ObjectReference, servicePort *v1.ServicePort) (int32, error) {
	pod, err := lbc.podLister.Pods(ref.Namespace).Get(ref.Name)
	if err != nil {
		return 0, fmt.Errorf("could not get Pod %v/%v: %v", ref.Namespace, ref.Name, err)
	}

	port, err := podFindPort(pod, servicePort)
	if err != nil {
		return 0, fmt.Errorf("could not find port %v from Pod %v/%v: %v", servicePort.TargetPort.String(), pod.Namespace, pod.Name, err)
	}
	return port, nil
}

// startShutdown commences shutting down the loadbalancer controller.
func (lbc *LoadBalancerController) startShutdown() {
	lbc.shutdownMu.Lock()
	defer lbc.shutdownMu.Unlock()

	// Only try draining the workqueue if we haven't already.
	if lbc.shutdown {
		klog.Infof("Shutting down is already in progress")
		return
	}

	lbc.shutdown = true
}

// ShutdownCommenced returns true if the controller is shutting down.  This includes deferred shutdown period.
func (lbc *LoadBalancerController) ShutdownCommenced() bool {
	lbc.shutdownMu.RLock()
	defer lbc.shutdownMu.RUnlock()

	return lbc.shutdown
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run(ctx context.Context) {
	klog.Infof("Starting nghttpx loadbalancer controller")

	ctrlCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := lbc.nghttpx.Start(ctrlCtx, lbc.nghttpxExecPath, nghttpx.ConfigPath(lbc.nghttpxConfDir)); err != nil {
			klog.Errorf("Could not start nghttpx: %v", err)
		}
	}()

	go lbc.ingInformer.Run(ctrlCtx.Done())
	go lbc.svcInformer.Run(ctrlCtx.Done())
	go lbc.secretInformer.Run(ctrlCtx.Done())
	go lbc.cmInformer.Run(ctrlCtx.Done())
	go lbc.podInformer.Run(ctrlCtx.Done())
	go lbc.nodeInformer.Run(ctrlCtx.Done())
	go lbc.ingClassInformer.Run(ctrlCtx.Done())

	hasSynced := []cache.InformerSynced{
		lbc.ingInformer.HasSynced,
		lbc.svcInformer.HasSynced,
		lbc.secretInformer.HasSynced,
		lbc.cmInformer.HasSynced,
		lbc.podInformer.HasSynced,
		lbc.nodeInformer.HasSynced,
		lbc.ingClassInformer.HasSynced,
	}

	if lbc.epInformer != nil {
		go lbc.epInformer.Run(ctrlCtx.Done())
		hasSynced = append(hasSynced, lbc.epInformer.HasSynced)
	}
	if lbc.epSliceInformer != nil {
		go lbc.epSliceInformer.Run(ctrlCtx.Done())
		hasSynced = append(hasSynced, lbc.epSliceInformer.HasSynced)
	}

	if !cache.WaitForCacheSync(ctrlCtx.Done(), hasSynced...) {
		return
	}

	go lbc.worker()

	wg.Add(1)
	go func() {
		defer wg.Done()
		lbc.syncIngress(ctrlCtx)
	}()

	go func() {
		<-ctx.Done()

		lbc.startShutdown()

		if lbc.deferredShutdownPeriod != 0 {
			klog.Infof("Deferred shutdown period is %v", lbc.deferredShutdownPeriod)

			lbc.enqueue()

			<-time.After(lbc.deferredShutdownPeriod)
		}

		klog.Infof("Commencing shutting down")

		cancel()
	}()

	<-ctrlCtx.Done()

	klog.Infof("Shutting down nghttpx loadbalancer controller")

	lbc.syncQueue.ShutDown()

	wg.Wait()
}

func (lbc *LoadBalancerController) retryOrForget(key interface{}, requeue bool) {
	if requeue {
		lbc.syncQueue.Add(key)
	}
}

// validateIngressClass checks whether this controller should process ing or not.
func (lbc *LoadBalancerController) validateIngressClass(ing *networking.Ingress) bool {
	if ing.Spec.IngressClassName != nil {
		ingClass, err := lbc.ingClassLister.Get(*ing.Spec.IngressClassName)
		if err != nil {
			klog.Errorf("Could not get IngressClass %v: %v", *ing.Spec.IngressClassName, err)
			return false
		}
		if ingClass.Spec.Controller != lbc.ingressClassController {
			klog.V(4).Infof("Skip Ingress %v/%v which needs IngressClass %v controller %v", ing.Namespace, ing.Name,
				ingClass.Name, ingClass.Spec.Controller)
			return false
		}
		return true
	}

	// Check defaults

	ingClasses, err := lbc.ingClassLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Could not list IngressClass: %v", err)
		return false
	}

	for _, ingClass := range ingClasses {
		if ingClass.Annotations[annotationIsDefaultIngressClass] != "true" {
			continue
		}

		if ingClass.Spec.Controller != lbc.ingressClassController {
			klog.V(4).Infof("Skip Ingress %v/%v because it defaults to IngressClass %v controller %v", ing.Namespace, ing.Name,
				ingClass.Name, ingClass.Spec.Controller)
			return false
		}
		return true
	}

	// If there is no default IngressClass, process the Ingress.
	return true
}

// syncIngress updates Ingress resource status.
func (lbc *LoadBalancerController) syncIngress(ctx context.Context) {
	for {
		if err := lbc.getLoadBalancerIngressAndUpdateIngress(ctx); err != nil {
			klog.Errorf("Could not update Ingress status: %v", err)
		}

		select {
		case <-ctx.Done():
			if err := lbc.removeAddressFromLoadBalancerIngress(); err != nil {
				klog.Errorf("Could not remove address from LoadBalancerIngress: %v", err)
			}
			return
		case <-time.After(time.Duration(float64(30*time.Second) * (rand.Float64() + 1))):
		}
	}
}

// getLoadBalancerIngressAndUpdateIngress gets addresses to set in Ingress resource, and updates Ingress Status with them.
func (lbc *LoadBalancerController) getLoadBalancerIngressAndUpdateIngress(ctx context.Context) error {
	var lbIngs []v1.LoadBalancerIngress

	if lbc.publishSvc == nil {
		thisPod, err := lbc.getThisPod()
		if err != nil {
			return err
		}

		selector := labels.Set(thisPod.Labels).AsSelector()
		lbIngs, err = lbc.getLoadBalancerIngress(selector)
		if err != nil {
			return fmt.Errorf("could not get Node IP of Ingress controller: %v", err)
		}
	} else {
		svc, err := lbc.svcLister.Services(lbc.publishSvc.Namespace).Get(lbc.publishSvc.Name)
		if err != nil {
			return err
		}

		lbIngs = lbc.getLoadBalancerIngressFromService(svc)
	}

	sortLoadBalancerIngress(lbIngs)

	return lbc.updateIngressStatus(ctx, uniqLoadBalancerIngress(lbIngs))
}

// getThisPod returns this controller's pod.
func (lbc *LoadBalancerController) getThisPod() (*v1.Pod, error) {
	pod, err := lbc.podLister.Pods(lbc.podInfo.Namespace).Get(lbc.podInfo.Name)
	if err != nil {
		return nil, fmt.Errorf("could not get Pod %v from lister: %v", lbc.podInfo, err)
	}
	return pod, nil
}

// updateIngressStatus updates LoadBalancerIngress field of all Ingresses.
func (lbc *LoadBalancerController) updateIngressStatus(ctx context.Context, lbIngs []v1.LoadBalancerIngress) error {

	ings, err := lbc.ingLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("could not list Ingress: %v", err)
	}

	for _, ing := range ings {
		select {
		case <-ctx.Done():
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

		klog.V(4).Infof("Update Ingress %v/%v .Status.LoadBalancer.Ingress to %#v", ing.Namespace, ing.Name, lbIngs)

		newIng := ing.DeepCopy()
		newIng.Status.LoadBalancer.Ingress = lbIngs

		if _, err := lbc.clientset.NetworkingV1().Ingresses(ing.Namespace).UpdateStatus(context.TODO(), newIng, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("Could not update Ingress %v/%v status: %v", ing.Namespace, ing.Name, err)
		}
	}

	return nil
}

// getLoadBalancerIngress creates array of v1.LoadBalancerIngress based on cached Pods and Nodes.
func (lbc *LoadBalancerController) getLoadBalancerIngress(selector labels.Selector) ([]v1.LoadBalancerIngress, error) {
	pods, err := lbc.podLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("could not list Pods with label %v", selector)
	}

	if len(pods) == 0 {
		return nil, nil
	}

	lbIngs := make([]v1.LoadBalancerIngress, 0, len(pods))

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
//
// TODO: This function actually returns the IP address of Node where pod is deployed rather than the address of pod.  Should we return the
// address of pod if hostNetwork is true?
func (lbc *LoadBalancerController) getPodAddress(pod *v1.Pod) (string, error) {
	node, err := lbc.nodeLister.Get(pod.Spec.NodeName)
	if err != nil {
		return "", fmt.Errorf("could not get Node %v for Pod %v/%v from lister: %v", pod.Spec.NodeName, pod.Namespace, pod.Name, err)
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
		return fmt.Errorf("could not remove address from LoadBalancerIngress: %v", err)
	}

	addr, err := lbc.getPodAddress(thisPod)
	if err != nil {
		return fmt.Errorf("could not remove address from LoadBalancerIngress: %v", err)
	}

	ings, err := lbc.ingLister.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("could not remove address from LoadBalancerIngress: %v", err)
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

			if _, err := lbc.clientset.NetworkingV1().Ingresses(newIng.Namespace).UpdateStatus(context.TODO(), newIng, metav1.UpdateOptions{}); err != nil {
				return err
			}
			return nil
		}); err != nil {
			klog.Errorf("Could not remove address from LoadBalancerIngress from Ingress %v/%v: %v", ing.Namespace, ing.Name, err)
		}
	}

	return nil
}
