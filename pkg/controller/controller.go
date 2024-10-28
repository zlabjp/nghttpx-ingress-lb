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
	"bytes"
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	listersdiscoveryv1 "k8s.io/client-go/listers/discovery/v1"
	listersnetworkingv1 "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/client-go/util/workqueue"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/klog/v2"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gatewayinformers "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"
	gatewaylistersv1 "sigs.k8s.io/gateway-api/pkg/client/listers/apis/v1"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
	slicesutil "github.com/zlabjp/nghttpx-ingress-lb/pkg/util/slices"
)

const (
	noResyncPeriod = 0

	// nghttpxQUICKeyingMaterialsSecretKey is a field name of QUIC keying materials in Secret.
	nghttpxQUICKeyingMaterialsSecretKey = "nghttpx-quic-keying-materials"
	// quicKeyingMaterialsUpdateTimestampKey is an annotation key which is associated to the value that contains the timestamp when QUIC
	// secret is last updated.
	quicKeyingMaterialsUpdateTimestampKey = "ingress.zlab.co.jp/quic-keying-materials-update-timestamp"

	// nghttpxTLSTicketKeySecretKey is a field name of TLS ticket keys in Secret.
	nghttpxTLSTicketKeySecretKey = "nghttpx-tls-ticket-key"
	// tlsTicketKeyUpdateTimestampKey is an annotation key which is associated to the value that contains the timestamp when TLS ticket
	// keys are last updated.
	tlsTicketKeyUpdateTimestampKey = "ingress.zlab.co.jp/tls-ticket-key-update-timestamp"

	// certificateGarbageCollectionPeriod is the period between garbage collection against certificate cache is performed.
	certificateGarbageCollectionPeriod = time.Hour
)

var (
	// syncKey is a key to put into the queue.  Since we create load balancer configuration using all available information, it is
	// suffice to queue only one item.  Further, queue is somewhat overkill here, but we just keep using it for simplicity.
	syncKey = struct{}{}
)

// serverReloader is the API to update underlying load balancer.
type serverReloader interface {
	// Start starts an nghttpx process using executable at path with configuration file at confPath, and waits for the process to finish.
	// If ctx is canceled, kill nghttpx process, and return.
	Start(ctx context.Context, path, confPath string) error
	// CheckAndReload checks whether the nghttpx configuration changed, and if so, make nghttpx reload its configuration.  If reloading
	// is required, and it successfully issues reloading, returns true.  If there is no need to reloading, it returns false.  On error,
	// it returns false, and non-nil error.
	CheckAndReload(ctx context.Context, ingressCfg *nghttpx.IngressConfig) (bool, error)
}

// LoadBalancerController watches the kubernetes api and adds/removes services from the loadbalancer
type LoadBalancerController struct {
	clientset        clientset.Interface
	gatewayClientset gatewayclientset.Interface

	watchNSInformers informers.SharedInformerFactory
	allNSInformers   informers.SharedInformerFactory
	cmNSInformers    informers.SharedInformerFactory
	gatewayInformers gatewayinformers.SharedInformerFactory

	// For tests
	ingIndexer          cache.Indexer
	ingClassIndexer     cache.Indexer
	epSliceIndexer      cache.Indexer
	svcIndexer          cache.Indexer
	secretIndexer       cache.Indexer
	cmIndexer           cache.Indexer
	podIndexer          cache.Indexer
	gatewayClassIndexer cache.Indexer
	gatewayIndexer      cache.Indexer
	httpRouteIndexer    cache.Indexer

	ingLister                               listersnetworkingv1.IngressLister
	ingClassLister                          listersnetworkingv1.IngressClassLister
	svcLister                               listerscorev1.ServiceLister
	epSliceLister                           listersdiscoveryv1.EndpointSliceLister
	secretLister                            listerscorev1.SecretLister
	cmLister                                listerscorev1.ConfigMapLister
	podLister                               listerscorev1.PodLister
	gatewayClassLister                      gatewaylistersv1.GatewayClassLister
	gatewayLister                           gatewaylistersv1.GatewayLister
	httpRouteLister                         gatewaylistersv1.HTTPRouteLister
	nghttpx                                 serverReloader
	pod                                     *corev1.Pod
	defaultSvc                              *types.NamespacedName
	nghttpxConfigMap                        *types.NamespacedName
	defaultTLSSecret                        *types.NamespacedName
	publishService                          *types.NamespacedName
	nghttpxHealthPort                       int32
	nghttpxAPIPort                          int32
	nghttpxConfDir                          string
	nghttpxExecPath                         string
	nghttpxHTTPPort                         int32
	nghttpxHTTPSPort                        int32
	nghttpxWorkers                          int32
	nghttpxWorkerProcessGraceShutdownPeriod time.Duration
	nghttpxMaxWorkerProcesses               int32
	watchNamespace                          string
	ingressClassController                  string
	allowInternalIP                         bool
	ocspRespKey                             string
	fetchOCSPRespFromSecret                 bool
	proxyProto                              bool
	noDefaultBackendOverride                bool
	deferredShutdownPeriod                  time.Duration
	healthzPort                             int32
	internalDefaultBackend                  bool
	http3                                   bool
	shareTLSTicketKey                       bool
	nghttpxSecret                           types.NamespacedName
	reconcileTimeout                        time.Duration
	leaderElectionConfig                    componentbaseconfig.LeaderElectionConfiguration
	requireIngressClass                     bool
	tlsTicketKeyPeriod                      time.Duration
	quicSecretPeriod                        time.Duration
	gatewayAPI                              bool
	gatewayClassController                  gatewayv1.GatewayController
	reloadRateLimiter                       flowcontrol.RateLimiter
	eventRecorder                           events.EventRecorder
	syncQueue                               workqueue.TypedInterface[struct{}]

	// shutdownMu protects shutdown from the concurrent read/write.
	shutdownMu sync.RWMutex
	shutdown   bool

	// certCacheMu protects certCache from the concurrent read/write.
	certCacheMu sync.Mutex
	certCache   map[types.NamespacedName]*certificateCacheEntry
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
	// NghttpxWorkers is the number of nghttpx worker threads.
	NghttpxWorkers int32
	// NghttpxWorkerProcessGraceShutdownPeriod is the maximum period for an nghttpx worker process to terminate gracefully.
	NghttpxWorkerProcessGraceShutdownPeriod time.Duration
	// NghttpxMaxWorkerProcesses is the maximum number of nghttpx worker processes which are spawned in every configuration reload.
	NghttpxMaxWorkerProcesses int32
	// NghttpxSecret is the Secret resource which contains secrets used by nghttpx.
	NghttpxSecret types.NamespacedName
	// DefaultTLSSecret is the default TLS Secret to enable TLS by default.
	DefaultTLSSecret *types.NamespacedName
	// IngressClassController is the name of IngressClass controller for this controller.
	IngressClassController  string
	AllowInternalIP         bool
	OCSPRespKey             string
	FetchOCSPRespFromSecret bool
	// ProxyProto toggles the use of PROXY protocol for all public-facing frontends.
	ProxyProto bool
	// PublishService is a namespace/name of Service whose addresses are written in Ingress resource instead of addresses of Ingress
	// controller Pod.
	PublishService *types.NamespacedName
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
	// HTTP3, if true, enables HTTP/3.
	HTTP3 bool
	// ShareTLSTicketKey, if true, shares TLS ticket key among ingress controllers via Secret.
	ShareTLSTicketKey bool
	// ReconcileTimeout is a timeout for a single reconciliation.  It is a safe guard to prevent a reconciliation from getting stuck
	// indefinitely.
	ReconcileTimeout time.Duration
	// LeaderElectionConfig is the configuration of leader election.
	LeaderElectionConfig componentbaseconfig.LeaderElectionConfiguration
	// RequireIngressClass, if set to true, ignores Ingress resource which does not specify .spec.ingressClassName.
	RequireIngressClass bool
	// TLSTicketKeyPeriod is the duration before TLS ticket keys are rotated and new key is generated.
	TLSTicketKeyPeriod time.Duration
	// QUICSecretPeriod is the duration before QUIC keying materials are rotated and new key is generated.
	QUICSecretPeriod time.Duration
	// GatewayAPI, if true, enables Gateway API.
	GatewayAPI bool
	// GatewayClassController is the name of GatewayClass controller for this controller.
	GatewayClassController gatewayv1.GatewayController
	// Pod is the Pod where this controller runs.
	Pod *corev1.Pod
	// EventRecorder is the event recorder.
	EventRecorder events.EventRecorder
}

// NewLoadBalancerController creates a controller for nghttpx loadbalancer
func NewLoadBalancerController(ctx context.Context, clientset clientset.Interface, gatewayClientset gatewayclientset.Interface,
	nghttpx serverReloader, config Config) (*LoadBalancerController, error) {
	log := klog.LoggerWithName(klog.FromContext(ctx), "loadBalancerController")

	ctx = klog.NewContext(ctx, log)

	lbc := LoadBalancerController{
		clientset:                               clientset,
		gatewayClientset:                        gatewayClientset,
		watchNSInformers:                        informers.NewSharedInformerFactoryWithOptions(clientset, noResyncPeriod, informers.WithNamespace(config.WatchNamespace)),
		allNSInformers:                          informers.NewSharedInformerFactory(clientset, noResyncPeriod),
		pod:                                     config.Pod,
		nghttpx:                                 nghttpx,
		nghttpxConfigMap:                        config.NghttpxConfigMap,
		nghttpxHealthPort:                       config.NghttpxHealthPort,
		nghttpxAPIPort:                          config.NghttpxAPIPort,
		nghttpxConfDir:                          config.NghttpxConfDir,
		nghttpxExecPath:                         config.NghttpxExecPath,
		nghttpxHTTPPort:                         config.NghttpxHTTPPort,
		nghttpxHTTPSPort:                        config.NghttpxHTTPSPort,
		nghttpxWorkers:                          config.NghttpxWorkers,
		nghttpxWorkerProcessGraceShutdownPeriod: config.NghttpxWorkerProcessGraceShutdownPeriod,
		nghttpxMaxWorkerProcesses:               config.NghttpxMaxWorkerProcesses,
		nghttpxSecret:                           config.NghttpxSecret,
		defaultSvc:                              config.DefaultBackendService,
		defaultTLSSecret:                        config.DefaultTLSSecret,
		watchNamespace:                          config.WatchNamespace,
		ingressClassController:                  config.IngressClassController,
		allowInternalIP:                         config.AllowInternalIP,
		ocspRespKey:                             config.OCSPRespKey,
		fetchOCSPRespFromSecret:                 config.FetchOCSPRespFromSecret,
		proxyProto:                              config.ProxyProto,
		publishService:                          config.PublishService,
		noDefaultBackendOverride:                config.NoDefaultBackendOverride,
		deferredShutdownPeriod:                  config.DeferredShutdownPeriod,
		healthzPort:                             config.HealthzPort,
		internalDefaultBackend:                  config.InternalDefaultBackend,
		http3:                                   config.HTTP3,
		shareTLSTicketKey:                       config.ShareTLSTicketKey,
		reconcileTimeout:                        config.ReconcileTimeout,
		leaderElectionConfig:                    config.LeaderElectionConfig,
		requireIngressClass:                     config.RequireIngressClass,
		tlsTicketKeyPeriod:                      config.TLSTicketKeyPeriod,
		quicSecretPeriod:                        config.QUICSecretPeriod,
		gatewayAPI:                              config.GatewayAPI,
		gatewayClassController:                  config.GatewayClassController,
		eventRecorder:                           config.EventRecorder,
		syncQueue:                               workqueue.NewTyped[struct{}](),
		reloadRateLimiter:                       flowcontrol.NewTokenBucketRateLimiter(float32(config.ReloadRate), config.ReloadBurst),
		certCache:                               make(map[types.NamespacedName]*certificateCacheEntry),
	}

	{
		f := lbc.watchNSInformers.Networking().V1().Ingresses()
		lbc.ingLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*networkingv1.Ingress]{
			validate: func(ctx context.Context, ing *networkingv1.Ingress, _ resourceEvent) bool {
				return lbc.validateIngressClass(ctx, ing)
			},
			handler: func(context.Context, *networkingv1.Ingress) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Ingress event handler")
			return nil, err
		}

		lbc.ingIndexer = inf.GetIndexer()
	}

	{
		f := lbc.allNSInformers.Discovery().V1().EndpointSlices()
		lbc.epSliceLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*discoveryv1.EndpointSlice]{
			validate: func(ctx context.Context, es *discoveryv1.EndpointSlice, _ resourceEvent) bool {
				return lbc.endpointSliceReferenced(ctx, es)
			},
			handler: func(context.Context, *discoveryv1.EndpointSlice) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add EndpointSlice event handler")
			return nil, err
		}

		lbc.epSliceIndexer = inf.GetIndexer()
	}

	{
		f := lbc.allNSInformers.Core().V1().Services()
		lbc.svcLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.Service]{
			validate: func(ctx context.Context, svc *corev1.Service, _ resourceEvent) bool {
				return lbc.serviceReferenced(ctx, namespacedName(svc))
			},
			handler: func(context.Context, *corev1.Service) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Service event handler")
			return nil, err
		}

		lbc.svcIndexer = inf.GetIndexer()
	}

	{
		f := lbc.allNSInformers.Core().V1().Secrets()
		lbc.secretLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.Secret]{
			validate: func(ctx context.Context, secret *corev1.Secret, _ resourceEvent) bool {
				return lbc.secretReferenced(ctx, secret)
			},
			handler: func(context.Context, *corev1.Secret) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Secret event handler")
			return nil, err
		}

		lbc.secretIndexer = inf.GetIndexer()
	}

	{
		f := lbc.allNSInformers.Core().V1().Pods()
		lbc.podLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.Pod]{
			validate: func(ctx context.Context, po *corev1.Pod, _ resourceEvent) bool {
				return lbc.podReferenced(ctx, po)
			},
			handler: func(context.Context, *corev1.Pod) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Pod event handler")
			return nil, err
		}

		lbc.podIndexer = inf.GetIndexer()
	}

	{
		f := lbc.allNSInformers.Networking().V1().IngressClasses()
		lbc.ingClassLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*networkingv1.IngressClass]{
			handler: func(context.Context, *networkingv1.IngressClass) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add IngressClass event handler")
			return nil, err
		}

		lbc.ingClassIndexer = inf.GetIndexer()
	}

	if lbc.nghttpxConfigMap != nil {
		lbc.cmNSInformers = informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod,
			informers.WithNamespace(lbc.nghttpxConfigMap.Namespace),
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.FieldSelector = "metadata.name=" + lbc.nghttpxConfigMap.Name
			}),
		)
		f := lbc.cmNSInformers.Core().V1().ConfigMaps()
		lbc.cmLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.ConfigMap]{
			handler: func(context.Context, *corev1.ConfigMap) {
				lbc.enqueue()
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add ConfigMap event handler")
			return nil, err
		}

		lbc.cmIndexer = inf.GetIndexer()
	}

	if lbc.gatewayAPI {
		lbc.gatewayInformers = gatewayinformers.NewSharedInformerFactoryWithOptions(
			gatewayClientset, noResyncPeriod, gatewayinformers.WithNamespace(config.WatchNamespace))

		{
			f := lbc.gatewayInformers.Gateway().V1().GatewayClasses()
			lbc.gatewayClassLister = f.Lister()
			inf := f.Informer()

			if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*gatewayv1.GatewayClass]{
				validate: func(_ context.Context, gc *gatewayv1.GatewayClass, _ resourceEvent) bool {
					return gc.Spec.ControllerName == lbc.gatewayClassController
				},
				handler: func(context.Context, *gatewayv1.GatewayClass) {
					lbc.enqueue()
				},
			}.Build(ctx)); err != nil {
				log.Error(err, "Unable to add GatewayClass event handler")
				return nil, err
			}

			lbc.gatewayClassIndexer = inf.GetIndexer()
		}

		{
			f := lbc.gatewayInformers.Gateway().V1().Gateways()
			lbc.gatewayLister = f.Lister()
			inf := f.Informer()

			if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*gatewayv1.Gateway]{
				validate: func(ctx context.Context, gtw *gatewayv1.Gateway, _ resourceEvent) bool {
					return lbc.validateGatewayGatewayClass(ctx, gtw)
				},
				handler: func(context.Context, *gatewayv1.Gateway) {
					lbc.enqueue()
				},
			}.Build(ctx)); err != nil {
				log.Error(err, "Unable to add Gateway event handler")
				return nil, err
			}

			lbc.gatewayIndexer = inf.GetIndexer()
		}

		{
			f := lbc.gatewayInformers.Gateway().V1().HTTPRoutes()
			lbc.httpRouteLister = f.Lister()
			inf := f.Informer()

			if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*gatewayv1.HTTPRoute]{
				validate: func(ctx context.Context, httpRoute *gatewayv1.HTTPRoute, _ resourceEvent) bool {
					return lbc.validateHTTPRouteGatewayClass(ctx, httpRoute)
				},
				handler: func(context.Context, *gatewayv1.HTTPRoute) {
					lbc.enqueue()
				},
			}.Build(ctx)); err != nil {
				log.Error(err, "Unable to add HTTPRoute event handler")
				return nil, err
			}

			lbc.httpRouteIndexer = inf.GetIndexer()
		}
	}

	return &lbc, nil
}

// endpointSliceReferenced returns true if we are interested in es.
func (lbc *LoadBalancerController) endpointSliceReferenced(ctx context.Context, es *discoveryv1.EndpointSlice) bool {
	svcName := es.Labels[discoveryv1.LabelServiceName]

	return svcName != "" && lbc.serviceReferenced(ctx, types.NamespacedName{Name: svcName, Namespace: es.Namespace})
}

func namespacedName(obj metav1.Object) types.NamespacedName {
	return types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
}

// serviceReferenced returns true if we are interested in svc.
func (lbc *LoadBalancerController) serviceReferenced(ctx context.Context, svc types.NamespacedName) bool {
	log := klog.FromContext(ctx)

	if !lbc.internalDefaultBackend && svc.Namespace == lbc.defaultSvc.Namespace && svc.Name == lbc.defaultSvc.Name {
		return true
	}

	ings, err := lbc.ingLister.Ingresses(svc.Namespace).List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list Ingress", "service", svc)
		return false
	}

	return slices.ContainsFunc(ings, func(ing *networkingv1.Ingress) bool {
		if !lbc.validateIngressClass(ctx, ing) {
			return false
		}

		if !lbc.noDefaultBackendOverride {
			if isb := getDefaultBackendService(ing); isb != nil && svc.Name == isb.Name {
				log.V(4).Info("Referenced by Ingress", "service", svc, "ingress", klog.KObj(ing))
				return true
			}
		}

		return slicesutil.ContainsPtrFunc(ing.Spec.Rules, func(rule *networkingv1.IngressRule) bool {
			return rule.HTTP != nil &&
				slicesutil.ContainsPtrFunc(rule.HTTP.Paths, func(path *networkingv1.HTTPIngressPath) bool {
					if isb := path.Backend.Service; isb != nil && svc.Name == isb.Name {
						log.V(4).Info("Referenced by Ingress", "service", svc, "ingress", klog.KObj(ing))
						return true
					}

					return false
				})
		})
	})
}

func getDefaultBackendService(ing *networkingv1.Ingress) *networkingv1.IngressServiceBackend {
	if ing.Spec.DefaultBackend == nil {
		return nil
	}

	return ing.Spec.DefaultBackend.Service
}

// podReferenced returns true if we are interested in pod.
func (lbc *LoadBalancerController) podReferenced(ctx context.Context, pod *corev1.Pod) bool {
	log := klog.FromContext(ctx)

	if !lbc.internalDefaultBackend {
		if svc, err := lbc.svcLister.Services(lbc.defaultSvc.Namespace).Get(lbc.defaultSvc.Name); err == nil {
			if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) {
				log.V(4).Info("Referenced by default Service", "pod", klog.KObj(pod), "service", lbc.defaultSvc)
				return true
			}
		}
	}

	ings, err := lbc.ingLister.Ingresses(pod.Namespace).List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list Ingress", "pod", klog.KObj(pod))
		return false
	}

	return slices.ContainsFunc(ings, func(ing *networkingv1.Ingress) bool {
		if !lbc.validateIngressClass(ctx, ing) {
			return false
		}

		if !lbc.noDefaultBackendOverride {
			if isb := getDefaultBackendService(ing); isb != nil {
				if svc, err := lbc.svcLister.Services(pod.Namespace).Get(isb.Name); err == nil {
					if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) {
						log.V(4).Info("Referenced by Ingress", "pod", klog.KObj(pod),
							"ingress", klog.KObj(ing), "service", klog.KObj(svc))
						return true
					}
				}
			}
		}

		return slicesutil.ContainsPtrFunc(ing.Spec.Rules, func(rule *networkingv1.IngressRule) bool {
			return rule.HTTP != nil &&
				slicesutil.ContainsPtrFunc(rule.HTTP.Paths, func(path *networkingv1.HTTPIngressPath) bool {
					isb := path.Backend.Service
					if isb == nil {
						return false
					}

					svc, err := lbc.svcLister.Services(pod.Namespace).Get(isb.Name)
					if err != nil {
						return false
					}

					if labels.ValidatedSetSelector(svc.Spec.Selector).Matches(labels.Set(pod.Labels)) {
						log.V(4).Info("Referenced by Ingress", "pod", klog.KObj(pod),
							"ingress", klog.KObj(ing), "service", klog.KObj(svc))
						return true
					}

					return false
				})
		})
	})
}

func (lbc *LoadBalancerController) enqueue() {
	lbc.syncQueue.Add(syncKey)
}

func (lbc *LoadBalancerController) worker(ctx context.Context) {
	log := klog.FromContext(ctx)

	work := func() bool {
		key, quit := lbc.syncQueue.Get()
		if quit {
			return true
		}

		defer lbc.syncQueue.Done(key)

		lbc.reloadRateLimiter.Accept()

		log := klog.LoggerWithValues(log, "reconcileID", uuid.NewUUID())
		ctx := klog.NewContext(context.WithoutCancel(ctx), log)

		ctx, cancel := context.WithTimeout(ctx, lbc.reconcileTimeout)
		defer cancel()

		if err := lbc.sync(ctx, key); err != nil {
			log.Error(err, "Unable to reconcile load balancer")
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
func (lbc *LoadBalancerController) getConfigMap(ctx context.Context, cmKey *types.NamespacedName) (*corev1.ConfigMap, error) {
	log := klog.FromContext(ctx)

	if cmKey == nil {
		return &corev1.ConfigMap{}, nil
	}

	cm, err := lbc.cmLister.ConfigMaps(cmKey.Namespace).Get(cmKey.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(3).Info("ConfigMap has been deleted", "configMap", cmKey)
			return &corev1.ConfigMap{}, nil
		}

		return nil, err
	}

	return cm, nil
}

func (lbc *LoadBalancerController) sync(ctx context.Context, key struct{}) error {
	log := klog.FromContext(ctx)

	start := time.Now()

	log.Info("Syncing load balancer")

	retry := false

	defer func() {
		lbc.retryOrForget(key, retry)

		log.Info("Finished syncing load balancer", "duration", time.Since(start))
	}()

	ingConfig, err := lbc.createConfig(ctx)
	if err != nil {
		return err
	}

	cm, err := lbc.getConfigMap(ctx, lbc.nghttpxConfigMap)
	if err != nil {
		return err
	}

	nghttpx.ReadConfig(ingConfig, cm)

	secret, err := lbc.secretLister.Secrets(lbc.nghttpxSecret.Namespace).Get(lbc.nghttpxSecret.Name)
	if err != nil {
		// Continue to processing so that missing Secret does not prevent the controller from reconciling new configuration.
		log.Error(err, "nghttpx secret not found")
	} else {
		if lbc.shareTLSTicketKey {
			ticketKey, ok := secret.Data[nghttpxTLSTicketKeySecretKey]
			if !ok {
				log.Error(nil, "Secret does not contain TLS ticket key")
			} else if err := nghttpx.VerifyTLSTicketKey(ticketKey); err != nil {
				log.Error(err, "Secret contains malformed TLS ticket key")
			} else {
				ingConfig.TLSTicketKeyFiles = nghttpx.CreateTLSTicketKeyFiles(ingConfig.ConfDir, ticketKey)
			}
		}

		if lbc.http3 {
			quicKM, ok := secret.Data[nghttpxQUICKeyingMaterialsSecretKey]
			if !ok {
				log.Error(nil, "Secret does not contain QUIC keying materials")
			} else if err := nghttpx.VerifyQUICKeyingMaterials(quicKM); err != nil {
				log.Error(err, "Secret contains malformed QUIC keying materials")
			} else {
				ingConfig.QUICSecretFile = nghttpx.CreateQUICSecretFile(ingConfig.ConfDir, quicKM)
			}
		}
	}

	reloaded, err := lbc.nghttpx.CheckAndReload(ctx, ingConfig)
	if err != nil {
		return err
	}

	if !reloaded {
		log.V(4).Info("No need to reload configuration.")
	}

	return nil
}

func (lbc *LoadBalancerController) getDefaultUpstream(ctx context.Context) *nghttpx.Upstream {
	log := klog.FromContext(ctx)

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
			Backends: []nghttpx.Backend{
				{
					Address:  "127.0.0.1",
					Port:     "9999",
					Protocol: nghttpx.ProtocolH1,
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
		log.Error(err, "Unable to get Service", "service", svcKey)

		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultBackend())

		return upstream
	}

	if len(svc.Spec.Ports) == 0 {
		log.Error(nil, "Service has no ports", "service", svcKey)

		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultBackend())

		return upstream
	}

	eps, err := lbc.getEndpoints(ctx, svc, &svc.Spec.Ports[0], &nghttpx.BackendConfig{})
	if err != nil {
		log.Error(err, "Unable to get endpoints for Service", "service", svcKey)

		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultBackend())
	} else if len(eps) == 0 {
		log.Error(nil, "Service does not have any active endpoints", "service", svcKey)

		upstream.Backends = append(upstream.Backends, nghttpx.NewDefaultBackend())
	} else {
		upstream.Backends = append(upstream.Backends, eps...)
	}

	return upstream
}

// createConfig creates nghttpx.IngressConfig.  In nghttpx terminology, nghttpx.Upstream is backend, nghttpx.Server is frontend
func (lbc *LoadBalancerController) createConfig(ctx context.Context) (*nghttpx.IngressConfig, error) {
	ingConfig := &nghttpx.IngressConfig{
		HealthPort:                       lbc.nghttpxHealthPort,
		APIPort:                          lbc.nghttpxAPIPort,
		ConfDir:                          lbc.nghttpxConfDir,
		HTTPPort:                         lbc.nghttpxHTTPPort,
		HTTPSPort:                        lbc.nghttpxHTTPSPort,
		Workers:                          lbc.nghttpxWorkers,
		WorkerProcessGraceShutdownPeriod: lbc.nghttpxWorkerProcessGraceShutdownPeriod,
		MaxWorkerProcesses:               lbc.nghttpxMaxWorkerProcesses,
		FetchOCSPRespFromSecret:          lbc.fetchOCSPRespFromSecret,
		ProxyProto:                       lbc.proxyProto,
		HTTP3:                            lbc.http3,
		ShareTLSTicketKey:                lbc.shareTLSTicketKey,
	}

	if lbc.defaultTLSSecret != nil {
		tlsCred, err := lbc.getTLSCredFromSecret(ctx, lbc.defaultTLSSecret)
		if err != nil {
			return nil, err
		}

		ingConfig.TLS = true
		ingConfig.DefaultTLSCred = tlsCred
	}

	ings, err := lbc.ingLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	upstreams, defaultUpstream, creds := lbc.createIngressUpstreams(ctx, ings)

	if lbc.gatewayAPI {
		httpRoutes, err := lbc.httpRouteLister.List(labels.Everything())
		if err != nil {
			return nil, err
		}

		gatewayUpstreams := lbc.createGatewayUpstreams(ctx, httpRoutes)

		gtws, err := lbc.gatewayLister.List(labels.Everything())
		if err != nil {
			return nil, err
		}

		gatewayCreds := lbc.createGatewayCredentials(ctx, gtws)

		upstreams = append(upstreams, gatewayUpstreams...)
		creds = append(creds, gatewayCreds...)
	}

	nghttpx.SortTLSCred(creds)
	creds = nghttpx.RemoveDuplicateTLSCred(creds)

	if ingConfig.DefaultTLSCred != nil {
		// Remove default TLS key pair from creds.
		i := slices.IndexFunc(creds, func(cred *nghttpx.TLSCred) bool {
			return nghttpx.TLSCredShareSamePaths(ingConfig.DefaultTLSCred, cred)
		})
		if i != -1 {
			creds = slices.Delete(creds, i, i+1)
		}

		ingConfig.SubTLSCred = creds
	} else if len(creds) > 0 {
		ingConfig.TLS = true
		ingConfig.DefaultTLSCred = creds[0]
		ingConfig.SubTLSCred = creds[1:]
	}

	// find default backend.  If only it is not found, use default backend.  This is useful to override default backend with ingress.
	defaultUpstreamFound := slices.ContainsFunc(upstreams, func(u *nghttpx.Upstream) bool {
		return u.Host == "" && (u.Path == "" || u.Path == "/")
	})

	if !defaultUpstreamFound {
		if defaultUpstream == nil {
			defaultUpstream = lbc.getDefaultUpstream(ctx)
		}

		upstreams = append(upstreams, defaultUpstream)
	}

	slices.SortFunc(upstreams, func(a, b *nghttpx.Upstream) int {
		return cmp.Or(
			cmp.Compare(a.Host, b.Host),
			cmp.Compare(a.Path, b.Path),
		)
	})

	upstreams = removeUpstreamsWithInconsistentBackendParams(ctx, upstreams)

	for _, value := range upstreams {
		backends := value.Backends
		slices.SortFunc(backends, func(a, b nghttpx.Backend) int {
			return cmp.Or(
				cmp.Compare(a.Address, b.Address),
				cmp.Compare(a.Port, b.Port),
			)
		})

		// remove duplicate Backend
		value.Backends = slices.CompactFunc(backends, func(a, b nghttpx.Backend) bool {
			return a.Address == b.Address && a.Port == b.Port
		})
	}

	ingConfig.Upstreams = upstreams

	if lbc.deferredShutdownPeriod != 0 {
		ingConfig.HealthzMruby = nghttpx.CreatePerPatternMrubyChecksumFile(lbc.nghttpxConfDir, lbc.createHealthzMruby())
	}

	return ingConfig, nil
}

func (lbc *LoadBalancerController) createIngressUpstreams(ctx context.Context, ings []*networkingv1.Ingress) (upstreams []*nghttpx.Upstream, defaultUpstream *nghttpx.Upstream, creds []*nghttpx.TLSCred) {
	log := klog.FromContext(ctx)

	for _, ing := range ings {
		if !lbc.validateIngressClass(ctx, ing) {
			continue
		}

		log := klog.LoggerWithValues(log, "ingress", klog.KObj(ing))
		ctx := klog.NewContext(ctx, log)

		log.V(4).Info("Processing Ingress")

		var requireTLS bool

		ingCreds, err := lbc.getTLSCredFromIngress(ctx, ing)
		if err != nil {
			log.Error(err, "Ingress is disabled because its TLS Secret cannot be processed")
			continue
		}

		creds = append(creds, ingCreds...)
		requireTLS = len(ingCreds) > 0

		bcm := ingressAnnotation(ing.Annotations).NewBackendConfigMapper(ctx)
		pcm := ingressAnnotation(ing.Annotations).NewPathConfigMapper(ctx)

		if !lbc.noDefaultBackendOverride {
			if isb := getDefaultBackendService(ing); isb != nil {
				// This overrides the default backend specified in command-line.  It is possible that the multiple Ingress
				// resource specifies this.  But specification does not any rules how to deal with it.  Just use the one we
				// meet last.
				gvk := networkingv1.SchemeGroupVersion.WithKind("Ingress")
				if ups, err := lbc.createUpstream(ctx, gvk, ing, "", "/", isb, false /* requireTLS */, pcm, bcm); err != nil {
					log.Error(err, "Unable to create default backend")
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

				reqPath := path.Path
				if idx := strings.Index(reqPath, "#"); idx != -1 {
					log.Error(nil, "Path includes fragment", "path", reqPath)
					reqPath = reqPath[:idx]
				}

				if idx := strings.Index(reqPath, "?"); idx != -1 {
					log.Error(nil, "Path includes query", "path", reqPath)
					reqPath = reqPath[:idx]
				}

				if lbc.noDefaultBackendOverride && rule.Host == "" && (reqPath == "" || reqPath == "/") {
					log.Error(nil, "Ignore rule which overrides default backend")
					continue
				}

				isb := path.Backend.Service
				if isb == nil {
					log.Error(nil, "No Service is set for path", "path", reqPath)
					continue
				}

				gvk := networkingv1.SchemeGroupVersion.WithKind("Ingress")

				ups, err := lbc.createUpstream(ctx, gvk, ing, rule.Host, reqPath, isb, requireTLS, pcm, bcm)
				if err != nil {
					log.Error(err, "Unable to create backend", "path", reqPath)
					continue
				}

				upstreams = append(upstreams, ups)
			}
		}
	}

	return
}

type backendOpts struct {
	index                    int
	mruby                    *nghttpx.ChecksumFile
	affinity                 nghttpx.Affinity
	affinityCookieName       string
	affinityCookiePath       string
	affinityCookieSecure     nghttpx.AffinityCookieSecure
	affinityCookieStickiness nghttpx.AffinityCookieStickiness
	readTimeout              *metav1.Duration
	writeTimeout             *metav1.Duration
}

func newBackendOpts(idx int, upstream *nghttpx.Upstream) backendOpts {
	opts := backendOpts{
		index:        idx,
		mruby:        upstream.Mruby,
		affinity:     upstream.Affinity,
		readTimeout:  upstream.ReadTimeout,
		writeTimeout: upstream.WriteTimeout,
	}

	if upstream.Affinity == nghttpx.AffinityCookie {
		opts.affinityCookieName = upstream.AffinityCookieName
		opts.affinityCookiePath = upstream.AffinityCookiePath
		opts.affinityCookieSecure = upstream.AffinityCookieSecure
		opts.affinityCookieStickiness = upstream.AffinityCookieStickiness
	}

	return opts
}

func removeUpstreamsWithInconsistentBackendParams(ctx context.Context, upstreams []*nghttpx.Upstream) []*nghttpx.Upstream {
	log := klog.FromContext(ctx)

	if len(upstreams) < 2 {
		return upstreams
	}

	// Reuse the same buffer.  golang has no problem when calling append with overlapped ranges.  If no errors are found, we return
	// upstreams without any modification.
	out := upstreams[:0]

	opts := newBackendOpts(0, upstreams[0])

	for i := 1; i < len(upstreams); i++ {
		b := upstreams[opts.index]
		c := upstreams[i]

		if b.Host != c.Host || b.Path != c.Path {
			if len(out) < opts.index {
				out = append(out, upstreams[opts.index:i]...)
			} else {
				out = upstreams[:i]
			}

			opts = newBackendOpts(i, c)

			continue
		}

		err := validateUpstreamBackendParams(c, opts)
		if err == nil {
			if c.Mruby != nil && opts.mruby == nil {
				opts.mruby = c.Mruby
			}

			if c.Affinity != nghttpx.AffinityNone && opts.affinity == nghttpx.AffinityNone {
				opts.affinity = c.Affinity
				if opts.affinity == nghttpx.AffinityCookie {
					opts.affinityCookieName = c.AffinityCookieName
					opts.affinityCookiePath = c.AffinityCookiePath
					opts.affinityCookieSecure = c.AffinityCookieSecure
					opts.affinityCookieStickiness = c.AffinityCookieStickiness
				}
			}

			if c.ReadTimeout != nil && opts.readTimeout == nil {
				opts.readTimeout = c.ReadTimeout
			}

			if c.WriteTimeout != nil && opts.writeTimeout == nil {
				opts.writeTimeout = c.WriteTimeout
			}

			continue
		}

		log.Error(err, "Inconsistent upstream backend parameters found")

		// We encountered mismatch in backend parameters.  Skip those upstreams.
		i++

		for ; i < len(upstreams); i++ {
			c := upstreams[i]
			if b.Host != c.Host || b.Path != c.Path {
				break
			}
		}

		for j := opts.index; j < i; j++ {
			log.Error(nil, "Skip upstream which contains inconsistent backend parameters",
				"upstream", upstreams[j].Name, "gvk", upstreams[j].GroupVersionKind, "source", upstreams[j].Source)
		}

		if i == len(upstreams) {
			return out
		}

		opts = newBackendOpts(i, upstreams[i])
	}

	if opts.index == 0 {
		return upstreams
	}

	return append(out, upstreams[opts.index:]...)
}

func validateUpstreamBackendParams(upstream *nghttpx.Upstream, opts backendOpts) error {
	if err := validateUpstreamBackendParamsMruby(upstream, opts); err != nil {
		return err
	}

	if err := validateUpstreamBackendParamsAffinity(upstream, opts); err != nil {
		return err
	}

	if err := validateUpstreamBackendParamsReadTimeout(upstream, opts); err != nil {
		return err
	}

	return validateUpstreamBackendParamsWriteTimeout(upstream, opts)
}

func validateUpstreamBackendParamsMruby(upstream *nghttpx.Upstream, opts backendOpts) error {
	if upstream.Mruby == nil || opts.mruby == nil || upstream.Mruby.Path == opts.mruby.Path {
		return nil
	}

	return fmt.Errorf("Inconsistent mruby path %v: previously it is set to %v", upstream.Mruby.Path, opts.mruby.Path)
}

func validateUpstreamBackendParamsAffinity(upstream *nghttpx.Upstream, opts backendOpts) error {
	if upstream.Affinity == nghttpx.AffinityNone || opts.affinity == nghttpx.AffinityNone ||
		(upstream.Affinity == opts.affinity &&
			upstream.AffinityCookieName == opts.affinityCookieName &&
			upstream.AffinityCookiePath == opts.affinityCookiePath &&
			upstream.AffinityCookieSecure == opts.affinityCookieSecure &&
			upstream.AffinityCookieStickiness == opts.affinityCookieStickiness) {
		return nil
	}

	return fmt.Errorf("Inconsistent affinity type=%v cookieName=%v cookiePath=%v cookieSecure=%v cookieStickiness=%v: previously they are set to type=%v cookieName=%v cookiePath=%v cookieSecure=%v cookieStickiness=%v",
		upstream.Affinity, upstream.AffinityCookieName, upstream.AffinityCookiePath, upstream.AffinityCookieSecure, upstream.AffinityCookieStickiness,
		opts.affinity, opts.affinityCookieName, opts.affinityCookiePath, opts.affinityCookieSecure, opts.affinityCookieStickiness)
}

func validateUpstreamBackendParamsReadTimeout(upstream *nghttpx.Upstream, opts backendOpts) error {
	if upstream.ReadTimeout == nil || opts.readTimeout == nil || *upstream.ReadTimeout == *opts.readTimeout {
		return nil
	}

	return fmt.Errorf("Inconsistent readTimeout %v: previously it is set to %v", *upstream.ReadTimeout, *opts.readTimeout)
}

func validateUpstreamBackendParamsWriteTimeout(upstream *nghttpx.Upstream, opts backendOpts) error {
	if upstream.WriteTimeout == nil || opts.writeTimeout == nil || *upstream.WriteTimeout == *opts.writeTimeout {
		return nil
	}

	return fmt.Errorf("Inconsistent writeTimeout %v: previously it is set to %v", *upstream.WriteTimeout, *opts.writeTimeout)
}

func (lbc *LoadBalancerController) createHealthzMruby() []byte {
	var code int
	if lbc.ShutdownCommenced() {
		code = 503
	} else {
		code = 200
	}

	return []byte(`
class App
  def on_req(env)
    resp = env.resp
    resp.status = ` + strconv.Itoa(code) + `
    resp.return ""
  end
end

App.new
`)
}

// createUpstream creates new nghttpx.Upstream for ing, host, path and isb.
func (lbc *LoadBalancerController) createUpstream(ctx context.Context, gvk schema.GroupVersionKind, obj metav1.Object, host, path string, isb *networkingv1.IngressServiceBackend,
	requireTLS bool, pcm *nghttpx.PathConfigMapper, bcm *nghttpx.BackendConfigMapper) (*nghttpx.Upstream, error) {
	log := klog.FromContext(ctx)

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

	pc := pcm.ConfigFor(host, normalizedPath)

	if pc.GetAffinity() == nghttpx.AffinityCookie && pc.GetAffinityCookieName() == "" {
		return nil, fmt.Errorf("%v %v/%v has empty affinity cookie name", gvk.Kind, obj.GetNamespace(), obj.GetName())
	}

	// The format of upsName is similar to backend option syntax of nghttpx.
	upsName := gvk.Group + "/" + gvk.Version + "/" + gvk.Kind + ":" + obj.GetNamespace() + "/" + isb.Name + "," + portStr + ";" + host + normalizedPath
	ups := &nghttpx.Upstream{
		Name:                     upsName,
		GroupVersionKind:         gvk,
		Source:                   namespacedName(obj),
		Host:                     host,
		Path:                     normalizedPath,
		RedirectIfNotTLS:         pc.GetRedirectIfNotTLS() && (requireTLS || lbc.defaultTLSSecret != nil),
		DoNotForward:             pc.GetDoNotForward(),
		Affinity:                 pc.GetAffinity(),
		AffinityCookieName:       pc.GetAffinityCookieName(),
		AffinityCookiePath:       pc.GetAffinityCookiePath(),
		AffinityCookieSecure:     pc.GetAffinityCookieSecure(),
		AffinityCookieStickiness: pc.GetAffinityCookieStickiness(),
		ReadTimeout:              pc.GetReadTimeout(),
		WriteTimeout:             pc.GetWriteTimeout(),
	}

	if mruby := pc.GetMruby(); mruby != "" {
		ups.Mruby = nghttpx.CreatePerPatternMrubyChecksumFile(lbc.nghttpxConfDir, []byte(mruby))
	} else if ups.DoNotForward {
		return nil, fmt.Errorf("%v %v/%v lacks mruby but doNotForward is used", gvk.Kind, obj.GetNamespace(), obj.GetName())
	}

	log.V(4).Info("Found rule", "upstream", upsName, "host", ups.Host, "path", ups.Path)

	if ups.DoNotForward {
		ups.Backends = []nghttpx.Backend{nghttpx.NewDefaultBackend()}
		return ups, nil
	}

	svcKey := strings.Join([]string{obj.GetNamespace(), isb.Name}, "/")

	svc, err := lbc.svcLister.Services(obj.GetNamespace()).Get(isb.Name)
	if err != nil {
		return nil, fmt.Errorf("error getting Service %v from the cache: %w", svcKey, err)
	}

	log.V(3).Info("Obtaining port information", "service", svcKey)

	var (
		key         string
		servicePort *corev1.ServicePort
	)

	for i := range svc.Spec.Ports {
		servicePort = &svc.Spec.Ports[i]
		// According to the documentation, servicePort.TargetPort is optional.  If it is omitted, use servicePort.Port.
		// servicePort.TargetPort could be a string.  This is really messy.

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

		break
	}

	if key == "" {
		return nil, fmt.Errorf("no backend service port found for Service %v", svcKey)
	}

	backendConfig := bcm.ConfigFor(ctx, isb.Name, key)

	eps, err := lbc.getEndpoints(ctx, svc, servicePort, backendConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to get endpoints for Service %v: %w", svcKey, err)
	}

	if len(eps) == 0 {
		return nil, fmt.Errorf("no active endpoints found for Service %v", svcKey)
	}

	ups.Backends = append(ups.Backends, eps...)

	return ups, nil
}

// getTLSCredFromSecret returns nghttpx.TLSCred obtained from the Secret denoted by secretKey.
func (lbc *LoadBalancerController) getTLSCredFromSecret(ctx context.Context, key *types.NamespacedName) (*nghttpx.TLSCred, error) {
	secret, err := lbc.secretLister.Secrets(key.Namespace).Get(key.Name)
	if err != nil {
		return nil, fmt.Errorf("unable to get TLS secret %v: %w", key, err)
	}

	return lbc.createTLSCredFromSecret(ctx, secret)
}

// getTLSCredFromIngress returns list of nghttpx.TLSCred obtained from Ingress resource.
func (lbc *LoadBalancerController) getTLSCredFromIngress(ctx context.Context, ing *networkingv1.Ingress) ([]*nghttpx.TLSCred, error) {
	if len(ing.Spec.TLS) == 0 {
		return nil, nil
	}

	creds := make([]*nghttpx.TLSCred, len(ing.Spec.TLS))

	for i := range ing.Spec.TLS {
		tls := &ing.Spec.TLS[i]

		secret, err := lbc.secretLister.Secrets(ing.Namespace).Get(tls.SecretName)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve Secret %v/%v for Ingress %v/%v: %w", ing.Namespace, tls.SecretName, ing.Namespace, ing.Name, err)
		}

		tlsCred, err := lbc.createTLSCredFromSecret(ctx, secret)
		if err != nil {
			return nil, err
		}

		creds[i] = tlsCred
	}

	return creds, nil
}

// createTLSCredFromSecret creates nghttpx.TLSCred from secret.
func (lbc *LoadBalancerController) createTLSCredFromSecret(ctx context.Context, secret *corev1.Secret) (*nghttpx.TLSCred, error) {
	cert, ok := secret.Data[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("Secret %v/%v has no certificate", secret.Namespace, secret.Name)
	}

	key, ok := secret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("Secret %v/%v has no private key", secret.Namespace, secret.Name)
	}

	cacheKey := createCertCacheKey(secret)
	certHash := calculateCertificateHash(cert, key)

	var leafCert *x509.Certificate

	cache, ok := lbc.getCertificateFromCache(cacheKey)
	if ok && bytes.Equal(certHash, cache.certificateHash) {
		leafCert = cache.leafCertificate
		cert = cache.certificate
		key = cache.key
	} else {
		var err error

		cert, err = nghttpx.NormalizePEM(cert)
		if err != nil {
			return nil, fmt.Errorf("unable to normalize certificate in Secret %v/%v: %w", secret.Namespace, secret.Name, err)
		}

		key, err = nghttpx.NormalizePEM(key)
		if err != nil {
			return nil, fmt.Errorf("unable to normalize private key in Secret %v/%v: %w", secret.Namespace, secret.Name, err)
		}

		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return nil, err
		}

		leafCert, err = nghttpx.ReadLeafCertificate(cert)
		if err != nil {
			return nil, err
		}

		lbc.cacheCertificate(cacheKey, &certificateCacheEntry{
			leafCertificate: leafCert,
			certificateHash: certHash,
			certificate:     cert,
			key:             key,
		})
	}

	if err := nghttpx.VerifyCertificate(ctx, leafCert, time.Now()); err != nil {
		return nil, err
	}

	// OCSP response in TLS secret is optional feature.
	return nghttpx.CreateTLSCred(lbc.nghttpxConfDir, strings.Join([]string{secret.Namespace, secret.Name}, "/"), cert, key, secret.Data[lbc.ocspRespKey]), nil
}

type certificateCacheEntry struct {
	// leafCertificate is a parsed form of Certificate.
	leafCertificate *x509.Certificate
	// certificateHash is the hash of certificate and private key which are not yet normalized.
	certificateHash []byte
	// certificate is a normalized certificate in PEM format.
	certificate []byte
	// key is a normalized private key in PEM format.
	key []byte
}

func (lbc *LoadBalancerController) getCertificateFromCache(key types.NamespacedName) (*certificateCacheEntry, bool) {
	lbc.certCacheMu.Lock()
	ent, ok := lbc.certCache[key]
	lbc.certCacheMu.Unlock()

	return ent, ok
}

func (lbc *LoadBalancerController) cacheCertificate(key types.NamespacedName, entry *certificateCacheEntry) {
	lbc.certCacheMu.Lock()
	lbc.certCache[key] = entry
	lbc.certCacheMu.Unlock()
}

func (lbc *LoadBalancerController) garbageCollectCertificate(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(certificateGarbageCollectionPeriod):
		}

		lbc.certCacheMu.Lock()

		for key := range lbc.certCache {
			if _, err := lbc.secretLister.Secrets(key.Namespace).Get(key.Name); err != nil && apierrors.IsNotFound(err) {
				delete(lbc.certCache, key)
			}
		}

		lbc.certCacheMu.Unlock()
	}
}

func (lbc *LoadBalancerController) secretReferenced(ctx context.Context, s *corev1.Secret) bool {
	log := klog.FromContext(ctx)

	if (lbc.defaultTLSSecret != nil && s.Namespace == lbc.defaultTLSSecret.Namespace && s.Name == lbc.defaultTLSSecret.Name) ||
		(s.Namespace == lbc.nghttpxSecret.Namespace && s.Name == lbc.nghttpxSecret.Name) {
		return true
	}

	ings, err := lbc.ingLister.Ingresses(s.Namespace).List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list Ingress", "secret", klog.KObj(s))
		return false
	}

	return slices.ContainsFunc(ings, func(ing *networkingv1.Ingress) bool {
		return lbc.validateIngressClass(ctx, ing) &&
			slicesutil.ContainsPtrFunc(ing.Spec.TLS, func(tls *networkingv1.IngressTLS) bool {
				return tls.SecretName == s.Name
			})
	})
}

// getEndpoints returns a list of Backend for a given service.  backendConfig is additional per-port configuration for backend, which must
// not be nil.
func (lbc *LoadBalancerController) getEndpoints(ctx context.Context, svc *corev1.Service, svcPort *corev1.ServicePort,
	backendConfig *nghttpx.BackendConfig) ([]nghttpx.Backend, error) {
	log := klog.FromContext(ctx)

	if svcPort.Protocol != "" && svcPort.Protocol != corev1.ProtocolTCP {
		return nil, fmt.Errorf("Service %v/%v has unsupported protocol %v", svc.Namespace, svc.Name, svcPort.Protocol)
	}

	log.V(3).Info("Getting endpoints",
		"service", klog.KObj(svc), "port", svcPort.Port, "targetPort", svcPort.TargetPort)

	if len(svc.Spec.Selector) == 0 {
		return lbc.getEndpointsFromEndpointSliceWithoutServiceSelectors(ctx, svc, svcPort, backendConfig)
	}

	return lbc.getEndpointsFromEndpointSlice(ctx, svc, svcPort, backendConfig)
}

func (lbc *LoadBalancerController) getEndpointsFromEndpointSliceWithoutServiceSelectors(
	ctx context.Context, svc *corev1.Service, svcPort *corev1.ServicePort, backendConfig *nghttpx.BackendConfig) ([]nghttpx.Backend, error) {
	log := klog.FromContext(ctx)

	var targetPort int32

	switch {
	case svcPort.TargetPort.IntVal != 0:
		targetPort = svcPort.TargetPort.IntVal
	case svcPort.TargetPort.StrVal == "":
		targetPort = svcPort.Port
	default:
		return nil, fmt.Errorf("Service %v/%v must have integer target port if specified: %v", svc.Namespace, svc.Name, svcPort.TargetPort)
	}

	ess, err := lbc.epSliceLister.EndpointSlices(svc.Namespace).List(newEndpointSliceSelector(svc))
	if err != nil {
		log.Error(err, "Unexpected error obtaining EndpointSlice")
		return nil, err
	}

	var backends []nghttpx.Backend

	for _, es := range ess {
		switch es.AddressType {
		case discoveryv1.AddressTypeIPv4, discoveryv1.AddressTypeIPv6:
		default:
			log.Error(nil, "Unsupported address type", "endpointSlice", klog.KObj(es), "addressType", es.AddressType)
			continue
		}

		if len(es.Ports) == 0 {
			log.Error(nil, "No port defined", "endpointSlice", klog.KObj(es))
			continue
		}

		for i := range es.Ports {
			epPort := &es.Ports[i]

			if (epPort.Protocol != nil && *epPort.Protocol != corev1.ProtocolTCP) ||
				epPort.Port == nil || *epPort.Port != targetPort {
				continue
			}

			for i := range es.Endpoints {
				ep := &es.Endpoints[i]
				if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
					continue
				}

				// TODO We historically added all addresses in Endpoints code.  Not sure we should just pick one here
				// instead.
				for _, addr := range ep.Addresses {
					backends = append(backends, lbc.createBackend(svc, addr, targetPort, backendConfig))
				}
			}
		}
	}

	if log := log.V(3); log.Enabled() {
		log.Info("Endpoints found", "backends", klog.Format(backends))
	}

	return backends, nil
}

func (lbc *LoadBalancerController) getEndpointsFromEndpointSlice(ctx context.Context, svc *corev1.Service, svcPort *corev1.ServicePort,
	backendConfig *nghttpx.BackendConfig) ([]nghttpx.Backend, error) {
	log := klog.FromContext(ctx)

	ess, err := lbc.epSliceLister.EndpointSlices(svc.Namespace).List(newEndpointSliceSelector(svc))
	if err != nil {
		log.Error(err, "Unexpected error obtaining EndpointSlice")
		return nil, err
	}

	var backends []nghttpx.Backend

	for _, es := range ess {
		switch es.AddressType {
		case discoveryv1.AddressTypeIPv4, discoveryv1.AddressTypeIPv6:
		default:
			log.Error(nil, "Unsupported address type", "endpoints", klog.KObj(es), "addressType", es.AddressType)
			continue
		}

		if len(es.Ports) == 0 {
			log.Error(nil, "No port defined", "endpoints", klog.KObj(es))
			continue
		}

		for i := range es.Ports {
			epPort := &es.Ports[i]

			if epPort.Protocol != nil && *epPort.Protocol != corev1.ProtocolTCP {
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
					log.Error(err, "Unable to get target port", "pod", klog.KRef(ref.Namespace, ref.Name),
						"servicePort", klog.Format(svcPort), "endpointPort", klog.Format(epPort))
					continue
				}

				// TODO We historically added all addresses in Endpoints code.  Not sure we should just pick one here
				// instead.
				for _, addr := range ep.Addresses {
					backends = append(backends, lbc.createBackend(svc, addr, targetPort, backendConfig))
				}
			}
		}
	}

	if log := log.V(3); log.Enabled() {
		log.Info("Endpoints found", "backends", klog.Format(backends))
	}

	return backends, nil
}

func newEndpointSliceSelector(svc *corev1.Service) labels.Selector {
	return labels.SelectorFromSet(labels.Set{
		discoveryv1.LabelServiceName: svc.Name,
	})
}

func (lbc *LoadBalancerController) createBackend(svc *corev1.Service, address string, targetPort int32, backendConfig *nghttpx.BackendConfig) nghttpx.Backend {
	ups := nghttpx.Backend{
		Address:  address,
		Port:     strconv.Itoa(int(targetPort)),
		Protocol: backendConfig.GetProto(),
		TLS:      backendConfig.GetTLS(),
		SNI:      backendConfig.GetSNI(),
		DNS:      backendConfig.GetDNS(),
		Group:    strings.Join([]string{svc.Namespace, svc.Name}, "/"),
		Weight:   backendConfig.GetWeight(),
	}
	// Set Protocol and Affinity here if they are empty.  Template expects them.
	if ups.Protocol == "" {
		ups.Protocol = nghttpx.ProtocolH1
	}

	return ups
}

// resolveTargetPort returns endpoint port.  This function verifies that endpoint port given in epPort matches the svcPort.  If svcPort is
// not a number, a port is looked up by referencing Pod denoted by ref.
func (lbc *LoadBalancerController) resolveTargetPort(svcPort *corev1.ServicePort, epPort *discoveryv1.EndpointPort, ref *corev1.ObjectReference) (int32, error) {
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
			return 0, fmt.Errorf("unable to find named port %v in Pod spec: %w", svcPort.TargetPort.String(), err)
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
func (lbc *LoadBalancerController) getNamedPortFromPod(ref *corev1.ObjectReference, servicePort *corev1.ServicePort) (int32, error) {
	pod, err := lbc.podLister.Pods(ref.Namespace).Get(ref.Name)
	if err != nil {
		return 0, fmt.Errorf("unable to get Pod %v/%v: %w", ref.Namespace, ref.Name, err)
	}

	port, err := podFindPort(pod, servicePort)
	if err != nil {
		return 0, fmt.Errorf("unable to find port %v from Pod %v/%v: %w", servicePort.TargetPort.String(), pod.Namespace, pod.Name, err)
	}

	return port, nil
}

// startShutdown commences shutting down the loadbalancer controller.
func (lbc *LoadBalancerController) startShutdown(ctx context.Context) {
	log := klog.FromContext(ctx)

	lbc.shutdownMu.Lock()
	defer lbc.shutdownMu.Unlock()

	// Only try draining the workqueue if we haven't already.
	if lbc.shutdown {
		log.Info("Shutting down is already in progress")
		return
	}

	lbc.eventRecorder.Eventf(lbc.pod, nil, corev1.EventTypeNormal, "Shutdown", "Shutdown", "Shutting down started")

	lbc.shutdown = true
}

// ShutdownCommenced returns true if the controller is shutting down.  This includes deferred shutdown period.
func (lbc *LoadBalancerController) ShutdownCommenced() bool {
	lbc.shutdownMu.RLock()
	defer lbc.shutdownMu.RUnlock()

	return lbc.shutdown
}

type legacyEventRecorderEventf func(regarding runtime.Object, related runtime.Object, eventtype, reason, action, note string, args ...interface{})

func (r legacyEventRecorderEventf) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	r(object, nil, eventtype, reason, reason, messageFmt, args...)
}

// Run starts the loadbalancer controller.
func (lbc *LoadBalancerController) Run(ctx context.Context) {
	log := klog.LoggerWithName(klog.FromContext(ctx), "loadBalancerController")

	log.Info("Starting nghttpx loadbalancer controller")

	ctrlCtx, cancel := context.WithCancel(klog.NewContext(context.WithoutCancel(ctx), log))
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		if err := lbc.nghttpx.Start(ctrlCtx, lbc.nghttpxExecPath, nghttpx.ConfigPath(lbc.nghttpxConfDir)); err != nil {
			log.Error(err, "Unable to start nghttpx")
		}
	}()

	allInformers := []informers.SharedInformerFactory{lbc.watchNSInformers, lbc.allNSInformers}

	if lbc.cmNSInformers != nil {
		allInformers = append(allInformers, lbc.cmNSInformers)
	}

	for _, f := range allInformers {
		f.Start(ctrlCtx.Done())
		defer f.Shutdown()
	}

	if lbc.gatewayInformers != nil {
		lbc.gatewayInformers.Start(ctrlCtx.Done())
		defer lbc.gatewayInformers.Shutdown()
	}

	for _, f := range allInformers {
		for v, ok := range f.WaitForCacheSync(ctrlCtx.Done()) {
			if !ok {
				log.Error(nil, "Unable to sync cache", "type", v)
				return
			}
		}
	}

	if lbc.gatewayInformers != nil {
		for v, ok := range lbc.gatewayInformers.WaitForCacheSync(ctrlCtx.Done()) {
			if !ok {
				log.Error(nil, "Unable to sync cache", "type", v)
				return
			}
		}
	}

	rlc := resourcelock.ResourceLockConfig{
		Identity:      lbc.pod.Name,
		EventRecorder: legacyEventRecorderEventf(lbc.eventRecorder.Eventf),
	}

	rl, err := resourcelock.New(resourcelock.LeasesResourceLock, lbc.pod.Namespace, lbc.leaderElectionConfig.ResourceName,
		lbc.clientset.CoreV1(), lbc.clientset.CoordinationV1(), rlc)
	if err != nil {
		log.Error(err, "Unable to create resource lock")
		return
	}

	lec := leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: lbc.leaderElectionConfig.LeaseDuration.Duration,
		RenewDeadline: lbc.leaderElectionConfig.RenewDeadline.Duration,
		RetryPeriod:   lbc.leaderElectionConfig.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				lc, err := NewLeaderController(ctx, lbc)
				if err != nil {
					log.Error(err, "NewLeaderController")

					return
				}

				if err := lc.Run(ctx); err != nil {
					log.Error(err, "LeaderController.Run returned error")
				}
			},
			OnStoppedLeading: func() {
				log.V(4).Info("Stopped leading")
			},
			OnNewLeader: func(string) {},
		},
	}

	wg.Add(1)

	go func() {
		defer wg.Done()

		for {
			le, err := leaderelection.NewLeaderElector(lec)
			if err != nil {
				log.Error(err, "Unable to create LeaderElector")
				cancel()

				return
			}

			le.Run(ctrlCtx)

			select {
			case <-ctrlCtx.Done():
				return
			default:
			}
		}
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		lbc.worker(ctrlCtx)
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		lbc.garbageCollectCertificate(ctrlCtx)
	}()

	go func() {
		<-ctx.Done()

		lbc.startShutdown(ctrlCtx)

		if lbc.deferredShutdownPeriod != 0 {
			log.Info("Deferred shutdown", "period", lbc.deferredShutdownPeriod)

			lbc.enqueue()

			<-time.After(lbc.deferredShutdownPeriod)
		}

		log.Info("Commencing shutting down")

		cancel()
	}()

	<-ctrlCtx.Done()

	log.Info("Shutting down nghttpx loadbalancer controller")

	lbc.syncQueue.ShutDown()

	wg.Wait()
}

func (lbc *LoadBalancerController) retryOrForget(key struct{}, requeue bool) {
	if requeue {
		lbc.syncQueue.Add(key)
	}
}

// validateIngressClass checks whether this controller should process ing or not.
func (lbc *LoadBalancerController) validateIngressClass(ctx context.Context, ing *networkingv1.Ingress) bool {
	return validateIngressClass(ctx, ing, lbc.ingressClassController, lbc.ingClassLister, lbc.requireIngressClass)
}

// LeaderController is operated by leader.  It is started when a controller gains leadership, and stopped when it is lost.
type LeaderController struct {
	lbc *LoadBalancerController

	watchNSInformers informers.SharedInformerFactory
	podNSInformers   informers.SharedInformerFactory
	secretInformers  informers.SharedInformerFactory
	svcInformers     informers.SharedInformerFactory
	allNSInformers   informers.SharedInformerFactory
	gatewayInformers gatewayinformers.SharedInformerFactory

	// For tests
	ingIndexer          cache.Indexer
	ingClassIndexer     cache.Indexer
	svcIndexer          cache.Indexer
	podIndexer          cache.Indexer
	secretIndexer       cache.Indexer
	nodeIndexer         cache.Indexer
	gatewayClassIndexer cache.Indexer
	gatewayIndexer      cache.Indexer
	httpRouteIndexer    cache.Indexer

	ingLister          listersnetworkingv1.IngressLister
	ingClassLister     listersnetworkingv1.IngressClassLister
	svcLister          listerscorev1.ServiceLister
	podLister          listerscorev1.PodLister
	secretLister       listerscorev1.SecretLister
	nodeLister         listerscorev1.NodeLister
	gatewayClassLister gatewaylistersv1.GatewayClassLister
	gatewayLister      gatewaylistersv1.GatewayLister
	httpRouteLister    gatewaylistersv1.HTTPRouteLister

	ingQueue          workqueue.TypedRateLimitingInterface[types.NamespacedName]
	secretQueue       workqueue.TypedRateLimitingInterface[types.NamespacedName]
	gatewayClassQueue workqueue.TypedRateLimitingInterface[types.NamespacedName]
	gatewayQueue      workqueue.TypedRateLimitingInterface[types.NamespacedName]
	httpRouteQueue    workqueue.TypedRateLimitingInterface[types.NamespacedName]

	// timeNow returns the current time and abstracted for test.
	timeNow func() metav1.Time
}

func NewLeaderController(ctx context.Context, lbc *LoadBalancerController) (*LeaderController, error) {
	log := klog.LoggerWithName(klog.FromContext(ctx), "leaderController")

	ctx = klog.NewContext(ctx, log)

	lc := &LeaderController{
		lbc:              lbc,
		watchNSInformers: informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod, informers.WithNamespace(lbc.watchNamespace)),
		podNSInformers: informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod,
			informers.WithNamespace(lbc.pod.Namespace),
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.LabelSelector = podLabelSelector(lbc.pod.Labels).String()
			}),
		),
		allNSInformers: informers.NewSharedInformerFactory(lbc.clientset, noResyncPeriod),
		secretInformers: informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod,
			informers.WithNamespace(lbc.nghttpxSecret.Namespace),
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.FieldSelector = "metadata.name=" + lbc.nghttpxSecret.Name
			}),
		),
		ingQueue: workqueue.NewTypedRateLimitingQueue[types.NamespacedName](workqueue.NewTypedMaxOfRateLimiter[types.NamespacedName](
			workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](5*time.Millisecond, 30*time.Second),
			&workqueue.TypedBucketRateLimiter[types.NamespacedName]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		)),
		secretQueue: workqueue.NewTypedRateLimitingQueue[types.NamespacedName](workqueue.NewTypedMaxOfRateLimiter[types.NamespacedName](
			workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](5*time.Millisecond, 30*time.Second),
			&workqueue.TypedBucketRateLimiter[types.NamespacedName]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		)),
		timeNow: metav1.Now,
	}

	{
		f := lc.watchNSInformers.Networking().V1().Ingresses()
		lc.ingLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*networkingv1.Ingress]{
			validate: func(ctx context.Context, ing *networkingv1.Ingress, event resourceEvent) bool {
				return event != resourceEventDelete && lc.validateIngressClass(ctx, ing)
			},
			handler: func(_ context.Context, ing *networkingv1.Ingress) {
				lc.enqueueIngress(ing)
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Ingress event handler")
			return nil, err
		}

		lc.ingIndexer = inf.GetIndexer()
	}

	{
		f := lc.podNSInformers.Core().V1().Pods()
		lc.podLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.Pod]{
			handler: func(ctx context.Context, _ *corev1.Pod) {
				lc.enqueueIngressAll(ctx)
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Pod event handler")
			return nil, err
		}

		lc.podIndexer = inf.GetIndexer()
	}

	{
		f := lc.secretInformers.Core().V1().Secrets()
		lc.secretLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.Secret]{
			handler: func(_ context.Context, secret *corev1.Secret) {
				lc.enqueueSecret(secret)
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Secret event handler")
			return nil, err
		}

		lc.secretIndexer = inf.GetIndexer()
	}

	if lbc.publishService != nil {
		lc.svcInformers = informers.NewSharedInformerFactoryWithOptions(lbc.clientset, noResyncPeriod,
			informers.WithNamespace(lbc.publishService.Namespace),
			informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
				opts.FieldSelector = "metadata.name=" + lbc.publishService.Name
			}),
		)

		f := lc.svcInformers.Core().V1().Services()
		lc.svcLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*corev1.Service]{
			handler: func(ctx context.Context, _ *corev1.Service) {
				lc.enqueueIngressAll(ctx)
			},
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add Service event handler")
			return nil, err
		}

		lc.svcIndexer = inf.GetIndexer()
	}

	{
		f := lc.allNSInformers.Networking().V1().IngressClasses()
		lc.ingClassLister = f.Lister()
		inf := f.Informer()

		if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*networkingv1.IngressClass]{
			handler: lc.enqueueIngressWithIngressClass,
		}.Build(ctx)); err != nil {
			log.Error(err, "Unable to add IngressClass event handler")
		}

		lc.ingClassIndexer = inf.GetIndexer()
	}

	{
		f := lc.allNSInformers.Core().V1().Nodes()
		lc.nodeLister = f.Lister()
		lc.nodeIndexer = f.Informer().GetIndexer()
	}

	if lbc.gatewayAPI {
		lc.gatewayClassQueue = workqueue.NewTypedRateLimitingQueue[types.NamespacedName](workqueue.NewTypedMaxOfRateLimiter[types.NamespacedName](
			workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](5*time.Millisecond, 30*time.Second),
			&workqueue.TypedBucketRateLimiter[types.NamespacedName]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		))

		lc.gatewayQueue = workqueue.NewTypedRateLimitingQueue[types.NamespacedName](workqueue.NewTypedMaxOfRateLimiter[types.NamespacedName](
			workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](5*time.Millisecond, 30*time.Second),
			&workqueue.TypedBucketRateLimiter[types.NamespacedName]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		))

		lc.httpRouteQueue = workqueue.NewTypedRateLimitingQueue[types.NamespacedName](workqueue.NewTypedMaxOfRateLimiter[types.NamespacedName](
			workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](5*time.Millisecond, 30*time.Second),
			&workqueue.TypedBucketRateLimiter[types.NamespacedName]{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		))

		lc.gatewayInformers = gatewayinformers.NewSharedInformerFactoryWithOptions(
			lbc.gatewayClientset, noResyncPeriod, gatewayinformers.WithNamespace(lbc.watchNamespace))

		{
			f := lc.gatewayInformers.Gateway().V1().GatewayClasses()
			lc.gatewayClassLister = f.Lister()
			inf := f.Informer()

			if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*gatewayv1.GatewayClass]{
				validate: func(_ context.Context, gc *gatewayv1.GatewayClass, _ resourceEvent) bool {
					return gc.Spec.ControllerName == lc.lbc.gatewayClassController
				},
				handler: func(_ context.Context, gc *gatewayv1.GatewayClass) {
					lc.enqueueGatewayClass(gc)
				},
			}.Build(ctx)); err != nil {
				log.Error(err, "Unable to add GatewayClass event handler")
				return nil, err
			}

			lc.gatewayClassIndexer = inf.GetIndexer()
		}

		{
			f := lc.gatewayInformers.Gateway().V1().Gateways()
			lc.gatewayLister = f.Lister()
			inf := f.Informer()

			if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*gatewayv1.Gateway]{
				validate: func(ctx context.Context, gtw *gatewayv1.Gateway, _ resourceEvent) bool {
					return lc.validateGatewayGatewayClass(ctx, gtw)
				},
				handler: func(ctx context.Context, gtw *gatewayv1.Gateway) {
					lc.enqueueGateway(gtw)
					lc.enqueueHTTPRouteFromGateway(ctx, gtw)
				},
			}.Build(ctx)); err != nil {
				log.Error(err, "Unable to add Gateway event handler")
				return nil, err
			}

			lc.gatewayIndexer = inf.GetIndexer()
		}

		{
			f := lc.gatewayInformers.Gateway().V1().HTTPRoutes()
			lc.httpRouteLister = f.Lister()
			inf := f.Informer()

			if _, err := inf.AddEventHandler(resourceEventHandlerBuilder[*gatewayv1.HTTPRoute]{
				validate: func(ctx context.Context, httpRoute *gatewayv1.HTTPRoute, _ resourceEvent) bool {
					return lc.validateHTTPRouteGatewayClass(ctx, httpRoute)
				},
				handler: func(_ context.Context, httpRoute *gatewayv1.HTTPRoute) {
					lc.enqueueHTTPRoute(httpRoute)
				},
			}.Build(ctx)); err != nil {
				log.Error(err, "Unable to add HTTPRoute event handler")
				return nil, err
			}

			lc.httpRouteIndexer = inf.GetIndexer()
		}
	}

	return lc, nil
}

func (lc *LeaderController) Run(ctx context.Context) error {
	log := klog.LoggerWithName(klog.FromContext(ctx), "leaderController")

	ctx = klog.NewContext(ctx, log)

	log.Info("Starting leader controller")

	var wg sync.WaitGroup

	allInformers := []informers.SharedInformerFactory{lc.watchNSInformers, lc.podNSInformers, lc.allNSInformers}

	if lc.secretInformers != nil {
		allInformers = append(allInformers, lc.secretInformers)
	}

	if lc.svcInformers != nil {
		allInformers = append(allInformers, lc.svcInformers)
	}

	for _, f := range allInformers {
		f.Start(ctx.Done())
		defer f.Shutdown()
	}

	if lc.gatewayInformers != nil {
		lc.gatewayInformers.Start(ctx.Done())
		defer lc.gatewayInformers.Shutdown()
	}

	for _, f := range allInformers {
		for v, ok := range f.WaitForCacheSync(ctx.Done()) {
			if !ok {
				return fmt.Errorf("Unable to sync cache %v", v)
			}
		}
	}

	if lc.gatewayInformers != nil {
		for v, ok := range lc.gatewayInformers.WaitForCacheSync(ctx.Done()) {
			if !ok {
				return fmt.Errorf("Unable to sync cache: %v", v)
			}
		}
	}

	// Add Secret to queue so that we can create it if missing.
	lc.secretQueue.Add(lc.lbc.nghttpxSecret)

	wg.Add(1)

	go func() {
		defer wg.Done()

		lc.secretWorker(ctx)
	}()

	wg.Add(1)

	go func() {
		defer wg.Done()

		lc.ingressWorker(ctx)
	}()

	if lc.lbc.gatewayAPI {
		wg.Add(1)

		go func() {
			defer wg.Done()

			lc.gatewayClassWorker(ctx)
		}()

		wg.Add(1)

		go func() {
			defer wg.Done()

			lc.gatewayWorker(ctx)
		}()

		wg.Add(1)

		go func() {
			defer wg.Done()

			lc.httpRouteWorker(ctx)
		}()
	}

	<-ctx.Done()

	log.Info("Shutting down leader controller")

	lc.ingQueue.ShutDown()

	if lc.lbc.http3 {
		lc.secretQueue.ShutDown()
	}

	if lc.lbc.gatewayAPI {
		lc.gatewayClassQueue.ShutDown()
		lc.gatewayQueue.ShutDown()
		lc.httpRouteQueue.ShutDown()
	}

	wg.Wait()

	return ctx.Err()
}

func (lc *LeaderController) enqueueIngress(ing *networkingv1.Ingress) {
	lc.ingQueue.Add(namespacedName(ing))
}

func (lc *LeaderController) enqueueIngressWithIngressClass(ctx context.Context, ingClass *networkingv1.IngressClass) {
	log := klog.FromContext(ctx)

	if ingClass.Spec.Controller != lc.lbc.ingressClassController {
		return
	}

	ings, err := lc.ingLister.Ingresses(ingClass.Namespace).List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list Ingress")
		return
	}

	defaultIngClass := ingClass.Annotations[networkingv1.AnnotationIsDefaultIngressClass] == "true"

	for _, ing := range ings {
		switch {
		case ing.Spec.IngressClassName == nil:
			if lc.lbc.requireIngressClass || !defaultIngClass {
				continue
			}
		case *ing.Spec.IngressClassName != ingClass.Name:
			continue
		}

		lc.enqueueIngress(ing)
	}
}

func (lc *LeaderController) enqueueIngressAll(ctx context.Context) {
	log := klog.FromContext(ctx)

	ings, err := lc.ingLister.List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list Ingress")
		return
	}

	for _, ing := range ings {
		if !lc.validateIngressClass(ctx, ing) {
			continue
		}

		lc.enqueueIngress(ing)
	}
}

func (lc *LeaderController) validateIngressClass(ctx context.Context, ing *networkingv1.Ingress) bool {
	return validateIngressClass(ctx, ing, lc.lbc.ingressClassController, lc.ingClassLister, lc.lbc.requireIngressClass)
}

func (lc *LeaderController) enqueueSecret(s *corev1.Secret) {
	lc.secretQueue.Add(namespacedName(s))
}

func (lc *LeaderController) secretWorker(ctx context.Context) {
	log := klog.FromContext(ctx)

	work := func() bool {
		key, quit := lc.secretQueue.Get()
		if quit {
			return true
		}

		defer lc.secretQueue.Done(key)

		log := klog.LoggerWithValues(log, "secret", key, "reconcileID", uuid.NewUUID())

		ctx, cancel := context.WithTimeout(klog.NewContext(ctx, log), lc.lbc.reconcileTimeout)
		defer cancel()

		if err := lc.syncSecret(ctx, key, time.Now()); err != nil {
			log.Error(err, "Unable to sync QUIC keying materials")
			lc.secretQueue.AddRateLimited(key)
		} else {
			lc.secretQueue.Forget(key)
		}

		return false
	}

	for {
		if quit := work(); quit {
			return
		}
	}
}

func (lc *LeaderController) syncSecret(ctx context.Context, key types.NamespacedName, now time.Time) error {
	log := klog.FromContext(ctx)

	log.V(2).Info("Syncing Secret")

	tstamp := now.Format(time.RFC3339)

	requeueAfter := 12 * time.Hour

	secret, err := lc.secretLister.Secrets(key.Namespace).Get(key.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "Unable to get Secret")
			return err
		}

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
		}

		if lc.lbc.shareTLSTicketKey || lc.lbc.http3 {
			secret.Annotations = make(map[string]string)
			secret.Data = make(map[string][]byte)
		}

		if lc.lbc.shareTLSTicketKey {
			key, err := nghttpx.NewInitialTLSTicketKey()
			if err != nil {
				return err
			}

			secret.Annotations[tlsTicketKeyUpdateTimestampKey] = tstamp
			secret.Data[nghttpxTLSTicketKeySecretKey] = key

			requeueAfter = min(requeueAfter, lc.lbc.tlsTicketKeyPeriod)
		}

		if lc.lbc.http3 {
			key, err := nghttpx.NewInitialQUICKeyingMaterials()
			if err != nil {
				return err
			}

			secret.Annotations[quicKeyingMaterialsUpdateTimestampKey] = tstamp
			secret.Data[nghttpxQUICKeyingMaterialsSecretKey] = key

			requeueAfter = min(requeueAfter, lc.lbc.quicSecretPeriod)
		}

		if _, err := lc.lbc.clientset.CoreV1().Secrets(secret.Namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
			log.Error(err, "Unable to create Secret")
			return err
		}

		log.Info("Secret was created", "requeueAfter", requeueAfter)

		// If Secret has been added to the queue and is waiting, this effectively overrides it.
		lc.secretQueue.AddAfter(key, requeueAfter)

		return nil
	}

	var (
		ticketKey       []byte
		ticketKeyUpdate bool
	)

	if lc.lbc.shareTLSTicketKey {
		var ticketKeyAddAfter time.Duration

		ticketKey, ticketKeyUpdate, ticketKeyAddAfter = lc.getTLSTicketKeyFromSecret(ctx, secret, now)

		if ticketKeyAddAfter != 0 {
			requeueAfter = min(requeueAfter, ticketKeyAddAfter)
		}
	}

	var (
		quicKM       []byte
		quicKMUpdate bool
	)

	if lc.lbc.http3 {
		var quicKMAddAfter time.Duration

		quicKM, quicKMUpdate, quicKMAddAfter = lc.getQUICKeyingMaterialsFromSecret(ctx, secret, now)

		if quicKMAddAfter != 0 {
			requeueAfter = min(requeueAfter, quicKMAddAfter)
		}
	}

	if !ticketKeyUpdate && !quicKMUpdate {
		log.Info("No update is required", "requeueAfter", requeueAfter)

		lc.secretQueue.AddAfter(key, requeueAfter)

		return nil
	}

	updatedSecret := secret.DeepCopy()
	if updatedSecret.Annotations == nil {
		updatedSecret.Annotations = make(map[string]string)
	}

	if updatedSecret.Data == nil {
		updatedSecret.Data = make(map[string][]byte)
	}

	if ticketKeyUpdate {
		var (
			key []byte
			err error
		)

		if len(ticketKey) == 0 {
			key, err = nghttpx.NewInitialTLSTicketKey()
		} else {
			key, err = nghttpx.UpdateTLSTicketKey(ticketKey)
		}

		if err != nil {
			return err
		}

		updatedSecret.Annotations[tlsTicketKeyUpdateTimestampKey] = tstamp
		updatedSecret.Data[nghttpxTLSTicketKeySecretKey] = key

		log.Info("TLS ticket keys were updated")

		requeueAfter = min(requeueAfter, lc.lbc.tlsTicketKeyPeriod)
	}

	if quicKMUpdate {
		var (
			key []byte
			err error
		)

		if len(quicKM) == 0 {
			key, err = nghttpx.NewInitialQUICKeyingMaterials()
		} else {
			key, err = nghttpx.UpdateQUICKeyingMaterials(quicKM)
		}

		if err != nil {
			return err
		}

		updatedSecret.Annotations[quicKeyingMaterialsUpdateTimestampKey] = tstamp
		updatedSecret.Data[nghttpxQUICKeyingMaterialsSecretKey] = key

		log.Info("QUIC keying materials were updated")

		requeueAfter = min(requeueAfter, lc.lbc.quicSecretPeriod)
	}

	if _, err := lc.lbc.clientset.CoreV1().Secrets(updatedSecret.Namespace).Update(ctx, updatedSecret, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Unable to update Secret")
		return err
	}

	log.Info("Secret was updated", "requeueAfter", requeueAfter)

	// If Secret has been added to the queue and is waiting, this effectively overrides it.
	lc.secretQueue.AddAfter(key, requeueAfter)

	return nil
}

func (lc *LeaderController) getTLSTicketKeyFromSecret(ctx context.Context, s *corev1.Secret, t time.Time) (ticketKey []byte, needsUpdate bool, requeueAfter time.Duration) {
	log := klog.FromContext(ctx)

	ts, ok := s.Annotations[tlsTicketKeyUpdateTimestampKey]
	if !ok {
		log.Error(nil, "Secret does not contain the annotation", "annotation", tlsTicketKeyUpdateTimestampKey)

		return nil, true, 0
	}

	lastUpdate, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		log.Error(err, "Unable to parse timestamp", "annotation", tlsTicketKeyUpdateTimestampKey)

		return nil, true, 0
	}

	ticketKey = s.Data[nghttpxTLSTicketKeySecretKey]

	if err := nghttpx.VerifyTLSTicketKey(ticketKey); err != nil {
		log.Error(err, "TLS ticket keys are malformed")

		return nil, true, 0
	}

	requeueAfter = lastUpdate.Add(lc.lbc.tlsTicketKeyPeriod).Sub(t)
	if requeueAfter > 0 {
		log.Info("TLS ticket keys are not expired and in a good shape", "requeueAfter", requeueAfter)

		return ticketKey, false, requeueAfter
	}

	return ticketKey, true, 0
}

func (lc *LeaderController) getQUICKeyingMaterialsFromSecret(ctx context.Context, s *corev1.Secret, t time.Time) (quicKM []byte, needsUpdate bool, requeueAfter time.Duration) {
	log := klog.FromContext(ctx)

	ts, ok := s.Annotations[quicKeyingMaterialsUpdateTimestampKey]
	if !ok {
		log.Error(nil, "Secret does not contain the annotation", "annotation", quicKeyingMaterialsUpdateTimestampKey)

		return nil, true, 0
	}

	lastUpdate, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		log.Error(err, "Unable to parse timestamp", "annotation", quicKeyingMaterialsUpdateTimestampKey)

		return nil, true, 0
	}

	quicKM = s.Data[nghttpxQUICKeyingMaterialsSecretKey]

	if err := nghttpx.VerifyQUICKeyingMaterials(quicKM); err != nil {
		log.Error(err, "QUIC keying materials are malformed")

		return nil, true, 0
	}

	requeueAfter = lastUpdate.Add(lc.lbc.quicSecretPeriod).Sub(t)
	if requeueAfter > 0 {
		log.Info("QUIC keying materials are not expired and in a good shape", "requeueAfter", requeueAfter)

		return quicKM, false, requeueAfter
	}

	return quicKM, true, 0
}

func (lc *LeaderController) ingressWorker(ctx context.Context) {
	log := klog.FromContext(ctx)

	work := func() bool {
		key, quit := lc.ingQueue.Get()
		if quit {
			return true
		}

		defer lc.ingQueue.Done(key)

		log := klog.LoggerWithValues(log, "ingress", key, "reconcileID", uuid.NewUUID())

		ctx, cancel := context.WithTimeout(klog.NewContext(ctx, log), lc.lbc.reconcileTimeout)
		defer cancel()

		if err := lc.syncIngress(ctx, key); err != nil {
			log.Error(err, "Unable to sync Ingress")
			lc.ingQueue.AddRateLimited(key)
		} else {
			lc.ingQueue.Forget(key)
		}

		return false
	}

	for {
		if quit := work(); quit {
			return
		}
	}
}

// syncIngress updates Ingress resource status.
func (lc *LeaderController) syncIngress(ctx context.Context, key types.NamespacedName) error {
	log := klog.FromContext(ctx)

	log.V(2).Info("Syncing Ingress")

	ing, err := lc.ingLister.Ingresses(key.Namespace).Get(key.Name)
	if err != nil {
		log.Error(err, "Unable to get Ingress")
		return err
	}

	if !lc.validateIngressClass(ctx, ing) {
		log.V(4).Info("Ingress is not controlled by this controller")
		// Deletion of LB address from status is done by an Ingress controller that now controls ing.
		return nil
	}

	lbIngs, err := lc.getLoadBalancerIngress(ctx)
	if err != nil {
		return err
	}

	if loadBalancerIngressesIPEqual(ing.Status.LoadBalancer.Ingress, lbIngs) {
		log.V(4).Info("Ingress has correct .Status.LoadBalancer.Ingress")
		return nil
	}

	if log := log.V(4); log.Enabled() {
		log.Info("Update Ingress .Status.LoadBalancer.Ingress", "loadBalancerIngress", klog.Format(lbIngs))
	}

	newIng := ing.DeepCopy()
	newIng.Status.LoadBalancer.Ingress = lbIngs

	if _, err := lc.lbc.clientset.NetworkingV1().Ingresses(ing.Namespace).UpdateStatus(ctx, newIng, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Unable to update Ingress status")
		return err
	}

	return nil
}

func (lc *LeaderController) getLoadBalancerIngress(ctx context.Context) ([]networkingv1.IngressLoadBalancerIngress, error) {
	log := klog.FromContext(ctx)

	var lbIngs []networkingv1.IngressLoadBalancerIngress

	if lc.lbc.publishService == nil {
		var err error

		lbIngs, err = lc.getLoadBalancerIngressSelector(ctx, podLabelSelector(lc.lbc.pod.Labels))
		if err != nil {
			return nil, fmt.Errorf("unable to get Pod or Node IP of Ingress controller: %w", err)
		}
	} else {
		svc, err := lc.svcLister.Services(lc.lbc.publishService.Namespace).Get(lc.lbc.publishService.Name)
		if err != nil {
			log.Error(err, "Unable to get Service", "service", lc.lbc.publishService)
			return nil, err
		}

		lbIngs = ingressLoadBalancerIngressFromService(svc)
	}

	sortLoadBalancerIngress(lbIngs)

	return uniqLoadBalancerIngress(lbIngs), nil
}

// getLoadBalancerIngressSelector creates array of networkingv1.IngressLoadBalancerIngress based on cached Pods and Nodes.
func (lc *LeaderController) getLoadBalancerIngressSelector(ctx context.Context, selector labels.Selector) ([]networkingv1.IngressLoadBalancerIngress, error) {
	log := klog.FromContext(ctx)

	pods, err := lc.podLister.List(selector)
	if err != nil {
		return nil, fmt.Errorf("unable to list Pods with label %v", selector)
	}

	if len(pods) == 0 {
		return nil, nil
	}

	lbIngs := make([]networkingv1.IngressLoadBalancerIngress, 0, len(pods))

	for _, pod := range pods {
		if !pod.Spec.HostNetwork {
			if pod.Status.PodIP == "" {
				continue
			}

			lbIngs = append(lbIngs, networkingv1.IngressLoadBalancerIngress{IP: pod.Status.PodIP})

			continue
		}

		externalIP, err := lc.getPodNodeAddress(pod)
		if err != nil {
			log.Error(err, "Unable to get Pod node address")
			continue
		}

		lbIng := networkingv1.IngressLoadBalancerIngress{}
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

// getPodNodeAddress returns address of Node where pod is running.  It prefers external IP.  It may return internal IP if configuration
// allows it.
func (lc *LeaderController) getPodNodeAddress(pod *corev1.Pod) (string, error) {
	node, err := lc.nodeLister.Get(pod.Spec.NodeName)
	if err != nil {
		return "", fmt.Errorf("unable to get Node %v for Pod %v/%v from lister: %w", pod.Spec.NodeName, pod.Namespace, pod.Name, err)
	}

	var externalIP string

	for i := range node.Status.Addresses {
		address := &node.Status.Addresses[i]
		switch address.Type {
		case corev1.NodeExternalIP:
			if address.Address != "" {
				return address.Address, nil
			}
		case corev1.NodeInternalIP:
			if externalIP == "" && lc.lbc.allowInternalIP {
				// Continue to the next iteration because we may encounter v1.NodeExternalIP later.
				externalIP = address.Address
			}
		}
	}

	if externalIP == "" {
		return "", fmt.Errorf("Node %v has no external IP", node.Name)
	}

	return externalIP, nil
}
