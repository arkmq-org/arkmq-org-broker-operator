package controllers

import (
	"strings"

	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func testMulticastRoutingForSubscriptions(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("multicast-app", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("events").Build())
	} else {
		builder.WithAddresses(NewAddressType("events").Build())
	}
	app := builder.WithConsumerOf(NewAddressRef("events").WithSubscriptions("sub1").Build()).Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "multicast-app")

	eventsAddr := caps.AddressConfigurations["events"]
	Expect(eventsAddr).NotTo(BeNil(), "expected addressConfigurations for 'events'")
	Expect(eventsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast),
		"expected routingTypes=MULTICAST for subscription address")
}

func testAnycastRoutingForConsumerOf(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("anycast-app", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("commands").Build())
	} else {
		builder.WithAddresses(NewAddressType("commands").Build())
	}
	app := builder.WithConsumerOf(NewAddressRef("commands").Build()).Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "anycast-app")

	commandsAddr := caps.AddressConfigurations["commands"]
	Expect(commandsAddr).NotTo(BeNil(), "expected addressConfigurations for 'commands'")
	Expect(commandsAddr.RoutingTypes).To(Equal(brokerproperties.RoutingTypeAnycast),
		"expected routingTypes=ANYCAST for consumerOf address")
}

func testConflictingRoutingTypesSameApp(useShared bool) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	builder := NewBrokerApp("conflict-app", "test")
	if useShared {
		builder.WithSharedAddresses(NewAddressType("mixed").Build())
	} else {
		builder.WithAddresses(NewAddressType("mixed").Build())
	}
	app := builder.WithConsumerOf(
		NewAddressRef("mixed").Build(),                             // ANYCAST
		NewAddressRef("mixed").WithSubscriptions("queue1").Build(), // MULTICAST
	).Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).To(HaveOccurred(), "processCapabilities should have failed with routing type conflict")

	expectedKeywords := []string{"mixed", "pubSub", "conflict"}
	errMsg := err.Error()
	for _, keyword := range expectedKeywords {
		Expect(strings.Contains(errMsg, keyword)).To(BeTrue(),
			"error message should contain '%s', got: %s", keyword, errMsg)
	}
}

var _ = Describe("brokerservice routing conflict", func() {

	It("multicast routing for subscriptions shared", Label(unitLabel), func() {
		testMulticastRoutingForSubscriptions(true)
	})

	It("multicast routing for subscriptions private", Label(unitLabel), func() {
		testMulticastRoutingForSubscriptions(false)
	})

	It("anycast routing for consumerOf shared", Label(unitLabel), func() {
		testAnycastRoutingForConsumerOf(true)
	})

	It("anycast routing for consumerOf private", Label(unitLabel), func() {
		testAnycastRoutingForConsumerOf(false)
	})

	It("conflicting routing types same app shared", Label(unitLabel), func() {
		testConflictingRoutingTypesSameApp(true)
	})

	It("conflicting routing types same app private", Label(unitLabel), func() {
		testConflictingRoutingTypesSameApp(false)
	})

	It("conflicting routing types multiple apps", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app1 := NewBrokerApp("producer-app", "test").
			WithSharedAddresses(NewAddressType("shared-events").Build()).
			WithProducerOf(NewAddressRef("shared-events").Build()).
			WithConsumerOf(NewAddressRef("shared-events").WithSubscriptions("producer-sub").Build()).
			Build()

		app2 := NewBrokerApp("consumer-app", "test").
			WithConsumerOf(NewAddressRef("shared-events").WithAppRef("test", "producer-app").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app1)
		Expect(err).NotTo(HaveOccurred())

		err = reconciler.processCapabilities(secret, app2)
		Expect(err).NotTo(HaveOccurred())

		caps1 := parseCapabilities(secret, "producer-app")
		caps2 := parseCapabilities(secret, "consumer-app")

		addr1 := caps1.AddressConfigurations["shared-events"]
		Expect(addr1).NotTo(BeNil())
		Expect(addr1.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast),
			"app1 should have MULTICAST routing for shared-events")

		addr2 := caps2.AddressConfigurations["shared-events"]
		if addr2 != nil {
			Expect(addr2.RoutingTypes).To(BeEmpty(),
				"app2 should NOT generate routingTypes for cross-app address")
		}

		Expect(addr2).NotTo(BeNil())
		Expect(addr2.QueueConfigs["shared-events"]).NotTo(BeNil())
		Expect(addr2.QueueConfigs["shared-events"].RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast),
			"app2 should generate ANYCAST queue config (conflict detected at validation time, not here)")
	})

	It("shared address both subscriptions", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app1 := NewBrokerApp("sub-app1", "test").
			WithSharedAddresses(NewAddressType("topic").Build()).
			WithConsumerOf(NewAddressRef("topic").WithSubscriptions("sub1").Build()).
			Build()

		app2 := NewBrokerApp("sub-app2", "test").
			WithConsumerOf(NewAddressRef("topic").WithAppRef("test", "sub-app1").WithSubscriptions("sub2").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app1)
		Expect(err).NotTo(HaveOccurred())
		err = reconciler.processCapabilities(secret, app2)
		Expect(err).NotTo(HaveOccurred())

		caps1 := parseCapabilities(secret, "sub-app1")
		caps2 := parseCapabilities(secret, "sub-app2")

		addr1 := caps1.AddressConfigurations["topic"]
		Expect(addr1).NotTo(BeNil())
		Expect(addr1.RoutingTypes).To(Equal(brokerproperties.RoutingTypeMulticast), "app1 should have MULTICAST routing")

		addr2 := caps2.AddressConfigurations["topic"]
		if addr2 != nil {
			Expect(addr2.RoutingTypes).To(BeEmpty(), "app2 should NOT generate routingTypes for cross-app address")
		}

		Expect(addr1.QueueConfigs["sub1"]).NotTo(BeNil())
		Expect(addr1.QueueConfigs["sub1"].RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast), "app1 should have MULTICAST queue sub1")
		Expect(addr2).NotTo(BeNil())
		Expect(addr2.QueueConfigs["sub2"]).NotTo(BeNil())
		Expect(addr2.QueueConfigs["sub2"].RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast), "app2 should have MULTICAST queue sub2")
	})

	It("shared address both consumerOf", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app1 := NewBrokerApp("consumer-app1", "test").
			WithSharedAddresses(NewAddressType("queue").Build()).
			WithConsumerOf(NewAddressRef("queue").Build()).
			Build()

		app2 := NewBrokerApp("consumer-app2", "test").
			WithConsumerOf(NewAddressRef("queue").WithAppRef("test", "consumer-app1").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app1)
		Expect(err).NotTo(HaveOccurred())
		err = reconciler.processCapabilities(secret, app2)
		Expect(err).NotTo(HaveOccurred())

		caps1 := parseCapabilities(secret, "consumer-app1")
		caps2 := parseCapabilities(secret, "consumer-app2")

		addr1 := caps1.AddressConfigurations["queue"]
		Expect(addr1).NotTo(BeNil())
		Expect(addr1.RoutingTypes).To(Equal(brokerproperties.RoutingTypeAnycast), "app1 should have ANYCAST routing")

		addr2 := caps2.AddressConfigurations["queue"]
		if addr2 != nil {
			Expect(addr2.RoutingTypes).To(BeEmpty(), "app2 should NOT generate routingTypes for cross-app address")
		}

		Expect(addr1.QueueConfigs["queue"]).NotTo(BeNil())
		Expect(addr1.QueueConfigs["queue"].RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast), "app1 should have ANYCAST queue")
		Expect(addr2).NotTo(BeNil())
		Expect(addr2.QueueConfigs["queue"]).NotTo(BeNil())
		Expect(addr2.QueueConfigs["queue"].RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast), "app2 should have ANYCAST queue")
	})
})
