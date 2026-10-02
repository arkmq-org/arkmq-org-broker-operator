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
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("brokerapp deployed condition", func() {

	It("validation error with previous deployment keeps deployed true", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		service := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test",
				Labels:    map[string]string{"app": "broker"},
			},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ReadyConditionReason,
					},
				},
				ProvisionedApps: []string{"test/test-app"},
			},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-app",
				Namespace:  "test",
				Generation: 2,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "broker"},
				},
				Addresses: []v1beta2.AddressType{
					{Address: "conflicting-address"},
				},
				SharedAddresses: []v1beta2.AddressType{
					{Address: "conflicting-address"},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         "test-service",
					Namespace:    "test",
					Secret:       "test-app-binding-secret",
					AssignedPort: 61616,
				},
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.DeployedConditionProvisionedReason,
					},
					{
						Type:   v1beta2.ValidConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ValidConditionSuccessReason,
					},
				},
			},
		}

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(namespace, service, app).
			WithStatusSubresource(app).
			Build()

		reconciler := NewBrokerAppReconciler(fakeClient, scheme, nil, logr.New(log.NullLogSink{}))

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-app",
				Namespace: "test",
			},
		}
		result, err := reconciler.Reconcile(context.TODO(), req)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		updatedApp := &v1beta2.BrokerApp{}
		err = fakeClient.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionFalse), "Valid should be False due to address conflict")
		Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
		Expect(validCond.Message).To(ContainSubstring("cannot be both private and public"))
		Expect(validCond.ObservedGeneration).To(Equal(app.Generation))

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue),
			"Deployed should remain True - validation failed but broker wasn't updated, old config still active")
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisionedReason))
		Expect(deployedCond.ObservedGeneration < app.Generation).To(BeTrue(),
			"Deployed observedGeneration should be less than current generation - it reflects old spec (may be 0 if condition predates observedGeneration)")

		Expect(updatedApp.Status.Service).NotTo(BeNil())
		Expect(updatedApp.Status.Service.Name).To(Equal("test-service"))
	})

	It("validation error without previous deployment sets deployed false", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		service := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test",
				Labels:    map[string]string{"app": "broker"},
			},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ReadyConditionReason,
					},
				},
			},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "new-app",
				Namespace: "test",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "broker"},
				},
				Addresses: []v1beta2.AddressType{
					{Address: "conflicting-address"},
				},
				SharedAddresses: []v1beta2.AddressType{
					{Address: "conflicting-address"},
				},
			},
		}

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(namespace, service, app).
			WithStatusSubresource(app).
			Build()

		reconciler := NewBrokerAppReconciler(fakeClient, scheme, nil, logr.New(log.NullLogSink{}))

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "new-app",
				Namespace: "test",
			},
		}
		result, err := reconciler.Reconcile(context.TODO(), req)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		updatedApp := &v1beta2.BrokerApp{}
		err = fakeClient.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionFalse), "Valid should be False due to address conflict")

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse),
			"Deployed should be False - never deployed and validation failed")
	})

	It("validation error with previous deployed false stays false", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		service := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test",
				Labels:    map[string]string{"app": "broker"},
			},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ReadyConditionReason,
					},
				},
			},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-app",
				Namespace: "test",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "broker"},
				},
				Addresses: []v1beta2.AddressType{
					{Address: "conflicting-address"},
				},
				SharedAddresses: []v1beta2.AddressType{
					{Address: "conflicting-address"},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         "test-service",
					Namespace:    "test",
					Secret:       "test-app-binding-secret",
					AssignedPort: 61616,
				},
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionFalse,
						Reason: v1beta2.DeployedConditionProvisioningPendingReason,
					},
					{
						Type:   v1beta2.ValidConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ValidConditionSuccessReason,
					},
				},
			},
		}

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(namespace, service, app).
			WithStatusSubresource(app).
			Build()

		reconciler := NewBrokerAppReconciler(fakeClient, scheme, nil, logr.New(log.NullLogSink{}))

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-app",
				Namespace: "test",
			},
		}
		result, err := reconciler.Reconcile(context.TODO(), req)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		updatedApp := &v1beta2.BrokerApp{}
		err = fakeClient.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionFalse))

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse),
			"Deployed should be False - previous state was Deployed=False and validation failed")
	})

	It("demonstrate old bug", Label(unitLabel), func() {
		Skip("This test demonstrates the OLD buggy behavior - skipped because we fixed it")
	})
})
