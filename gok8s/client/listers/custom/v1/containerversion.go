/*
Copyright 2018 The Kubernetes Authors.

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

// This file was automatically generated by lister-gen

package v1

import (
	v1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// ContainerVersionLister helps list ContainerVersions.
type ContainerVersionLister interface {
	// List lists all ContainerVersions in the indexer.
	List(selector labels.Selector) (ret []*v1.ContainerVersion, err error)
	// ContainerVersions returns an object that can list and get ContainerVersions.
	ContainerVersions(namespace string) ContainerVersionNamespaceLister
	ContainerVersionListerExpansion
}

// containerVersionLister implements the ContainerVersionLister interface.
type containerVersionLister struct {
	indexer cache.Indexer
}

// NewContainerVersionLister returns a new ContainerVersionLister.
func NewContainerVersionLister(indexer cache.Indexer) ContainerVersionLister {
	return &containerVersionLister{indexer: indexer}
}

// List lists all ContainerVersions in the indexer.
func (s *containerVersionLister) List(selector labels.Selector) (ret []*v1.ContainerVersion, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ContainerVersion))
	})
	return ret, err
}

// ContainerVersions returns an object that can list and get ContainerVersions.
func (s *containerVersionLister) ContainerVersions(namespace string) ContainerVersionNamespaceLister {
	return containerVersionNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// ContainerVersionNamespaceLister helps list and get ContainerVersions.
type ContainerVersionNamespaceLister interface {
	// List lists all ContainerVersions in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1.ContainerVersion, err error)
	// Get retrieves the ContainerVersion from the indexer for a given namespace and name.
	Get(name string) (*v1.ContainerVersion, error)
	ContainerVersionNamespaceListerExpansion
}

// containerVersionNamespaceLister implements the ContainerVersionNamespaceLister
// interface.
type containerVersionNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all ContainerVersions in the indexer for a given namespace.
func (s containerVersionNamespaceLister) List(selector labels.Selector) (ret []*v1.ContainerVersion, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1.ContainerVersion))
	})
	return ret, err
}

// Get retrieves the ContainerVersion from the indexer for a given namespace and name.
func (s containerVersionNamespaceLister) Get(name string) (*v1.ContainerVersion, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1.Resource("containerversion"), name)
	}
	return obj.(*v1.ContainerVersion), nil
}
