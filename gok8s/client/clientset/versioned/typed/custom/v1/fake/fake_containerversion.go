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

package fake

import (
	custom_v1 "github.com/nearmap/cvmanager/gok8s/apis/custom/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeContainerVersions implements ContainerVersionInterface
type FakeContainerVersions struct {
	Fake *FakeCustomV1
	ns   string
}

var containerversionsResource = schema.GroupVersionResource{Group: "custom.k8s.io", Version: "v1", Resource: "containerversions"}

var containerversionsKind = schema.GroupVersionKind{Group: "custom.k8s.io", Version: "v1", Kind: "ContainerVersion"}

// Get takes name of the containerVersion, and returns the corresponding containerVersion object, and an error if there is any.
func (c *FakeContainerVersions) Get(name string, options v1.GetOptions) (result *custom_v1.ContainerVersion, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(containerversionsResource, c.ns, name), &custom_v1.ContainerVersion{})

	if obj == nil {
		return nil, err
	}
	return obj.(*custom_v1.ContainerVersion), err
}

// List takes label and field selectors, and returns the list of ContainerVersions that match those selectors.
func (c *FakeContainerVersions) List(opts v1.ListOptions) (result *custom_v1.ContainerVersionList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(containerversionsResource, containerversionsKind, c.ns, opts), &custom_v1.ContainerVersionList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &custom_v1.ContainerVersionList{}
	for _, item := range obj.(*custom_v1.ContainerVersionList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested containerVersions.
func (c *FakeContainerVersions) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(containerversionsResource, c.ns, opts))

}

// Create takes the representation of a containerVersion and creates it.  Returns the server's representation of the containerVersion, and an error, if there is any.
func (c *FakeContainerVersions) Create(containerVersion *custom_v1.ContainerVersion) (result *custom_v1.ContainerVersion, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(containerversionsResource, c.ns, containerVersion), &custom_v1.ContainerVersion{})

	if obj == nil {
		return nil, err
	}
	return obj.(*custom_v1.ContainerVersion), err
}

// Update takes the representation of a containerVersion and updates it. Returns the server's representation of the containerVersion, and an error, if there is any.
func (c *FakeContainerVersions) Update(containerVersion *custom_v1.ContainerVersion) (result *custom_v1.ContainerVersion, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(containerversionsResource, c.ns, containerVersion), &custom_v1.ContainerVersion{})

	if obj == nil {
		return nil, err
	}
	return obj.(*custom_v1.ContainerVersion), err
}

// Delete takes name of the containerVersion and deletes it. Returns an error if one occurs.
func (c *FakeContainerVersions) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(containerversionsResource, c.ns, name), &custom_v1.ContainerVersion{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeContainerVersions) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(containerversionsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &custom_v1.ContainerVersionList{})
	return err
}

// Patch applies the patch and returns the patched containerVersion.
func (c *FakeContainerVersions) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *custom_v1.ContainerVersion, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(containerversionsResource, c.ns, name, data, subresources...), &custom_v1.ContainerVersion{})

	if obj == nil {
		return nil, err
	}
	return obj.(*custom_v1.ContainerVersion), err
}
