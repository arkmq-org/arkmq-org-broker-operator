package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BrokerService address routing conflict detection", func() {
	Context("routing type selection", func() {
		It("should use MULTICAST routing type for subscription addresses", func() {
			testMulticastRoutingForSubscriptions(true)
		})

		It("should use MULTICAST routing type for subscription addresses with private addresses", func() {
			testMulticastRoutingForSubscriptions(false)
		})

		It("should use ANYCAST routing type for consumerOf addresses", func() {
			testAnycastRoutingForConsumerOf(true)
		})

		It("should use ANYCAST routing type for consumerOf addresses with private addresses", func() {
			testAnycastRoutingForConsumerOf(false)
		})

		It("should reject conflicting routing types in the same app", func() {
			testConflictingRoutingTypesSameApp(true)
		})

		It("should reject conflicting routing types in the same app with private addresses", func() {
			testConflictingRoutingTypesSameApp(false)
		})

		It("should allow shared multicast routing across apps", func() {
			testSharedAddressBothSubscriptions()
		})

		It("should allow shared anycast routing across apps", func() {
			testSharedAddressBothConsumerOf()
		})

		It("should detect conflicts when different apps mix routing types", func() {
			testConflictingRoutingTypesMultipleApps()
		})
	})
})

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

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."events".routingTypes=MULTICAST`))
	Expect(props).NotTo(ContainSubstring(`addressConfigurations."events".routingTypes=ANYCAST`))
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

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."commands".routingTypes=ANYCAST`))
	Expect(props).NotTo(ContainSubstring(`addressConfigurations."commands".routingTypes=MULTICAST`))
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
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("mixed"))
	Expect(err.Error()).To(ContainSubstring("pubSub"))
	Expect(err.Error()).To(ContainSubstring("conflict"))
}

func testConflictingRoutingTypesMultipleApps() {
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

	Expect(reconciler.processCapabilities(secret, app1)).NotTo(HaveOccurred())
	Expect(reconciler.processCapabilities(secret, app2)).NotTo(HaveOccurred())

	props1 := string(secret.Data["test-producer-app-capabilities.properties"])
	props2 := string(secret.Data["test-consumer-app-capabilities.properties"])

	Expect(props1).To(ContainSubstring(`addressConfigurations."shared-events".routingTypes=MULTICAST`))
	Expect(props2).NotTo(ContainSubstring(`addressConfigurations."shared-events".routingTypes`))
	Expect(props2).To(ContainSubstring(`queueConfigs."shared-events".routingType=ANYCAST`))
}

func testSharedAddressBothSubscriptions() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app1 := NewBrokerApp("sub-app1", "test").
		WithSharedAddresses(NewAddressType("topic").Build()).
		WithConsumerOf(NewAddressRef("topic").WithSubscriptions("sub1").Build()).
		Build()

	app2 := NewBrokerApp("sub-app2", "test").
		WithConsumerOf(NewAddressRef("topic").WithAppRef("test", "sub-app1").WithSubscriptions("sub2").Build()).
		Build()

	Expect(reconciler.processCapabilities(secret, app1)).NotTo(HaveOccurred())
	Expect(reconciler.processCapabilities(secret, app2)).NotTo(HaveOccurred())

	props1 := string(secret.Data["test-sub-app1-capabilities.properties"])
	props2 := string(secret.Data["test-sub-app2-capabilities.properties"])

	Expect(props1).To(ContainSubstring(`addressConfigurations."topic".routingTypes=MULTICAST`))
	Expect(props2).NotTo(ContainSubstring(`addressConfigurations."topic".routingTypes`))
	Expect(props1).To(ContainSubstring(`queueConfigs."sub1".routingType=MULTICAST`))
	Expect(props2).To(ContainSubstring(`queueConfigs."sub2".routingType=MULTICAST`))
}

func testSharedAddressBothConsumerOf() {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app1 := NewBrokerApp("consumer-app1", "test").
		WithSharedAddresses(NewAddressType("queue").Build()).
		WithConsumerOf(NewAddressRef("queue").Build()).
		Build()

	app2 := NewBrokerApp("consumer-app2", "test").
		WithConsumerOf(NewAddressRef("queue").WithAppRef("test", "consumer-app1").Build()).
		Build()

	Expect(reconciler.processCapabilities(secret, app1)).NotTo(HaveOccurred())
	Expect(reconciler.processCapabilities(secret, app2)).NotTo(HaveOccurred())

	props1 := string(secret.Data["test-consumer-app1-capabilities.properties"])
	props2 := string(secret.Data["test-consumer-app2-capabilities.properties"])

	Expect(props1).To(ContainSubstring(`addressConfigurations."queue".routingTypes=ANYCAST`))
	Expect(props2).NotTo(ContainSubstring(`addressConfigurations."queue".routingTypes`))
	Expect(props1).To(ContainSubstring(`queueConfigs."queue".routingType=ANYCAST`))
	Expect(props2).To(ContainSubstring(`queueConfigs."queue".routingType=ANYCAST`))
}
