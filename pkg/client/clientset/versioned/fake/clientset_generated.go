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
	clientset "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned"
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v1beta1"
	fakebrokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v1beta1/fake"
	brokerv2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha1"
	fakebrokerv2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha1/fake"
	brokerv2alpha2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha2"
	fakebrokerv2alpha2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha2/fake"
	brokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha3"
	fakebrokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha3/fake"
	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha4"
	fakebrokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha4/fake"
	brokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha5"
	fakebrokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha5/fake"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/testing"
)

// NewSimpleClientset returns a clientset that will respond with the provided objects.
// It's backed by a very simple object tracker that processes creates, updates and deletions as-is,
// without applying any validations and/or defaults. It shouldn't be considered a replacement
// for a real clientset and is mostly useful in simple unit tests.
func NewSimpleClientset(objects ...runtime.Object) *Clientset {
	o := testing.NewObjectTracker(scheme, codecs.UniversalDecoder())
	for _, obj := range objects {
		if err := o.Add(obj); err != nil {
			panic(err)
		}
	}

	clientset := Clientset{}
	clientset.Fake = testing.Fake{}

	//	fakePtr := testing.Fake{}
	clientset.Fake.AddReactor("*", "*", testing.ObjectReaction(o))
	clientset.Fake.AddWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := o.Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		return true, watch, nil
	})
	clientset.discovery = &fakediscovery.FakeDiscovery{Fake: &clientset.Fake}

	return &clientset
}

// Clientset implements clientset.Interface. Meant to be embedded into a
// struct to get a default implementation. This makes faking out just the method
// you want to test easier.
type Clientset struct {
	testing.Fake
	discovery *fakediscovery.FakeDiscovery
}

func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	return c.discovery
}

var _ clientset.Interface = &Clientset{}

// BrokerV2alpha1 retrieves the BrokerV2alpha1Client
func (c *Clientset) BrokerV2alpha1() brokerv2alpha1.BrokerV2alpha1Interface {
	return &fakebrokerv2alpha1.FakeBrokerV2alpha1{Fake: &c.Fake}
}

// BrokerV2alpha2 retrieves the BrokerV2alpha2Client
func (c *Clientset) BrokerV2alpha2() brokerv2alpha2.BrokerV2alpha2Interface {
	return &fakebrokerv2alpha2.FakeBrokerV2alpha2{Fake: &c.Fake}
}

// BrokerV2alpha3 retrieves the BrokerV2alpha3Client
func (c *Clientset) BrokerV2alpha3() brokerv2alpha3.BrokerV2alpha3Interface {
	return &fakebrokerv2alpha3.FakeBrokerV2alpha3{Fake: &c.Fake}
}

// BrokerV2alpha4 retrieves the BrokerV2alpha3Client
func (c *Clientset) BrokerV2alpha4() brokerv2alpha4.BrokerV2alpha4Interface {
	return &fakebrokerv2alpha4.FakeBrokerV2alpha4{Fake: &c.Fake}
}

// BrokerV2alpha5 retrieves the BrokerV2alpha5Client
func (c *Clientset) BrokerV2alpha5() brokerv2alpha5.BrokerV2alpha5Interface {
	return &fakebrokerv2alpha5.FakeBrokerV2alpha5{Fake: &c.Fake}
}

// BrokerV1beta1 retrieves the BrokerV1beta1Client
func (c *Clientset) BrokerV1beta1() brokerv1beta1.BrokerV1beta1Interface {
	return &fakebrokerv1beta1.FakeBrokerV1beta1{Fake: &c.Fake}
}

// Broker retrieves the BrokerV1Beta1Client
func (c *Clientset) Broker() brokerv1beta1.BrokerV1beta1Interface {
	return &fakebrokerv1beta1.FakeBrokerV1beta1{Fake: &c.Fake}
}
