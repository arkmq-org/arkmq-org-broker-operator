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
	"context"
	"encoding/json"
	"fmt"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	mgmt "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/artemis"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func buildMockQueueCountResponse(queues []mgmt.QueueMessageCount) map[string]interface{} {
	result := make(map[string]interface{}, len(queues))
	for _, qc := range queues {
		key := fmt.Sprintf(`org.apache.activemq.artemis:address="%s",broker="test-broker",component=addresses,queue="%s",routing-type="%s",subcomponent=queues`,
			qc.Address, qc.Queue, qc.RoutingType)
		result[key] = map[string]interface{}{"MessageCount": float64(qc.Count)}
	}
	return result
}

var _ = Describe("detectDrainingAddresses unit", Label(unitLabel), func() {

	buildOldSecretData := func(app *broker.BrokerApp, addresses ...string) map[string][]byte {
		caps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: make(map[string]*brokerproperties.AddressConfiguration),
		}
		for _, addr := range addresses {
			caps.AddressConfigurations[addr] = &brokerproperties.AddressConfiguration{}
		}
		data, err := json.Marshal(caps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		return map[string][]byte{capKey: data}
	}

	It("skips drain detection when app was never provisioned", Label(unitLabel), func() {
		ns := "default"
		appName := "new-app"

		app := NewBrokerApp(appName, ns).
			WithAddresses(NewAddressType("orders").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")

		oldSecretData := buildOldSecretData(app, "orders")
		emptyTracker := newAddressTracker()

		result, err := reconciler.detectDrainingAddresses(app, emptyTracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeNil(), "should skip drain when app was never provisioned")
	})

	It("detects draining addresses when app was provisioned", Label(unitLabel), func() {
		ns := "default"
		appName := "provisioned-app"

		app := NewBrokerApp(appName, ns).
			WithAddresses(NewAddressType("orders").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		oldSecretData := buildOldSecretData(app, "orders")
		emptyTracker := newAddressTracker()

		result, err := reconciler.detectDrainingAddresses(app, emptyTracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil(), "should detect draining addresses when app was provisioned")
		Expect(result).To(HaveLen(1))
		Expect(result[0].Address).To(Equal("orders"))
	})

	It("skips drain when a different app was provisioned but not this one", Label(unitLabel), func() {
		ns := "default"
		appName := "unprovisioned-app"
		otherAppName := "other-app"

		app := NewBrokerApp(appName, ns).
			WithAddresses(NewAddressType("orders").Build()).
			Build()
		otherApp := NewBrokerApp(otherAppName, ns).Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(otherApp),
		}

		oldSecretData := buildOldSecretData(app, "orders")
		emptyTracker := newAddressTracker()

		result, err := reconciler.detectDrainingAddresses(app, emptyTracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeNil(), "should skip drain when only a different app was provisioned")
	})

	It("detects drain when app was provisioned at a previous generation", Label(unitLabel), func() {
		ns := "default"
		appName := "updated-app"

		app := NewBrokerApp(appName, ns).
			WithAddresses(NewAddressType("orders").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		app.Generation = 1
		provisionedIdentity := AppIdentityWithGeneration(app)
		app.Generation = 3

		reconciler.status.ProvisionedApps = []string{provisionedIdentity}

		oldSecretData := buildOldSecretData(app, "orders")
		emptyTracker := newAddressTracker()

		result, err := reconciler.detectDrainingAddresses(app, emptyTracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil(), "should detect drain when app was provisioned at any prior generation")
	})

	It("should not add consume permission for producer-only address during drain", Label(unitLabel), func() {
		ns := "default"
		appName := "producer-app"

		app := NewBrokerApp(appName, ns).
			WithProducerOf(NewAddressRef("notifications").Build()).
			WithProducerOf(NewAddressRef("orders").Build()).
			WithConsumerOf(NewAddressRef("orders").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}
		Expect(reconciler.Client.Create(context.TODO(), app)).To(Succeed())

		// Old secret has both addresses configured with producer-only roles for notifications
		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"notifications": {RoutingTypes: brokerproperties.RoutingTypeAnycast},
				"orders":        {RoutingTypes: brokerproperties.RoutingTypeAnycast},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"notifications": {
					producerRole(AppIdentity(app)): {Send: true},
				},
				"orders": {
					producerRole(AppIdentity(app)): {Send: true},
					consumerRole(AppIdentity(app)): {Consume: true},
				},
			},
		}
		oldData, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: oldData}

		// Current spec: notifications removed, orders remains
		app.Spec.Capabilities = []broker.AppCapabilityType{
			{
				ProducerOf: []broker.AddressRef{{Address: "orders"}},
				ConsumerOf: []broker.AddressRef{{Address: "orders"}},
			},
		}

		tracker, buildErr := reconciler.buildAddressTracker(app)
		Expect(buildErr).NotTo(HaveOccurred())

		skip, drainErr := reconciler.processPendingEmpty(app, tracker, oldSecretData)
		Expect(drainErr).NotTo(HaveOccurred())
		Expect(skip).To(BeFalse())

		// Generate capabilities from the tracker
		secret := &corev1.Secret{}
		secret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(secret, app, tracker)).To(Succeed())

		var caps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(secret.Data[capKey], &caps)).To(Succeed())

		// notifications should be preserved during drain but should NOT have consume permission
		Expect(caps.AddressConfigurations).Should(HaveKey("notifications"),
			"draining address should be preserved in config")
		if notifRoles, ok := caps.SecurityRoles["notifications"]; ok {
			for roleName, perms := range notifRoles {
				Expect(perms.Consume).Should(BeFalse(),
					"producer-only address should not gain consume permission during drain, role: %s", roleName)
			}
		}

		// orders should retain both send and consume
		Expect(caps.SecurityRoles).Should(HaveKey("orders"))
		hasSend := false
		hasConsume := false
		for _, perms := range caps.SecurityRoles["orders"] {
			if perms.Send {
				hasSend = true
			}
			if perms.Consume {
				hasConsume = true
			}
		}
		Expect(hasSend).Should(BeTrue(), "orders should retain send permission")
		Expect(hasConsume).Should(BeTrue(), "orders should retain consume permission")
	})

	It("does not flag address that is still in the current spec", Label(unitLabel), func() {
		ns := "default"
		appName := "retained-app"

		app := NewBrokerApp(appName, ns).
			WithAddresses(NewAddressType("orders").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		oldSecretData := buildOldSecretData(app, "orders", "notifications")

		tracker, err := reconciler.buildAddressTracker(app)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconciler.detectDrainingAddresses(app, tracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1), "only removed address should be flagged")
		Expect(result[0].Address).To(Equal("notifications"))
	})

	It("extracts subscription queues from multicast address old config", Label(unitLabel), func() {
		ns := "default"
		appName := "multicast-app"

		app := NewBrokerApp(appName, ns).Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"client-a.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"client-b.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
		}
		data, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: data}

		emptyTracker := newAddressTracker()

		result, err := reconciler.detectDrainingAddresses(app, emptyTracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Address).To(Equal("events"))
		Expect(result[0].Subscriptions).To(ConsistOf("client-a.events", "client-b.events"))
	})

	It("sets HasActiveProducers when sender role is present in old config", Label(unitLabel), func() {
		ns := "default"
		appName := "producer-check-app"

		app := NewBrokerApp(appName, ns).Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		appIdentity := AppIdentity(app)
		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"orders": {RoutingTypes: brokerproperties.RoutingTypeAnycast},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"orders": {
					producerRole(appIdentity): {Send: true},
					consumerRole(appIdentity): {Consume: true},
				},
			},
		}
		data, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: data}

		result, err := reconciler.detectDrainingAddresses(app, newAddressTracker(), oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].HasActiveProducers).To(BeTrue(), "should flag active producers from deployed config")
	})

	It("detects removed subscription on a retained multicast address", Label(unitLabel), func() {
		ns := "default"
		appName := "sub-removal-app"

		app := NewBrokerApp(appName, ns).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("kept.events").Build()).
			WithProducerOf(NewAddressRef("events").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		appIdentity := AppIdentity(app)
		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"kept.events":    {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"removed.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"events": {
					producerRole(appIdentity): {Send: true},
					consumerRole(appIdentity): {Consume: true},
				},
			},
		}
		data, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: data}

		tracker, err := reconciler.buildAddressTracker(app)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconciler.detectDrainingAddresses(app, tracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].Address).To(Equal("events"))
		Expect(result[0].IsRetained).To(BeTrue(), "address is still in the spec")
		Expect(result[0].Subscriptions).To(ConsistOf("removed.events"))
	})

	It("does not set HasActiveProducers for retained address even when producer role exists", Label(unitLabel), func() {
		ns := "default"
		appName := "retained-producer-app"

		app := NewBrokerApp(appName, ns).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("kept.events").Build()).
			WithProducerOf(NewAddressRef("events").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}
		reconciler.instance.Status.PendingEmpty = []broker.PendingEmptyAddressStatus{
			{App: AppIdentity(app), Messages: map[string]int64{"events::removed.events": 5}},
		}

		appIdentity := AppIdentity(app)
		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"kept.events":    {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"removed.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"events": {
					producerRole(appIdentity): {Send: true},
					consumerRole(appIdentity): {Consume: true},
				},
			},
		}
		data, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: data}

		tracker, err := reconciler.buildAddressTracker(app)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconciler.detectDrainingAddresses(app, tracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].HasActiveProducers).To(BeFalse(),
			"retained address should not flag active producers — enabled=false handles it")
	})

	It("sets HasActiveProducers on first detection of retained subscription drain", Label(unitLabel), func() {
		ns := "default"
		appName := "first-detect-app"

		app := NewBrokerApp(appName, ns).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("kept.events").Build()).
			WithProducerOf(NewAddressRef("events").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		appIdentity := AppIdentity(app)
		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"kept.events":    {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"removed.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"events": {
					producerRole(appIdentity): {Send: true},
				},
			},
		}
		data, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: data}

		tracker, err := reconciler.buildAddressTracker(app)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconciler.detectDrainingAddresses(app, tracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].HasActiveProducers).To(BeTrue(),
			"first detection should gate Jolokia until enabled=false config is deployed")
	})

	It("clears HasActiveProducers when sender role was already revoked", Label(unitLabel), func() {
		ns := "default"
		appName := "revoked-producer-app"

		app := NewBrokerApp(appName, ns).Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}

		appIdentity := AppIdentity(app)
		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"orders": {RoutingTypes: brokerproperties.RoutingTypeAnycast},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"orders": {
					consumerRole(appIdentity): {Consume: true},
				},
			},
		}
		data, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: data}

		result, err := reconciler.detectDrainingAddresses(app, newAddressTracker(), oldSecretData)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
		Expect(result[0].HasActiveProducers).To(BeFalse(), "should not flag producers when sender role was already revoked")
	})
})

var _ = Describe("isDrainConfigApplied unit", Label(unitLabel), func() {

	It("returns false when any candidate has active producers", Label(unitLabel), func() {
		ns := "default"
		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")

		candidates := []DrainingAddressInfo{
			{Address: "orders", HasActiveProducers: true},
		}
		Expect(reconciler.isDrainConfigApplied(candidates)).To(BeFalse(),
			"should not allow Jolokia check when producers are still active")
	})

	It("returns false when AppsProvisioned is not synced", Label(unitLabel), func() {
		ns := "default"
		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")

		candidates := []DrainingAddressInfo{
			{Address: "orders", HasActiveProducers: false},
		}
		Expect(reconciler.isDrainConfigApplied(candidates)).To(BeFalse(),
			"should not allow Jolokia check when AppsProvisioned condition is missing")
	})

	It("returns true when no active producers and AppsProvisioned is synced", Label(unitLabel), func() {
		ns := "default"
		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")

		meta.SetStatusCondition(&reconciler.instance.Status.Conditions, metav1.Condition{
			Type:   broker.AppsProvisionedConditionType,
			Status: metav1.ConditionTrue,
			Reason: broker.AppsProvisionedConditionSyncedReason,
		})

		candidates := []DrainingAddressInfo{
			{Address: "orders", HasActiveProducers: false},
		}
		Expect(reconciler.isDrainConfigApplied(candidates)).To(BeTrue(),
			"should allow Jolokia check when producers revoked and config applied")
	})
})

var _ = Describe("provisionedAppIdentity unit", Label(unitLabel), func() {

	It("returns current generation when app has no pending addresses", Label(unitLabel), func() {
		ns := "default"
		app := NewBrokerApp("myapp", ns).Build()
		app.Generation = 5

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")

		Expect(reconciler.provisionedAppIdentity(app)).To(Equal(AppIdentityWithGeneration(app)))
	})

	It("returns held generation when app has pending addresses", Label(unitLabel), func() {
		ns := "default"
		app := NewBrokerApp("myapp", ns).Build()
		app.Generation = 7

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.instance.Status.ProvisionedApps = []string{ns + "/myapp@5"}
		reconciler.pendingEmptyAddresses = map[string]map[string]bool{
			AppIdentity(app): {"orders": true},
		}

		Expect(reconciler.provisionedAppIdentity(app)).To(Equal(ns+"/myapp@5"),
			"should hold generation at previously provisioned value during drain")
	})

	It("falls back to current generation on first detection", Label(unitLabel), func() {
		ns := "default"
		app := NewBrokerApp("myapp", ns).Build()
		app.Generation = 3

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.pendingEmptyAddresses = map[string]map[string]bool{
			AppIdentity(app): {"orders": true},
		}

		Expect(reconciler.provisionedAppIdentity(app)).To(Equal(AppIdentityWithGeneration(app)),
			"should use current generation when no previous provisioned entry exists")
	})
})

var _ = Describe("cross-app drain unit", Label(unitLabel), func() {

	It("should drop cross-app producer send permission when owner app is draining", Label(unitLabel), func() {
		ns := "default"

		ownerApp := NewBrokerApp("owner-app", ns).
			WithAddresses(NewAddressType("orders").Build()).
			WithProducerOf(NewAddressRef("orders").Build()).
			WithConsumerOf(NewAddressRef("orders").Build()).
			Build()

		producerApp := NewBrokerApp("producer-app", ns).
			WithProducerOf(NewAddressRef("orders").WithAppRef(ns, "owner-app").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(ownerApp),
			AppIdentityWithGeneration(producerApp),
		}
		Expect(reconciler.Client.Create(context.TODO(), ownerApp)).To(Succeed())
		Expect(reconciler.Client.Create(context.TODO(), producerApp)).To(Succeed())

		ownerCapKey := AppIdentityPrefixed(ownerApp, "capabilities.json")
		oldOwnerCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"orders": {RoutingTypes: brokerproperties.RoutingTypeAnycast},
			},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"orders": {
					producerRole(AppIdentity(ownerApp)): {Send: true},
					consumerRole(AppIdentity(ownerApp)): {Consume: true},
				},
			},
		}
		oldOwnerData, err := json.Marshal(oldOwnerCaps)
		Expect(err).NotTo(HaveOccurred())

		producerCapKey := AppIdentityPrefixed(producerApp, "capabilities.json")
		oldProducerCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{},
			SecurityRoles: map[string]map[string]*brokerproperties.RolePermissions{
				"orders": {
					producerRole(AppIdentity(producerApp)): {Send: true},
				},
			},
		}
		oldProducerData, err := json.Marshal(oldProducerCaps)
		Expect(err).NotTo(HaveOccurred())

		oldSecretData := map[string][]byte{
			ownerCapKey:    oldOwnerData,
			producerCapKey: oldProducerData,
		}

		// Simulate ownerApp address removal to trigger drain detection
		ownerApp.Spec.Capabilities = nil
		ownerApp.Spec.Addresses = nil

		// Pass 1: buildAddressTracker + processDrain for both apps
		ownerTracker, buildErr := reconciler.buildAddressTracker(ownerApp)
		Expect(buildErr).NotTo(HaveOccurred())
		ownerSkip, drainErr := reconciler.processPendingEmpty(ownerApp, ownerTracker, oldSecretData)
		Expect(drainErr).NotTo(HaveOccurred())
		Expect(ownerSkip).To(BeFalse())

		producerTracker, buildErr := reconciler.buildAddressTracker(producerApp)
		Expect(buildErr).NotTo(HaveOccurred())

		// Build activeApps set (ownerApp is draining but not skipped, producerApp is live)
		activeApps := map[string]bool{
			AppIdentity(ownerApp):    true,
			AppIdentity(producerApp): true,
		}

		// Pass 2: filterCrossAppRoles then processCapabilities

		// ownerApp's capabilities should have consume but NOT send
		reconciler.filterCrossAppRoles(ownerApp, ownerTracker, activeApps)
		ownerSecret := &corev1.Secret{}
		ownerSecret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(ownerSecret, ownerApp, ownerTracker)).To(Succeed())

		var ownerCaps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(ownerSecret.Data[ownerCapKey], &ownerCaps)).To(Succeed())
		Expect(ownerCaps.AddressConfigurations).Should(HaveKey("orders"),
			"draining address should be preserved in owner config")
		for roleName, perms := range ownerCaps.SecurityRoles["orders"] {
			Expect(perms.Send).Should(BeFalse(),
				"owner's producer role should be dropped during drain, role: %s", roleName)
		}

		// producerApp's Send should be dropped by filterCrossAppRoles
		reconciler.filterCrossAppRoles(producerApp, producerTracker, activeApps)
		producerSecret := &corev1.Secret{}
		producerSecret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(producerSecret, producerApp, producerTracker)).To(Succeed())

		var producerCaps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(producerSecret.Data[producerCapKey], &producerCaps)).To(Succeed())

		hasSendForOrders := false
		if ordersRoles, ok := producerCaps.SecurityRoles["orders"]; ok {
			for _, perms := range ordersRoles {
				if perms.Send {
					hasSendForOrders = true
				}
			}
		}
		Expect(hasSendForOrders).Should(BeFalse(),
			"cross-app producer should not retain send permission when owner app is draining")
	})

	It("should drop all cross-app roles when owner app is not active", Label(unitLabel), func() {
		ns := "default"

		producerApp := NewBrokerApp("producer-app", ns).
			WithProducerOf(NewAddressRef("orders").WithAppRef(ns, "gone-app").Build()).
			WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "gone-app").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")

		producerTracker, buildErr := reconciler.buildAddressTracker(producerApp)
		Expect(buildErr).NotTo(HaveOccurred())

		// gone-app is not in activeApps — it was deleted or removed
		activeApps := map[string]bool{
			AppIdentity(producerApp): true,
		}

		reconciler.filterCrossAppRoles(producerApp, producerTracker, activeApps)

		producerCapKey := AppIdentityPrefixed(producerApp, "capabilities.json")
		producerSecret := &corev1.Secret{}
		producerSecret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(producerSecret, producerApp, producerTracker)).To(Succeed())

		var producerCaps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(producerSecret.Data[producerCapKey], &producerCaps)).To(Succeed())

		Expect(producerCaps.AddressConfigurations).ShouldNot(HaveKey("orders"),
			"address config for gone owner's address should be pruned from capabilities")

		for roleName, perms := range producerCaps.SecurityRoles["orders"] {
			Expect(perms.Send).Should(BeFalse(),
				"send should be dropped when owner is gone, role: %s", roleName)
			Expect(perms.Consume).Should(BeFalse(),
				"consume should be dropped when owner is gone, role: %s", roleName)
		}
	})

	It("should keep cross-app consumer role when owner is draining", Label(unitLabel), func() {
		ns := "default"

		ownerApp := NewBrokerApp("owner-app", ns).
			WithAddresses(NewAddressType("orders").Build()).
			WithConsumerOf(NewAddressRef("orders").Build()).
			Build()

		consumerApp := NewBrokerApp("consumer-app", ns).
			WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner-app").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(ownerApp),
			AppIdentityWithGeneration(consumerApp),
		}
		Expect(reconciler.Client.Create(context.TODO(), ownerApp)).To(Succeed())
		Expect(reconciler.Client.Create(context.TODO(), consumerApp)).To(Succeed())

		oldOwnerCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"orders": {RoutingTypes: brokerproperties.RoutingTypeAnycast},
			},
		}
		oldOwnerData, err := json.Marshal(oldOwnerCaps)
		Expect(err).NotTo(HaveOccurred())
		ownerCapKey := AppIdentityPrefixed(ownerApp, "capabilities.json")
		oldSecretData := map[string][]byte{ownerCapKey: oldOwnerData}

		// ownerApp removes its address, triggering drain
		ownerApp.Spec.Capabilities = nil
		ownerApp.Spec.Addresses = nil

		ownerTracker, _ := reconciler.buildAddressTracker(ownerApp)
		_, err = reconciler.processPendingEmpty(ownerApp, ownerTracker, oldSecretData)
		Expect(err).NotTo(HaveOccurred())

		consumerTracker, buildErr := reconciler.buildAddressTracker(consumerApp)
		Expect(buildErr).NotTo(HaveOccurred())

		activeApps := map[string]bool{
			AppIdentity(ownerApp):    true,
			AppIdentity(consumerApp): true,
		}

		reconciler.filterCrossAppRoles(consumerApp, consumerTracker, activeApps)

		consumerCapKey := AppIdentityPrefixed(consumerApp, "capabilities.json")
		consumerSecret := &corev1.Secret{}
		consumerSecret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(consumerSecret, consumerApp, consumerTracker)).To(Succeed())

		var consumerCaps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(consumerSecret.Data[consumerCapKey], &consumerCaps)).To(Succeed())

		hasConsumeForOrders := false
		if ordersRoles, ok := consumerCaps.SecurityRoles["orders"]; ok {
			for _, perms := range ordersRoles {
				if perms.Consume {
					hasConsumeForOrders = true
				}
			}
		}
		Expect(hasConsumeForOrders).Should(BeTrue(),
			"cross-app consumer should keep consume permission when owner is draining (helps drain)")
	})

	It("stores FQQNs in pendingEmptyAddresses for retained subscription drains", Label(unitLabel), func() {
		ns := "default"

		app := NewBrokerApp("sub-drain-app", ns).
			WithAddresses(NewAddressType("events").WithSubscriptions("kept.events").WithPubSub(true).Build()).
			WithProducerOf(NewAddressRef("events").Build()).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("kept.events").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}
		reconciler.instance.Status.PendingEmpty = []broker.PendingEmptyAddressStatus{
			{App: AppIdentity(app), Messages: map[string]int64{"events::removed.events": 3}},
		}
		Expect(reconciler.Client.Create(context.TODO(), app)).To(Succeed())

		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"kept.events":    {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"removed.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
		}
		oldData, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: oldData}

		tracker, buildErr := reconciler.buildAddressTracker(app)
		Expect(buildErr).NotTo(HaveOccurred())
		_, drainErr := reconciler.processPendingEmpty(app, tracker, oldSecretData)
		Expect(drainErr).NotTo(HaveOccurred())

		role := AppIdentity(app)
		Expect(reconciler.pendingEmptyAddresses).To(HaveKey(role))
		Expect(reconciler.pendingEmptyAddresses[role]).To(HaveKey("events::removed.events"),
			"retained subscription drain should store FQQN, not bare address")
		Expect(reconciler.pendingEmptyAddresses[role]).ShouldNot(HaveKey("events"),
			"retained subscription drain should not store bare address name")
	})

	It("cross-app producer keeps send permission when owner has retained subscription drain", Label(unitLabel), func() {
		ns := "default"

		ownerApp := NewBrokerApp("owner-app", ns).
			WithAddresses(NewAddressType("events").WithSubscriptions("kept.events").WithPubSub(true).Build()).
			WithProducerOf(NewAddressRef("events").Build()).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("kept.events").Build()).
			Build()

		producerApp := NewBrokerApp("cross-producer", ns).
			WithProducerOf(NewAddressRef("events").WithAppRef(ns, "owner-app").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(ownerApp),
			AppIdentityWithGeneration(producerApp),
		}
		reconciler.instance.Status.PendingEmpty = []broker.PendingEmptyAddressStatus{
			{App: AppIdentity(ownerApp), Messages: map[string]int64{"events::removed.events": 3}},
		}
		Expect(reconciler.Client.Create(context.TODO(), ownerApp)).To(Succeed())
		Expect(reconciler.Client.Create(context.TODO(), producerApp)).To(Succeed())

		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"kept.events":    {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"removed.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
		}
		oldData, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		ownerCapKey := AppIdentityPrefixed(ownerApp, "capabilities.json")
		oldSecretData := map[string][]byte{ownerCapKey: oldData}

		ownerTracker, _ := reconciler.buildAddressTracker(ownerApp)
		_, drainErr := reconciler.processPendingEmpty(ownerApp, ownerTracker, oldSecretData)
		Expect(drainErr).NotTo(HaveOccurred())

		producerTracker, _ := reconciler.buildAddressTracker(producerApp)
		activeApps := map[string]bool{
			AppIdentity(ownerApp):    true,
			AppIdentity(producerApp): true,
		}
		reconciler.filterCrossAppRoles(producerApp, producerTracker, activeApps)

		producerCapKey := AppIdentityPrefixed(producerApp, "capabilities.json")
		producerSecret := &corev1.Secret{}
		producerSecret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(producerSecret, producerApp, producerTracker)).To(Succeed())

		var producerCaps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(producerSecret.Data[producerCapKey], &producerCaps)).To(Succeed())

		hasSend := false
		if eventsRoles, ok := producerCaps.SecurityRoles["events"]; ok {
			for _, perms := range eventsRoles {
				if perms.Send {
					hasSend = true
				}
			}
		}
		Expect(hasSend).Should(BeTrue(),
			"cross-app producer should keep send when owner has a retained subscription drain")
	})

	It("processCapabilities sets enabled=false on draining subscription queue", Label(unitLabel), func() {
		ns := "default"

		app := NewBrokerApp("enabled-test-app", ns).
			WithAddresses(NewAddressType("events").WithSubscriptions("kept.events").WithPubSub(true).Build()).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("kept.events").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.status.ProvisionedApps = []string{
			AppIdentityWithGeneration(app),
		}
		reconciler.instance.Status.PendingEmpty = []broker.PendingEmptyAddressStatus{
			{App: AppIdentity(app), Messages: map[string]int64{"events::removed.events": 5}},
		}
		Expect(reconciler.Client.Create(context.TODO(), app)).To(Succeed())

		oldCaps := &brokerproperties.CapabilitiesJSON{
			AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{
				"events": {
					RoutingTypes: brokerproperties.RoutingTypeMulticast,
					QueueConfigs: map[string]*brokerproperties.QueueConfig{
						"kept.events":    {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
						"removed.events": {RoutingType: brokerproperties.RoutingTypeMulticast, Address: "events"},
					},
				},
			},
		}
		oldData, err := json.Marshal(oldCaps)
		Expect(err).NotTo(HaveOccurred())
		capKey := AppIdentityPrefixed(app, "capabilities.json")
		oldSecretData := map[string][]byte{capKey: oldData}

		tracker, buildErr := reconciler.buildAddressTracker(app)
		Expect(buildErr).NotTo(HaveOccurred())
		_, drainErr := reconciler.processPendingEmpty(app, tracker, oldSecretData)
		Expect(drainErr).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		secret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(secret, app, tracker)).To(Succeed())

		var caps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(secret.Data[capKey], &caps)).To(Succeed())

		Expect(caps.AddressConfigurations).To(HaveKey("events"))
		eventsCfg := caps.AddressConfigurations["events"]
		Expect(eventsCfg.QueueConfigs).To(HaveKey("kept.events"))
		Expect(eventsCfg.QueueConfigs).To(HaveKey("removed.events"))

		Expect(*eventsCfg.QueueConfigs["kept.events"].Enabled).To(BeTrue(),
			"active subscription should have enabled=true")
		Expect(*eventsCfg.QueueConfigs["removed.events"].Enabled).To(BeFalse(),
			"draining subscription should have enabled=false")
	})

	It("should not filter locally owned address even when cross-app owner is draining", Label(unitLabel), func() {
		ns := "default"

		// app owns "orders" locally AND has a cross-app ProducerOf for same address
		app := NewBrokerApp("dual-app", ns).
			WithAddresses(NewAddressType("orders").Build()).
			WithProducerOf(NewAddressRef("orders").Build()).
			WithProducerOf(NewAddressRef("orders").WithAppRef(ns, "other-app").Build()).
			Build()

		reconciler := BrokerServiceInstanceReconcilerForTestWithInstance(ns, "test-service")
		reconciler.pendingEmptyAddresses = map[string]map[string]bool{
			SlashPrefixValue(ns, "other-app"): {"orders": true},
		}

		tracker, buildErr := reconciler.buildAddressTracker(app)
		Expect(buildErr).NotTo(HaveOccurred())

		activeApps := map[string]bool{
			AppIdentity(app):                  true,
			SlashPrefixValue(ns, "other-app"): true,
		}

		reconciler.filterCrossAppRoles(app, tracker, activeApps)

		capKey := AppIdentityPrefixed(app, "capabilities.json")
		secret := &corev1.Secret{}
		secret.Data = make(map[string][]byte)
		Expect(reconciler.processCapabilities(secret, app, tracker)).To(Succeed())

		var caps brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(secret.Data[capKey], &caps)).To(Succeed())

		hasSend := false
		if ordersRoles, ok := caps.SecurityRoles["orders"]; ok {
			for _, perms := range ordersRoles {
				if perms.Send {
					hasSend = true
				}
			}
		}
		Expect(hasSend).Should(BeTrue(),
			"locally owned address should retain send even when cross-app owner is draining")
	})
})

var _ = Describe("evaluateAddressDrainStatus unit", func() {

	var (
		mockCtrl *gomock.Controller
		logger   = logf.Log.WithName("drain-test")
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
	})
	AfterEach(func() {
		mockCtrl.Finish()
	})

	mockArtemisWithQueues := func(queues []mgmt.QueueMessageCount) *mgmt.Artemis {
		j := jolokia.NewMockIJolokia(mockCtrl)
		j.EXPECT().ReadMap(gomock.Any()).Return(
			buildMockQueueCountResponse(queues), nil,
		)
		return mgmt.GetArtemisWithJolokia(j, "test-broker")
	}

	It("marks anycast address as undrained when queue has messages", Label(unitLabel), func() {
		artemis := mockArtemisWithQueues([]mgmt.QueueMessageCount{
			{Address: "orders", Queue: "orders", RoutingType: "anycast", Count: 10},
		})
		addresses := []DrainingAddressInfo{{Address: "orders"}}

		result := evaluateAddressDrainStatus(logger, artemis, addresses)
		Expect(result).To(HaveKey("orders"))
		Expect(result["orders"].drained).To(BeFalse())
		Expect(result["orders"].messageCounts["orders"]).To(Equal(int64(10)))
	})

	It("marks anycast address as drained when queue has zero messages", Label(unitLabel), func() {
		artemis := mockArtemisWithQueues([]mgmt.QueueMessageCount{
			{Address: "orders", Queue: "orders", RoutingType: "anycast", Count: 0},
		})
		addresses := []DrainingAddressInfo{{Address: "orders"}}

		result := evaluateAddressDrainStatus(logger, artemis, addresses)
		Expect(result["orders"].drained).To(BeTrue())
	})

	It("marks address as drained when its queue is absent but other queues exist", Label(unitLabel), func() {
		artemis := mockArtemisWithQueues([]mgmt.QueueMessageCount{
			{Address: "other", Queue: "other", RoutingType: "anycast", Count: 0},
		})
		addresses := []DrainingAddressInfo{{Address: "orders"}}

		result := evaluateAddressDrainStatus(logger, artemis, addresses)
		Expect(result["orders"].drained).To(BeTrue())
	})

	It("marks multicast address as undrained when any subscription has messages", Label(unitLabel), func() {
		artemis := mockArtemisWithQueues([]mgmt.QueueMessageCount{
			{Address: "events", Queue: "client-a.events", RoutingType: "multicast", Count: 0},
			{Address: "events", Queue: "client-b.events", RoutingType: "multicast", Count: 5},
		})
		addresses := []DrainingAddressInfo{
			{Address: "events", Subscriptions: []string{"client-a.events", "client-b.events"}},
		}

		result := evaluateAddressDrainStatus(logger, artemis, addresses)
		Expect(result["events"].drained).To(BeFalse())
		Expect(result["events"].messageCounts["events::client-b.events"]).To(Equal(int64(5)))
	})

	It("marks multicast address as drained when all subscriptions are empty", Label(unitLabel), func() {
		artemis := mockArtemisWithQueues([]mgmt.QueueMessageCount{
			{Address: "events", Queue: "client-a.events", RoutingType: "multicast", Count: 0},
			{Address: "events", Queue: "client-b.events", RoutingType: "multicast", Count: 0},
		})
		addresses := []DrainingAddressInfo{
			{Address: "events", Subscriptions: []string{"client-a.events", "client-b.events"}},
		}

		result := evaluateAddressDrainStatus(logger, artemis, addresses)
		Expect(result["events"].drained).To(BeTrue())
	})

	It("marks all addresses as undrained when Jolokia fails", Label(unitLabel), func() {
		j := jolokia.NewMockIJolokia(mockCtrl)
		j.EXPECT().ReadMap(gomock.Any()).Return(nil, fmt.Errorf("connection refused"))
		artemis := mgmt.GetArtemisWithJolokia(j, "test-broker")

		addresses := []DrainingAddressInfo{{Address: "orders"}}

		result := evaluateAddressDrainStatus(logger, artemis, addresses)
		Expect(result["orders"].drained).To(BeFalse())
	})
})
