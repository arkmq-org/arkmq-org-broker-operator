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
	"log"
	"strings"
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProcessCapabilities_EmptySubscriptionsArray_MulticastOnly(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// Address with empty array should generate MULTICAST routing only
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multicast-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address:       "events",
					Subscriptions: &[]string{}, // Empty = multicast only
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-multicast-app-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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

func TestProcessCapabilities_SingleQueue_AnycastRouting(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// Address with a single queue should generate ANYCAST with that queue
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anycast-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address: "orders",
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-anycast-app-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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

func TestProcessCapabilities_MultipleSubs_AllCreated(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// Address with multiple subs should create all of them
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-queue-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address:       "tasks",
					Subscriptions: &[]string{"high-priority", "low-priority", "default"},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-multi-queue-app-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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

		if !strings.Contains(props, `queueConfigs."`+queue+`".routingType=ANYCAST`) {
			t.Errorf("expected routingType=ANYCAST for queue '%s'", queue)
		}

		if !strings.Contains(props, `queueConfigs."`+queue+`".address=tasks`) {
			t.Errorf("expected queue '%s' to map to address 'tasks'", queue)
		}
	}
}

func TestProcessCapabilities_SubsWithCapabilities_SubsAndRBAC(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// Address with queues AND capabilities should create queues AND RBAC
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "queue-with-caps",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address: "commands",
				},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ProducerOf: []broker.AddressRef{
						{Address: "commands"},
					},
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

	props := string(secret.Data["test-queue-with-caps-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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

func TestProcessCapabilities_NoQueuesField_InferredFromCapabilities(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// Address without queues field (omitempty) should use current behavior
	// (queues inferred from capabilities)
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inferred-queues",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address: "legacy",
					// Queues field omitted - should infer from capabilities
				},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{Address: "legacy"},
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-inferred-queues-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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

func TestProcessCapabilities_MixedMulticastAndAnycast(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mixed-routing",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address:       "events",
					Subscriptions: &[]string{}, // Multicast only
				},
				{
					Address: "commands",
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-mixed-routing-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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

func TestProcessCapabilities_SubsWithSubscriberCapability(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// Address with subs + Subscriptions should create both declared queues AND
	// queues from subscription capability
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscriber-with-queues",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{
					Address:       "notifications",
					Subscriptions: &[]string{"email", "sms"},
				},
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:       "notifications",
							Subscriptions: &[]string{"push"},
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

	props := string(secret.Data["test-subscriber-with-queues-capabilities.properties"])
	log.Printf("PROPS: \n\n%s\n\n", props)

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
