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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

func installDrainGatePolicies(ctx context.Context) {
	data, err := os.ReadFile(filepath.Join("..", "config", "admission", "brokerapp_drain_gate.yaml"))
	Expect(err).NotTo(HaveOccurred())
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); err != nil {
			Expect(errors.Is(err, io.EOF)).To(BeTrue(), "%v", err)
			break
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := k8sClient.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
		DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), obj) })
	}
}

func expectDrainGateDenial(err error, contains string) {
	GinkgoHelper()
	Expect(err).To(HaveOccurred())
	Expect(apierrors.IsForbidden(err) || apierrors.IsInvalid(err)).To(BeTrue(), "expected admission denial, got %v", err)
	Expect(err.Error()).To(ContainSubstring(contains))
}

var _ = Describe("brokerapp drain gate", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
			}

			rootIssuer = InstallClusteredIssuer(rootIssuerName, nil)

			rootCert = InstallCert(rootCertName, rootCertNamespce, func(candidate *cmv1.Certificate) {
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

			InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
				candidate.Spec.CommonName = common.OperatorName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	It("refuses to drop a queue until its producer is revoked and its messages consumed", func() {

		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		installDrainGatePolicies(ctx)

		serviceName := NextSpecResourceName()
		appName := "drain-gate-app"
		address := "GATE.ORDERS"
		messages := 3

		sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
		InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = sharedOperandCertName
			candidate.Spec.CommonName = serviceName
			candidate.Spec.DNSNames = []string{serviceName, fmt.Sprintf("%s.%s", serviceName, defaultNamespace), fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()), common.ClusterDNSWildCard(serviceName, defaultNamespace)}
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
		})
		prometheusCertName := common.DefaultPrometheusCertSecretName
		InstallCert(prometheusCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = prometheusCertName
			candidate.Spec.CommonName = "prometheus"
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
		})
		appCertName := appName + common.AppCertSecretSuffix
		InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = appCertName
			candidate.Spec.CommonName = appName
			candidate.Spec.Subject.Organizations = nil
			candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
		})
		DeferCleanup(func() {
			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		By("deploying the service")
		service := &broker.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: defaultNamespace, Labels: map[string]string{"test": "drain-gate"}},
			Spec: broker.BrokerServiceSpec{Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			}},
		}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		serviceKey := client.ObjectKeyFromObject(service)

		By("deploying an app that owns, produces to and consumes from an address")
		app := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: defaultNamespace},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"test": "drain-gate"}},
				Addresses:       []broker.AddressType{{Address: address}},
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: address}},
					ConsumerOf: []broker.AddressRef{{Address: address}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		appKey := client.ObjectKeyFromObject(app)

		DeferCleanup(func() {
			force := func(obj client.Object) {
				if k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj) == nil {
					ann := obj.GetAnnotations()
					if ann == nil {
						ann = map[string]string{}
					}
					ann["broker.arkmq.org/allow-message-loss"] = "true"
					obj.SetAnnotations(ann)
					_ = k8sClient.Update(ctx, obj)
					_ = k8sClient.Delete(ctx, obj)
				}
			}
			force(&broker.BrokerApp{ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: defaultNamespace}})
			force(&broker.BrokerService{ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: defaultNamespace}})
		})

		current := &broker.BrokerApp{}
		drainOf := func(g Gomega) *broker.AddressDrainStatus {
			g.Expect(k8sClient.Get(ctx, appKey, current)).To(Succeed())
			g.Expect(current.Status.AddressesObservedGeneration).To(Equal(current.Generation))
			for i := range current.Status.Addresses {
				if current.Status.Addresses[i].Address == address {
					return &current.Status.Addresses[i]
				}
			}
			g.Expect(current.Status.Addresses).To(ContainElement(HaveField("Address", address)))
			return nil
		}

		By("waiting for the app to be ready and its drain state observed")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, appKey, current)).To(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).To(BeTrue())
			g.Expect(drainOf(g).Queues).To(ConsistOf(broker.QueueDrainStatus{Name: address, MessageCount: broker.QueueDrainUnknown}),
				"a queue the app may send to is not read")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("sending messages from inside the cluster")
		runArtemisClientJob(ctx, current, appCertName, "gate-producer",
			fmt.Sprintf("producer --protocol=AMQP --message-count %d --destination queue://%s", messages, address))

		withoutAddress := func(a *broker.BrokerApp) {
			a.Spec.Addresses = nil
			a.Spec.Capabilities = nil
		}
		mutate := func(change func(*broker.BrokerApp), opts ...client.UpdateOption) error {
			latest := &broker.BrokerApp{}
			Expect(k8sClient.Get(ctx, appKey, latest)).To(Succeed())
			change(latest)
			return k8sClient.Update(ctx, latest, opts...)
		}

		By("removing the address while the app may send to it is refused, dry run included")
		expectDrainGateDenial(mutate(withoutAddress, client.DryRunAll), address)
		expectDrainGateDenial(mutate(withoutAddress), address)
		expectDrainGateDenial(k8sClient.Delete(ctx, current, client.DryRunAll), address)

		By("revoking producerOf")
		Expect(mutate(func(a *broker.BrokerApp) {
			a.Spec.Capabilities[0].ProducerOf = nil
		})).To(Succeed())

		By("the operator reports the queued messages once the broker runs the new generation")
		Eventually(func(g Gomega) {
			g.Expect(drainOf(g).Queues).To(ConsistOf(broker.QueueDrainStatus{Name: address, MessageCount: int64(messages)}))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("removing the address while it holds messages is refused")
		expectDrainGateDenial(mutate(withoutAddress), "addresses still hold messages or grant send: "+address)
		expectDrainGateDenial(k8sClient.Delete(ctx, current), address)

		By("consumers drain the queue")
		runArtemisClientJob(ctx, current, appCertName, "gate-consumer",
			fmt.Sprintf("consumer --protocol=AMQP --message-count %d --destination queue://%s", messages, address))

		Eventually(func(g Gomega) {
			g.Expect(drainOf(g).Queues).To(ConsistOf(broker.QueueDrainStatus{Name: address, MessageCount: 0}))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("now the address can be removed, dry run first")
		Expect(mutate(withoutAddress, client.DryRunAll)).To(Succeed())
		Expect(mutate(withoutAddress)).To(Succeed())

		By("the service cannot be deleted while it provisions the app")
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, serviceKey, svc)).To(Succeed())
			g.Expect(svc.Status.ProvisionedApps).To(ContainElement(ContainSubstring(appName)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		expectDrainGateDenial(k8sClient.Delete(ctx, service, client.DryRunAll), appName)

		By("the app, holding no queue, can be deleted, then the service")
		Expect(k8sClient.Delete(ctx, current)).To(Succeed())
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, serviceKey, svc)).To(Succeed())
			g.Expect(k8sClient.Delete(ctx, svc)).To(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})
})

// runArtemisClientJob runs the artemis CLI as the app, over mTLS, and waits for it to succeed
func runArtemisClientJob(ctx context.Context, app *broker.BrokerApp, appCertName, name, args string) {
	GinkgoHelper()
	job := artemisClientJob(app, appCertName, name, args)
	Expect(k8sClient.Create(ctx, job)).To(Succeed())
	DeferCleanup(func() {
		_ = k8sClient.Delete(context.Background(), job, client.PropagationPolicy(metav1.DeletePropagationBackground))
	})

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
		g.Expect(job.Status.Succeeded).To(BeNumerically("==", 1))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

// artemisClientJob prepares a job running the artemis CLI as the app. Args
// without --url connect over AMQP.
func artemisClientJob(app *broker.BrokerApp, appCertName, name, args string) *batchv1.Job {
	GinkgoHelper()
	pemName := name + "-pemcfg"
	pem := secrets.NewSecret(types.NamespacedName{Name: pemName, Namespace: defaultNamespace}, map[string][]byte{
		"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
		"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
	}, nil)
	Expect(k8sClient.Create(context.Background(), pem)).To(Succeed())
	DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), pem) })

	if !strings.Contains(args, "--url") {
		args += " --url amqps://${BROKER_SERVICE_HOST}:${BROKER_SERVICE_PORT}" +
			"?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/ca.pem" +
			"\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg"
	}

	fromBinding := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: app.Status.Service.Secret},
			Key:                  key,
		}}
	}
	secretVolume := func(volume, secret string) corev1.Volume {
		return corev1.Volume{Name: volume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: secret}}}
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyOnFailure,
			Containers: []corev1.Container{{
				Name:    name,
				Image:   version.LatestKubeImage,
				Command: []string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis " + args},
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
				secretVolume("cert", appCertName),
				secretVolume("pem", pemName),
			},
		}}},
	}
}
