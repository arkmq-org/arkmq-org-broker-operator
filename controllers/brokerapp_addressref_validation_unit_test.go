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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/go-logr/logr"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
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
			err := validateCapabilityRoutingConsistency(cap, nil)
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
			Expect(validateCapabilityRoutingConsistency(cap, nil)).To(Succeed())
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
			Expect(validateCapabilityRoutingConsistency(cap, nil)).To(Succeed())
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
			Expect(validateCapabilityRoutingConsistency(cap, nil)).To(Succeed())
		})

		It("inherits multicast from declared address when ProducerOf has no routing signal", Label(unitLabel), func() {
			declared := map[string]bool{"events": true}
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "events"},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "events", Subscriptions: []string{"sub1"}},
				},
			}
			Expect(validateCapabilityRoutingConsistency(cap, declared)).To(Succeed())
		})

		It("rejects explicit anycast ProducerOf when declared address is multicast", Label(unitLabel), func() {
			declared := map[string]bool{"events": true}
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "events", PubSub: boolPtr(false)},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "events", Subscriptions: []string{"sub1"}},
				},
			}
			err := validateCapabilityRoutingConsistency(cap, declared)
			Expect(err).To(HaveOccurred())
		})

		It("no inheritance when address is not in spec.addresses", Label(unitLabel), func() {
			cap := broker.AppCapabilityType{
				ProducerOf: []broker.AddressRef{
					{Address: "orders"},
				},
				ConsumerOf: []broker.AddressRef{
					{Address: "orders", Subscriptions: []string{"sub1"}},
				},
			}
			err := validateCapabilityRoutingConsistency(cap, nil)
			Expect(err).To(HaveOccurred(),
				"without declared address, ProducerOf defaults to anycast conflicting with multicast ConsumerOf")
		})
	})
})

var _ = Describe("checkAddressSharing uses BoundSpec", Label(unitLabel), func() {

	newReconciler := func(consumerApp *broker.BrokerApp) BrokerAppInstanceReconciler {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		return BrokerAppInstanceReconciler{
			BrokerAppReconciler: &BrokerAppReconciler{
				ReconcilerLoop: &ReconcilerLoop{
					KubeBits: &KubeBits{Client: cl, log: logr.New(log.NullLogSink{})},
				},
			},
			instance: consumerApp,
			status:   consumerApp.Status.DeepCopy(),
		}
	}

	It("validates against BoundSpec when owner has unvalidated spec change", Label(unitLabel), func() {
		ns := "default"

		ownerApp := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: "owner", Namespace: ns},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{
					{Address: "events", ConsumerOfAppSelector: "true", Subscriptions: []string{"sub1"}},
				},
			},
			Status: broker.BrokerAppStatus{
				BoundSpec: &broker.AppBoundSpec{
					Generation: 1,
					Spec: broker.BrokerAppSpec{
						Addresses: []broker.AddressType{
							{Address: "events", ConsumerOfAppSelector: "true"},
						},
					},
				},
			},
		}

		consumerApp := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: "consumer", Namespace: ns},
		}

		reconciler := newReconciler(consumerApp)
		ref := broker.AddressRef{Address: "events", AppName: "owner"}

		err := reconciler.checkAddressSharing(ownerApp, ref, "consumer")
		Expect(err).ToNot(HaveOccurred(),
			"should validate against BoundSpec (anycast) not live Spec (multicast)")
	})

	It("rejects when BoundSpec also has multicast mismatch", Label(unitLabel), func() {
		ns := "default"

		ownerApp := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: "owner", Namespace: ns},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{
					{Address: "events", ConsumerOfAppSelector: "true", Subscriptions: []string{"sub1"}},
				},
			},
			Status: broker.BrokerAppStatus{
				BoundSpec: &broker.AppBoundSpec{
					Generation: 1,
					Spec: broker.BrokerAppSpec{
						Addresses: []broker.AddressType{
							{Address: "events", ConsumerOfAppSelector: "true", Subscriptions: []string{"sub1"}},
						},
					},
				},
			},
		}

		consumerApp := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: "consumer", Namespace: ns},
		}

		reconciler := newReconciler(consumerApp)
		ref := broker.AddressRef{Address: "events", AppName: "owner"}

		err := reconciler.checkAddressSharing(ownerApp, ref, "consumer")
		Expect(err).To(HaveOccurred(),
			"should reject when BoundSpec also has multicast but consumer references anycast")
		Expect(err.Error()).To(ContainSubstring("pubSub"))
	})

	It("falls back to Spec when BoundSpec is nil", Label(unitLabel), func() {
		ns := "default"

		ownerApp := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: "owner", Namespace: ns},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{
					{Address: "events", ConsumerOfAppSelector: "true"},
				},
			},
		}

		consumerApp := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: "consumer", Namespace: ns},
		}

		reconciler := newReconciler(consumerApp)
		ref := broker.AddressRef{Address: "events", AppName: "owner"}

		err := reconciler.checkAddressSharing(ownerApp, ref, "consumer")
		Expect(err).ToNot(HaveOccurred(),
			"should fall back to Spec when BoundSpec is nil (new app not yet deployed)")
	})
})
