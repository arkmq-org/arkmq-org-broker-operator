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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BrokerService capability processing", func() {
	Context("address ownership and referencing", func() {
		It("should mark an address as owned when the app declares it directly", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("owner", "test").
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred())

			props := string(secret.Data["test-owner-capabilities.properties"])

			// Should have addressConfiguration (owned)
			Expect(props).To(ContainSubstring(`addressConfigurations."orders"`))

			// Should have RBAC
			Expect(props).To(ContainSubstring(`securityRoles."orders"`))
		})
		It("should mark an address as referenced when another app owns it", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("consumer", "test").
				WithConsumerOf(NewAddressRef("orders").WithAppRef("other", "owner").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred())

			props := string(secret.Data["test-consumer-capabilities.properties"])

			// Should NOT have addressConfiguration routing types (not owned)
			Expect(props).NotTo(ContainSubstring(`addressConfigurations."orders".routingTypes`))

			// Should have queue configs (needed even for referenced addresses)
			Expect(props).To(ContainSubstring(`addressConfigurations."orders".queueConfigs`))

			// Should still have RBAC
			Expect(props).To(ContainSubstring(`securityRoles."orders"`))
		})
		It("should handle a mix of owned and referenced addresses correctly", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("mixed", "test").
				WithAddresses(NewAddressType("local-queue").Build()).
				WithProducerOf(NewAddressRef("local-queue").Build()).
				WithConsumerOf(NewAddressRef("shared-queue").WithAppRef("other", "owner").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred())

			props := string(secret.Data["test-mixed-capabilities.properties"])

			GinkgoWriter.Printf("PROPS: \n%s\n", props)

			// Should have addressConfiguration routing types for owned address
			Expect(props).To(ContainSubstring(`addressConfigurations."local-queue".routingTypes`))

			// Should NOT have addressConfiguration routing types for referenced address
			Expect(props).NotTo(ContainSubstring(`addressConfigurations."shared-queue".routingTypes`))

			// "local-queue" is ProducerOf only but in addresses, so queue configs expected
			Expect(props).To(ContainSubstring(`addressConfigurations."local-queue".queueConfigs`))

			// "shared-queue" is ConsumerOf, so queue configs expected
			Expect(props).To(ContainSubstring(`addressConfigurations."shared-queue".queueConfigs`))

			// Should have RBAC for both
			Expect(props).To(ContainSubstring(`securityRoles."local-queue"`))
			Expect(props).To(ContainSubstring(`securityRoles."shared-queue"`))
		})

		DescribeTable("addresses registry no capabilities",
			func(useShared bool) {
				reconciler := BrokerServiceInstanceReconcilerForTest()
				secret := CreateSecret("test-secret", "test")

				builder := NewBrokerApp("address-registry", "test")
				if useShared {
					builder.WithSharedAddresses(
						NewAddressType("events").Build(),
						NewAddressType("commands").Build(),
						NewAddressType("queries").Build(),
					)
				} else {
					builder.WithAddresses(
						NewAddressType("events").Build(),
						NewAddressType("commands").Build(),
						NewAddressType("queries").Build(),
					)
				}
				app := builder.Build()

				Expect(reconciler.processCapabilities(secret, app)).NotTo(HaveOccurred())

				props := string(secret.Data["test-address-registry-capabilities.properties"])
				Expect(props).To(ContainSubstring(`addressConfigurations."events"`))
				Expect(props).To(ContainSubstring(`addressConfigurations."commands"`))
				Expect(props).To(ContainSubstring(`addressConfigurations."queries"`))
				Expect(props).NotTo(ContainSubstring(`securityRoles."events"`))
				Expect(props).NotTo(ContainSubstring(`securityRoles."commands"`))
				Expect(props).NotTo(ContainSubstring(`securityRoles."queries"`))
			},
			Entry("non-shared", false),
			Entry("shared", true),
		)

		DescribeTable("spec addresses with capabilities",
			func(useShared bool) {
				reconciler := BrokerServiceInstanceReconcilerForTest()
				secret := CreateSecret("test-secret", "test")

				builder := NewBrokerApp("producer", "test")
				if useShared {
					builder.WithSharedAddresses(
						NewAddressType("events").Build(),
						NewAddressType("commands").Build(),
					)
				} else {
					builder.WithAddresses(
						NewAddressType("events").Build(),
						NewAddressType("commands").Build(),
					)
				}
				app := builder.WithProducerOf(NewAddressRef("events").Build()).
					WithConsumerOf(NewAddressRef("shared-queue").WithAppRef("other", "owner").Build()).
					Build()

				Expect(reconciler.processCapabilities(secret, app)).NotTo(HaveOccurred())

				props := string(secret.Data["test-producer-capabilities.properties"])
				Expect(props).To(ContainSubstring(`addressConfigurations."events"`))
				Expect(props).To(ContainSubstring(`addressConfigurations."commands"`))
				Expect(props).To(ContainSubstring(`securityRoles."events"`))
				Expect(props).To(ContainSubstring(`securityRoles."shared-queue"`))
			},
			Entry("non-shared", false),
			Entry("shared", true),
		)
		It("should generate queue config for a single consumer", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("consumer", "test").
				WithConsumerOf(NewAddressRef("orders").WithAppRef("other", "producer").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred())

			props := string(secret.Data["test-consumer-capabilities.properties"])

			// Should have queue configs even with a single consumer role
			// Current bug: condition is `len(addr.consumerRoles) > 1` which requires 2+ roles
			Expect(props).To(ContainSubstring(`queueConfigs."orders".routingType=ANYCAST`))
			Expect(props).To(ContainSubstring(`queueConfigs."orders".address=orders`))
		})
		It("should generate queue config for a single subscriber", func() {
			reconciler := BrokerServiceInstanceReconcilerForTest()
			secret := CreateSecret("test-secret", "test")

			app := NewBrokerApp("subscriber", "test").
				WithConsumerOf(NewAddressRef("events").WithAppRef("other", "producer").WithSubscriptions("joe").Build()).
				Build()

			err := reconciler.processCapabilities(secret, app)
			Expect(err).NotTo(HaveOccurred())

			props := string(secret.Data["test-subscriber-capabilities.properties"])

			GinkgoWriter.Printf("PROPS: \n%s\n", props)

			// Should have queue configs even with a single subscriber role
			Expect(props).To(ContainSubstring(`queueConfigs."joe".routingType=MULTICAST`))
			Expect(props).To(ContainSubstring(`queueConfigs."joe".address=events`))
		})
	})
})
