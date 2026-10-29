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

func testEmptySubscriptionsArrayMulticastOnly() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("multicast-app", "test").
		WithAddresses(NewAddressType("events").WithPubSub(true).Build()).
		Build()

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

func testSingleQueueAnycastRouting() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("anycast-app", "test").
		WithAddresses(NewAddressType("orders").Build()).
		Build()

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

func testMultipleSubsAllCreated() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("multi-queue-app", "test").
		WithAddresses(NewAddressType("tasks").WithSubscriptions("high-priority", "low-priority", "default").Build()).
		Build()

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

func testSubsWithCapabilitiesSubsAndRBAC() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("queue-with-caps", "test").
		WithAddresses(NewAddressType("commands").Build()).
		WithProducerOf(NewAddressRef("commands").Build()).
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

func testNoQueuesFieldInferredFromCapabilities() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("inferred-queues", "test").
		WithAddresses(NewAddressType("legacy").Build()).
		WithConsumerOf(NewAddressRef("legacy").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "inferred-queues")

	legacyAddr := caps.AddressConfigurations["legacy"]
	Expect(legacyAddr).NotTo(BeNil(), "expected addressConfigurations for owned address 'legacy'")
	Expect(legacyAddr.RoutingTypes).NotTo(BeEmpty(), "expected routingTypes for owned address 'legacy'")
	Expect(legacyAddr.QueueConfigs["legacy"]).NotTo(BeNil(), "expected queueConfigs inferred from ConsumerOf capability")
	Expect(caps.SecurityRoles).To(HaveKey("legacy"), "expected securityRoles from capabilities")
}

func testMixedMulticastAndAnycast() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("mixed-routing", "test").
		WithAddresses(
			NewAddressType("events").WithPubSub(true).Build(),
			NewAddressType("commands").Build(),
		).
		Build()

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

func testSubsWithSubscriberCapability() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("subscriber-with-queues", "test").
		WithAddresses(NewAddressType("notifications").WithSubscriptions("email", "sms").Build()).
		WithConsumerOf(NewAddressRef("notifications").WithSubscriptions("push").Build()).
		Build()

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

	It("empty subscriptions array multicast only", Label(unitLabel), func() {
		testEmptySubscriptionsArrayMulticastOnly()
	})

	It("single queue anycast routing", Label(unitLabel), func() {
		testSingleQueueAnycastRouting()
	})

	It("multiple subs all created", Label(unitLabel), func() {
		testMultipleSubsAllCreated()
	})

	It("subs with capabilities subs and RBAC", Label(unitLabel), func() {
		testSubsWithCapabilitiesSubsAndRBAC()
	})

	It("no queues field inferred from capabilities", Label(unitLabel), func() {
		testNoQueuesFieldInferredFromCapabilities()
	})

	It("mixed multicast and anycast", Label(unitLabel), func() {
		testMixedMulticastAndAnycast()
	})

	It("subs with subscriber capability", Label(unitLabel), func() {
		testSubsWithSubscriberCapability()
	})
})
