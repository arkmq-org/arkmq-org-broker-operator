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
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
)

var _ = Describe("NetworkPolicy for BrokerCluster operand", Label("network-policy"), func() {

	BeforeEach(func() {
		Expect(os.Setenv("BROKER_NETWORK_POLICY", "true")).To(Succeed())
		BeforeEachSpec()
	})

	AfterEach(func() {
		Expect(os.Unsetenv("BROKER_NETWORK_POLICY")).To(Succeed())
		AfterEachSpec()
	})

	brokerPropertiesAMQP := func() []string {
		return []string{
			"acceptorConfigurations.custom-amqp.factoryClassName=org.apache.activemq.artemis.core.remoting.impl.netty.NettyAcceptorFactory",
			"acceptorConfigurations.custom-amqp.params.port=5672",
			"acceptorConfigurations.custom-amqp.params.host=0.0.0.0",
			"acceptorConfigurations.custom-amqp.params.protocols=AMQP",
		}
	}

	Context("broker-properties acceptor with ResourceTemplate opens port in NetworkPolicy", func() {
		It("allows cross-pod traffic on a port opened via ResourceTemplate", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" || !isOpenshift {
				Skip("requires OpenShift cluster with NP-enforcing CNI for cross-pod connectivity testing")
			}

			netpolKind := "NetworkPolicy"
			brokerCr, createdCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.BrokerCluster) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.BrokerProperties = brokerPropertiesAMQP()
				candidate.Spec.ResourceTemplates = []brokerv1beta2.ResourceTemplate{
					{
						Selector: &brokerv1beta2.ResourceSelector{
							Kind: &netpolKind,
						},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"spec": map[string]interface{}{
									"ingress": []interface{}{
										map[string]interface{}{
											"ports": []interface{}{
												map[string]interface{}{"port": int64(7800), "protocol": "TCP"},
												map[string]interface{}{"port": int64(8161), "protocol": "TCP"},
												map[string]interface{}{"port": int64(8778), "protocol": "TCP"},
												map[string]interface{}{"port": int64(61616), "protocol": "TCP"},
												map[string]interface{}{"port": int64(5672), "protocol": "TCP"},
											},
										},
									},
								},
							},
						}),
					},
				}
			})

			netpolKey := types.NamespacedName{
				Name:      brokerCr.Name + "-netpol",
				Namespace: defaultNamespace,
			}

			By("verifying the NetworkPolicy includes all ports from the ResourceTemplate")
			netpolObject := netv1.NetworkPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
				ingressPorts := collectIngressPorts(&netpolObject)
				g.Expect(ingressPorts).To(ContainElement(int32(7800)), "jgroups port")
				g.Expect(ingressPorts).To(ContainElement(int32(8161)), "console-jolokia port")
				g.Expect(ingressPorts).To(ContainElement(int32(8778)), "jolokia port")
				g.Expect(ingressPorts).To(ContainElement(int32(61616)), "all-protocols port")
				g.Expect(ingressPorts).To(ContainElement(int32(5672)), "amqp port from resource template")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for broker to be ready")
			brokerKey := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCr.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			clientPodName := "netpol-positive-client"
			createClientPod(clientPodName, defaultNamespace)
			defer deleteClientPod(clientPodName, defaultNamespace)

			brokerFQDN := common.OrdinalFQDNS(brokerCr.Name, defaultNamespace, 0)

			By("verifying the broker-properties acceptor port is reachable from a separate client pod")
			Eventually(func(g Gomega) {
				result, err := RunCommandInPodWithNamespace(
					clientPodName, defaultNamespace, clientPodName,
					[]string{"/bin/bash", "-c", fmt.Sprintf("timeout 5 bash -c 'echo > /dev/tcp/%s/5672'", brokerFQDN)})
				g.Expect(err).To(BeNil(), "exec should succeed")
				g.Expect(result).NotTo(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})
	})

	Context("broker-properties acceptor without ResourceTemplate blocks port via NetworkPolicy", func() {
		It("blocks cross-pod traffic on a port not listed in the NetworkPolicy", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" || !isOpenshift {
				Skip("requires OpenShift cluster with NP-enforcing CNI for cross-pod connectivity testing")
			}

			brokerCr, createdCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.BrokerCluster) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.BrokerProperties = brokerPropertiesAMQP()
			})

			netpolKey := types.NamespacedName{
				Name:      brokerCr.Name + "-netpol",
				Namespace: defaultNamespace,
			}

			By("verifying the NetworkPolicy does NOT include port 5672")
			netpolObject := netv1.NetworkPolicy{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
				ingressPorts := collectIngressPorts(&netpolObject)
				g.Expect(ingressPorts).NotTo(ContainElement(int32(5672)), "5672 should not be in NP without ResourceTemplate")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for broker to be ready")
			brokerKey := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCr.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("confirming the broker IS listening on 5672 from inside its own pod (loopback bypasses NP)")
			brokerPodName := namer.CrToSS(brokerCr.Name) + "-0"
			Eventually(func(g Gomega) {
				result, err := RunCommandInPod(brokerPodName, brokerCr.Name+"-container",
					[]string{"/bin/bash", "-c", "timeout 3 bash -c 'echo > /dev/tcp/localhost/5672'"})
				g.Expect(err).To(BeNil())
				g.Expect(result).NotTo(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			clientPodName := "netpol-negative-client"
			createClientPod(clientPodName, defaultNamespace)
			defer deleteClientPod(clientPodName, defaultNamespace)

			brokerFQDN := common.OrdinalFQDNS(brokerCr.Name, defaultNamespace, 0)

			By("verifying cross-pod connection to port 5672 is blocked by NetworkPolicy")
			Eventually(func(g Gomega) {
				result, err := RunCommandInPodWithNamespace(
					clientPodName, defaultNamespace, clientPodName,
					[]string{"/bin/bash", "-c", fmt.Sprintf("timeout 5 bash -c 'echo > /dev/tcp/%s/5672' 2>&1; echo EXIT_CODE=$?", brokerFQDN)})
				g.Expect(err).To(BeNil(), "exec should succeed even if the TCP probe fails")
				g.Expect(result).NotTo(BeNil())
				g.Expect(*result).To(ContainSubstring("EXIT_CODE="), "should report exit code")
				g.Expect(*result).NotTo(ContainSubstring("EXIT_CODE=0"), "connection should NOT succeed when NP blocks port 5672")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})
	})
})

func createClientPod(name, namespace string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: &boolTrue,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: &boolFalse,
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
					},
					Name:    name,
					Image:   "registry.access.redhat.com/ubi8/ubi:8.9",
					Command: []string{"/bin/bash", "-c", "sleep infinity"},
				},
			},
		},
	}

	Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

	podKey := types.NamespacedName{Name: name, Namespace: namespace}
	Eventually(func(g Gomega) {
		fetched := &corev1.Pod{}
		g.Expect(k8sClient.Get(ctx, podKey, fetched)).Should(Succeed())
		g.Expect(fetched.Status.ContainerStatuses).NotTo(BeEmpty())
		g.Expect(fetched.Status.ContainerStatuses[0].State.Running).NotTo(BeNil())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

func deleteClientPod(name, namespace string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := k8sClient.Delete(ctx, pod)
	Expect(err).To(BeNil())

	podKey := types.NamespacedName{Name: name, Namespace: namespace}
	Eventually(func(g Gomega) {
		fetched := &corev1.Pod{}
		err := k8sClient.Get(ctx, podKey, fetched)
		if err != nil {
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "pod should be gone or terminating")
			return
		}
		g.Expect(fetched.DeletionTimestamp).NotTo(BeNil(), "pod should be terminating")
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

func collectIngressPorts(netpol *netv1.NetworkPolicy) []int32 {
	var ports []int32
	for _, rule := range netpol.Spec.Ingress {
		for _, p := range rule.Ports {
			if p.Port != nil {
				ports = append(ports, int32(p.Port.IntValue()))
			}
		}
	}
	return ports
}
