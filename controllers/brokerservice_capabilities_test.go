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

func TestProcessCapabilities_OwnedAddress(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{Address: "orders"}},
			Capabilities: []broker.AppCapabilityType{
				{
					ProducerOf: []broker.AddressRef{
						{Address: "orders"}, // Local reference (owned)
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-owner-capabilities.properties"])

	// Should have addressConfiguration (owned)
	if !strings.Contains(props, `addressConfigurations."orders"`) {
		t.Error("expected addressConfigurations for owned address 'orders'")
	}

	// Should have RBAC
	if !strings.Contains(props, `securityRoles."orders"`) {
		t.Error("expected securityRoles for owned address 'orders'")
	}
}

func TestProcessCapabilities_ReferencedAddress(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:      "orders",
							AppNamespace: "other",
							AppName:      "owner",
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

	props := string(secret.Data["test-consumer-capabilities.properties"])

	// Should NOT have addressConfiguration routing types (not owned)
	if strings.Contains(props, `addressConfigurations."orders".routingTypes`) {
		t.Error("should NOT have addressConfigurations.routingTypes for referenced address 'orders'")
	}

	// Should have queue configs (needed even for referenced addresses)
	if !strings.Contains(props, `addressConfigurations."orders".queueConfigs`) {
		t.Error("expected queueConfigs for referenced address 'orders'")
	}

	// Should still have RBAC
	if !strings.Contains(props, `securityRoles."orders"`) {
		t.Error("expected securityRoles for referenced address 'orders'")
	}
}

func TestProcessCapabilities_MixedOwnedAndReferenced(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mixed",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{Address: "local-queue"}},
			Capabilities: []broker.AppCapabilityType{
				{
					ProducerOf: []broker.AddressRef{
						{Address: "local-queue"}, // Owned
					},
					ConsumerOf: []broker.AddressRef{
						{
							Address:      "shared-queue",
							AppNamespace: "other",
							AppName:      "owner",
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

	props := string(secret.Data["test-mixed-capabilities.properties"])

	log.Printf("PROPS: \n\n%s\n\n", props)

	// Should have addressConfiguration routing types for owned address
	if !strings.Contains(props, `addressConfigurations."local-queue".routingTypes`) {
		t.Error("expected addressConfigurations.routingTypes for owned address 'local-queue'")
	}

	// Should NOT have addressConfiguration routing types for referenced address
	if strings.Contains(props, `addressConfigurations."shared-queue".routingTypes`) {
		t.Error("should NOT have addressConfigurations.routingTypes for referenced address 'shared-queue'")
	}

	// "local-queue" is ProducerOf only but in addresses, so queue configs expected
	if !strings.Contains(props, `addressConfigurations."local-queue".queueConfigs`) {
		t.Error("should have queueConfigs for producer-only address 'local-queue'")
	}
	// "shared-queue" is ConsumerOf, so queue configs expected
	if !strings.Contains(props, `addressConfigurations."shared-queue".queueConfigs`) {
		t.Error("expected queueConfigs for referenced consumer address 'shared-queue'")
	}

	// Should have RBAC for both
	if !strings.Contains(props, `securityRoles."local-queue"`) {
		t.Error("expected securityRoles for owned address 'local-queue'")
	}
	if !strings.Contains(props, `securityRoles."shared-queue"`) {
		t.Error("expected securityRoles for referenced address 'shared-queue'")
	}
}

func TestProcessCapabilities_AddressRegistryNoCapabilities(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App declares addresses but has no capabilities (address registry pattern)
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "address-registry",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{Address: "events"},
				{Address: "commands"},
				{Address: "queries"},
			},
			// No capabilities - this app just owns the addresses for others to reference
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-address-registry-capabilities.properties"])

	// Should have addressConfigurations for all declared addresses (since they're owned)
	if !strings.Contains(props, `addressConfigurations."events"`) {
		t.Error("expected addressConfigurations for owned address 'events'")
	}
	if !strings.Contains(props, `addressConfigurations."commands"`) {
		t.Error("expected addressConfigurations for owned address 'commands'")
	}
	if !strings.Contains(props, `addressConfigurations."queries"`) {
		t.Error("expected addressConfigurations for owned address 'queries'")
	}

	// Should NOT have securityRoles (no capabilities = no RBAC)
	// Capabilities define the roles for RBAC, so without capabilities there are no roles
	if strings.Contains(props, `securityRoles."events"`) {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
	if strings.Contains(props, `securityRoles."commands"`) {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
	if strings.Contains(props, `securityRoles."queries"`) {
		t.Error("should NOT have securityRoles when app has no capabilities")
	}
}

func TestProcessCapabilities_SpecAddressesWithCapabilities(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App declares addresses in spec.addresses AND uses them in capabilities
	// This is the typical pattern for addresses that should be shareable
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "producer",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{Address: "events"},
				{Address: "commands"}, // declared but not used in capabilities
			},
			Capabilities: []broker.AppCapabilityType{
				{
					ProducerOf: []broker.AddressRef{
						{Address: "events"}, // also in spec.addresses
					},
				},
			},
		},
	}

	err := reconciler.processCapabilities(secret, app)
	if err != nil {
		t.Fatalf("processCapabilities failed: %v", err)
	}

	props := string(secret.Data["test-producer-capabilities.properties"])

	// Should have addressConfigurations for both addresses (both are in spec.addresses)
	if !strings.Contains(props, `addressConfigurations."events"`) {
		t.Error("expected addressConfigurations for 'events'")
	}
	if !strings.Contains(props, `addressConfigurations."commands"`) {
		t.Error("expected addressConfigurations for 'commands'")
	}

	// Should have RBAC only for addresses used in capabilities
	if !strings.Contains(props, `securityRoles."events"`) {
		t.Error("expected securityRoles for 'events' (used in capabilities)")
	}
	if strings.Contains(props, `securityRoles."commands"`) {
		t.Error("should NOT have securityRoles for 'commands' (not in capabilities)")
	}
}

func TestProcessCapabilities_QueueConfigsForSingleConsumer(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App with a single consumer capability - should generate queue configs
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					ConsumerOf: []broker.AddressRef{
						{
							Address:      "orders",
							AppNamespace: "other",
							AppName:      "producer",
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

	props := string(secret.Data["test-consumer-capabilities.properties"])

	// Should have queue configs even with a single consumer role
	// Current bug: condition is `len(addr.consumerRoles) > 1` which requires 2+ roles
	if !strings.Contains(props, `queueConfigs."orders".routingType=ANYCAST`) {
		t.Error("expected queueConfigs for single consumer role")
	}
	if !strings.Contains(props, `queueConfigs."orders".address=orders`) {
		t.Error("expected queueConfigs address mapping for single consumer role")
	}
}

func TestProcessCapabilities_QueueConfigsForSingleSubscriber(t *testing.T) {
	reconciler := &BrokerServiceInstanceReconciler{}
	secret := &corev1.Secret{Data: make(map[string][]byte)}

	// App with a single subscriber capability - should generate queue configs
	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "subscriber",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{
				{
					SubscriberOf: []broker.AddressRef{
						{
							Address:      "events::joe",
							AppNamespace: "other",
							AppName:      "producer",
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

	props := string(secret.Data["test-subscriber-capabilities.properties"])

	log.Printf("PROPS: \n\n%s\n\n", props)

	// Should have queue configs even with a single subscriber role
	if !strings.Contains(props, `queueConfigs."joe".routingType=MULTICAST`) {
		t.Error("expected queueConfigs for single subscriber role")
	}
	if !strings.Contains(props, `queueConfigs."joe".address=events`) {
		t.Error("expected queueConfigs address mapping for single subscriber role")
	}
}
