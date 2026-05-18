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
	"io"
	"net/http"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/configmaps"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

var _ = Describe("pub sub scale", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("pub n sub, ha pub, partitioned sub", Label("slow"), func() {
		It("validation", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				ctx := context.Background()
				brokerCrd := generateArtemisSpec(defaultNamespace)

				brokerCrd.Spec.Console.Expose = true

				brokerCrd.Spec.DeploymentPlan.PersistenceEnabled = boolFalse
				brokerCrd.Spec.DeploymentPlan.Clustered = &boolFalse
				brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)

				By("deplying secret with jaas config for auth")
				jaasSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: "k8s.io.api.core.v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pub-sub-jaas-config",
						Namespace: brokerCrd.ObjectMeta.Namespace,
					},
				}

				jaasSecret.StringData = map[string]string{JaasConfigKey: `
				activemq {
	
					// ensure the operator can connect to the mgmt console by referencing the existing properties config
					org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
						org.apache.activemq.jaas.properties.user="artemis-users.properties"
						org.apache.activemq.jaas.properties.role="artemis-roles.properties"
						baseDir="/home/jboss/amq-broker/etc";
	
					// app specific users and roles	
					org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
						reload=true
						debug=true
						org.apache.activemq.jaas.properties.user="users.properties"
						org.apache.activemq.jaas.properties.role="roles.properties";
	
				};`,
					"users.properties": `
					 control-plane=passwd

					 p=passwd
					 c1=passwd
					 c2=passwd
					 c3=passwd
					 c4=passwd`,

					"roles.properties": `
					
					# rbac
					control-plane=control-plane,control-plane-0,control-plane-1
					consumers=c1,c2,c3,c4
					producers=p

					# shard roles for connectionRouter partitioning
					shard-consumers-broker-0=c1,c2
					shard-consumers-broker-1=c3,c4
					shard-producers=p`,
				}

				By("Deploying the jaas secret " + jaasSecret.Name)
				Expect(k8sClient.Create(ctx, jaasSecret)).Should(Succeed())

				brokerCrd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{jaasSecret.Name}

				brokerCrd.Spec.Env = []corev1.EnvVar{
					{
						Name: "CR_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.labels['" + selectors.LabelResourceKey + "']"},
						},
					},
				}

				By("deploying custom logging for the broker")
				loggingConfigMapName := "my-logging-config"
				loggingData := make(map[string]string)
				loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
			appender.stdout.type = Console
			rootLogger = info, STDOUT
			logger.activemq.name=org.apache.activemq.artemis.core.config.impl.ConfigurationImpl
			logger.activemq.level=TRACE
			logger.jaas.name=org.apache.activemq.artemis.spi.core.security.jaas
			logger.jaas.level=TRACE
			logger.rest.name=org.apache.activemq.artemis.core
			logger.rest.level=INFO`

				loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
				Expect(k8sClient.Create(ctx, loggingConfigMap)).Should(Succeed())
				brokerCrd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}

				By("configuring the broker")
				brokerCrd.Spec.BrokerProperties = []string{
					"addressConfigurations.COMMANDS.routingTypes=MULTICAST",

					"# rbac",
					"securityRoles.COMMANDS.producers.send=true",
					"securityRoles.COMMANDS.consumers.consume=true",
					"securityRoles.COMMANDS.consumers.createNonDurableQueue=true",
					"securityRoles.COMMANDS.consumers.deleteNonDurableQueue=true",

					"# control-plane rbac",
					"securityRoles.COMMANDS.control-plane.createDurableQueue=true",
					"securityRoles.COMMANDS.control-plane.consume=true",
					"securityRoles.COMMANDS.control-plane.send=true",

					"# federation internal links etc use the ACTIVEMQ_ARTEMIS_FEDERATION prefix",
					"securityRoles.\"$ACTIVEMQ_ARTEMIS_FEDERATION.#\".control-plane.createNonDurableQueue=true",
					"securityRoles.\"$ACTIVEMQ_ARTEMIS_FEDERATION.#\".control-plane.createAddress=true",
					"securityRoles.\"$ACTIVEMQ_ARTEMIS_FEDERATION.#\".control-plane.consume=true",
					"securityRoles.\"$ACTIVEMQ_ARTEMIS_FEDERATION.#\".control-plane.send=true",

					// with properties update - can have this static with dns
					"# federate the address, publish on N goes to [0..N]",
					"broker-0.AMQPConnections.target.uri=tcp://${CR_NAME}-ss-1.${CR_NAME}-hdls-svc:61616",
					"broker-1.AMQPConnections.target.uri=tcp://${CR_NAME}-ss-0.${CR_NAME}-hdls-svc:61616",
					// how to use TLS and sni here?

					"# speed up mesh formation",
					"AMQPConnections.target.retryInterval=1000",

					// feature - service account
					"AMQPConnections.target.user=control-plane",
					"AMQPConnections.target.password=passwd",
					"AMQPConnections.target.autostart=true",

					"AMQPConnections.target.federations.peerN.localAddressPolicies.forCommands.includes.justCommands.addressMatch=COMMANDS",

					"# routing",
					"connectionRouters.partitionOnRole.keyType=ROLE_NAME",
					"connectionRouters.partitionOnRole.localTargetFilter=NULL|producers|consumers-broker-${STATEFUL_SET_ORDINAL}",

					"# need to extract the single role that is used to partition, a `shard-` prefixed role",
					"connectionRouters.partitionOnRole.keyFilter=(?<=^shard-).*", // match and strip the shard- prefix

					"acceptorConfigurations.tcp.params.router=partitionOnRole", // matching spec.acceptor
				}

				brokerCrd.Spec.Acceptors = []brokerv1beta1.AcceptorType{{Name: "tcp", Port: 61616, Expose: true}}

				brokerCrd.Spec.DeploymentPlan.EnableMetricsPlugin = &boolTrue

				if !isOpenshift {
					brokerCrd.Spec.IngressDomain = defaultTestIngressDomain
				}

				By("provisioning the broker")
				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}
				createdBrokerCrdKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				By("verifying broker started - not really necessary, can be async")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, createdBrokerCrdKey, createdBrokerCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("\nStatus:%v", createdBrokerCrd.Status)
					}
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("provisioning loadbalanced service for this CR, for use within the cluster via dns")
				svc := &corev1.Service{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "v1",
						Kind:       "Service",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      brokerCrd.Name,
						Namespace: defaultNamespace,
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							selectors.LabelResourceKey: createdBrokerCrd.Name,
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

				By("provisioning an app, publisher and consumers, using the broker image to access the artemis client from within the cluster")
				deploymentTemplate := func(name string, command []string) appsv1.Deployment {
					appLables := map[string]string{"app": name}
					return appsv1.Deployment{

						TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace, Labels: appLables},
						Spec: appsv1.DeploymentSpec{
							Selector: &metav1.LabelSelector{MatchLabels: appLables},
							Replicas: common.Int32ToPtr(1),
							Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: appLables},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name:    name,
											Image:   version.GetDefaultKubeImage(),
											Command: command,
										},
									},
								}},
						},
					}

				}
				By("deploying consumers")

				serviceUrl := "tcp://" + brokerCrd.Name + ":62616"

				// loop until successfully get messages as we expect rejection by the router
				numMessagesToConsume := 10
				scriptTemplate := "until /opt/amq/bin/artemis perf consumer --password passwd --user %s --url %s --silent --message-count %d topic://COMMANDS; do echo retry; sleep 1; done;"
				commonCommand := []string{"/bin/sh", "-c"}

				consumer1 := deploymentTemplate(
					"consumer1",
					append(commonCommand, fmt.Sprintf(scriptTemplate, "c1", serviceUrl, numMessagesToConsume)),
				)

				consumer3 := deploymentTemplate(
					"consumer3",
					append(commonCommand, fmt.Sprintf(scriptTemplate, "c3", serviceUrl, numMessagesToConsume)),
				)

				Expect(k8sClient.Create(ctx, &consumer1)).Should(Succeed())
				Expect(k8sClient.Create(ctx, &consumer3)).Should(Succeed())

				By("deploying producer")

				producer := deploymentTemplate(
					"producer",
					[]string{"/opt/amq/bin/artemis", "perf", "producer", "--user", "p", "--password", "passwd", "--url", serviceUrl, "--rate", "2", "--silent", "topic://COMMANDS"},
				)
				Expect(k8sClient.Create(ctx, &producer)).Should(Succeed())

				By("verifying - consumer on each broker, messages flowing..")

				By("verifying metrics")
				nonZeroRoutedMetricFor := func(g Gomega, ordinal string) bool {
					pod := &corev1.Pod{}
					podName := namer.CrToSS(createdBrokerCrd.Name) + "-" + ordinal
					podNamespacedName := types.NamespacedName{Name: podName, Namespace: defaultNamespace}
					g.Expect(k8sClient.Get(ctx, podNamespacedName, pod)).Should(Succeed())

					g.Expect(pod.Status).ShouldNot(BeNil())
					g.Expect(pod.Status.PodIP).ShouldNot(BeEmpty())

					resp, err := http.Get("http://" + pod.Status.PodIP + ":8161/metrics")
					g.Expect(err).Should(Succeed())

					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					g.Expect(err).Should(Succeed())

					lines := strings.Split(string(body), "\n")

					var done = false
					if verbose {
						fmt.Printf("\nStart Metrics for COMMANDS on %v with Headers %v \n", ordinal, resp.Header)
					}
					for _, line := range lines {
						if strings.Contains(line, "COMMANDS") {
							if verbose {
								fmt.Printf("%s\n", line)
							}
						}
						if strings.Contains(line, "artemis_routed_message_count{address=\"COMMANDS\",broker=\"amq-broker\",}") {
							if !strings.Contains(line, "} 0.0") {
								done = true
							}
						}
					}
					return done
				}

				Eventually(func(g Gomega) {

					foundMetric0 := nonZeroRoutedMetricFor(g, "0")
					foundMetric1 := nonZeroRoutedMetricFor(g, "1")

					g.Expect(foundMetric0).Should(Equal(true))
					g.Expect(foundMetric1).Should(Equal(true))

				}, existingClusterTimeout*2, existingClusterInterval*5).Should(Succeed())

				CleanResource(&producer, producer.Name, defaultNamespace)
				CleanResource(&consumer1, producer.Name, defaultNamespace)
				CleanResource(&consumer3, producer.Name, defaultNamespace)

				CleanResource(createdBrokerCrd, createdBrokerCrd.Name, defaultNamespace)
				CleanResource(jaasSecret, jaasSecret.Name, defaultNamespace)
				CleanResource(loggingConfigMap, loggingConfigMap.Name, defaultNamespace)
				CleanResource(svc, svc.Name, defaultNamespace)
			}

		})

		It("from zero to hero", func() {

			By("start with empty broker - don't want to depend on artemis create and defaults that may change version on version")
			// feature: empty broker
			// - needs to be env aware - container limits etc
			// - control plane auth with service account

			By("provision broker(s) for scale pub sub")
			// new CR:
			// provisionBrokers { type: pubSub - that name may be meaningless to anyone else!
			// only need volume if consumers are durable (chicken and egg here!)
			// persistence keyed of volume mount type?
			// io -> iops
			// mold empty broker into a broker for the 'pubSub' purpose - respecting the env.
			// challenge - some of the env may not be known till deployment
			//  }

			By("onboard app one")

			// new CR:
			/* onboardApp {
				// labels to match provisiond brokers
				// inorder to reason about capacity, feedback - the labels have to have meaning/capacity/bounds
				label: pubSub, zoneA, local disks?


				producer on COMMANDS with role P // address, RBAC send P

				consumer on COMMANDS2 with role A // non persistent multicast queue, rbac create, consume A
				consumer on COMMANDS2 with role B and backlog 1M // // durable multicast queue, rbac create, consume B

				TBD
			}
			*/

			By("onboard app two")

			By("decision - need to scale")
			{
				By("add new partition")

				By("update mesh/cluster/collection of brokers")

				// feature: mirror reload config required for partition 0
				// feature: properties remove for scale down - to remove mirror to ordinal-N on broker-(N-1)
				// mirror store and forward queue could be managed via properties, independent of the connection

			}
		})
	})
})
