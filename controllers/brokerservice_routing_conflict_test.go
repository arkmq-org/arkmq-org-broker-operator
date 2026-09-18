package controllers

import (
	"strings"
	"testing"

	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
)

// Helper functions for paired tests

func testMulticastRoutingForSubscriptions(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "multicast-app")

	eventsAddr := caps.AddressConfigurations["events"]
	if eventsAddr == nil {
		t.Fatal("expected addressConfigurations for 'events'")
	}

	// Should use MULTICAST routing type (NOT ANYCAST) for subscription address
	if eventsAddr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Errorf("expected routingTypes=MULTICAST for subscription address, got %q", eventsAddr.RoutingTypes)
	}
}

func testAnycastRoutingForConsumerOf(t *testing.T, useShared bool) {
	t.Helper()
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
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	caps := parseCapabilities(t, secret, "anycast-app")

	commandsAddr := caps.AddressConfigurations["commands"]
	if commandsAddr == nil {
		t.Fatal("expected addressConfigurations for 'commands'")
	}

	// Should use ANYCAST routing type (NOT MULTICAST) for consumerOf address
	if commandsAddr.RoutingTypes != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected routingTypes=ANYCAST for consumerOf address, got %q", commandsAddr.RoutingTypes)
	}
}

func testConflictingRoutingTypesSameApp(t *testing.T, useShared bool) {
	t.Helper()
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
	if err == nil {
		t.Fatal("processCapabilities should have failed with routing type conflict")
	}

	// Verify the error message mentions the conflict
	expectedKeywords := []string{"mixed", "pubSub", "conflict"}
	errMsg := err.Error()
	for _, keyword := range expectedKeywords {
		if !strings.Contains(errMsg, keyword) {
			t.Errorf("error message should contain '%s', got: %s", keyword, errMsg)
		}
	}

	t.Logf("Correctly rejected same-app routing conflict: %v", err)
}

// TestProcessCapabilities_MulticastRoutingForSubscriptions tests that subscription addresses use MULTICAST routing
func TestProcessCapabilities_MulticastRoutingForSubscriptions(t *testing.T) {
	testMulticastRoutingForSubscriptions(t, true)
}

// TestProcessCapabilities_MulticastRoutingForSubscriptions_Private tests MULTICAST routing with private addresses
func TestProcessCapabilities_MulticastRoutingForSubscriptions_Private(t *testing.T) {
	testMulticastRoutingForSubscriptions(t, false)
}

// TestProcessCapabilities_AnycastRoutingForConsumerOf tests that consumerOf addresses use ANYCAST routing
func TestProcessCapabilities_AnycastRoutingForConsumerOf(t *testing.T) {
	testAnycastRoutingForConsumerOf(t, true)
}

// TestProcessCapabilities_AnycastRoutingForConsumerOf_Private tests ANYCAST routing with private addresses
func TestProcessCapabilities_AnycastRoutingForConsumerOf_Private(t *testing.T) {
	testAnycastRoutingForConsumerOf(t, false)
}

// TestProcessCapabilities_ConflictingRoutingTypes_SameApp tests that an address cannot be used with both
// Subscriptions (MULTICAST) and ConsumerOf (ANYCAST) in the same app
func TestProcessCapabilities_ConflictingRoutingTypes_SameApp(t *testing.T) {
	testConflictingRoutingTypesSameApp(t, true)
}

// TestProcessCapabilities_ConflictingRoutingTypes_SameApp_Private tests conflict detection with private addresses
func TestProcessCapabilities_ConflictingRoutingTypes_SameApp_Private(t *testing.T) {
	testConflictingRoutingTypesSameApp(t, false)
}

// TestProcessCapabilities_ConflictingRoutingTypes_MultipleApps tests the multi-app conflict scenario
func TestProcessCapabilities_ConflictingRoutingTypes_MultipleApps(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	// App 1: Producer with Subscriptions (MULTICAST)
	app1 := NewBrokerApp("producer-app", "test").
		WithSharedAddresses(NewAddressType("shared-events").Build()).
		WithProducerOf(NewAddressRef("shared-events").Build()).
		WithConsumerOf(NewAddressRef("shared-events").WithSubscriptions("producer-sub").Build()).
		Build()

	// App 2: Consumer with ConsumerOf (ANYCAST)
	app2 := NewBrokerApp("consumer-app", "test").
		WithConsumerOf(NewAddressRef("shared-events").WithAppRef("test", "producer-app").Build()).
		Build()

	// Process app1 first
	err := reconciler.processCapabilities(secret, app1)
	if err != nil {
		t.Fatalf("processCapabilities for app1 failed: %v", err)
	}

	// Process app2 (this should detect the conflict)
	err = reconciler.processCapabilities(secret, app2)
	if err != nil {
		t.Fatalf("processCapabilities for app2 failed: %v", err)
	}

	caps1 := parseCapabilities(t, secret, "producer-app")
	caps2 := parseCapabilities(t, secret, "consumer-app")

	// App1 should have MULTICAST routing for shared-events
	if addr := caps1.AddressConfigurations["shared-events"]; addr == nil || addr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Error("app1 should have MULTICAST routing for shared-events")
	}

	// App2 should NOT generate routingTypes (not owned)
	if addr := caps2.AddressConfigurations["shared-events"]; addr != nil && addr.RoutingTypes != "" {
		t.Error("app2 should NOT generate routingTypes for cross-app address")
	}

	// NOTE: Cross-app routing conflicts are detected at BrokerApp validation time (in brokerapp_controller),
	// not during capability processing. See TestRoutingTypeConflictValidation in brokerapp_controller_unit_test.go
	// for proper cross-app conflict validation tests.
	//
	// At this level (processCapabilities), app2 successfully generates its ANYCAST queue config,
	// but the BrokerApp reconciler would reject app2's spec during validation before it gets deployed.
	addr2 := caps2.AddressConfigurations["shared-events"]
	if addr2 == nil || addr2.QueueConfigs["shared-events"] == nil || addr2.QueueConfigs["shared-events"].RoutingType != brokerproperties.RoutingTypeAnycast {
		t.Error("app2 should generate ANYCAST queue config (conflict detected at validation time, not here)")
	}
}

// TestProcessCapabilities_SharedAddress_BothSubscriptions tests that two apps can share an address
// if BOTH use Subscriptions (both MULTICAST)
func TestProcessCapabilities_SharedAddress_BothSubscriptions(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	// App 1: Subscriptions
	app1 := NewBrokerApp("sub-app1", "test").
		WithSharedAddresses(NewAddressType("topic").Build()).
		WithConsumerOf(NewAddressRef("topic").WithSubscriptions("sub1").Build()).
		Build()

	// App 2: Also Subscriptions (compatible)
	app2 := NewBrokerApp("sub-app2", "test").
		WithConsumerOf(NewAddressRef("topic").WithAppRef("test", "sub-app1").WithSubscriptions("sub2").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app1)
	if err != nil {
		t.Fatalf("processCapabilities for app1 failed: %v", err)
	}
	err = reconciler.processCapabilities(secret, app2)
	if err != nil {
		t.Fatalf("processCapabilities for app2 failed: %v", err)
	}

	caps1 := parseCapabilities(t, secret, "sub-app1")
	caps2 := parseCapabilities(t, secret, "sub-app2")

	// Both should have MULTICAST routing (compatible)
	if addr := caps1.AddressConfigurations["topic"]; addr == nil || addr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Error("app1 should have MULTICAST routing")
	}

	// App2 doesn't own the address, so no routingTypes
	if addr := caps2.AddressConfigurations["topic"]; addr != nil && addr.RoutingTypes != "" {
		t.Error("app2 should NOT generate routingTypes for cross-app address")
	}

	// Both should have their subscription queues under the "topic" address entry
	if addr := caps1.AddressConfigurations["topic"]; addr == nil || addr.QueueConfigs["sub1"] == nil || addr.QueueConfigs["sub1"].RoutingType != brokerproperties.RoutingTypeMulticast {
		t.Error("app1 should have MULTICAST queue sub1")
	}
	if addr := caps2.AddressConfigurations["topic"]; addr == nil || addr.QueueConfigs["sub2"] == nil || addr.QueueConfigs["sub2"].RoutingType != brokerproperties.RoutingTypeMulticast {
		t.Error("app2 should have MULTICAST queue sub2")
	}
}

// TestProcessCapabilities_SharedAddress_BothConsumerOf tests that two apps can share an address
// if BOTH use ConsumerOf (both ANYCAST)
func TestProcessCapabilities_SharedAddress_BothConsumerOf(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	// App 1: ConsumerOf
	app1 := NewBrokerApp("consumer-app1", "test").
		WithSharedAddresses(NewAddressType("queue").Build()).
		WithConsumerOf(NewAddressRef("queue").Build()).
		Build()

	// App 2: Also ConsumerOf (compatible)
	app2 := NewBrokerApp("consumer-app2", "test").
		WithConsumerOf(NewAddressRef("queue").WithAppRef("test", "consumer-app1").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app1)
	if err != nil {
		t.Fatalf("processCapabilities for app1 failed: %v", err)
	}
	err = reconciler.processCapabilities(secret, app2)
	if err != nil {
		t.Fatalf("processCapabilities for app2 failed: %v", err)
	}

	caps1 := parseCapabilities(t, secret, "consumer-app1")
	caps2 := parseCapabilities(t, secret, "consumer-app2")

	// Both should have ANYCAST routing (compatible)
	if addr := caps1.AddressConfigurations["queue"]; addr == nil || addr.RoutingTypes != brokerproperties.RoutingTypeAnycast {
		t.Error("app1 should have ANYCAST routing")
	}

	// App2 doesn't own the address
	if addr := caps2.AddressConfigurations["queue"]; addr != nil && addr.RoutingTypes != "" {
		t.Error("app2 should NOT generate routingTypes for cross-app address")
	}

	// Both should have ANYCAST queues
	if addr := caps1.AddressConfigurations["queue"]; addr == nil || addr.QueueConfigs["queue"] == nil || addr.QueueConfigs["queue"].RoutingType != brokerproperties.RoutingTypeAnycast {
		t.Error("app1 should have ANYCAST queue")
	}
	if addr := caps2.AddressConfigurations["queue"]; addr == nil || addr.QueueConfigs["queue"] == nil || addr.QueueConfigs["queue"].RoutingType != brokerproperties.RoutingTypeAnycast {
		t.Error("app2 should have ANYCAST queue")
	}
}
