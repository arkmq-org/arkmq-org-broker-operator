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
	"testing"

	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
)

// Helper functions for paired tests

func testEmptySubscriptionsArrayMulticastOnly(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "multicast-app")

	eventsAddr := caps.AddressConfigurations["events"]
	if eventsAddr == nil {
		t.Fatal("expected addressConfigurations for owned address 'events'")
	}

	if eventsAddr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Errorf("expected MULTICAST routing type for empty queues array, got %q", eventsAddr.RoutingTypes)
	}

	// Should NOT have any queueConfigs (no specific queues declared for multicast-only)
	if len(eventsAddr.QueueConfigs) > 0 {
		t.Error("should NOT have queueConfigs for multicast-only address (empty queues array)")
	}

	// Should NOT have RBAC since no capabilities
	if _, ok := caps.SecurityRoles["events"]; ok {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
}

func testSingleQueueAnycastRouting(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "anycast-app")

	ordersAddr := caps.AddressConfigurations["orders"]
	if ordersAddr == nil {
		t.Fatal("expected addressConfigurations for owned address 'orders'")
	}

	if ordersAddr.RoutingTypes != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected ANYCAST routing type for address with queues, got %q", ordersAddr.RoutingTypes)
	}

	queueCfg := ordersAddr.QueueConfigs["orders"]
	if queueCfg == nil {
		t.Fatal("expected queueConfigs for declared queue 'orders'")
	}
	if queueCfg.RoutingType != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected queueConfigs routingType=ANYCAST for declared queue, got %q", queueCfg.RoutingType)
	}
	if queueCfg.Address != "orders" {
		t.Errorf("expected queueConfigs address=orders, got %q", queueCfg.Address)
	}
}

func testMultipleSubsAllCreated(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "multi-queue-app")

	tasksAddr := caps.AddressConfigurations["tasks"]
	if tasksAddr == nil {
		t.Fatal("expected addressConfigurations for owned address 'tasks'")
	}

	if tasksAddr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Errorf("expected MULTICAST routing for subscription address, got %q", tasksAddr.RoutingTypes)
	}

	for _, queueName := range []string{"high-priority", "low-priority", "default"} {
		queueCfg := tasksAddr.QueueConfigs[queueName]
		if queueCfg == nil {
			t.Errorf("expected queueConfigs for declared queue %q", queueName)
			continue
		}
		if queueCfg.RoutingType != brokerproperties.RoutingTypeMulticast {
			t.Errorf("expected routingType=MULTICAST for queue %q (subscriptions imply pub/sub), got %q", queueName, queueCfg.RoutingType)
		}
		if queueCfg.Address != "tasks" {
			t.Errorf("expected queue %q to map to address 'tasks', got %q", queueName, queueCfg.Address)
		}
	}
}

func testSubsWithCapabilitiesSubsAndRBAC(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "queue-with-caps")

	commandsAddr := caps.AddressConfigurations["commands"]
	if commandsAddr == nil {
		t.Fatal("expected addressConfigurations for 'commands'")
	}
	if commandsAddr.RoutingTypes != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected ANYCAST routing, got %q", commandsAddr.RoutingTypes)
	}
	if commandsAddr.QueueConfigs["commands"] == nil || commandsAddr.QueueConfigs["commands"].RoutingType != brokerproperties.RoutingTypeAnycast {
		t.Error("expected queueConfigs for declared queue 'commands' with ANYCAST routing")
	}

	commandsRoles := caps.SecurityRoles["commands"]
	if commandsRoles == nil {
		t.Fatal("expected securityRoles for 'commands'")
	}
	if p := commandsRoles["test-queue-with-caps-producer"]; p == nil || !p.Send {
		t.Error("expected producer RBAC role with send=true")
	}
	if p := commandsRoles["test-queue-with-caps-consumer"]; p == nil || !p.Consume {
		t.Error("expected consumer RBAC role with consume=true")
	}
}

func testNoQueuesFieldInferredFromCapabilities(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "inferred-queues")

	legacyAddr := caps.AddressConfigurations["legacy"]
	if legacyAddr == nil {
		t.Fatal("expected addressConfigurations for owned address 'legacy'")
	}
	if legacyAddr.RoutingTypes == "" {
		t.Error("expected routingTypes for owned address 'legacy'")
	}
	if legacyAddr.QueueConfigs["legacy"] == nil {
		t.Error("expected queueConfigs inferred from ConsumerOf capability")
	}
	if _, ok := caps.SecurityRoles["legacy"]; !ok {
		t.Error("expected securityRoles from capabilities")
	}
}

func testMixedMulticastAndAnycast(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "mixed-routing")

	if caps.AddressConfigurations["events"] == nil {
		t.Error("expected addressConfigurations for 'events'")
	}
	if caps.AddressConfigurations["commands"] == nil {
		t.Error("expected addressConfigurations for 'commands'")
	}

	// Should have queueConfig only for 'commands' (anycast), not 'events' (multicast-only)
	if len(caps.AddressConfigurations["events"].QueueConfigs) > 0 {
		t.Error("should NOT have queueConfigs for multicast-only address 'events'")
	}
	if caps.AddressConfigurations["commands"].QueueConfigs["commands"] == nil {
		t.Error("expected queueConfigs for anycast address 'commands'")
	}
}

func testSubsWithSubscriberCapability(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "subscriber-with-queues")

	notifAddr := caps.AddressConfigurations["notifications"]
	if notifAddr == nil {
		t.Fatal("expected addressConfigurations for 'notifications'")
	}

	for _, queueName := range []string{"email", "sms", "push"} {
		queueCfg := notifAddr.QueueConfigs[queueName]
		if queueCfg == nil {
			t.Errorf("expected queueConfigs for queue %q", queueName)
			continue
		}
		if queueCfg.RoutingType != brokerproperties.RoutingTypeMulticast {
			t.Errorf("expected MULTICAST routing for queue %q, got %q", queueName, queueCfg.RoutingType)
		}
	}
}

func TestProcessCapabilities_EmptySubscriptionsArray_MulticastOnly(t *testing.T) {
	testEmptySubscriptionsArrayMulticastOnly(t, false)
}

func TestProcessCapabilities_EmptySubscriptionsArray_MulticastOnly_Shared(t *testing.T) {
	testEmptySubscriptionsArrayMulticastOnly(t, true)
}

func TestProcessCapabilities_SingleQueue_AnycastRouting(t *testing.T) {
	testSingleQueueAnycastRouting(t, false)
}

func TestProcessCapabilities_SingleQueue_AnycastRouting_Shared(t *testing.T) {
	testSingleQueueAnycastRouting(t, true)
}

func TestProcessCapabilities_MultipleSubs_AllCreated(t *testing.T) {
	testMultipleSubsAllCreated(t, false)
}

func TestProcessCapabilities_MultipleSubs_AllCreated_Shared(t *testing.T) {
	testMultipleSubsAllCreated(t, true)
}

func TestProcessCapabilities_SubsWithCapabilities_SubsAndRBAC(t *testing.T) {
	testSubsWithCapabilitiesSubsAndRBAC(t, false)
}

func TestProcessCapabilities_SubsWithCapabilities_SubsAndRBAC_Shared(t *testing.T) {
	testSubsWithCapabilitiesSubsAndRBAC(t, true)
}

func TestProcessCapabilities_NoQueuesField_InferredFromCapabilities(t *testing.T) {
	testNoQueuesFieldInferredFromCapabilities(t, false)
}

func TestProcessCapabilities_NoQueuesField_InferredFromCapabilities_Shared(t *testing.T) {
	testNoQueuesFieldInferredFromCapabilities(t, true)
}

func TestProcessCapabilities_MixedMulticastAndAnycast(t *testing.T) {
	testMixedMulticastAndAnycast(t, false)
}

func TestProcessCapabilities_MixedMulticastAndAnycast_Shared(t *testing.T) {
	testMixedMulticastAndAnycast(t, true)
}

func TestProcessCapabilities_SubsWithSubscriberCapability(t *testing.T) {
	testSubsWithSubscriberCapability(t, false)
}

func TestProcessCapabilities_SubsWithSubscriberCapability_Shared(t *testing.T) {
	testSubsWithSubscriberCapability(t, true)
}
