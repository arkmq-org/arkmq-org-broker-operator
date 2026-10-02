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
	"encoding/json"
	"fmt"

	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func parseCapabilities(secret *corev1.Secret, appName string) brokerproperties.CapabilitiesJSON {
	key := "test-" + appName + "-capabilities.json"
	data := secret.Data[key]
	Expect(data).NotTo(BeEmpty(), fmt.Sprintf("no capabilities JSON for key %q", key))
	var result brokerproperties.CapabilitiesJSON
	err := json.Unmarshal(data, &result)
	Expect(err).NotTo(HaveOccurred(), "failed to unmarshal capabilities JSON")
	return result
}

func testAddressRegistryNoCapabilities(useShared bool) {
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

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "address-registry")

	Expect(caps.AddressConfigurations).To(HaveKey("events"), "expected addressConfigurations for owned address 'events'")
	Expect(caps.AddressConfigurations).To(HaveKey("commands"), "expected addressConfigurations for owned address 'commands'")
	Expect(caps.AddressConfigurations).To(HaveKey("queries"), "expected addressConfigurations for owned address 'queries'")

	Expect(caps.SecurityRoles).NotTo(HaveKey("events"), "should NOT have securityRoles when app has no capabilities")
	Expect(caps.SecurityRoles).NotTo(HaveKey("commands"), "should NOT have securityRoles when app has no capabilities")
	Expect(caps.SecurityRoles).NotTo(HaveKey("queries"), "should NOT have securityRoles when app has no capabilities")
}

func testSpecAddressesWithCapabilities(useShared bool) {
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
	app := builder.WithProducerOf(NewAddressRef("events").Build()).Build()

	err := reconciler.processCapabilities(secret, app)
	Expect(err).NotTo(HaveOccurred())

	caps := parseCapabilities(secret, "producer")

	Expect(caps.AddressConfigurations).To(HaveKey("events"), "expected addressConfigurations for 'events'")
	Expect(caps.AddressConfigurations).To(HaveKey("commands"), "expected addressConfigurations for 'commands'")

	Expect(caps.SecurityRoles).To(HaveKey("events"), "expected securityRoles for 'events' (used in capabilities)")
	Expect(caps.SecurityRoles).NotTo(HaveKey("commands"), "should NOT have securityRoles for 'commands' (not in capabilities)")
}

var _ = Describe("brokerservice capabilities", func() {

	It("generates config for owned address", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("owner", "test").
			WithAddresses(NewAddressType("orders").Build()).
			WithProducerOf(NewAddressRef("orders").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "owner")

		Expect(caps.AddressConfigurations).To(HaveKey("orders"), "expected addressConfigurations for owned address 'orders'")
		Expect(caps.SecurityRoles).To(HaveKey("orders"), "expected securityRoles for owned address 'orders'")
	})

	It("generates config for referenced address", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("consumer", "test").
			WithConsumerOf(NewAddressRef("orders").WithAppRef("other", "owner").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "consumer")

		ordersAddr := caps.AddressConfigurations["orders"]
		Expect(ordersAddr).NotTo(BeNil(), "expected addressConfigurations entry for 'orders'")

		Expect(ordersAddr.RoutingTypes).To(BeEmpty(), "should NOT have routingTypes for referenced address 'orders'")
		Expect(ordersAddr.QueueConfigs).NotTo(BeEmpty(), "expected queueConfigs for referenced address 'orders'")
		Expect(caps.SecurityRoles).To(HaveKey("orders"), "expected securityRoles for referenced address 'orders'")
	})

	It("handles mixed owned and referenced addresses", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("mixed", "test").
			WithAddresses(NewAddressType("local-queue").Build()).
			WithProducerOf(NewAddressRef("local-queue").Build()).
			WithConsumerOf(NewAddressRef("shared-queue").WithAppRef("other", "owner").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "mixed")

		localAddr := caps.AddressConfigurations["local-queue"]
		Expect(localAddr).NotTo(BeNil(), "expected addressConfigurations for 'local-queue'")
		sharedAddr := caps.AddressConfigurations["shared-queue"]
		Expect(sharedAddr).NotTo(BeNil(), "expected addressConfigurations for 'shared-queue'")

		Expect(localAddr.RoutingTypes).NotTo(BeEmpty(), "expected routingTypes for owned address 'local-queue'")
		Expect(sharedAddr.RoutingTypes).To(BeEmpty(), "should NOT have routingTypes for referenced address 'shared-queue'")

		Expect(localAddr.QueueConfigs).NotTo(BeEmpty(), "should have queueConfigs for producer-only address 'local-queue'")
		Expect(sharedAddr.QueueConfigs).NotTo(BeEmpty(), "expected queueConfigs for referenced consumer address 'shared-queue'")

		Expect(caps.SecurityRoles).To(HaveKey("local-queue"), "expected securityRoles for owned address 'local-queue'")
		Expect(caps.SecurityRoles).To(HaveKey("shared-queue"), "expected securityRoles for referenced address 'shared-queue'")
	})

	It("address registry no capabilities private", Label(unitLabel), func() {
		testAddressRegistryNoCapabilities(false)
	})

	It("address registry no capabilities shared", Label(unitLabel), func() {
		testAddressRegistryNoCapabilities(true)
	})

	It("spec addresses with capabilities private", Label(unitLabel), func() {
		testSpecAddressesWithCapabilities(false)
	})

	It("spec addresses with capabilities shared", Label(unitLabel), func() {
		testSpecAddressesWithCapabilities(true)
	})

	It("generates queue configs for single consumer", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("consumer", "test").
			WithConsumerOf(NewAddressRef("orders").WithAppRef("other", "producer").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "consumer")

		ordersAddr := caps.AddressConfigurations["orders"]
		Expect(ordersAddr).NotTo(BeNil(), "expected addressConfigurations for 'orders'")

		queueCfg := ordersAddr.QueueConfigs["orders"]
		Expect(queueCfg).NotTo(BeNil(), "expected queueConfigs entry for single consumer role")
		Expect(queueCfg.RoutingType).To(Equal(brokerproperties.RoutingTypeAnycast))
		Expect(queueCfg.Address).To(Equal("orders"))
	})

	It("generates queue configs for single subscriber", Label(unitLabel), func() {
		reconciler := BrokerServiceInstanceReconcilerForTest()
		secret := CreateSecret("test-secret", "test")

		app := NewBrokerApp("subscriber", "test").
			WithConsumerOf(NewAddressRef("events").WithAppRef("other", "producer").WithSubscriptions("joe").Build()).
			Build()

		err := reconciler.processCapabilities(secret, app)
		Expect(err).NotTo(HaveOccurred())

		caps := parseCapabilities(secret, "subscriber")

		eventsAddr := caps.AddressConfigurations["events"]
		Expect(eventsAddr).NotTo(BeNil(), "expected addressConfigurations for 'events'")

		joeCfg := eventsAddr.QueueConfigs["joe"]
		Expect(joeCfg).NotTo(BeNil(), "expected queueConfigs entry for single subscriber role")
		Expect(joeCfg.RoutingType).To(Equal(brokerproperties.RoutingTypeMulticast))
		Expect(joeCfg.Address).To(Equal("events"))
	})
})
