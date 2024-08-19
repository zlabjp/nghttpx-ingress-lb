package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

func (lbc *LoadBalancerController) addGatewayClassNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gc := obj.(*gatewayv1.GatewayClass)

	if gc.Spec.ControllerName != lbc.gatewayClassController {
		return
	}

	log.V(4).Info("GatewayClass added", "gatewayClass", klog.KObj(gc))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateGatewayClassNotification(ctx context.Context, old, cur any) {
	log := klog.FromContext(ctx)

	oldGC := old.(*gatewayv1.GatewayClass)
	curGC := cur.(*gatewayv1.GatewayClass)

	if oldGC.Spec.ControllerName != lbc.gatewayClassController && curGC.Spec.ControllerName != lbc.gatewayClassController {
		return
	}

	log.V(4).Info("GatewayClass updated", "gatewayClass", klog.KObj(curGC))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteGatewayClassNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gc, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Unable to get object from tombstone", "object", obj)
			return
		}

		gc, ok = tombstone.Obj.(*gatewayv1.GatewayClass)
		if !ok {
			log.Error(nil, "Tombstone contained object that is not GatewayClass", "object", obj)
			return
		}
	}

	if gc.Spec.ControllerName != lbc.gatewayClassController {
		return
	}

	log.V(4).Info("GatewayClass deleted", "gatewayClass", klog.KObj(gc))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) addGatewayNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gtw := obj.(*gatewayv1.Gateway)

	if !lbc.validateGatewayGatewayClass(ctx, gtw) {
		return
	}

	log.V(4).Info("Gateway added", "gateway", klog.KObj(gtw))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateGatewayNotification(ctx context.Context, old, cur any) {
	log := klog.FromContext(ctx)

	oldGtw := old.(*gatewayv1.Gateway)
	curGtw := cur.(*gatewayv1.Gateway)

	if !lbc.validateGatewayGatewayClass(ctx, oldGtw) && !lbc.validateGatewayGatewayClass(ctx, curGtw) {
		return
	}

	log.V(4).Info("Gateway updated", "gateway", klog.KObj(curGtw))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteGatewayNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gtw, ok := obj.(*gatewayv1.Gateway)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Unable to get object from tombstone", "object", obj)
			return
		}

		gtw, ok = tombstone.Obj.(*gatewayv1.Gateway)
		if !ok {
			log.Error(nil, "Tombstone contained object that is not Gateway", "object", obj)
			return
		}
	}

	if !lbc.validateGatewayGatewayClass(ctx, gtw) {
		return
	}

	log.V(4).Info("Gateway deleted", "gateway", klog.KObj(gtw))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) addHTTPRouteNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	httpRoute := obj.(*gatewayv1.HTTPRoute)

	if !lbc.validateHTTPRouteGatewayClass(ctx, httpRoute) {
		return
	}

	log.V(4).Info("HTTPRoute added", "httpRoute", klog.KObj(httpRoute))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) updateHTTPRouteNotification(ctx context.Context, old, cur any) {
	log := klog.FromContext(ctx)

	oldHTTPRoute := old.(*gatewayv1.HTTPRoute)
	curHTTPRoute := cur.(*gatewayv1.HTTPRoute)

	if !lbc.validateHTTPRouteGatewayClass(ctx, oldHTTPRoute) && !lbc.validateHTTPRouteGatewayClass(ctx, curHTTPRoute) {
		return
	}

	log.V(4).Info("HTTPRoute updated", "httpRoute", klog.KObj(curHTTPRoute))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) deleteHTTPRouteNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	httpRoute, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Unable to get object from tombstone", "object", obj)
			return
		}

		httpRoute, ok = tombstone.Obj.(*gatewayv1.HTTPRoute)
		if !ok {
			log.Error(nil, "Tombstone contained object that is not HTTPRoute", "object", obj)
			return
		}
	}

	if !lbc.validateHTTPRouteGatewayClass(ctx, httpRoute) {
		return
	}

	log.V(4).Info("HTTPRoute deleted", "httpRoute", klog.KObj(httpRoute))

	lbc.enqueue()
}

func (lbc *LoadBalancerController) createGatewayUpstreams(ctx context.Context, httpRoutes []*gatewayv1.HTTPRoute) (upstreams []*nghttpx.Upstream) {
	log := klog.FromContext(ctx)

	gvk := gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute")

	for _, httpRoute := range httpRoutes {
		if !lbc.validateHTTPRouteGatewayClass(ctx, httpRoute) {
			continue
		}

		log := klog.LoggerWithValues(log, "httpRoute", klog.KObj(httpRoute))
		ctx := klog.NewContext(ctx, log)

		log.V(4).Info("Processing HTTPRoute")

		accepted, requireTLS, hostnames := lbc.httpRouteAccepted(ctx, httpRoute)
		if !accepted {
			continue
		}

		if len(hostnames) == 0 {
			hostnames = []gatewayv1.Hostname{""}
		}

		bcm := ingressAnnotation(httpRoute.Annotations).NewBackendConfigMapper(ctx)
		pcm := ingressAnnotation(httpRoute.Annotations).NewPathConfigMapper(ctx)

		for i := range httpRoute.Spec.Rules {
			rule := &httpRoute.Spec.Rules[i]

			backends, err := lbc.createHTTPRouteBackends(httpRoute, rule.BackendRefs)
			if err != nil {
				log.Error(err, "Unable to create backend")
				continue
			}

			for i := range rule.Matches {
				m := &rule.Matches[i]

				var path string

				if m.Path == nil {
					path = "/"
				} else {
					path = ptr.Deref(m.Path.Value, "/")
				}

				for _, hostname := range hostnames {
					for i := range backends {
						isb := &backends[i]

						ups, err := lbc.createUpstream(ctx, gvk, httpRoute, string(hostname), path, isb, requireTLS, pcm, bcm)
						if err != nil {
							log.Error(err, "Unable to create backend", "hostname", hostname, "path", path)
							continue
						}

						upstreams = append(upstreams, ups)
					}
				}
			}
		}
	}

	return
}

func (lbc *LoadBalancerController) httpRouteAccepted(ctx context.Context, httpRoute *gatewayv1.HTTPRoute) (accepted, requireTLS bool, hostnames []gatewayv1.Hostname) {
	log := klog.FromContext(ctx)

	var requireTLSP *bool

	for i := range httpRoute.Spec.ParentRefs {
		paRef := &httpRoute.Spec.ParentRefs[i]

		if !parentGateway(paRef, httpRoute.Namespace) {
			continue
		}

		paStatus := findHTTPRouteParentStatus(httpRoute, paRef)
		if paStatus == nil {
			continue
		}

		if cond := findCondition(paStatus.Conditions, string(gatewayv1.RouteConditionAccepted)); cond == nil || cond.Status != metav1.ConditionTrue {
			continue
		}

		gtw, err := lbc.gatewayLister.Gateways(httpRoute.Namespace).Get(string(paRef.Name))
		if err != nil {
			log.Error(err, "Unable to get Gateway")
			continue
		}

		if !lbc.validateGatewayGatewayClass(ctx, gtw) {
			continue
		}

		if cond := findCondition(gtw.Status.Conditions, string(gatewayv1.GatewayConditionAccepted)); cond == nil || cond.Status != metav1.ConditionTrue {
			continue
		}

		sectionName := ptr.Deref(paRef.SectionName, gatewayv1.SectionName(""))
		if sectionName == "" {
			if len(httpRoute.Spec.Hostnames) > 0 {
				var hosts []gatewayv1.Hostname

				for i := range gtw.Spec.Listeners {
					l := &gtw.Spec.Listeners[i]

					if l.Hostname == nil {
						hosts = append(hosts, httpRoute.Spec.Hostnames...)
						break
					}

					for _, h := range httpRoute.Spec.Hostnames {
						if hostnameMatch(*l.Hostname, h) {
							hosts = append(hosts, h)
						}
					}
				}

				if len(hosts) == 0 {
					continue
				}

				hostnames = append(hostnames, hosts...)
			} else {
				for i := range gtw.Spec.Listeners {
					l := &gtw.Spec.Listeners[i]

					if l.Hostname != nil {
						hostnames = append(hostnames, *l.Hostname)
					}
				}
			}

			accepted = true
			requireTLSP = ptr.To(false)

			continue
		}

		lidx := slices.IndexFunc(gtw.Spec.Listeners, func(l gatewayv1.Listener) bool {
			return l.Name == sectionName
		})
		if lidx == -1 {
			continue
		}

		l := &gtw.Spec.Listeners[lidx]

		if l.Hostname == nil {
			hostnames = append(hostnames, httpRoute.Spec.Hostnames...)
		} else if len(httpRoute.Spec.Hostnames) > 0 {
			var hosts []gatewayv1.Hostname

			for _, h := range httpRoute.Spec.Hostnames {
				if hostnameMatch(*l.Hostname, h) {
					hosts = append(hosts, h)
				}
			}

			if len(hosts) == 0 {
				continue
			}

			hostnames = append(hostnames, hosts...)
		} else {
			hostnames = append(hostnames, *l.Hostname)
		}

		accepted = true

		if gtw.Spec.Listeners[lidx].Protocol == gatewayv1.HTTPSProtocolType {
			if requireTLSP == nil {
				requireTLSP = ptr.To(true)
			}
		} else {
			requireTLSP = ptr.To(false)
		}
	}

	slices.Sort(hostnames)
	hostnames = slices.Compact(hostnames)

	return accepted, ptr.Deref(requireTLSP, false), hostnames
}

func (lbc *LoadBalancerController) createHTTPRouteBackends(httpRoute *gatewayv1.HTTPRoute,
	backendRefs []gatewayv1.HTTPBackendRef) ([]networkingv1.IngressServiceBackend, error) {
	isbs := make([]networkingv1.IngressServiceBackend, len(backendRefs))

	for i := range backendRefs {
		bkRef := &backendRefs[i]

		if ptr.Deref(bkRef.Group, "") != "" ||
			ptr.Deref(bkRef.Kind, "Service") != "Service" ||
			ptr.Deref(bkRef.Namespace, gatewayv1.Namespace(httpRoute.Namespace)) != gatewayv1.Namespace(httpRoute.Namespace) {
			return nil, errors.New("Backend is not Service")
		}

		if bkRef.Port == nil {
			return nil, errors.New("Service port is omitted")
		}

		isbs[i] = networkingv1.IngressServiceBackend{
			Name: string(bkRef.Name),
			Port: networkingv1.ServiceBackendPort{
				Number: int32(*bkRef.Port),
			},
		}
	}

	return isbs, nil
}

func (lbc *LoadBalancerController) createGatewayCredentials(ctx context.Context, gtws []*gatewayv1.Gateway) []*nghttpx.TLSCred {
	log := klog.FromContext(ctx)

	var creds []*nghttpx.TLSCred

	for _, gtw := range gtws {
		if !lbc.validateGatewayGatewayClass(ctx, gtw) {
			continue
		}

		if cond := findCondition(gtw.Status.Conditions, string(gatewayv1.GatewayConditionAccepted)); cond == nil || cond.Status != metav1.ConditionTrue {
			continue
		}

		for i := range gtw.Spec.Listeners {
			l := &gtw.Spec.Listeners[i]

			if l.Protocol != gatewayv1.HTTPSProtocolType || l.TLS == nil {
				continue
			}

			for i := range l.TLS.CertificateRefs {
				certRef := &l.TLS.CertificateRefs[i]

				if ptr.Deref(certRef.Group, "") != "" ||
					ptr.Deref(certRef.Kind, "Secret") != "Secret" ||
					ptr.Deref(certRef.Namespace, gatewayv1.Namespace(gtw.Namespace)) != gatewayv1.Namespace(gtw.Namespace) {
					continue
				}

				secret, err := lbc.secretLister.Secrets(gtw.Namespace).Get(string(certRef.Name))
				if err != nil {
					log.Error(err, "Unable to get Secret")
					continue
				}

				tlsCred, err := lbc.createTLSCredFromSecret(ctx, secret)
				if err != nil {
					log.Error(err, "Unable to get TLS credentials from Secret")
					continue
				}

				creds = append(creds, tlsCred)
			}
		}
	}

	return creds
}

func (lbc *LoadBalancerController) validateGatewayGatewayClass(ctx context.Context, gtw *gatewayv1.Gateway) bool {
	return validateGatewayGatewayClass(ctx, gtw, lbc.gatewayClassController, lbc.gatewayClassLister)
}

func (lbc *LoadBalancerController) validateHTTPRouteGatewayClass(ctx context.Context, httpRoute *gatewayv1.HTTPRoute) bool {
	return validateHTTPRouteGatewayClass(ctx, httpRoute, lbc.gatewayClassController, lbc.gatewayClassLister, lbc.gatewayLister)
}

func (lc *LeaderController) addGatewayClassNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gc := obj.(*gatewayv1.GatewayClass)

	if gc.Spec.ControllerName != lc.lbc.gatewayClassController {
		return
	}

	log.V(4).Info("GatewayClass added", "gatewayClass", klog.KObj(gc))

	lc.enqueueGatewayClass(gc)
}

func (lc *LeaderController) updateGatewayClassNotification(ctx context.Context, old, cur any) {
	log := klog.FromContext(ctx)

	oldGC := old.(*gatewayv1.GatewayClass)
	curGC := cur.(*gatewayv1.GatewayClass)

	if oldGC.Spec.ControllerName != lc.lbc.gatewayClassController && curGC.Spec.ControllerName != lc.lbc.gatewayClassController {
		return
	}

	log.V(4).Info("GatewayClass updated", "gatewayClass", klog.KObj(curGC))

	lc.enqueueGatewayClass(curGC)
}

func (lc *LeaderController) deleteGatewayClassNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gc, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Unable to get object from tombstone", "object", obj)
			return
		}

		gc, ok = tombstone.Obj.(*gatewayv1.GatewayClass)
		if !ok {
			log.Error(nil, "Tombstone contained object that is not GatewayClass", "object", obj)
			return
		}
	}

	if gc.Spec.ControllerName != lc.lbc.gatewayClassController {
		return
	}

	log.V(4).Info("GatewayClass deleted", "gatewayClass", klog.KObj(gc))

	lc.enqueueGatewayClass(gc)
}

func (lc *LeaderController) addGatewayNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gtw := obj.(*gatewayv1.Gateway)

	if !lc.validateGatewayGatewayClass(ctx, gtw) {
		return
	}

	log.V(4).Info("Gateway added", "gateway", klog.KObj(gtw))

	lc.enqueueGateway(gtw)
	lc.enqueueHTTPRouteFromGateway(ctx, gtw)
}

func (lc *LeaderController) updateGatewayNotification(ctx context.Context, old, cur any) {
	log := klog.FromContext(ctx)

	oldGtw := old.(*gatewayv1.Gateway)
	curGtw := cur.(*gatewayv1.Gateway)

	if !lc.validateGatewayGatewayClass(ctx, oldGtw) && !lc.validateGatewayGatewayClass(ctx, curGtw) {
		return
	}

	log.V(4).Info("Gateway updated", "gateway", klog.KObj(curGtw))

	lc.enqueueGateway(curGtw)
	lc.enqueueHTTPRouteFromGateway(ctx, curGtw)
}

func (lc *LeaderController) deleteGatewayNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	gtw, ok := obj.(*gatewayv1.Gateway)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Unable to get object from tombstone", "object", obj)
			return
		}

		gtw, ok = tombstone.Obj.(*gatewayv1.Gateway)
		if !ok {
			log.Error(nil, "Tombstone contained object that is not Gateway", "object", obj)
			return
		}
	}

	if !lc.validateGatewayGatewayClass(ctx, gtw) {
		return
	}

	log.V(4).Info("Gateway deleted", "gateway", klog.KObj(gtw))

	lc.enqueueGateway(gtw)
	lc.enqueueHTTPRouteFromGateway(ctx, gtw)
}

func (lc *LeaderController) addHTTPRouteNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	httpRoute := obj.(*gatewayv1.HTTPRoute)

	if !lc.validateHTTPRouteGatewayClass(ctx, httpRoute) {
		return
	}

	log.V(4).Info("HTTPRoute added", "httpRoute", klog.KObj(httpRoute))

	lc.enqueueHTTPRoute(httpRoute)
}

func (lc *LeaderController) updateHTTPRouteNotification(ctx context.Context, old, cur any) {
	log := klog.FromContext(ctx)

	oldHTTPRoute := old.(*gatewayv1.HTTPRoute)
	curHTTPRoute := cur.(*gatewayv1.HTTPRoute)

	if !lc.validateHTTPRouteGatewayClass(ctx, oldHTTPRoute) && !lc.validateHTTPRouteGatewayClass(ctx, curHTTPRoute) {
		return
	}

	log.V(4).Info("HTTPRoute updated", "httpRoute", klog.KObj(curHTTPRoute))

	lc.enqueueHTTPRoute(curHTTPRoute)
}

func (lc *LeaderController) deleteHTTPRouteNotification(ctx context.Context, obj any) {
	log := klog.FromContext(ctx)

	httpRoute, ok := obj.(*gatewayv1.HTTPRoute)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			log.Error(nil, "Unable to get object from tombstone", "object", obj)
			return
		}

		httpRoute, ok = tombstone.Obj.(*gatewayv1.HTTPRoute)
		if !ok {
			log.Error(nil, "Tombstone contained object that is not HTTPRoute", "object", obj)
			return
		}
	}

	if !lc.validateHTTPRouteGatewayClass(ctx, httpRoute) {
		return
	}

	log.V(4).Info("HTTPRoute deleted", "httpRoute", klog.KObj(httpRoute))

	lc.enqueueHTTPRoute(httpRoute)
}

func (lc *LeaderController) enqueueGatewayClass(gc *gatewayv1.GatewayClass) {
	lc.gatewayClassQueue.Add(namespacedName(gc))
}

func (lc *LeaderController) enqueueGateway(gtw *gatewayv1.Gateway) {
	lc.gatewayQueue.Add(namespacedName(gtw))
}

func (lc *LeaderController) enqueueHTTPRoute(httpRoute *gatewayv1.HTTPRoute) {
	lc.httpRouteQueue.Add(namespacedName(httpRoute))
}

func (lc *LeaderController) enqueueHTTPRouteFromGateway(ctx context.Context, gtw *gatewayv1.Gateway) {
	log := klog.FromContext(ctx)

	httpRoutes, err := lc.httpRouteLister.HTTPRoutes(gtw.Namespace).List(labels.Everything())
	if err != nil {
		log.Error(err, "Unable to list HTTPRoute")
		return
	}

	for _, httpRoute := range httpRoutes {
		if slices.ContainsFunc(httpRoute.Spec.ParentRefs, func(paRef gatewayv1.ParentReference) bool {
			return parentGateway(&paRef, httpRoute.Namespace) && paRef.Name == gatewayv1.ObjectName(gtw.Name)
		}) {
			lc.enqueueHTTPRoute(httpRoute)
		}
	}
}

func (lc *LeaderController) validateGatewayGatewayClass(ctx context.Context, gtw *gatewayv1.Gateway) bool {
	return validateGatewayGatewayClass(ctx, gtw, lc.lbc.gatewayClassController, lc.gatewayClassLister)
}

func (lc *LeaderController) validateHTTPRouteGatewayClass(ctx context.Context, httpRoute *gatewayv1.HTTPRoute) bool {
	return validateHTTPRouteGatewayClass(ctx, httpRoute, lc.lbc.gatewayClassController, lc.gatewayClassLister, lc.gatewayLister)
}

func (lc *LeaderController) gatewayClassWorker(ctx context.Context) {
	log := klog.FromContext(ctx)

	work := func() bool {
		key, quit := lc.gatewayClassQueue.Get()
		if quit {
			return true
		}

		defer lc.gatewayClassQueue.Done(key)

		log := klog.LoggerWithValues(log, "gatewayClass", key, "reconcileID", uuid.NewUUID())

		ctx, cancel := context.WithTimeout(klog.NewContext(ctx, log), lc.lbc.reconcileTimeout)
		defer cancel()

		if err := lc.syncGatewayClass(ctx, key); err != nil {
			log.Error(err, "Unable to sync GatewayClass")
			lc.gatewayClassQueue.AddRateLimited(key)
		} else {
			lc.gatewayClassQueue.Forget(key)
		}

		return false
	}

	for {
		if quit := work(); quit {
			return
		}
	}
}

func (lc *LeaderController) syncGatewayClass(ctx context.Context, key types.NamespacedName) error {
	log := klog.FromContext(ctx)

	log.V(2).Info("Syncing GatewayClass")

	gc, err := lc.gatewayClassLister.Get(key.Name)
	if err != nil {
		log.Error(err, "Unable to get GatewayClass")
		return err
	}

	if gc.Spec.ControllerName != lc.lbc.gatewayClassController {
		log.V(4).Info("GatewayClass is not controlled by this controller")
		return nil
	}

	newGC := gc.DeepCopy()

	cond := findCondition(newGC.Status.Conditions, string(gatewayv1.GatewayClassConditionStatusAccepted))
	if cond == nil {
		newGC.Status.Conditions, cond = appendCondition(newGC.Status.Conditions,
			metav1.Condition{Type: string(gatewayv1.GatewayClassConditionStatusAccepted)})
	}

	cond.Reason = string(gatewayv1.GatewayClassReasonAccepted)
	cond.Message = ""
	cond.ObservedGeneration = newGC.Generation

	if cond.Status != metav1.ConditionTrue {
		cond.Status = metav1.ConditionTrue
		cond.LastTransitionTime = lc.timeNow()
	}

	if equality.Semantic.DeepEqual(gc.Status, newGC.Status) {
		return nil
	}

	if _, err := lc.lbc.gatewayClientset.GatewayV1().GatewayClasses().UpdateStatus(ctx, newGC, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Unable to update GatewayClass status")
		return err
	}

	return nil
}

func (lc *LeaderController) gatewayWorker(ctx context.Context) {
	log := klog.FromContext(ctx)

	work := func() bool {
		key, quit := lc.gatewayQueue.Get()
		if quit {
			return true
		}

		defer lc.gatewayQueue.Done(key)

		log := klog.LoggerWithValues(log, "gateway", key, "reconcileID", uuid.NewUUID())

		ctx, cancel := context.WithTimeout(klog.NewContext(ctx, log), lc.lbc.reconcileTimeout)
		defer cancel()

		if err := lc.syncGateway(ctx, key); err != nil {
			log.Error(err, "Unable to sync Gateway")
			lc.gatewayQueue.AddRateLimited(key)
		} else {
			lc.gatewayQueue.Forget(key)
		}

		return false
	}

	for {
		if quit := work(); quit {
			return
		}
	}
}

func (lc *LeaderController) syncGateway(ctx context.Context, key types.NamespacedName) error {
	log := klog.FromContext(ctx)

	log.V(2).Info("Syncing Gateway")

	gtw, err := lc.gatewayLister.Gateways(key.Namespace).Get(key.Name)
	if err != nil {
		log.Error(err, "Unable to get Gateway")
		return err
	}

	if !lc.validateGatewayGatewayClass(ctx, gtw) {
		log.V(4).Info("Gateway is not controlled by this controller")
		return nil
	}

	for i := range gtw.Spec.Listeners {
		l := &gtw.Spec.Listeners[i]

		switch l.Protocol {
		case gatewayv1.HTTPProtocolType:
			if l.TLS != nil {
				return lc.updateGatewayStatusWithError(ctx, gtw, fmt.Errorf(".spec.listeners[%v].tls must not be set", i))
			}
		case gatewayv1.HTTPSProtocolType:
			if l.TLS == nil {
				return lc.updateGatewayStatusWithError(ctx, gtw, fmt.Errorf(".spec.listeners[%v].tls is required", i))
			}

			if ptr.Deref(l.TLS.Mode, gatewayv1.TLSModeTerminate) != gatewayv1.TLSModeTerminate {
				return lc.updateGatewayStatusWithError(ctx, gtw, fmt.Errorf(".spec.listeners[%v].tls.mode must be Terminate", i))
			}

			if len(l.TLS.CertificateRefs) == 0 {
				return lc.updateGatewayStatusWithError(ctx, gtw, fmt.Errorf(".spec.listeners[%v].tls.certificateRefs must contain at least one reference", i))
			}
		default:
			return lc.updateGatewayStatusWithError(ctx, gtw, fmt.Errorf(".spec.listeners[%v].protocol is not supported: %v", i, l.Protocol))
		}
	}

	newGtw := gtw.DeepCopy()

	t := lc.timeNow()

	cond := findCondition(newGtw.Status.Conditions, string(gatewayv1.GatewayConditionAccepted))
	if cond == nil {
		newGtw.Status.Conditions, cond = appendCondition(newGtw.Status.Conditions,
			metav1.Condition{Type: string(gatewayv1.GatewayConditionAccepted)})
	}

	cond.Reason = string(gatewayv1.GatewayReasonAccepted)
	cond.Message = ""
	cond.ObservedGeneration = newGtw.Generation

	if cond.Status != metav1.ConditionTrue {
		cond.Status = metav1.ConditionTrue
		cond.LastTransitionTime = t
	}

	cond = findCondition(newGtw.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if cond == nil {
		newGtw.Status.Conditions, cond = appendCondition(newGtw.Status.Conditions,
			metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed)})
	}

	cond.Reason = string(gatewayv1.GatewayReasonProgrammed)
	cond.Message = ""
	cond.ObservedGeneration = newGtw.Generation

	if cond.Status != metav1.ConditionTrue {
		cond.Status = metav1.ConditionTrue
		cond.LastTransitionTime = t
	}

	if equality.Semantic.DeepEqual(gtw.Status, newGtw.Status) {
		return nil
	}

	if _, err := lc.lbc.gatewayClientset.GatewayV1().Gateways(newGtw.Namespace).UpdateStatus(ctx, newGtw, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Unable to update Gateway status")
		return err
	}

	return nil
}

func (lc *LeaderController) updateGatewayStatusWithError(ctx context.Context, gtw *gatewayv1.Gateway, statusErr error) error {
	log := klog.FromContext(ctx)

	t := lc.timeNow()

	newGtw := gtw.DeepCopy()

	cond := findCondition(newGtw.Status.Conditions, string(gatewayv1.GatewayConditionAccepted))
	if cond == nil {
		newGtw.Status.Conditions, cond = appendCondition(newGtw.Status.Conditions,
			metav1.Condition{Type: string(gatewayv1.GatewayConditionAccepted)})
	}

	cond.Reason = string(gatewayv1.GatewayReasonInvalid)
	cond.Message = statusErr.Error()
	cond.ObservedGeneration = newGtw.Generation

	if cond.Status != metav1.ConditionFalse {
		cond.Status = metav1.ConditionFalse
		cond.LastTransitionTime = t
	}

	cond = findCondition(newGtw.Status.Conditions, string(gatewayv1.GatewayConditionProgrammed))
	if cond == nil {
		newGtw.Status.Conditions, cond = appendCondition(newGtw.Status.Conditions,
			metav1.Condition{Type: string(gatewayv1.GatewayConditionProgrammed)})
	}

	cond.Reason = string(gatewayv1.GatewayReasonInvalid)
	cond.Message = statusErr.Error()
	cond.ObservedGeneration = newGtw.Generation

	if cond.Status != metav1.ConditionFalse {
		cond.Status = metav1.ConditionFalse
		cond.LastTransitionTime = t
	}

	if equality.Semantic.DeepEqual(gtw.Status, newGtw.Status) {
		return nil
	}

	if _, err := lc.lbc.gatewayClientset.GatewayV1().Gateways(newGtw.Namespace).UpdateStatus(ctx, newGtw, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Unable to update Gateway status")
		return err
	}

	return nil
}

func (lc *LeaderController) httpRouteWorker(ctx context.Context) {
	log := klog.FromContext(ctx)

	work := func() bool {
		key, quit := lc.httpRouteQueue.Get()
		if quit {
			return true
		}

		defer lc.httpRouteQueue.Done(key)

		log := klog.LoggerWithValues(log, "httpRoute", key, "reconcileID", uuid.NewUUID())

		ctx, cancel := context.WithTimeout(klog.NewContext(ctx, log), lc.lbc.reconcileTimeout)
		defer cancel()

		if err := lc.syncHTTPRoute(ctx, key); err != nil {
			log.Error(err, "Unable to sync HTTPRoute")
			lc.httpRouteQueue.AddRateLimited(key)
		} else {
			lc.httpRouteQueue.Forget(key)
		}

		return false
	}

	for {
		if quit := work(); quit {
			return
		}
	}
}

func (lc *LeaderController) syncHTTPRoute(ctx context.Context, key types.NamespacedName) error {
	log := klog.FromContext(ctx)

	log.V(2).Info("Syncing HTTPRoute")

	httpRoute, err := lc.httpRouteLister.HTTPRoutes(key.Namespace).Get(key.Name)
	if err != nil {
		log.Error(err, "Unable to get HTTPRoute")
		return err
	}

	if !lc.validateHTTPRouteGatewayClass(ctx, httpRoute) {
		log.V(4).Info("HTTPRoute is not controlled by this controller")
		return nil
	}

	t := lc.timeNow()

	newHTTPRoute := httpRoute.DeepCopy()

	for i := range httpRoute.Spec.ParentRefs {
		paRef := &httpRoute.Spec.ParentRefs[i]

		if !parentGateway(paRef, httpRoute.Namespace) {
			continue
		}

		gtw, err := lc.gatewayLister.Gateways(httpRoute.Namespace).Get(string(paRef.Name))
		if err != nil {
			continue
		}

		if !lc.validateGatewayGatewayClass(ctx, gtw) {
			continue
		}

		if cond := findCondition(gtw.Status.Conditions, string(gatewayv1.GatewayConditionAccepted)); cond == nil || cond.Status != metav1.ConditionTrue {
			lc.updateHTTPRouteParentRefStatus(newHTTPRoute, paRef, string(gatewayv1.RouteReasonPending), metav1.ConditionUnknown, t)
			continue
		}

		sectionName := ptr.Deref(paRef.SectionName, gatewayv1.SectionName(""))
		if sectionName == "" {
			if len(httpRoute.Spec.Hostnames) > 0 {
				if !slices.ContainsFunc(gtw.Spec.Listeners, func(l gatewayv1.Listener) bool {
					if l.Hostname == nil {
						return true
					}

					return slices.ContainsFunc(httpRoute.Spec.Hostnames, func(h gatewayv1.Hostname) bool {
						return hostnameMatch(*l.Hostname, h)
					})
				}) {
					lc.updateHTTPRouteParentRefStatus(newHTTPRoute, paRef, string(gatewayv1.RouteReasonNoMatchingListenerHostname), metav1.ConditionFalse, t)
					continue
				}
			}

			lc.updateHTTPRouteParentRefStatus(newHTTPRoute, paRef, string(gatewayv1.RouteReasonAccepted), metav1.ConditionTrue, t)

			continue
		}

		lidx := slices.IndexFunc(gtw.Spec.Listeners, func(l gatewayv1.Listener) bool {
			return l.Name == sectionName
		})
		if lidx == -1 {
			lc.updateHTTPRouteParentRefStatus(newHTTPRoute, paRef, string(gatewayv1.RouteReasonNoMatchingParent), metav1.ConditionFalse, t)
			continue
		}

		l := &gtw.Spec.Listeners[lidx]

		if l.Hostname != nil && len(httpRoute.Spec.Hostnames) > 0 {
			if !slices.ContainsFunc(httpRoute.Spec.Hostnames, func(h gatewayv1.Hostname) bool {
				return hostnameMatch(*l.Hostname, h)
			}) {
				lc.updateHTTPRouteParentRefStatus(newHTTPRoute, paRef, string(gatewayv1.RouteReasonNoMatchingListenerHostname), metav1.ConditionFalse, t)
				continue
			}
		}

		lc.updateHTTPRouteParentRefStatus(newHTTPRoute, paRef, string(gatewayv1.RouteReasonAccepted), metav1.ConditionTrue, t)
	}

	newHTTPRoute.Status.Parents = slices.DeleteFunc(newHTTPRoute.Status.Parents, func(ps gatewayv1.RouteParentStatus) bool {
		return !slices.ContainsFunc(newHTTPRoute.Spec.ParentRefs, func(paRef gatewayv1.ParentReference) bool {
			return parentRefEqual(&ps.ParentRef, &paRef, newHTTPRoute.Namespace)
		})
	})

	if equality.Semantic.DeepEqual(httpRoute.Status, newHTTPRoute.Status) {
		return nil
	}

	if _, err := lc.lbc.gatewayClientset.GatewayV1().HTTPRoutes(newHTTPRoute.Namespace).UpdateStatus(ctx, newHTTPRoute, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Unable to update HTTPRoute status")
		return err
	}

	return nil
}

func (lc *LeaderController) updateHTTPRouteParentRefStatus(httpRoute *gatewayv1.HTTPRoute, paRef *gatewayv1.ParentReference,
	reason string, status metav1.ConditionStatus, t metav1.Time) {
	paStatus := findHTTPRouteParentStatus(httpRoute, paRef)
	if paStatus == nil {
		httpRoute.Status.Parents = append(httpRoute.Status.Parents, gatewayv1.RouteParentStatus{
			ParentRef: *paRef,
		})
		paStatus = &httpRoute.Status.Parents[len(httpRoute.Status.Parents)-1]
	}

	paStatus.ControllerName = lc.lbc.gatewayClassController

	cond := findCondition(paStatus.Conditions, string(gatewayv1.RouteConditionAccepted))
	if cond == nil {
		paStatus.Conditions, cond = appendCondition(paStatus.Conditions,
			metav1.Condition{Type: string(gatewayv1.RouteConditionAccepted)})
	}

	cond.Reason = reason
	cond.Message = ""
	cond.ObservedGeneration = httpRoute.Generation

	if cond.Status != status {
		cond.Status = status
		cond.LastTransitionTime = t
	}
}
