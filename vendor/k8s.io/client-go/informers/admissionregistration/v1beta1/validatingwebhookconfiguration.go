/*
Copyright 2017 The Kubernetes Authors.

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

// This file was automatically generated by informer-gen

package v1beta1

import (
	time "time"

	admissionregistration_v1beta1 "k8s.io/api/admissionregistration/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	internalinterfaces "k8s.io/client-go/informers/internalinterfaces"
	kubernetes "k8s.io/client-go/kubernetes"
	v1beta1 "k8s.io/client-go/listers/admissionregistration/v1beta1"
	cache "k8s.io/client-go/tools/cache"
)

// ValidatingWebhookConfigurationInformer provides access to a shared informer and lister for
// ValidatingWebhookConfigurations.
type ValidatingWebhookConfigurationInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1beta1.ValidatingWebhookConfigurationLister
}

type validatingWebhookConfigurationInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewValidatingWebhookConfigurationInformer constructs a new informer for ValidatingWebhookConfiguration type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewValidatingWebhookConfigurationInformer(client kubernetes.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredValidatingWebhookConfigurationInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredValidatingWebhookConfigurationInformer constructs a new informer for ValidatingWebhookConfiguration type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredValidatingWebhookConfigurationInformer(client kubernetes.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Watch(options)
			},
		},
		&admissionregistration_v1beta1.ValidatingWebhookConfiguration{},
		resyncPeriod,
		indexers,
	)
}

func (f *validatingWebhookConfigurationInformer) defaultInformer(client kubernetes.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredValidatingWebhookConfigurationInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *validatingWebhookConfigurationInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&admissionregistration_v1beta1.ValidatingWebhookConfiguration{}, f.defaultInformer)
}

func (f *validatingWebhookConfigurationInformer) Lister() v1beta1.ValidatingWebhookConfigurationLister {
	return v1beta1.NewValidatingWebhookConfigurationLister(f.Informer().GetIndexer())
}
