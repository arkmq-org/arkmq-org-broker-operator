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
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/version"
)

var _ = Describe("broker-service-poc", func() {

	var installedCertManager bool = false

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			//if cert manager/trust manager is not installed, install it
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
				installedCertManager = true
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
				candidate.Spec.CommonName = "activemq-artemis-operator"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

		}

	})

	AfterEach(func() {

		if false && os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			UnInstallCaBundle(common.DefaultOperatorCASecretName)
			UninstallClusteredIssuer(caIssuerName)
			UninstallCert(rootCert.Name, rootCert.Namespace)
			UninstallCert(common.DefaultOperatorCertSecretName, defaultNamespace)
			UninstallClusteredIssuer(rootIssuerName)

			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}
		AfterEachSpec()
	})

	Context("round trip simple", func() {

		It("non persistent", Label("verySlow"), func() {

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
				candidate.Spec.DNSNames = []string{serviceName, fmt.Sprintf("%s.%s", serviceName, defaultNamespace), fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()), common.ClusterDNSWildCard(serviceName, defaultNamespace)}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			prometheusCertName := common.DefaultPrometheusCertSecretName
			By("installing prometheus cert")
			InstallCert(prometheusCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = prometheusCertName
				candidate.Spec.CommonName = "prometheus"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			brokerImage := version.LatestKubeImage
			jvmRemoteDebug := false
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "production"},
				},
				Spec: broker.BrokerServiceSpec{

					Env: []corev1.EnvVar{
						{
							Name:  "JAVA_ARGS_APPEND",
							Value: "-Dlog4j2.level=DEBUG -Djava.security.debug=logincontext",
						},
					},
				},
			}

			crd.Spec.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			if jvmRemoteDebug {
				crd.Spec.Env = append(crd.Spec.Env,
					corev1.EnvVar{
						Name:  "JDK_JAVA_OPTIONS",
						Value: "-agentlib:jdwp=transport=dt_socket,server=y,suspend=y,address=5005",
					})
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			var debugService *corev1.Service = nil
			if jvmRemoteDebug {
				// minikube> kubectl port-forward svc/debug 5005:5005 --namespace test
				By("setup debug")
				debugService = &corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "debug",
						Namespace: defaultNamespace,
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
						Selector: map[string]string{
							"Broker": crd.Name,
						},
						Ports: []corev1.ServicePort{
							{
								Port: 5005,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, debugService)).Should(Succeed())
			}

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("Checking ready cr status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Service STATUS: %v\n\n", createdCrd.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("checking broker cr status insync - should be in the service if important to user")
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			brokerCrd := &broker.Broker{}

			var appPropsRv = ""
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Broker STATUS: %v\n\n", brokerCrd.Status)
				}

				condition := meta.FindStatusCondition(brokerCrd.Status.Conditions, broker.ConfigAppliedConditionType)
				g.Expect(condition).NotTo(BeNil())

				for _, externalConfig := range brokerCrd.Status.ExternalConfigs {
					if externalConfig.Name == AppPropertiesSecretName(brokerCrd.Name) {
						appPropsRv = externalConfig.ResourceVersion
					}
				}
				g.Expect(appPropsRv).ShouldNot(BeEmpty())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying a matching app")
			appName := "first-app" // a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character (e.g. 'example.com', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*')",
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
						MatchLabels: map[string]string{
							"env": "production",
						}},

					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP.JOBS"}},
							ConsumerOf: []broker.AddressRef{{Address: "APP.JOBS"}},
						},
						{
							ProducerOf: []broker.AddressRef{{Address: "APP.COMMANDS"}},
							SubscriberOf: []broker.AddressRef{

								// jms consumer queue of the form <address>::<connection client id>.<subscription name>
								{Address: `APP.COMMANDS::client-1.sub-1`},
								{Address: `APP.COMMANDS::client-2.sub-2`},
							},
						},
					},

					// Some Resource requirement, that needs to be satisified by matched service
				},
			}

			appCertName := app.Name + common.AppCertSecretSuffix
			By("installing app client cert")
			InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = appCertName
				candidate.Spec.CommonName = app.Name
				candidate.Spec.Subject.Organizations = nil
				candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("Deploying the App " + app.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verify app status")
			appKey := types.NamespacedName{Name: app.Name, Namespace: crd.Namespace}
			createdApp := &broker.BrokerApp{}
			var bindingSecretNameFromAppStatus string

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())

				if verbose {
					fmt.Printf("App STATUS: %v\n\n", createdApp.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
				bindingSecretNameFromAppStatus = createdApp.Status.Service.Secret

				// Verify a port was auto-assigned
				g.Expect(createdApp.Status.Service.AssignedPort).ShouldNot(BeZero())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("checking broker cr status insync after app add - should be in the service if important to user")
			var appPropsRvUpdated = ""
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Broker STATUS: %v\n\n", brokerCrd.Status)
				}

				for _, externalConfig := range brokerCrd.Status.ExternalConfigs {
					if externalConfig.Name == AppPropertiesSecretName(brokerCrd.Name) {
						appPropsRvUpdated = externalConfig.ResourceVersion
					}
				}
				g.Expect(appPropsRvUpdated).ShouldNot(BeEmpty())

				g.Expect(appPropsRvUpdated).ShouldNot(Equal(appPropsRv))
				appPropsRv = appPropsRvUpdated // reset

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("exercise app resource")
			By("connect as app client via amqp mtls from inside the cluster")

			By("provisioning pemcfg secret for app client cert")

			serviceHostEnvVar := "BROKER_SERVICE_HOST"
			servicePortEnvVar := "BROKER_SERVICE_PORT"
			appClientPemcfgSecretName := "cert-pemcfg"
			appClientPemcfgKey := types.NamespacedName{Name: appClientPemcfgSecretName, Namespace: defaultNamespace}
			appClientPemcfgSecret := secrets.NewSecret(appClientPemcfgKey, map[string][]byte{
				"tls.pemcfg": []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
				// TODO: using 6, but it seems it must be n+1, and not 25 etc,
				// where n is the existing list, which I guess won't be a constant either
				"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
			}, nil)
			Expect(k8sClient.Create(ctx, appClientPemcfgSecret, &client.CreateOptions{})).Should(Succeed())

			By("provisioning an app, publisher and consumers, using the broker image to access the artemis client from within the cluster")
			jobTemplate := func(name string, replicas int32, command []string) batchv1.Job {
				appLables := map[string]string{"app": name}
				return batchv1.Job{

					TypeMeta:   metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace, Labels: appLables},
					Spec: batchv1.JobSpec{
						Parallelism: common.Int32ToPtr(replicas),
						Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: appLables},
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
											{
												Name:      "trust",
												MountPath: "/app/tls/ca",
											},
											{
												Name:      "cert",
												MountPath: "/app/tls/client",
											},
											{
												Name:      "pem",
												MountPath: "/app/tls/pem",
											},
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
							}},
					},
				}
			}

			buf := &bytes.Buffer{}
			fmt.Fprintf(buf, "amqps://${%s}:${%s}", serviceHostEnvVar, servicePortEnvVar)
			fmt.Fprintf(buf, "?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/ca.pem")
			fmt.Fprintf(buf, "\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg")

			serviceUrl := buf.String()

			By("deploying single producer to send one message")
			producer := jobTemplate(
				"producer",
				1,
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --user p --password passwd --url " + serviceUrl + " --message-count 1 --destination queue://APP.JOBS;"},
			)
			Expect(k8sClient.Create(ctx, &producer)).Should(Succeed())

			By("getting producer job status")
			producerKey := types.NamespacedName{Name: producer.Name, Namespace: crd.Namespace}
			producerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, producerKey, producerJob)).Should(Succeed())

				if verbose {
					fmt.Printf("Producer job STATUS: %v\n\n", producerJob.Status)
				}
				g.Expect(producerJob.Status.Succeeded).Should(BeNumerically("==", 1))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying consumer")
			consumer := jobTemplate(
				"consumer",
				1,
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --user p --password passwd --url " + serviceUrl + " --message-count 1 --destination queue://APP.JOBS;"},
			)
			Expect(k8sClient.Create(ctx, &consumer)).Should(Succeed())

			By("getting consumer job status")
			consumerKey := types.NamespacedName{Name: consumer.Name, Namespace: crd.Namespace}
			consumerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, consumerKey, consumerJob)).Should(Succeed())

				if verbose {
					fmt.Printf("Consumer job STATUS: %v\n\n", consumerJob.Status)
				}
				g.Expect(consumerJob.Status.Succeeded).Should(BeNumerically("==", 1))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying pub sub")

			By("deploying durable subscribers")
			clientId := "client-1"
			subName := "sub-1"

			subscriber1 := jobTemplate(
				clientId,
				1,
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url " + serviceUrl + " --message-count=1 --durable --clientID=" + clientId + " --subscriptionName=" + subName + " --destination topic://APP.COMMANDS;"},
			)
			Expect(k8sClient.Create(ctx, &subscriber1)).Should(Succeed())

			clientId = "client-2"
			subName = "sub-2"
			subscriber2 := jobTemplate(
				clientId,
				1,
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url " + serviceUrl + " --message-count=1 --durable --clientID=" + clientId + " --subscriptionName=" + subName + " --destination topic://APP.COMMANDS;"},
			)
			Expect(k8sClient.Create(ctx, &subscriber2)).Should(Succeed())

			// may need a delay or check stats to see active subs

			publisher := jobTemplate(
				"publisher",
				1,
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url " + serviceUrl + " --message-count 1 --destination topic://APP.COMMANDS;exit $?"},
			)
			Expect(k8sClient.Create(ctx, &publisher)).Should(Succeed())

			By("Verifying stats...")
			// TODO

			By("updating app")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				createdApp.Spec.Capabilities = append(createdApp.Spec.Capabilities, broker.AppCapabilityType{
					ProducerOf: []broker.AddressRef{
						{
							Address: "brian",
						},
					},
				})
				g.Expect(k8sClient.Update(ctx, createdApp)).Should(Succeed())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("checking broker cr status insync after update")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Broker STATUS: %v\n\n", brokerCrd.Status)
				}

				for _, externalConfig := range brokerCrd.Status.ExternalConfigs {
					if externalConfig.Name == AppPropertiesSecretName(brokerCrd.Name) {
						appPropsRvUpdated = externalConfig.ResourceVersion
					}
				}
				g.Expect(appPropsRvUpdated).ShouldNot(BeEmpty())

				g.Expect(appPropsRvUpdated).ShouldNot(Equal(appPropsRv))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("removing app")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())

			appPropsRv = appPropsRvUpdated
			By("checking broker cr status insync after remove")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Broker STATUS: %v\n\n", brokerCrd.Status)
				}

				for _, externalConfig := range brokerCrd.Status.ExternalConfigs {
					if externalConfig.Name == AppPropertiesSecretName(brokerCrd.Name) {
						appPropsRvUpdated = externalConfig.ResourceVersion
					}
				}
				g.Expect(appPropsRvUpdated).ShouldNot(BeEmpty())

				g.Expect(appPropsRvUpdated).ShouldNot(Equal(appPropsRv))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("tidy up")
			cascade_foreground_policy := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, producerJob, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, consumerJob, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &subscriber1, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &subscriber2, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &publisher, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())

			Expect(k8sClient.Delete(ctx, appClientPemcfgSecret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())

			if jvmRemoteDebug {
				Expect(k8sClient.Delete(ctx, debugService)).Should(Succeed())
			}

			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

	Context("prometheus queue metrics for app queues", func() {

		It("exposes ConsumerCount and MessageCount for app queues", func() {

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
				candidate.Spec.DNSNames = []string{serviceName, fmt.Sprintf("%s.%s", serviceName, defaultNamespace), fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()), common.ClusterDNSWildCard(serviceName, defaultNamespace)}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			prometheusCertName := common.DefaultPrometheusCertSecretName
			By("installing prometheus cert")
			InstallCert(prometheusCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = prometheusCertName
				candidate.Spec.CommonName = "prometheus"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			brokerImage := version.LatestKubeImage
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "metrics-test"},
				},
				Spec: broker.BrokerServiceSpec{
					Image: &brokerImage,
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}

			By("Deploying the BrokerService " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("Checking ready cr status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Service STATUS: %v\n\n", createdCrd.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying an app with specific queues in ConsumerOf")
			appName := "metrics-test-app"
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
						MatchLabels: map[string]string{
							"env": "metrics-test",
						}},

					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{Address: "METRICS.QUEUE.ONE"},
								{Address: "METRICS.QUEUE.TWO"},
							},
							ProducerOf: []broker.AddressRef{
								{Address: "METRICS.QUEUE.ONE"},
								{Address: "METRICS.QUEUE.TWO"},
							},
						},
					},
				},
			}

			appCertName := app.Name + common.AppCertSecretSuffix
			By("installing app client cert")
			InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = appCertName
				candidate.Spec.CommonName = app.Name
				candidate.Spec.Subject.Organizations = nil
				candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("Deploying the App " + app.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verify app status becomes ready")
			appKey := types.NamespacedName{Name: app.Name, Namespace: crd.Namespace}
			createdApp := &broker.BrokerApp{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())

				if verbose {
					fmt.Printf("App STATUS: %v\n\n", createdApp.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying control-plane-override secret exists with prometheus config")
			overrideSecretName := serviceName + "-control-plane-override"
			overrideSecretKey := types.NamespacedName{Name: overrideSecretName, Namespace: defaultNamespace}
			overrideSecret := &corev1.Secret{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, overrideSecretKey, overrideSecret)).Should(Succeed())

				// Verify the secret contains the prometheus exporter config
				prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
				g.Expect(ok).Should(BeTrue(), "should have _prometheus_exporter.yaml key")

				prometheusConfig := string(prometheusYaml)
				if verbose {
					fmt.Printf("Prometheus config:\n%s\n", prometheusConfig)
				}

				// Verify it includes queue-level metrics for the app queues
				g.Expect(prometheusConfig).Should(ContainSubstring("METRICS.QUEUE.ONE"), "should include METRICS.QUEUE.ONE")
				g.Expect(prometheusConfig).Should(ContainSubstring("METRICS.QUEUE.TWO"), "should include METRICS.QUEUE.TWO")
				g.Expect(prometheusConfig).Should(ContainSubstring("MessageCount"), "should include MessageCount attribute")
				g.Expect(prometheusConfig).Should(ContainSubstring("ConsumerCount"), "should include ConsumerCount attribute")

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker picks up the override and applies it")
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			brokerCrd := &broker.Broker{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("Broker STATUS: %v\n\n", brokerCrd.Status)
				}

				g.Expect(meta.IsStatusConditionTrue(brokerCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(brokerCrd.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("scraping prometheus metrics and verifying queue-level metrics are exposed")
			serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

			Eventually(func(g Gomega) {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				httpClient := http.Client{
					Transport: transport,
					Timeout:   time.Second * 5,
				}

				httpClientTransport := httpClient.Transport.(*http.Transport)
				httpClientTransport.TLSClientConfig = &tls.Config{
					ServerName:         serverName,
					InsecureSkipVerify: false,
				}
				httpClientTransport.TLSClientConfig.GetClientCertificate =
					func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
						return common.GetOperatorClientCertificate(k8sClient, cri)
					}

				if rootCas, err := common.GetRootCAs(k8sClient); err == nil {
					httpClientTransport.TLSClientConfig.RootCAs = rootCas
				}

				resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
				g.Expect(err).Should(Succeed())

				if resp != nil {
					fmt.Printf("Prometheus metrics scrape: status=%d\n", resp.StatusCode)
					g.Expect(resp.StatusCode).Should(Equal(200))

					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					g.Expect(err).Should(Succeed())

					bodyStr := string(body)
					if verbose {
						fmt.Printf("Metrics response (first 20000 chars):\n%s\n", bodyStr[:min(20000, len(bodyStr))])
					}

					// Verify queue-level metrics for app queues are present
					g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_message_count.*queue="METRICS\.QUEUE\.ONE"`), "should have MessageCount for METRICS.QUEUE.ONE")
					g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_message_count.*queue="METRICS\.QUEUE\.TWO"`), "should have MessageCount for METRICS.QUEUE.TWO")
					g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_consumer_count.*queue="METRICS\.QUEUE\.ONE"`), "should have ConsumerCount for METRICS.QUEUE.ONE")
					g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_consumer_count.*queue="METRICS\.QUEUE\.TWO"`), "should have ConsumerCount for METRICS.QUEUE.TWO")
				}

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("scraping prometheus metrics with app client cert and verifying access")
			Eventually(func(g Gomega) {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				httpClient := http.Client{
					Transport: transport,
					Timeout:   time.Second * 5,
				}

				httpClientTransport := httpClient.Transport.(*http.Transport)
				httpClientTransport.TLSClientConfig = &tls.Config{
					ServerName:         serverName,
					InsecureSkipVerify: false,
				}

				// Get app client certificate from secret
				appCertSecret := &corev1.Secret{}
				appCertSecretKey := types.NamespacedName{Name: appCertName, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(ctx, appCertSecretKey, appCertSecret)).Should(Succeed())

				certPEM := appCertSecret.Data["tls.crt"]
				keyPEM := appCertSecret.Data["tls.key"]
				g.Expect(certPEM).ShouldNot(BeEmpty())
				g.Expect(keyPEM).ShouldNot(BeEmpty())

				cert, err := tls.X509KeyPair(certPEM, keyPEM)
				g.Expect(err).Should(Succeed())

				httpClientTransport.TLSClientConfig.Certificates = []tls.Certificate{cert}

				if rootCas, err := common.GetRootCAs(k8sClient); err == nil {
					httpClientTransport.TLSClientConfig.RootCAs = rootCas
				}

				resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
				g.Expect(err).Should(Succeed())

				if resp != nil {
					fmt.Printf("Prometheus metrics scrape with app cert: status=%d\n", resp.StatusCode)
					g.Expect(resp.StatusCode).Should(Equal(401))

					// need to update the control plane cert users/roles -
					// will avoid this by using ou's in generated control plane certs.
					// needs: https://issues.apache.org/jira/browse/ARTEMIS-5959
					// then we can work the 200 ok
					/*
						defer resp.Body.Close()
						body, err := io.ReadAll(resp.Body)
						g.Expect(err).Should(Succeed())

						bodyStr := string(body)
						if verbose {
							fmt.Printf("Metrics response with app cert (first 20000 chars):\n%s\n", bodyStr[:min(20000, len(bodyStr))])
						}

						// Verify queue-level metrics for app queues are present with app cert too
						g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_message_count.*queue="METRICS\.QUEUE\.ONE"`), "should have MessageCount for METRICS.QUEUE.ONE")
						g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_consumer_count.*queue="METRICS\.QUEUE\.ONE"`), "should have ConsumerCount for METRICS.QUEUE.ONE")
					*/
				}

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("tidy up")
			cascade_foreground_policy := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, createdApp, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())

			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

})
