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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func hasKeyContaining(data map[string][]byte, substring string) bool {
	for k := range data {
		if strings.Contains(k, substring) {
			return true
		}
	}
	return false
}

func newBrokerApp(namespace, name string) v1beta2.BrokerApp {
	return v1beta2.BrokerApp{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

type appCert struct {
	namespace  string
	appName    string
	commonName string
}

func mustGenerateKeyPairCN(commonName string) (certPEM, keyPEM []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

func newServiceReconcilerForApps(apps []v1beta2.BrokerApp) *BrokerServiceInstanceReconciler {
	certs := make([]appCert, 0, len(apps))
	for _, app := range apps {
		certs = append(certs, appCert{namespace: app.Namespace, appName: app.Name, commonName: app.Name})
	}
	return newServiceReconcilerWithCerts(certs...)
}

func newServiceReconcilerWithCerts(certs ...appCert) *BrokerServiceInstanceReconciler {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, c := range certs {
		certPEM, keyPEM := mustGenerateKeyPairCN(c.commonName)
		builder = builder.WithObjects(WithCerts(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.appName + common.AppCertSecretSuffix,
				Namespace: c.namespace,
			},
			Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		})...)
	}

	return newServiceReconcilerWithClient(builder.Build())
}

func newServiceReconcilerWithClient(cl client.Client) *BrokerServiceInstanceReconciler {
	return &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{Client: cl, log: logr.Discard()},
			},
		},
	}
}

var _ = Describe("brokerservice controller unit", func() {

	It("reconcile with app move", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		s1Name := "service1"
		s2Name := "service2"
		appName := "my-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}

		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}
		s1 := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: s1Name, Namespace: ns, UID: types.UID("uid-s1")},
			Status:     v1beta2.BrokerServiceStatus{},
		}
		s2 := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: s2Name, Namespace: ns, UID: types.UID("uid-s2")},
			Status:     v1beta2.BrokerServiceStatus{},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns, UID: types.UID("uid-app")},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: s1Name, Namespace: ns, Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(namespace, oc, s1, s2, app)...).WithStatusSubresource(s1, s2, app)
		builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		})
		cl := builder.Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), reqS1)
		Expect(err).NotTo(HaveOccurred())

		secretS1 := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s1Name), Namespace: ns}, secretS1)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasKeyContaining(secretS1.Data, appName)).To(BeTrue(), "S1 secret should contain app config")

		err = cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, app)
		Expect(err).NotTo(HaveOccurred())
		app.Status.Service = &v1beta2.BrokerServiceBindingStatus{
			Name: s2Name, Namespace: ns, Secret: "app-binding-secret", AssignedPort: 61617,
		}
		Expect(cl.Status().Update(context.TODO(), app)).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), reqS1)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s1Name), Namespace: ns}, secretS1)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasKeyContaining(secretS1.Data, appName)).To(BeFalse(), "S1 secret should NOT contain app config after move")

		reqS2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s2Name, Namespace: ns}}
		_, err = r.Reconcile(context.TODO(), reqS2)
		Expect(err).NotTo(HaveOccurred())

		secretS2 := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s2Name), Namespace: ns}, secretS2)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasKeyContaining(secretS2.Data, appName)).To(BeTrue(), "S2 secret should contain app config after move")
	})

	It("error propagation", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		s1Name := "service1"
		ns := "default"
		s1 := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: s1Name, Namespace: ns, UID: types.UID("uid-s1")},
		}

		interceptorFuncs := interceptor.Funcs{
			List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.SecretList); ok {
					return fmt.Errorf("simulated list error")
				}
				return client.List(ctx, list, opts...)
			},
		}

		builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(s1)...).WithStatusSubresource(s1).WithInterceptorFuncs(interceptorFuncs)
		builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		})
		cl := builder.Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), reqS1)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated list error"))

		err = cl.Get(context.TODO(), reqS1.NamespacedName, s1)
		Expect(err).To(BeNil())

		Expect(meta.IsStatusConditionPresentAndEqual(s1.Status.Conditions, v1beta2.DeployedConditionType, metav1.ConditionUnknown)).To(BeTrue())
		Expect(meta.IsStatusConditionFalse(s1.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue())

		validCond := meta.FindStatusCondition(s1.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))
	})

	It("status update failure", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		s1Name := "service1"
		ns := "default"
		s1 := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: s1Name, Namespace: ns, UID: types.UID("uid-s1")},
		}

		interceptorFuncs := interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return fmt.Errorf("simulated status update error")
			},
		}

		builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(s1)...).WithStatusSubresource(s1).WithInterceptorFuncs(interceptorFuncs)
		builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			return nil
		})
		cl := builder.Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
		result, err := r.Reconcile(context.TODO(), reqS1)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated status update error"))
		Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
	})

	It("requires index", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		s1Name := "service1"
		appName := "my-app"

		s1 := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: s1Name, Namespace: ns, UID: types.UID("uid-s1")},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns, UID: types.UID("uid-app")},
		}

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(s1, app)...).WithStatusSubresource(s1, app).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), reqS1)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("index"))
	})

	It("deployed condition transition", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-broker-service"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Status:     v1beta2.BrokerServiceStatus{},
		}

		builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(svc)...).WithStatusSubresource(svc, &v1beta2.Broker{})
		builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		})
		cl := builder.Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNotReadyReason))
		creationTime := deployedCond.LastTransitionTime

		time.Sleep(1 * time.Second)

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
			Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue,
		})
		meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
			Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue,
		})
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		ss := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: svcName + "-ss", Namespace: ns},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							selectors.LabelAppKubernetesInstance: svcName,
							selectors.LabelBrokerService:         svcName,
						},
					},
				},
			},
		}
		err = cl.Create(context.TODO(), ss)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(deployedCond.Reason).To(Equal(v1beta2.ReadyConditionReason))
		Expect(deployedCond.LastTransitionTime.After(creationTime.Time)).To(BeTrue())
	})

	It("status applied apps", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"
		appName := "my-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns, Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				app := rawObj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).To(BeEmpty())

		secretName := AppPropertiesSecretName(svcName)
		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.ResourceVersion).NotTo(BeEmpty())
		Expect(secret.Annotations[common.ProvisionedAppsAnnotation]).To(Equal(fmt.Sprintf("%s/%s@0", ns, appName)))

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
		}
		brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
			{Name: secretName, ResourceVersion: secret.ResourceVersion},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).To(Equal([]string{fmt.Sprintf("%s/%s@0", ns, appName)}))
	})

	It("status applied apps incremental", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"
		app1Name := "my-app-1"
		app2Name := "my-app-2"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}
		app1 := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: app1Name, Namespace: ns},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns, Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app1)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				app := rawObj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secretName := AppPropertiesSecretName(svcName)
		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred())
		secretV1 := secret.ResourceVersion

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())
		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
		}
		brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
			{Name: secretName, ResourceVersion: secretV1},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).To(Equal([]string{fmt.Sprintf("%s/%s@0", ns, app1Name)}))

		app2 := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: app2Name, Namespace: ns},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns, Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}
		err = cl.Create(context.TODO(), app2)
		Expect(err).NotTo(HaveOccurred())
		Expect(cl.Create(context.TODO(), NewAppCertSecret(app2Name, ns))).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.ResourceVersion).NotTo(Equal(secretV1))
		secretV2 := secret.ResourceVersion

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).To(Equal([]string{fmt.Sprintf("%s/%s@0", ns, app1Name)}))

		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())
		brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
			{Name: secretName, ResourceVersion: secretV2},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		expectedApps := []string{fmt.Sprintf("%s/%s@0", ns, app1Name), fmt.Sprintf("%s/%s@0", ns, app2Name)}
		sort.Strings(expectedApps)
		sort.Strings(updatedSvc.Status.ProvisionedApps)
		Expect(updatedSvc.Status.ProvisionedApps).To(Equal(expectedApps))
	})

	It("apps provisioned condition", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				app := rawObj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		cond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(v1beta2.AppsProvisionedConditionWaitingReason))

		secretName := AppPropertiesSecretName(svcName)
		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.ResourceVersion).NotTo(BeEmpty())

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
		}
		brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
			{Name: secretName, ResourceVersion: secret.ResourceVersion},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		currentSecretResourceVersion := secret.ResourceVersion

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred())
		Expect(secret.ResourceVersion).To(Equal(currentSecretResourceVersion))

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		cond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(v1beta2.AppsProvisionedConditionSyncedReason))
	})

	It("prometheus override secret", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"
		appName := "metrics-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
			Spec: v1beta2.BrokerAppSpec{
				Capabilities: []v1beta2.AppCapabilityType{
					{
						ConsumerOf: []v1beta2.AddressRef{
							{Address: "TEST.QUEUE.ONE"},
							{Address: "TEST.QUEUE.TWO"},
						},
						ProducerOf: []v1beta2.AddressRef{
							{Address: "TEST.QUEUE.ONE"},
						},
					},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns, Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				app := rawObj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		overrideSecretName := svcName + "-control-plane-override"
		overrideSecret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideSecretName, Namespace: ns}, overrideSecret)
		Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should exist")

		prometheusYaml, ok := overrideSecret.Data[PrometheusConfigFileName]
		Expect(ok).To(BeTrue(), "should have prometheus key")
		Expect(prometheusYaml).NotTo(BeEmpty())

		prometheusConfig := string(prometheusYaml)
		Expect(prometheusConfig).To(ContainSubstring("org.apache.activemq.artemis:broker=*,component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*"))
		Expect(prometheusConfig).To(ContainSubstring("TEST.QUEUE.ONE"))
		Expect(prometheusConfig).To(ContainSubstring("TEST.QUEUE.TWO"))
		Expect(prometheusConfig).To(ContainSubstring("MessageCount"))
		Expect(prometheusConfig).To(ContainSubstring("ConsumerCount"))
		Expect(prometheusConfig).To(ContainSubstring("DeliveringCount"))
	})

	It("prometheus override no apps", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				app := rawObj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		overrideSecretName := svcName + "-control-plane-override"
		overrideSecret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideSecretName, Namespace: ns}, overrideSecret)
		Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should exist even without apps")

		prometheusYaml, ok := overrideSecret.Data[PrometheusConfigFileName]
		Expect(ok).To(BeTrue(), "should have prometheus key")
		Expect(prometheusYaml).NotTo(BeEmpty())

		prometheusConfig := string(prometheusYaml)
		Expect(prometheusConfig).To(ContainSubstring("component=addresses,address=*,subcomponent=queues"))
	})

	Context("valid condition", func() {
		It("sets Valid=True for valid spec", Label(unitLabel), func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(WithCerts(svc)...).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil())
			Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))
		})

		It("sets Valid=False for invalid resource name", Label(unitLabel), func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			invalidName := "broker/service"
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: invalidName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(WithCerts(svc)...).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil(), "Valid condition should be set for validation errors")
			Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
			Expect(validCondition.Message).NotTo(BeEmpty())

			deployedCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCondition).NotTo(BeNil())
			Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
			Expect(deployedCondition.Message).NotTo(BeEmpty())
		})

		It("Valid condition persists across reconciles", Label(unitLabel), func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service-persist"
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(WithCerts(svc)...).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svc.Name, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			validCondition1 := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition1).NotTo(BeNil())
			Expect(validCondition1.Status).To(Equal(metav1.ConditionTrue))

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			validCondition2 := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition2).NotTo(BeNil())
			Expect(validCondition2.Status).To(Equal(metav1.ConditionTrue))

			Expect(validCondition1.LastTransitionTime).To(Equal(validCondition2.LastTransitionTime))
		})
	})

	It("idempotent status after creation", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "test-service"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Status:     v1beta2.BrokerServiceStatus{},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc)...).
			WithStatusSubresource(svc)).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		initialStatus := updatedSvc.Status.DeepCopy()
		Expect(meta.FindStatusCondition(initialStatus.Conditions, v1beta2.AppsProvisionedConditionType).Message).To(ContainSubstring("not found"))

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		firstStatus := updatedSvc.Status

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		secondStatus := updatedSvc.Status

		Expect(firstStatus.Conditions).To(Equal(secondStatus.Conditions))
		Expect(meta.FindStatusCondition(secondStatus.Conditions, v1beta2.ValidConditionType)).NotTo(BeNil())
		Expect(meta.FindStatusCondition(secondStatus.Conditions, v1beta2.DeployedConditionType)).NotTo(BeNil())
		Expect(meta.FindStatusCondition(secondStatus.Conditions, v1beta2.AppsProvisionedConditionType)).NotTo(BeNil())
		Expect(meta.FindStatusCondition(secondStatus.Conditions, v1beta2.ReadyConditionType)).NotTo(BeNil())
	})

	It("condition independence", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "test-service"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Status:     v1beta2.BrokerServiceStatus{},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc)...).
			WithStatusSubresource(svc)).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

		deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))

		appsCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
		Expect(appsCond).NotTo(BeNil())
		Expect(appsCond.Status).To(Equal(metav1.ConditionFalse))

		readyCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ReadyConditionType)
		Expect(readyCond).NotTo(BeNil())
		Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
	})

	It("valid persists through runtime errors", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "test-service"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Status:     v1beta2.BrokerServiceStatus{},
		}

		interceptorFuncs := interceptor.Funcs{
			List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*corev1.SecretList); ok {
					return fmt.Errorf("simulated API list error")
				}
				return client.List(ctx, list, opts...)
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc)...).
			WithStatusSubresource(svc).
			WithInterceptorFuncs(interceptorFuncs)).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))

		deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionUnknown))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionCrudKindErrorReason))
	})

	It("condition transitions on recovery", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)

		ns := "default"
		svcName := "test-service"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Status:     v1beta2.BrokerServiceStatus{},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc)...).
			WithStatusSubresource(svc, &v1beta2.Broker{})).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

		deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNotReadyReason))

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue},
			{Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		ss := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: svcName + "-ss", Namespace: ns},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							selectors.LabelAppKubernetesInstance: svcName,
							selectors.LabelBrokerService:         svcName,
						},
					},
				},
			},
		}
		err = cl.Create(context.TODO(), ss)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		validCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

		deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(deployedCond.Reason).To(Equal(v1beta2.ReadyConditionReason))
	})

	It("grants queryMBeans to app metrics role", Label(unitLabel), func() {
		reconciler := &BrokerServiceInstanceReconciler{}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "my-app", Namespace: "ns1"},
			Spec: v1beta2.BrokerAppSpec{
				Capabilities: []v1beta2.AppCapabilityType{{
					ProducerOf: []v1beta2.AddressRef{{Address: "my-address"}},
					ConsumerOf: []v1beta2.AddressRef{{Address: "my-address"}},
				}},
			},
		}

		secret := &corev1.Secret{}
		Expect(reconciler.processCapabilities(secret, app)).NotTo(HaveOccurred())

		var cfg brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(secret.Data[AppIdentityPrefixed(app, "capabilities.json")], &cfg)).NotTo(HaveOccurred())

		Expect(cfg.SecurityRoles["mops.mbeanserver.queryMBeans"]["ns1-my-app-metrics"].View).To(BeTrue(),
			"app metrics role needs the queryMBeans gate to enumerate its own mbeans")

		Expect(cfg.SecurityRoles["mops.queue.my-address"]["ns1-my-app-metrics"].View).To(BeTrue())
	})

	It("omits queryMBeans without queues", Label(unitLabel), func() {
		reconciler := &BrokerServiceInstanceReconciler{}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "quiet-app", Namespace: "ns1"},
			Spec:       v1beta2.BrokerAppSpec{},
		}

		secret := &corev1.Secret{}
		Expect(reconciler.processCapabilities(secret, app)).NotTo(HaveOccurred())

		var cfg brokerproperties.CapabilitiesJSON
		Expect(json.Unmarshal(secret.Data[AppIdentityPrefixed(app, "capabilities.json")], &cfg)).NotTo(HaveOccurred())

		_, hasQueryMBeans := cfg.SecurityRoles["mops.mbeanserver.queryMBeans"]
		Expect(hasQueryMBeans).To(BeFalse())
	})

	It("app identity entries are order independent", Label(unitLabel), func() {
		apps := []v1beta2.BrokerApp{
			newBrokerApp("ns2", "app-alpha"), newBrokerApp("ns1", "app-beta"), newBrokerApp("ns1", "app-alpha"),
		}
		reconciler := newServiceReconcilerForApps(apps)

		forward, err := reconciler.appIdentityEntries(apps)
		Expect(err).NotTo(HaveOccurred())

		reversed, err := reconciler.appIdentityEntries([]v1beta2.BrokerApp{apps[2], apps[0], apps[1]})
		Expect(err).NotTo(HaveOccurred())

		Expect(forward).To(Equal(reversed), "entries must not depend on list order")

		identities := make([]string, 0, len(forward))
		for _, e := range forward {
			identities = append(identities, e.Identity)
		}
		Expect(identities).To(Equal([]string{"ns1-app-alpha", "ns1-app-beta", "ns2-app-alpha"}))
	})

	It("app identity entries use the cert common name", Label(unitLabel), func() {
		reconciler := newServiceReconcilerWithCerts(
			appCert{namespace: "ns1", appName: "app-alpha", commonName: "app-alpha.ns1.svc"})

		entries, err := reconciler.appIdentityEntries([]v1beta2.BrokerApp{newBrokerApp("ns1", "app-alpha")})
		Expect(err).NotTo(HaveOccurred())

		Expect(entries).To(HaveLen(1))
		Expect(entries[0].Identity).To(Equal("ns1-app-alpha"))
		Expect(entries[0].CNPattern).To(Equal(common.EscapeForRegex("app-alpha.ns1.svc")),
			"pattern must come from the cert CN, not the app name")
	})

	It("app identity entries escape the common name", Label(unitLabel), func() {
		reconciler := newServiceReconcilerWithCerts(
			appCert{namespace: "ns1", appName: "my-app", commonName: "my.app-1.ns1.svc"})

		entries, err := reconciler.appIdentityEntries([]v1beta2.BrokerApp{newBrokerApp("ns1", "my-app")})
		Expect(err).NotTo(HaveOccurred())

		Expect(entries).To(HaveLen(1))
		Expect(entries[0].CNPattern).To(Equal(`my\.app\-1\.ns1\.svc`))
	})

	It("app identity entries fail without app cert", Label(unitLabel), func() {
		reconciler := newServiceReconcilerWithCerts()

		_, err := reconciler.appIdentityEntries([]v1beta2.BrokerApp{newBrokerApp("ns1", "app-alpha")})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("app-alpha" + common.AppCertSecretSuffix))
	})

	It("app identity entries fail on unreadable app cert", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "app-alpha" + common.AppCertSecretSuffix,
				Namespace: "ns1",
			},
			Data: map[string][]byte{"tls.crt": []byte("not a certificate")},
		}).Build()

		reconciler := newServiceReconcilerWithClient(cl)

		_, err := reconciler.appIdentityEntries([]v1beta2.BrokerApp{newBrokerApp("ns1", "app-alpha")})
		Expect(err).To(HaveOccurred())
	})

	It("no requeues when broker ready but external configs empty", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"
		appName := "my-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns,
					Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				a := rawObj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue, Reason: "AllPodsReady"},
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "ResourceReady"},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		result, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(result.RequeueAfter == 0).To(BeTrue(),
			"expected no RequeueAfter when broker is Ready but ExternalConfigs is empty, got %v", result)

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).To(BeEmpty())

		appsProv := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
		Expect(appsProv).NotTo(BeNil())
		Expect(appsProv.Status).To(Equal(metav1.ConditionFalse))
	})

	It("no requeues when external config version stale", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"
		appName := "my-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns,
					Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				a := rawObj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		secretName := AppPropertiesSecretName(svcName)
		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue, Reason: "AllPodsReady"},
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "ResourceReady"},
		}
		brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
			{Name: secretName, ResourceVersion: "stale-version-that-wont-match"},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		result, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(result.RequeueAfter == 0).To(BeTrue(),
			"expected no RequeueAfter ExternalConfigs version is stale, got %v", result)

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).To(BeEmpty())
	})

	It("no requeue when external configs synced", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-service"
		appName := "my-app"

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}
		oc := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
			Data:       map[string][]byte{"ca.pem": []byte("bla")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
			Spec:       v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name: svcName, Namespace: ns,
					Secret: "binding-secret", AssignedPort: 61616,
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, oc, svc, app)...).
			WithStatusSubresource(svc, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				a := rawObj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			}).Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secretName := AppPropertiesSecretName(svcName)
		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
		Expect(err).NotTo(HaveOccurred())

		brokerCR := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
		Expect(err).NotTo(HaveOccurred())

		brokerCR.Status.Conditions = []metav1.Condition{
			{Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue, Reason: "AllPodsReady"},
			{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "ResourceReady"},
		}
		brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
			{Name: secretName, ResourceVersion: secret.ResourceVersion},
		}
		err = cl.Status().Update(context.TODO(), brokerCR)
		Expect(err).NotTo(HaveOccurred())

		result, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Duration(0)), "should not requeue when ExternalConfigs is synced")

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		Expect(updatedSvc.Status.ProvisionedApps).NotTo(BeEmpty())

		appsProv := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
		Expect(appsProv).NotTo(BeNil())
		Expect(appsProv.Status).To(Equal(metav1.ConditionTrue))
	})
})
