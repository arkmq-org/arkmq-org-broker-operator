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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/configmaps"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/namer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("artemis as scaledown controller", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("managed scale down test", func() {
		It("deploy plan 3 clustered messages on ordinal 1", Label("managed-scaledown-check", "verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerName := NextSpecResourceName()
				ctx := context.Background()

				brokerCrd := generateOriginalArtemisSpec(defaultNamespace, brokerName)

				By("deploying custom logging for the broker")
				loggingConfigMapName := "scale-down-logging-config"
				loggingData := make(map[string]string)
				loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
appender.stdout.type = Console
appender.stdout.layout.type = PatternLayout
appender.stdout.layout.pattern = %d %-5level [%logger] %msg%n
logger.a.name=org.jgroups
logger.a.level=ERROR
logger.b.name=org.apache.activemq.artemis.core.cluster
logger.b.level=TRACE
logger.c.name=org.apache.activemq.artemis.api.core.jgroups
logger.c.level=ERROR
logger.d.name=org.apache.activemq.artemis.core.server.impl.ScaleDownHandler
logger.d.level=TRACE
logger.e.name=org.apache.activemq.artemis.core.server.impl.PrimaryOnlyActivation
logger.e.level=TRACE
logger.f.name=org.apache.activemq.artemis.core.client
logger.f.level=TRACE
logger.g.name=org.apache.activemq.artemis.core.server.cluster.impl.ClusterConnectionImpl
logger.g.level=DEBUG
rootLogger = INFO, STDOUT`

				loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
				Expect(k8sClient.Create(ctx, loggingConfigMap)).Should(Succeed())
				brokerCrd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}

				brokerCrd.Spec.DeploymentPlan.Clustered = ptr.To(true)
				brokerCrd.Spec.DeploymentPlan.Size = ptr.To(int32(3))
				brokerCrd.Spec.DeploymentPlan.PersistenceEnabled = true
				brokerCrd.Spec.BrokerProperties = []string{
					"HAPolicyConfiguration=PRIMARY_ONLY",
					// use the cluster discovery group for scaledown
					//"HAPolicyConfiguration.scaleDownConfiguration.discoveryGroup=my-discovery-group",
					// alternative, static scale down to 0 always...
					"HAPolicyConfiguration.scaleDownConfiguration.connectors=ordinalZero",
					"connectorConfigurations.ordinalZero.factoryClassName=org.apache.activemq.artemis.core.remoting.impl.netty.NettyConnectorFactory",
					"connectorConfigurations.ordinalZero.params.port=61616",
					"connectorConfigurations.ordinalZero.params.host=" + common.OrdinalFQDNS(brokerCrd.Name, brokerCrd.Namespace, 0),

					// this errors out with topology NPE on scaledown even though dns resolves ok
					//"connectorConfigurations.ordinalZero.params.host=" + brokerName + "-ss-0." + brokerName + "-hdls-svc.test",

					// this is the trigger for using condition based scaledown, the operator overrides as necessary
					// it cannot be enabled till scaledown, otherwise SS rollout will scaledown!
					ScaleDownConfigTrigger,
				}

				Expect(k8sClient.Create(ctx, brokerCrd)).Should(Succeed())

				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}
				By("verifying ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					if verbose {
						fmt.Printf("\nSTATUS:%v\n", createdBrokerCrd.Status)
					}

					By("Check ready status")
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(3))
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				pod0BeforeScaleDown := &corev1.Pod{}
				podWithOrdinal0 := namer.CrToSS(brokerName) + "-0"
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podWithOrdinal0, Namespace: defaultNamespace}, pod0BeforeScaleDown)).Should(Succeed())

				podWithOrdinal1 := namer.CrToSS(brokerName) + "-1"

				By("Sending a message to 1")
				Eventually(func(g Gomega) {

					sendCmd := []string{"amq-broker/bin/artemis", "producer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podWithOrdinal1 + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
					content, err := RunCommandInPod(podWithOrdinal1, brokerName+"-container", sendCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("Produced: 1 messages"))

				}, timeout, interval).Should(Succeed())

				By("Scaling down to ss-0")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					createdBrokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
					g.Expect(k8sClient.Update(ctx, createdBrokerCrd)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking presence of scale down condition")
				Eventually(func(g Gomega) {
					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)

					// this is blocked by the need for config reload and restart * 2
					if verbose {
						fmt.Printf("\nSTATUS: %d:%v\n", *createdBrokerCrd.Spec.DeploymentPlan.Size, createdBrokerCrd.Status)
					}

					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking SS replica count scaled down eventually")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(createdBrokerCrd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(*sfsFound.Spec.Replicas).Should(BeEquivalentTo(1))

					// it can take 2x projection reload and N restarts with jgroups
				}, existingClusterTimeout*4, existingClusterInterval).Should(Succeed())

				By("Checking scale down condition gone and ready len 1")
				Eventually(func(g Gomega) {
					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)

					if verbose {
						fmt.Printf("\nSTATUS: %d:%v\n", *createdBrokerCrd.Spec.DeploymentPlan.Size, createdBrokerCrd.Status)
					}

					g.Expect(meta.FindStatusCondition(createdBrokerCrd.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType)).Should(BeNil())
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking scale down to 0 complete, message migrated to 0")
				Eventually(func(g Gomega) {

					By("checking the pod 0 after scaling down")
					pod0AfterScaleDown := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podWithOrdinal0, Namespace: defaultNamespace}, pod0AfterScaleDown)).Should(Succeed())

					By("Checking messsage count on broker 0")
					curlUrl := "http://" + podWithOrdinal0 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22,component=addresses,address=%22DLQ%22,subcomponent=queues,routing-type=%22anycast%22,queue=%22DLQ%22/MessageCount"
					curlCmd := []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
					result, err := RunCommandInPod(podWithOrdinal0, brokerName+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*result).To(ContainSubstring("\"value\":1"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Receiving a message from 0")
				Eventually(func(g Gomega) {

					rcvCmd := []string{"amq-broker/bin/artemis", "consumer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podWithOrdinal0 + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--receive-timeout", "10000", "--break-on-null", "--verbose"}
					content, err := RunCommandInPod(podWithOrdinal0, brokerName+"-container", rcvCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("JMS Message ID:"))

				}, timeout, interval).Should(Succeed())

				By("verifying ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					if verbose {
						fmt.Printf("\nSTATUS: %d:%v\n", *createdBrokerCrd.Spec.DeploymentPlan.Size, createdBrokerCrd.Status)
					}

					By("Check ready status")
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdBrokerCrd)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, loggingConfigMap)).Should(Succeed())
			}
		})

		It("deploy plan 2 clustered update rollout no scaledown", Label("managed-rollout-no-scaledown-check"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerName := NextSpecResourceName()
				ctx := context.Background()

				brokerCrd := generateOriginalArtemisSpec(defaultNamespace, brokerName)

				By("deploying custom logging for the broker")
				loggingConfigMapName := "scale-down-2-logging-config"
				loggingData := make(map[string]string)
				loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
			appender.stdout.type = Console
			rootLogger = info, STDOUT
			logger.activemq.name=org.apache.activemq.artemis.core
			logger.activemq.level=INFO
			# Audit loggers: to enable change levels from OFF to INFO
			logger.audit_base.name = org.apache.activemq.audit.base
			logger.audit_base.level = DEBUG`

				loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
				Expect(k8sClient.Create(ctx, loggingConfigMap)).Should(Succeed())
				brokerCrd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}

				booleanTrue := true
				brokerCrd.Spec.DeploymentPlan.Clustered = &booleanTrue
				brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				brokerCrd.Spec.DeploymentPlan.PersistenceEnabled = true
				brokerCrd.Spec.BrokerProperties = []string{
					"HAPolicyConfiguration=PRIMARY_ONLY",
					// use the cluster discovery group for scaledown
					"HAPolicyConfiguration.scaleDownConfiguration.discoveryGroup=my-discovery-group",
					// this is the trigger for using condition based scaledown, the operator overrides as necessary
					// it cannot be enabled till scaledown, otherwise SS rollout will scaledown!
					// 	const ScaleDownConfigTrigger = "HAPolicyConfiguration.scaleDownConfiguration.enabled=false"
					ScaleDownConfigTrigger,
				}

				Expect(k8sClient.Create(ctx, brokerCrd)).Should(Succeed())

				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}
				By("verifying ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					By("Check ready status")
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				pod0BeforeScaleDown := &corev1.Pod{}
				podWithOrdinal0 := namer.CrToSS(brokerName) + "-0"
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podWithOrdinal0, Namespace: defaultNamespace}, pod0BeforeScaleDown)).Should(Succeed())

				podWithOrdinal1 := namer.CrToSS(brokerName) + "-1"

				By("Sending a message to 1")
				Eventually(func(g Gomega) {

					sendCmd := []string{"amq-broker/bin/artemis", "producer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podWithOrdinal1 + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
					content, err := RunCommandInPod(podWithOrdinal1, brokerName+"-container", sendCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("Produced: 1 messages"))

				}, timeout, interval).Should(Succeed())

				By("force rollout of CR with env var update")
				var updatedVersion string
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					createdBrokerCrd.Spec.Env = []corev1.EnvVar{{Name: "JOE", Value: "JOE"}}
					g.Expect(k8sClient.Update(ctx, createdBrokerCrd)).Should(Succeed())
					updatedVersion = createdBrokerCrd.ResourceVersion

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					if verbose {
						fmt.Printf("\nSTATUS: %d:%v\n", *createdBrokerCrd.Spec.DeploymentPlan.Size, createdBrokerCrd.Status)
					}

					g.Expect(createdBrokerCrd.ResourceVersion).ShouldNot(BeEquivalentTo(updatedVersion))
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking no scale down")
				Eventually(func(g Gomega) {

					By("checking the pod 1 after restart has new env")
					pod0AfterScaleDown := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podWithOrdinal0, Namespace: defaultNamespace}, pod0AfterScaleDown)).Should(Succeed())

					var foundJoe bool
					for _, e := range pod0AfterScaleDown.Spec.Containers[0].Env {
						if e.Name == "JOE" {
							foundJoe = true
							break
						}
					}
					g.Expect(foundJoe).Should(BeTrue())

					By("Checking messsage count on broker 1")
					curlUrl := "http://" + podWithOrdinal1 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22,component=addresses,address=%22DLQ%22,subcomponent=queues,routing-type=%22anycast%22,queue=%22DLQ%22/MessageCount"
					curlCmd := []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
					result, err := RunCommandInPod(podWithOrdinal1, brokerName+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*result).To(ContainSubstring("\"value\":1"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdBrokerCrd)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, loggingConfigMap)).Should(Succeed())
			}
		})
	})
})
