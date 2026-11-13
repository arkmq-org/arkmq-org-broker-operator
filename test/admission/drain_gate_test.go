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

// Package admission runs the drain gate policies on a real kube-apiserver,
// without the operator: each test writes the status the operator would
// publish and checks what the API server admits.
package admission

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const ns = "gate"

var (
	cl  client.Client
	cfg *rest.Config
	// on an existing cluster, kube-controller-manager runs: namespaces finalize
	// and policies are type checked. Opt-in, the cluster needs the CRDs and no
	// other copy of the policies.
	existingCluster = os.Getenv("ADMISSION_EXISTING_CLUSTER") == "true"
	policies        []*unstructured.Unstructured
)

func TestMain(m *testing.M) {
	var env *envtest.Environment
	var err error
	if existingCluster {
		if cfg, err = config.GetConfig(); err != nil {
			panic(err)
		}
	} else {
		if os.Getenv("KUBEBUILDER_ASSETS") == "" {
			fmt.Println("KUBEBUILDER_ASSETS not set, skipping admission policy tests")
			os.Exit(0)
		}
		env = &envtest.Environment{
			CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
			ErrorIfCRDPathMissing: true,
		}
		if cfg, err = env.Start(); err != nil {
			panic(err)
		}
	}
	code := func() int {
		if env != nil {
			defer func() { _ = env.Stop() }()
		} else {
			defer teardown()
		}
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = admissionv1.AddToScheme(scheme)
		_ = rbacv1.AddToScheme(scheme)
		_ = apiextensionsv1.AddToScheme(scheme)
		if cl, err = client.New(cfg, client.Options{Scheme: scheme}); err != nil {
			panic(err)
		}
		if err = setup(); err != nil {
			panic(err)
		}
		return m.Run()
	}()
	os.Exit(code)
}

// leaves an existing cluster as found, apps first since the gate guards them
func teardown() {
	ctx := context.TODO()
	apps := &broker.BrokerAppList{}
	if cl.List(ctx, apps, client.InNamespace(ns)) == nil {
		for i := range apps.Items {
			forceDelete(&apps.Items[i])
		}
	}
	for _, p := range policies {
		_ = cl.Delete(ctx, p)
	}
	_ = cl.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
}

func setup() error {
	ctx := context.TODO()
	if err := cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "admission", "brokerapp_drain_gate.yaml"))
	if err != nil {
		return err
	}
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := cl.Create(ctx, obj); err != nil {
			return err
		}
		policies = append(policies, obj)
	}
	return waitForEnforcement()
}

// policies are enforced once the API server has loaded them
func waitForEnforcement() error {
	probe := app("probe", owned("orders"))
	if err := cl.Create(context.TODO(), probe); err != nil {
		return err
	}
	setStatus(probe, bound(), observed(probe), drained("orders", 5))
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		err := cl.Delete(context.TODO(), probe, client.DryRunAll)
		if apierrors.IsForbidden(err) || apierrors.IsInvalid(err) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("drain gate not enforced after 30s")
}

// fixtures

type specOpt func(*broker.BrokerApp)
type statusOpt func(*broker.BrokerApp)

func app(name string, opts ...specOpt) *broker.BrokerApp {
	a := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: broker.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"type": "broker"}},
		},
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

func owned(address string, subscriptions ...string) specOpt {
	return func(a *broker.BrokerApp) {
		a.Spec.Addresses = append(a.Spec.Addresses, broker.AddressType{Address: address, Subscriptions: subscriptions})
	}
}

func producerOf(address string) specOpt {
	return func(a *broker.BrokerApp) {
		a.Spec.Capabilities = append(a.Spec.Capabilities, broker.AppCapabilityType{
			ProducerOf: []broker.AddressRef{{Address: address}},
		})
	}
}

func consumerOf(address, appName string, subscriptions ...string) specOpt {
	return func(a *broker.BrokerApp) {
		a.Spec.Capabilities = append(a.Spec.Capabilities, broker.AppCapabilityType{
			ConsumerOf: []broker.AddressRef{{Address: address, AppName: appName, Subscriptions: subscriptions}},
		})
	}
}

func bound() statusOpt {
	return func(a *broker.BrokerApp) {
		a.Status.Service = &broker.BrokerServiceBindingStatus{Name: "svc", Namespace: ns, Secret: a.Name + "-binding-secret", AssignedPort: 61616}
	}
}

func observed(a *broker.BrokerApp) statusOpt {
	generation := a.Generation
	return func(a *broker.BrokerApp) { a.Status.AddressesObservedGeneration = generation }
}

// drained describes an anycast address and the counts of its queue
func drained(address string, counts ...int64) statusOpt {
	return func(a *broker.BrokerApp) {
		e := broker.AddressDrainStatus{Address: address}
		e.Queues = queues(address, counts)
		a.Status.Addresses = append(a.Status.Addresses, e)
	}
}

// subscriptions describes subscription queues by name
func subscriptions(address string, counts map[string]int64) statusOpt {
	return subscriptionsGranted(address, false, counts)
}

// subscriptionsGranted also tells whether an app may still send to the address
func subscriptionsGranted(address string, sendGranted bool, counts map[string]int64) statusOpt {
	return func(a *broker.BrokerApp) {
		e := broker.AddressDrainStatus{Address: address, SendGranted: sendGranted}
		for name, n := range counts {
			e.Queues = append(e.Queues, broker.QueueDrainStatus{Name: name, MessageCount: n})
		}
		a.Status.Addresses = append(a.Status.Addresses, e)
	}
}

func queues(address string, counts []int64) []broker.QueueDrainStatus {
	var q []broker.QueueDrainStatus
	for _, n := range counts {
		q = append(q, broker.QueueDrainStatus{Name: address, MessageCount: n})
	}
	return q
}

func setStatus(a *broker.BrokerApp, opts ...statusOpt) {
	a.Status = broker.BrokerAppStatus{}
	for _, o := range opts {
		o(a)
	}
	if err := cl.Status().Update(context.TODO(), a); err != nil {
		panic(err)
	}
}

func create(t *testing.T, a *broker.BrokerApp) *broker.BrokerApp {
	t.Helper()
	require.NoError(t, cl.Create(context.TODO(), a))
	t.Cleanup(func() { forceDelete(a) })
	return a
}

func forceDelete(a *broker.BrokerApp) {
	current := &broker.BrokerApp{}
	if cl.Get(context.TODO(), client.ObjectKeyFromObject(a), current) == nil {
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations["broker.arkmq.org/allow-message-loss"] = "true"
		_ = cl.Update(context.TODO(), current)
		_ = cl.Delete(context.TODO(), current)
	}
}

func requireDenied(t *testing.T, err error, contains string) {
	t.Helper()
	require.Error(t, err)
	require.True(t, apierrors.IsForbidden(err) || apierrors.IsInvalid(err), "expected admission denial, got %v", err)
	require.Contains(t, err.Error(), contains)
}

func update(t *testing.T, a *broker.BrokerApp, mutate func(*broker.BrokerApp), opts ...client.UpdateOption) error {
	t.Helper()
	current := &broker.BrokerApp{}
	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(a), current))
	mutate(current)
	return cl.Update(context.TODO(), current, opts...)
}

func dropAddresses(a *broker.BrokerApp) { a.Spec.Addresses = nil; a.Spec.Capabilities = nil }

// tests

func TestPoliciesTypeCheck(t *testing.T) {
	for _, name := range []string{"brokerapp-drain-gate", "brokerservice-drain-gate", "brokerservice-eviction-gate"} {
		policy := &admissionv1.ValidatingAdmissionPolicy{}
		require.NoError(t, cl.Get(context.TODO(), client.ObjectKey{Name: name}, policy))
		if policy.Status.TypeChecking == nil {
			// written by kube-controller-manager, which envtest does not run
			t.Skip("no type checking result on this API server")
		}
		for _, w := range policy.Status.TypeChecking.ExpressionWarnings {
			t.Errorf("%s: %s: %s", name, w.FieldRef, w.Warning)
		}
	}
}

func TestRemovingAddressWithMessagesIsDenied(t *testing.T) {
	a := create(t, app("orders-full", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 3))

	err := update(t, a, dropAddresses)
	requireDenied(t, err, "addresses still hold messages or grant send: orders")
}

func TestRemovingAddressWithProducerOfIsDenied(t *testing.T) {
	a := create(t, app("orders-produced", owned("orders"), producerOf("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 0))

	err := update(t, a, dropAddresses)
	requireDenied(t, err, "grant send: orders")
}

func TestRemovingAddressGrantingSendToOtherAppsIsDenied(t *testing.T) {
	a := create(t, app("orders-shared", owned("orders")))
	require.NoError(t, update(t, a, func(a *broker.BrokerApp) {
		a.Spec.Addresses[0].ProducerOfAppSelector = "true"
	}))
	current := &broker.BrokerApp{}
	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(a), current))
	setStatus(current, bound(), observed(current), drained("orders", 0))

	requireDenied(t, update(t, a, dropAddresses), "grant send: orders")

	By := "revoking the grant is allowed, the removal waits for that generation to be observed"
	require.NoError(t, update(t, a, func(a *broker.BrokerApp) { a.Spec.Addresses[0].ProducerOfAppSelector = "" }), By)
	requireDenied(t, update(t, a, dropAddresses), "not observed for generation 3")

	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(a), current))
	setStatus(current, bound(), observed(current), drained("orders", 0))
	require.NoError(t, update(t, a, dropAddresses))
}

func TestRemovingDrainedAddressIsAllowed(t *testing.T) {
	a := create(t, app("orders-empty", owned("orders"), owned("keep")))
	setStatus(a, bound(), observed(a), drained("orders", 0), drained("keep", 9))

	require.NoError(t, update(t, a, func(a *broker.BrokerApp) {
		a.Spec.Addresses = []broker.AddressType{{Address: "keep"}}
	}), "only the removed address needs to be drained")
}

func TestUnknownCountIsDenied(t *testing.T) {
	a := create(t, app("orders-unknown", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", broker.QueueDrainUnknown))

	requireDenied(t, update(t, a, dropAddresses), "orders")
}

func TestMissingObservationIsDenied(t *testing.T) {
	a := create(t, app("orders-unobserved", owned("orders")))
	setStatus(a, bound(), observed(a))

	requireDenied(t, update(t, a, dropAddresses), "orders")
}

func TestStaleGenerationIsDenied(t *testing.T) {
	a := create(t, app("orders-stale", owned("orders"), producerOf("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 0))

	By := "the user revokes producerOf, the operator has not observed that generation yet"
	require.NoError(t, update(t, a, func(a *broker.BrokerApp) { a.Spec.Capabilities = nil }), By)

	err := update(t, a, dropAddresses)
	requireDenied(t, err, "not observed for generation 2")
}

func TestAddingAndUnrelatedChangesAreAllowed(t *testing.T) {
	a := create(t, app("orders-busy", owned("orders"), producerOf("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 50))

	require.NoError(t, update(t, a, func(a *broker.BrokerApp) {
		a.Spec.Addresses = append(a.Spec.Addresses, broker.AddressType{Address: "more"})
	}))
	require.NoError(t, update(t, a, func(a *broker.BrokerApp) {
		a.Spec.Capabilities = nil
	}), "revoking producerOf is how a drain starts and must always be allowed")
	require.NoError(t, update(t, a, func(a *broker.BrokerApp) {
		a.Labels = map[string]string{"team": "x"}
	}))
}

func TestRoutingChangeCountsAsRemoval(t *testing.T) {
	a := create(t, app("orders-routing", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 4))

	err := update(t, a, func(a *broker.BrokerApp) { a.Spec.Addresses[0].PubSub = ptr.To(true) })
	requireDenied(t, err, "orders")
}

func TestRemovingSubscription(t *testing.T) {
	a := create(t, app("prices", owned("prices", "a", "b")))
	setStatus(a, bound(), observed(a), subscriptions("prices", map[string]int64{"a": 0, "b": 2}))

	requireDenied(t,
		update(t, a, func(a *broker.BrokerApp) { a.Spec.Addresses[0].Subscriptions = []string{"a"} }),
		"subscriptions removed from addresses still hold messages or receive them: prices")

	require.NoError(t,
		update(t, a, func(a *broker.BrokerApp) { a.Spec.Addresses[0].Subscriptions = []string{"b"} }),
		"subscription a is empty")
}

func TestRemovingSubscriptionOfATopicStillReceivingIsDenied(t *testing.T) {
	a := create(t, app("prices-live", owned("prices", "a", "b")))
	setStatus(a, bound(), observed(a), subscriptionsGranted("prices", true, map[string]int64{"a": 0, "b": 0}))

	requireDenied(t,
		update(t, a, func(a *broker.BrokerApp) { a.Spec.Addresses[0].Subscriptions = []string{"b"} }),
		"receive them: prices")
}

func TestRemovingConsumedSubscriptionOfATopicStillReceivingIsDenied(t *testing.T) {
	a := create(t, app("news-live", consumerOf("news", "publisher", "mine")))
	setStatus(a, bound(), observed(a), subscriptionsGranted("news", true, map[string]int64{"mine": 0}))

	requireDenied(t, update(t, a, func(a *broker.BrokerApp) { a.Spec.Capabilities = nil }), "news")
}

func TestRemovingConsumedSubscription(t *testing.T) {
	a := create(t, app("news-reader", consumerOf("news", "publisher", "mine")))
	setStatus(a, bound(), observed(a), subscriptions("news", map[string]int64{"mine": 1}))

	requireDenied(t, update(t, a, func(a *broker.BrokerApp) { a.Spec.Capabilities = nil }), "news")

	setStatus(a, bound(), observed(a), subscriptions("news", map[string]int64{"mine": 0}))
	require.NoError(t, update(t, a, func(a *broker.BrokerApp) { a.Spec.Capabilities = nil }))
}

func TestRemovingFullyQualifiedQueueRef(t *testing.T) {
	a := create(t, app("fqqn-reader", consumerOf("orders::special", "owner")))
	setStatus(a, bound(), observed(a), subscriptions("orders", map[string]int64{"special": 6}))

	requireDenied(t, update(t, a, func(a *broker.BrokerApp) { a.Spec.Capabilities = nil }), "queues still hold messages or receive them: orders::special")
}

func TestChangingServiceSelectorRequiresFullDrain(t *testing.T) {
	a := create(t, app("orders-mover", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 1))

	err := update(t, a, func(a *broker.BrokerApp) {
		a.Spec.ServiceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"type": "other"}}
	})
	requireDenied(t, err, "orders")
}

func TestDelete(t *testing.T) {
	a := create(t, app("orders-delete", owned("orders"), consumerOf("news", "publisher", "mine")))
	setStatus(a, bound(), observed(a), drained("orders", 0), subscriptions("news", map[string]int64{"mine": 2}))

	requireDenied(t, cl.Delete(context.TODO(), a), "news")

	setStatus(a, bound(), observed(a), drained("orders", 0), subscriptions("news", map[string]int64{"mine": 0}))
	require.NoError(t, cl.Delete(context.TODO(), a))
}

func TestDryRunReportsTheDenialWithoutChangingAnything(t *testing.T) {
	a := create(t, app("orders-dryrun", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 3))

	requireDenied(t, update(t, a, dropAddresses, client.DryRunAll), "orders")
	requireDenied(t, cl.Delete(context.TODO(), a, client.DryRunAll), "orders")

	setStatus(a, bound(), observed(a), drained("orders", 0))
	require.NoError(t, update(t, a, dropAddresses, client.DryRunAll))
	require.NoError(t, cl.Delete(context.TODO(), a, client.DryRunAll))

	current := &broker.BrokerApp{}
	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(a), current))
	require.Len(t, current.Spec.Addresses, 1, "dry run leaves the spec alone")
	require.Nil(t, current.DeletionTimestamp, "dry run leaves the app in place")
}

func TestUnboundAppIsNotGated(t *testing.T) {
	a := create(t, app("orders-unbound", owned("orders")))
	setStatus(a, drained("orders", 3))

	require.NoError(t, update(t, a, dropAddresses))
}

func TestOverrideAnnotation(t *testing.T) {
	a := create(t, app("orders-forced", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 3))

	require.NoError(t, update(t, a, func(a *broker.BrokerApp) {
		a.Annotations = map[string]string{"broker.arkmq.org/allow-message-loss": "true"}
		dropAddresses(a)
	}), "the override can be set in the same update")

	b := create(t, app("orders-forced-delete", owned("orders")))
	setStatus(b, bound(), observed(b), drained("orders", 3))
	require.NoError(t, update(t, b, func(a *broker.BrokerApp) {
		a.Annotations = map[string]string{"broker.arkmq.org/allow-message-loss": "true"}
	}))
	require.NoError(t, cl.Delete(context.TODO(), b))
}

func TestServiceDeletion(t *testing.T) {
	svc := &broker.BrokerService{ObjectMeta: metav1.ObjectMeta{Name: "svc-busy", Namespace: ns}}
	require.NoError(t, cl.Create(context.TODO(), svc))
	svc.Status.ProvisionedApps = []string{ns + "/orders-app@3"}
	require.NoError(t, cl.Status().Update(context.TODO(), svc))

	err := cl.Delete(context.TODO(), svc)
	requireDenied(t, err, "service still provisions apps: gate/orders-app@3")
	requireDenied(t, cl.Delete(context.TODO(), svc, client.DryRunAll), "orders-app")

	svc.Status.ProvisionedApps = nil
	require.NoError(t, cl.Status().Update(context.TODO(), svc))
	require.NoError(t, cl.Delete(context.TODO(), svc))
}

func TestStatusUpdatesAreNotGated(t *testing.T) {
	a := create(t, app("orders-status", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 3))

	current := &broker.BrokerApp{}
	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(a), current))
	current.Status.Addresses = nil
	require.NoError(t, cl.Status().Update(context.TODO(), current), "the operator must always be able to publish status")
	require.False(t, strings.Contains(fmt.Sprint(current.Status.Addresses), "orders"))
}

func service(t *testing.T, name string, labels map[string]string, provisioned ...string) *broker.BrokerService {
	t.Helper()
	svc := &broker.BrokerService{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}}
	require.NoError(t, cl.Create(context.TODO(), svc))
	svc.Status.ProvisionedApps = provisioned
	require.NoError(t, cl.Status().Update(context.TODO(), svc))
	t.Cleanup(func() {
		current := &broker.BrokerService{}
		if cl.Get(context.TODO(), client.ObjectKeyFromObject(svc), current) == nil {
			current.Annotations = map[string]string{"broker.arkmq.org/allow-message-loss": "true"}
			_ = cl.Update(context.TODO(), current)
			_ = cl.Delete(context.TODO(), current)
		}
	})
	return svc
}

func updateService(t *testing.T, svc *broker.BrokerService, mutate func(*broker.BrokerService)) error {
	t.Helper()
	current := &broker.BrokerService{}
	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(svc), current))
	mutate(current)
	return cl.Update(context.TODO(), current)
}

func TestServiceSelectorChangesThatCanEvictApps(t *testing.T) {
	svc := service(t, "svc-evict", map[string]string{"type": "broker", "team": "a"}, ns+"/orders-app@2")

	requireDenied(t, updateService(t, svc, func(s *broker.BrokerService) { s.Spec.AppSelectorExpression = "false" }),
		"appSelectorExpression cannot change while the service provisions apps: gate/orders-app@2")
	requireDenied(t, updateService(t, svc, func(s *broker.BrokerService) { delete(s.Labels, "type") }),
		"labels apps may select on cannot be removed or changed while the service provisions apps: type")
	requireDenied(t, updateService(t, svc, func(s *broker.BrokerService) { s.Labels["team"] = "b" }), "team")

	require.NoError(t, updateService(t, svc, func(s *broker.BrokerService) { s.Labels["extra"] = "x" }),
		"adding a label cannot unbind an app")
	require.NoError(t, updateService(t, svc, func(s *broker.BrokerService) {
		s.Annotations = map[string]string{"broker.arkmq.org/allow-message-loss": "true"}
		s.Spec.AppSelectorExpression = "false"
	}), "the override lifts the gate")
}

func TestServiceWithoutAppsCanChangeSelection(t *testing.T) {
	svc := service(t, "svc-empty", map[string]string{"type": "broker"})

	require.NoError(t, updateService(t, svc, func(s *broker.BrokerService) {
		s.Spec.AppSelectorExpression = "false"
		s.Labels = nil
	}))
}
