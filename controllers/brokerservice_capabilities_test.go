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
	"testing"

	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	corev1 "k8s.io/api/core/v1"
)

func parseCapabilities(t *testing.T, secret *corev1.Secret, appName string) brokerproperties.CapabilitiesJSON {
	t.Helper()
	key := "test-" + appName + "-capabilities.json"
	data := secret.Data[key]
	if len(data) == 0 {
		t.Fatalf("no capabilities JSON for key %q", key)
	}
	var result brokerproperties.CapabilitiesJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal capabilities JSON: %v", err)
	}
	return result
}

// Helper functions for paired tests

func testAddressRegistryNoCapabilities(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "address-registry")

	// Should have addressConfigurations for all declared addresses (since they're owned)
	if _, ok := caps.AddressConfigurations["events"]; !ok {
		t.Error("expected addressConfigurations for owned address 'events'")
	}
	if _, ok := caps.AddressConfigurations["commands"]; !ok {
		t.Error("expected addressConfigurations for owned address 'commands'")
	}
	if _, ok := caps.AddressConfigurations["queries"]; !ok {
		t.Error("expected addressConfigurations for owned address 'queries'")
	}

	// Should NOT have securityRoles (no capabilities = no RBAC)
	if _, ok := caps.SecurityRoles["events"]; ok {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
	if _, ok := caps.SecurityRoles["commands"]; ok {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
	if _, ok := caps.SecurityRoles["queries"]; ok {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
}

func testSpecAddressesWithCapabilities(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "producer")

	// Should have addressConfigurations for both addresses (both are in spec.addresses or spec.sharedAddresses)
	if _, ok := caps.AddressConfigurations["events"]; !ok {
		t.Error("expected addressConfigurations for 'events'")
	}
	if _, ok := caps.AddressConfigurations["commands"]; !ok {
		t.Error("expected addressConfigurations for 'commands'")
	}

	// Should have RBAC only for addresses used in capabilities
	if _, ok := caps.SecurityRoles["events"]; !ok {
		t.Error("expected securityRoles for 'events' (used in capabilities)")
	}
	if _, ok := caps.SecurityRoles["commands"]; ok {
		t.Error("should NOT have securityRoles for 'commands' (not in capabilities)")
	}
}

func TestProcessCapabilities_OwnedAddress(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("owner", "test").
		WithAddresses(NewAddressType("orders").Build()).
		WithProducerOf(NewAddressRef("orders").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "owner")

	// Should have addressConfiguration (owned)
	if _, ok := caps.AddressConfigurations["orders"]; !ok {
		t.Error("expected addressConfigurations for owned address 'orders'")
	}

	// Should have RBAC
	if _, ok := caps.SecurityRoles["orders"]; !ok {
		t.Error("expected securityRoles for owned address 'orders'")
	}
}

func TestProcessCapabilities_ReferencedAddress(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("consumer", "test").
		WithConsumerOf(NewAddressRef("orders").WithAppRef("other", "owner").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "consumer")

	ordersAddr := caps.AddressConfigurations["orders"]
	if ordersAddr == nil {
		t.Fatal("expected addressConfigurations entry for 'orders'")
	}

	// Should NOT have routingTypes (not owned)
	if ordersAddr.RoutingTypes != "" {
		t.Error("should NOT have routingTypes for referenced address 'orders'")
	}

	// Should have queue configs (needed even for referenced addresses)
	if len(ordersAddr.QueueConfigs) == 0 {
		t.Error("expected queueConfigs for referenced address 'orders'")
	}

	// Should still have RBAC
	if _, ok := caps.SecurityRoles["orders"]; !ok {
		t.Error("expected securityRoles for referenced address 'orders'")
	}
}

func TestProcessCapabilities_MixedOwnedAndReferenced(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("mixed", "test").
		WithAddresses(NewAddressType("local-queue").Build()).
		WithProducerOf(NewAddressRef("local-queue").Build()).
		WithConsumerOf(NewAddressRef("shared-queue").WithAppRef("other", "owner").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "mixed")

	localAddr := caps.AddressConfigurations["local-queue"]
	if localAddr == nil {
		t.Fatal("expected addressConfigurations for 'local-queue'")
	}
	sharedAddr := caps.AddressConfigurations["shared-queue"]
	if sharedAddr == nil {
		t.Fatal("expected addressConfigurations for 'shared-queue'")
	}

	// Should have routingTypes for owned address
	if localAddr.RoutingTypes == "" {
		t.Error("expected routingTypes for owned address 'local-queue'")
	}

	// Should NOT have routingTypes for referenced address
	if sharedAddr.RoutingTypes != "" {
		t.Error("should NOT have routingTypes for referenced address 'shared-queue'")
	}

	// "local-queue" is ProducerOf only but in addresses, so queue configs expected
	if len(localAddr.QueueConfigs) == 0 {
		t.Error("should have queueConfigs for producer-only address 'local-queue'")
	}
	// "shared-queue" is ConsumerOf, so queue configs expected
	if len(sharedAddr.QueueConfigs) == 0 {
		t.Error("expected queueConfigs for referenced consumer address 'shared-queue'")
	}

	// Should have RBAC for both
	if _, ok := caps.SecurityRoles["local-queue"]; !ok {
		t.Error("expected securityRoles for owned address 'local-queue'")
	}
	if _, ok := caps.SecurityRoles["shared-queue"]; !ok {
		t.Error("expected securityRoles for referenced address 'shared-queue'")
	}
}

func TestProcessCapabilities_AddressRegistryNoCapabilities(t *testing.T) {
	testAddressRegistryNoCapabilities(t, false)
}

func TestProcessCapabilities_AddressRegistryNoCapabilities_Shared(t *testing.T) {
	testAddressRegistryNoCapabilities(t, true)
}

func TestProcessCapabilities_SpecAddressesWithCapabilities(t *testing.T) {
	testSpecAddressesWithCapabilities(t, false)
}

func TestProcessCapabilities_SpecAddressesWithCapabilities_Shared(t *testing.T) {
	testSpecAddressesWithCapabilities(t, true)
}

func TestProcessCapabilities_QueueConfigsForSingleConsumer(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("consumer", "test").
		WithConsumerOf(NewAddressRef("orders").WithAppRef("other", "producer").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "consumer")

	ordersAddr := caps.AddressConfigurations["orders"]
	if ordersAddr == nil {
		t.Fatal("expected addressConfigurations for 'orders'")
	}

	queueCfg := ordersAddr.QueueConfigs["orders"]
	if queueCfg == nil {
		t.Error("expected queueConfigs entry for single consumer role")
	} else {
		if queueCfg.RoutingType != brokerproperties.RoutingTypeAnycast {
			t.Errorf("expected ANYCAST routingType, got %s", queueCfg.RoutingType)
		}
		if queueCfg.Address != "orders" {
			t.Errorf("expected address=orders, got %s", queueCfg.Address)
		}
	}
}

func TestProcessCapabilities_QueueConfigsForSingleSubscriber(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("subscriber", "test").
		WithConsumerOf(NewAddressRef("events").WithAppRef("other", "producer").WithSubscriptions("joe").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "subscriber")

	eventsAddr := caps.AddressConfigurations["events"]
	if eventsAddr == nil {
		t.Fatal("expected addressConfigurations for 'events'")
	}

	joeCfg := eventsAddr.QueueConfigs["joe"]
	if joeCfg == nil {
		t.Error("expected queueConfigs entry for single subscriber role")
	} else {
		if joeCfg.RoutingType != brokerproperties.RoutingTypeMulticast {
			t.Errorf("expected MULTICAST routingType, got %s", joeCfg.RoutingType)
		}
		if joeCfg.Address != "events" {
			t.Errorf("expected address=events, got %s", joeCfg.Address)
		}
	}
}
