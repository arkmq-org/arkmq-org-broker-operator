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
	"context"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/configmaps"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/version"
)

var _ = Describe("jdbc fast failover", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("artemis", Label("slow"), func() {
		It("cr with db store", Label("verySlow"), func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("deploying db and service to unrestricted  default namespace")
				dbName := "pdb"
				var dbPort int32 = 5432

				dbAppLables := map[string]string{"app": dbName}
				db := appsv1.Deployment{

					TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
					// into unrestricted "default" namespace as requires run as root
					ObjectMeta: metav1.ObjectMeta{Name: dbName, Namespace: "default",
						Labels: dbAppLables,
					},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: dbAppLables},
						Replicas: common.Int32ToPtr(1),
						Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: dbAppLables},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  dbName,
										Image: "docker.io/postgres:16.2",
										Env: []corev1.EnvVar{
											{
												Name:  "POSTGRES_PASSWORD",
												Value: "postgres",
											},
										},
										Ports: []corev1.ContainerPort{
											{
												ContainerPort: dbPort,
											},
										},
										Resources: corev1.ResourceRequirements{
											Limits: map[corev1.ResourceName]resource.Quantity{
												corev1.ResourceCPU: {
													Format: "2",
												},
												corev1.ResourceMemory: {
													Format: "4Gi",
												},
											},
										},
										ReadinessProbe: &corev1.Probe{
											ProbeHandler: corev1.ProbeHandler{
												TCPSocket: &corev1.TCPSocketAction{
													Port: intstr.IntOrString{
														IntVal: dbPort,
													},
												},
											},
											InitialDelaySeconds: 10,
											PeriodSeconds:       5,
										},
									},
								},
							}},
					},
				}
				CreateOrOverwriteResource(&db)

				dbService := corev1.Service{
					TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "core/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: dbName, Namespace: "default"},
					Spec: corev1.ServiceSpec{
						Selector: dbAppLables,
						Ports: []corev1.ServicePort{
							{
								Name:     dbName,
								Protocol: "TCP",
								Port:     dbPort,
							},
						},
					},
				}
				CreateOrOverwriteResource(&dbService)

				By("verifying dbservice has a jdbc endpoint, db is ready!")
				Eventually(func(g Gomega) {

					endpoints := &corev1.Endpoints{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dbService.Name, Namespace: dbService.Namespace}, endpoints)).Should(Succeed())
					if verbose {
						fmt.Printf("\nDB endpoints:%v", endpoints.Subsets)
					}
					g.Expect(len(endpoints.Subsets)).Should(BeNumerically("==", 1))
					g.Expect(len(endpoints.Subsets[0].Addresses)).Should(BeNumerically("==", 1))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("deploying artemis")

				peerLabel := "fast-ha-peer"
				peerPrefix := NextSpecResourceName()
				ctx := context.Background()
				peerA := generateArtemisSpec(defaultNamespace)
				peerA.Name = peerPrefix + "-peer-a"

				peerA.Spec.Acceptors = []brokerv1beta1.AcceptorType{{Name: "tcp", Port: 61616, Expose: true}}

				if !isOpenshift {
					peerA.Spec.IngressDomain = defaultTestIngressDomain
				}

				peerA.Spec.DeploymentPlan.PersistenceEnabled = boolFalse
				peerA.Spec.DeploymentPlan.Clustered = &boolFalse

				peerA.Spec.DeploymentPlan.Labels = map[string]string{peerLabel: peerPrefix}

				peerA.Spec.Env = []corev1.EnvVar{
					{
						Name:  "ARTEMIS_EXTRA_LIBS",
						Value: "/amq/init/config/extra-libs",
					},
				}

				By("configuring the broker")
				peerA.Spec.BrokerProperties = []string{
					"addressConfigurations.JOBS.routingTypes=ANYCAST",
					"addressConfigurations.JOBS.queueConfigs.JOBS.routingType=ANYCAST",

					"criticalAnalyzer=false",
					"HAPolicyConfiguration=SHARED_STORE_PRIMARY",
					"storeConfiguration=DATABASE",
					"storeConfiguration.jdbcDriverClassName=org.postgresql.Driver",
					"storeConfiguration.jdbcConnectionUrl=jdbc:postgresql://" + dbName + ".default" + ":" + fmt.Sprintf("%d", dbPort) + "/postgres",
					"storeConfiguration.dataSourceProperties.username=postgres",
					"storeConfiguration.dataSourceProperties.password=postgres", // not in url to get logging redaction
					"storeConfiguration.jdbcLockRenewPeriodMillis=2000",
					"storeConfiguration.jdbcLockExpirationMillis=6000",
				}

				By("patching ss to add init container to download jdbc jar")

				kindMatchSs := "StatefulSet"
				peerA.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
					{
						Selector: &brokerv1beta1.ResourceSelector{
							Kind: &kindMatchSs,
						},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
							"kind": "StatefulSet",
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{
										"initContainers": []interface{}{
											map[string]interface{}{
												"name":  "download-jdbc-driver",
												"image": version.GetDefaultInitImage(),
												"command": []interface{}{
													"sh",
													"-c",
													"mkdir -p /amq/init/config/extra-libs && curl -o /amq/init/config/extra-libs/postgresql-42.7.1.jar https://jdbc.postgresql.org/download/postgresql-42.7.1.jar",
												},
												"volumeMounts": []interface{}{
													map[string]interface{}{
														"name":      "amq-cfg-dir",
														"mountPath": "/amq/init/config",
													},
												},
												// need to fill in defaults as this is a full overwrite
												// that will loop in reconcile with server side applied absent defaults
												"terminationMessagePath":   "/dev/termination-log",
												"terminationMessagePolicy": "File",
												"imagePullPolicy":          "IfNotPresent",
											},
										},
									},
								},
							},
						},
						}),
					},
				}

				By("deploying custom logging to a file so we can check events from a probe for the broker")
				loggingConfigMapName := peerPrefix + "-probe-logging-config"
				loggingData := make(map[string]string)
				loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
			appender.stdout.type = Console
			rootLogger = info, STDOUT

			# audit logging would also be configured here

			appender.for_startup_probe.name = for_startup_probe
			appender.for_startup_probe.fileName = ${sys:artemis.instance}/log/startup_probe.log
			appender.for_startup_probe.type = File
			appender.for_startup_probe.append = false
			appender.for_startup_probe.bufferedIO = false
			appender.for_startup_probe.immediateFlush = true
			appender.for_startup_probe.createOnDemand = false

			# limit to server logging events
			logger.startup_prope_messages.name=org.apache.activemq.artemis.core.server
  			logger.startup_prope_messages.appenderRef.for_startup_probe.ref=for_startup_probe
			logger.startup_prope_messages.includeLocation=false
			logger.startup_prope_messages.level=INFO

			appender.for_startup_probe.filter.justTwo.type= RegexFilter
			# limit to relevant logging events so the file does not grow
			appender.for_startup_probe.filter.justTwo.regex = .*AMQ(221000|221002|221007).*
			appender.for_startup_probe.filter.justTwo.onMatch = "ACCEPT"
			appender.for_startup_probe.filter.justTwo.onMismatch = "DENY"`

				loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
				Expect(k8sClient.Create(ctx, loggingConfigMap)).Should(Succeed())
				peerA.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}

				By("Configuring probe to keep Pod alive while waiting to obtain shared jdbc lock")

				peerA.Spec.DeploymentPlan.LivenessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						Exec: &corev1.ExecAction{
							Command: []string{
								"/bin/bash", "-x", "-c",
								// ports ok || look for starting && !stopped (ie: waiting to activate)
								`/opt/amq/bin/readinessProbe.sh 1 || LOGFILE=/home/jboss/amq-broker/log/startup_probe.log; grep "AMQ221000" $LOGFILE && ! grep "AMQ221002" $LOGFILE`,
							},
						},
					},
					InitialDelaySeconds: 5,
					TimeoutSeconds:      5,
					PeriodSeconds:       5,
					SuccessThreshold:    1,
					FailureThreshold:    2,
				}

				By("cloning peer")
				peerB := &brokerv1beta1.ActiveMQArtemis{}

				// a clone of peerA
				peerA.DeepCopyInto(peerB)
				peerB.Name = peerPrefix + "-peer-b"

				By("provisioning the broker peer-a")
				Expect(k8sClient.Create(ctx, &peerA)).Should(Succeed())

				By("provisioning the broker peer-b")
				Expect(k8sClient.Create(ctx, peerB)).Should(Succeed())

				By("verifying one broker ready, other starting; it is a race to lock the db")
				peerACrd := &brokerv1beta1.ActiveMQArtemis{}
				peerBCrd := &brokerv1beta1.ActiveMQArtemis{}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      peerPrefix + "-peer-a",
						Namespace: defaultNamespace}, peerACrd)).Should(Succeed())

					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      peerPrefix + "-peer-b",
						Namespace: defaultNamespace}, peerBCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("\npeer-a CR Status:%v", peerACrd.Status)
						fmt.Printf("\npeer-b CR Status:%v", peerBCrd.Status)
					}

					readyPeer := meta.IsStatusConditionTrue(peerACrd.Status.Conditions, brokerv1beta1.ReadyConditionType) ||
						meta.IsStatusConditionTrue(peerBCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)

					startingPeer := len(peerACrd.Status.PodStatus.Starting) == 1 || len(peerBCrd.Status.PodStatus.Starting) == 1

					// probes keep locking peer in non-ready state
					g.Expect(readyPeer && startingPeer).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				brokerService := "broker-ha-jdbc"
				By("provisioning loadbalanced service for this CR, for use within the cluster via dns")
				svc := &corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      brokerService,
						Namespace: defaultNamespace,
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							peerLabel: peerPrefix, // shared by both CRs
						},
						Ports: []corev1.ServicePort{
							{
								Port:       62616,
								TargetPort: intstr.IntOrString{IntVal: 61616},
							},
						},
					},
				}

				Expect(k8sClient.Create(ctx, svc)).Should(Succeed())

				By("verifying service is ok - status does not reflect endpoints which is not ideal")
				createdService := &corev1.Service{}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svc.Name,
						Namespace: defaultNamespace}, createdService)).Should(Succeed())

					if verbose {
						fmt.Printf("\nsvc Status:%v", createdService.Status)
					}
					g.Expect(len(createdService.Status.LoadBalancer.Ingress)).Should(BeNumerically("==", 0))
					g.Expect(len(createdService.Status.Conditions)).Should(BeNumerically("==", 0))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("validating access to service via exec on either pod, will use peer-b")
				url := "tcp://" + brokerService + ":62616"
				podName := peerB.Name + "-ss-0"
				containerName := peerB.Name + "-container"
				Eventually(func(g Gomega) {
					sendCmd := []string{"amq-broker/bin/artemis", "producer", "--user", "Jay", "--password", "activemq", "--url", url, "--message-count", "1", "--destination", "queue://JOBS"}
					content, err := RunCommandInPod(podName, containerName, sendCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("Produced: 1 messages"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("killing active pod")
				activePod := &corev1.Pod{}
				activePod.Namespace = defaultNamespace
				if len(peerACrd.Status.PodStatus.Ready) == 1 {
					activePod.Name = peerA.Name + "-ss-0"
				} else {
					activePod.Name = peerB.Name + "-ss-0"
				}
				Expect(k8sClient.Delete(ctx, activePod)).To(Succeed())

				By("consuming our message, if peer-b is active, it may take a little while to restart")
				Eventually(func(g Gomega) {

					recvCmd := []string{"amq-broker/bin/artemis", "consumer", "--user", "Jay", "--password", "activemq", "--url", url, "--message-count", "1", "--receive-timeout", "5000", "--break-on-null", "--verbose", "--destination", "queue://JOBS"}
					content, err := RunCommandInPod(podName, containerName, recvCmd)

					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("JMS Message ID:"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(svc, svc.Name, defaultNamespace)
				CleanResource(&peerA, peerA.Name, defaultNamespace)
				CleanResource(peerB, peerB.Name, defaultNamespace)
				CleanResource(&db, db.Name, "default")
				CleanResource(&dbService, dbService.Name, "default")
				CleanResource(loggingConfigMap, loggingConfigMapName, defaultNamespace)
			}
		})
	})
})
