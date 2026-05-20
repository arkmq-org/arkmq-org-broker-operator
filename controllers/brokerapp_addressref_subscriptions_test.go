package controllers

import (
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestProcessCapabilities_AddressRefSubscriptions_ANYCAST tests ANYCAST queue (nil subscriptions)
func TestProcessCapabilities_AddressRefSubscriptions_ANYCAST(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anycast-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address: "commands",
							// Subscriptions: nil means ANYCAST
						},
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have ANYCAST routing
	if !contains(props, `addressConfigurations."commands".routingTypes=ANYCAST`) {
		t.Error("expected routingTypes=ANYCAST for nil subscriptions")
	}

	// Should have ANYCAST queue
	if !contains(props, `queueConfigs."commands".routingType=ANYCAST`) {
		t.Error("expected ANYCAST queue config")
	}
}

// TestProcessCapabilities_AddressRefSubscriptions_MULTICAST tests MULTICAST topic with subscription queues
func TestProcessCapabilities_AddressRefSubscriptions_MULTICAST(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	subs := []string{"sub1", "sub2"}
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multicast-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:       "events",
							Subscriptions: &subs,
						},
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have MULTICAST routing
	if !contains(props, `addressConfigurations."events".routingTypes=MULTICAST`) {
		t.Error("expected routingTypes=MULTICAST for subscriptions array")
	}

	// Should have MULTICAST subscription queues
	if !contains(props, `queueConfigs."sub1".routingType=MULTICAST`) {
		t.Error("expected MULTICAST queue sub1")
	}

	if !contains(props, `queueConfigs."sub2".routingType=MULTICAST`) {
		t.Error("expected MULTICAST queue sub2")
	}

	// Should have subscriber roles for FQQN
	if !contains(props, `securityRoles."events\:\:sub1"`) {
		t.Error("expected subscriber role for events::sub1")
	}

	if !contains(props, `securityRoles."events\:\:sub2"`) {
		t.Error("expected subscriber role for events::sub2")
	}
}

// TestProcessCapabilities_AddressRefSubscriptions_ProducerMULTICAST tests producer declaring MULTICAST address
func TestProcessCapabilities_AddressRefSubscriptions_ProducerMULTICAST(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	emptyArray := []string{}
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "producer-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ProducerOf: []broker.AddressRef{
						{
							Address:       "notifications",
							Subscriptions: &emptyArray, // Empty array = MULTICAST declaration
						},
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-producer-app-capabilities.properties"])
	t.Logf("PROPS:\n%s\n", props)

	// Should have MULTICAST routing (declared by producer)
	if !contains(props, `addressConfigurations."notifications".routingTypes=MULTICAST`) {
		t.Error("expected routingTypes=MULTICAST for empty subscriptions array in ProducerOf")
	}

	// Should NOT have queue configs (producer doesn't create queues)
	if contains(props, `queueConfigs."notifications"`) {
		t.Error("producer should not create queue configs")
	}
}

// TestProcessCapabilities_AddressRefSubscriptions_Conflict tests same-app routing conflict
func TestProcessCapabilities_AddressRefSubscriptions_Conflict(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	subs := []string{"sub1"}
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "conflict-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address: "mixed",
							// Subscriptions: nil (ANYCAST)
						},
						{
							Address:       "mixed",
							Subscriptions: &subs, // MULTICAST
						},
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err == nil {
		t.Fatal("expected error for routing type conflict")
	}

	if !contains(err.Error(), "ANYCAST") || !contains(err.Error(), "MULTICAST") || !contains(err.Error(), "conflict") {
		t.Errorf("error should mention routing type conflict, got: %v", err)
	}

	t.Logf("Correctly rejected conflict: %v", err)
}

// Helper function
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) &&
		(s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
