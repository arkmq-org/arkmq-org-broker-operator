package controllers

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("addressref subscriptions", func() {

	It("ANYCAST queue for nil subscriptions", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("anycast-app", "test").
			WithConsumerOf(NewAddressRef("commands").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "anycast-app")

		commandsAddr := caps.AddressConfigurations["commands"]
		Expect(commandsAddr).NotTo(BeNil(), "expected addressConfigurations for 'commands'")

		Expect(commandsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeAnycast),
			"expected routingTypes=ANYCAST for nil subscriptions")

		queueCfg := commandsAddr.QueueConfigs["commands"]
		Expect(queueCfg).NotTo(BeNil(), "expected ANYCAST queue config")
		Expect(queueCfg.RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast),
			"expected queueConfig routingType=ANYCAST")
	})

	It("MULTICAST topic with subscription queues", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("multicast-app", "test").
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("sub1", "sub2").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "multicast-app")

		eventsAddr := caps.AddressConfigurations["events"]
		Expect(eventsAddr).NotTo(BeNil(), "expected addressConfigurations for 'events'")

		Expect(eventsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast),
			"expected routingTypes=MULTICAST for subscriptions")

		sub1 := eventsAddr.QueueConfigs["sub1"]
		Expect(sub1).NotTo(BeNil(), "expected MULTICAST queue sub1")
		Expect(sub1.RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast))

		sub2 := eventsAddr.QueueConfigs["sub2"]
		Expect(sub2).NotTo(BeNil(), "expected MULTICAST queue sub2")
		Expect(sub2.RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast))

		Expect(caps.SecurityRoles).To(HaveKey("events::sub1"), "expected subscriber role for events::sub1")
		Expect(caps.SecurityRoles).To(HaveKey("events::sub2"), "expected subscriber role for events::sub2")
	})

	It("empty subscriptions producer ANYCAST", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("producer-app", "test").
			WithProducerOf(NewAddressRef("notifications").WithSubscriptions().Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "producer-app")

		notifAddr := caps.AddressConfigurations["notifications"]
		Expect(notifAddr).NotTo(BeNil(), "expected addressConfigurations for 'notifications'")

		Expect(notifAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeAnycast),
			"expected routingTypes=ANYCAST for empty subscriptions")

		Expect(notifAddr.QueueConfigs).NotTo(BeEmpty(), "producer should create queue configs")
	})

	It("routing type conflict", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("conflict-app", "test").
			WithConsumerOf(
				NewAddressRef("mixed").Build(),
				NewAddressRef("mixed").WithSubscriptions("sub1").Build(),
			).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).To(HaveOccurred(), "expected error for routing type conflict")
		Expect(err.Error()).To(ContainSubstring("conflict"))
	})

	It("shared address subscriptions only multicast", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("producer-app", "test").
			WithSharedAddresses(NewAddressType("events").WithSubscriptions("sub1").Build()).
			WithProducerOf(v1beta2.AddressRef{Address: "events", PubSub: &[]bool{true}[0]}).Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "producer-app")

		eventsAddr := caps.AddressConfigurations["events"]
		Expect(eventsAddr).NotTo(BeNil(), "expected addressConfigurations for 'events'")

		Expect(eventsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast),
			"expected routingTypes=MULTICAST")
	})
})
