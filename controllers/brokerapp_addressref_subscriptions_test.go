package controllers

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BrokerApp address subscription processing", func() {
	Context("subscription routing type derivation", func() {
		It("should derive ANYCAST routing when subscriptions is nil", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("anycast-app", "test").
				WithConsumerOf(NewAddressRef("commands").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred(), "processCapabilities failed")

			props := string(secret.Data["test-anycast-app-capabilities.properties"])
			GinkgoWriter.Printf("PROPS: \n%s\n", props)

			Expect(props).To(ContainSubstring(`addressConfigurations."commands".routingTypes=ANYCAST`))
			Expect(props).To(ContainSubstring(`queueConfigs."commands".routingType=ANYCAST`))

		})
		It("should derive MULTICAST routing when subscription queues are specified", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("multicast-app", "test").
				WithConsumerOf(NewAddressRef("events").WithSubscriptions("sub1", "sub2").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred(), "processCapabilities failed")

			props := string(secret.Data["test-multicast-app-capabilities.properties"])
			GinkgoWriter.Printf("PROPS:\n%s\n", props)

			Expect(props).To(ContainSubstring(`addressConfigurations."events".routingTypes=MULTICAST`))
			Expect(props).To(ContainSubstring(`queueConfigs."sub1".routingType=MULTICAST`))
			Expect(props).To(ContainSubstring(`queueConfigs."sub2".routingType=MULTICAST`))
			Expect(props).To(ContainSubstring(`securityRoles."events\:\:sub1"`))
			Expect(props).To(ContainSubstring(`securityRoles."events\:\:sub2"`))
		})
		It("should produce ANYCAST", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("producer-app", "test").
				WithProducerOf(NewAddressRef("notifications").WithSubscriptions().Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred(), "processCapabilities failed")

			props := string(secret.Data["test-producer-app-capabilities.properties"])
			GinkgoWriter.Printf("PROPS:\n%s\n", props)

			Expect(props).To(ContainSubstring(`addressConfigurations."notifications".routingTypes=ANYCAST`))
			Expect(props).To(ContainSubstring(`queueConfigs."notifications"`))
		})
		It("should detect a routing type conflict within the same app", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("conflict-app", "test").
				WithConsumerOf(
					NewAddressRef("mixed").Build(),                           // nil subscriptions = ANYCAST
					NewAddressRef("mixed").WithSubscriptions("sub1").Build(), // MULTICAST
				).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).To(HaveOccurred(), "expected error for routing type conflict")
			Expect(err.Error()).To(ContainSubstring("conflict"))
			GinkgoWriter.Printf("Correctly rejected conflict: %v", err)
		})
		It("should accept an address list that is exclusively MULTICAST", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("producer-app", "test").
				WithSharedAddresses(NewAddressType("events").WithSubscriptions("sub1").Build()).
				WithProducerOf(v1beta2.AddressRef{Address: "events", PubSub: &[]bool{true}[0]}).Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred(), "processCapabilities failed")

			props := string(secret.Data["test-producer-app-capabilities.properties"])
			GinkgoWriter.Printf("PROPS:\n%s\n", props)

			Expect(props).To(ContainSubstring(`addressConfigurations."events".routingTypes=MULTICAST`))
			Expect(props).NotTo(ContainSubstring(`addressConfigurations."events".routingTypes=ANYCAST`))
		})
	})
})
