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

package v2alpha1

import (
	"context"

	v2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha1"
	scheme "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ActiveMQArtemisAddressesGetter has a method to return a ActiveMQArtemisAddressInterface.
// A group's client should implement this interface.
type ActiveMQArtemisAddressesGetter interface {
	ActiveMQArtemisAddresses(namespace string) ActiveMQArtemisAddressInterface
}

// ActiveMQArtemisAddressInterface has methods to work with ActiveMQArtemisAddress resources.
type ActiveMQArtemisAddressInterface interface {
	Create(*v2alpha1.ActiveMQArtemisAddress) (*v2alpha1.ActiveMQArtemisAddress, error)
	Update(*v2alpha1.ActiveMQArtemisAddress) (*v2alpha1.ActiveMQArtemisAddress, error)
	UpdateStatus(*v2alpha1.ActiveMQArtemisAddress) (*v2alpha1.ActiveMQArtemisAddress, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v2alpha1.ActiveMQArtemisAddress, error)
	List(opts v1.ListOptions) (*v2alpha1.ActiveMQArtemisAddressList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v2alpha1.ActiveMQArtemisAddress, err error)
	ActiveMQArtemisAddressExpansion
}

// activeMQArtemisAddresses implements ActiveMQArtemisAddressInterface
type activeMQArtemisAddresses struct {
	client rest.Interface
	ns     string
}

// newActiveMQArtemisAddresses returns a ActiveMQArtemisAddresses
func newActiveMQArtemisAddresses(c *BrokerV2alpha1Client, namespace string) *activeMQArtemisAddresses {
	return &activeMQArtemisAddresses{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the activeMQArtemisAddress, and returns the corresponding activeMQArtemisAddress object, and an error if there is any.
func (c *activeMQArtemisAddresses) Get(name string, options v1.GetOptions) (result *v2alpha1.ActiveMQArtemisAddress, err error) {
	result = &v2alpha1.ActiveMQArtemisAddress{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ActiveMQArtemisAddresses that match those selectors.
func (c *activeMQArtemisAddresses) List(opts v1.ListOptions) (result *v2alpha1.ActiveMQArtemisAddressList, err error) {
	result = &v2alpha1.ActiveMQArtemisAddressList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested activeMQArtemisAddresses.
func (c *activeMQArtemisAddresses) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(context.TODO())
}

// Create takes the representation of a activeMQArtemisAddress and creates it.  Returns the server's representation of the activeMQArtemisAddress, and an error, if there is any.
func (c *activeMQArtemisAddresses) Create(activeMQArtemisAddress *v2alpha1.ActiveMQArtemisAddress) (result *v2alpha1.ActiveMQArtemisAddress, err error) {
	result = &v2alpha1.ActiveMQArtemisAddress{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		Body(activeMQArtemisAddress).
		Do(context.TODO()).
		Into(result)
	return
}

// Update takes the representation of a activeMQArtemisAddress and updates it. Returns the server's representation of the activeMQArtemisAddress, and an error, if there is any.
func (c *activeMQArtemisAddresses) Update(activeMQArtemisAddress *v2alpha1.ActiveMQArtemisAddress) (result *v2alpha1.ActiveMQArtemisAddress, err error) {
	result = &v2alpha1.ActiveMQArtemisAddress{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		Name(activeMQArtemisAddress.Name).
		Body(activeMQArtemisAddress).
		Do(context.TODO()).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *activeMQArtemisAddresses) UpdateStatus(activeMQArtemisAddress *v2alpha1.ActiveMQArtemisAddress) (result *v2alpha1.ActiveMQArtemisAddress, err error) {
	result = &v2alpha1.ActiveMQArtemisAddress{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		Name(activeMQArtemisAddress.Name).
		SubResource("status").
		Body(activeMQArtemisAddress).
		Do(context.TODO()).
		Into(result)
	return
}

// Delete takes name of the activeMQArtemisAddress and deletes it. Returns an error if one occurs.
func (c *activeMQArtemisAddresses) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		Name(name).
		Body(options).
		Do(context.TODO()).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *activeMQArtemisAddresses) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do(context.TODO()).
		Error()
}

// Patch applies the patch and returns the patched activeMQArtemisAddress.
func (c *activeMQArtemisAddresses) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v2alpha1.ActiveMQArtemisAddress, err error) {
	result = &v2alpha1.ActiveMQArtemisAddress{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("activemqartemisaddresses").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do(context.TODO()).
		Into(result)
	return
}
