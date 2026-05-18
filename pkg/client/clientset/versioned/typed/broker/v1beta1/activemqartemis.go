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

package v1beta1

import (
	"context"

	v1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	scheme "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ActiveMQArtemisesGetter has a method to return a ActiveMQArtemisInterface.
// A group's client should implement this interface.
type ActiveMQArtemisesGetter interface {
	ActiveMQArtemises(namespace string) ActiveMQArtemisInterface
}

// ActiveMQArtemisInterface has methods to work with ActiveMQArtemis resources.
type ActiveMQArtemisInterface interface {
	Create(*v1beta1.ActiveMQArtemis) (*v1beta1.ActiveMQArtemis, error)
	Update(*v1beta1.ActiveMQArtemis) (*v1beta1.ActiveMQArtemis, error)
	UpdateStatus(*v1beta1.ActiveMQArtemis) (*v1beta1.ActiveMQArtemis, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1beta1.ActiveMQArtemis, error)
	List(opts v1.ListOptions) (*v1beta1.ActiveMQArtemisList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta1.ActiveMQArtemis, err error)
	ActiveMQArtemisExpansion
}

// activeMQArtemises implements ActiveMQArtemisInterface
type activeMQArtemises struct {
	client rest.Interface
	ns     string
}

// newActiveMQArtemises returns a ActiveMQArtemises
func newActiveMQArtemises(c *BrokerV1beta1Client, namespace string) *activeMQArtemises {
	return &activeMQArtemises{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the activeMQArtemis, and returns the corresponding activeMQArtemis object, and an error if there is any.
func (c *activeMQArtemises) Get(name string, options v1.GetOptions) (result *v1beta1.ActiveMQArtemis, err error) {
	result = &v1beta1.ActiveMQArtemis{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemises").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ActiveMQArtemises that match those selectors.
func (c *activeMQArtemises) List(opts v1.ListOptions) (result *v1beta1.ActiveMQArtemisList, err error) {
	result = &v1beta1.ActiveMQArtemisList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemises").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(context.TODO()).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested activeMQArtemises.
func (c *activeMQArtemises) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("activemqartemises").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(context.TODO())
}

// Create takes the representation of a activeMQArtemis and creates it.  Returns the server's representation of the activeMQArtemis, and an error, if there is any.
func (c *activeMQArtemises) Create(activeMQArtemis *v1beta1.ActiveMQArtemis) (result *v1beta1.ActiveMQArtemis, err error) {
	result = &v1beta1.ActiveMQArtemis{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("activemqartemises").
		Body(activeMQArtemis).
		Do(context.TODO()).
		Into(result)
	return
}

// Update takes the representation of a activeMQArtemis and updates it. Returns the server's representation of the activeMQArtemis, and an error, if there is any.
func (c *activeMQArtemises) Update(activeMQArtemis *v1beta1.ActiveMQArtemis) (result *v1beta1.ActiveMQArtemis, err error) {
	result = &v1beta1.ActiveMQArtemis{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("activemqartemises").
		Name(activeMQArtemis.Name).
		Body(activeMQArtemis).
		Do(context.TODO()).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *activeMQArtemises) UpdateStatus(activeMQArtemis *v1beta1.ActiveMQArtemis) (result *v1beta1.ActiveMQArtemis, err error) {
	result = &v1beta1.ActiveMQArtemis{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("activemqartemises").
		Name(activeMQArtemis.Name).
		SubResource("status").
		Body(activeMQArtemis).
		Do(context.TODO()).
		Into(result)
	return
}

// Delete takes name of the activeMQArtemis and deletes it. Returns an error if one occurs.
func (c *activeMQArtemises) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("activemqartemises").
		Name(name).
		Body(options).
		Do(context.TODO()).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *activeMQArtemises) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("activemqartemises").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do(context.TODO()).
		Error()
}

// Patch applies the patch and returns the patched activeMQArtemis.
func (c *activeMQArtemises) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1beta1.ActiveMQArtemis, err error) {
	result = &v1beta1.ActiveMQArtemis{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("activemqartemises").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do(context.TODO()).
		Into(result)
	return
}
