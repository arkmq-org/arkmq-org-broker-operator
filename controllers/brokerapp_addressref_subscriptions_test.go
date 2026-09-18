package controllers

import (
	"strings"
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
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

	caps := parseCapabilities(t, secret, "anycast-app")

	commandsAddr := caps.AddressConfigurations["commands"]
	if commandsAddr == nil {
		t.Fatal("expected addressConfigurations for 'commands'")
	}

	// Should have ANYCAST routing
	if commandsAddr.RoutingTypes != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected routingTypes=ANYCAST for nil subscriptions, got %s", commandsAddr.RoutingTypes)
	}

	// Should have ANYCAST queue config
	queueCfg := commandsAddr.QueueConfigs["commands"]
	if queueCfg == nil {
		t.Error("expected ANYCAST queue config")
	} else if queueCfg.RoutingType != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected queueConfig routingType=ANYCAST, got %s", queueCfg.RoutingType)
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

	caps := parseCapabilities(t, secret, "multicast-app")

	eventsAddr := caps.AddressConfigurations["events"]
	if eventsAddr == nil {
		t.Fatal("expected addressConfigurations for 'events'")
	}

	// Should have MULTICAST routing
	if eventsAddr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Errorf("expected routingTypes=MULTICAST for subscriptions, got %s", eventsAddr.RoutingTypes)
	}

	// Should have MULTICAST subscription queues
	sub1 := eventsAddr.QueueConfigs["sub1"]
	if sub1 == nil || sub1.RoutingType != brokerproperties.RoutingTypeMulticast {
		t.Error("expected MULTICAST queue sub1")
	}
	sub2 := eventsAddr.QueueConfigs["sub2"]
	if sub2 == nil || sub2.RoutingType != brokerproperties.RoutingTypeMulticast {
		t.Error("expected MULTICAST queue sub2")
	}

	// Should have subscriber roles for FQQN (key is the raw "events::sub1", no escaping)
	if _, ok := caps.SecurityRoles["events::sub1"]; !ok {
		t.Error("expected subscriber role for events::sub1")
	}
	if _, ok := caps.SecurityRoles["events::sub2"]; !ok {
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

	caps := parseCapabilities(t, secret, "producer-app")

	notifAddr := caps.AddressConfigurations["notifications"]
	if notifAddr == nil {
		t.Fatal("expected addressConfigurations for 'notifications'")
	}

	// Should have ANYCAST routing (empty subs = ANYCAST)
	if notifAddr.RoutingTypes != brokerproperties.RoutingTypeAnycast {
		t.Errorf("expected routingTypes=ANYCAST for empty subscriptions, got %s", notifAddr.RoutingTypes)
	}

	// Producer should create queue configs
	if len(notifAddr.QueueConfigs) == 0 {
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

	caps := parseCapabilities(t, secret, "producer-app")

	eventsAddr := caps.AddressConfigurations["events"]
	if eventsAddr == nil {
		t.Fatal("expected addressConfigurations for 'events'")
	}

	// Should have MULTICAST routing only
	if eventsAddr.RoutingTypes != brokerproperties.RoutingTypeMulticast {
		t.Errorf("expected routingTypes=MULTICAST, got %s", eventsAddr.RoutingTypes)
	}
}
