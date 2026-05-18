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
	"os"
	"reflect"
	"strconv"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/volumes"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	routev1 "github.com/openshift/api/route/v1"
)

var _ = Describe("artemis controller 2", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("persistent volumes tests", Label("controller-2-test"), func() {
		It("controller resource recover test", Label("controller-resource-recover-test"), func() {

			By("deploy a broker cr")
			acceptorName := "amqp"
			_, crd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:      acceptorName,
						Protocols: "amqp",
						Port:      5672,
						Expose:    true,
					},
				}
				candidate.Spec.IngressDomain = "arkmq-org.io"
			})

			brokerKey := types.NamespacedName{
				Name:      crd.Name,
				Namespace: defaultNamespace,
			}

			brokerPropSecretName := crd.Name + "-props"
			brokerPropSecretKey := types.NamespacedName{
				Name:      brokerPropSecretName,
				Namespace: defaultNamespace,
			}
			brokerPropSecret := corev1.Secret{}
			var secretId types.UID
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
				g.Expect(k8sClient.Get(ctx, brokerPropSecretKey, &brokerPropSecret)).Should(Succeed())
				secretId = brokerPropSecret.UID
			}, timeout, interval).Should(Succeed())

			By("delete the broker properties secret")
			Expect(k8sClient.Delete(ctx, &brokerPropSecret)).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerPropSecretKey, &brokerPropSecret)).Should(Succeed())
				newSecretId := brokerPropSecret.UID
				g.Expect(newSecretId).ShouldNot(Equal(secretId))
			}, timeout, interval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" && isOpenshift {
				By("modify route labels")
				routeKey := types.NamespacedName{
					Name:      crd.Name + "-" + acceptorName + "-0-" + "svc-rte",
					Namespace: defaultNamespace,
				}

				acceptorRoute := routev1.Route{}
				var originalLables map[string]string
				// compare resource version as there will be an update
				var routeVersion string
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, routeKey, &acceptorRoute)).Should(Succeed())
					originalLables = CloneStringMap(acceptorRoute.Labels)
					routeVersion = acceptorRoute.ResourceVersion
					g.Expect(len(acceptorRoute.Labels) > 0).To(BeTrue())
					for k, v := range acceptorRoute.Labels {
						acceptorRoute.Labels[k] = v + "change"
					}
					g.Expect(k8sClient.Update(ctx, &acceptorRoute)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, routeKey, &acceptorRoute)).Should(Succeed())
					newRouteVersion := acceptorRoute.ResourceVersion
					g.Expect(newRouteVersion).ShouldNot(Equal(routeVersion))
					g.Expect(reflect.DeepEqual(originalLables, acceptorRoute.Labels)).Should(BeTrue())
				}, timeout, interval).Should(Succeed())
			} else {
				By("modify ingress labels")
				ingKey := types.NamespacedName{
					Name:      crd.Name + "-" + acceptorName + "-0-" + "svc-ing",
					Namespace: defaultNamespace,
				}

				acceptorIng := netv1.Ingress{}
				var originalLables map[string]string
				// compare resource version as there will be an update
				var ingVersion string
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ingKey, &acceptorIng)).Should(Succeed())
					originalLables = CloneStringMap(acceptorIng.Labels)
					ingVersion = acceptorIng.ResourceVersion
					g.Expect(len(acceptorIng.Labels) > 0).To(BeTrue())
					for k, v := range acceptorIng.Labels {
						acceptorIng.Labels[k] = v + "change"
					}
					g.Expect(k8sClient.Update(ctx, &acceptorIng)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ingKey, &acceptorIng)).Should(Succeed())
					newIngVersion := acceptorIng.ResourceVersion
					g.Expect(newIngVersion).ShouldNot(Equal(ingVersion))
					g.Expect(reflect.DeepEqual(originalLables, acceptorIng.Labels)).Should(BeTrue())
				}, timeout, interval).Should(Succeed())

				By("modifying ingress host")
				originalHost := ""
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ingKey, &acceptorIng)).Should(Succeed())
					originalHost = acceptorIng.Spec.Rules[0].Host
					ingVersion = acceptorIng.ResourceVersion
					acceptorIng.Spec.Rules[0].Host = originalHost + "s"
					g.Expect(k8sClient.Update(ctx, &acceptorIng)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ingKey, &acceptorIng)).Should(Succeed())
					newIngVersion := acceptorIng.ResourceVersion
					g.Expect(newIngVersion).ShouldNot(Equal(ingVersion))
					g.Expect(acceptorIng.Spec.Rules[0].Host).Should(Equal(originalHost))
				}, timeout, interval).Should(Succeed())
			}
			CleanResource(crd, crd.Name, defaultNamespace)
		})

		It("external volumes attach", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("create extra volume")
				pvc, createdPvc := DeployCustomPVC("my-pvc", defaultNamespace, func(candidate *corev1.PersistentVolumeClaim) {

					candidate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					}
					candidate.Spec.Resources = corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("16Mi"),
						},
					}
				})

				By("use a pod to write some files to the volume")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
					candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						TimeoutSeconds:      5,
					}
					candidate.Spec.DeploymentPlan.ExtraVolumes = []corev1.Volume{
						{
							Name: "mydata",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "my-pvc",
								},
							},
						},
					}
				})

				By("waiting for pod ready")
				WaitForPod(brokerCr.Name)

				By("writing some files to the volume")
				podWithOrdinal := namer.CrToSS(brokerCr.Name) + "-0"

				newFileCmd := []string{"/bin/sh", "-c", "echo Hello-World > /amq/extra/volumes/mydata/fake.config"}
				content, err := RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", newFileCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(BeEmpty())

				By("check the file exists")
				lsCmd := []string{"ls", "/amq/extra/volumes/mydata"}
				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", lsCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(ContainSubstring("fake.config"))

				By("shut down the pod")
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)

				By("deploying broker to use existing volume")
				brokerCr, createdBrokerCr = DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						TimeoutSeconds:      5,
					}
					candidate.Spec.DeploymentPlan.ExtraVolumes = []corev1.Volume{
						{
							Name: "mydata",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "my-pvc",
								},
							},
						},
					}
					candidate.Spec.DeploymentPlan.ExtraVolumeMounts = []corev1.VolumeMount{
						{
							Name:      "mydata",
							MountPath: "/opt/common",
						},
					}
				})

				By("waiting for pod ready")
				WaitForPod(brokerCr.Name)

				By("checking the file still exist on volumes")
				lsCmd = []string{"ls", "/opt/common"}
				catCmd := []string{"cat", "/opt/common/fake.config"}
				podWithOrdinal = namer.CrToSS(brokerCr.Name) + "-0"
				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", lsCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(ContainSubstring("fake.config"), *content)

				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", catCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(ContainSubstring("Hello-World"))

				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(createdPvc, pvc.Name, pvc.Namespace)
			}
		})

		It("external pvc with pvc templates", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerCrName := NextSpecResourceName()

				pvcName := "mydata-" + namer.CrToSS(brokerCrName) + "-0"
				By("create extra pvc: " + pvcName)
				pvc, createdPvc := DeployCustomPVC(pvcName, defaultNamespace, func(candidate *corev1.PersistentVolumeClaim) {

					candidate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					}
					candidate.Spec.Resources = corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("16Mi"),
						},
					}
				})

				By("use a pod to write some files to the volume")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Name = brokerCrName
					candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
					candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						TimeoutSeconds:      5,
					}
					candidate.Spec.DeploymentPlan.ExtraVolumeClaimTemplates = []brokerv1beta1.VolumeClaimTemplate{
						{
							ObjectMeta: brokerv1beta1.ObjectMeta{
								Name: "mydata",
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("16Mi"),
									},
								},
							},
						},
					}
				})

				defaultMoutPath := volumes.GetDefaultExternalPVCMountPath("mydata")

				By("waiting for pod ready")
				WaitForPod(brokerCr.Name)

				By("writing some files to the volume")
				podWithOrdinal := namer.CrToSS(brokerCr.Name) + "-0"

				newFileCmd := []string{"/bin/sh", "-c", "echo my-data-pod-0 > " + *defaultMoutPath + "/fake.data"}
				content, err := RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", newFileCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(BeEmpty())

				By("check the file exists")
				lsCmd := []string{"ls", *defaultMoutPath}
				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", lsCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(ContainSubstring("fake.data"))

				By("shut down the pod")
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)

				By("deploying 2 brokers")
				mountPath := "/opt/mydata"
				brokerCr, createdBrokerCr = DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Name = brokerCrName
					candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
					candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						TimeoutSeconds:      5,
					}
					candidate.Spec.DeploymentPlan.ExtraVolumeClaimTemplates = []brokerv1beta1.VolumeClaimTemplate{
						{
							ObjectMeta: brokerv1beta1.ObjectMeta{
								Name: "mydata",
							},
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
								Resources: corev1.VolumeResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("16Mi"),
									},
								},
							},
						},
					}
					candidate.Spec.DeploymentPlan.ExtraVolumeMounts = []corev1.VolumeMount{
						{
							Name:      "mydata",
							MountPath: mountPath,
						},
					}
				})

				By("waiting 2 pods ready")
				WaitForPods(brokerCr.Name, 0, 1)

				By("checking the file exists on pod 0")
				lsCmd = []string{"ls", mountPath}
				catCmd := []string{"cat", mountPath + "/fake.data"}

				podWithOrdinal = namer.CrToSS(brokerCr.Name) + "-0"
				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", lsCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(ContainSubstring("fake.data"))

				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", catCmd)
				Expect(err).To(BeNil())
				Expect(*content).To(ContainSubstring("my-data-pod-0"))

				By("checking the file not exist on pod 1")
				lsCmd = []string{"ls", mountPath}

				podWithOrdinal = namer.CrToSS(brokerCr.Name) + "-1"
				content, err = RunCommandInPod(podWithOrdinal, brokerCr.Name+"-container", lsCmd)
				Expect(err).To(BeNil())
				Expect(*content).NotTo(ContainSubstring("fake.data"))

				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(createdPvc, pvc.Name, pvc.Namespace)
			}
		})
	})

	It("route reconcile", func() {

		if isOpenshift && os.Getenv("USE_EXISTING_CLUSTER") == "true" && os.Getenv("DEPLOY_OPERATOR") == "false" {

			By("start to capture test log needs local operator")
			StartCapturingLog()
			defer StopCapturingLog()

			By("deploy a broker cr")
			acceptorName := "amqp"
			_, crd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:      acceptorName,
						Protocols: "amqp",
						Port:      5672,
						Expose:    true,
					},
				}
				// no brokerHost or domain, openshift will annotate the route host.generated
				//candidate.Spec.IngressDomain = "arkmq-org.io"
			})

			routeKey := types.NamespacedName{
				Name:      crd.Name + "-" + acceptorName + "-0-" + "svc-rte",
				Namespace: defaultNamespace,
			}

			acceptorRoute := routev1.Route{}
			var routeVersion string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, &acceptorRoute)).Should(Succeed())
				routeVersion = acceptorRoute.ResourceVersion
				g.Expect(routeVersion).ShouldNot(Equal(""))
			}, timeout, interval).Should(Succeed())

			By("verify no route change, ver: " + acceptorRoute.ResourceVersion + ", gen: " + strconv.FormatInt(acceptorRoute.Generation, 10))

			By("force another reconcole with new env var, verify no change to route version")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			newEnvName := "GG"
			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(crd.Name, defaultNamespace, createdCrd)).Should(BeTrue())
				createdCrd.Spec.Env = []corev1.EnvVar{{Name: newEnvName, Value: newEnvName}}
				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("Checking SS has new env var")
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				sfsFound := &appsv1.StatefulSet{}

				g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
				found := false
				for _, e := range sfsFound.Spec.Template.Spec.Containers[0].Env {
					By("checking env: " + e.Name)
					if e.Name == newEnvName {
						found = true
					}
				}
				g.Expect(found).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("wait for ready")
			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(crd.Name, defaultNamespace, createdCrd)).Should(BeTrue())
				g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("checking route not updated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, &acceptorRoute)).Should(Succeed())

				By("verify no route change, ver: " + acceptorRoute.ResourceVersion + ", gen: " + strconv.FormatInt(acceptorRoute.Generation, 10))

				g.Expect(routeVersion).Should(Equal(acceptorRoute.ResourceVersion))
			}, timeout, interval).Should(Succeed())

			By("finding no Updating v1.Route ")
			matches, err := FindAllInCapturingLog(`Updating \*v1.Route`)
			Expect(err).To(BeNil())
			Expect(len(matches)).To(Equal(0))

			CleanResource(crd, crd.Name, defaultNamespace)
		}
	})
})
