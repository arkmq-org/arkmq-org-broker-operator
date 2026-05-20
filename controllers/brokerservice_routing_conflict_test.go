package controllers

import (
	"log"
	"strings"
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestProcessCapabilities_MulticastRoutingForSubscriptions tests that subscription addresses use MULTICAST routing
func TestProcessCapabilities_MulticastRoutingForSubscriptions(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multicast-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			SharedAddresses: []broker.AddressType{
				{Address: "events"},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:       "events",
							Subscriptions: &[]string{"sub1"},
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
	log.Printf("PROPS:\n\n%s\n\n", props)

	// Should use MULTICAST routing type (NOT ANYCAST) for subscription address
	if !strings.Contains(props, `addressConfigurations."events".routingTypes=MULTICAST`) {
		t.Error("expected routingTypes=MULTICAST for subscription address")
	}

	if strings.Contains(props, `addressConfigurations."events".routingTypes=ANYCAST`) {
		t.Error("should NOT have routingTypes=ANYCAST for subscription address")
	}
}

// TestProcessCapabilities_AnycastRoutingForConsumerOf tests that consumerOf addresses use ANYCAST routing
func TestProcessCapabilities_AnycastRoutingForConsumerOf(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anycast-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			SharedAddresses: []broker.AddressType{
				{Address: "commands"},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{Address: "commands"},
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
	log.Printf("PROPS:\n\n%s\n\n", props)

	// Should use ANYCAST routing type (NOT MULTICAST) for consumerOf address
	if !strings.Contains(props, `addressConfigurations."commands".routingTypes=ANYCAST`) {
		t.Error("expected routingTypes=ANYCAST for consumerOf address")
	}

	if strings.Contains(props, `addressConfigurations."commands".routingTypes=MULTICAST`) {
		t.Error("should NOT have routingTypes=MULTICAST for consumerOf address")
	}
}

// TestProcessCapabilities_ConflictingRoutingTypes_SameApp tests that an address cannot be used with both
// Subscriptions (MULTICAST) and ConsumerOf (ANYCAST) in the same app
func TestProcessCapabilities_ConflictingRoutingTypes_SameApp(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "conflict-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			SharedAddresses: []broker.AddressType{
				{Address: "mixed"},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{Address: "mixed"}, // ANYCAST
						{
							Address:       "mixed",
							Subscriptions: &[]string{"queue1"}, // MULTICAST
						},
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err == nil {
		t.Fatal("processCapabilities should have failed with routing type conflict")
	}

	// Verify the error message mentions the conflict
	expectedKeywords := []string{"mixed", "ANYCAST", "MULTICAST", "conflict"}
	errMsg := err.Error()
	for _, keyword := range expectedKeywords {
		if !strings.Contains(errMsg, keyword) {
			t.Errorf("error message should contain '%s', got: %s", keyword, errMsg)
		}
	}

	t.Logf("Correctly rejected same-app routing conflict: %v", err)
}

// TestProcessCapabilities_ConflictingRoutingTypes_MultipleApps tests the multi-app conflict scenario
func TestProcessCapabilities_ConflictingRoutingTypes_MultipleApps(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App 1: Producer with Subscriptions (MULTICAST)
	app1 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "producer-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			SharedAddresses: []broker.AddressType{
				{Address: "shared-events"},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ProducerOf: []broker.AddressRef{
						{Address: "shared-events"},
					},
					ConsumerOf: []broker.AddressRef{
						{
							Address:       "shared-events",
							Subscriptions: &[]string{"producer-sub"}, // MULTICAST
						},
					},
				},
			},
		},
	}

	// App 2: Consumer with ConsumerOf (ANYCAST)
	app2 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:      "shared-events",
							AppNamespace: "test",
							AppName:      "producer-app",
						},
					},
				},
			},
		},
	}

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

	log.Printf("APP1 PROPS:\n\n%s\n\n", props1)
	log.Printf("APP2 PROPS:\n\n%s\n\n", props2)

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
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App 1: Subscriptions
	app1 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sub-app1",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			SharedAddresses: []broker.AddressType{
				{Address: "topic"},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:       "topic",
							Subscriptions: &[]string{"sub1"},
						},
					},
				},
			},
		},
	}

	// App 2: Also Subscriptions (compatible)
	app2 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sub-app2",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:       "topic",
							AppNamespace:  "test",
							AppName:       "sub-app1",
							Subscriptions: &[]string{"sub2"},
						},
					},
				},
			},
		},
	}

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

	log.Printf("APP1 PROPS:\n\n%s\n\n", props1)
	log.Printf("APP2 PROPS:\n\n%s\n\n", props2)

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
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App 1: ConsumerOf
	app1 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-app1",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			SharedAddresses: []broker.AddressType{
				{Address: "queue"},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{Address: "queue"},
					},
				},
			},
		},
	}

	// App 2: Also ConsumerOf (compatible)
	app2 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer-app2",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:      "queue",
							AppNamespace: "test",
							AppName:      "consumer-app1",
						},
					},
				},
			},
		},
	}

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

	log.Printf("APP1 PROPS:\n\n%s\n\n", props1)
	log.Printf("APP2 PROPS:\n\n%s\n\n", props2)

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
