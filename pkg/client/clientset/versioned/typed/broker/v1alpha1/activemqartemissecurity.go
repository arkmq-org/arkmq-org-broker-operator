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

package v1alpha1

import (
	"context"

	v1alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1alpha1"
	scheme "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ActiveMQArtemisSecuritiesGetter has a method to return a ActiveMQArtemisSecurityInterface.
// A group's client should implement this interface.
type ActiveMQArtemisSecuritiesGetter interface {
	ActiveMQArtemisSecurities(namespace string) ActiveMQArtemisSecurityInterface
}

// ActiveMQArtemisSecurityInterface has methods to work with ActiveMQArtemisSecurity resources.
type ActiveMQArtemisSecurityInterface interface {
	Create(*v1alpha1.ActiveMQArtemisSecurity) (*v1alpha1.ActiveMQArtemisSecurity, error)
	Update(*v1alpha1.ActiveMQArtemisSecurity) (*v1alpha1.ActiveMQArtemisSecurity, error)
	UpdateStatus(*v1alpha1.ActiveMQArtemisSecurity) (*v1alpha1.ActiveMQArtemisSecurity, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.ActiveMQArtemisSecurity, error)
	List(opts v1.ListOptions) (*v1alpha1.ActiveMQArtemisSecurityList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ActiveMQArtemisSecurity, err error)
	ActiveMQArtemisSecurityExpansion
}

// activeMQArtemisSecurities implements ActiveMQArtemisSecurityInterface
type activeMQArtemisSecurities struct {
	client rest.Interface
	ns     string
}

// newActiveMQArtemisSecurities returns a ActiveMQArtemisSecurities
func newActiveMQArtemisSecurities(c *BrokerV1alpha1Client, namespace string) *activeMQArtemisSecurities {
	return &activeMQArtemisSecurities{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the activeMQArtemisSecurity, and returns the corresponding activeMQArtemisSecurity object, and an error if there is any.
func (c *activeMQArtemisSecurities) Get(name string, options v1.GetOptions) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	result = &v1alpha1.ActiveMQArtemisSecurity{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ActiveMQArtemisAddresses that match those selectors.
func (c *activeMQArtemisSecurities) List(opts v1.ListOptions) (result *v1alpha1.ActiveMQArtemisSecurityList, err error) {
	result = &v1alpha1.ActiveMQArtemisSecurityList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested activeMQArtemisSecurities.
func (c *activeMQArtemisSecurities) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(context.TODO())
}

// Create takes the representation of a activeMQArtemisSecurity and creates it.  Returns the server's representation of the activeMQArtemisSecurity, and an error, if there is any.
func (c *activeMQArtemisSecurities) Create(activeMQArtemisSecurity *v1alpha1.ActiveMQArtemisSecurity) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	result = &v1alpha1.ActiveMQArtemisSecurity{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		Body(activeMQArtemisSecurity).
		Do(context.TODO()).
		Into(result)
	return
}

// Update takes the representation of a activeMQArtemisSecurity and updates it. Returns the server's representation of the activeMQArtemisSecurity, and an error, if there is any.
func (c *activeMQArtemisSecurities) Update(activeMQArtemisSecurity *v1alpha1.ActiveMQArtemisSecurity) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	result = &v1alpha1.ActiveMQArtemisSecurity{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		Name(activeMQArtemisSecurity.Name).
		Body(activeMQArtemisSecurity).
		Do(context.TODO()).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *activeMQArtemisSecurities) UpdateStatus(activeMQArtemisSecurity *v1alpha1.ActiveMQArtemisSecurity) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	result = &v1alpha1.ActiveMQArtemisSecurity{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		Name(activeMQArtemisSecurity.Name).
		SubResource("status").
		Body(activeMQArtemisSecurity).
		Do(context.TODO()).
		Into(result)
	return
}

// Delete takes name of the activeMQArtemisSecurity and deletes it. Returns an error if one occurs.
func (c *activeMQArtemisSecurities) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		Name(name).
		Body(options).
		Do(context.TODO()).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *activeMQArtemisSecurities) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do(context.TODO()).
		Error()
}

// Patch applies the patch and returns the patched activeMQArtemisSecurity.
func (c *activeMQArtemisSecurities) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.ActiveMQArtemisSecurity, err error) {
	result = &v1alpha1.ActiveMQArtemisSecurity{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("activemqartemissecurities").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do(context.TODO()).
		Into(result)
	return
}
