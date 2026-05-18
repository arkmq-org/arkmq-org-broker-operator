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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
)

var _ = Describe("Read-only root filesystem support", Label("read-only-root-filesystem"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("using container security context to enable read-only root filesystem and resource templates to patch StatefulSet", Label("resource-templates"), func() {
		It("successfully deploys and connects 2 clustered brokers", func() {

			By("deploy a broker cr")
			cr, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.JolokiaAgentEnabled = true
				candidate.Spec.DeploymentPlan.MessageMigration = ptr.To(true)
				candidate.Spec.DeploymentPlan.PersistenceEnabled = true
				candidate.Spec.DeploymentPlan.Size = ptr.To(int32(2))

				candidate.Spec.DeploymentPlan.ContainerSecurityContext = &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					RunAsNonRoot:             ptr.To(true),
					ReadOnlyRootFilesystem:   ptr.To(true),
				}

				candidate.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
					{
						Selector: &brokerv1beta1.ResourceSelector{Kind: ptr.To("StatefulSet")},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"kind": "StatefulSet",
								"spec": map[string]interface{}{
									"template": map[string]interface{}{
										"spec": map[string]interface{}{
											"volumes": []interface{}{
												// The jboss-home volume is needed for the broker instance
												map[string]interface{}{
													"name":     "jboss-home",
													"emptyDir": map[string]interface{}{},
												},
												// The jolokia-config volume is needed for the jolokia agent
												// configuration created by the joloklia module at
												// /opt/jboss/container/jolokia/etc/jolokia.properties
												map[string]interface{}{
													"name":     "jolokia-config",
													"emptyDir": map[string]interface{}{},
												},
												// The tmp-dir volume is needed for logs created
												// by the readinessProbe.sh script in the /tmp directory
												map[string]interface{}{
													"name":     "tmp-dir",
													"emptyDir": map[string]interface{}{},
												},
											},
											"initContainers": []interface{}{
												map[string]interface{}{
													"name": "$(CR_NAME)-container-init",
													"volumeMounts": []interface{}{
														map[string]interface{}{
															"name":      "jboss-home",
															"mountPath": "/home/jboss",
														},
													},
												},
											},
											"containers": []interface{}{
												map[string]interface{}{
													"name": "$(CR_NAME)-container",
													"env": []interface{}{
														// These env vars are needed because the lauch script
														// fails to substitute them in the jgroups-ping.xml file
														// when the root filesystem is read-only
														map[string]interface{}{
															"name":  "APPLICATION_NAME",
															"value": "$(CR_NAME)",
														},
														map[string]interface{}{
															"name":  "PING_SVC_NAME",
															"value": "ping-svc",
														},
													},
													"volumeMounts": []interface{}{
														map[string]interface{}{
															"name":      "jboss-home",
															"mountPath": "/home/jboss",
														},
														map[string]interface{}{
															"name":      "jolokia-config",
															"mountPath": "/opt/jboss/container/jolokia/etc",
														},
														map[string]interface{}{
															"name":      "tmp-dir",
															"mountPath": "/tmp",
														},
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
			})

			By("checking containers have read-only root filesystem")
			Eventually(func(g Gomega) {
				brokerStatefulSet := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name + "-ss", Namespace: cr.Namespace}, brokerStatefulSet)).Should(Succeed())
				for _, initContainer := range brokerStatefulSet.Spec.Template.Spec.InitContainers {
					g.Expect(*initContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				}
				for _, container := range brokerStatefulSet.Spec.Template.Spec.Containers {
					g.Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("checking ready condition is true")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, cr)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ReadyConditionType)).To(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking cluster connections")
				for _, ordinal := range []string{"0", "1"} {
					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(cr.Name+"-ss-"+ordinal, cr.Name, cr.Namespace,
							[]string{"amq-broker/bin/artemis", "check", "node", "--peers", "2"}, g)
						g.Expect(stdOutContent).Should(ContainSubstring("success"))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}
			}

			CleanResource(cr, cr.Name, cr.Namespace)
		})
	})

	Context("using resource templates to enable read-only filesystem and patch StatefulSet", Label("resource-templates"), func() {
		It("successfully deploys and connects 2 clustered brokers", func() {

			By("deploy a broker cr")
			cr, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.JolokiaAgentEnabled = true
				candidate.Spec.DeploymentPlan.MessageMigration = ptr.To(true)
				candidate.Spec.DeploymentPlan.PersistenceEnabled = true
				candidate.Spec.DeploymentPlan.Size = ptr.To(int32(2))

				candidate.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
					{
						Selector: &brokerv1beta1.ResourceSelector{Kind: ptr.To("StatefulSet")},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"kind": "StatefulSet",
								"spec": map[string]interface{}{
									"template": map[string]interface{}{
										"spec": map[string]interface{}{
											"volumes": []interface{}{
												// The jboss-home volume is needed for the broker instance
												map[string]interface{}{
													"name":     "jboss-home",
													"emptyDir": map[string]interface{}{},
												},
												// The jolokia-config volume is needed for the jolokia agent
												// configuration created by the joloklia module at
												// /opt/jboss/container/jolokia/etc/jolokia.properties
												map[string]interface{}{
													"name":     "jolokia-config",
													"emptyDir": map[string]interface{}{},
												},
												// The tmp-dir volume is needed for logs created
												// by the readinessProbe.sh script in the /tmp directory
												map[string]interface{}{
													"name":     "tmp-dir",
													"emptyDir": map[string]interface{}{},
												},
											},
											"initContainers": []interface{}{
												map[string]interface{}{
													"name": "$(CR_NAME)-container-init",
													"securityContext": map[string]interface{}{
														"readOnlyRootFilesystem": true,
													},
													"volumeMounts": []interface{}{
														map[string]interface{}{
															"name":      "jboss-home",
															"mountPath": "/home/jboss",
														},
													},
												},
											},
											"containers": []interface{}{
												map[string]interface{}{
													"name": "$(CR_NAME)-container",
													"env": []interface{}{
														// These env vars are needed because the lauch script
														// fails to substitute them in the jgroups-ping.xml file
														// when the root filesystem is read-only
														map[string]interface{}{
															"name":  "APPLICATION_NAME",
															"value": "$(CR_NAME)",
														},
														map[string]interface{}{
															"name":  "PING_SVC_NAME",
															"value": "ping-svc",
														},
													},
													"securityContext": map[string]interface{}{
														"readOnlyRootFilesystem": true,
													},
													"volumeMounts": []interface{}{
														map[string]interface{}{
															"name":      "jboss-home",
															"mountPath": "/home/jboss",
														},
														map[string]interface{}{
															"name":      "jolokia-config",
															"mountPath": "/opt/jboss/container/jolokia/etc",
														},
														map[string]interface{}{
															"name":      "tmp-dir",
															"mountPath": "/tmp",
														},
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
			})

			By("checking containers have read-only root filesystem")
			Eventually(func(g Gomega) {
				brokerStatefulSet := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name + "-ss", Namespace: cr.Namespace}, brokerStatefulSet)).Should(Succeed())
				for _, initContainer := range brokerStatefulSet.Spec.Template.Spec.InitContainers {
					g.Expect(*initContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				}
				for _, container := range brokerStatefulSet.Spec.Template.Spec.Containers {
					g.Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("checking ready condition is true")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, cr)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ReadyConditionType)).To(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking cluster connections")
				for _, ordinal := range []string{"0", "1"} {
					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(cr.Name+"-ss-"+ordinal, cr.Name, cr.Namespace,
							[]string{"amq-broker/bin/artemis", "check", "node", "--peers", "2"}, g)
						g.Expect(stdOutContent).Should(ContainSubstring("success"))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}
			}

			CleanResource(cr, cr.Name, cr.Namespace)
		})
	})

	Context("using container security context to enable read-only root filesystem and extra volumes to patch StatefulSet", Label("resource-templates"), func() {
		It("successfully deploys and connects 2 clustered brokers", func() {

			By("deploy a broker cr")
			cr, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.JolokiaAgentEnabled = true
				candidate.Spec.DeploymentPlan.MessageMigration = ptr.To(true)
				candidate.Spec.DeploymentPlan.PersistenceEnabled = true
				candidate.Spec.DeploymentPlan.Size = ptr.To(int32(2))

				candidate.Spec.DeploymentPlan.ContainerSecurityContext = &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					RunAsNonRoot:             ptr.To(true),
					ReadOnlyRootFilesystem:   ptr.To(true),
				}

				// Use ExtraVolumes and ExtraVolumeMounts to add volumes needed for read-only root filesystem
				candidate.Spec.DeploymentPlan.ExtraVolumes = []corev1.Volume{
					{
						Name: "jboss-home",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
					{
						Name: "jolokia-config",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
					{
						Name: "tmp-dir",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				}
				candidate.Spec.DeploymentPlan.ExtraVolumeMounts = []corev1.VolumeMount{
					{
						Name:      "jboss-home",
						MountPath: "/home/jboss",
					},
					{
						Name:      "jolokia-config",
						MountPath: "/opt/jboss/container/jolokia/etc",
					},
					{
						Name:      "tmp-dir",
						MountPath: "/tmp",
					},
				}

				// These env vars are needed because the lauch script
				// fails to substitute them in the jgroups-ping.xml file
				// when the root filesystem is read-only
				candidate.Spec.Env = []corev1.EnvVar{
					{
						Name: "APPLICATION_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{
								FieldPath: "metadata.labels['ActiveMQArtemis']",
							},
						},
					},
					{
						Name:  "PING_SVC_NAME",
						Value: "ping-svc",
					},
				}
			})

			By("checking containers have read-only root filesystem")
			Eventually(func(g Gomega) {
				brokerStatefulSet := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name + "-ss", Namespace: cr.Namespace}, brokerStatefulSet)).Should(Succeed())
				for _, initContainer := range brokerStatefulSet.Spec.Template.Spec.InitContainers {
					g.Expect(*initContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				}
				for _, container := range brokerStatefulSet.Spec.Template.Spec.Containers {
					g.Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("checking ready condition is true")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, cr)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ReadyConditionType)).To(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking cluster connections")
				for _, ordinal := range []string{"0", "1"} {
					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(cr.Name+"-ss-"+ordinal, cr.Name, cr.Namespace,
							[]string{"amq-broker/bin/artemis", "check", "node", "--peers", "2"}, g)
						g.Expect(stdOutContent).Should(ContainSubstring("success"))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}
			}

			CleanResource(cr, cr.Name, cr.Namespace)
		})
	})
})
