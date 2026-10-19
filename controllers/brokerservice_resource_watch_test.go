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
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("brokerservice resource watches", Label(unitLabel), func() {

	var (
		ns      string
		svcName string
		appName string
		scheme  = NewServiceUnitTestScheme()
	)

	setupReconciler := func() (client.Client, *BrokerServiceReconciler, ctrl.Request) {
		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)
		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("test")},
		}
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns, UID: types.UID("uid-svc")},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns, UID: types.UID("uid-app")},
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

		builder := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app)...).
			WithStatusSubresource(svc, app, &v1beta2.Broker{})
		builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			a := rawObj.(*v1beta2.BrokerApp)
			if a.Status.Service != nil {
				return []string{a.Status.Service.Key()}
			}
			return nil
		})
		cl := builder.Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		return cl, r, req
	}

	BeforeEach(func() {
		ns = "default"
		svcName = "watch-test-svc"
		appName = "watch-test-app"
	})

	It("recreates app-properties secret on reconcile after external deletion", func() {
		cl, r, req := setupReconciler()

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secretName := AppPropertiesSecretName(svcName)
		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred(), "app-properties secret should exist after first reconcile")

		Expect(cl.Delete(context.TODO(), secret)).To(Succeed())
		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(errors.IsNotFound(err)).To(BeTrue(), "secret should be deleted")

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred(), "app-properties secret should be recreated by reconcile")
	})

	It("recreates control-plane-override secret on reconcile after external deletion", func() {
		cl, r, req := setupReconciler()

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		overrideName := svcName + "-control-plane" + common.BrokerPropsSuffix
		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should exist after first reconcile")

		Expect(cl.Delete(context.TODO(), secret)).To(Succeed())
		err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideName, Namespace: ns}, secret)
		Expect(errors.IsNotFound(err)).To(BeTrue(), "secret should be deleted")

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should be recreated by reconcile")
	})

	It("recreates headless service on reconcile after external deletion", func() {
		cl, r, req := setupReconciler()

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		k8sSvc := &corev1.Service{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, k8sSvc)
		Expect(err).NotTo(HaveOccurred(), "headless service should exist after first reconcile")

		Expect(cl.Delete(context.TODO(), k8sSvc)).To(Succeed())
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, k8sSvc)
		Expect(errors.IsNotFound(err)).To(BeTrue(), "service should be deleted")

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, k8sSvc)
		Expect(err).NotTo(HaveOccurred(), "headless service should be recreated by reconcile")
	})

	It("recreates broker CR on reconcile after external deletion", func() {
		cl, r, req := setupReconciler()

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)
		Expect(err).NotTo(HaveOccurred(), "broker should exist after first reconcile")

		Expect(cl.Delete(context.TODO(), brokerCR)).To(Succeed())
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)
		Expect(errors.IsNotFound(err)).To(BeTrue(), "broker should be deleted")

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)
		Expect(err).NotTo(HaveOccurred(), "broker should be recreated by reconcile")
	})

	Context("service label on dependents", func() {

		It("app-properties secret carries the broker service label", func() {
			cl, r, req := setupReconciler()

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(svcName), Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Labels).To(HaveKeyWithValue(selectors.LabelBrokerService, svcName),
				"app-properties secret must carry the broker service label for watch filtering")
		})

		It("control-plane-override secret carries the broker service label", func() {
			cl, r, req := setupReconciler()

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName + "-control-plane" + common.BrokerPropsSuffix, Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Labels).To(HaveKeyWithValue(selectors.LabelBrokerService, svcName),
				"control-plane-override secret must carry the broker service label for watch filtering")
		})

		It("headless service carries the broker service label", func() {
			cl, r, req := setupReconciler()

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			k8sSvc := &corev1.Service{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, k8sSvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sSvc.Labels).To(HaveKeyWithValue(selectors.LabelBrokerService, svcName),
				"headless service must carry the broker service label for watch filtering")
		})
	})

	Context("dependent to service mapper", func() {

		It("maps a labeled secret to the owning BrokerService", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      AppPropertiesSecretName(svcName),
					Namespace: ns,
					Labels:    map[string]string{selectors.LabelBrokerService: svcName},
				},
			}

			requests := dependentToServiceMapper(context.TODO(), secret)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(svcName))
			Expect(requests[0].Namespace).To(Equal(ns))
		})

		It("maps a labeled service to the owning BrokerService", func() {
			k8sSvc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
					Labels:    map[string]string{selectors.LabelBrokerService: svcName},
				},
			}

			requests := dependentToServiceMapper(context.TODO(), k8sSvc)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(svcName))
			Expect(requests[0].Namespace).To(Equal(ns))
		})

		It("ignores objects without the broker service label", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-secret",
					Namespace: ns,
				},
			}

			requests := dependentToServiceMapper(context.TODO(), secret)
			Expect(requests).To(BeEmpty(), "objects without the label should produce no reconcile request")
		})
	})

	Context("dependent resources have no owner references", func() {

		It("all dependent resources are created without owner references", func() {
			cl, r, req := setupReconciler()

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			appSecret := &corev1.Secret{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(svcName), Namespace: ns}, appSecret)).To(Succeed())
			Expect(appSecret.GetOwnerReferences()).To(BeEmpty())

			overrideSecret := &corev1.Secret{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName + "-control-plane" + common.BrokerPropsSuffix, Namespace: ns}, overrideSecret)).To(Succeed())
			Expect(overrideSecret.GetOwnerReferences()).To(BeEmpty())

			brokerCR := &v1beta2.Broker{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, brokerCR)).To(Succeed())
			Expect(brokerCR.GetOwnerReferences()).To(BeEmpty())

			k8sSvc := &corev1.Service{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, k8sSvc)).To(Succeed())
			Expect(k8sSvc.GetOwnerReferences()).To(BeEmpty())
		})
	})
})
