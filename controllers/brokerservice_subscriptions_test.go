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
	"strings"
	"testing"
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

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have addressConfiguration with routing types
	if !strings.Contains(props, `addressConfigurations."events".routingTypes=`) {
		t.Error("expected addressConfigurations.routingTypes for owned address 'events'")
	}

	// Should contain MULTICAST routing
	if !strings.Contains(props, `MULTICAST`) {
		t.Error("expected MULTICAST routing type for empty queues array")
	}

	// Should NOT have any queueConfigs (no specific queues declared)
	if strings.Contains(props, `queueConfigs`) {
		t.Error("should NOT have queueConfigs for multicast-only address (empty queues array)")
	}

	// Should NOT have RBAC since no capabilities
	if strings.Contains(props, `securityRoles."events"`) {
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

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have addressConfiguration
	if !strings.Contains(props, `addressConfigurations."orders".routingTypes=`) {
		t.Error("expected addressConfigurations.routingTypes for owned address 'orders'")
	}

	// Should contain ANYCAST routing
	if !strings.Contains(props, `ANYCAST`) {
		t.Error("expected ANYCAST routing type for address with queues")
	}

	// Should have queueConfig for the specified queue
	if !strings.Contains(props, `addressConfigurations."orders".queueConfigs."orders"`) {
		t.Error("expected queueConfigs for declared queue 'orders'")
	}

	// Should have routingType ANYCAST for the queue
	if !strings.Contains(props, `queueConfigs."orders".routingType=ANYCAST`) {
		t.Error("expected queueConfigs routingType=ANYCAST for declared queue")
	}

	// Should map queue to address
	if !strings.Contains(props, `queueConfigs."orders".address=orders`) {
		t.Error("expected queueConfigs address mapping for declared queue")
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

	props := string(secret.Data["test-multi-queue-app-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have addressConfiguration
	if !strings.Contains(props, `addressConfigurations."tasks".routingTypes=`) {
		t.Error("expected addressConfigurations.routingTypes for owned address 'tasks'")
	}

	// Should have queueConfigs for all three queues
	queues := []string{"high-priority", "low-priority", "default"}
	for _, queue := range queues {
		if !strings.Contains(props, `queueConfigs."`+queue+`"`) {
			t.Errorf("expected queueConfigs for declared queue '%s'", queue)
		}

		if !strings.Contains(props, `queueConfigs."`+queue+`".routingType=MULTICAST`) {
			t.Errorf("expected routingType=MULTICAST for queue '%s' (subscriptions imply pub/sub)", queue)
		}

		if !strings.Contains(props, `queueConfigs."`+queue+`".address=tasks`) {
			t.Errorf("expected queue '%s' to map to address 'tasks'", queue)
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

	props := string(secret.Data["test-queue-with-caps-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have addressConfiguration
	if !strings.Contains(props, `addressConfigurations."commands".routingTypes=`) {
		t.Error("expected addressConfigurations.routingTypes for owned address 'commands'")
	}

	// Should have queueConfig for the declared queue
	if !strings.Contains(props, `queueConfigs."commands".routingType=ANYCAST`) {
		t.Error("expected queueConfigs for declared queue 'commands'")
	}

	// Should have RBAC for both producer and consumer
	if !strings.Contains(props, `securityRoles."commands"."test-queue-with-caps-producer".send=true`) {
		t.Error("expected producer RBAC role")
	}

	if !strings.Contains(props, `securityRoles."commands"."test-queue-with-caps-consumer".consume=true`) {
		t.Error("expected consumer RBAC role")
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

	props := string(secret.Data["test-inferred-queues-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have addressConfiguration
	if !strings.Contains(props, `addressConfigurations."legacy".routingTypes=`) {
		t.Error("expected addressConfigurations.routingTypes for owned address 'legacy'")
	}

	// Should have queueConfig inferred from ConsumerOf capability
	// Current behavior: creates queue with same name as address
	if !strings.Contains(props, `queueConfigs."legacy"`) {
		t.Error("expected queueConfigs inferred from ConsumerOf capability")
	}

	// Should have RBAC
	if !strings.Contains(props, `securityRoles."legacy"`) {
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

	props := string(secret.Data["test-mixed-routing-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have addressConfigurations for both
	if !strings.Contains(props, `addressConfigurations."events"`) {
		t.Error("expected addressConfigurations for 'events'")
	}
	if !strings.Contains(props, `addressConfigurations."commands"`) {
		t.Error("expected addressConfigurations for 'commands'")
	}

	// Should have queueConfig only for 'commands' (has queues), not 'events' (empty queues)
	if strings.Contains(props, `addressConfigurations."events".queueConfigs`) {
		t.Error("should NOT have queueConfigs for multicast-only address 'events'")
	}

	if !strings.Contains(props, `addressConfigurations."commands".queueConfigs."commands"`) {
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

	props := string(secret.Data["test-subscriber-with-queues-capabilities.properties"])
	t.Logf("PROPS: \n%s\n", props)

	// Should have queueConfigs for both declared queues (email, sms)
	if !strings.Contains(props, `queueConfigs."email"`) {
		t.Error("expected queueConfigs for declared queue 'email'")
	}
	if !strings.Contains(props, `queueConfigs."sms"`) {
		t.Error("expected queueConfigs for declared queue 'sms'")
	}

	// Should also have queueConfig for the FQQN queue from SubscriberOf
	if !strings.Contains(props, `queueConfigs."push"`) {
		t.Error("expected queueConfigs for subscriber FQQN queue 'push'")
	}

	// The FQQN queue should be MULTICAST (from SubscriberOf)
	if !strings.Contains(props, `queueConfigs."push".routingType=MULTICAST`) {
		t.Error("expected MULTICAST routing for subscriber FQQN queue")
	}

	// The declared queues should be ANYCAST (from spec.addresses.queues)
	if !strings.Contains(props, `queueConfigs."email".routingType=MULTICAST`) {
		t.Error("expected MULTICAST routing for declared queue 'email'")
	}
	if !strings.Contains(props, `queueConfigs."sms".routingType=MULTICAST`) {
		t.Error("expected MULTICAST routing for declared queue 'sms'")
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
