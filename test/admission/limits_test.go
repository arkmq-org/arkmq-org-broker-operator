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

package admission

// Limits of the drain gate. Each test asserts the behaviour that would keep
// messages from being stranded, so a failure demonstrates the limit. They fail
// by design and only run with RUN_GATE_LIMITS=true.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func limit(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_GATE_LIMITS") != "true" {
		t.Skip("documents a limit of the drain gate, set RUN_GATE_LIMITS=true to run")
	}
}

func TestLimit8OverrideNeedsNoMoreThanEditRights(t *testing.T) {
	limit(t)
	ctx := context.TODO()
	a := create(t, app("orders-editor", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 3))

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "brokerapp-editor", Namespace: ns},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"broker.arkmq.org"},
			Resources: []string{"brokerapps"},
			Verbs:     []string{"get", "list", "update", "patch", "delete"},
		}},
	}
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "brokerapp-editor", Namespace: ns},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: role.Name},
		Subjects:   []rbacv1.Subject{{Kind: "User", APIGroup: "rbac.authorization.k8s.io", Name: "app-developer"}},
	}
	require.NoError(t, cl.Create(ctx, role))
	require.NoError(t, cl.Create(ctx, binding))
	t.Cleanup(func() { _ = cl.Delete(ctx, binding); _ = cl.Delete(ctx, role) })

	developerCfg := rest.CopyConfig(cfg)
	developerCfg.Impersonate = rest.ImpersonationConfig{UserName: "app-developer", Groups: []string{"system:authenticated"}}
	developer, err := client.New(developerCfg, client.Options{Scheme: cl.Scheme()})
	require.NoError(t, err)

	current := &broker.BrokerApp{}
	require.Eventually(t, func() bool {
		return developer.Get(ctx, client.ObjectKeyFromObject(a), current) == nil
	}, 10*time.Second, 200*time.Millisecond, "the developer can read the app")

	current.Annotations = map[string]string{"broker.arkmq.org/allow-message-loss": "true"}
	current.Spec.Addresses = nil
	err = developer.Update(ctx, current)
	t.Logf("LIMIT 8: app developer sets the override and drops a queue holding 3 messages: %v", err)
	require.Error(t, err, "an app developer should not be able to bypass the gate")
}

func TestLimit6NamespaceDeletionWaitsOnUndrainedApps(t *testing.T) {
	limit(t)
	if !existingCluster {
		t.Skip("namespaces only finalize with kube-controller-manager, run with ADMISSION_EXISTING_CLUSTER=true")
	}
	ctx := context.TODO()
	nsName := "gate-limit6"
	require.NoError(t, cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}))
	a := app("orders-in-ns", owned("orders"))
	a.Namespace = nsName
	require.NoError(t, cl.Create(ctx, a))
	setStatus(a, bound(), observed(a), drained("orders", 3))
	t.Cleanup(func() { forceDelete(a) })

	require.NoError(t, cl.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}))

	gone := func() bool {
		return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKey{Name: nsName}, &corev1.Namespace{}))
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && !gone() {
		time.Sleep(2 * time.Second)
	}
	if !gone() {
		current := &corev1.Namespace{}
		require.NoError(t, cl.Get(ctx, client.ObjectKey{Name: nsName}, current))
		for _, c := range current.Status.Conditions {
			if c.Status == corev1.ConditionTrue {
				t.Logf("LIMIT 6: namespace %s: %s: %s", current.Status.Phase, c.Type, c.Message)
			}
		}
	}
	require.True(t, gone(), "deleting the namespace should complete")
}

// runs last: it deletes the BrokerApp CRD, then puts it back
func TestLimit5cCRDDeletionSkipsTheGate(t *testing.T) {
	limit(t)
	if existingCluster {
		t.Skip("deletes a cluster-wide CRD, envtest only")
	}
	ctx := context.TODO()
	a := create(t, app("orders-crd", owned("orders")))
	setStatus(a, bound(), observed(a), drained("orders", 3))
	requireDenied(t, cl.Delete(ctx, a, client.DryRunAll), "orders")

	crd := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "brokerapps.broker.arkmq.org"}}
	require.NoError(t, cl.Delete(ctx, crd))
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			return apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(crd), crd))
		}, 60*time.Second, 500*time.Millisecond)
		_, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "crd", "bases", "broker.arkmq.org_brokerapps.yaml")},
		})
		require.NoError(t, err)
	})

	removed := false
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && !removed {
		removed = apierrors.IsNotFound(cl.Get(ctx, client.ObjectKeyFromObject(crd), crd))
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("LIMIT 5c: CRD deleted with an app holding 3 messages: crd removed=%v", removed)
	require.False(t, removed, "the app holding messages should keep its CRD from being deleted")
}
