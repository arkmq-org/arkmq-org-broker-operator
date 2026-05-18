package controllers

import (
	"strings"
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
)

// TestProcessCapabilities_AddressRefSubscriptions_ANYCAST tests ANYCAST queue (nil subscriptions)
func TestProcessCapabilities_AddressRefSubscriptions_ANYCAST(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("anycast-app", "test").
		WithConsumerOf(NewAddressRef("commands").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have ANYCAST routing
	if !strings.Contains(props, `addressConfigurations."commands".routingTypes=ANYCAST`) {
		t.Error("expected routingTypes=ANYCAST for nil subscriptions")
	}

	// Should have ANYCAST queue
	if !strings.Contains(props, `queueConfigs."commands".routingType=ANYCAST`) {
		t.Error("expected ANYCAST queue config")
	}
}

// TestProcessCapabilities_AddressRefSubscriptions_MULTICAST tests MULTICAST topic with subscription queues
func TestProcessCapabilities_AddressRefSubscriptions_MULTICAST(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("multicast-app", "test").
		WithConsumerOf(NewAddressRef("events").WithSubscriptions("sub1", "sub2").Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have MULTICAST routing
	if !strings.Contains(props, `addressConfigurations."events".routingTypes=MULTICAST`) {
		t.Error("expected routingTypes=MULTICAST for subscriptions array")
	}

	// Should have MULTICAST subscription queues
	if !strings.Contains(props, `queueConfigs."sub1".routingType=MULTICAST`) {
		t.Error("expected MULTICAST queue sub1")
	}

	if !strings.Contains(props, `queueConfigs."sub2".routingType=MULTICAST`) {
		t.Error("expected MULTICAST queue sub2")
	}

	// Should have subscriber roles for FQQN
	if !strings.Contains(props, `securityRoles."events\:\:sub1"`) {
		t.Error("expected subscriber role for events::sub1")
	}

	if !strings.Contains(props, `securityRoles."events\:\:sub2"`) {
		t.Error("expected subscriber role for events::sub2")
	}
}

func TestProcessCapabilities_AddressRefEmptySubscriptions_ProducerANYCAST(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("producer-app", "test").
		WithProducerOf(NewAddressRef("notifications").WithSubscriptions().Build()).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-producer-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have ANYCAST routing, empty subs omitted
	if !strings.Contains(props, `addressConfigurations."notifications".routingTypes=ANYCAST`) {
		t.Error("expected routingTypes=ANYCAST for empty subscriptions array in ProducerOf")
	}

	if !strings.Contains(props, `queueConfigs."notifications"`) {
		t.Error("producer should create queue configs")
	}
}

// TestProcessCapabilities_AddressRefSubscriptions_Conflict tests same-app routing conflict
func TestProcessCapabilities_AddressRefSubscriptions_Conflict(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("conflict-app", "test").
		WithConsumerOf(
			NewAddressRef("mixed").Build(),                           // nil subscriptions = ANYCAST
			NewAddressRef("mixed").WithSubscriptions("sub1").Build(), // MULTICAST
		).
		Build()

	err := reconciler.processCapabilities(secret, app)
	if err == nil {
		t.Fatal("expected error for routing type conflict")
	}

	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("error should mention routing type conflict, got: %v", err)
	}

	t.Logf("Correctly rejected conflict: %v", err)
}

func TestProcessCapabilities_SharedAddressSubscriptions_OnlyMulticast(t *testing.T) {
	reconciler := BrokerServiceInstanceReconcilerForTest()
	secret := CreateSecret("test-secret", "test")

	app := NewBrokerApp("producer-app", "test").
		WithSharedAddresses(NewAddressType("events").WithSubscriptions("sub1").Build()).
		WithProducerOf(v1beta2.AddressRef{Address: "events", PubSub: &[]bool{true}[0]}).Build()

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-producer-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have MULTICAST routing
	if !strings.Contains(props, `addressConfigurations."events".routingTypes=MULTICAST`) {
		t.Error("expected routingTypes=MULTICAST for subscriptions array")
	}

	if strings.Contains(props, `addressConfigurations."events".routingTypes=ANYCAST`) {
		t.Error("expected only routingTypes=MULTICAST for subscriptions array")
	}
}
