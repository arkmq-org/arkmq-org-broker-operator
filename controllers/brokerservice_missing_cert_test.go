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

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("brokerservice missing certificate conditions", Label(unitLabel), func() {

	var (
		ns      string
		svcName string
		appName string
	)

	BeforeEach(func() {
		ns = "default"
		svcName = "cert-test-svc"
		appName = "cert-test-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)
		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)
	})

	boundApp := func() *v1beta2.BrokerApp {
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
			Spec: v1beta2.BrokerAppSpec{
				Addresses:    []v1beta2.AddressType{{Address: "orders"}},
				Capabilities: []v1beta2.AppCapabilityType{{ProducerOf: []v1beta2.AddressRef{{Address: "orders"}}}},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns, Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}
		WithBoundSpec(app)
		return app
	}

	newClient := func(objects ...client.Object) client.Client {
		scheme := NewServiceUnitTestScheme()
		builder := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(objects...).
			WithStatusSubresource(&v1beta2.BrokerService{}, &v1beta2.BrokerApp{}, &v1beta2.Broker{})
		builder = SetupBrokerAppIndexer(builder)
		return builder.Build()
	}

	reconcileAndGetStatus := func(cl client.Client) *v1beta2.BrokerService {
		r := NewBrokerServiceReconciler(cl, cl.Scheme(), nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, _ = r.Reconcile(context.TODO(), req)

		updatedSvc := &v1beta2.BrokerService{}
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
		return updatedSvc
	}

	Context("never deployed - cert missing on first reconcile", func() {

		It("reports MissingCertificate on Valid condition when operator CA is absent", func() {
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}
			objects := []client.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
				svc, boundApp(),
				NewCertSecret(common.GetOperatorCertSecretName(), ns, "operator"),
				NewAppCertSecret(appName, ns),
				NewCertSecret(svcName+"-"+common.DefaultOperandCertSecretName, ns, svcName),
			}
			cl := newClient(objects...)

			updatedSvc := reconcileAndGetStatus(cl)

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionFalse),
				"Valid should be False when cert is missing")
			Expect(validCond.Reason).To(Equal(v1beta2.DeployedConditionMissingCertReason))
			Expect(validCond.Message).To(ContainSubstring("op_ca"))

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).To(BeNil(), "Deployed should not be set on first reconcile with error")
		})

		It("reports MissingCertificate on Valid condition when app cert is absent", func() {
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			svcObjects := WithCerts(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
				oc, svc,
			)
			cl := newClient(append(svcObjects, boundApp())...)

			updatedSvc := reconcileAndGetStatus(cl)

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionFalse),
				"Valid should be False when app cert is missing")
			Expect(validCond.Reason).To(Equal(v1beta2.DeployedConditionMissingCertReason))
			Expect(validCond.Message).To(ContainSubstring(appName + common.AppCertSecretSuffix))

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).To(BeNil(), "Deployed should not be set on first reconcile with error")
		})

		It("reports MissingCertificate on Valid condition when operand cert is absent", func() {
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			objects := []client.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
				oc, svc, boundApp(),
				NewCertSecret(common.GetOperatorCertSecretName(), ns, "operator"),
				NewAppCertSecret(appName, ns),
			}
			cl := newClient(objects...)

			updatedSvc := reconcileAndGetStatus(cl)

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionFalse),
				"Valid should be False when operand cert is missing")
			Expect(validCond.Reason).To(Equal(v1beta2.DeployedConditionMissingCertReason))
			Expect(validCond.Message).To(ContainSubstring("broker-cert"))

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).To(BeNil(), "Deployed should not be set on first reconcile with error")
		})
	})

	Context("deletion with missing operand cert", func() {

		It("completes deletion when operand cert was never present", func() {
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			objects := []client.Object{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
				oc, svc, boundApp(),
				NewCertSecret(common.GetOperatorCertSecretName(), ns, "operator"),
				NewAppCertSecret(appName, ns),
			}
			cl := newClient(objects...)

			By("first reconcile reports missing cert without adding the finalizer")
			updatedSvc := reconcileAndGetStatus(cl)

			Expect(updatedSvc.Finalizers).NotTo(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"finalizer should not be added when processSpec fails")

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.DeployedConditionMissingCertReason))

			By("deleting the service")
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)).To(Succeed())
			Expect(cl.Delete(context.TODO(), updatedSvc)).To(Succeed())

			By("reconcile after deletion completes without the finalizer blocking it")
			r := NewBrokerServiceReconciler(cl, cl.Scheme(), nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, _ = r.Reconcile(context.TODO(), req)

			getErr := cl.Get(context.TODO(), req.NamespacedName, &v1beta2.BrokerService{})
			Expect(errors.IsNotFound(getErr)).To(BeTrue(),
				"service should be fully deleted — no finalizer was added so nothing blocks GC")
		})
	})

	Context("already deployed - cert deleted after successful reconcile", func() {

		It("preserves Deployed=True when operator CA is deleted after deployment", func() {
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			objects := WithCerts(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
				oc, svc, boundApp(),
			)
			cl := newClient(objects...)

			// First reconcile succeeds — broker is deployed
			updatedSvc := reconcileAndGetStatus(cl)
			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())

			// Simulate the broker reporting deployed
			brokerCR := &v1beta2.Broker{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)).To(Succeed())
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue, Reason: "AllPodsReady",
			})
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready",
			})
			Expect(cl.Status().Update(context.TODO(), brokerCR)).To(Succeed())

			// Reconcile again to pick up Deployed=True
			updatedSvc = reconcileAndGetStatus(cl)
			deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue), "broker should be Deployed=True")

			// Delete the operator CA secret
			caSecret := &corev1.Secret{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: "op_ca", Namespace: ns}, caSecret)).To(Succeed())
			Expect(cl.Delete(context.TODO(), caSecret)).To(Succeed())

			// Reconcile after cert deletion
			updatedSvc = reconcileAndGetStatus(cl)
			deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue),
				"Deployed should stay True — the broker is still running")
			Expect(deployedCond.Reason).To(Equal(v1beta2.ReadyConditionReason),
				"Deployed should preserve its original reason")

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionFalse),
				"Valid should report the cert issue")
			Expect(validCond.Reason).To(Equal(v1beta2.DeployedConditionMissingCertReason))
			Expect(validCond.Message).To(ContainSubstring("op_ca"),
				"message should name the missing secret")
		})

		It("Valid recovers when cert is restored", func() {
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			objects := WithCerts(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}},
				oc, svc, boundApp(),
			)
			cl := newClient(objects...)

			// Deploy successfully
			reconcileAndGetStatus(cl)
			brokerCR := &v1beta2.Broker{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)).To(Succeed())
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue, Reason: "AllPodsReady",
			})
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready",
			})
			Expect(cl.Status().Update(context.TODO(), brokerCR)).To(Succeed())
			updatedSvc := reconcileAndGetStatus(cl)
			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))

			// Delete the CA
			caSecret := &corev1.Secret{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: "op_ca", Namespace: ns}, caSecret)).To(Succeed())
			Expect(cl.Delete(context.TODO(), caSecret)).To(Succeed())

			// Reconcile — Valid should report MissingCertificate, Deployed stays untouched
			updatedSvc = reconcileAndGetStatus(cl)
			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.DeployedConditionMissingCertReason))

			deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue),
				"Deployed should stay True — broker is still running")

			// Restore the CA
			restoredCA := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("restored")},
			}
			Expect(cl.Create(context.TODO(), restoredCA)).To(Succeed())

			// Need to re-read broker to get fresh resource version
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)).To(Succeed())
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue, Reason: "AllPodsReady",
			})
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready",
			})
			Expect(cl.Status().Update(context.TODO(), brokerCR)).To(Succeed())

			// Reconcile after restore — Valid should recover
			updatedSvc = reconcileAndGetStatus(cl)
			validCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionSuccessReason),
				"Valid should recover after cert is restored")

			deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})
})
