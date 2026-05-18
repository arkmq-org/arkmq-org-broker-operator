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

/*
As usual, we start with the necessary imports. We also define some utility variables.
*/
package controllers

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("security without controller", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("brokerProperties rbac", func() {

		It("security with management role access", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "admin"

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "broker-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{JaasConfigKey: `
		    // a full login.config
		    activemq {

				// ensure the operator can connect to the mgmt console by referencing the existing properties config
				org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
					reload=true
					org.apache.activemq.jaas.properties.user="artemis-users.properties"
					org.apache.activemq.jaas.properties.role="artemis-roles.properties"
					baseDir="/home/jboss/amq-broker/etc";

				// app specific users and roles
				org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
					reload=true
					org.apache.activemq.jaas.properties.user="users.properties"
					org.apache.activemq.jaas.properties.role="roles.properties";

			};`,
				"users.properties": `
				tom=tom
				joe=joe`,
				"roles.properties": `
				toms=tom
				joes=joe`,
			}

			crd.Spec.BrokerProperties = []string{
				"# create tom's work queue",
				"addressConfigurations.TOMS_WORK_QUEUE.routingTypes=ANYCAST",
				"addressConfigurations.TOMS_WORK_QUEUE.queueConfigs.TOMS_WORK_QUEUE.routingType=ANYCAST",
				"addressConfigurations.TOMS_WORK_QUEUE.queueConfigs.TOMS_WORK_QUEUE.durable=true",

				"# rbac, give tom's role send access",
				"securityRoles.TOMS_WORK_QUEUE.toms.send=true",

				"addressConfigurations.TOPIC.routingTypes=MULTICAST",
				"addressConfigurations.TOPIC.queueConfigs.FOR_TOM.routingType=MULTICAST",
				"addressConfigurations.TOPIC.queueConfigs.FOR_TOM.address=TOPIC",
				"addressConfigurations.TOPIC.queueConfigs.FOR_TOM.durable=true",

				"# rbac topic, give tom's role send access to fqqn, :: needs escape",
				"securityRoles.\"TOPIC\\:\\:FOR_TOM\".toms.send=true",

				"# amqp acceptor",
				"acceptorConfigurations.amqp.factoryClassName=org.apache.activemq.artemis.core.remoting.impl.netty.NettyAcceptorFactory",
				"acceptorConfigurations.amqp.params.host=${HOSTNAME}",
				"acceptorConfigurations.amqp.params.port=61617",

				"# rbac, give tom's role view/edit access for JMX",
				"securityRoles.\"mops.address.TOMS_WORK_QUEUE.*\".toms.view=true",
				"securityRoles.\"mops.address.TOMS_WORK_QUEUE.*\".toms.edit=true",

				"securityRoles.\"mops.#\".admin.view=true",
				"securityRoles.\"mops.#\".admin.edit=true",

				"# deny forceFailover",
				"securityRoles.\"mops.broker.forceFailover\".denied=-",
			}

			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}
			crd.Spec.Env = []corev1.EnvVar{{
				Name: "JAVA_ARGS_APPEND",
				// no role check in hawtio as roles are checked by the broker via the guard installed with this ArtemisRbacMBeanServerBuilder
				Value: "-Dhawtio.roles= -Djavax.management.builder.initial=org.apache.activemq.artemis.core.server.management.ArtemisRbacMBeanServerBuilder",
			}}

			By("removing management.xml authorization section from artemis create with a resource template patch")
			var kindMatchSs string = "StatefulSet"

			crd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
				{
					Selector: &brokerv1beta1.ResourceSelector{
						Kind: &kindMatchSs,
					},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
						"kind": &kindMatchSs,
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"initContainers": []interface{}{
										map[string]interface{}{
											"name": crd.Name + "-container-init",
											// overwrite cmd args array with additional echo to clear management.xml
											"args": []string{
												"-c",
												`/opt/amq/bin/launch.sh && /opt/amq-broker/script/default.sh; echo "Empty management.xml";echo "<management-context xmlns=\"http://activemq.apache.org/schema\" />" > /amq/init/config/amq-broker/etc/management.xml`,
											},
										},
									},
								},
							},
						},
					},
					}),
				},
			}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			By("Checking ready, operator can access broker status via jmx")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verify joe cannot see address message count attribute")
			podWithOrdinal0 := namer.CrToSS(crd.Name) + "-0"
			originHeader := "Origin: http://" + "localhost" // value not verified but presence necessary
			curlUrl := "http://" + podWithOrdinal0 + ":8161/console/jolokia/read/org.apache.activemq.artemis:address=%22TOMS_WORK_QUEUE%22,broker=%22amq-broker%22,component=addresses/MessageCount"
			curlCmd := []string{"curl", "-S", "-v", "-H", originHeader, "-u", "joe:joe", curlUrl}
			Eventually(func(g Gomega) {
				result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)

				g.Expect(err).To(BeNil())
				// joe does not have permission
				g.Expect(*result).To(ContainSubstring("403"))
				g.Expect(*result).To(ContainSubstring("VIEW"))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verify tom can see address message count attribute")
			curlCmd = []string{"curl", "-S", "-v", "-H", originHeader, "-u", "tom:tom", curlUrl}
			Eventually(func(g Gomega) {
				result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)

				g.Expect(err).To(BeNil())
				g.Expect(*result).To(ContainSubstring("200"))
				g.Expect(*result).To(ContainSubstring("\"value\":0"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verify admin can't call forceFailover")
			curlUrl = "http://" + podWithOrdinal0 + ":8161/console/jolokia/exec/org.apache.activemq.artemis:broker=%22amq-broker%22/forceFailover"
			curlCmd = []string{"curl", "-S", "-v", "-H", originHeader, "-u", "admin:admin", curlUrl}
			Eventually(func(g Gomega) {
				result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)
				g.Expect(err).To(BeNil())
				g.Expect(*result).To(ContainSubstring("403"))
				g.Expect(*result).To(ContainSubstring("EDIT"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Verify send to FQQN")
			Eventually(func(g Gomega) {
				result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container",
					[]string{"/bin/sh", "-c", "/opt/amq/bin/artemis producer --protocol=AMQP --user tom --password tom --url tcp://" + podWithOrdinal0 + ":61617 --message-count 10 --destination topic://TOPIC::FOR_TOM"})
				if verbose {
					fmt.Printf("err: %v, res: %v\n", err, *result)
				}
				g.Expect(err).To(BeNil())
				g.Expect(*result).To(ContainSubstring("TOPIC::FOR_TOM, thread=0 Produced: 10 messages"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Verify no permission to send to topic")
			Eventually(func(g Gomega) {
				result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container",
					[]string{"/bin/sh", "-c", "/opt/amq/bin/artemis producer --protocol=AMQP --user tom --password tom --url tcp://" + podWithOrdinal0 + ":61617 --message-count 10 --destination topic://TOPIC"})
				if verbose {
					fmt.Printf("err: %v, res: %v\n", err, *result)
				}
				g.Expect(err).To(BeNil())
				g.Expect(*result).To(ContainSubstring("tom does not have permission='SEND' on address TOPIC"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
		})
	})

})
