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
	v1alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeActiveMQArtemisSecurities implements ActiveMQArtemisSecurityInterface
type FakeActiveMQArtemisSecurities struct {
	Fake *FakeBrokerV1alpha1
	ns   string
}

var activemqartemissecuritiesResource = schema.GroupVersionResource{Group: "broker.amq.io", Version: "v1alpha1", Resource: "activemqartemissecurities"}

var activemqartemissecuritiesKind = schema.GroupVersionKind{Group: "broker.amq.io", Version: "v1alpha1", Kind: "ActiveMQArtemisSecurity"}

// Get takes name of the activeMQArtemisSecurity, and returns the corresponding activeMQArtemisSecurity object, and an error if there is any.
func (c *FakeActiveMQArtemisSecurities) Get(name string, options v1.GetOptions) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(activemqartemissecuritiesResource, c.ns, name), &v1alpha1.ActiveMQArtemisSecurity{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ActiveMQArtemisSecurity), err
}

// List takes label and field selectors, and returns the list of ActiveMQArtemisSecurities that match those selectors.
func (c *FakeActiveMQArtemisSecurities) List(opts v1.ListOptions) (result *v1alpha1.ActiveMQArtemisSecurityList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(activemqartemissecuritiesResource, activemqartemissecuritiesKind, c.ns, opts), &v1alpha1.ActiveMQArtemisSecurityList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ActiveMQArtemisSecurityList{}
	for _, item := range obj.(*v1alpha1.ActiveMQArtemisSecurityList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested activeMQArtemisSecurities.
func (c *FakeActiveMQArtemisSecurities) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(activemqartemissecuritiesResource, c.ns, opts))

}

// Create takes the representation of a activeMQArtemisSecurity and creates it.  Returns the server's representation of the activeMQArtemisSecurity, and an error, if there is any.
func (c *FakeActiveMQArtemisSecurities) Create(activeMQArtemisSecurity *v1alpha1.ActiveMQArtemisSecurity) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(activemqartemissecuritiesResource, c.ns, activeMQArtemisSecurity), &v1alpha1.ActiveMQArtemisSecurity{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ActiveMQArtemisSecurity), err
}

// Update takes the representation of a activeMQArtemisSecurity and updates it. Returns the server's representation of the activeMQArtemisSecurity, and an error, if there is any.
func (c *FakeActiveMQArtemisSecurities) Update(activeMQArtemisSecurity *v1alpha1.ActiveMQArtemisSecurity) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(activemqartemissecuritiesResource, c.ns, activeMQArtemisSecurity), &v1alpha1.ActiveMQArtemisSecurity{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ActiveMQArtemisSecurity), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeActiveMQArtemisSecurities) UpdateStatus(activeMQArtemisSecurity *v1alpha1.ActiveMQArtemisSecurity) (*v1alpha1.ActiveMQArtemisSecurity, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(activemqartemissecuritiesResource, "status", c.ns, activeMQArtemisSecurity), &v1alpha1.ActiveMQArtemisSecurity{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ActiveMQArtemisSecurity), err
}

// Delete takes name of the activeMQArtemisSecurity and deletes it. Returns an error if one occurs.
func (c *FakeActiveMQArtemisSecurities) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(activemqartemissecuritiesResource, c.ns, name), &v1alpha1.ActiveMQArtemisSecurity{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeActiveMQArtemisSecurities) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(activemqartemissecuritiesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.ActiveMQArtemisSecurityList{})
	return err
}

// Patch applies the patch and returns the patched activeMQArtemisSecurity.
func (c *FakeActiveMQArtemisSecurities) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(activemqartemissecuritiesResource, c.ns, name, pt, data, subresources...), &v1alpha1.ActiveMQArtemisSecurity{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ActiveMQArtemisSecurity), err
}
