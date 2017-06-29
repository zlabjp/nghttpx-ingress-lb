/**
 * Copyright 2016, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */
package controller

import (
	corelisters "k8s.io/client-go/listers/core/v1"
	extensionslisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
)

// ingressLister makes a Store that lists Ingresses.
type ingressLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	extensionslisters.IngressLister
}

// newIngressLister creates new ingressLister.
func newIngressLister(indexer cache.Indexer) *ingressLister {
	return &ingressLister{
		indexer:       indexer,
		IngressLister: extensionslisters.NewIngressLister(indexer),
	}
}

// secrLister makes a Store that lists Secrets.
type secretLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	corelisters.SecretLister
}

// newSecretLister creates new secretLister.
func newSecretLister(indexer cache.Indexer) *secretLister {
	return &secretLister{
		indexer:      indexer,
		SecretLister: corelisters.NewSecretLister(indexer),
	}
}

// configMapLister makes a Store that lists ConfigMaps.
type configMapLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	corelisters.ConfigMapLister
}

// newConfigMapLister creates new configMapLister.
func newConfigMapLister(indexer cache.Indexer) *configMapLister {
	return &configMapLister{
		indexer:         indexer,
		ConfigMapLister: corelisters.NewConfigMapLister(indexer),
	}
}

// serviceLister makes a Store that lists Services.
type serviceLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	corelisters.ServiceLister
}

// newServiceLister creates new serviceLister.
func newServiceLister(indexer cache.Indexer) *serviceLister {
	return &serviceLister{
		indexer:       indexer,
		ServiceLister: corelisters.NewServiceLister(indexer),
	}
}

// endpointsLister makes a Store that lists Endpoints.
type endpointsLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	corelisters.EndpointsLister
}

// newEndpointsLister creates new endpointsLister.
func newEndpointsLister(indexer cache.Indexer) *endpointsLister {
	return &endpointsLister{
		indexer:         indexer,
		EndpointsLister: corelisters.NewEndpointsLister(indexer),
	}
}

// podLister makes a Store that lists Pods.
type podLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	corelisters.PodLister
}

// newPodLister creates new podLister.
func newPodLister(indexer cache.Indexer) *podLister {
	return &podLister{
		indexer:   indexer,
		PodLister: corelisters.NewPodLister(indexer),
	}
}

// nodeLister makes a Store that lists Nodes.
type nodeLister struct {
	// indexer is added here so that object can be added to indexer in test.
	indexer cache.Indexer
	corelisters.NodeLister
}

// newNodeLister creates new nodeLister.
func newNodeLister(indexer cache.Indexer) *nodeLister {
	return &nodeLister{
		indexer:    indexer,
		NodeLister: corelisters.NewNodeLister(indexer),
	}
}
