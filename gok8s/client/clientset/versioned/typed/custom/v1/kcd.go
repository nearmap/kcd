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

// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/nearmap/kcd/gok8s/apis/custom/v1"
	scheme "github.com/nearmap/kcd/gok8s/client/clientset/versioned/scheme"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// KCDsGetter has a method to return a KCDInterface.
// A group's client should implement this interface.
type KCDsGetter interface {
	KCDs(namespace string) KCDInterface
}

// KCDInterface has methods to work with KCD resources.
type KCDInterface interface {
	Create(*v1.KCD) (*v1.KCD, error)
	Update(*v1.KCD) (*v1.KCD, error)
	Delete(name string, options *meta_v1.DeleteOptions) error
	DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error
	Get(name string, options meta_v1.GetOptions) (*v1.KCD, error)
	List(opts meta_v1.ListOptions) (*v1.KCDList, error)
	Watch(opts meta_v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.KCD, err error)
	KCDExpansion
}

// kCDs implements KCDInterface
type kCDs struct {
	client rest.Interface
	ns     string
}

// newKCDs returns a KCDs
func newKCDs(c *CustomV1Client, namespace string) *kCDs {
	return &kCDs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the kCD, and returns the corresponding kCD object, and an error if there is any.
func (c *kCDs) Get(name string, options meta_v1.GetOptions) (result *v1.KCD, err error) {
	result = &v1.KCD{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("kcds").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of KCDs that match those selectors.
func (c *kCDs) List(opts meta_v1.ListOptions) (result *v1.KCDList, err error) {
	result = &v1.KCDList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("kcds").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested kCDs.
func (c *kCDs) Watch(opts meta_v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("kcds").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a kCD and creates it.  Returns the server's representation of the kCD, and an error, if there is any.
func (c *kCDs) Create(kCD *v1.KCD) (result *v1.KCD, err error) {
	result = &v1.KCD{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("kcds").
		Body(kCD).
		Do().
		Into(result)
	return
}

// Update takes the representation of a kCD and updates it. Returns the server's representation of the kCD, and an error, if there is any.
func (c *kCDs) Update(kCD *v1.KCD) (result *v1.KCD, err error) {
	result = &v1.KCD{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("kcds").
		Name(kCD.Name).
		Body(kCD).
		Do().
		Into(result)
	return
}

// Delete takes name of the kCD and deletes it. Returns an error if one occurs.
func (c *kCDs) Delete(name string, options *meta_v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("kcds").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *kCDs) DeleteCollection(options *meta_v1.DeleteOptions, listOptions meta_v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("kcds").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched kCD.
func (c *kCDs) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.KCD, err error) {
	result = &v1.KCD{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("kcds").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}