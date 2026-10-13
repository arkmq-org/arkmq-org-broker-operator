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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("brokerservice security", func() {

	It("provisions bound app", Label(unitLabel), func() {
		scheme := NewServiceUnitTestScheme()

		svcNs := "broker-services"
		svcName := "premium-broker"
		allowedNs := "trusted-team"
		appName := "legitimate-app"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace == "trusted-team"`,
			},
			Status: v1beta2.BrokerServiceStatus{},
		}

		legitimateApp := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: allowedNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, legitimateApp, opCASecret)...).
			WithStatusSubresource(svc, legitimateApp).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      svc.Name + "-app-bp",
			Namespace: svcNs,
		}, secret)
		Expect(err).NotTo(HaveOccurred(), "App properties secret should be created")

		if err == nil {
			provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
			if hasAnnotation {
				Expect(provisionedApps).To(ContainSubstring(appName), "Should provision apps that match selector")
			}

			acceptorKey := AppIdentityPrefixed(legitimateApp, "acceptor.json")
			_, hasAcceptorConfig := secret.Data[acceptorKey]
			Expect(hasAcceptorConfig).To(BeTrue(), "Should create acceptor config for authorized app")
		}
	})

	It("provisions all bound apps", Label(unitLabel), func() {
		scheme := NewServiceUnitTestScheme()

		svcNs := "broker-services"
		svcName := "premium-broker"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace.startsWith("team-")`,
			},
			Status: v1beta2.BrokerServiceStatus{},
		}

		matchingApp1 := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app1",
				Namespace: "team-a",
			},
			Spec: v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61616,
				},
			},
		}

		matchingApp2 := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app2",
				Namespace: "team-b",
			},
			Spec: v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61617,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, matchingApp1, matchingApp2, opCASecret)...).
			WithStatusSubresource(svc, matchingApp1, matchingApp2).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      svc.Name + "-app-bp",
			Namespace: svcNs,
		}, secret)
		Expect(err).NotTo(HaveOccurred(), "App properties secret should be created")

		if err == nil {
			_, hasApp1Config := secret.Data[AppIdentityPrefixed(matchingApp1, "acceptor.json")]
			Expect(hasApp1Config).To(BeTrue(), "Should provision app1")

			_, hasApp2Config := secret.Data[AppIdentityPrefixed(matchingApp2, "acceptor.json")]
			Expect(hasApp2Config).To(BeTrue(), "Should provision app2")

			provisionedApps := secret.Annotations[common.ProvisionedAppsAnnotation]
			Expect(provisionedApps).To(ContainSubstring("app1"))
			Expect(provisionedApps).To(ContainSubstring("app2"))
		}
	})

	It("includes all bound apps in prometheus config", Label(unitLabel), func() {
		scheme := NewServiceUnitTestScheme()

		svcNs := "broker-services"
		svcName := "metrics-broker"
		appNs := "trusted-team"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
			},
			Spec:   v1beta2.BrokerServiceSpec{},
			Status: v1beta2.BrokerServiceStatus{},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "valid-app",
				Namespace: appNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				Capabilities: []v1beta2.AppCapabilityType{
					{
						ConsumerOf: []v1beta2.AddressRef{
							{Address: "VALID.QUEUE.ONE"},
							{Address: "VALID.QUEUE.TWO"},
						},
					},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, opCASecret)...).
			WithStatusSubresource(svc, app).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				a := obj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		overrideSecretName := svcName + "-control-plane-override"
		overrideSecret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      overrideSecretName,
			Namespace: svcNs,
		}, overrideSecret)
		Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should be created")

		prometheusYaml, ok := overrideSecret.Data[PrometheusConfigFileName]
		Expect(ok).To(BeTrue(), "should have prometheus key")
		Expect(prometheusYaml).NotTo(BeEmpty())

		prometheusConfig := string(prometheusYaml)

		Expect(prometheusConfig).To(ContainSubstring("VALID.QUEUE.ONE"),
			"App's ConsumerOf addresses should be in Prometheus config")
		Expect(prometheusConfig).To(ContainSubstring("VALID.QUEUE.TWO"),
			"App's ConsumerOf addresses should be in Prometheus config")
	})
})
