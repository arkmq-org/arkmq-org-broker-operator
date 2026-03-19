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

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/namer"
)

var _ = Describe("NetworkPolicy for broker operand", Label("network-policy"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	// brokerPropertiesAMQP returns the broker-properties that configure an
	// acceptor on port 5672 speaking AMQP. Used by both positive and negative
	// tests so the broker always listens on the same port.
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
			brokerCr, createdCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.Broker) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.BrokerProperties = brokerPropertiesAMQP()
				candidate.Spec.ResourceTemplates = []brokerv1beta2.ResourceTemplate{
					{
						Selector: &brokerv1beta2.ResourceSelector{
							Kind: &netpolKind,
						},
						// ResourceTemplate patches REPLACE the entire spec.ingress list
						// (no merge key on NetworkPolicyIngressRule), so all desired
						// ports must be specified here -- not just the extra one.
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"kind":       "NetworkPolicy",
								"apiVersion": "networking.k8s.io/v1",
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

			// TCP connectivity check from a separate pod (not loopback) to
			// verify that the NetworkPolicy allows cross-pod traffic on 5672.
			// Bash's /dev/tcp opens a TCP socket; timeout kills it if it hangs.
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

			brokerCr, createdCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.Broker) {
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

			// Cross-pod TCP probe to a port NOT in the NetworkPolicy.
			// The command appends "EXIT_CODE=$?" so the exec always returns
			// success (the outer echo never fails), letting us inspect the
			// connection result from the output rather than the exec error.
			By("verifying cross-pod connection to port 5672 is blocked by NetworkPolicy")
			Consistently(func(g Gomega) {
				result, err := RunCommandInPodWithNamespace(
					clientPodName, defaultNamespace, clientPodName,
					[]string{"/bin/bash", "-c", fmt.Sprintf("timeout 5 bash -c 'echo > /dev/tcp/%s/5672' 2>&1; echo EXIT_CODE=$?", brokerFQDN)})
				g.Expect(err).To(BeNil(), "exec should succeed even if the TCP probe fails")
				g.Expect(result).NotTo(BeNil())
				g.Expect(*result).To(ContainSubstring("EXIT_CODE="), "should report exit code")
				g.Expect(*result).NotTo(ContainSubstring("EXIT_CODE=0"), "connection should NOT succeed when NP blocks port 5672")
			}, "30s", "10s").Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})
	})
})

var _ = Describe("BrokerService NetworkPolicy for BrokerApp ports", Label("network-policy"), func() {

	var installedCertManager bool = false

	BeforeEach(func() {
		BeforeEachSpec()

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
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
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" && installedCertManager {
			Expect(UninstallCertManager()).To(Succeed())
			installedCertManager = false
		}
		AfterEachSpec()
	})

	It("includes BrokerApp acceptor ports in the NetworkPolicy created for the broker", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" || !isOpenshift {
			Skip("requires OpenShift cluster with NP-enforcing CNI")
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()

		sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
		By("installing broker cert")
		InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = sharedOperandCertName
			candidate.Spec.CommonName = serviceName
			candidate.Spec.DNSNames = []string{
				serviceName,
				fmt.Sprintf("%s.%s", serviceName, defaultNamespace),
				fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()),
				common.ClusterDNSWildCard(serviceName, defaultNamespace),
			}
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

		By("deploying a BrokerService")
		svc := brokerv1beta2.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: brokerv1beta2.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"env": "netpol-test"},
			},
			Spec: brokerv1beta2.BrokerServiceSpec{},
		}
		Expect(k8sClient.Create(ctx, &svc)).Should(Succeed())

		serviceKey := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
		createdSvc := &brokerv1beta2.BrokerService{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, serviceKey, createdSvc)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(createdSvc.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying the NetworkPolicy exists with only restricted ports before any app is bound")
		netpolKey := types.NamespacedName{
			Name:      serviceName + "-netpol",
			Namespace: defaultNamespace,
		}
		netpolObject := netv1.NetworkPolicy{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
			ports := collectIngressPorts(&netpolObject)
			g.Expect(ports).To(ContainElement(int32(8778)), "Jolokia port")
			g.Expect(ports).To(ContainElement(int32(8888)), "Prometheus port")
			g.Expect(ports).To(HaveLen(2), "only restricted ports before app binding")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deploying a BrokerApp that binds to the service")
		appPort := int32(62616)
		app := brokerv1beta2.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: brokerv1beta2.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netpol-app",
				Namespace: defaultNamespace,
			},
			Spec: brokerv1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "netpol-test"},
				},
				Acceptor: brokerv1beta2.AppAcceptorType{
					Port: appPort,
				},
				Capabilities: []brokerv1beta2.AppCapabilityType{
					{
						ProducerOf: []brokerv1beta2.AppAddressType{{Address: "TEST.QUEUE"}},
						ConsumerOf: []brokerv1beta2.AppAddressType{{Address: "TEST.QUEUE"}},
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

		Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

		appKey := types.NamespacedName{Name: app.Name, Namespace: app.Namespace}
		createdApp := &brokerv1beta2.BrokerApp{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying the NetworkPolicy now includes the BrokerApp acceptor port")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
			ports := collectIngressPorts(&netpolObject)
			g.Expect(ports).To(ContainElement(int32(8778)), "Jolokia port")
			g.Expect(ports).To(ContainElement(int32(8888)), "Prometheus port")
			g.Expect(ports).To(ContainElement(appPort), "BrokerApp acceptor port")
			g.Expect(ports).To(HaveLen(3), "restricted ports + app port")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting the BrokerApp and verifying the port is removed from the NetworkPolicy")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
			ports := collectIngressPorts(&netpolObject)
			g.Expect(ports).NotTo(ContainElement(appPort), "app port removed after unbind")
			g.Expect(ports).To(HaveLen(2), "only restricted ports after app removal")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		CleanResource(&svc, svc.Name, defaultNamespace)
	})
})

// createClientPod deploys a minimal UBI pod for cross-pod connectivity testing.
// The pod runs "sleep infinity" and is ready once its container is running.
func createClientPod(name, namespace string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
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
	k8sClient.Delete(ctx, pod)

	podKey := types.NamespacedName{Name: name, Namespace: namespace}
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, podKey, &corev1.Pod{})
		g.Expect(err).To(HaveOccurred(), "pod should be gone")
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

// collectIngressPorts extracts all port numbers from all ingress rules
// of a NetworkPolicy into a flat slice for easy assertion.
func collectIngressPorts(netpol *netv1.NetworkPolicy) []int32 {
	_ = context.Background()
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
