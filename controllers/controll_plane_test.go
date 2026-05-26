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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

type brokerImageVariant struct {
	name      string
	image     string
	initImage string
}

var brokerVersionMatrix = []brokerImageVariant{
	{name: "default (2.53.0)"},
	{
		name:      "snapshot",
		image:     "quay.io/arkmq-org/arkmq-org-broker-kubernetes:snapshot",
		initImage: "quay.io/arkmq-org/arkmq-org-broker-init:snapshot",
	},
}

func applyBrokerImage(crd *brokerv1beta1.ActiveMQArtemis, variant brokerImageVariant) {
	if variant.image != "" {
		crd.Spec.DeploymentPlan.Image = variant.image
	}
	if variant.initImage != "" {
		crd.Spec.DeploymentPlan.InitImage = variant.initImage
	}
}

var _ = Describe("minimal", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	for _, variant := range brokerVersionMatrix {
		variant := variant

		Context("restricted rbac ["+variant.name+"]", func() {

			It("operator role access", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}

				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)

				crd.Spec.BrokerProperties = []string{
					"messageCounterSamplePeriod=500",
				}

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Checking deployed and status annotation populated via status script")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())

					podList := &corev1.PodList{}
					g.Expect(k8sClient.List(ctx, podList,
						rtclient.InNamespace(defaultNamespace),
						rtclient.MatchingLabels{"ActiveMQArtemis": crd.Name},
					)).Should(Succeed())
					g.Expect(podList.Items).ShouldNot(BeEmpty())
					annotation := podList.Items[0].Annotations[BrokerStatusAnnotationKey]
					g.Expect(annotation).ShouldNot(BeEmpty(), "status script should have written the annotation")
					g.Expect(annotation).Should(ContainSubstring(`"state":"STARTED"`))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("hot reload with valid config updates", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				bpSecretName := NextSpecResourceName() + "-bp"
				bpSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      bpSecretName,
						Namespace: defaultNamespace,
					},
					StringData: map[string]string{
						"extra.properties": "messageCounterSamplePeriod=600",
					},
				}

				By("Deploying the -bp secret " + bpSecret.Name)
				Expect(k8sClient.Create(ctx, bpSecret)).Should(Succeed())

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}
				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)
				crd.Spec.BrokerProperties = []string{
					"messageCounterSamplePeriod=500",
				}
				crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{bpSecretName}

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for ready and config applied")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Update 1 - changing property via CR brokerProperties")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.BrokerProperties = []string{
						"globalMaxSize=256m",
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying config still applied after CR update")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Update 2 - changing property in -bp secret")
				secretKey := types.NamespacedName{Name: bpSecret.Name, Namespace: defaultNamespace}
				createdSecret := &corev1.Secret{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, createdSecret)).Should(Succeed())
					createdSecret.Data["extra.properties"] = []byte("messageCounterSamplePeriod=700")
					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying config still applied after secret update")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdSecret)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("hot reload with invalid config updates", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				bpSecretName := NextSpecResourceName() + "-bp"
				bpSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      bpSecretName,
						Namespace: defaultNamespace,
					},
					StringData: map[string]string{
						"extra.properties": "messageCounterSamplePeriod=600",
					},
				}

				By("Deploying the -bp secret " + bpSecret.Name)
				Expect(k8sClient.Create(ctx, bpSecret)).Should(Succeed())

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}
				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)
				crd.Spec.BrokerProperties = []string{
					"messageCounterSamplePeriod=500",
				}
				crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{bpSecretName}

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for ready and config applied")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Invalid update 1 - injecting duplicate keys via CR brokerProperties")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.BrokerProperties = []string{
						"messageCounterSamplePeriod=500",
						"messageCounterSamplePeriod=700",
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying Valid condition reports duplicate key error")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
					validationCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(validationCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey))
					g.Expect(validationCondition.Message).To(ContainSubstring("messageCounterSamplePeriod"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Fix - removing duplicate key from CR")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.BrokerProperties = []string{
						"messageCounterSamplePeriod=500",
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying Valid condition recovers")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Invalid update 2 - injecting duplicate keys via -bp secret")
				secretKey := types.NamespacedName{Name: bpSecret.Name, Namespace: defaultNamespace}
				createdSecret := &corev1.Secret{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, createdSecret)).Should(Succeed())
					createdSecret.Data["extra.properties"] = []byte("messageCounterSamplePeriod=600\nmessageCounterSamplePeriod=700")
					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying Valid condition reports extra mount validation error")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
					validationCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(validationCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
					g.Expect(validationCondition.Message).To(ContainSubstring("extra.properties"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Fix - correcting the -bp secret")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, createdSecret)).Should(Succeed())
					createdSecret.Data["extra.properties"] = []byte("messageCounterSamplePeriod=600")
					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying Valid condition recovers after secret fix")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Waiting for config to be applied after recovery")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdSecret)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("hot reload with invalid property rejected by broker", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}
				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)
				crd.Spec.BrokerProperties = []string{
					"messageCounterSamplePeriod=500",
				}

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for ready and config applied")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Injecting invalid property via CR brokerProperties")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.BrokerProperties = []string{
						"notValid=bla",
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying ConfigApplied becomes False with AppliedWithError")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).To(ContainSubstring("notValid"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Fixing the invalid property")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.BrokerProperties = []string{
						"messageCounterSamplePeriod=500",
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying ConfigApplied recovers to True")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("hot reload with invalid enum produces ApplyError", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}
				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)
				crd.Spec.BrokerProperties = []string{
					"globalMaxSize=128m",
				}

				By("Deploying broker with valid property")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for ready and config applied")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Hot reload: adding invalid enum property")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.BrokerProperties = []string{
						"globalMaxSize=128m",
						"addressSettings.#.addressFullMessagePolicy=INVALID_ENUM",
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Waiting for ConfigApplied to report AppliedWithError")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					if verbose && condition != nil {
						fmt.Printf("ConfigApplied: status=%s reason=%s message=%s\n", condition.Status, condition.Reason, condition.Message)
					}
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).To(ContainSubstring("addressFullMessagePolicy"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying AppliedWithError persists (does not recover)")
				Consistently(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					if verbose && condition != nil {
						fmt.Printf("ConfigApplied: status=%s reason=%s\n", condition.Status, condition.Reason)
					}
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
				}, 60*time.Second, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("hot reload survives broker process restart", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}
				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)
				crd.Spec.BrokerProperties = []string{
					"messageCounterSamplePeriod=500",
				}

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for ready and config applied (green status)")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				podName := crd.Name + "-ss-0"
				brokerContainerName := crd.Name + "-container"

				By("Recording initial state before kill")
				pod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: defaultNamespace}, pod)).Should(Succeed())
				var initialRestartCount int32
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.Name == brokerContainerName {
						initialRestartCount = cs.RestartCount
						break
					}
				}
				configConditionBefore := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
				Expect(configConditionBefore).NotTo(BeNil())
				transitionBefore := configConditionBefore.LastTransitionTime

				By("Killing the broker process (PID 1)")
				RunCommandInPod(podName, brokerContainerName, []string{"/bin/sh", "-c", "kill 1"})

				By("Waiting for container restart count to increment")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: defaultNamespace}, pod)).Should(Succeed())
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.Name == brokerContainerName {
							g.Expect(cs.RestartCount).To(BeNumerically(">", initialRestartCount))
							return
						}
					}
					g.Expect(false).To(BeTrue(), "broker container not found in pod status")
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Waiting for full recovery: Ready and ConfigApplied green with new transition time")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					if verbose && condition != nil {
						fmt.Printf("Recovery poll: ConfigApplied status=%s reason=%s transition=%s\n",
							condition.Status, condition.Reason, condition.LastTransitionTime.Format("15:04:05"))
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))
					// The condition must have transitioned after the restart,
					// proving the status script re-reported status post-restart
					g.Expect(condition.LastTransitionTime.Time.After(transitionBefore.Time)).To(BeTrue(),
						"ConfigApplied lastTransitionTime did not advance after restart")
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("public jolokia endpoint with dedicated realm in restricted mode", func() {

				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					return
				}

				ctx := context.Background()

				consoleAuthSecretName := NextSpecResourceName() + "-console-auth"
				consoleAuthSecret := &corev1.Secret{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Secret",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      consoleAuthSecretName,
						Namespace: defaultNamespace,
					},
					StringData: map[string]string{
						"_console-users.properties": "console=console123\nuser1=password1",
						"_console-roles.properties": "admin=console,user1",
					},
				}

				By("Deploying the console auth secret " + consoleAuthSecret.Name)
				Expect(k8sClient.Create(ctx, consoleAuthSecret)).Should(Succeed())

				consoleAuthBaseDir := "/amq/extra/secrets/" + consoleAuthSecretName

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}
				crd.Spec.Restricted = common.NewTrue()
				applyBrokerImage(&crd, variant)
				crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{consoleAuthSecretName}
				crd.Spec.Env = []corev1.EnvVar{
					{Name: "JAVA_ARGS_APPEND", Value: "-javaagent:/opt/agents/jolokia.jar=host=0.0.0.0,port=8161,agentId=public,authMode=jaas,realm=console_authenticator"},
				}
				crd.Spec.BrokerProperties = []string{
					`jaasConfigs."console_authenticator".modules.props.loginModuleClass=org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule`,
					`jaasConfigs."console_authenticator".modules.props.controlFlag=required`,
					`jaasConfigs."console_authenticator".modules.props.params."org.apache.activemq.jaas.properties.user"=_console-users.properties`,
					`jaasConfigs."console_authenticator".modules.props.params."org.apache.activemq.jaas.properties.role"=_console-roles.properties`,
					fmt.Sprintf(`jaasConfigs."console_authenticator".modules.props.params.baseDir=%s`, consoleAuthBaseDir),
					`securityRoles."mops.broker".admin.view=true`,
					`securityRoles."mops.broker.getVersion".admin.view=true`,
					`securityRoles."mops.broker.getStatus".admin.view=true`,
					`securityRoles."mops.mbeanserver.queryMBeans".admin.view=true`,
				}

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for ready and config applied")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				podName := crd.Name + "-ss-0"
				brokerContainerName := crd.Name + "-container"
				brokerName := crd.Name
				jolokiaVersionPath := fmt.Sprintf("http://%s:8161/jolokia/version", podName)
				jolokiaReadBroker := fmt.Sprintf("http://%s:8161/jolokia/read/org.apache.activemq.artemis:broker=%%22%s%%22/Version", podName, brokerName)

				By("Checking if public Jolokia on 8161 is listening")
				Eventually(func(g Gomega) {
					checkCmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "-u", "console:console123", jolokiaVersionPath}
					result, err := RunCommandInPod(podName, brokerContainerName, checkCmd)
					g.Expect(err).To(BeNil())
					fmt.Printf("Port 8161 check: http_code=%s\n", *result)
					g.Expect(*result).NotTo(Equal("000"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying user1:password1 can read broker Version via JMX on 8161")
				Eventually(func(g Gomega) {
					curlCmd := []string{"curl", "-s", "-w", "\nHTTP_CODE:%{http_code}", "-u", "user1:password1", jolokiaReadBroker}
					result, err := RunCommandInPod(podName, brokerContainerName, curlCmd)
					g.Expect(err).To(BeNil())
					fmt.Printf("user1 MBean read result: %s\n", *result)
					g.Expect(*result).To(ContainSubstring("HTTP_CODE:200"))
					g.Expect(*result).To(ContainSubstring(`"status":200`))
					g.Expect(*result).To(ContainSubstring(`"value"`))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying status-script credentials (sidecar:sidecar) are rejected on 8161")
				Eventually(func(g Gomega) {
					curlCmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "-u", "sidecar:sidecar", jolokiaVersionPath}
					result, err := RunCommandInPod(podName, brokerContainerName, curlCmd)
					g.Expect(err).To(BeNil())
					fmt.Printf("Status-script auth response code: %s\n", *result)
					g.Expect(*result).To(Equal("401"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying unauthenticated access is rejected on 8161")
				Eventually(func(g Gomega) {
					curlCmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", jolokiaVersionPath}
					result, err := RunCommandInPod(podName, brokerContainerName, curlCmd)
					g.Expect(err).To(BeNil())
					fmt.Printf("No-auth response code: %s\n", *result)
					g.Expect(*result).To(Equal("401"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, consoleAuthSecret)).Should(Succeed())
			})
		})
	}
})
