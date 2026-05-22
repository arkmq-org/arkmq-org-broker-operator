/*
Copyright 2021.

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
package common

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
}

var _ = Describe("Common Test", func() {
	It("Default Resync Period", func() {

		currentPeriod := 30 * time.Second // default

		valueFromEnv, defined := os.LookupEnv("RECONCILE_RESYNC_PERIOD")
		if defined {
			currentPeriod, _ = time.ParseDuration(valueFromEnv)
		}

		Expect(GetReconcileResyncPeriod()).To(Equal(currentPeriod))
	})

	It("getDeploymentCondition", func() {

		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					Size: Int32ToPtr(2),
				},
			},
		}

		condition := getDeploymentCondition(cr, nil, true, nil)
		Expect(condition.Status).Should(BeEquivalentTo(metav1.ConditionFalse))

		cr.Status.PodStatus = olm.DeploymentStatus{
			Ready: []string{"a", "b"},
		}

		condition = getDeploymentCondition(cr, nil, true, nil)
		Expect(condition.Status).Should(BeEquivalentTo(metav1.ConditionTrue))

		cr.Status.PodStatus = olm.DeploymentStatus{
			Ready: []string{"a", "b", "c"}, // over provisioned still true when scaling down
		}
		cr.Status.Conditions = []metav1.Condition{{Status: metav1.ConditionTrue, Type: v1beta2.ScaleDownPendingConditionType, Reason: v1beta2.ScaleDownPendingConditionPendingEmptyReason}}

		condition = getDeploymentCondition(cr, nil, true, nil)
		Expect(condition.Status).Should(BeEquivalentTo(metav1.ConditionTrue))
		Expect(condition.Reason).ShouldNot(BeEquivalentTo(v1beta2.ScaleDownPendingConditionType))

		cr.Status.PodStatus = olm.DeploymentStatus{
			Ready: []string{"a"},
		}
		condition = getDeploymentCondition(cr, nil, true, nil)
		Expect(condition.Status).Should(BeEquivalentTo(metav1.ConditionFalse))
	})

	Describe("ResolveSecret", func() {
		var (
			scheme    *runtime.Scheme
			namespace string
			crName    string
		)

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			namespace = "test-ns"
			crName = "test-broker"
		})

		It("should return CR-specific secret when it exists", func() {
			crSpecificSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crName + "-control-plane-override",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"key": []byte("cr-specific-value"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(crSpecificSecret).Build()

			secret, err := ResolveSecret(crName, namespace, "control-plane-override", fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret).NotTo(BeNil())
			Expect(secret.Name).To(Equal(crName + "-control-plane-override"))
			Expect(secret.Data["key"]).To(Equal([]byte("cr-specific-value")))
		})

		It("should fallback to shared secret when CR-specific doesn't exist", func() {
			sharedSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "control-plane-override",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"key": []byte("shared-value"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sharedSecret).Build()

			secret, err := ResolveSecret(crName, namespace, "control-plane-override", fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret).NotTo(BeNil())
			Expect(secret.Name).To(Equal("control-plane-override"))
			Expect(secret.Data["key"]).To(Equal([]byte("shared-value")))
		})

		It("should return nil when no secret exists", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			secret, err := ResolveSecret(crName, namespace, "control-plane-override", fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret).To(BeNil())
		})

		It("should prefer CR-specific over shared when both exist", func() {
			crSpecificSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crName + "-control-plane-override",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"key": []byte("cr-specific-value"),
				},
			}

			sharedSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "control-plane-override",
					Namespace: namespace,
				},
				Data: map[string][]byte{
					"key": []byte("shared-value"),
				},
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(crSpecificSecret, sharedSecret).Build()

			secret, err := ResolveSecret(crName, namespace, "control-plane-override", fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret).NotTo(BeNil())
			Expect(secret.Name).To(Equal(crName + "-control-plane-override"))
			Expect(secret.Data["key"]).To(Equal([]byte("cr-specific-value")))
		})

		It("should return error when Get fails with non-NotFound error", func() {
			fakeClient := &errorClient{
				Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			}

			secret, err := ResolveSecret(crName, namespace, "control-plane-override", fakeClient)
			Expect(err).To(HaveOccurred())
			Expect(secret).To(BeNil())
		})
	})

	Describe("GetOperatorSecretWithFallback", func() {
		const testNamespace = "operator-ns"

		var scheme *runtime.Scheme

		BeforeEach(func() {
			scheme = runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			SetOperatorNameSpace(testNamespace)
		})

		AfterEach(func() {
			UnsetOperatorNameSpace()
			operatorCertSecretName = nil
			operatorCASecretName = nil
			os.Unsetenv("ARKMQ_ORG_BROKER_MANAGER_CERT_SECRET_NAME")
			os.Unsetenv("ACTIVEMQ_ARTEMIS_MANAGER_CERT_SECRET_NAME")
			os.Unsetenv("ARKMQ_ORG_BROKER_MANAGER_CA_SECRET_NAME")
			os.Unsetenv("ACTIVEMQ_ARTEMIS_MANAGER_CA_SECRET_NAME")
		})

		It("should use new default secret when it exists", func() {
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: DefaultOperatorCertSecretName, Namespace: testNamespace},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newSecret).Build()

			secret, err := GetOperatorClientCertSecret(fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Name).To(Equal(DefaultOperatorCertSecretName))
		})

		It("should fall back to legacy secret when no env vars are set and new secret is missing", func() {
			legacySecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: LegacyOperatorCertSecretName, Namespace: testNamespace},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacySecret).Build()

			secret, err := GetOperatorClientCertSecret(fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Name).To(Equal(LegacyOperatorCertSecretName))
		})

		It("should prefer new secret over legacy when both exist and no env vars are set", func() {
			newSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: DefaultOperatorCertSecretName, Namespace: testNamespace},
			}
			legacySecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: LegacyOperatorCertSecretName, Namespace: testNamespace},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(newSecret, legacySecret).Build()

			secret, err := GetOperatorClientCertSecret(fakeClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Name).To(Equal(DefaultOperatorCertSecretName))
		})

		It("should not fall back to legacy when new env var is set", func() {
			os.Setenv("ARKMQ_ORG_BROKER_MANAGER_CERT_SECRET_NAME", "custom-cert")

			legacySecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: LegacyOperatorCertSecretName, Namespace: testNamespace},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacySecret).Build()

			_, err := GetOperatorClientCertSecret(fakeClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("custom-cert"))
			Expect(err.Error()).NotTo(ContainSubstring(LegacyOperatorCertSecretName))
		})

		It("should not fall back to legacy when legacy env var is set", func() {
			os.Setenv("ACTIVEMQ_ARTEMIS_MANAGER_CA_SECRET_NAME", "custom-ca")

			legacySecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: LegacyOperatorCASecretName, Namespace: testNamespace},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacySecret).Build()

			_, err := GetOperatorCASecret(fakeClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("custom-ca"))
			Expect(err.Error()).NotTo(ContainSubstring(LegacyOperatorCASecretName))
		})

		It("should return error when neither new nor legacy secret exists", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			_, err := GetOperatorClientCertSecret(fakeClient)
			Expect(err).To(HaveOccurred())
		})
	})
})

// errorClient is a fake client that returns errors for Get operations
type errorClient struct {
	client.Client
}

func (e *errorClient) Get(ctx context.Context, key types.NamespacedName, obj client.Object, opts ...client.GetOption) error {
	return errors.New("simulated error")
}
