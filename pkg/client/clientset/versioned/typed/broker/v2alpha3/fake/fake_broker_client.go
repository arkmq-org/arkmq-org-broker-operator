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
	v2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v2alpha3"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeBrokerV2alpha3 struct {
	*testing.Fake
}

func (c *FakeBrokerV2alpha3) ActiveMQArtemises(namespace string) v2alpha3.ActiveMQArtemisInterface {
	return &FakeActiveMQArtemises{c, namespace}
}

func (c *FakeBrokerV2alpha3) ActiveMQArtemisAddresses(namespace string) v2alpha3.ActiveMQArtemisAddressInterface {
	return &FakeActiveMQArtemisAddresses{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeBrokerV2alpha3) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
