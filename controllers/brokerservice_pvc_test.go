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
// +kubebuilder:docs-gen:collapse=Apache License

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	mgmt "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/artemis"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

var _ = Describe("broker-service-pvc", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

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

			By("installing operator cert")
			InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
				candidate.Spec.CommonName = "arkmq-org-broker-operator"
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

	Context("persistent broker service", func() {

		It("creates a PVC and messages survive a pod restart", Label("slow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()

			serviceName := NextSpecResourceName()

			sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
			By("installing broker cert")
			InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sharedOperandCertName
				candidate.Spec.CommonName = serviceName
				candidate.Spec.DNSNames = []string{
					serviceName,
					fmt.Sprintf("%s.%s", serviceName, defaultNamespace),
					fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()),
					common.ClusterDNSWildCard(serviceName, defaultNamespace),
				}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("deploying BrokerService with persistent storage")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"app": serviceName},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory:         resource.MustParse("1Gi"),
							broker.ResourceJournalStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for BrokerService to reach Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				if verbose {
					fmt.Printf("Service STATUS: %v\n\n", createdCrd.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying child Broker CR has a journal claim template and mount")
			brokerCrd := &broker.Broker{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, brokerCrd)).Should(Succeed())
				g.Expect(brokerCrd.Spec.ExtraVolumeClaimTemplates).Should(HaveLen(1))
				journalClaim := brokerCrd.Spec.ExtraVolumeClaimTemplates[0]
				g.Expect(journalClaim.Name).Should(Equal("journal"))
				storageRequest := journalClaim.Spec.Resources.Requests[corev1.ResourceStorage]
				g.Expect(storageRequest.String()).Should(Equal("1Gi"))
				g.Expect(brokerCrd.Spec.ExtraVolumeMounts).Should(HaveLen(1))
				g.Expect(brokerCrd.Spec.ExtraVolumeMounts[0].Name).Should(Equal("journal"))
				g.Expect(brokerCrd.Spec.ExtraVolumeMounts[0].MountPath).Should(Equal("/app/data"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying StatefulSet has VolumeClaimTemplates")
			ssKey := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: crd.Namespace}
			ss := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, ss)).Should(Succeed())
				g.Expect(ss.Spec.VolumeClaimTemplates).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC is created and bound")
			// PVC name pattern: <claim-template-name>-<ss-name>-<ordinal>
			// e.g. journal-mysvc-ss-0
			pvcName := "journal-" + namer.CrToSS(crd.Name) + "-0"
			pvcKey := types.NamespacedName{Name: pvcName, Namespace: crd.Namespace}
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			originalPVCUID := pvc.UID
			originalPVName := pvc.Spec.VolumeName

			By("verifying the PVC has no owner so deleting the CR does not delete the journal")
			Expect(pvc.OwnerReferences).Should(BeEmpty())

			By("verifying the journal claim is mounted at /app/data in the broker pod")
			brokerPodName := namer.CrToSS(crd.Name) + "-0"
			Eventually(func(g Gomega) {
				mounts := ExecOnPod(brokerPodName, crd.Name, crd.Namespace, []string{"cat", "/proc/self/mountinfo"}, g)
				g.Expect(mounts).Should(ContainSubstring(" /app/data "))
				dataDir := ExecOnPod(brokerPodName, crd.Name, crd.Namespace, []string{"ls", "/app/data"}, g)
				g.Expect(dataDir).Should(ContainSubstring("bindings"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying BrokerApp to produce and consume messages on an anycast queue")
			appName := NextSpecResourceName()
			appCertName := appName + common.AppCertSecretSuffix
			By("installing app client cert")
			InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = appCertName
				candidate.Spec.CommonName = appName
				candidate.Spec.Subject.Organizations = nil
				candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			appClientPemcfgSecretName := appName + "-pemcfg"
			appClientPemcfgKey := types.NamespacedName{Name: appClientPemcfgSecretName, Namespace: defaultNamespace}
			appClientPemcfgSecret := secrets.NewSecret(appClientPemcfgKey, map[string][]byte{
				"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
				"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
			}, nil)
			Expect(k8sClient.Create(ctx, appClientPemcfgSecret, &client.CreateOptions{})).Should(Succeed())

			app := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": serviceName},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{
									Address: "APP.PERSISTENT.QUEUE",
								},
							},
							ConsumerOf: []broker.AddressRef{
								{
									Address: "APP.PERSISTENT.QUEUE",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("waiting for BrokerApp to reach Ready")
			appKey := types.NamespacedName{Name: app.Name, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}
			var bindingSecretNameFromAppStatus string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
				bindingSecretNameFromAppStatus = createdApp.Status.Service.Secret
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			serviceHostEnvVar := "BROKER_SERVICE_HOST"
			servicePortEnvVar := "BROKER_SERVICE_PORT"
			boolFalse := false
			brokerImage := version.LatestKubeImage

			jobTemplate := func(name string, command []string) batchv1.Job {
				appLabels := map[string]string{"app": name}
				return batchv1.Job{
					TypeMeta:   metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace, Labels: appLabels},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: appLabels},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    name,
										Image:   brokerImage,
										Command: command,
										Env: []corev1.EnvVar{
											{
												Name:  "JDK_JAVA_OPTIONS",
												Value: "-Djava.security.properties=/app/tls/pem/java.security",
											},
											{
												Name: serviceHostEnvVar,
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: bindingSecretNameFromAppStatus,
														},
														Key:      "host",
														Optional: &boolFalse,
													},
												},
											},
											{
												Name: servicePortEnvVar,
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: bindingSecretNameFromAppStatus,
														},
														Key:      "port",
														Optional: &boolFalse,
													},
												},
											},
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "trust", MountPath: "/app/tls/ca"},
											{Name: "cert", MountPath: "/app/tls/client"},
											{Name: "pem", MountPath: "/app/tls/pem"},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: "trust",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: common.DefaultOperatorCASecretName,
											},
										},
									},
									{
										Name: "cert",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: appCertName,
											},
										},
									},
									{
										Name: "pem",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: appClientPemcfgSecretName,
											},
										},
									},
								},
								RestartPolicy: corev1.RestartPolicyOnFailure,
							},
						},
					},
				}
			}

			buf := &bytes.Buffer{}
			fmt.Fprintf(buf, "amqps://${%s}:${%s}", serviceHostEnvVar, servicePortEnvVar)
			fmt.Fprintf(buf, "?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/ca.pem")
			fmt.Fprintf(buf, "\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg")
			serviceUrl := buf.String()

			By("sending messages to an anycast queue before pod restart")
			producer := jobTemplate(
				appName+"-producer",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url " + serviceUrl + " --message-count 5 --destination queue://APP.PERSISTENT.QUEUE;"},
			)
			Expect(k8sClient.Create(ctx, &producer)).Should(Succeed())

			producerKey := types.NamespacedName{Name: producer.Name, Namespace: defaultNamespace}
			producerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, producerKey, producerJob)).Should(Succeed())
				g.Expect(producerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deleting the broker pod to trigger a restart")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(crd.Namespace),
				client.MatchingLabels{selectors.LabelBrokerService: crd.Name},
			)).Should(Succeed())
			Expect(podList.Items).ShouldNot(BeEmpty())
			Expect(k8sClient.Delete(ctx, &podList.Items[0])).Should(Succeed())

			By("waiting for replacement pod to be Ready")
			Eventually(func(g Gomega) {
				replacementPods := &corev1.PodList{}
				g.Expect(k8sClient.List(ctx, replacementPods,
					client.InNamespace(crd.Namespace),
					client.MatchingLabels{selectors.LabelBrokerService: crd.Name},
				)).Should(Succeed())
				g.Expect(replacementPods.Items).ShouldNot(BeEmpty())
				ready := false
				for _, c := range replacementPods.Items[0].Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						ready = true
					}
				}
				g.Expect(ready).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC remains Bound to the same PV after pod restart")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))
				g.Expect(pvc.UID).Should(Equal(originalPVCUID))
				g.Expect(pvc.Spec.VolumeName).Should(Equal(originalPVName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying messages persisted across pod restart via Jolokia")
			podFQDN := common.OrdinalStringFQDNS(crd.Name, crd.Namespace, "0")
			artemisAgent := mgmt.GetArtemisAgentForRestricted(k8sClient, crd.Name, podFQDN)
			Eventually(func(g Gomega) {
				count, err := artemisAgent.GetTotalMessageCount()
				g.Expect(err).To(BeNil())
				g.Expect(count).To(Equal("5"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("consuming messages from the queue after pod restart to verify persistence")
			consumer := jobTemplate(
				appName+"-consumer",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url " + serviceUrl + " --message-count 5 --destination queue://APP.PERSISTENT.QUEUE;"},
			)
			Expect(k8sClient.Create(ctx, &consumer)).Should(Succeed())

			consumerKey := types.NamespacedName{Name: consumer.Name, Namespace: defaultNamespace}
			consumerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, consumerKey, consumerJob)).Should(Succeed())
				g.Expect(consumerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying queue is empty after consumption via Jolokia")
			Eventually(func(g Gomega) {
				count, err := artemisAgent.GetTotalMessageCount()
				g.Expect(err).To(BeNil())
				g.Expect(count).To(Equal("0"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC survives deletion of BrokerService")
			CleanResource(createdCrd, createdCrd.Name, createdCrd.Namespace)
			survivingPvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, pvcKey, survivingPvc)).To(Succeed())
			Expect(survivingPvc.Status.Phase).To(Equal(corev1.ClaimBound))
			Expect(survivingPvc.UID).To(Equal(originalPVCUID))
			Expect(survivingPvc.Spec.VolumeName).To(Equal(originalPVName))

			By("tidy up")
			_ = k8sClient.Delete(ctx, survivingPvc)
			CleanResource(&producer, producer.Name, defaultNamespace)
			CleanResource(&consumer, consumer.Name, defaultNamespace)
			CleanResource(&app, app.Name, defaultNamespace)
			_ = k8sClient.Delete(ctx, appClientPemcfgSecret)
			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("propagates journalStorageClass to StatefulSet VolumeClaimTemplate", Label("slow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()

			serviceName := NextSpecResourceName()

			sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
			By("installing broker cert")
			InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sharedOperandCertName
				candidate.Spec.CommonName = serviceName
				candidate.Spec.DNSNames = []string{
					serviceName,
					fmt.Sprintf("%s.%s", serviceName, defaultNamespace),
					fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()),
					common.ClusterDNSWildCard(serviceName, defaultNamespace),
				}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("deploying BrokerService with journalStorageClass set")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory:         resource.MustParse("1Gi"),
							broker.ResourceJournalStorage: resource.MustParse("1Gi"),
						},
					},
					JournalStorageClass: "standard",
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for BrokerService to reach Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying child Broker CR journal claim template has correct storageClassName from journalStorageClass")
			brokerCrd := &broker.Broker{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, brokerCrd)).Should(Succeed())
				g.Expect(brokerCrd.Spec.ExtraVolumeClaimTemplates).Should(HaveLen(1))
				journalClaim := brokerCrd.Spec.ExtraVolumeClaimTemplates[0]
				storageRequest := journalClaim.Spec.Resources.Requests[corev1.ResourceStorage]
				g.Expect(storageRequest.String()).Should(Equal("1Gi"))
				g.Expect(journalClaim.Spec.StorageClassName).ShouldNot(BeNil())
				g.Expect(*journalClaim.Spec.StorageClassName).Should(Equal("standard"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying StatefulSet VolumeClaimTemplate has correct storageClassName from journalStorageClass")
			ssKey := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: crd.Namespace}
			ss := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, ss)).Should(Succeed())
				g.Expect(ss.Spec.VolumeClaimTemplates).ShouldNot(BeEmpty())
				g.Expect(ss.Spec.VolumeClaimTemplates[0].Spec.StorageClassName).ShouldNot(BeNil())
				g.Expect(*ss.Spec.VolumeClaimTemplates[0].Spec.StorageClassName).Should(Equal("standard"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("tidy up")
			CleanResource(createdCrd, createdCrd.Name, createdCrd.Namespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("persists messages across broker pod restart on an XFS-backed journal volume", Label("slow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			xfsStorageClass := os.Getenv("XFS_STORAGE_CLASS")
			if xfsStorageClass == "" {
				By("skipping XFS test: XFS_STORAGE_CLASS env var is not set")
				return
			}

			ctx := context.Background()

			serviceName := NextSpecResourceName()

			sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
			By("installing broker cert")
			InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sharedOperandCertName
				candidate.Spec.CommonName = serviceName
				candidate.Spec.DNSNames = []string{
					serviceName,
					fmt.Sprintf("%s.%s", serviceName, defaultNamespace),
					fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()),
					common.ClusterDNSWildCard(serviceName, defaultNamespace),
				}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("deploying BrokerService with XFS storage class")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"app": serviceName},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory:         resource.MustParse("1Gi"),
							broker.ResourceJournalStorage: resource.MustParse("1Gi"),
						},
					},
					JournalStorageClass: xfsStorageClass,
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			pvcName := "journal-" + namer.CrToSS(crd.Name) + "-0"
			brokerPodName := namer.CrToSS(crd.Name) + "-0"

			DeferCleanup(func() {
				CleanResource(&crd, crd.Name, crd.Namespace)
				pvcToDelete := &corev1.PersistentVolumeClaim{}
				if k8sClient.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: crd.Namespace}, pvcToDelete) == nil {
					if pvName := pvcToDelete.Spec.VolumeName; pvName != "" {
						_ = k8sClient.Delete(ctx, pvcToDelete)
						pv := &corev1.PersistentVolume{}
						if k8sClient.Get(ctx, types.NamespacedName{Name: pvName}, pv) == nil {
							pv.Spec.ClaimRef = nil
							_ = k8sClient.Update(ctx, pv)
						}
					} else {
						_ = k8sClient.Delete(ctx, pvcToDelete)
					}
				}
				UninstallCert(sharedOperandCertName, defaultNamespace)
			})

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for BrokerService to reach Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC is created and bound to XFS storage class")
			pvcKey := types.NamespacedName{Name: pvcName, Namespace: crd.Namespace}
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))
				g.Expect(pvc.Spec.StorageClassName).ShouldNot(BeNil())
				g.Expect(*pvc.Spec.StorageClassName).Should(Equal(xfsStorageClass))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			originalPVCUID := pvc.UID
			originalPVName := pvc.Spec.VolumeName

			By("verifying the filesystem type is XFS on /app/data mount in the broker pod")
			Eventually(func(g Gomega) {
				mounts := ExecOnPod(brokerPodName, crd.Name, crd.Namespace, []string{"df", "-T", "/app/data"}, g)
				g.Expect(mounts).Should(ContainSubstring("xfs"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying BrokerApp to produce and consume messages on an XFS-backed queue")
			appName := NextSpecResourceName()
			appCertName := appName + common.AppCertSecretSuffix
			By("installing app client cert")
			InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = appCertName
				candidate.Spec.CommonName = appName
				candidate.Spec.Subject.Organizations = nil
				candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			appClientPemcfgSecretName := appName + "-pemcfg-xfs"
			appClientPemcfgKey := types.NamespacedName{Name: appClientPemcfgSecretName, Namespace: defaultNamespace}
			appClientPemcfgSecret := secrets.NewSecret(appClientPemcfgKey, map[string][]byte{
				"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
				"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
			}, nil)
			Expect(k8sClient.Create(ctx, appClientPemcfgSecret, &client.CreateOptions{})).Should(Succeed())

			app := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": serviceName},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{
									Address: "APP.XFS.JOURNAL",
								},
							},
							ConsumerOf: []broker.AddressRef{
								{
									Address: "APP.XFS.JOURNAL",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			DeferCleanup(func() {
				CleanResource(&app, app.Name, defaultNamespace)
				_ = k8sClient.Delete(ctx, appClientPemcfgSecret)
				UninstallCert(appCertName, defaultNamespace)
			})

			By("waiting for BrokerApp to reach Ready")
			appKey := types.NamespacedName{Name: app.Name, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}
			var bindingSecretNameFromAppStatus string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
				bindingSecretNameFromAppStatus = createdApp.Status.Service.Secret
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			serviceHostEnvVar := "BROKER_SERVICE_HOST"
			servicePortEnvVar := "BROKER_SERVICE_PORT"
			boolFalse := false
			brokerImage := version.LatestKubeImage

			xfsJobTemplate := func(name string, command []string) batchv1.Job {
				appLabels := map[string]string{"app": name}
				return batchv1.Job{
					TypeMeta:   metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace, Labels: appLabels},
					Spec: batchv1.JobSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: appLabels},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    name,
										Image:   brokerImage,
										Command: command,
										Env: []corev1.EnvVar{
											{
												Name:  "JDK_JAVA_OPTIONS",
												Value: "-Djava.security.properties=/app/tls/pem/java.security",
											},
											{
												Name: serviceHostEnvVar,
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: bindingSecretNameFromAppStatus,
														},
														Key:      "host",
														Optional: &boolFalse,
													},
												},
											},
											{
												Name: servicePortEnvVar,
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: bindingSecretNameFromAppStatus,
														},
														Key:      "port",
														Optional: &boolFalse,
													},
												},
											},
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "trust", MountPath: "/app/tls/ca"},
											{Name: "cert", MountPath: "/app/tls/client"},
											{Name: "pem", MountPath: "/app/tls/pem"},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: "trust",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: common.DefaultOperatorCASecretName,
											},
										},
									},
									{
										Name: "cert",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: appCertName,
											},
										},
									},
									{
										Name: "pem",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: appClientPemcfgSecretName,
											},
										},
									},
								},
								RestartPolicy: corev1.RestartPolicyOnFailure,
							},
						},
					},
				}
			}

			buf := &bytes.Buffer{}
			fmt.Fprintf(buf, "amqps://${%s}:${%s}", serviceHostEnvVar, servicePortEnvVar)
			fmt.Fprintf(buf, "?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/ca.pem")
			fmt.Fprintf(buf, "\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg")
			serviceUrl := buf.String()

			By("producing 12 persistent messages to APP.XFS.JOURNAL on XFS storage")
			producer := xfsJobTemplate(
				appName+"-xfs-producer",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url " + serviceUrl + " --message-count 12 --destination queue://APP.XFS.JOURNAL;"},
			)
			Expect(k8sClient.Create(ctx, &producer)).Should(Succeed())
			DeferCleanup(func() { CleanResource(&producer, producer.Name, defaultNamespace) })

			producerKey := types.NamespacedName{Name: producer.Name, Namespace: defaultNamespace}
			producerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, producerKey, producerJob)).Should(Succeed())
				g.Expect(producerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying messages landed on queue via Jolokia")
			podFQDN := common.OrdinalStringFQDNS(crd.Name, crd.Namespace, "0")
			artemisAgent := mgmt.GetArtemisAgentForRestricted(k8sClient, crd.Name, podFQDN)
			Eventually(func(g Gomega) {
				count, err := artemisAgent.GetTotalMessageCount()
				g.Expect(err).To(BeNil())
				g.Expect(count).To(Equal("12"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("restarting the broker pod to verify XFS persistence")
			brokerPod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brokerPodName, Namespace: defaultNamespace}, brokerPod)).Should(Succeed())
			originalPodUID := brokerPod.UID
			Expect(k8sClient.Delete(ctx, brokerPod)).Should(Succeed())

			By("waiting for replacement broker pod to be Ready")
			Eventually(func(g Gomega) {
				freshPod := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brokerPodName, Namespace: defaultNamespace}, freshPod)).Should(Succeed())
				g.Expect(freshPod.UID).ShouldNot(Equal(originalPodUID))
				g.Expect(freshPod.DeletionTimestamp).Should(BeNil())
				ready := false
				for _, c := range freshPod.Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						ready = true
						break
					}
				}
				g.Expect(ready).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying the filesystem type is still XFS on /app/data mount in replacement broker pod")
			Eventually(func(g Gomega) {
				mounts := ExecOnPod(brokerPodName, crd.Name, crd.Namespace, []string{"df", "-T", "/app/data"}, g)
				g.Expect(mounts).Should(ContainSubstring("xfs"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC remains Bound after pod restart")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))
				g.Expect(pvc.UID).Should(Equal(originalPVCUID))
				g.Expect(pvc.Spec.VolumeName).Should(Equal(originalPVName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying messages persisted on XFS across pod restart via Jolokia")
			artemisAgent = mgmt.GetArtemisAgentForRestricted(k8sClient, crd.Name, podFQDN)
			Eventually(func(g Gomega) {
				count, err := artemisAgent.GetTotalMessageCount()
				g.Expect(err).To(BeNil())
				g.Expect(count).To(Equal("12"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("consuming 12 messages from APP.XFS.JOURNAL after pod restart")
			consumer := xfsJobTemplate(
				appName+"-xfs-consumer",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url " + serviceUrl + " --message-count 12 --destination queue://APP.XFS.JOURNAL;"},
			)
			Expect(k8sClient.Create(ctx, &consumer)).Should(Succeed())
			DeferCleanup(func() { CleanResource(&consumer, consumer.Name, defaultNamespace) })

			consumerKey := types.NamespacedName{Name: consumer.Name, Namespace: defaultNamespace}
			consumerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, consumerKey, consumerJob)).Should(Succeed())
				g.Expect(consumerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying queue is empty after consumption — zero message loss on XFS")
			Eventually(func(g Gomega) {
				count, err := artemisAgent.GetTotalMessageCount()
				g.Expect(err).To(BeNil())
				g.Expect(count).To(Equal("0"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC survives deletion of BrokerService")
			CleanResource(createdCrd, createdCrd.Name, createdCrd.Namespace)
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, serviceKey, &broker.BrokerService{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			survivingPvc := &corev1.PersistentVolumeClaim{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, survivingPvc)).To(Succeed())
				g.Expect(survivingPvc.Status.Phase).To(Equal(corev1.ClaimBound))
				g.Expect(survivingPvc.UID).To(Equal(originalPVCUID))
				g.Expect(survivingPvc.Spec.VolumeName).To(Equal(originalPVName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})
})
