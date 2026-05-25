package controllers

import (
	"strings"
	"testing"
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

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should use MULTICAST routing type (NOT ANYCAST) for subscription address
	if !strings.Contains(props, `addressConfigurations."events".routingTypes=MULTICAST`) {
		t.Error("expected routingTypes=MULTICAST for subscription address")
	}

	if strings.Contains(props, `addressConfigurations."events".routingTypes=ANYCAST`) {
		t.Error("should NOT have routingTypes=ANYCAST for subscription address")
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

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should use ANYCAST routing type (NOT MULTICAST) for consumerOf address
	if !strings.Contains(props, `addressConfigurations."commands".routingTypes=ANYCAST`) {
		t.Error("expected routingTypes=ANYCAST for consumerOf address")
	}

	if strings.Contains(props, `addressConfigurations."commands".routingTypes=MULTICAST`) {
		t.Error("should NOT have routingTypes=MULTICAST for consumerOf address")
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

	props1 := string(secret.Data["test-producer-app-capabilities.properties"])
	props2 := string(secret.Data["test-consumer-app-capabilities.properties"])

	t.Logf("APP1 PROPS:\n%s\n", props1)
	t.Logf("APP2 PROPS:\n%s\n", props2)

	// App1 should have MULTICAST routing for shared-events
	if !strings.Contains(props1, `addressConfigurations."shared-events".routingTypes=MULTICAST`) {
		t.Error("app1 should have MULTICAST routing for shared-events")
	}

	// App2 should NOT generate routingTypes (not owned)
	if strings.Contains(props2, `addressConfigurations."shared-events".routingTypes`) {
		t.Error("app2 should NOT generate routingTypes for cross-app address")
	}

	// NOTE: Cross-app routing conflicts are detected at BrokerApp validation time (in brokerapp_controller),
	// not during capability processing. See TestRoutingTypeConflictValidation in brokerapp_controller_unit_test.go
	// for proper cross-app conflict validation tests.
	//
	// At this level (processCapabilities), app2 successfully generates its ANYCAST queue config,
	// but the BrokerApp reconciler would reject app2's spec during validation before it gets deployed.
	if !strings.Contains(props2, `queueConfigs."shared-events".routingType=ANYCAST`) {
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

	props1 := string(secret.Data["test-sub-app1-capabilities.properties"])
	props2 := string(secret.Data["test-sub-app2-capabilities.properties"])

	t.Logf("APP1 PROPS:\n%s\n", props1)
	t.Logf("APP2 PROPS:\n%s\n", props2)

	// Both should have MULTICAST routing (compatible)
	if !strings.Contains(props1, `addressConfigurations."topic".routingTypes=MULTICAST`) {
		t.Error("app1 should have MULTICAST routing")
	}

	// App2 doesn't own the address, so no routingTypes
	if strings.Contains(props2, `addressConfigurations."topic".routingTypes`) {
		t.Error("app2 should NOT generate routingTypes for cross-app address")
	}

	// But both should have their subscription queues
	if !strings.Contains(props1, `queueConfigs."sub1".routingType=MULTICAST`) {
		t.Error("app1 should have MULTICAST queue sub1")
	}

	if !strings.Contains(props2, `queueConfigs."sub2".routingType=MULTICAST`) {
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

	props1 := string(secret.Data["test-consumer-app1-capabilities.properties"])
	props2 := string(secret.Data["test-consumer-app2-capabilities.properties"])

	t.Logf("APP1 PROPS:\n%s\n", props1)
	t.Logf("APP2 PROPS:\n%s\n", props2)

	// Both should have ANYCAST routing (compatible)
	if !strings.Contains(props1, `addressConfigurations."queue".routingTypes=ANYCAST`) {
		t.Error("app1 should have ANYCAST routing")
	}

	// App2 doesn't own the address
	if strings.Contains(props2, `addressConfigurations."queue".routingTypes`) {
		t.Error("app2 should NOT generate routingTypes for cross-app address")
	}

	// Both should have ANYCAST queues
	if !strings.Contains(props1, `queueConfigs."queue".routingType=ANYCAST`) {
		t.Error("app1 should have ANYCAST queue")
	}

	if !strings.Contains(props2, `queueConfigs."queue".routingType=ANYCAST`) {
		t.Error("app2 should have ANYCAST queue")
	}
}
