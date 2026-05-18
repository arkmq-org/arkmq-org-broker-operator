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

package versioned

import (
	"fmt"

	brokerv1alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v1alpha1"
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v1beta1"
	brokerv2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha1"
	brokerv2alpha2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha2"
	brokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha3"
	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha4"
	brokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha5"
	discovery "k8s.io/client-go/discovery"
	rest "k8s.io/client-go/rest"
	flowcontrol "k8s.io/client-go/util/flowcontrol"
)

type Interface interface {
	Discovery() discovery.DiscoveryInterface
	BrokerV2alpha1() brokerv2alpha1.BrokerV2alpha1Interface
	BrokerV2alpha2() brokerv2alpha2.BrokerV2alpha2Interface
	BrokerV2alpha3() brokerv2alpha3.BrokerV2alpha3Interface
	BrokerV2alpha4() brokerv2alpha4.BrokerV2alpha4Interface
	BrokerV2alpha5() brokerv2alpha5.BrokerV2alpha5Interface
	BrokerV1beta1() brokerv1beta1.BrokerV1beta1Interface
	// Deprecated: please explicitly pick a version if possible.
	Broker() brokerv1beta1.BrokerV1beta1Interface
}

// Clientset contains the clients for groups. Each group has exactly one
// version included in a Clientset.
type Clientset struct {
	*discovery.DiscoveryClient
	brokerV1alpha1 *brokerv1alpha1.BrokerV1alpha1Client
	brokerV2alpha1 *brokerv2alpha1.BrokerV2alpha1Client
	brokerV2alpha2 *brokerv2alpha2.BrokerV2alpha2Client
	brokerV2alpha3 *brokerv2alpha3.BrokerV2alpha3Client
	brokerV2alpha4 *brokerv2alpha4.BrokerV2alpha4Client
	brokerV2alpha5 *brokerv2alpha5.BrokerV2alpha5Client
	brokerV1beta1  *brokerv1beta1.BrokerV1beta1Client
}

// BrokerV1alpha1 retrieves the BrokerV1alpha1Client
func (c *Clientset) BrokerV1alpha1() brokerv1alpha1.BrokerV1alpha1Interface {
	return c.brokerV1alpha1
}

// BrokerV2alpha1 retrieves the BrokerV2alpha1Client
func (c *Clientset) BrokerV2alpha1() brokerv2alpha1.BrokerV2alpha1Interface {
	return c.brokerV2alpha1
}

// BrokerV2alpha2 retrieves the BrokerV2alpha2Client
func (c *Clientset) BrokerV2alpha2() brokerv2alpha2.BrokerV2alpha2Interface {
	return c.brokerV2alpha2
}

// BrokerV2alpha3 retrieves the BrokerV2alpha3Client
func (c *Clientset) BrokerV2alpha3() brokerv2alpha3.BrokerV2alpha3Interface {
	return c.brokerV2alpha3
}

// BrokerV2alpha4 retrieves the BrokerV2alpha4Client
func (c *Clientset) BrokerV2alpha4() brokerv2alpha4.BrokerV2alpha4Interface {
	return c.brokerV2alpha4
}

// BrokerV2alpha5 retrieves the BrokerV2alpha5Client
func (c *Clientset) BrokerV2alpha5() brokerv2alpha5.BrokerV2alpha5Interface {
	return c.brokerV2alpha5
}

// BrokerV1beta1 retrieves the BrokerV1beta1Client
func (c *Clientset) BrokerV1beta1() brokerv1beta1.BrokerV1beta1Interface {
	return c.brokerV1beta1
}

// Deprecated: Broker retrieves the default version of BrokerClient.
// Please explicitly pick a version.
func (c *Clientset) Broker() brokerv1beta1.BrokerV1beta1Interface {
	return c.brokerV1beta1
}

// Discovery retrieves the DiscoveryClient
func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	if c == nil {
		return nil
	}
	return c.DiscoveryClient
}

// NewForConfig creates a new Clientset for the given config.
func NewForConfig(c *rest.Config) (*Clientset, error) {
	configShallowCopy := *c
	if configShallowCopy.RateLimiter == nil && configShallowCopy.QPS > 0 {
		configShallowCopy.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(configShallowCopy.QPS, configShallowCopy.Burst)
	}
	var cs Clientset
	var err error
	cs.brokerV1alpha1, err = brokerv1alpha1.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}

	cs.brokerV2alpha1, err = brokerv2alpha1.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.brokerV2alpha2, err = brokerv2alpha2.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.brokerV2alpha3, err = brokerv2alpha3.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.brokerV2alpha4, err = brokerv2alpha4.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.brokerV2alpha5, err = brokerv2alpha5.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}
	cs.brokerV1beta1, err = brokerv1beta1.NewForConfig(&configShallowCopy)
	if err != nil {
		return nil, err
	}

	cs.DiscoveryClient, err = discovery.NewDiscoveryClientForConfig(&configShallowCopy)
	if err != nil {
		fmt.Printf("failed to create the DiscoveryClient: %v\n", err)
		return nil, err
	}
	return &cs, nil
}

// NewForConfigOrDie creates a new Clientset for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *Clientset {
	var cs Clientset
	cs.brokerV1alpha1 = brokerv1alpha1.NewForConfigOrDie(c)
	cs.brokerV2alpha1 = brokerv2alpha1.NewForConfigOrDie(c)
	cs.brokerV2alpha2 = brokerv2alpha2.NewForConfigOrDie(c)
	cs.brokerV2alpha3 = brokerv2alpha3.NewForConfigOrDie(c)
	cs.brokerV2alpha4 = brokerv2alpha4.NewForConfigOrDie(c)
	cs.brokerV2alpha5 = brokerv2alpha5.NewForConfigOrDie(c)
	cs.brokerV1beta1 = brokerv1beta1.NewForConfigOrDie(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClientForConfigOrDie(c)
	return &cs
}

// New creates a new Clientset for the given RESTClient.
func New(c rest.Interface) *Clientset {
	var cs Clientset
	cs.brokerV1alpha1 = brokerv1alpha1.New(c)
	cs.brokerV2alpha1 = brokerv2alpha1.New(c)
	cs.brokerV2alpha2 = brokerv2alpha2.New(c)
	cs.brokerV2alpha3 = brokerv2alpha3.New(c)
	cs.brokerV2alpha4 = brokerv2alpha4.New(c)
	cs.brokerV2alpha5 = brokerv2alpha5.New(c)
	cs.brokerV1beta1 = brokerv1beta1.New(c)

	cs.DiscoveryClient = discovery.NewDiscoveryClient(c)
	return &cs
}
