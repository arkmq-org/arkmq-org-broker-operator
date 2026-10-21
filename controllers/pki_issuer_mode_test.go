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

package controllers

import (
	"context"
	"os"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

// In issuer mode the operator is handed a ClusterIssuer and issues every
// certificate it needs. The CA bundle stays a prerequisite, as in manual mode.
var _ = Describe("pki issuer mode", Label("pki-issuer-mode"), func() {

	var caIssuer *cmv1.ClusterIssuer

	BeforeEach(func() {
		BeforeEachSpec()

		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		if !CertManagerInstalled() {
			Expect(InstallCertManager()).To(Succeed())
		}

		rootIssuer := InstallClusteredIssuer(rootIssuerName, nil)
		InstallCert(rootCertName, rootCertNamespce, func(candidate *cmv1.Certificate) {
			candidate.Spec.IsCA = true
			candidate.Spec.CommonName = "artemis.root.ca"
			candidate.Spec.SecretName = rootCertSecretName
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
				Name: rootIssuer.Name,
				Kind: "ClusterIssuer",
			}
		})
		caIssuer = InstallClusteredIssuer(caIssuerName, func(candidate *cmv1.ClusterIssuer) {
			candidate.Spec.SelfSigned = nil
			candidate.Spec.CA = &cmv1.CAIssuer{
				SecretName: rootCertSecretName,
			}
		})
		InstallCaBundle(common.DefaultOperatorCASecretName, rootCertSecretName, caPemTrustStoreName)

		By("leaving no operator cert behind from manual mode specs")
		removeOperatorCert()

		common.SetClusterIssuerName(caIssuer.Name)
	})

	AfterEach(func() {
		common.UnsetClusterIssuerName()

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			removeOperatorCert()
		}

		AfterEachSpec()
	})

	It("issues every certificate a service and its app need", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()

		By("deploying a service with no certificate in place")
		service := broker.BrokerService{
			TypeMeta: metav1.TypeMeta{Kind: "BrokerService", APIVersion: broker.GroupVersion.Identifier()},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"pki": serviceName},
			},
		}
		Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
		DeferCleanup(CleanResource, &service, service.Name, service.Namespace)

		By("deploying an app with no certificate in place")
		app := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{Kind: "BrokerApp", APIVersion: broker.GroupVersion.Identifier()},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName + "-app",
				Namespace: defaultNamespace,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pki": serviceName}},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{{Address: "PKI.JOBS"}},
						ConsumerOf: []broker.AddressRef{{Address: "PKI.JOBS"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &app)).Should(Succeed())
		DeferCleanup(CleanResource, &app, app.Name, app.Namespace)

		operatorCertKey := types.NamespacedName{Name: common.DefaultOperatorCertSecretName, Namespace: defaultNamespace}
		brokerCertKey := types.NamespacedName{Name: serviceName + "-" + common.DefaultOperandCertSecretName, Namespace: defaultNamespace}
		prometheusCertKey := types.NamespacedName{Name: serviceName + "-" + common.DefaultPrometheusCertSecretName, Namespace: defaultNamespace}
		appCertKey := types.NamespacedName{Name: app.Name + common.AppCertSecretSuffix, Namespace: app.Namespace}
		bindingSecretKey := types.NamespacedName{Name: BindingsSecretName(app.Name), Namespace: app.Namespace}

		By("checking the operator issued its own cert, with an unguessable identity")
		expectIssuedWithUUIDCommonName(operatorCertKey, nil)

		By("checking each cert is issued by a Certificate its CR owns")
		expectIssuedWithUUIDCommonName(brokerCertKey, &metav1.OwnerReference{Kind: "BrokerService", Name: serviceName})
		expectIssuedWithUUIDCommonName(prometheusCertKey, &metav1.OwnerReference{Kind: "BrokerService", Name: serviceName})
		expectIssuedWithUUIDCommonName(appCertKey, &metav1.OwnerReference{Kind: "BrokerApp", Name: app.Name})

		By("checking the app cert is issued into the app's binding secret")
		certificate := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, appCertKey, certificate)).Should(Succeed())
		Expect(certificate.Spec.SecretName).Should(Equal(bindingSecretKey.Name))
		Expect(errors.IsNotFound(k8sClient.Get(ctx, appCertKey, &corev1.Secret{}))).Should(BeTrue())

		By("checking the service and the app become ready")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			g.Expect(current.Status.Service).ShouldNot(BeNil())
			g.Expect(current.Status.Service.Secret).Should(Equal(bindingSecretKey.Name))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("checking the binding secret holds the binding and the cert, each written by its own manager")
		Eventually(func(g Gomega) {
			binding := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, bindingSecretKey, binding)).Should(Succeed())
			g.Expect(binding.Data).Should(HaveKey("host"))
			g.Expect(binding.Data).Should(HaveKey("port"))
			g.Expect(binding.Data).Should(HaveKey("uri"))
			g.Expect(binding.Data).Should(HaveKey("tls.crt"))
			g.Expect(binding.Data).Should(HaveKey("tls.key"))
			g.Expect(binding.ManagedFields).Should(ContainElement(SatisfyAll(
				HaveField("Manager", common.OperatorName),
				HaveField("Operation", metav1.ManagedFieldsOperationApply),
			)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("checking the operator's writes do not make cert-manager issue again")
		Expect(k8sClient.Get(ctx, appCertKey, certificate)).Should(Succeed())
		revision := certificate.Status.Revision
		Expect(revision).ShouldNot(BeNil())
		Consistently(func(g Gomega) {
			current := &cmv1.Certificate{}
			g.Expect(k8sClient.Get(ctx, appCertKey, current)).Should(Succeed())
			g.Expect(current.Status.Revision).Should(Equal(revision))
		}, "30s", "5s").Should(Succeed())

		By("sending a message over mtls with the issued app cert")
		producer := pkiProducerJob(serviceName+"-producer", bindingSecretKey.Name, bindingSecretKey.Name, "PKI.JOBS")
		Expect(k8sClient.Create(ctx, producer)).Should(Succeed())
		DeferCleanup(CleanResource, producer, producer.Name, producer.Namespace)
		DeferCleanup(UninstallSecret, pkiPemcfgSecretName, defaultNamespace)
		Eventually(func(g Gomega) {
			job := &batchv1.Job{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: producer.Name, Namespace: producer.Namespace}, job)).Should(Succeed())
			g.Expect(job.Status.Succeeded).Should(BeNumerically("==", 1))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting the app leaves neither its Certificate nor its binding secret behind")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(errors.IsNotFound(k8sClient.Get(ctx, appCertKey, &cmv1.Certificate{}))).Should(BeTrue())
			g.Expect(errors.IsNotFound(k8sClient.Get(ctx, bindingSecretKey, &corev1.Secret{}))).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting the service leaves none of its certs behind")
		CleanResource(&service, service.Name, service.Namespace)
		Eventually(func(g Gomega) {
			for _, key := range []types.NamespacedName{brokerCertKey, prometheusCertKey} {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, key, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, key, &corev1.Secret{}))).Should(BeTrue())
			}
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("leaves nothing behind for an app that never bound", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		app := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{Kind: "BrokerApp", APIVersion: broker.GroupVersion.Identifier()},
			ObjectMeta: metav1.ObjectMeta{
				Name:      NextSpecResourceName() + "-app",
				Namespace: defaultNamespace,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pki": "no-such-service"}},
				Capabilities: []broker.AppCapabilityType{
					{ProducerOf: []broker.AddressRef{{Address: "PKI.UNBOUND"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &app)).Should(Succeed())
		DeferCleanup(CleanResource, &app, app.Name, app.Namespace)

		appCertKey := types.NamespacedName{Name: app.Name + common.AppCertSecretSuffix, Namespace: app.Namespace}
		bindingSecretKey := types.NamespacedName{Name: BindingsSecretName(app.Name), Namespace: app.Namespace}

		By("checking the cert is issued while the app waits for a service, with no binding yet")
		expectIssuedWithUUIDCommonName(appCertKey, &metav1.OwnerReference{Kind: "BrokerApp", Name: app.Name})
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, current)).Should(Succeed())
			condition := meta.FindStatusCondition(current.Status.Conditions, broker.DeployedConditionType)
			g.Expect(condition).ShouldNot(BeNil())
			g.Expect(condition.Status).Should(Equal(metav1.ConditionFalse))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		binding := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, bindingSecretKey, binding)).Should(Succeed())
		Expect(binding.Data).ShouldNot(HaveKey("host"))

		By("deleting the app leaves neither its Certificate nor its binding secret behind")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(errors.IsNotFound(k8sClient.Get(ctx, appCertKey, &cmv1.Certificate{}))).Should(BeTrue())
			g.Expect(errors.IsNotFound(k8sClient.Get(ctx, bindingSecretKey, &corev1.Secret{}))).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("issues certificates from a namespace-scoped operator", Label("do"), func() {
		if os.Getenv("DEPLOY_OPERATOR") != "true" {
			return
		}

		ctx := context.Background()

		By("redeploying the operator, bound by role.yaml alone, with the cluster issuer")
		Expect(uninstallOperator(false, defaultNamespace)).To(Succeed())
		Expect(installOperator(map[string]string{common.ClusterIssuerEnvVar: caIssuer.Name}, defaultNamespace)).To(Succeed())
		defer func() {
			Expect(uninstallOperator(false, defaultNamespace)).To(Succeed())
			Expect(installOperator(nil, defaultNamespace)).To(Succeed())
		}()

		service := broker.BrokerService{
			TypeMeta:   metav1.TypeMeta{Kind: "BrokerService", APIVersion: broker.GroupVersion.Identifier()},
			ObjectMeta: metav1.ObjectMeta{Name: NextSpecResourceName(), Namespace: defaultNamespace},
		}
		Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
		DeferCleanup(CleanResource, &service, service.Name, service.Namespace)

		expectIssuedWithUUIDCommonName(types.NamespacedName{Name: common.DefaultOperatorCertSecretName, Namespace: defaultNamespace}, nil)
		expectIssuedWithUUIDCommonName(types.NamespacedName{Name: service.Name + "-" + common.DefaultOperandCertSecretName, Namespace: defaultNamespace},
			&metav1.OwnerReference{Kind: "BrokerService", Name: service.Name})

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("reports why a certificate cannot be issued", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		missingIssuer := NextSpecResourceName() + "-missing"
		common.SetClusterIssuerName(missingIssuer)

		service := broker.BrokerService{
			TypeMeta:   metav1.TypeMeta{Kind: "BrokerService", APIVersion: broker.GroupVersion.Identifier()},
			ObjectMeta: metav1.ObjectMeta{Name: NextSpecResourceName(), Namespace: defaultNamespace},
		}
		Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
		DeferCleanup(CleanResource, &service, service.Name, service.Namespace)

		By("checking the service surfaces cert-manager's reason, naming the issuer")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, current)).Should(Succeed())
			condition := meta.FindStatusCondition(current.Status.Conditions, broker.DeployedConditionType)
			g.Expect(condition).ShouldNot(BeNil())
			g.Expect(condition.Status).Should(Equal(metav1.ConditionFalse))
			g.Expect(condition.Reason).Should(Equal(broker.DeployedConditionWaitingForCertificateReason))
			g.Expect(condition.Message).Should(ContainSubstring(missingIssuer))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	DescribeTable("issues over a secret the user left under a cert's name",
		func(baseSecretName string, converges bool) {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()
			secretName := serviceName + "-" + baseSecretName
			key := types.NamespacedName{Name: secretName, Namespace: defaultNamespace}

			By("dropping a user secret where the service would issue its own")
			InstallSecret(secretName, defaultNamespace, func(candidate *corev1.Secret) {
				candidate.StringData = map[string]string{"tls.crt": "user", "tls.key": "user", "user": "kept"}
			})
			DeferCleanup(func() {
				if k8sClient.Get(ctx, key, &corev1.Secret{}) == nil {
					UninstallSecret(secretName, defaultNamespace)
				}
			})

			service := broker.BrokerService{
				TypeMeta:   metav1.TypeMeta{Kind: "BrokerService", APIVersion: broker.GroupVersion.Identifier()},
				ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: defaultNamespace},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
			DeferCleanup(CleanResource, &service, service.Name, service.Namespace)

			By("checking the cert is issued into it, leaving the user's other keys alone")
			expectIssuedWithUUIDCommonName(key, &metav1.OwnerReference{Kind: "BrokerService", Name: serviceName})
			current := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, key, current)).Should(Succeed())
			Expect(current.Data).Should(HaveKeyWithValue("user", []byte("kept")))

			// a broker pod per entry is costly: the issuance path is the same for every cert
			if converges {
				By("checking the service converges, without touching the CR")
				Eventually(func(g Gomega) {
					current := &broker.BrokerService{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, current)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		},
		Entry("for the broker cert", common.DefaultOperandCertSecretName, true),
		Entry("for the prometheus cert", common.DefaultPrometheusCertSecretName, false),
	)
})

// removeOperatorCert deletes the operator cert and the Certificate behind it,
// whichever of them exist: manual mode specs install them, issuer mode issues
// them, and either may have stopped short.
func removeOperatorCert() {
	key := types.NamespacedName{Name: common.DefaultOperatorCertSecretName, Namespace: defaultNamespace}
	for _, obj := range []client.Object{&cmv1.Certificate{}, &corev1.Secret{}} {
		if k8sClient.Get(ctx, key, obj) == nil {
			CleanResource(obj, key.Name, key.Namespace)
		}
	}
}

// expectIssuedWithUUIDCommonName waits for the Certificate under key, owned by
// owner when one is given, to issue its secret a UUID common name. The secret
// is owned by the Certificate.
func expectIssuedWithUUIDCommonName(key types.NamespacedName, owner *metav1.OwnerReference) {
	Eventually(func(g Gomega) {
		certificate := &cmv1.Certificate{}
		g.Expect(k8sClient.Get(ctx, key, certificate)).Should(Succeed())
		g.Expect(certificate.Spec.IssuerRef.Kind).Should(Equal("ClusterIssuer"))
		g.Expect(certificate.Spec.IssuerRef.Name).Should(Equal(caIssuerName))
		if owner != nil {
			g.Expect(certificate.OwnerReferences).Should(ContainElement(SatisfyAll(
				HaveField("Kind", owner.Kind),
				HaveField("Name", owner.Name),
			)))
		}

		secret := &corev1.Secret{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certificate.Spec.SecretName, Namespace: key.Namespace}, secret)).Should(Succeed())
		g.Expect(secret.Annotations).Should(HaveKeyWithValue(cmv1.CertificateNameKey, key.Name))
		g.Expect(secret.OwnerReferences).Should(ContainElement(SatisfyAll(
			HaveField("Kind", cmv1.CertificateKind),
			HaveField("UID", certificate.UID),
		)))

		subject, err := common.ExtractCertSubjectFromSecret(secret)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(subject.CommonName).Should(Equal(certificate.Spec.CommonName))
		_, err = uuid.Parse(subject.CommonName)
		g.Expect(err).ShouldNot(HaveOccurred(), "common name %q is not a uuid", subject.CommonName)
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

const pkiPemcfgSecretName = "pki-issuer-mode-pemcfg"

// pkiProducerJob sends one message to queue, presenting the app cert, using the
// artemis client shipped in the broker image.
func pkiProducerJob(name string, appCertSecretName string, bindingSecretName string, queue string) *batchv1.Job {
	pemcfg := secrets.NewSecret(types.NamespacedName{Name: pkiPemcfgSecretName, Namespace: defaultNamespace}, map[string][]byte{
		"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
		"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
	}, nil)
	CreateOrOverwriteResource(pemcfg)

	fromBinding := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: bindingSecretName},
			Key:                  key,
		}}
	}
	secretVolume := func(volume string, secretName string) corev1.Volume {
		return corev1.Volume{Name: volume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secretName}}}
	}

	url := "amqps://${BROKER_SERVICE_HOST}:${BROKER_SERVICE_PORT}" +
		"?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/" + caPemTrustStoreName +
		"\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg"

	return &batchv1.Job{
		TypeMeta:   metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  name,
						Image: version.LatestKubeImage,
						Command: []string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* " +
							"org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --user p --password passwd" +
							" --url " + url + " --message-count 1 --destination queue://" + queue + ";"},
						Env: []corev1.EnvVar{
							{Name: "JDK_JAVA_OPTIONS", Value: "-Djava.security.properties=/app/tls/pem/java.security"},
							{Name: "BROKER_SERVICE_HOST", ValueFrom: fromBinding("host")},
							{Name: "BROKER_SERVICE_PORT", ValueFrom: fromBinding("port")},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "trust", MountPath: "/app/tls/ca"},
							{Name: "cert", MountPath: "/app/tls/client"},
							{Name: "pem", MountPath: "/app/tls/pem"},
						},
					}},
					Volumes: []corev1.Volume{
						secretVolume("trust", common.DefaultOperatorCASecretName),
						secretVolume("cert", appCertSecretName),
						secretVolume("pem", pkiPemcfgSecretName),
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
}
