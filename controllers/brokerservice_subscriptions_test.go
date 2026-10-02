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
	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func testEmptySubscriptionsArrayMulticastOnly(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("multicast-app", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("events").WithPubSub(true).Build())
	} else {
		builder.WithAddresses(NewAddressType("events").WithPubSub(true).Build())
	}
	app := builder.Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "multicast-app")

	eventsAddr := caps.AddressConfigurations["events"]
	Expect(eventsAddr).NotTo(BeNil(), "expected addressConfigurations for owned address 'events'")

	Expect(eventsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast),
		"expected MULTICAST routing type for empty queues array")

	Expect(eventsAddr.QueueConfigs).To(BeEmpty(),
		"should NOT have queueConfigs for multicast-only address (empty queues array)")

	Expect(caps.SecurityRoles).NotTo(HaveKey("events"),
		"should NOT have securityRoles when app has no capabilities")
}

func testSingleQueueAnycastRouting(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("anycast-app", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("orders").Build())
	} else {
		builder.WithAddresses(NewAddressType("orders").Build())
	}
	app := builder.Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "anycast-app")

	ordersAddr := caps.AddressConfigurations["orders"]
	Expect(ordersAddr).NotTo(BeNil(), "expected addressConfigurations for owned address 'orders'")

	Expect(ordersAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeAnycast),
		"expected ANYCAST routing type for address with queues")

	queueCfg := ordersAddr.QueueConfigs["orders"]
	Expect(queueCfg).NotTo(BeNil(), "expected queueConfigs for declared queue 'orders'")
	Expect(queueCfg.RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast),
		"expected queueConfigs routingType=ANYCAST for declared queue")
	Expect(queueCfg.Address).To(Equal("orders"),
		"expected queueConfigs address=orders")
}

func testMultipleSubsAllCreated(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("multi-queue-app", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("tasks").WithSubscriptions("high-priority", "low-priority", "default").Build())
	} else {
		builder.WithAddresses(NewAddressType("tasks").WithSubscriptions("high-priority", "low-priority", "default").Build())
	}
	app := builder.Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "multi-queue-app")

	tasksAddr := caps.AddressConfigurations["tasks"]
	Expect(tasksAddr).NotTo(BeNil(), "expected addressConfigurations for owned address 'tasks'")

	Expect(tasksAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast),
		"expected MULTICAST routing for subscription address")

	for _, queueName := range []string{"high-priority", "low-priority", "default"} {
		queueCfg := tasksAddr.QueueConfigs[queueName]
		Expect(queueCfg).NotTo(BeNil(), "expected queueConfigs for declared queue %q", queueName)
		Expect(queueCfg.RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast),
			"expected routingType=MULTICAST for queue %q (subscriptions imply pub/sub)", queueName)
		Expect(queueCfg.Address).To(Equal("tasks"),
			"expected queue %q to map to address 'tasks'", queueName)
	}
}

func testSubsWithCapabilitiesSubsAndRBAC(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("queue-with-caps", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("commands").Build())
	} else {
		builder.WithAddresses(NewAddressType("commands").Build())
	}
	app := builder.WithProducerOf(NewAddressRef("commands").Build()).
		WithConsumerOf(NewAddressRef("commands").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "queue-with-caps")

	commandsAddr := caps.AddressConfigurations["commands"]
	Expect(commandsAddr).NotTo(BeNil(), "expected addressConfigurations for 'commands'")
	Expect(commandsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeAnycast),
		"expected ANYCAST routing")
	Expect(commandsAddr.QueueConfigs["commands"]).NotTo(BeNil())
	Expect(commandsAddr.QueueConfigs["commands"].RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast),
		"expected queueConfigs for declared queue 'commands' with ANYCAST routing")

	commandsRoles := caps.SecurityRoles["commands"]
	Expect(commandsRoles).NotTo(BeNil(), "expected securityRoles for 'commands'")
	p := commandsRoles["test-queue-with-caps-producer"]
	Expect(p).NotTo(BeNil(), "expected producer RBAC role")
	Expect(p.Send).To(BeTrue(), "expected producer RBAC role with send=true")
	c := commandsRoles["test-queue-with-caps-consumer"]
	Expect(c).NotTo(BeNil(), "expected consumer RBAC role")
	Expect(c.Consume).To(BeTrue(), "expected consumer RBAC role with consume=true")
}

func testNoQueuesFieldInferredFromCapabilities(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("inferred-queues", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("legacy").Build())
	} else {
		builder.WithAddresses(NewAddressType("legacy").Build())
	}
	app := builder.WithConsumerOf(NewAddressRef("legacy").Build()).Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "inferred-queues")

	legacyAddr := caps.AddressConfigurations["legacy"]
	Expect(legacyAddr).NotTo(BeNil(), "expected addressConfigurations for owned address 'legacy'")
	Expect(legacyAddr.RoutingTypes).NotTo(BeEmpty(), "expected routingTypes for owned address 'legacy'")
	Expect(legacyAddr.QueueConfigs["legacy"]).NotTo(BeNil(), "expected queueConfigs inferred from ConsumerOf capability")
	Expect(caps.SecurityRoles).To(HaveKey("legacy"), "expected securityRoles from capabilities")
}

func testMixedMulticastAndAnycast(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("mixed-routing", "test")
	if useShared {
		builder.WithSharedAddresses(
			NewAddressType("events").WithPubSub(true).Build(),
			NewAddressType("commands").Build(),
		)
	} else {
		builder.WithAddresses(
			NewAddressType("events").WithPubSub(true).Build(),
			NewAddressType("commands").Build(),
		)
	}
	app := builder.Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "mixed-routing")

	Expect(caps.AddressConfigurations).To(HaveKey("events"), "expected addressConfigurations for 'events'")
	Expect(caps.AddressConfigurations).To(HaveKey("commands"), "expected addressConfigurations for 'commands'")

	Expect(caps.AddressConfigurations["events"].QueueConfigs).To(BeEmpty(),
		"should NOT have queueConfigs for multicast-only address 'events'")
	Expect(caps.AddressConfigurations["commands"].QueueConfigs["commands"]).NotTo(BeNil(),
		"expected queueConfigs for anycast address 'commands'")
}

func testSubsWithSubscriberCapability(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("subscriber-with-queues", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("notifications").WithSubscriptions("email", "sms").Build())
	} else {
		builder.WithAddresses(NewAddressType("notifications").WithSubscriptions("email", "sms").Build())
	}
	app := builder.WithConsumerOf(NewAddressRef("notifications").WithSubscriptions("push").Build()).Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "subscriber-with-queues")

	notifAddr := caps.AddressConfigurations["notifications"]
	Expect(notifAddr).NotTo(BeNil(), "expected addressConfigurations for 'notifications'")

	for _, queueName := range []string{"email", "sms", "push"} {
		queueCfg := notifAddr.QueueConfigs[queueName]
		Expect(queueCfg).NotTo(BeNil(), "expected queueConfigs for queue %q", queueName)
		Expect(queueCfg.RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast),
			"expected MULTICAST routing for queue %q", queueName)
	}
}

var _ = Describe("brokerservice subscriptions", func() {

	It("empty subscriptions array multicast only private", Label(unitLabel), func() {
		testEmptySubscriptionsArrayMulticastOnly(false)
	})

	It("empty subscriptions array multicast only shared", Label(unitLabel), func() {
		testEmptySubscriptionsArrayMulticastOnly(true)
	})

	It("single queue anycast routing private", Label(unitLabel), func() {
		testSingleQueueAnycastRouting(false)
	})

	It("single queue anycast routing shared", Label(unitLabel), func() {
		testSingleQueueAnycastRouting(true)
	})

	It("multiple subs all created private", Label(unitLabel), func() {
		testMultipleSubsAllCreated(false)
	})

	It("multiple subs all created shared", Label(unitLabel), func() {
		testMultipleSubsAllCreated(true)
	})

	It("subs with capabilities subs and RBAC private", Label(unitLabel), func() {
		testSubsWithCapabilitiesSubsAndRBAC(false)
	})

	It("subs with capabilities subs and RBAC shared", Label(unitLabel), func() {
		testSubsWithCapabilitiesSubsAndRBAC(true)
	})

	It("no queues field inferred from capabilities private", Label(unitLabel), func() {
		testNoQueuesFieldInferredFromCapabilities(false)
	})

	It("no queues field inferred from capabilities shared", Label(unitLabel), func() {
		testNoQueuesFieldInferredFromCapabilities(true)
	})

	It("mixed multicast and anycast private", Label(unitLabel), func() {
		testMixedMulticastAndAnycast(false)
	})

	It("mixed multicast and anycast shared", Label(unitLabel), func() {
		testMixedMulticastAndAnycast(true)
	})

	It("subs with subscriber capability private", Label(unitLabel), func() {
		testSubsWithSubscriberCapability(false)
	})

	It("subs with subscriber capability shared", Label(unitLabel), func() {
		testSubsWithSubscriberCapability(true)
	})
})
