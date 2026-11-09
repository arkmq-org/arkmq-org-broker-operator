/*
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

package controllers

import (
	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func boolPtr(b bool) *bool { return &b }

var _ = Describe("addressref validation unit", Label(unitLabel), func() {

	Context("validateAddressRef", func() {

		It("rejects empty address", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: ""}
			err := validateAddressRef(ref, "ProducerOf", 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("address must be specified"))
		})

		It("rejects address with FQQN separator", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "topic::queue1"}
			err := validateAddressRef(ref, "ConsumerOf", 1)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("FQQN format"))
		})

		It("rejects appNamespace without appName", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "orders", AppNamespace: "other-ns"}
			err := validateAddressRef(ref, "ProducerOf", 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("appName is required when appNamespace is set"))
		})

		It("allows appName without appNamespace for same-namespace inference", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "orders", AppName: "other-app"}
			err := validateAddressRef(ref, "ConsumerOf", 0)
			Expect(err).NotTo(HaveOccurred())
		})

		It("rejects pubSub=false with subscriptions", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address:       "orders",
				PubSub:        boolPtr(false),
				Subscriptions: []string{"sub1"},
			}
			err := validateAddressRef(ref, "ConsumerOf", 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pubSub cannot be false when subscriptions are specified"))
		})

		It("accepts valid cross-app ref", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "orders", AppNamespace: "ns", AppName: "app"}
			Expect(validateAddressRef(ref, "ProducerOf", 0)).To(Succeed())
		})

		It("accepts valid local ref", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "orders"}
			Expect(validateAddressRef(ref, "ConsumerOf", 0)).To(Succeed())
		})
	})

	Context("validateProducerOfRef", func() {

		It("rejects empty address via base validation", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: ""}
			err := validateProducerOfRef(ref, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ProducerOf"))
			Expect(err.Error()).To(ContainSubstring("address must be specified"))
		})

		It("rejects multicast producer with subscription queue names", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address:       "events",
				PubSub:        boolPtr(true),
				Subscriptions: []string{"sub1"},
			}
			err := validateProducerOfRef(ref, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("producers don't create queues"))
		})

		It("accepts multicast producer with empty subscriptions", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address: "events",
				PubSub:  boolPtr(true),
			}
			Expect(validateProducerOfRef(ref, 0)).To(Succeed())
		})

		It("accepts anycast producer", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "commands"}
			Expect(validateProducerOfRef(ref, 0)).To(Succeed())
		})
	})

	Context("validateConsumerOfRef", func() {

		It("rejects empty address via base validation", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: ""}
			err := validateConsumerOfRef(ref, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ConsumerOf"))
			Expect(err.Error()).To(ContainSubstring("address must be specified"))
		})

		It("rejects pubSub consumer with zero subscriptions", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address: "events",
				PubSub:  boolPtr(true),
			}
			err := validateConsumerOfRef(ref, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must specify at least one subscription queue name"))
		})

		It("rejects pubSub consumer with empty queue name", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address:       "events",
				PubSub:        boolPtr(true),
				Subscriptions: []string{"valid-sub", ""},
			}
			err := validateConsumerOfRef(ref, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("queue name cannot be empty"))
		})

		It("rejects pubSub consumer with FQQN in subscription", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address:       "events",
				PubSub:        boolPtr(true),
				Subscriptions: []string{"events::sub1"},
			}
			err := validateConsumerOfRef(ref, 0)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("FQQN format"))
		})

		It("accepts pubSub consumer with valid subscriptions", Label(unitLabel), func() {
			ref := broker.AddressRef{
				Address:       "events",
				PubSub:        boolPtr(true),
				Subscriptions: []string{"sub1", "sub2"},
			}
			Expect(validateConsumerOfRef(ref, 0)).To(Succeed())
		})

		It("accepts anycast consumer", Label(unitLabel), func() {
			ref := broker.AddressRef{Address: "commands"}
			Expect(validateConsumerOfRef(ref, 0)).To(Succeed())
		})
	})

	Context("validateCapabilityRoutingConsistency", func() {

		It("rejects mixed routing for same address", Label(unitLabel), func() {
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "orders", PubSub: boolPtr(true)},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "orders"},
				},
			}
			err := validateCapabilityRoutingConsistency(cap)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("orders"))
			Expect(err.Error()).To(ContainSubstring("consistent semantics"))
		})

		It("accepts consistent multicast routing", Label(unitLabel), func() {
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "events", PubSub: boolPtr(true)},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "events", PubSub: boolPtr(true), Subscriptions: []string{"sub1"}},
				},
			}
			Expect(validateCapabilityRoutingConsistency(cap)).To(Succeed())
		})

		It("accepts consistent anycast routing", Label(unitLabel), func() {
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "commands"},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "commands"},
				},
			}
			Expect(validateCapabilityRoutingConsistency(cap)).To(Succeed())
		})

		It("skips cross-app refs in consistency check", Label(unitLabel), func() {
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "events", PubSub: boolPtr(true)},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "events", AppNamespace: "other-ns", AppName: "other-app"},
				},
			}
			Expect(validateCapabilityRoutingConsistency(cap)).To(Succeed())
		})
	})
})
