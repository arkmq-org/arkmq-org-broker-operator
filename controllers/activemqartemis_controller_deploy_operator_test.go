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
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"bufio"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:embed testdata/dummy_broker.py
var dummyPythonScript string

var _ = Describe("artemis controller", Label("do"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("tls jolokia access", Label("do-secure-console-with-sni"), func() {
		It("check the util works in test env", func() {
			domainName := common.GetClusterDomain()
			Expect(domainName).To(Equal("cluster.local"))
		})
		It("get status from broker", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" && os.Getenv("DEPLOY_OPERATOR") == "true" {
				brokerName := "artemis-broker"
				hdlsName := brokerName + "-hdls-svc." + defaultNamespace + ".svc.cluster.local"

				commonSecretName := "common-amq-tls-sni-secret"
				commonSecret, err := CreateTlsSecret(commonSecretName, defaultNamespace, defaultPassword, []string{"*." + hdlsName})
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, commonSecret)).Should(Succeed())

				acceptorSecretName := "acceptor-amq-tls-sni-secret"
				acceptorSecret, err := CreateTlsSecret(acceptorSecretName, defaultNamespace, defaultPassword, []string{"*." + hdlsName})
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, acceptorSecret)).Should(Succeed())

				createdSecret := corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      commonSecretName,
					Namespace: defaultNamespace,
				}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Deploying the broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

					candidate.Name = brokerName
					candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
						{
							Name:       "artemis-acceptor",
							Port:       61617,
							SSLEnabled: true,
							SSLSecret:  acceptorSecretName,
						},
					}
					candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
					candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
						TimeoutSeconds:      5,
					}
					candidate.Spec.Console.Expose = true
					candidate.Spec.Console.SSLEnabled = true
					candidate.Spec.Console.SSLSecret = commonSecretName

					if !isOpenshift {
						candidate.Spec.IngressDomain = defaultTestIngressDomain
					}
				})

				By("Check ready status")
				Eventually(func(g Gomega) {
					oprLog, rrr := GetOperatorLog(defaultNamespace)
					g.Expect(rrr).To(BeNil())
					getPersistedVersionedCrd(brokerCr.ObjectMeta.Name, defaultNamespace, createdBrokerCr)
					g.Expect(len(createdBrokerCr.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))
					if verbose {
						fmt.Printf("STATUS:%v\n", createdBrokerCr.Status)
					}
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue(), *oprLog)
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking test queue")
				Eventually(func(g Gomega) {
					ssName := namer.CrToSS(createdBrokerCr.Name)
					for i := 0; i < 2; i++ {
						podWithOrdinal := ssName + "-" + strconv.Itoa(i)
						checkCmd := []string{"amq-broker/bin/artemis", "check", "queue", "--name", "TEST", "--produce", "1000", "--consume", "1000", "--url", "tcp://" + podWithOrdinal + "." + hdlsName + ":61617?sslEnabled=true&trustStorePath=/etc/" + acceptorSecretName + "-volume/client.ts&trustStorePassword=password"}
						_, err := RunCommandInPodWithNamespace(podWithOrdinal, defaultNamespace, createdBrokerCr.Name+"-container", checkCmd)
						g.Expect(err).To(BeNil())
					}
				}, timeout, interval).Should(Succeed())

				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(commonSecret, commonSecret.Name, defaultNamespace)
			}
		})
	})

	Context("operator logging config", Label("do-operator-log"), func() {
		It("test operator with env var", func() {
			if os.Getenv("DEPLOY_OPERATOR") == "true" {
				// re-install a new operator to have a fresh log
				uninstallOperator(false, defaultNamespace)
				installOperator(nil, defaultNamespace)
				By("checking default operator should have INFO logs")
				Eventually(func(g Gomega) {
					oprLog, err := GetOperatorLog(defaultNamespace)
					g.Expect(err).To(BeNil())
					g.Expect(*oprLog).To(ContainSubstring("INFO"))
					g.Expect(*oprLog).NotTo(ContainSubstring("DEBUG"))
					g.Expect(*oprLog).NotTo(ContainSubstring("ERROR"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Uninstall existing operator")
				uninstallOperator(false, defaultNamespace)

				By("install the operator again with logging env var")
				envMap := make(map[string]string)
				envMap["ARGS"] = "--zap-log-level=error"
				installOperator(envMap, defaultNamespace)
				By("deploy a basic broker to produce some more log")
				brokerCr, createdCr := DeployCustomBroker(defaultNamespace, nil)

				By("wait for pod so enough log is generated")
				Eventually(func(g Gomega) {
					getPersistedVersionedCrd(brokerCr.Name, defaultNamespace, createdCr)
					g.Expect(len(createdCr.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("check no INFO/DEBUG in the log")
				oprLog, err := GetOperatorLog(defaultNamespace)
				Expect(err).To(BeNil())
				Expect(*oprLog).NotTo(ContainSubstring("DEBUG"))
				// every info line should have setup logger name
				buffer := bytes.NewBufferString(*oprLog)
				scanner := bufio.NewScanner(buffer)
				for scanner.Scan() {
					line := scanner.Text()
					if strings.Contains(line, "INFO") {
						words := strings.Fields(line)
						index := 0
						foundSetupLogger := false
						for index < len(words) {
							if words[index] == "setup" {
								foundSetupLogger = true
								break
							}
							index++
						}
						Expect(foundSetupLogger).To(BeTrue())
						Expect(words[index-1]).To(Equal("INFO"))
					}
				}

				Expect(scanner.Err()).To(BeNil())

				//clean up all resources
				Expect(k8sClient.Delete(ctx, createdCr)).Should(Succeed())
			}
		})
	})

	Context("operator deployment in default namespace", Label("do-operator-with-custom-related-images"), func() {
		It("default broker versions", func() {
			if os.Getenv("DEPLOY_OPERATOR") == "true" {
				By("Uninstall existing operator")
				uninstallOperator(false, defaultNamespace)

				By("install the operator again with custom related images")
				setupEnvs := make(map[string]string)
				setupEnvs["RELATED_IMAGE_BROKER_KUBERNETES_"+version.GetDefaultCompactVersion()] = "quay.io/arkmq-org/fake-broker:latest"
				setupEnvs["RELATED_IMAGE_BROKER_INIT_"+version.GetDefaultCompactVersion()] = "quay.io/arkmq-org/fake-broker-init:latest"
				installOperator(setupEnvs, defaultNamespace)

				By("deploy a broker")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Spec.Version = ""
					candidate.Spec.DeploymentPlan.Image = ""
					candidate.Spec.DeploymentPlan.InitImage = ""
				})

				By("checking the default broker version would be the latest")
				createdSs := &appsv1.StatefulSet{}
				ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
					mainContainer := createdSs.Spec.Template.Spec.Containers[0]
					g.Expect(mainContainer.Image).To(Equal("quay.io/arkmq-org/fake-broker:latest"))
					initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
					g.Expect(initContainer.Image).To(Equal("quay.io/arkmq-org/fake-broker-init:latest"))

				}, timeout, interval).Should(Succeed())

				By("checking the CR status")
				brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

					g.Expect(createdBrokerCr.Status.Version.Image).Should(ContainSubstring("fake"))
					g.Expect(createdBrokerCr.Status.Version.InitImage).Should(ContainSubstring("fake"))
					g.Expect(createdBrokerCr.Status.Version.BrokerVersion).Should(Equal(version.GetDefaultVersion()))

					g.Expect(createdBrokerCr.Status.Upgrade.MajorUpdates).Should(BeTrue())
					g.Expect(createdBrokerCr.Status.Upgrade.MinorUpdates).Should(BeTrue())
					g.Expect(createdBrokerCr.Status.Upgrade.PatchUpdates).Should(BeTrue())
					g.Expect(createdBrokerCr.Status.Upgrade.SecurityUpdates).Should(BeTrue())

				}, timeout, interval).Should(Succeed())

				CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
			}
		})
	})

	Context("operator deployment in restricted namespace", Label("do-operator-restricted"), func() {
		It("test in a restricted namespace", func() {
			if os.Getenv("DEPLOY_OPERATOR") == "true" {
				restrictedNs := NextSpecResourceName()
				restrictedSecurityPolicy := "restricted"
				uninstallOperator(false, defaultNamespace)
				By("creating a restricted namespace " + restrictedNs)
				createNamespace(restrictedNs, &restrictedSecurityPolicy)
				Expect(installOperator(nil, restrictedNs)).To(Succeed())

				By("checking operator deployment")
				deployment := appsv1.Deployment{}
				deploymentKey := types.NamespacedName{Name: depName, Namespace: restrictedNs}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, deploymentKey, &deployment)).Should(Succeed())
					g.Expect(deployment.Status.ReadyReplicas).Should(Equal(int32(1)))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				uninstallOperator(false, restrictedNs)
				deleteNamespace(restrictedNs, true, Default)
				Expect(installOperator(nil, defaultNamespace)).To(Succeed())
			}
		})
	})

	Context("operator deployment under high load", Label("do-operator-high-load"), Label("slow"), func() {
		It("maintains memory usage below threshold when managing 1000 broker CRs", func() {
			if os.Getenv("DEPLOY_OPERATOR") == "true" {
				tempNs := NextSpecResourceName()
				uninstallOperator(false, defaultNamespace)
				By("creating a temp namespace " + tempNs)
				createNamespace(tempNs, nil)
				Expect(installOperator(nil, tempNs)).To(Succeed())

				By("checking operator deployment")
				Eventually(func(g Gomega) {
					deployment := appsv1.Deployment{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: depName,
						Namespace: tempNs}, &deployment)).Should(Succeed())
					g.Expect(deployment.Status.ReadyReplicas).Should(Equal(int32(1)))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				count := 1000
				ssKind := "StatefulSet"
				crNamePrefix := NextSpecResourceName()

				for i := 0; i < count; i++ {
					crName := crNamePrefix + "-" + strconv.Itoa(i)

					statefulSetPatch := &unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind": "StatefulSet",
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{
										"initContainers": []interface{}{
											map[string]interface{}{
												"name":    crName + "-container-init",
												"command": []interface{}{"/bin/bash"},
												"args":    []interface{}{"-c", "echo 'Dummy init container'"},
												"image":   "registry.access.redhat.com/ubi9/python-312-minimal",
											},
										},
										"containers": []interface{}{
											map[string]interface{}{
												"name":    crName + "-container",
												"command": []interface{}{"python3"},
												"args":    []interface{}{"-c", dummyPythonScript},
												"image":   "registry.access.redhat.com/ubi9/python-312-minimal",
											},
										},
									},
								},
							},
						},
					}

					rawBytes, err := json.Marshal(statefulSetPatch.Object)
					Expect(err).To(BeNil())
					By("deploying dummy broker with cr: " + crName)
					cr := brokerv1beta1.ActiveMQArtemis{
						TypeMeta: metav1.TypeMeta{
							Kind:       "ActiveMQArtemis",
							APIVersion: brokerv1beta1.GroupVersion.Identifier(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      crName,
							Namespace: tempNs,
						},
						Spec: brokerv1beta1.ActiveMQArtemisSpec{
							DeploymentPlan: brokerv1beta1.DeploymentPlanType{
								Size: ptr.To(int32(0)),
								ReadinessProbe: &corev1.Probe{
									ProbeHandler: corev1.ProbeHandler{
										TCPSocket: &corev1.TCPSocketAction{
											Port: intstr.IntOrString{
												IntVal: 8161,
											},
										},
									},
								},
							},
							ResourceTemplates: []brokerv1beta1.ResourceTemplate{
								{
									Selector: &brokerv1beta1.ResourceSelector{Kind: &ssKind},
									Patch:    runtime.RawExtension{Raw: rawBytes},
								},
							},
						},
					}

					Expect(k8sClient.Create(ctx, &cr)).Should(Succeed())
				}

				createdSs := &appsv1.StatefulSet{}
				createdCr := &brokerv1beta1.ActiveMQArtemis{}

				for i := 0; i < count; i++ {
					crName := crNamePrefix + "-" + strconv.Itoa(i)

					By("checking dummy broker with cr: " + crName)
					ssKey := types.NamespacedName{Name: namer.CrToSS(crName), Namespace: tempNs}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					crKey := types.NamespacedName{Name: crName, Namespace: tempNs}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, crKey, createdCr)).Should(Succeed())
						g.Expect(meta.IsStatusConditionTrue(createdCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				podList := &corev1.PodList{}
				Expect(k8sClient.List(context.Background(), podList, client.InNamespace(tempNs),
					client.MatchingLabels{"control-plane": "controller-manager", "name": oprName})).To(Succeed())
				Expect(len(podList.Items) > 0).To(BeTrue())

				By("checking manager container memory")
				Eventually(func(g Gomega) {
					currentMemoryText, err := RunCommandInPodWithNamespace(
						podList.Items[0].Name, tempNs, "manager",
						[]string{"cat", "/sys/fs/cgroup/memory.current"})
					g.Expect(err).To(BeNil())

					currentMemoryBytes, err := strconv.ParseInt(
						strings.TrimSpace(*currentMemoryText), 10, 64)
					g.Expect(err).To(BeNil())

					// 256Mi threshold is set below the 384Mi limit to ensure adequate
					// headroom for memory spikes and to verify the operator runs
					// efficiently when managing 1000 broker CRs
					thresholdMemoryMegaBytes := int64(256 * 1024 * 1024)
					g.Expect(currentMemoryBytes < thresholdMemoryMegaBytes).Should(BeTrue(),
						"memory usage %d bytes exceeds threshold of %d bytes",
						currentMemoryBytes, thresholdMemoryMegaBytes)
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				uninstallOperator(false, tempNs)
				deleteNamespace(tempNs, false, Default)
				Expect(installOperator(nil, defaultNamespace)).To(Succeed())
			}
		})
	})
})
