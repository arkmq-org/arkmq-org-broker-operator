/*
Copyright 2020 The Kubernetes Authors.

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
	v2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeActiveMQArtemises implements ActiveMQArtemisInterface
type FakeActiveMQArtemises struct {
	Fake *FakeBrokerV2alpha5
	ns   string
}

var activemqartemisesResource = schema.GroupVersionResource{Group: "broker", Version: "v2alpha5", Resource: "activemqartemises"}

var activemqartemisesKind = schema.GroupVersionKind{Group: "broker", Version: "v2alpha5", Kind: "ActiveMQArtemis"}

// Get takes name of the activeMQArtemis, and returns the corresponding activeMQArtemis object, and an error if there is any.
func (c *FakeActiveMQArtemises) Get(name string, options v1.GetOptions) (result *v2alpha5.ActiveMQArtemis, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(activemqartemisesResource, c.ns, name), &v2alpha5.ActiveMQArtemis{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha5.ActiveMQArtemis), err
}

// List takes label and field selectors, and returns the list of ActiveMQArtemises that match those selectors.
func (c *FakeActiveMQArtemises) List(opts v1.ListOptions) (result *v2alpha5.ActiveMQArtemisList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(activemqartemisesResource, activemqartemisesKind, c.ns, opts), &v2alpha5.ActiveMQArtemisList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v2alpha5.ActiveMQArtemisList{}
	for _, item := range obj.(*v2alpha5.ActiveMQArtemisList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested activeMQArtemises.
func (c *FakeActiveMQArtemises) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(activemqartemisesResource, c.ns, opts))

}

// Create takes the representation of a activeMQArtemis and creates it.  Returns the server's representation of the activeMQArtemis, and an error, if there is any.
func (c *FakeActiveMQArtemises) Create(activeMQArtemis *v2alpha5.ActiveMQArtemis) (result *v2alpha5.ActiveMQArtemis, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(activemqartemisesResource, c.ns, activeMQArtemis), &v2alpha5.ActiveMQArtemis{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha5.ActiveMQArtemis), err
}

// Update takes the representation of a activeMQArtemis and updates it. Returns the server's representation of the activeMQArtemis, and an error, if there is any.
func (c *FakeActiveMQArtemises) Update(activeMQArtemis *v2alpha5.ActiveMQArtemis) (result *v2alpha5.ActiveMQArtemis, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(activemqartemisesResource, c.ns, activeMQArtemis), &v2alpha5.ActiveMQArtemis{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha5.ActiveMQArtemis), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeActiveMQArtemises) UpdateStatus(activeMQArtemis *v2alpha5.ActiveMQArtemis) (*v2alpha5.ActiveMQArtemis, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(activemqartemisesResource, "status", c.ns, activeMQArtemis), &v2alpha5.ActiveMQArtemis{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha5.ActiveMQArtemis), err
}

// Delete takes name of the activeMQArtemis and deletes it. Returns an error if one occurs.
func (c *FakeActiveMQArtemises) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(activemqartemisesResource, c.ns, name), &v2alpha5.ActiveMQArtemis{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeActiveMQArtemises) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(activemqartemisesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v2alpha5.ActiveMQArtemisList{})
	return err
}

// Patch applies the patch and returns the patched activeMQArtemis.
func (c *FakeActiveMQArtemises) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v2alpha5.ActiveMQArtemis, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(activemqartemisesResource, c.ns, name, pt, data, subresources...), &v2alpha5.ActiveMQArtemis{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha5.ActiveMQArtemis), err
}
