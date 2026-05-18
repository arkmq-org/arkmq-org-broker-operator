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
	v2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeActiveMQArtemisAddresses implements ActiveMQArtemisAddressInterface
type FakeActiveMQArtemisAddresses struct {
	Fake *FakeBrokerV2alpha3
	ns   string
}

var activemqartemisaddressesResource = schema.GroupVersionResource{Group: "broker.amq.io", Version: "v2alpha3", Resource: "activemqartemisaddresses"}

var activemqartemisaddressesKind = schema.GroupVersionKind{Group: "broker.amq.io", Version: "v2alpha3", Kind: "ActiveMQArtemisAddress"}

// Get takes name of the activeMQArtemisAddress, and returns the corresponding activeMQArtemisAddress object, and an error if there is any.
func (c *FakeActiveMQArtemisAddresses) Get(name string, options v1.GetOptions) (result *v2alpha3.ActiveMQArtemisAddress, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(activemqartemisaddressesResource, c.ns, name), &v2alpha3.ActiveMQArtemisAddress{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha3.ActiveMQArtemisAddress), err
}

// List takes label and field selectors, and returns the list of ActiveMQArtemisAddresses that match those selectors.
func (c *FakeActiveMQArtemisAddresses) List(opts v1.ListOptions) (result *v2alpha3.ActiveMQArtemisAddressList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(activemqartemisaddressesResource, activemqartemisaddressesKind, c.ns, opts), &v2alpha3.ActiveMQArtemisAddressList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v2alpha3.ActiveMQArtemisAddressList{}
	for _, item := range obj.(*v2alpha3.ActiveMQArtemisAddressList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested activeMQArtemisAddresses.
func (c *FakeActiveMQArtemisAddresses) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(activemqartemisaddressesResource, c.ns, opts))

}

// Create takes the representation of a activeMQArtemisAddress and creates it.  Returns the server's representation of the activeMQArtemisAddress, and an error, if there is any.
func (c *FakeActiveMQArtemisAddresses) Create(activeMQArtemisAddress *v2alpha3.ActiveMQArtemisAddress) (result *v2alpha3.ActiveMQArtemisAddress, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(activemqartemisaddressesResource, c.ns, activeMQArtemisAddress), &v2alpha3.ActiveMQArtemisAddress{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha3.ActiveMQArtemisAddress), err
}

// Update takes the representation of a activeMQArtemisAddress and updates it. Returns the server's representation of the activeMQArtemisAddress, and an error, if there is any.
func (c *FakeActiveMQArtemisAddresses) Update(activeMQArtemisAddress *v2alpha3.ActiveMQArtemisAddress) (result *v2alpha3.ActiveMQArtemisAddress, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(activemqartemisaddressesResource, c.ns, activeMQArtemisAddress), &v2alpha3.ActiveMQArtemisAddress{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha3.ActiveMQArtemisAddress), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeActiveMQArtemisAddresses) UpdateStatus(activeMQArtemisAddress *v2alpha3.ActiveMQArtemisAddress) (*v2alpha3.ActiveMQArtemisAddress, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(activemqartemisaddressesResource, "status", c.ns, activeMQArtemisAddress), &v2alpha3.ActiveMQArtemisAddress{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha3.ActiveMQArtemisAddress), err
}

// Delete takes name of the activeMQArtemisAddress and deletes it. Returns an error if one occurs.
func (c *FakeActiveMQArtemisAddresses) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(activemqartemisaddressesResource, c.ns, name), &v2alpha3.ActiveMQArtemisAddress{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeActiveMQArtemisAddresses) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(activemqartemisaddressesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v2alpha3.ActiveMQArtemisAddressList{})
	return err
}

// Patch applies the patch and returns the patched activeMQArtemisAddress.
func (c *FakeActiveMQArtemisAddresses) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v2alpha3.ActiveMQArtemisAddress, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(activemqartemisaddressesResource, c.ns, name, pt, data, subresources...), &v2alpha3.ActiveMQArtemisAddress{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v2alpha3.ActiveMQArtemisAddress), err
}
