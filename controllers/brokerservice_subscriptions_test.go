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

var _ = Describe("BrokerService subscription generation", func() {
	Context("address and queue config generation from subscriptions", func() {
		It("should generate MULTICAST-only config when subscriptions array is empty", func() {
			testEmptySubscriptionsArrayMulticastOnly(false)

		})
		It("should generate MULTICAST-only config when subscriptions array is empty (shared address)", func() {
			testEmptySubscriptionsArrayMulticastOnly(true)

		})
		It("should generate ANYCAST config for a single queue subscription", func() {
			testSingleQueueAnycastRouting(false)

		})
		It("should generate ANYCAST config for a single queue subscription (shared address)", func() {
			testSingleQueueAnycastRouting(true)

		})
		It("should generate config for multiple subscriptions", func() {
			testMultipleSubsAllCreated(false)

		})
		It("should generate config for multiple subscriptions (shared address)", func() {
			testMultipleSubsAllCreated(true)

		})
		It("should generate subscription config including RBAC rules", func() {
			testSubsWithCapabilitiesSubsAndRBAC(false)

		})
		It("should generate subscription config including RBAC rules (shared address)", func() {
			testSubsWithCapabilitiesSubsAndRBAC(true)

		})
		It("should infer address config from capabilities when no explicit queue is specified", func() {
			testNoQueuesFieldInferredFromCapabilities(false)

		})
		It("should infer address config from capabilities when no explicit queue is specified (shared address)", func() {
			testNoQueuesFieldInferredFromCapabilities(true)

		})
		It("should generate config for mixed MULTICAST and ANYCAST addresses", func() {
			testMixedMulticastAndAnycast(false)

		})
		It("should generate config for mixed MULTICAST and ANYCAST addresses (shared address)", func() {
			testMixedMulticastAndAnycast(true)

		})
		It("should generate config for a subscription that includes subscriber capability", func() {
			testSubsWithSubscriberCapability(false)

		})
		It("should generate config for a subscription that includes subscriber capability (shared address)", func() {
			testSubsWithSubscriberCapability(true)

		})
	})
})

// Helper functions for paired tests

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

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."events".routingTypes=`))
	Expect(props).To(ContainSubstring(`MULTICAST`))
	Expect(props).NotTo(ContainSubstring(`queueConfigs`))
	Expect(props).NotTo(ContainSubstring(`securityRoles."events"`))
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

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."orders".routingTypes=`))
	Expect(props).To(ContainSubstring(`ANYCAST`))
	Expect(props).To(ContainSubstring(`addressConfigurations."orders".queueConfigs."orders"`))
	Expect(props).To(ContainSubstring(`queueConfigs."orders".routingType=ANYCAST`))
	Expect(props).To(ContainSubstring(`queueConfigs."orders".address=orders`))
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

	props := string(secret.Data["test-multi-queue-app-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."tasks".routingTypes=`))

	queues := []string{"high-priority", "low-priority", "default"}
	for _, queue := range queues {
		Expect(props).To(ContainSubstring(`queueConfigs."` + queue + `"`))
		Expect(props).To(ContainSubstring(`queueConfigs."` + queue + `".routingType=MULTICAST`))
		Expect(props).To(ContainSubstring(`queueConfigs."` + queue + `".address=tasks`))
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

	props := string(secret.Data["test-queue-with-caps-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."commands".routingTypes=`))
	Expect(props).To(ContainSubstring(`queueConfigs."commands".routingType=ANYCAST`))
	Expect(props).To(ContainSubstring(`securityRoles."commands"."test-queue-with-caps-producer".send=true`))
	Expect(props).To(ContainSubstring(`securityRoles."commands"."test-queue-with-caps-consumer".consume=true`))
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

	props := string(secret.Data["test-inferred-queues-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."legacy".routingTypes=`))
	Expect(props).To(ContainSubstring(`queueConfigs."legacy"`))
	Expect(props).To(ContainSubstring(`securityRoles."legacy"`))
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

	props := string(secret.Data["test-mixed-routing-capabilities.properties"])
	Expect(props).To(ContainSubstring(`addressConfigurations."events"`))
	Expect(props).To(ContainSubstring(`addressConfigurations."commands"`))
	Expect(props).NotTo(ContainSubstring(`addressConfigurations."events".queueConfigs`))
	Expect(props).To(ContainSubstring(`addressConfigurations."commands".queueConfigs."commands"`))
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

	props := string(secret.Data["test-subscriber-with-queues-capabilities.properties"])
	Expect(props).To(ContainSubstring(`queueConfigs."email"`))
	Expect(props).To(ContainSubstring(`queueConfigs."sms"`))
	Expect(props).To(ContainSubstring(`queueConfigs."push"`))
	Expect(props).To(ContainSubstring(`queueConfigs."push".routingType=MULTICAST`))
	Expect(props).To(ContainSubstring(`queueConfigs."email".routingType=MULTICAST`))
	Expect(props).To(ContainSubstring(`queueConfigs."sms".routingType=MULTICAST`))
}
