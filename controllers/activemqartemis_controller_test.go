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
	"container/list"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha4"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/client/clientset/versioned/typed/broker/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/configmaps"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	ss "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/statefulsets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/blang/semver/v4"

	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/cr2jinja2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"

	"github.com/Azure/go-amqp"
	routev1 "github.com/openshift/api/route/v1"
	netv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("artemis controller", func() {

	brokerPropertiesMatchString := "broker.properties"

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("tls secret reuse", Label("tls-secret-reuse"), func() {
		It("console and acceptor share one secret", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				commonSecretName := "common-amq-tls-secret"
				commonSecret, err := CreateTlsSecret(commonSecretName, defaultNamespace, defaultPassword, defaultSanDnsNames)
				Expect(err).To(BeNil())

				Expect(k8sClient.Create(ctx, commonSecret)).Should(Succeed())

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

					candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
						{
							Name:               "amqp-ssl",
							Protocols:          "amqp",
							Port:               5671,
							SSLEnabled:         true,
							SSLSecret:          commonSecretName,
							EnabledProtocols:   "TLSv1,TLSv1.1,TLSv1.2",
							NeedClientAuth:     false,
							WantClientAuth:     false,
							Expose:             true,
							AnycastPrefix:      "jms.queue.",
							MulticastPrefix:    "/topic/",
							ConnectionsAllowed: 5,
						},
					}

					candidate.Spec.Connectors = []brokerv1beta1.ConnectorType{
						{
							Name:       "connector-ssl",
							Host:       "0.0.0.0",
							Type:       "tcp",
							Port:       5671,
							SSLEnabled: true,
							SSLSecret:  commonSecretName,
							Expose:     true,
						},
					}
				})

				By("verify pod is up")
				WaitForPod(brokerCr.Name)

				By("checking service is created for console")
				serviceName := brokerCr.Name + "-amqp-ssl-0-svc"
				service := corev1.Service{}
				serviceKey := types.NamespacedName{
					Name:      serviceName,
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &service)).To(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking service is created for acceptor")
				serviceName = brokerCr.Name + "-amqp-ssl-0-svc"
				service = corev1.Service{}
				serviceKey = types.NamespacedName{
					Name:      serviceName,
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &service)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking service is created for connector")
				serviceName = brokerCr.Name + "-connector-ssl-0-svc"
				service = corev1.Service{}
				serviceKey = types.NamespacedName{
					Name:      serviceName,
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &service)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				if isOpenshift {

					By("check route is created for console")
					routeKey := types.NamespacedName{
						Name:      brokerCr.Name + "-wconsj-0-svc-rte",
						Namespace: defaultNamespace,
					}
					route := routev1.Route{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
						g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("wconsj-0")))
						g.Expect(route.Spec.To.Kind).To(Equal("Service"))
						g.Expect(route.Spec.To.Name).To(Equal(brokerCr.Name + "-wconsj-0-svc"))
						g.Expect(route.Spec.TLS.Termination).To(BeEquivalentTo(routev1.TLSTerminationPassthrough))
						g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(BeEquivalentTo(routev1.InsecureEdgeTerminationPolicyNone))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("checking route is created for acceptor")
					routeKey = types.NamespacedName{
						Name:      brokerCr.Name + "-amqp-ssl-0-svc-rte",
						Namespace: defaultNamespace,
					}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
						g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("amqp-ssl-0")))
						g.Expect(route.Spec.To.Kind).To(Equal("Service"))
						g.Expect(route.Spec.To.Name).To(Equal(brokerCr.Name + "-amqp-ssl-0-svc"))
						g.Expect(route.Spec.TLS.Termination).To(BeEquivalentTo(routev1.TLSTerminationPassthrough))
						g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(BeEquivalentTo(routev1.InsecureEdgeTerminationPolicyNone))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("checking route is created for connector")
					routeKey = types.NamespacedName{
						Name:      brokerCr.Name + "-connector-ssl-0-svc-rte",
						Namespace: defaultNamespace,
					}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
						g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("connector-ssl-0")))
						g.Expect(route.Spec.To.Kind).To(Equal("Service"))
						g.Expect(route.Spec.To.Name).To(Equal(brokerCr.Name + "-connector-ssl-0-svc"))
						g.Expect(route.Spec.TLS.Termination).To(BeEquivalentTo(routev1.TLSTerminationPassthrough))
						g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(BeEquivalentTo(routev1.InsecureEdgeTerminationPolicyNone))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				} else {
					By("check ingress is created for console")
					ingKey := types.NamespacedName{
						Name:      brokerCr.Name + "-wconsj-0-svc-ing",
						Namespace: defaultNamespace,
					}
					ingress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

						g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
						g.Expect(ingress.Spec.Rules[0].Host).To(ContainSubstring(defaultTestIngressDomain))
						g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(BeEquivalentTo(brokerCr.Name + "-wconsj-0-svc"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(BeEquivalentTo("wconsj-0"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
						g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

						g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
						g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(ContainSubstring(defaultTestIngressDomain))

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("check ingress is created for acceptor")
					ingKey = types.NamespacedName{
						Name:      brokerCr.Name + "-amqp-ssl-0-svc-ing",
						Namespace: defaultNamespace,
					}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

						g.Expect(len(ingress.Spec.Rules)).To(Equal(1)) //artemis-broker-amqp-ssl-0-svc-ing.
						g.Expect(ingress.Spec.Rules[0].Host).To(Equal(brokerCr.Name + "-amqp-ssl-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))
						g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(brokerCr.Name + "-amqp-ssl-0-svc"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("amqp-ssl-0"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
						g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(Equal(netv1.PathTypePrefix))

						g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
						g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(Equal(brokerCr.Name + "-amqp-ssl-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("check ingress is created for connector")
					ingKey = types.NamespacedName{
						Name:      brokerCr.Name + "-connector-ssl-0-svc-ing",
						Namespace: defaultNamespace,
					}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

						g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
						g.Expect(ingress.Spec.Rules[0].Host).To(Equal(brokerCr.Name + "-connector-ssl-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))
						g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(brokerCr.Name + "-connector-ssl-0-svc"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("connector-ssl-0"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
						g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(Equal(netv1.PathTypePrefix))

						g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
						g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(Equal(brokerCr.Name + "-connector-ssl-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(commonSecret, commonSecret.Name, defaultNamespace)
			}
		})
	})

	Context("Statefulset options", Label("statefulset-options"), func() {
		It("revision history limit", func() {
			//Deploy a broker cr
			var limit int32 = 1000
			brokerCr, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.RevisionHistoryLimit = &limit
			})

			//retrieve statefylset
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			//checking the limits
			Expect(createdSs.Spec.RevisionHistoryLimit).To(Not(BeNil()))
			Expect(*createdSs.Spec.RevisionHistoryLimit).To(Equal(limit))

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})
	})

	Context("pod disruption budget", Label("pod-disruption-budget"), func() {
		It("pod disruption budget validation", func() {
			minOne := intstr.FromInt(1)
			matchLabels := make(map[string]string)
			matchLabels["my-label"] = "my-value"

			mySelector := &metav1.LabelSelector{
				MatchLabels: matchLabels,
			}
			pdb := policyv1.PodDisruptionBudgetSpec{
				MinAvailable: &minOne,
				Selector:     mySelector,
			}

			brokerCr, createdCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.PodDisruptionBudget = &pdb
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			})

			brokerKey := types.NamespacedName{
				Name:      brokerCr.Name,
				Namespace: defaultNamespace,
			}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

				pdbCondition := meta.FindStatusCondition(createdCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(pdbCondition).NotTo(BeNil())
				g.Expect(pdbCondition.Reason).To(Equal(brokerv1beta1.ValidConditionPDBNonNilSelectorReason))
				g.Expect(pdbCondition.Message).To(Equal(common.PDBNonNilSelectorMessage))

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("pod disruption apply number", func() {
			minOne := intstr.FromInt(1)
			pdb := policyv1.PodDisruptionBudgetSpec{
				MinAvailable: &minOne,
			}

			brokerCr, createdCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.PodDisruptionBudget = &pdb
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			})

			pdbKey := types.NamespacedName{
				Name:      brokerCr.Name + "-pdb",
				Namespace: defaultNamespace,
			}

			pdbObject := policyv1.PodDisruptionBudget{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pdbKey, &pdbObject)).Should(Succeed())

				g.Expect(*pdbObject.Spec.MinAvailable).To(BeEquivalentTo(intstr.FromInt(1)))
				labelValue, ok := pdbObject.Spec.Selector.MatchLabels["ActiveMQArtemis"]
				g.Expect(ok).To(BeTrue())
				g.Expect(labelValue).To(Equal(createdCr.Name))

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					g.Expect(pdbObject.Status.DisruptionsAllowed).To(Equal(int32(1)))
					g.Expect(pdbObject.Status.CurrentHealthy).To(Equal(int32(2)))
					g.Expect(pdbObject.Status.DesiredHealthy).To(Equal(int32(1)))
					g.Expect(pdbObject.Status.ExpectedPods).To(Equal(int32(2)))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("pod disruption apply percentage", func() {
			max50cent := intstr.FromString("50%")
			pdb := policyv1.PodDisruptionBudgetSpec{
				MaxUnavailable: &max50cent,
			}

			brokerCr, createdCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.PodDisruptionBudget = &pdb
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			})

			pdbKey := types.NamespacedName{
				Name:      brokerCr.Name + "-pdb",
				Namespace: defaultNamespace,
			}

			pdbObject := policyv1.PodDisruptionBudget{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pdbKey, &pdbObject)).Should(Succeed())
				g.Expect(*pdbObject.Spec.MaxUnavailable).To(BeEquivalentTo(max50cent))
				labelValue, ok := pdbObject.Spec.Selector.MatchLabels["ActiveMQArtemis"]
				g.Expect(ok).To(BeTrue())
				g.Expect(labelValue).To(Equal(createdCr.Name))

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("pod disruption update", func() {
			minOne := intstr.FromInt(1)
			minTwo := intstr.FromInt(2)
			pdb := policyv1.PodDisruptionBudgetSpec{
				MinAvailable: &minOne,
			}

			brokerCr, createdCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.PodDisruptionBudget = &pdb
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			})

			pdbKey := types.NamespacedName{
				Name:      brokerCr.Name + "-pdb",
				Namespace: defaultNamespace,
			}

			pdbObject := policyv1.PodDisruptionBudget{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pdbKey, &pdbObject)).Should(Succeed())

				g.Expect(*pdbObject.Spec.MinAvailable).To(BeEquivalentTo(intstr.FromInt(1)))
				labelValue, ok := pdbObject.Spec.Selector.MatchLabels["ActiveMQArtemis"]
				g.Expect(ok).To(BeTrue())
				g.Expect(labelValue).To(Equal(createdCr.Name))

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					g.Expect(pdbObject.Status.DisruptionsAllowed).To(Equal(int32(1)))
					g.Expect(pdbObject.Status.CurrentHealthy).To(Equal(int32(2)))
					g.Expect(pdbObject.Status.DesiredHealthy).To(Equal(int32(1)))
					g.Expect(pdbObject.Status.ExpectedPods).To(Equal(int32(2)))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("update pdb minAvaliable")
			prevResourceVersion := pdbObject.ResourceVersion

			brokerKey := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCr)).Should(Succeed())
				createdCr.Spec.DeploymentPlan.PodDisruptionBudget.MinAvailable = &minTwo
				g.Expect(k8sClient.Update(ctx, createdCr)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pdbKey, &pdbObject)).Should(Succeed())
				g.Expect(*pdbObject.Spec.MinAvailable).To(BeEquivalentTo(intstr.FromInt(2)))
				g.Expect(prevResourceVersion).NotTo(Equal(pdbObject.ResourceVersion))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})
	})

	Context("broker status on resource error", func() {
		It("two acceptors with names too long for kube", func() {
			By("deploy a broker")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:   "my-extra-long-acceptor",
						Port:   61626,
						Expose: true,
					},
					{
						Name:   "my-clashing-acceptor",
						Port:   61627,
						Expose: true,
					},
				}

				if !isOpenshift {
					candidate.Spec.IngressDomain = defaultTestIngressDomain
				}
			})
			By("checking the CR gets status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)
				g.Expect(condition).NotTo(BeNil())
				if isOpenshift {
					// no limit like kube
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				} else {
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.DeployedConditionCrudKindErrorReason))
					g.Expect(condition.Message).To(ContainSubstring("my-clashing-acceptor"))
					g.Expect(condition.Message).To(ContainSubstring("failed"))

					condition = meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Message).ShouldNot(ContainSubstring("invalid character"))

					// resource error, deployed false, ready false
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeFalse())

				}

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("two acceptors with port clash", func() {
			By("deploy a broker")
			samePort := int32(61636)
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:   "first",
						Port:   samePort,
						Expose: true,
					},
					{
						Name:   "second",
						Port:   samePort,
						Expose: true,
					},
				}

				if !isOpenshift {
					candidate.Spec.IngressDomain = defaultTestIngressDomain
				}
			})

			By("checking the CR gets status updated as invalid")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

				if verbose {
					fmt.Printf("\nStatus:%v\n", createdBrokerCr.Status)
				}
				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedDuplicateAcceptorPort))

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

	})

	Context("broker versions map", func() {
		versionList := []string{
			"7.8.1",
			"7.8.2",
			"7.8.3",
			"7.10.0",
			"7.10.1",
			"7.10.2",
			"7.9.0",
			"7.9.1",
			"7.9.2",
			"7.9.3",
			"7.9.4",
		}
		versionsMap := make([]semver.Version, len(versionList))
		for i, raw := range versionList {
			v := semver.MustParse(raw)
			versionsMap[i] = v
		}
		semver.Sort(versionsMap)

		It("resolve various versions", func() {
			resolved := common.ResolveBrokerVersion(versionsMap, "")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.10.2"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.10.2"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7.8")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.8.3"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7.9")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.9.4"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7.9.0")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.9.0"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7.10")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.10.2"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7.10.1")
			Expect(resolved).NotTo(BeNil())
			Expect(resolved.String()).To(Equal("7.10.1"))

			resolved = common.ResolveBrokerVersion(versionsMap, "7.11")
			Expect(resolved).To(BeNil())

			resolved = common.ResolveBrokerVersion(versionsMap, "8")
			Expect(resolved).To(BeNil())

		})

	})

	Context("broker versions", func() {
		It("version validation when both version and images are explicitly specified", func() {
			By("deploy a broker with images specified")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = version.GetDefaultVersion()
				candidate.Spec.DeploymentPlan.Image = "myrepo/my-image:1.0"
				candidate.Spec.DeploymentPlan.InitImage = "myrepo/my-init-image:1.0"
			})

			By("checking the CR gets status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("version validation when version is loosly specified and images are explicitly specified", func() {
			By("deploy a broker with images specified")
			latestVersion := semver.MustParse(version.GetDefaultVersion())
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = strconv.FormatUint(latestVersion.Major, 10)
				candidate.Spec.DeploymentPlan.Image = "myrepo/my-image:1.0"
				candidate.Spec.DeploymentPlan.InitImage = "myrepo/my-init-image:1.0"
			})

			By("checking the CR gets status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionPresentAndEqual(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType, metav1.ConditionUnknown)).Should(BeTrue())

				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionUnknownReason))
				g.Expect(condition.Message).To(ContainSubstring(common.NotSupportedImageVersionMessage))
			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("version validation when version and images are loosly specified", func() {
			By("deploy a broker with images default and version")
			latestVersion := semver.MustParse(version.GetDefaultVersion())
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = strconv.FormatUint(latestVersion.Major, 10)
				candidate.Spec.DeploymentPlan.Image = "placeholder"
				candidate.Spec.DeploymentPlan.InitImage = ""
			})

			By("checking the CR ok")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("images need to be in pairs", func() {
			By("deploy a broker with one image specified")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.InitImage = "myrepo/my-init-image:1.0"
			})

			By("checking the CR gets status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

				g.Expect(meta.IsStatusConditionPresentAndEqual(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType, metav1.ConditionUnknown)).Should(BeTrue())
				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionUnknownReason))
				g.Expect(condition.Message).To(ContainSubstring("both"))

				By("checking status has useful info on what the operator would deploy")
				g.Expect(createdBrokerCr.Status.Version.Image).ShouldNot(BeEmpty())
				g.Expect(createdBrokerCr.Status.Version.InitImage).Should(ContainSubstring("my"))
				g.Expect(createdBrokerCr.Status.Version.BrokerVersion).ShouldNot(BeEmpty())

				g.Expect(createdBrokerCr.Status.Upgrade.MajorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.MinorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.PatchUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.SecurityUpdates).Should(BeFalse())

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("images need to be in pairs, image placeholder ok", func() {
			By("deploy a broker with just image placeholder")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.Image = "placeholder"
			})

			By("checking the CR does not get rejected with status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

				g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())

				By("checking status has useful info on what the operator would deploy")
				g.Expect(createdBrokerCr.Status.Version.Image).ShouldNot(BeEmpty())
				g.Expect(createdBrokerCr.Status.Version.BrokerVersion).ShouldNot(BeEmpty())

				g.Expect(createdBrokerCr.Status.Upgrade.MajorUpdates).Should(BeTrue())
				g.Expect(createdBrokerCr.Status.Upgrade.MinorUpdates).Should(BeTrue())
				g.Expect(createdBrokerCr.Status.Upgrade.PatchUpdates).Should(BeTrue())
				g.Expect(createdBrokerCr.Status.Upgrade.SecurityUpdates).Should(BeTrue())

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("version validation invalid", func() {
			By("deploy a broker with bad version format")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = "x-y"
			})

			By("checking the CR gets rejected with status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

				g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())
				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionInvalidVersionReason))
				g.Expect(condition.Message).Should(ContainSubstring("Version"))
			}, timeout, interval).Should(Succeed())

			// cleanup
			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("version validation not found", func() {
			By("deploy a broker with bad version format")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = "77.8"
			})

			By("checking the CR gets rejected with status updated")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

				g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())
				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionInvalidVersionReason))
				g.Expect(condition.Message).Should(ContainSubstring("version"))
			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("version handling when images are explicitly specified", func() {
			By("deploy a broker with images specified")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.Image = "myrepo/my-image:1.0"
				candidate.Spec.DeploymentPlan.InitImage = "myrepo/my-init-image:1.0"
			})

			By("checking the image should respect the CR values")
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
				mainContainer := createdSs.Spec.Template.Spec.Containers[0]
				g.Expect(mainContainer.Image).To(Equal("myrepo/my-image:1.0"))
				initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
				g.Expect(initContainer.Image).To(Equal("myrepo/my-init-image:1.0"))

			}, timeout, interval).Should(Succeed())

			By("checking the CR status")
			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				g.Expect(meta.IsStatusConditionPresentAndEqual(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType, metav1.ConditionUnknown)).Should(BeTrue())

				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionUnknownReason))
				g.Expect(condition.Message).To(ContainSubstring(common.UnkonwonImageVersionMessage))

				g.Expect(createdBrokerCr.Status.Version.Image).Should(ContainSubstring("my"))
				g.Expect(createdBrokerCr.Status.Version.InitImage).Should(ContainSubstring("my"))
				g.Expect(createdBrokerCr.Status.Version.BrokerVersion).ShouldNot(BeEmpty())

				g.Expect(createdBrokerCr.Status.Upgrade.MajorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.MinorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.PatchUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.SecurityUpdates).Should(BeFalse())

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("specify only major version", func() {
			os.Setenv("RELATED_IMAGE_BROKER_KUBERNETES_"+version.GetDefaultCompactVersion(), "quay.io/arkmq-org/fake-broker:latest")
			os.Setenv("RELATED_IMAGE_BROKER_INIT_"+version.GetDefaultCompactVersion(), "quay.io/arkmq-org/fake-broker-init:latest")
			defer func() {
				os.Unsetenv("RELATED_IMAGE_BROKER_KUBERNETES_" + version.GetDefaultCompactVersion())
				os.Unsetenv("RELATED_IMAGE_BROKER_INIT_" + version.GetDefaultCompactVersion())
			}()
			By("deploy a broker")
			verFields := strings.Split(version.GetDefaultVersion(), ".")
			major := verFields[0]
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = major
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

				g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())

				g.Expect(createdBrokerCr.Status.Version.Image).Should(ContainSubstring("fake"))
				g.Expect(createdBrokerCr.Status.Version.InitImage).Should(ContainSubstring("fake"))
				g.Expect(createdBrokerCr.Status.Version.BrokerVersion).Should(Equal(version.GetDefaultVersion()))

				g.Expect(createdBrokerCr.Status.Upgrade.MajorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.MinorUpdates).Should(BeTrue())
				g.Expect(createdBrokerCr.Status.Upgrade.PatchUpdates).Should(BeTrue())
				g.Expect(createdBrokerCr.Status.Upgrade.SecurityUpdates).Should(BeTrue())

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("specify only major.minor version", func() {
			os.Setenv("RELATED_IMAGE_BROKER_KUBERNETES_"+version.GetDefaultCompactVersion(), "quay.io/arkmq-org/fake-broker:latest")
			os.Setenv("RELATED_IMAGE_BROKER_INIT_"+version.GetDefaultCompactVersion(), "quay.io/arkmq-org/fake-broker-init:latest")
			defer func() {
				os.Unsetenv("RELATED_IMAGE_BROKER_KUBERNETES_" + version.GetDefaultCompactVersion())
				os.Unsetenv("RELATED_IMAGE_BROKER_INIT_" + version.GetDefaultCompactVersion())
			}()
			By("deploy a broker")
			verFields := strings.Split(version.GetDefaultVersion(), ".")
			major := verFields[0]
			minor := verFields[1]
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = major + "." + minor
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

				g.Expect(createdBrokerCr.Status.Upgrade.MajorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.MinorUpdates).Should(BeFalse())
				g.Expect(createdBrokerCr.Status.Upgrade.PatchUpdates).Should(BeTrue())
				g.Expect(createdBrokerCr.Status.Upgrade.SecurityUpdates).Should(BeTrue())

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("default broker versions", func() {
			os.Setenv("RELATED_IMAGE_BROKER_KUBERNETES_"+version.GetDefaultCompactVersion(), "quay.io/arkmq-org/fake-broker:latest")
			os.Setenv("RELATED_IMAGE_BROKER_INIT_"+version.GetDefaultCompactVersion(), "quay.io/arkmq-org/fake-broker-init:latest")
			defer func() {
				os.Unsetenv("RELATED_IMAGE_BROKER_KUBERNETES_" + version.GetDefaultCompactVersion())
				os.Unsetenv("RELATED_IMAGE_BROKER_INIT_" + version.GetDefaultCompactVersion())
			}()
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
		})

		It("ok with valid=unknown, relaxed version validation", func() {
			By("deploy a broker")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Version = ""
				candidate.Spec.DeploymentPlan.Image = ""
				candidate.Spec.DeploymentPlan.InitImage = ""
			})

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				var initImage = ""
				var versionString = ""
				brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

					By("verifying started")

					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					By("extracting version info from the CR status")

					initImage = createdBrokerCr.Status.Version.InitImage
					versionString = createdBrokerCr.Status.Version.BrokerVersion

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Eventually(func(g Gomega) {

					By("updating with invalid  but non fatal version and just init image")

					g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

					createdBrokerCr.Spec.Version = versionString
					createdBrokerCr.Spec.DeploymentPlan.InitImage = initImage

					g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Eventually(func(g Gomega) {

					By("verifying restarted")

					g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())

					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					By("verifying unknown validation status but ok")

					g.Expect(meta.IsStatusConditionTrue(createdBrokerCr.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

	})

	Context("broker resource tracking", Label("broker-resource-tracking-context"), func() {
		It("default user credential secret", func() {
			By("deploy a broker")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)

			var amqUser []byte
			var amqPassword []byte
			var amqClusterUser []byte
			var amqClusterPassword []byte

			By("checking the default credential secret created")
			credSecretKey := types.NamespacedName{
				Name:      brokerCr.Name + "-credentials-secret",
				Namespace: defaultNamespace,
			}
			credSecret := &corev1.Secret{}
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, credSecretKey, credSecret)).Should(Succeed())

				var amqKeyFound bool
				g.Expect(len(credSecret.Data)).To(Equal(4))
				amqUser, amqKeyFound = credSecret.Data["AMQ_USER"]
				g.Expect(amqKeyFound).Should(BeTrue())
				amqPassword, amqKeyFound = credSecret.Data["AMQ_PASSWORD"]
				g.Expect(amqKeyFound).Should(BeTrue())
				amqClusterUser, amqKeyFound = credSecret.Data["AMQ_CLUSTER_USER"]
				g.Expect(amqKeyFound).Should(BeTrue())
				amqClusterPassword, amqKeyFound = credSecret.Data["AMQ_CLUSTER_PASSWORD"]
				g.Expect(amqKeyFound).Should(BeTrue())

				g.Expect(len(credSecret.OwnerReferences) > 0).Should(BeTrue())
				ownerFound := false
				for _, oref := range credSecret.OwnerReferences {
					if oref.Kind == artemisGvk.Kind && oref.Name == brokerCr.Name {
						ownerFound = true
						break
					}
				}
				g.Expect(ownerFound).To(BeTrue())

				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())

				initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_USER" {
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"), envVar)
					}
					if envVar.Name == "AMQ_PASSWORD" {
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"), envVar)
					}
					if envVar.Name == "AMQ_CLUSTER_USER" {
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"), envVar)
					}
					if envVar.Name == "AMQ_CLUSTER_PASSWORD" {
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"), envVar)
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("update the broker to trigger reconcile")
			brokerKey := types.NamespacedName{
				Name:      brokerCr.Name,
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				createdBrokerCr.Spec.Env = append(createdBrokerCr.Spec.Env, corev1.EnvVar{Name: "NEW_VAR", Value: "NEW_VALUE"})
				g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			newCreatedBrokerCr := brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, &newCreatedBrokerCr)).Should(Succeed())
				g.Expect(len(newCreatedBrokerCr.Spec.Env)).To(Equal(1))
				g.Expect(newCreatedBrokerCr.Spec.Env[0].Name).To(Equal("NEW_VAR"))
				g.Expect(newCreatedBrokerCr.Spec.Env[0].Value).To(Equal("NEW_VALUE"))
			}, timeout, interval).Should(Succeed())

			newSS := &appsv1.StatefulSet{}
			credSecretAfterRecon := &corev1.Secret{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, newSS)).Should(Succeed())

				container := newSS.Spec.Template.Spec.Containers[0]
				newVarFound := false
				for _, envVar := range container.Env {
					if envVar.Name == "NEW_VAR" {
						newVarFound = true
						g.Expect(envVar.Value).To(Equal("NEW_VALUE"))
					}
				}
				g.Expect(newVarFound).To(BeTrue())

				initContainer := newSS.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					switch envVar.Name {
					case "AMQ_USER":
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(credSecretKey.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"))
					case "AMQ_PASSWORD":
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(credSecretKey.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"))
					case "AMQ_CLUSTER_USER":
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(credSecretKey.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"))
					case "AMQ_CLUSTER_PASSWORD":
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(credSecretKey.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"))
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

				g.Expect(k8sClient.Get(ctx, credSecretKey, credSecretAfterRecon)).Should(Succeed())
				g.Expect(credSecretAfterRecon.Data["AMQ_USER"]).To(Equal(amqUser))
				g.Expect(credSecretAfterRecon.Data["AMQ_PASSWORD"]).To(Equal(amqPassword))
				g.Expect(credSecretAfterRecon.Data["AMQ_CLUSTER_USER"]).To(Equal(amqClusterUser))
				g.Expect(credSecretAfterRecon.Data["AMQ_CLUSTER_PASSWORD"]).To(Equal(amqClusterPassword))

				g.Expect(len(credSecretAfterRecon.OwnerReferences) > 0).Should(BeTrue())
				ownerFound := false
				for _, oref := range credSecretAfterRecon.OwnerReferences {
					if oref.Kind == artemisGvk.Kind && oref.Name == brokerCr.Name {
						ownerFound = true
						break
					}
				}
				g.Expect(ownerFound).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("default user credential secret with values in CR", func() {
			By("deploy a broker with adminUser and adminPassword specified")
			brokerCr, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.AdminUser = "adminuser"
				candidate.Spec.AdminPassword = "adminpassword"
			})

			By("checking the default credential secret created")
			credSecretKey := types.NamespacedName{
				Name:      brokerCr.Name + "-credentials-secret",
				Namespace: defaultNamespace,
			}
			credSecret := &corev1.Secret{}
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, credSecretKey, credSecret)).Should(Succeed())
				g.Expect(len(credSecret.Data)).To(Equal(4))
				g.Expect(string(credSecret.Data["AMQ_USER"])).To(Equal("adminuser"))
				g.Expect(string(credSecret.Data["AMQ_PASSWORD"])).To(Equal("adminpassword"))
				g.Expect(len(credSecret.OwnerReferences) > 0).Should(BeTrue())
				ownerFound := false
				for _, oref := range credSecret.OwnerReferences {
					if oref.Kind == artemisGvk.Kind && oref.Name == brokerCr.Name {
						ownerFound = true
						break
					}
				}
				g.Expect(ownerFound).To(BeTrue())

				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())

				initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_USER" {
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"), envVar)
					}
					if envVar.Name == "AMQ_PASSWORD" {
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"), envVar)
					}
					if envVar.Name == "AMQ_CLUSTER_USER" {
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"), envVar)
					}
					if envVar.Name == "AMQ_CLUSTER_PASSWORD" {
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(brokerCr.Name+"-credentials-secret"), envVar)
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"), envVar)
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
		})

		It("user credential secret", func() {
			brokerCrName := "broker-user-cred"
			userSecretName := brokerCrName + "-credentials-secret"
			secretData := make(map[string]string)
			secretData["AMQ_USER"] = "myuser"
			secretData["AMQ_PASSWORD"] = "mypassword"
			secretData["AMQ_CLUSTER_USER"] = "myclusteruser"
			secretData["AMQ_CLUSTER_PASSWORD"] = "myclusterpassword"

			By("deploy a user credential secret")
			secret, _ := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = userSecretName
				candidate.StringData = secretData
			})

			By("deploy a broker")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Name = brokerCrName
			})

			By("retrieve the secret and statefulset to check user secret takes effect")
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			secretKey := types.NamespacedName{Name: secret.Name, Namespace: defaultNamespace}
			secretAfterRecon := &corev1.Secret{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretKey, secretAfterRecon)).Should(Succeed())
				g.Expect(string(secretAfterRecon.Data["AMQ_USER"])).To(Equal("myuser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_PASSWORD"])).To(Equal("mypassword"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_USER"])).To(Equal("myclusteruser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_PASSWORD"])).To(Equal("myclusterpassword"))

				if len(secretAfterRecon.OwnerReferences) > 0 {
					for _, oref := range secretAfterRecon.OwnerReferences {
						g.Expect(oref.Kind).NotTo(Equal(artemisGvk.Kind))
					}
				}

				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
				initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					switch envVar.Name {
					case "AMQ_USER":
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"))
					case "AMQ_PASSWORD":
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"))
					case "AMQ_CLUSTER_USER":
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"))
					case "AMQ_CLUSTER_PASSWORD":
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"))
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("start to capture test log")
			StartCapturingLog()
			defer StopCapturingLog()

			By("update the broker to trigger reconcile")
			brokerKey := types.NamespacedName{
				Name:      brokerCr.Name,
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				createdBrokerCr.Spec.Env = append(createdBrokerCr.Spec.Env, corev1.EnvVar{Name: "NEW_VAR", Value: "NEW_VALUE"})
				g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			newCreatedBrokerCr := brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, &newCreatedBrokerCr)).Should(Succeed())
				g.Expect(len(newCreatedBrokerCr.Spec.Env)).To(Equal(1))
				g.Expect(newCreatedBrokerCr.Spec.Env[0].Name).To(Equal("NEW_VAR"))
				g.Expect(newCreatedBrokerCr.Spec.Env[0].Value).To(Equal("NEW_VALUE"))
			}, timeout, interval).Should(Succeed())

			newSS := &appsv1.StatefulSet{}
			secretAfterRecon = &corev1.Secret{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretKey, secretAfterRecon)).Should(Succeed())
				g.Expect(string(secretAfterRecon.Data["AMQ_USER"])).To(Equal("myuser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_PASSWORD"])).To(Equal("mypassword"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_USER"])).To(Equal("myclusteruser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_PASSWORD"])).To(Equal("myclusterpassword"))

				if len(secretAfterRecon.OwnerReferences) > 0 {
					for _, oref := range secretAfterRecon.OwnerReferences {
						g.Expect(oref.Kind).NotTo(Equal(artemisGvk.Kind))
					}
				}

				g.Expect(k8sClient.Get(ctx, ssKey, newSS)).Should(Succeed())
				initContainer := newSS.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					switch envVar.Name {
					case "AMQ_USER":
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"))
					case "AMQ_PASSWORD":
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"))
					case "AMQ_CLUSTER_USER":
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"))
					case "AMQ_CLUSTER_PASSWORD":
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"))
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

				container := newSS.Spec.Template.Spec.Containers[0]
				newVarFound := false
				for _, envVar := range container.Env {
					if envVar.Name == "NEW_VAR" {
						newVarFound = true
						g.Expect(envVar.Value).To(Equal("NEW_VALUE"))
					}
				}
				g.Expect(newVarFound).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			hasMatch, matchErr := MatchInCapturingLog("Failed to create new \\*v1\\.Secret")
			Expect(matchErr).To(BeNil())
			Expect(hasMatch).To(BeFalse())

			hasMatch, matchErr = MatchInCapturingLog("The secret " + secret.Name + " is ignored because its owner references doesn't include ActiveMQArtemis/" + brokerCr.Name)
			Expect(matchErr).To(BeNil())
			Expect(hasMatch).To(BeTrue())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
			CleanResource(secret, secret.Name, defaultNamespace)
		})

		It("user credential secret with CR values provided", func() {
			brokerCrName := "broker-user-cred"
			userSecretName := brokerCrName + "-credentials-secret"
			secretData := make(map[string]string)
			secretData["AMQ_USER"] = "myuser"
			secretData["AMQ_PASSWORD"] = "mypassword"
			secretData["AMQ_CLUSTER_USER"] = "myclusteruser"
			secretData["AMQ_CLUSTER_PASSWORD"] = "myclusterpassword"

			By("deploy a user credential secret")
			secret, _ := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = userSecretName
				candidate.StringData = secretData
			})

			By("deploy a broker with admin user and password")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Name = brokerCrName
				candidate.Spec.AdminUser = "adminuser"
				candidate.Spec.AdminPassword = "adminpassword"
			})

			By("retrieve the secret and statefulset to check user secret takes effect but not the CR ones")
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			secretKey := types.NamespacedName{Name: secret.Name, Namespace: defaultNamespace}
			secretAfterRecon := &corev1.Secret{}
			Eventually(func(g Gomega) {
				// The user secret's values are not updated from those in CR
				g.Expect(k8sClient.Get(ctx, secretKey, secretAfterRecon)).Should(Succeed())
				g.Expect(string(secretAfterRecon.Data["AMQ_USER"])).To(Equal("myuser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_PASSWORD"])).To(Equal("mypassword"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_USER"])).To(Equal("myclusteruser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_PASSWORD"])).To(Equal("myclusterpassword"))

				if len(secretAfterRecon.OwnerReferences) > 0 {
					for _, oref := range secretAfterRecon.OwnerReferences {
						g.Expect(oref.Kind).NotTo(Equal(artemisGvk.Kind))
					}
				}

				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
				initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					switch envVar.Name {
					case "AMQ_USER":
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"))
					case "AMQ_PASSWORD":
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"))
					case "AMQ_CLUSTER_USER":
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"))
					case "AMQ_CLUSTER_PASSWORD":
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"))
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("update the broker to trigger reconcile")
			brokerKey := types.NamespacedName{
				Name:      brokerCr.Name,
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				createdBrokerCr.Spec.Env = append(createdBrokerCr.Spec.Env, corev1.EnvVar{Name: "NEW_VAR", Value: "NEW_VALUE"})
				g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			newCreatedBrokerCr := brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, &newCreatedBrokerCr)).Should(Succeed())
				g.Expect(len(newCreatedBrokerCr.Spec.Env)).To(Equal(1))
				g.Expect(newCreatedBrokerCr.Spec.Env[0].Name).To(Equal("NEW_VAR"))
				g.Expect(newCreatedBrokerCr.Spec.Env[0].Value).To(Equal("NEW_VALUE"))
			}, timeout, interval).Should(Succeed())

			newSS := &appsv1.StatefulSet{}
			secretAfterRecon = &corev1.Secret{}

			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, secretKey, secretAfterRecon)).Should(Succeed())
				g.Expect(string(secretAfterRecon.Data["AMQ_USER"])).To(Equal("myuser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_PASSWORD"])).To(Equal("mypassword"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_USER"])).To(Equal("myclusteruser"))
				g.Expect(string(secretAfterRecon.Data["AMQ_CLUSTER_PASSWORD"])).To(Equal("myclusterpassword"))

				if len(secretAfterRecon.OwnerReferences) > 0 {
					for _, oref := range secretAfterRecon.OwnerReferences {
						g.Expect(oref.Kind).NotTo(Equal(artemisGvk.Kind))
					}
				}

				g.Expect(k8sClient.Get(ctx, ssKey, newSS)).Should(Succeed())
				initContainer := newSS.Spec.Template.Spec.InitContainers[0]
				userFound := false
				passwordFound := false
				clusterUserFound := false
				clusterPasswordFound := false
				for _, envVar := range initContainer.Env {
					switch envVar.Name {
					case "AMQ_USER":
						userFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_USER"))
					case "AMQ_PASSWORD":
						passwordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_PASSWORD"))
					case "AMQ_CLUSTER_USER":
						clusterUserFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_USER"))
					case "AMQ_CLUSTER_PASSWORD":
						clusterPasswordFound = true
						g.Expect(envVar.ValueFrom.SecretKeyRef.Name).To(Equal(secret.Name))
						g.Expect(envVar.ValueFrom.SecretKeyRef.Key).To(Equal("AMQ_CLUSTER_PASSWORD"))
					}
				}
				g.Expect(userFound).To(BeTrue())
				g.Expect(passwordFound).To(BeTrue())
				g.Expect(clusterUserFound).To(BeTrue())
				g.Expect(clusterPasswordFound).To(BeTrue())

				container := newSS.Spec.Template.Spec.Containers[0]
				newVarFound := false
				for _, envVar := range container.Env {
					if envVar.Name == "NEW_VAR" {
						newVarFound = true
						g.Expect(envVar.Value).To(Equal("NEW_VALUE"))
					}
				}
				g.Expect(newVarFound).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			CleanResource(brokerCr, brokerCr.Name, defaultNamespace)
			CleanResource(secret, secret.Name, defaultNamespace)
		})
	})

	Context("New address settings options", func() {
		It("Deploy broker with new address settings", func() {

			By("By creating a crd with address settings in spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			// add address settings, to an existing crd
			ma := "merge_all"
			dlqabc := "dlqabc"
			maxSize := "10m"
			maxMessages := int64(5000)
			redistributionDelay := int32(5000)
			configDeleteDiverts := "OFF"

			crd.Spec.AddressSettings = brokerv1beta1.AddressSettingsType{
				ApplyRule: &ma,
				AddressSetting: []brokerv1beta1.AddressSettingType{
					{
						Match:               "abc#",
						DeadLetterAddress:   &dlqabc,
						MaxSizeBytes:        &maxSize,
						MaxSizeMessages:     &maxMessages,
						ConfigDeleteDiverts: &configDeleteDiverts,
						RedistributionDelay: &redistributionDelay,
					},
					{
						Match:               "#",
						DeadLetterAddress:   &dlqabc,
						MaxSizeBytes:        &maxSize,
						MaxSizeMessages:     &maxMessages,
						ConfigDeleteDiverts: &configDeleteDiverts,
						RedistributionDelay: &redistributionDelay,
					},
				},
			}
			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())

			By("tracking the yaconfig init command with user_address_settings and verifying new options are in")
			key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
			var initArgsString string
			Eventually(func() bool {
				createdSs := &appsv1.StatefulSet{}

				if k8sClient.Get(ctx, key, createdSs) != nil {
					return false
				}

				initArgsString = strings.Join(createdSs.Spec.Template.Spec.InitContainers[0].Args, ",")
				if !strings.Contains(initArgsString, "max_size_messages: 5000") {
					return false
				}

				value := cr2jinja2.GetUniqueShellSafeSubstution(configDeleteDiverts)
				fullString := "config_delete_diverts: " + value
				return strings.Contains(initArgsString, fullString)

			}, timeout, interval).Should(BeTrue())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("verifying started")
				brokerKey := types.NamespacedName{Name: createdCrd.Name, Namespace: createdCrd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Versions Test", func() {
		It("default image to use latest", func() {
			crd := generateArtemisSpec(defaultNamespace)
			brokerObj, convErr := ConvertArtemisToBrokerCluster(&crd)
			Expect(convErr).To(BeNil())
			imageToUse := common.DetermineImageToUse(brokerObj, "KUBERNETES")
			Expect(imageToUse).To(Equal(version.GetDefaultKubeImage()), "actual", imageToUse)

			imageToUse = common.DetermineImageToUse(brokerObj, "INIT")
			Expect(imageToUse).To(Equal(version.GetDefaultInitImage()), "actual", imageToUse)
			compactVersionToUse, verr := common.DetermineCompactVersionToUse(brokerObj)
			Expect(verr).To(BeNil())
			Expect(compactVersionToUse).To(Equal(version.GetDefaultCompactVersion()), "actual", compactVersionToUse)
		})
	})

	Context("CrVersionConversionTest", func() {
		It("can reconcile different version", func() {

			var float32Var = float32(2.3)
			var ma = "all"
			toCreate := v2alpha5.ActiveMQArtemis{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemis",
					APIVersion: v2alpha5.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
				Spec: v2alpha5.ActiveMQArtemisSpec{
					AddressSettings: v2alpha5.AddressSettingsType{
						ApplyRule: &ma,
						AddressSetting: []v2alpha5.AddressSettingType{
							{
								Match:                              "#",
								RedeliveryDelayMultiplier:          &float32Var,
								RedeliveryCollisionAvoidanceFactor: &float32Var,
							},
						},
					},
					DeploymentPlan: v2alpha5.DeploymentPlanType{
						Size: common.Int32ToPtr(0),
					},
				},
			}

			By("deploying v2lpha5 with float32, ignored")
			Expect(k8sClient.Create(context.TODO(), &toCreate)).Should(Succeed())

			key := types.NamespacedName{Name: namer.CrToSS(toCreate.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("checking status - using original version - exercise in marshalling")
			key = types.NamespacedName{Name: toCreate.Name, Namespace: toCreate.Namespace}
			createdCrdv2alpha5 := &v2alpha5.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdCrdv2alpha5)).Should(Succeed())
				g.Expect(len(createdCrdv2alpha5.Status.PodStatus.Stopped)).Should(BeEquivalentTo(1))
			}, timeout, interval).Should(Succeed())

			By("checking status - using served version - with conditions")
			key = types.NamespacedName{Name: toCreate.Name, Namespace: toCreate.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdCrd)).Should(Succeed())
				g.Expect(len(createdCrd.Status.PodStatus.Stopped)).Should(BeEquivalentTo(1))
				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		It("can reconcile latest version with config in brokerProperties", func() {

			toCreate := brokerv1beta1.ActiveMQArtemis{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemis",
					APIVersion: brokerv1beta1.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
				Spec: brokerv1beta1.ActiveMQArtemisSpec{
					BrokerProperties: []string{
						"addressesSettings.#.redeliveryMultiplier=2.3",
						"addressesSettings.#.redeliveryCollisionAvoidanceFactor=1.2",
					},
					DeploymentPlan: brokerv1beta1.DeploymentPlanType{
						Size: common.Int32ToPtr(0),
					},
				},
			}

			Expect(k8sClient.Create(context.TODO(), &toCreate)).Should(Succeed())

			key := types.NamespacedName{Name: namer.CrToSS(toCreate.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			key = types.NamespacedName{Name: toCreate.Name, Namespace: toCreate.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdCrd)).Should(Succeed())
				g.Expect(len(createdCrd.Status.PodStatus.Stopped)).Should(BeEquivalentTo(1))
				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())

		})

		It("updates owner references when upgrading from v2alpha5 to v1beta1", func() {
			brokerCr := v2alpha5.ActiveMQArtemis{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemis",
					APIVersion: v2alpha5.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
				Spec: v2alpha5.ActiveMQArtemisSpec{
					DeploymentPlan: v2alpha5.DeploymentPlanType{
						Size: common.Int32ToPtr(0),
					},
				},
			}

			By("deploying v2alpha5 CR")
			Expect(k8sClient.Create(context.TODO(), &brokerCr)).Should(Succeed())

			ssKey := types.NamespacedName{Name: namer.CrToSS(brokerCr.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}

			By("updating statefulset owner references to simulate outdated owner references")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())

				g.Expect(createdSs.OwnerReferences[0].APIVersion).Should(Equal(brokerv1beta1.GroupVersion.Identifier()))
				g.Expect(createdSs.OwnerReferences[0].Kind).Should(Equal("ActiveMQArtemis"))
				g.Expect(createdSs.OwnerReferences[0].Name).Should(Equal(brokerCr.Name))

				createdSs.OwnerReferences[0].APIVersion = v2alpha5.GroupVersion.Identifier()

				createdSs.OwnerReferences = append(createdSs.OwnerReferences, metav1.OwnerReference{
					APIVersion: "v1test1", Kind: "TestKind", Name: "test-name", UID: "test-uid"})

				g.Expect(k8sClient.Update(ctx, createdSs)).Should(Succeed())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			propsSecretKey := types.NamespacedName{Name: brokerCr.Name + "-props", Namespace: defaultNamespace}
			createdSecret := &corev1.Secret{}

			By("updating secret owner references to simulate outdated owner references")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, propsSecretKey, createdSecret)).Should(Succeed())

				g.Expect(createdSecret.OwnerReferences[0].APIVersion).Should(Equal(brokerv1beta1.GroupVersion.Identifier()))
				g.Expect(createdSecret.OwnerReferences[0].Kind).Should(Equal("ActiveMQArtemis"))
				g.Expect(createdSecret.OwnerReferences[0].Name).Should(Equal(brokerCr.Name))

				createdSecret.OwnerReferences[0].APIVersion = v2alpha5.GroupVersion.Identifier()

				createdSecret.OwnerReferences = append(createdSecret.OwnerReferences, metav1.OwnerReference{
					APIVersion: "v1test1", Kind: "TestKind", Name: "test-name", UID: "test-uid"})

				g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating CR to v1beta1 version by triggering reconciliation")
			crKey := types.NamespacedName{Name: brokerCr.Name, Namespace: brokerCr.Namespace}
			deployedBrokerCr := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crKey, deployedBrokerCr)).Should(Succeed())

				deployedBrokerCr.Annotations = map[string]string{"test-reconcile": "trigger"}
				g.Expect(k8sClient.Update(ctx, deployedBrokerCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for owner references to be updated to v1beta1 on StatefulSet")
			Eventually(func(g Gomega) {
				updatedSs := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, ssKey, updatedSs)).Should(Succeed())
				g.Expect(len(updatedSs.OwnerReferences)).Should(BeEquivalentTo(2))
				g.Expect(updatedSs.OwnerReferences[0].APIVersion).Should(Equal(brokerv1beta1.GroupVersion.Identifier()))
				g.Expect(updatedSs.OwnerReferences[0].Kind).Should(Equal("ActiveMQArtemis"))
				g.Expect(updatedSs.OwnerReferences[0].Name).Should(Equal(deployedBrokerCr.Name))

				g.Expect(updatedSs.OwnerReferences[1].APIVersion).Should(Equal("v1test1"))
				g.Expect(updatedSs.OwnerReferences[1].Kind).Should(Equal("TestKind"))
				g.Expect(updatedSs.OwnerReferences[1].Name).Should(Equal("test-name"))
				g.Expect(updatedSs.OwnerReferences[1].UID).Should(Equal(types.UID("test-uid")))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for owner references to be updated to v1beta1 on Secret")
			Eventually(func(g Gomega) {
				updatedSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, updatedSecret)).Should(Succeed())
				g.Expect(len(updatedSecret.OwnerReferences)).Should(BeEquivalentTo(2))
				g.Expect(updatedSecret.OwnerReferences[0].APIVersion).Should(Equal(brokerv1beta1.GroupVersion.Identifier()))
				g.Expect(updatedSecret.OwnerReferences[0].Kind).Should(Equal("ActiveMQArtemis"))
				g.Expect(updatedSecret.OwnerReferences[0].Name).Should(Equal(deployedBrokerCr.Name))

				g.Expect(updatedSecret.OwnerReferences[1].APIVersion).Should(Equal("v1test1"))
				g.Expect(updatedSecret.OwnerReferences[1].Kind).Should(Equal("TestKind"))
				g.Expect(updatedSecret.OwnerReferences[1].Name).Should(Equal("test-name"))
				g.Expect(updatedSecret.OwnerReferences[1].UID).Should(Equal(types.UID("test-uid")))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, deployedBrokerCr)).Should(Succeed())
		})
	})

	Context("Console config test", func() {
		It("checking console target service port name for metrics", Label("console-expose-metrics"), func() {
			By("Deploying a broker with console exposed")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: 1,
					PeriodSeconds:       1,
					TimeoutSeconds:      5,
				}
				candidate.Spec.Console.Expose = true
				candidate.Spec.DeploymentPlan.JolokiaAgentEnabled = true

				if !isOpenshift {
					candidate.Spec.IngressDomain = defaultTestIngressDomain
				}
			})

			ssKey := types.NamespacedName{
				Name:      namer.CrToSS(brokerCr.Name),
				Namespace: defaultNamespace,
			}

			By("checking statefulset has service port exposed")
			currentSS := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				g.Expect(len(currentSS.Spec.Template.Spec.Containers[0].Ports)).To(Equal(2))
				jolokiaPortFound, consolePortFound := false, false
				for _, cport := range currentSS.Spec.Template.Spec.Containers[0].Ports {
					if cport.Name == "jolokia" && cport.ContainerPort == int32(8778) && cport.Protocol == corev1.ProtocolTCP {
						jolokiaPortFound = true
					}
					if cport.Name == "wconsj" && cport.ContainerPort == int32(8161) && cport.Protocol == corev1.ProtocolTCP {
						consolePortFound = true
					}
				}
				g.Expect(jolokiaPortFound).To(BeTrue())
				g.Expect(consolePortFound).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("checking services have service ports for metrics exposed")

			for i := 0; i < 2; i++ {
				svcName := brokerCr.Name + "-wconsj-" + strconv.Itoa(i) + "-svc"
				port0Name := "wconsj-" + strconv.Itoa(i)
				port1Name := "wconsj"
				portNumber := int32(8162)
				portNumber2 := int32(8161)
				serviceKey := types.NamespacedName{Name: svcName, Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					retrievedService := corev1.Service{}
					g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
					g.Expect(len(retrievedService.Spec.Ports)).To(Equal(2))
					port0Found, port1Found := false, false
					for _, p := range retrievedService.Spec.Ports {
						if p.Name == port0Name && p.Port == portNumber && p.Protocol == corev1.ProtocolTCP && p.TargetPort == intstr.FromInt(int(portNumber2)) {
							port0Found = true
						}
						if p.Name == port1Name && p.Port == portNumber2 && p.Protocol == corev1.ProtocolTCP && p.TargetPort == intstr.FromInt(int(portNumber2)) {
							port1Found = true
						}
					}
					g.Expect(port0Found).To(BeTrue())
					g.Expect(port1Found).To(BeTrue())
				}, timeout, interval).Should(Succeed())
			}

			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})

		It("Exposing secured console", Label("console-expose-ssl"), func() {
			//we need to use existing cluster to differentiate testing
			//between openshift and k8s, also need it to check pod status
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				crd := generateArtemisSpec(defaultNamespace)
				crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: 1,
					PeriodSeconds:       1,
					TimeoutSeconds:      5,
				}

				crd.Spec.Console.Expose = true
				crd.Spec.Console.SSLEnabled = true
				crd.Spec.IngressDomain = defaultTestIngressDomain

				By("deploying well known secret name that the operator will look for")
				consoleSecretName := crd.Name + "-console-secret"
				consoleSecret, err := CreateTlsSecret(consoleSecretName, defaultNamespace, defaultPassword, defaultSanDnsNames)
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, consoleSecret)).Should(Succeed())

				createdSecret := corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      consoleSecretName,
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				WaitForPod(crd.Name)
				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(crd.Name),
					Namespace: defaultNamespace,
				}
				currentSS := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify pod is up")
				WaitForPod(crd.Name)

				var host string

				if isOpenshift {
					By("check route is created")
					routeKey := types.NamespacedName{
						Name:      crd.Name + "-wconsj-0-svc-rte",
						Namespace: defaultNamespace,
					}
					route := routev1.Route{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())

						host = route.Name + "-" + defaultNamespace + "." + crd.Spec.IngressDomain
						g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("wconsj-0")))
						g.Expect(route.Spec.To.Kind).To(Equal("Service"))
						g.Expect(route.Spec.To.Name).To(Equal(crd.Name + "-wconsj-0-svc"))
						g.Expect(route.Spec.TLS.Termination).To(BeEquivalentTo(routev1.TLSTerminationPassthrough))
						g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(BeEquivalentTo(routev1.InsecureEdgeTerminationPolicyNone))
						g.Expect(route.Spec.Host).To(Equal(host))
					}).Should(Succeed())

				} else {
					By("check ingress is created")
					ingKey := types.NamespacedName{
						Name:      crd.Name + "-wconsj-0-svc-ing",
						Namespace: defaultNamespace,
					}
					ingress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

						host = ingress.Name + "-" + defaultNamespace + "." + crd.Spec.IngressDomain
						g.Expect(ingress.Spec.Rules).To(HaveLen(1))
						g.Expect(ingress.Spec.Rules[0].Host).To(Equal(host))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths).To(HaveLen(1))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(BeEquivalentTo(crd.Name + "-wconsj-0-svc"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(BeEquivalentTo("wconsj-0"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
						g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

						g.Expect(ingress.Spec.TLS).To(HaveLen(1))
						g.Expect(ingress.Spec.TLS[0].Hosts).To(HaveLen(1))
						g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(Equal(host))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				if isOpenshift || isIngressSSLPassthroughEnabled {
					By("check console is reachable")
					httpClient := http.Client{
						Transport: &http.Transport{
							DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
								return (&net.Dialer{}).DialContext(ctx, network, clusterIngressHost+":443")
							},
							TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
						},
						Timeout: timeout,
					}
					Eventually(func(g Gomega) {
						res, err := httpClient.Get("https://" + host + "/console")
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(res.StatusCode).Should(Equal(200))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(consoleSecret, consoleSecret.Name, defaultNamespace)
			}
		})

		It("Exposing secured broker with custom ingress hosts", Label("console-expose-ssl"), func() {
			//we need to use existing cluster to differentiate testing
			//between openshift and k8s, also need it to check pod status
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				crd := generateArtemisSpec(defaultNamespace)

				By("deploying ssl secret")
				sslSecretName := crd.Name + "-ssl-secret"
				sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, defaultPassword, defaultSanDnsNames)
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())

				createdSecret := corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      sslSecretName,
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				specIngressHost := "$(CR_NAME)-$(ITEM_NAME)-$(BROKER_ORDINAL).$(INGRESS_DOMAIN)"

				crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: 1,
					PeriodSeconds:       1,
					TimeoutSeconds:      5,
				}

				crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:        "my-acceptor",
						Port:        61626,
						Expose:      true,
						SSLEnabled:  true,
						SSLSecret:   sslSecretName,
						IngressHost: specIngressHost,
					},
				}

				crd.Spec.Connectors = []brokerv1beta1.ConnectorType{
					{
						Name:        "my-connector",
						Port:        61626,
						Expose:      true,
						SSLEnabled:  true,
						SSLSecret:   sslSecretName,
						IngressHost: specIngressHost,
					},
				}

				crd.Spec.Console = brokerv1beta1.ConsoleType{
					Name:        "my-console",
					Expose:      true,
					SSLEnabled:  true,
					SSLSecret:   sslSecretName,
					IngressHost: specIngressHost,
				}

				crd.Spec.IngressDomain = defaultTestIngressDomain

				By("deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(crd.Name),
					Namespace: defaultNamespace,
				}
				currentSS := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify pod is up")
				WaitForPod(crd.Name)

				if isOpenshift {
					By("check console acceptor is created")
					acceptorHost := crd.Name + "-my-acceptor-0." + crd.Spec.IngressDomain
					acceptorRouteKey := types.NamespacedName{
						Name:      crd.Name + "-my-acceptor-0-svc-rte",
						Namespace: defaultNamespace,
					}
					acceptorRoute := routev1.Route{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, acceptorRouteKey, &acceptorRoute)).To(Succeed())

						g.Expect(acceptorRoute.Spec.Host).To(Equal(acceptorHost))
					}).Should(Succeed())

					By("check console connector is created")
					connectorHost := crd.Name + "-my-connector-0." + crd.Spec.IngressDomain
					connectorRouteKey := types.NamespacedName{
						Name:      crd.Name + "-my-connector-0-svc-rte",
						Namespace: defaultNamespace,
					}
					connectorRoute := routev1.Route{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, connectorRouteKey, &connectorRoute)).To(Succeed())

						g.Expect(connectorRoute.Spec.Host).To(Equal(connectorHost))
					}).Should(Succeed())

					By("check console route is created")
					consoleHost := crd.Name + "-my-console-0." + crd.Spec.IngressDomain
					consoleRouteKey := types.NamespacedName{
						Name:      crd.Name + "-my-console-0-svc-rte",
						Namespace: defaultNamespace,
					}
					consoleRoute := routev1.Route{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, consoleRouteKey, &consoleRoute)).To(Succeed())

						g.Expect(consoleRoute.Spec.Host).To(Equal(consoleHost))
					}).Should(Succeed())
				} else {
					By("check acceptor ingress is created")
					acceptorHost := crd.Name + "-my-acceptor-0." + crd.Spec.IngressDomain
					acceptorIngKey := types.NamespacedName{
						Name:      crd.Name + "-my-acceptor-0-svc-ing",
						Namespace: defaultNamespace,
					}
					acceptorIngress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, acceptorIngKey, &acceptorIngress)).To(Succeed())

						g.Expect(acceptorIngress.Spec.Rules).To(HaveLen(1))
						g.Expect(acceptorIngress.Spec.Rules[0].Host).To(Equal(acceptorHost))

						g.Expect(acceptorIngress.Spec.TLS).To(HaveLen(1))
						g.Expect(acceptorIngress.Spec.TLS[0].Hosts).To(HaveLen(1))
						g.Expect(acceptorIngress.Spec.TLS[0].Hosts[0]).To(Equal(acceptorHost))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("check connector ingress is created")
					connectorHost := crd.Name + "-my-connector-0." + crd.Spec.IngressDomain
					connectorIngKey := types.NamespacedName{
						Name:      crd.Name + "-my-connector-0-svc-ing",
						Namespace: defaultNamespace,
					}
					connectorIngress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, connectorIngKey, &connectorIngress)).To(Succeed())

						g.Expect(connectorIngress.Spec.Rules).To(HaveLen(1))
						g.Expect(connectorIngress.Spec.Rules[0].Host).To(Equal(connectorHost))

						g.Expect(connectorIngress.Spec.TLS).To(HaveLen(1))
						g.Expect(connectorIngress.Spec.TLS[0].Hosts).To(HaveLen(1))
						g.Expect(connectorIngress.Spec.TLS[0].Hosts[0]).To(Equal(connectorHost))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("check console ingress is created")
					consoleHost := crd.Name + "-my-console-0." + crd.Spec.IngressDomain
					consoleIngKey := types.NamespacedName{
						Name:      crd.Name + "-my-console-0-svc-ing",
						Namespace: defaultNamespace,
					}
					consoleIngress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, consoleIngKey, &consoleIngress)).To(Succeed())

						g.Expect(consoleIngress.Spec.Rules).To(HaveLen(1))
						g.Expect(consoleIngress.Spec.Rules[0].Host).To(Equal(consoleHost))

						g.Expect(consoleIngress.Spec.TLS).To(HaveLen(1))
						g.Expect(consoleIngress.Spec.TLS[0].Hosts).To(HaveLen(1))
						g.Expect(consoleIngress.Spec.TLS[0].Hosts[0]).To(Equal(consoleHost))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(sslSecret, sslSecret.Name, defaultNamespace)
			}
		})

		It("Exposing secured console with user specified secret", Label("console-expose-ssl-no-default-secret"), func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				crd := generateArtemisSpec(defaultNamespace)
				crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: 1,
					PeriodSeconds:       1,
					TimeoutSeconds:      5,
				}

				// an eariler operator would not set an owner ref on services and we can clash
				// on create when trying to create a service with the same name..
				// verify that we find and fix the owner ref before reconcile
				var dudPort = int32(22)
				serviceKey := types.NamespacedName{Name: crd.Name + "-wconsj-0-svc", Namespace: defaultNamespace}
				existingSrviceWithOutOwnerRef := corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: serviceKey.Name, Namespace: serviceKey.Namespace},
					Spec: corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: dudPort}},
					},
				}

				Expect(k8sClient.Create(ctx, &existingSrviceWithOutOwnerRef)).Should(Succeed())
				retrievedService := corev1.Service{}
				// ensure it is present before the artemis cr
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
					g.Expect(retrievedService.ResourceVersion).Should(Not(BeEmpty()))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				crd.Spec.Console.Expose = true
				crd.Spec.Console.SSLEnabled = true
				crd.Spec.Console.SSLSecret = "my-secret"

				if !isOpenshift {
					crd.Spec.IngressDomain = defaultTestIngressDomain
				}

				By("deploying user specified secret")
				consoleSecretName := "my-secret"
				consoleSecret, err := CreateTlsSecret(consoleSecretName, defaultNamespace, defaultPassword, defaultSanDnsNames)
				Expect(err).To(BeNil())

				Expect(k8sClient.Create(ctx, consoleSecret)).Should(Succeed())

				createdSecret := corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      consoleSecretName,
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(crd.Name),
					Namespace: defaultNamespace,
				}
				currentSS := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify pod is up")
				WaitForPod(crd.Name)

				if isOpenshift {
					By("check route is created")
					routeKey := types.NamespacedName{
						Name:      crd.Name + "-wconsj-0-svc-rte",
						Namespace: defaultNamespace,
					}
					route := routev1.Route{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
						g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("wconsj-0")))
						g.Expect(route.Spec.To.Kind).To(Equal("Service"))
						g.Expect(route.Spec.To.Name).To(Equal(crd.Name + "-wconsj-0-svc"))
						g.Expect(route.Spec.TLS.Termination).To(BeEquivalentTo(routev1.TLSTerminationPassthrough))
						g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(BeEquivalentTo(routev1.InsecureEdgeTerminationPolicyNone))
					}).Should(Succeed())

				} else {
					By("check ingress is created")
					ingKey := types.NamespacedName{
						Name:      crd.Name + "-wconsj-0-svc-ing",
						Namespace: defaultNamespace,
					}
					ingress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

						g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
						g.Expect(ingress.Spec.Rules[0].Host).To(ContainSubstring(defaultTestIngressDomain))
						g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(BeEquivalentTo(crd.Name + "-wconsj-0-svc"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(BeEquivalentTo("wconsj-0"))
						g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
						g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

						g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
						g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
						g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(ContainSubstring(defaultTestIngressDomain))

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("verify service still exists and is updated")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
					g.Expect(len(retrievedService.GetOwnerReferences())).Should(BeEquivalentTo(1))
					g.Expect(retrievedService.Spec.Ports[0].Port).Should(Not(BeEquivalentTo(dudPort)))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(consoleSecret, consoleSecret.Name, defaultNamespace)

				By("verify service gets owned and deleted")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Not(Succeed()))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		})
	})

	Context("Expose mode test", func() {
		It("expose with ingress mode", Label("console", "acceptor", "connector", "ingress"), func() {
			By("Deploying a broker with console")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Console.Expose = true
				candidate.Spec.Console.ExposeMode = &brokerv1beta1.ExposeModes.Ingress

				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "acceptor",
						Port:       61616,
						Expose:     true,
						ExposeMode: &brokerv1beta1.ExposeModes.Ingress,
					},
				}

				candidate.Spec.Connectors = []brokerv1beta1.ConnectorType{
					{
						Name:       "connector",
						Port:       61616,
						Expose:     true,
						ExposeMode: &brokerv1beta1.ExposeModes.Ingress,
					},
				}

				// force a non-fatal failure for the validation of the broker images
				candidate.Spec.DeploymentPlan.Image = version.GetDefaultKubeImage()
				candidate.Spec.Version = ""
			})

			brokerKey := types.NamespacedName{Name: brokerCr.Name, Namespace: brokerCr.Namespace}
			deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

			By("verify acceptor with invalid ingress settings")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

				validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validation).ShouldNot(BeNil())
				g.Expect(validation.Reason).Should(Equal(brokerv1beta1.ValidConditionFailedInvalidIngressSettings))
				g.Expect(validation.Message).Should(ContainSubstring(".Spec.Acceptors \"acceptor\" has invalid ingress settings, IngressHost unspecified and no Spec.IngressDomain default domain provided"))
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				deployedCrd.Spec.Acceptors[0].IngressHost = "acceptor." + defaultTestIngressDomain
				g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verify connector with invalid ingress settings")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

				validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validation).ShouldNot(BeNil())
				g.Expect(validation.Reason).Should(Equal(brokerv1beta1.ValidConditionFailedInvalidIngressSettings))
				g.Expect(validation.Message).Should(ContainSubstring(".Spec.Connectors \"connector\" has invalid ingress settings, IngressHost unspecified and no Spec.IngressDomain default domain provided"))
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				deployedCrd.Spec.Connectors[0].IngressHost = "connector." + defaultTestIngressDomain
				g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verify console with invalid ingress settings")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

				validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validation).ShouldNot(BeNil())
				g.Expect(validation.Reason).Should(Equal(brokerv1beta1.ValidConditionFailedInvalidIngressSettings))
				g.Expect(validation.Message).Should(ContainSubstring(".Spec.Console has invalid ingress settings, IngressHost unspecified and no Spec.IngressDomain default domain provided"))
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				deployedCrd.Spec.Acceptors[0].IngressHost = ""
				deployedCrd.Spec.Connectors[0].IngressHost = ""
				deployedCrd.Spec.IngressDomain = defaultTestIngressDomain
				g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("check ingress is created for console")
			ingKey := types.NamespacedName{
				Name:      brokerCr.Name + "-wconsj-0-svc-ing",
				Namespace: defaultNamespace,
			}
			ingress := netv1.Ingress{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

				g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
				g.Expect(ingress.Spec.Rules[0].Host).To(ContainSubstring(defaultTestIngressDomain))
				g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(BeEquivalentTo(brokerCr.Name + "-wconsj-0-svc"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(BeEquivalentTo("wconsj-0"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
				g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if isOpenshift || isIngressSSLPassthroughEnabled {
				host := ingress.Name + "-" + defaultNamespace + "." + deployedCrd.Spec.IngressDomain

				By("check console is reachable")
				httpClient := http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
					DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, clusterIngressHost+":80")
					}}}
				Eventually(func(g Gomega) {
					res, err := httpClient.Get("http://" + host + "/console")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(res.StatusCode).Should(Equal(200))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("check ingress is created for acceptor")
			ingKey = types.NamespacedName{
				Name:      brokerCr.Name + "-acceptor-0-svc-ing",
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

				g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
				g.Expect(ingress.Spec.Rules[0].Host).To(Equal(brokerCr.Name + "-acceptor-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))
				g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(brokerCr.Name + "-acceptor-0-svc"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("acceptor-0"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
				g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(Equal(netv1.PathTypePrefix))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("check ingress is created for connector")
			ingKey = types.NamespacedName{
				Name:      brokerCr.Name + "-connector-0-svc-ing",
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

				g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
				g.Expect(ingress.Spec.Rules[0].Host).To(Equal(brokerCr.Name + "-connector-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))
				g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(brokerCr.Name + "-connector-0-svc"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("connector-0"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(Equal("/"))
				g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(Equal(netv1.PathTypePrefix))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})

		It("expose with secure ingress mode", Label("console", "acceptor", "conector", "ingress", "ssl"), func() {
			var sslSecret *corev1.Secret

			By("Deploying a broker with SSL secret")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

				By("deploying ssl secret")
				var sslSecretErr error
				sslSecretName := candidate.Name + "-ssl-secret"
				sslSecret, sslSecretErr = CreateTlsSecret(sslSecretName, defaultNamespace, defaultPassword, defaultSanDnsNames)
				Expect(sslSecretErr).To(BeNil())
				Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())

				candidate.Spec.IngressDomain = defaultTestIngressDomain
				candidate.Spec.Console.Expose = true
				candidate.Spec.Console.ExposeMode = &brokerv1beta1.ExposeModes.Ingress
				candidate.Spec.Console.SSLEnabled = true
				candidate.Spec.Console.SSLSecret = sslSecretName

				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "acceptor",
						Port:       61617,
						Expose:     true,
						ExposeMode: &brokerv1beta1.ExposeModes.Ingress,
						SSLEnabled: true,
						SSLSecret:  sslSecretName,
					},
				}

				candidate.Spec.Connectors = []brokerv1beta1.ConnectorType{
					{
						Name:       "connector",
						Port:       61617,
						Expose:     true,
						ExposeMode: &brokerv1beta1.ExposeModes.Ingress,
						SSLEnabled: true,
						SSLSecret:  sslSecretName,
					},
				}
			})

			By("check ingress is created for console")
			ingKey := types.NamespacedName{
				Name:      brokerCr.Name + "-wconsj-0-svc-ing",
				Namespace: defaultNamespace,
			}
			ingress := netv1.Ingress{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

				g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
				g.Expect(ingress.Spec.Rules[0].Host).To(ContainSubstring(defaultTestIngressDomain))
				g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(BeEquivalentTo(brokerCr.Name + "-wconsj-0-svc"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(BeEquivalentTo("wconsj-0"))

				if isOpenshift {
					g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo(""))
					g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypeImplementationSpecific))
				} else {
					g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
					g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

					g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
					g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
					g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(ContainSubstring(defaultTestIngressDomain))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if isOpenshift || isIngressSSLPassthroughEnabled {
				host := ingress.Name + "-" + defaultNamespace + "." + brokerCr.Spec.IngressDomain

				By("check console is reachable")
				httpClient := http.Client{Timeout: timeout, Transport: &http.Transport{
					DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
						return (&net.Dialer{}).DialContext(ctx, network, clusterIngressHost+":443")
					}, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
				Eventually(func(g Gomega) {
					res, err := httpClient.Get("https://" + host + "/console")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(res.StatusCode).Should(Equal(200))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("check ingress is created for acceptor")
			ingKey = types.NamespacedName{
				Name:      brokerCr.Name + "-acceptor-0-svc-ing",
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

				g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
				g.Expect(ingress.Spec.Rules[0].Host).To(Equal(brokerCr.Name + "-acceptor-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))
				g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(brokerCr.Name + "-acceptor-0-svc"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("acceptor-0"))

				if isOpenshift {
					g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo(""))
					g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypeImplementationSpecific))
				} else {
					g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
					g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

					g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
					g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
					g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(ContainSubstring(defaultTestIngressDomain))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if isOpenshift || isIngressSSLPassthroughEnabled {
				host := ingress.Name + "-" + defaultNamespace + "." + brokerCr.Spec.IngressDomain

				By("check acceptor is reachable")
				Eventually(func(g Gomega) {
					url := "amqps://" + clusterIngressHost + ":443"
					connTLSConfig := amqp.ConnTLSConfig(&tls.Config{ServerName: host, InsecureSkipVerify: true})
					client, err := amqp.Dial(url, amqp.ConnSASLPlain("dummy-user", "dummy-pass"), amqp.ConnTLS(true), connTLSConfig)
					g.Expect(err).Should(BeNil())
					g.Expect(client).ShouldNot(BeNil())
					defer client.Close()
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("check ingress is created for connector")
			ingKey = types.NamespacedName{
				Name:      brokerCr.Name + "-connector-0-svc-ing",
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())

				g.Expect(len(ingress.Spec.Rules)).To(Equal(1))
				g.Expect(ingress.Spec.Rules[0].Host).To(Equal(brokerCr.Name + "-connector-0-svc-ing-" + defaultNamespace + "." + defaultTestIngressDomain))
				g.Expect(len(ingress.Spec.Rules[0].HTTP.Paths)).To(BeEquivalentTo(1))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(Equal(brokerCr.Name + "-connector-0-svc"))
				g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(Equal("connector-0"))

				if isOpenshift {
					g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo(""))
					g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypeImplementationSpecific))
				} else {
					g.Expect(ingress.Spec.Rules[0].HTTP.Paths[0].Path).To(BeEquivalentTo("/"))
					g.Expect(*ingress.Spec.Rules[0].HTTP.Paths[0].PathType).To(BeEquivalentTo(netv1.PathTypePrefix))

					g.Expect(len(ingress.Spec.TLS)).To(BeEquivalentTo(1))
					g.Expect(len(ingress.Spec.TLS[0].Hosts)).To(BeEquivalentTo(1))
					g.Expect(ingress.Spec.TLS[0].Hosts[0]).To(ContainSubstring(defaultTestIngressDomain))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
			CleanResource(sslSecret, sslSecret.Name, defaultNamespace)
		})

		It("expose with route mode", Label("console", "acceptor", "connector", "route"), func() {
			By("Deploying a broker with console")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Console.Expose = true
				candidate.Spec.Console.ExposeMode = &brokerv1beta1.ExposeModes.Route

				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "acceptor",
						Port:       61616,
						Expose:     true,
						ExposeMode: &brokerv1beta1.ExposeModes.Route,
					},
				}

				candidate.Spec.Connectors = []brokerv1beta1.ConnectorType{
					{
						Name:       "connector",
						Port:       61616,
						Expose:     true,
						ExposeMode: &brokerv1beta1.ExposeModes.Route,
					},
				}
			})

			if isOpenshift {
				By("check route is created for console")
				routeKey := types.NamespacedName{
					Name:      brokerCr.Name + "-wconsj-0-svc-rte",
					Namespace: defaultNamespace,
				}
				route := routev1.Route{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
					g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("wconsj-0")))
					g.Expect(route.Spec.To.Kind).To(Equal("Service"))
					g.Expect(route.Spec.To.Name).To(Equal(brokerCr.Name + "-wconsj-0-svc"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking route is created for acceptor")
				routeKey = types.NamespacedName{
					Name:      brokerCr.Name + "-acceptor-0-svc-rte",
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
					g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("acceptor-0")))
					g.Expect(route.Spec.To.Kind).To(Equal("Service"))
					g.Expect(route.Spec.To.Name).To(Equal(brokerCr.Name + "-acceptor-0-svc"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking route is created for connector")
				routeKey = types.NamespacedName{
					Name:      brokerCr.Name + "-connector-0-svc-rte",
					Namespace: defaultNamespace,
				}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())
					g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromString("connector-0")))
					g.Expect(route.Spec.To.Kind).To(Equal("Service"))
					g.Expect(route.Spec.To.Name).To(Equal(brokerCr.Name + "-connector-0-svc"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			} else {
				brokerKey := types.NamespacedName{Name: brokerCr.Name, Namespace: brokerCr.Namespace}
				deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verify invalid acceptor expose mode")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

					validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(validation).ShouldNot(BeNil())
					g.Expect(validation.Reason).Should(Equal(brokerv1beta1.ValidConditionFailedInvalidExposeMode))
					g.Expect(validation.Message).Should(ContainSubstring(".Spec.Acceptors \"acceptor\" has invalid expose mode route, it is only supported on OpenShift"))
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					deployedCrd.Spec.Acceptors[0].ExposeMode = nil
					g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("verify invalid connector expose mode")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

					validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(validation).ShouldNot(BeNil())
					g.Expect(validation.Reason).Should(Equal(brokerv1beta1.ValidConditionFailedInvalidExposeMode))
					g.Expect(validation.Message).Should(ContainSubstring(".Spec.Connectors \"connector\" has invalid expose mode route, it is only supported on OpenShift"))
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					deployedCrd.Spec.Connectors[0].ExposeMode = nil
					g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("verify invalid console expose mode")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

					validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(validation).ShouldNot(BeNil())
					g.Expect(validation.Reason).Should(Equal(brokerv1beta1.ValidConditionFailedInvalidExposeMode))
					g.Expect(validation.Message).Should(ContainSubstring(".Spec.Console has invalid expose mode route, it is only supported on OpenShift"))
				}, timeout, interval).Should(Succeed())
			}

			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})
	})

	It("ssl console secret validation", func() {

		crd := generateArtemisSpec(defaultNamespace)
		crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       1,
			TimeoutSeconds:      5,
		}

		crd.Spec.Console.Expose = true
		crd.Spec.Console.SSLEnabled = true

		if !isOpenshift {
			crd.Spec.IngressDomain = defaultTestIngressDomain
		}

		By("Deploying broker" + crd.Name)
		Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

		brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
		deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

		By("verify invalid status on no console secret")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

			Validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
			g.Expect(Validation).ShouldNot(BeNil())
			g.Expect(Validation.Message).Should(ContainSubstring("is not found"))
		}, timeout, interval).Should(Succeed())

		By("deploy secret - empty")
		var consoleSecret corev1.Secret

		consoleSecretName := crd.Name + "-console-secret"
		consoleSecret = corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      consoleSecretName,
				Namespace: defaultNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, &consoleSecret)).Should(Succeed())

		createdSecret := corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      consoleSecretName,
			Namespace: defaultNamespace,
		}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())
		}, timeout, interval).Should(Succeed())

		By("verify different invalid status, auto retry on not found for external dep")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

			Validation := meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
			g.Expect(Validation).ToNot(BeNil())
			g.Expect(Validation.Message).Should(ContainSubstring("must have key"))
		}, timeout, interval).Should(Succeed())

		By("update secret with valid keys")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, secretKey, &createdSecret)).To(Succeed())

			// only presence of key is validated
			createdSecret.Data = map[string][]byte{
				"broker.ks":      nil,
				"trustStorePath": nil,
			}
			createdSecret.StringData = map[string]string{
				"keyStorePassword":   "bla",
				"trustStorePassword": "bla",
			}
			g.Expect(k8sClient.Update(ctx, &createdSecret)).To(Succeed())

		}, timeout, interval).Should(Succeed())

		By("verify valid status")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())

			g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, &consoleSecret)).Should(Succeed())
	})

	Context("Image update test", func() {

		It("deploy ImagePullBackOff update delete ok", func() {

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       1,
				TimeoutSeconds:      5,
			}

			By("Deploying Cr to find valid image " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

			ssKey := types.NamespacedName{
				Name:      namer.CrToSS(crd.Name),
				Namespace: defaultNamespace,
			}
			currentSS := &appsv1.StatefulSet{}
			var ssVersion string
			var imageUrl string
			var initImageUrl string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				ssVersion = currentSS.ResourceVersion
				g.Expect(ssVersion).ShouldNot(BeEmpty())
				imageUrl = currentSS.Spec.Template.Spec.Containers[0].Image
				g.Expect(imageUrl).ShouldNot(BeEmpty())
				initImageUrl = currentSS.Spec.Template.Spec.InitContainers[0].Image
				g.Expect(initImageUrl).ShouldNot(BeEmpty())
			}, timeout, interval).Should(Succeed())

			// update CR with dud image, keeping it lower case to avoid InvalidImageName
			// "couldn't parse image reference "quay.io/Blacloud/activemq-Bla-broker-kubernetes:1.0.7": invalid reference format: repository name must be lowercase"
			dudImage := strings.ReplaceAll(imageUrl, "broker", "bla")
			By("Replacing image " + imageUrl + ", with dud: " + dudImage)
			Expect(dudImage).ShouldNot(Equal(imageUrl))

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
				deployedCrd.Spec.DeploymentPlan.Image = dudImage
				deployedCrd.Spec.DeploymentPlan.InitImage = initImageUrl // we validate both are set when one is set
				g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("verify dud image in ss")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
					g.Expect(currentSS.Spec.Template.Spec.Containers[0].Image).Should(Equal(dudImage))
				}, timeout, interval).Should(Succeed())

				By("verify Waiting error status of pod")
				podKey := types.NamespacedName{Name: namer.CrToSS(crd.Name) + "-0", Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())
					By("verify error status of pod")
					g.Expect(len(pod.Status.ContainerStatuses)).Should(Equal(1))
					g.Expect(pod.Status.ContainerStatuses[0].State.Waiting).Should(Not(BeNil()))
					g.Expect(pod.Status.ContainerStatuses[0].State.Waiting.Reason).Should(ContainSubstring("ImagePullBackOff"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Replacing dud image " + dudImage + ", with original: " + imageUrl)

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					deployedCrd.Spec.DeploymentPlan.Image = imageUrl
					g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
					By("updating cr with correct image: " + imageUrl)
				}, timeout, interval).Should(Succeed())

				By("verify good image in ss")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
					g.Expect(currentSS.Spec.Template.Spec.Containers[0].Image).Should(Equal(imageUrl))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				// we must force a rollback of the old ss rollout as it won't complete
				// due to the failure to become ready
				// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#forced-rollback

				By("verify no roll out yet")
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())

					By("verify error status of pod still pending")
					g.Expect(len(pod.Status.ContainerStatuses)).Should(Equal(1))
					g.Expect(pod.Status.ContainerStatuses[0].State.Waiting).Should(Not(BeNil()))
					g.Expect(pod.Status.ContainerStatuses[0].State.Waiting.Reason).Should(ContainSubstring("ImagePullBackOff"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("deleting pending pod")

				Eventually(func(g Gomega) {
					By("deleting existing pod that is stuck on rollout..")
					pod := &corev1.Pod{}
					zeroGracePeriodSeconds := int64(0) // immediate delete
					g.Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())
					By("Deleting pod: " + podKey.Name)
					g.Expect(k8sClient.Delete(ctx, pod, &client.DeleteOptions{GracePeriodSeconds: &zeroGracePeriodSeconds})).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify ready status of pod with correct image")
				WaitForPod(crd.Name)
			}

			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})
	})

	Context("ClientId autoshard Test", func() {

		produceMessage := func(serverName string, clientId string, linkAddress string, messageId int, g Gomega) {

			url := "amqps://" + clusterIngressHost + ":443"
			connTLSConfig := amqp.ConnTLSConfig(&tls.Config{ServerName: serverName, InsecureSkipVerify: true})
			client, err := amqp.Dial(url, amqp.ConnContainerID(clientId), amqp.ConnSASLPlain("dummy-user", "dummy-pass"), amqp.ConnTLS(true), connTLSConfig)
			g.Expect(err).Should(BeNil())
			g.Expect(client).ShouldNot(BeNil())
			defer client.Close()

			session, err := client.NewSession()

			g.Expect(err).Should(BeNil())
			g.Expect(session).ShouldNot(BeNil())

			sender, err := session.NewSender(
				amqp.LinkTargetAddress(linkAddress),
			)
			g.Expect(err).Should(BeNil())
			ctx, cancel := context.WithTimeout(ctx, 4*time.Second)

			defer sender.Close(ctx)
			defer cancel()

			msg := amqp.NewMessage([]byte("Hello! from:" + clientId))
			msg.ApplicationProperties = make(map[string]interface{})
			msg.ApplicationProperties["MessageID"] = messageId
			msg.ApplicationProperties["ClientID"] = clientId

			err = sender.Send(ctx, msg)
			g.Expect(err).Should(BeNil())
		}

		consumeMatchingMessage := func(serverName string, linkAddress string, receivedTracker map[string]*list.List, g Gomega) {

			url := "amqps://" + clusterIngressHost + ":443"
			connTLSConfig := amqp.ConnTLSConfig(&tls.Config{ServerName: serverName, InsecureSkipVerify: true})
			client, err := amqp.Dial(url, amqp.ConnContainerID("$.artemis.internal.router.client.dummy"), amqp.ConnSASLPlain("dummy-user", "dummy-pass"), amqp.ConnTLS(true), connTLSConfig)
			g.Expect(err).Should(BeNil())
			g.Expect(client).ShouldNot(BeNil())

			defer client.Close()

			session, err := client.NewSession()
			g.Expect(err).Should(BeNil())
			g.Expect(session).ShouldNot(BeNil())

			receiver, err := session.NewReceiver(
				amqp.LinkSourceAddress(linkAddress),
				amqp.LinkSourceCapabilities("queue"),
				amqp.LinkCredit(50),
			)
			g.Expect(err).Should(BeNil())
			g.Expect(receiver).ShouldNot(BeNil())

			ctx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
			defer receiver.Close(ctx)
			defer cancel()

			// Receive messages till error or nil
			for {
				msg, err := receiver.Receive(ctx)

				if err != nil || msg == nil {
					break
				}

				g.Expect(err).Should(BeNil())
				g.Expect(msg).ShouldNot(BeNil())

				var senderClientId = msg.ApplicationProperties["ClientID"].(string)
				receivedTracker[senderClientId].PushBack(msg.ApplicationProperties["MessageID"])

				err = receiver.AcceptMessage(ctx, msg)
				g.Expect(err).Should(BeNil())

			}
		}

		It("deploy 2 with clientID auto sharding", func() {

			isClusteredBoolean := false
			NOT := false
			crd := generateArtemisSpec(defaultNamespace)

			By("deploying ssl secret")
			sslSecretName := crd.Name + "-ssl-secret"
			sslSecret, sslSecretErr := CreateTlsSecret(sslSecretName, defaultNamespace, defaultPassword, defaultSanDnsNames)
			Expect(sslSecretErr).To(BeNil())
			Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())

			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       5,
				TimeoutSeconds:      5,
			}
			crd.Spec.DeploymentPlan.LivenessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       5,
				TimeoutSeconds:      5,
			}

			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			crd.Spec.DeploymentPlan.Clustered = &isClusteredBoolean
			crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{{
				Name:                "tcp",
				Port:                62616,
				Expose:              true,
				IngressHost:         "$(CR_NAME)-$(BROKER_ORDINAL)-tcp." + defaultTestIngressDomain,
				SSLEnabled:          true,
				SSLSecret:           sslSecretName,
				BindToAllInterfaces: &NOT,
			}}

			linkAddress := "LB.TESTQ"

			crd.Spec.BrokerProperties = []string{
				"connectionRouters.autoShard.keyType=CLIENT_ID",
				"connectionRouters.autoShard.localTargetFilter=NULL|${STATEFUL_SET_ORDINAL}|-${STATEFUL_SET_ORDINAL}",
				"connectionRouters.autoShard.policyConfiguration=CONSISTENT_HASH_MODULO",
				"connectionRouters.autoShard.policyConfiguration.properties.MODULO=2",
				"acceptorConfigurations.tcp.params.router=autoShard",           // matching spec.acceptor
				"addressesSettings.\"LB.#\".defaultAddressRoutingType=ANYCAST", // b/c cannot set linkTargetCapabilities from amqp client
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				crdNsName := types.NamespacedName{
					Name:      crd.Name,
					Namespace: defaultNamespace,
				}

				deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, crdNsName, deployedCrd)).Should(Succeed())
					g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				var ingressHosts [2]string
				for i := 0; i < len(ingressHosts); i++ {
					ingressHosts[i] = fmt.Sprintf("%s-%d-tcp.%s", crd.Name, i, defaultTestIngressDomain)
				}

				By("verify partition by sending eventually... expect auto-shard error if we get the wrong broker")
				numberOfMessagesToSendPerClientId := 5
				sentMessageSequenceId := 0
				urlBalancerCounter := 1 // round robin over urls if len(urls) > 0
				clientIds := []string{"W", "ONE", "TWO", "THREE", "FOUR", "0299s-99", "D-0301-c-e-SomeHostBla"}
				for _, id := range clientIds {
					// send messages on new connection with each clientId
					for i := 0; i < numberOfMessagesToSendPerClientId; i++ {
						Eventually(func(g Gomega) {
							urlBalancerCounter++
							produceMessage(ingressHosts[urlBalancerCounter%len(ingressHosts)], id, linkAddress, sentMessageSequenceId+1, g)
							sentMessageSequenceId++ // on success
						}, timeout*4, interval).Should(Succeed())
					}
				}

				Expect(len(clientIds) * numberOfMessagesToSendPerClientId).Should(BeEquivalentTo(sentMessageSequenceId))

				By("verify partition by consuming messages from each broker")
				receivedIdTracker := map[string]*list.List{}
				for _, id := range clientIds {
					receivedIdTracker[id] = list.New()
				}
				for _, url := range ingressHosts {
					Eventually(func(g Gomega) {
						consumeMatchingMessage(url, linkAddress, receivedIdTracker, g)
					}).Should(Succeed())
				}

				for _, list := range receivedIdTracker {
					Expect(list.Len()).Should(BeEquivalentTo(5))

					By("verifying received in order per clientId, ie: producer was sharded nicely")
					receivedIdTracker := list.Front().Value.(int64)
					for e := list.Front(); e != nil; e = e.Next() {
						if e.Value == receivedIdTracker {
							receivedIdTracker++
						}
					}

					By("verifying all in order")
					Expect(receivedIdTracker - list.Front().Value.(int64)).Should(BeEquivalentTo(numberOfMessagesToSendPerClientId))
				}

				Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, sslSecret)).Should(Succeed())
			}

		})
	})

	Context("Probe defaults reconcile", func() {
		It("deploy", func() {
			boolTrueVal := true
			boolFalseVal := false

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.MessageMigration = &boolTrueVal

			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       1,
				TimeoutSeconds:      5,
				// default values are server-side applied - the ones that we set that are > 0 get tracked as owned by us
				// omitted values have default nil,false,0
				// SuccessThreshod defaults to 1 and is applied on write, our 0 default is ignored
			}

			By("Deploying Cr with Probe and defaults " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("verifying started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, timeout*5, interval).Should(Succeed())

				key := types.NamespacedName{
					Name:      namer.CrToSS(crd.Name),
					Namespace: defaultNamespace,
				}
				currentSS := &appsv1.StatefulSet{}
				var ssVersion string
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, key, currentSS)).Should(Succeed())
					ssVersion = currentSS.ResourceVersion
					g.Expect(ssVersion).ShouldNot(BeEmpty())

					By("verifying default filled in and respected")
					g.Expect(currentSS.Spec.Template.Spec.Containers[0].ReadinessProbe.SuccessThreshold).Should(BeEquivalentTo(1))
					g.Expect(currentSS.Spec.Template.Spec.Containers[0].ReadinessProbe.FailureThreshold).Should(BeEquivalentTo(3))

					By("verifying what was configured")
					g.Expect(currentSS.Spec.Template.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds).Should(BeEquivalentTo(1))
				}, timeout, interval).Should(Succeed())

				crdVer := deployedCrd.ResourceVersion
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					crdVer = deployedCrd.ResourceVersion
					deployedCrd.Spec.DeploymentPlan.MessageMigration = &boolFalseVal
					By("force reconcile via CR update of Ver:" + crdVer)
					g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("verify no change in ssVersion but change in brokerCr")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					By("verify crd new revision: " + deployedCrd.ResourceVersion)
					g.Expect(deployedCrd.ResourceVersion).ShouldNot(Equal(crdVer))

					By("Verify second generation...")
					g.Expect(deployedCrd.Generation).Should(BeEquivalentTo(2))

					g.Expect(k8sClient.Get(ctx, key, currentSS)).Should(Succeed())
					By("verify no new ss revision: " + currentSS.ResourceVersion)
					g.Expect(currentSS.ResourceVersion).Should(Equal(ssVersion))
					By("Verify first generation...")
					g.Expect(currentSS.Generation).Should(BeEquivalentTo(1))

				}, timeout, interval).Should(Succeed())

				By("Force SS update via CR update to Probe")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					deployedCrd.Spec.DeploymentPlan.ReadinessProbe.InitialDelaySeconds = 2
					g.Expect(k8sClient.Update(ctx, deployedCrd)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("verifying update to SS")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, key, currentSS)).Should(Succeed())
					By("verify no new ss revision: " + currentSS.ResourceVersion)
					g.Expect(currentSS.ResourceVersion).ShouldNot(Equal(ssVersion))
					By("Verify second generation...")
					g.Expect(currentSS.Generation).Should(BeEquivalentTo(2))

				}, timeout*2, interval).Should(Succeed())

				By("Deleting the ready ss pod")
				key = types.NamespacedName{Name: namer.CrToSS(crd.Name) + "-0", Namespace: defaultNamespace}
				zeroGracePeriodSeconds := int64(0) // immediate delete
				var podResourceVersion string
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, key, pod)).Should(Succeed())
					podResourceVersion = pod.ResourceVersion
					g.Expect(podResourceVersion).ShouldNot(BeEmpty())
					g.Expect(k8sClient.Delete(ctx, pod, &client.DeleteOptions{GracePeriodSeconds: &zeroGracePeriodSeconds})).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("Again finding pod instance with new resource version")
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(k8sClient.Get(ctx, key, pod)).Should(Succeed())
					g.Expect(pod.ResourceVersion).ShouldNot(Equal(podResourceVersion))
				}, timeout, interval).Should(Succeed())

				By("Verying new pod restarted via CR Status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, timeout*5, interval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})

		It("verify mimimal updates via unequal", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				crd := generateArtemisSpec(defaultNamespace)
				crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
					InitialDelaySeconds: 1,
					PeriodSeconds:       1,
					TimeoutSeconds:      5,
					// default values are server-side applied - the ones that we set that are > 0 get tracked as owned by us
					// omitted values have default nil,false,0
					// SuccessThreshod defaults to 1 and is applied on write, our 0 default is ignored

					// on reconcile, we must only reapply what is provided here
				}

				By("start to capture test log")
				StartCapturingLog()
				defer StopCapturingLog()

				By("Deploying Cr with Probe and defaults " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				deployedCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, deployedCrd)).Should(Succeed())
					g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				unequalEntries, _ := FindAllInCapturingLog("unequal")
				Expect(len(unequalEntries)).Should(BeNumerically("==", 0))

				Expect(k8sClient.Delete(ctx, deployedCrd)).Should(Succeed())
			}
		})

	})

	Context("SS delete recreate Test", func() {
		It("deploy, delete ss, verify", func() {

			crd := generateArtemisSpec(defaultNamespace)
			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			key := types.NamespacedName{
				Name:      namer.CrToSS(crd.Name),
				Namespace: defaultNamespace,
			}

			currentSS := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, currentSS)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			brokerKey := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: defaultNamespace}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("manually deleting SS quickly")
			ssVersion := currentSS.ResourceVersion
			Expect(k8sClient.Delete(ctx, currentSS)).Should(Succeed())

			By("checking new version created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, currentSS)).Should(Succeed())
				g.Expect(currentSS.ResourceVersion).ShouldNot(Equal(ssVersion))
				ssVersion = currentSS.ResourceVersion
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("verifying started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("manually deleting SS again once ready")
				Expect(k8sClient.Delete(ctx, currentSS)).Should(Succeed())

				By("checking new version created again")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, key, currentSS)).Should(Succeed())
					g.Expect(currentSS.ResourceVersion).ShouldNot(Equal(ssVersion))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})
	})

	Context("PVC no gc test", func() {
		It("deploy, verify, undeploy, verify, redeploy, verify", func() {

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.PersistenceEnabled = true

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: defaultNamespace}

				By("verifing started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				podName := namer.CrToSS(crd.Name) + "-0"

				By("sending a message to 1")
				Eventually(func(g Gomega) {

					sendCmd := []string{"amq-broker/bin/artemis", "producer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podName + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
					content, err := RunCommandInPod(podName, crd.Name+"-container", sendCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("Produced: 1 messages"))

				}, timeout, interval).Should(Succeed())

				By("finding PVC")
				pvcKey := types.NamespacedName{Namespace: defaultNamespace, Name: crd.Name + "-" + namer.CrToSS(crd.Name) + "-0"}
				pvc := &corev1.PersistentVolumeClaim{}
				Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				// at some stage, there should/could be an owner or SS controller does this gc management
				Expect(len(pvc.OwnerReferences)).Should(BeEquivalentTo(0))

				By("undeploying CR")
				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())

				Eventually(func(g Gomega) {
					By("again finding PVC b/c it has not been gc'ed - " + pvcKey.Name)
					g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("retaining the PV " + crd.ObjectMeta.Name)
				Eventually(func(g Gomega) {
					pvKey := types.NamespacedName{Namespace: defaultNamespace, Name: pvc.Spec.VolumeName}
					pv := &corev1.PersistentVolume{}

					g.Expect(k8sClient.Get(ctx, pvKey, pv)).Should(Succeed())

					pv.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimRetain
					Expect(k8sClient.Update(ctx, pv)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("deleting the PVC " + crd.ObjectMeta.Name)
				Expect(k8sClient.Delete(ctx, pvc)).Should(Succeed())

				By("updating the PV " + crd.ObjectMeta.Name)
				Eventually(func(g Gomega) {
					pvKey := types.NamespacedName{Namespace: defaultNamespace, Name: pvc.Spec.VolumeName}
					pv := &corev1.PersistentVolume{}

					g.Expect(k8sClient.Get(ctx, pvKey, pv)).Should(Succeed())

					pv.Spec.ClaimRef.Name = crd.Name + "c-" + namer.CrToSS(crd.Name+"c") + "-0"
					pv.Spec.ClaimRef.UID = ""
					pv.Spec.ClaimRef.ResourceVersion = ""
					g.Expect(k8sClient.Update(ctx, pv)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("Redeploying the CRD " + crd.ObjectMeta.Name)
				newCrd := crd
				newCrd.ResourceVersion = ""
				newCrd.Name = crd.Name + "c"
				Expect(k8sClient.Create(ctx, &newCrd)).Should(Succeed())

				newCreatedCrd := &brokerv1beta1.ActiveMQArtemis{}
				newBrokerKey := types.NamespacedName{Name: newCrd.Name, Namespace: defaultNamespace}

				By("verifing started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, newBrokerKey, newCreatedCrd)).Should(Succeed())
					g.Expect(len(newCreatedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(newCreatedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(newCreatedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				newPodName := namer.CrToSS(newCrd.Name) + "-0"

				By("Receiving a message from 0")
				Eventually(func(g Gomega) {

					rcvCmd := []string{"amq-broker/bin/artemis", "consumer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + newPodName + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--receive-timeout", "10000", "--break-on-null", "--verbose"}
					content, err := RunCommandInPod(newPodName, newCrd.Name+"-container", rcvCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*content).Should(ContainSubstring("JMS Message ID:"))

				}, timeout, interval).Should(Succeed())
			}
		})
	})

	Context("PVC upgrade owner reference test", Label("pvc-owner-reference-upgrade"), func() {
		It("faking a broker deployment with owned pvc", func() {
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.PersistenceEnabled = true

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying the CRD " + crd.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: defaultNamespace}

				By("verifing started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("finding PVC")
				pvcKey := types.NamespacedName{Namespace: defaultNamespace, Name: crd.Name + "-" + namer.CrToSS(crd.Name) + "-0"}
				pvc := &corev1.PersistentVolumeClaim{}
				Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				Expect(len(pvc.OwnerReferences)).Should(BeEquivalentTo(0))

				// added back owner reference
				pvc.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: brokerv1beta1.GroupVersion.String(),
					Kind:       "ActiveMQArtemis",
					Name:       createdCrd.Name,
					UID:        createdCrd.GetUID()}}
				Expect(k8sClient.Update(ctx, pvc)).Should(Succeed())

				Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				Expect(len(pvc.OwnerReferences)).To(BeEquivalentTo(1))

				shutdownControllerManager()

				Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				Expect(len(pvc.OwnerReferences)).To(BeEquivalentTo(1))

				createControllerManager(true, defaultNamespace)

				// Expect the owner reference gets removed
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
					g.Expect(len(pvc.OwnerReferences)).Should(BeEquivalentTo(0))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("undeploying CR")
				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())

				Eventually(func(g Gomega) {
					By("again finding PVC should succeed - " + pvcKey.Name)
					g.Expect(k8sClient.Get(ctx, pvcKey, &corev1.PersistentVolumeClaim{})).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		})
	})

	Context("Tolerations Existing Cluster", func() {
		It("Toleration of artemis", func() {

			By("Creating a crd with tolerations")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       5,
			}
			crd.Spec.DeploymentPlan.Tolerations = []corev1.Toleration{
				{
					Key:    "artemis",
					Effect: corev1.TaintEffectPreferNoSchedule,
				},
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			// some required services on crc get evicted which invalidates this test of taints
			if !isOpenshift && os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("veryify pod started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, timeout*5, interval).Should(Succeed())

				By("veryify no taints on node")
				Eventually(func(g Gomega) {

					// find our node, take the first one...
					nodes := &corev1.NodeList{}
					g.Expect(k8sClient.List(ctx, nodes, &client.ListOptions{})).Should(Succeed())
					g.Expect(len(nodes.Items) > 0).Should(BeTrue())

					node := nodes.Items[0]
					g.Expect(len(node.Spec.Taints)).Should(BeEquivalentTo(0))
				}, timeout*2, interval).Should(Succeed())

				By("veryify pod status still started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				}, timeout*2, interval).Should(Succeed())

				By("applying taints to node")
				Eventually(func(g Gomega) {

					// find our node, take the first one...
					nodes := &corev1.NodeList{}
					g.Expect(k8sClient.List(ctx, nodes, &client.ListOptions{})).Should(Succeed())
					g.Expect(len(nodes.Items) > 0).Should(BeTrue())

					node := nodes.Items[0]
					g.Expect(len(node.Spec.Taints)).Should(BeEquivalentTo(0))
					node.Spec.Taints = []corev1.Taint{{Key: "artemis", Effect: corev1.TaintEffectPreferNoSchedule}}

					g.Expect(k8sClient.Update(ctx, &node)).Should(Succeed())
				}, timeout*2, interval).Should(Succeed())

				By("veryify pod status still started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, timeout*2, interval).Should(Succeed())

				By("reverting taints on node")
				Eventually(func(g Gomega) {

					// find our node, take the first one...
					nodes := &corev1.NodeList{}
					g.Expect(k8sClient.List(ctx, nodes, &client.ListOptions{})).Should(Succeed())
					g.Expect(len(nodes.Items) > 0).Should(BeTrue())

					node := nodes.Items[0]
					g.Expect(len(node.Spec.Taints)).Should(BeEquivalentTo(1))
					node.Spec.Taints = []corev1.Taint{}
					g.Expect(k8sClient.Update(ctx, &node)).Should(Succeed())
				}, timeout*2, interval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, createdCrd))

		})

		It("Toleration of artemis required add/remove verify status", func() {

			By("Creating a crd with tolerations")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 2,
				PeriodSeconds:       5,
			}

			crd.Spec.DeploymentPlan.Tolerations = []corev1.Toleration{
				{
					Key:      "artemis",
					Value:    "ok",
					Operator: corev1.TolerationOpEqual,
					Effect:   corev1.TaintEffectNoExecute,
				},
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			// some required services get evicted which invalidates this test of taints
			if false && os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("veryify pod started as no taints in play")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, timeout*5, interval).Should(Succeed())

				By("apply matching taint with wrong value, force eviction")
				Eventually(func(g Gomega) {

					// find our node, take the first one...
					nodes := &corev1.NodeList{}
					g.Expect(k8sClient.List(ctx, nodes, &client.ListOptions{})).Should(Succeed())
					g.Expect(len(nodes.Items) > 0).Should(BeTrue())

					node := nodes.Items[0]
					g.Expect(len(node.Spec.Taints)).Should(BeEquivalentTo(0))

					node.Spec.Taints = []corev1.Taint{{Key: "artemis", Value: "no", Effect: corev1.TaintEffectNoExecute}}
					g.Expect(k8sClient.Update(ctx, &node)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("veryify pod status now starting, evicted!")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Starting)).Should(BeEquivalentTo(1))
					g.Expect(common.IsConditionPresentAndEqualIgnoringMessage(createdCrd.Status.Conditions, metav1.Condition{
						Type:   brokerv1beta1.DeployedConditionType,
						Status: metav1.ConditionFalse,
						Reason: brokerv1beta1.DeployedConditionNotReadyReason,
					})).Should(BeTrue())
					g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("updating taint to match key and value")
				Eventually(func(g Gomega) {

					// find our node, take the first one...
					nodes := &corev1.NodeList{}
					g.Expect(k8sClient.List(ctx, nodes, &client.ListOptions{})).Should(Succeed())
					g.Expect(len(nodes.Items) > 0).Should(BeTrue())

					node := nodes.Items[0]
					g.Expect(len(node.Spec.Taints)).Should(BeEquivalentTo(1))
					node.Spec.Taints = []corev1.Taint{{Key: "artemis", Value: "ok", Effect: corev1.TaintEffectNoExecute}}
					g.Expect(k8sClient.Update(ctx, &node)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("veryify ready status on CR, started again")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("reverting taints on node")
				Eventually(func(g Gomega) {

					// find our node, take the first one...
					nodes := &corev1.NodeList{}
					g.Expect(k8sClient.List(ctx, nodes, &client.ListOptions{})).Should(Succeed())
					g.Expect(len(nodes.Items) > 0).Should(BeTrue())

					node := nodes.Items[0]
					g.Expect(len(node.Spec.Taints)).Should(BeEquivalentTo(1))
					node.Spec.Taints = []corev1.Taint{}
					g.Expect(k8sClient.Update(ctx, &node)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, &crd))

		})

	})

	Context("Console secret Test", func() {

		It("deploy broker with ssl enabled console", func() {

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.Console.Expose = true
			crd.Spec.Console.SSLEnabled = true

			brokerCR, convertErr := ConvertArtemisToBrokerCluster(&crd)
			Expect(convertErr).To(BeNil())

			outer := NewBrokerClusterReconciler(k8Manager, ctrl.Log, isOpenshift)
			reconcilerImpl := NewBrokerClusterReconcilerImpl(brokerCR, outer)

			defaultConsoleSecretName := crd.Name + "-console-secret"
			tlsSecret, err := CreateTlsSecret(defaultConsoleSecretName, defaultNamespace, "password", nil)
			Expect(err).To(BeNil())
			Expect(k8sClient.Create(ctx, tlsSecret)).To(Succeed())

			By("veryify secret exists")
			createdTlsSecret := &corev1.Secret{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: tlsSecret.Name, Namespace: tlsSecret.Namespace}, createdTlsSecret)).Should(Succeed())
				g.Expect(createdTlsSecret.ResourceVersion).ShouldNot(BeEmpty())
			}, timeout*2, interval).Should(Succeed())

			currentSS := &appsv1.StatefulSet{}
			currentSS.Name = namer.CrToSS(crd.Name)
			currentSS.Namespace = defaultNamespace
			currentSS.Spec.Template.Spec.InitContainers = []corev1.Container{{
				Name: "main-container",
			}}
			currentSS.Spec.Template.Spec.Containers = []corev1.Container{{
				Name: "init-container",
			}}

			namers := MakeNamers(brokerCR)
			Expect(reconcilerImpl.ProcessConsole(brokerCR, *namers, brokerReconciler.Client, brokerReconciler.Scheme, currentSS)).Should(Succeed())

			secretName := namers.SecretsConsoleNameBuilder.Name()
			internalSecretName := secretName + "-internal"
			consoleArgs := "AMQ_CONSOLE_ARGS"

			foundSecret := false
			foundInternalSecret := false
			defaultSecretPath := "/etc/" + defaultConsoleSecretName + "-volume/"
			defaultSslArgs := " --ssl-key " + defaultSecretPath + "broker.ks --ssl-key-password password --ssl-trust " + defaultSecretPath + "client.ts --ssl-trust-password password"
			for _, reqres := range common.ToResourceList(reconcilerImpl.requestedResources) {
				if reqres.GetObjectKind().GroupVersionKind().Kind == "Secret" {
					secret := reqres.(*corev1.Secret)
					if secret.Name == secretName {
						foundSecret = true
					}
					if secret.Name == internalSecretName {
						foundInternalSecret = true
						consoleSslValue := secret.Data[consoleArgs]
						Expect(consoleSslValue).To(Equal([]byte(defaultSslArgs)))
					}
				}
			}
			Expect(foundSecret).To(BeFalse())
			Expect(foundInternalSecret).To(BeTrue())

			foundSecretRef := false
			foundSecretKey := false
			for _, evar := range currentSS.Spec.Template.Spec.InitContainers[0].Env {
				if evar.Name == consoleArgs {
					if evar.ValueFrom.SecretKeyRef.Name == internalSecretName {
						foundSecretRef = true
					}
					if evar.ValueFrom.SecretKeyRef.Key == consoleArgs {
						foundSecretKey = true
					}
				}
			}
			Expect(foundSecretRef).To(BeTrue())
			Expect(foundSecretKey).To(BeTrue())
			CleanResource(tlsSecret, tlsSecret.Name, tlsSecret.Namespace)
		})

		It("reconcile verify internal secret owner ref", func() {
			By("stop existing manager as we wish to populate etcd and reconcile directly")
			shutdownControllerManager()
			defer createControllerManagerForSuite()

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.Console.Expose = true
			crd.Spec.Console.SSLEnabled = true

			By("real owner ref requirements, it must exist else we get GC")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			crdKey := types.NamespacedName{Namespace: crd.Namespace, Name: crd.Name}

			By("using typed client to retain kind info")
			restConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
			typedClient, err := v1beta1.NewForConfig(restConfig)
			Expect(err).Should(BeNil())
			Eventually(func(g Gomega) {
				createdCrd, err = typedClient.ActiveMQArtemises(crdKey.Namespace).Get(crdKey.Name, metav1.GetOptions{})
				g.Expect(createdCrd.ResourceVersion).ShouldNot(BeEmpty())
			}, timeout, interval).Should(Succeed())

			brokerCR, convertErr := ConvertArtemisToBrokerCluster(createdCrd)
			Expect(convertErr).To(BeNil())

			outer := NewBrokerClusterReconciler(k8Manager, ctrl.Log, isOpenshift)
			reconcilerImpl := NewBrokerClusterReconcilerImpl(brokerCR, outer)

			namers := MakeNamers(brokerCR)
			defaultConsoleSecretName := createdCrd.Name + "-console-secret"

			tlsSecret, err := CreateTlsSecret(defaultConsoleSecretName, defaultNamespace, "password", nil)
			Expect(err).To(BeNil())
			Expect(k8sClient.Create(ctx, tlsSecret)).To(Succeed())

			internalSecretName := defaultConsoleSecretName + "-internal"
			internalConsoleSecretKey := types.NamespacedName{Namespace: createdCrd.Namespace, Name: internalSecretName}

			currentSS := &appsv1.StatefulSet{}
			currentSS.Name = namers.SsNameBuilder.Name()
			currentSS.Namespace = defaultNamespace

			By("reconciling expecting generation of secret and secret -internal")
			reconcilerImpl.ProcessConsole(brokerCR, *namers, k8sClient, k8sClient.Scheme(), currentSS)
			By("creating internal secret via process resources")
			reconcilerImpl.ProcessResources(brokerCR, k8sClient, k8sClient.Scheme())

			By("finding internal secret with owner ref")
			createdInternalSecret := &corev1.Secret{}
			createdDefaultSecret := &corev1.Secret{}
			var resourceVer string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, internalConsoleSecretKey, createdInternalSecret)).Should(Succeed())
				g.Expect(len(createdInternalSecret.OwnerReferences)).Should(BeEquivalentTo(1))
				g.Expect(*createdInternalSecret.GetOwnerReferences()[0].Controller).Should(BeTrue())
				g.Expect(createdInternalSecret.GetOwnerReferences()[0].BlockOwnerDeletion).Should(BeNil())
				g.Expect(createdInternalSecret.ResourceVersion).ShouldNot(BeNil())
				resourceVer = createdInternalSecret.ResourceVersion
			}, timeout, interval).Should(Succeed())

			By("faking deployed resources")
			// this can't work b/c we are faking the openshift env and no route crd is deployed
			// reconcilerImpl.CurrentDeployedResources(&crd, k8sClient)

			By("populating deployed")
			reconcilerImpl.deployed = make(map[reflect.Type][]client.Object)
			reconcilerImpl.requestedResources = nil

			By("finding in etcd, cache flushed")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, internalConsoleSecretKey, createdInternalSecret)).Should(Succeed())
				g.Expect(strconv.Atoi(createdInternalSecret.ResourceVersion)).Should(BeNumerically(">", 0))
				g.Expect(len(createdInternalSecret.OwnerReferences)).Should(BeEquivalentTo(1))

			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Get(ctx, internalConsoleSecretKey, createdInternalSecret)).Should(Succeed())
			By("populating deployed with " + createdInternalSecret.Name)
			reconcilerImpl.addToDeployed(reflect.TypeOf(corev1.Secret{}), createdInternalSecret)

			By("forcing second reconcile subset with mod to AMQ_CONSOLE_ARGS content for internal secret")
			brokerCR.Spec.Console.UseClientAuth = true

			reconcilerImpl.ProcessConsole(brokerCR, *namers, k8sClient, k8sClient.Scheme(), currentSS)
			reconcilerImpl.ProcessResources(brokerCR, k8sClient, k8sClient.Scheme())

			By("finding again internal secret with owner ref")
			createdInternalSecret = &corev1.Secret{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, internalConsoleSecretKey, createdInternalSecret)).Should(Succeed())
				g.Expect(len(createdInternalSecret.OwnerReferences)).Should(BeEquivalentTo(1))
				g.Expect(*createdInternalSecret.GetOwnerReferences()[0].Controller).Should(BeTrue())
				g.Expect(createdInternalSecret.GetOwnerReferences()[0].BlockOwnerDeletion).Should(BeNil())
				g.Expect(createdInternalSecret.ResourceVersion).ShouldNot(BeEquivalentTo(resourceVer))
			}, timeout, interval).Should(Succeed())

			By("cleanup")
			k8sClient.Delete(ctx, createdInternalSecret)
			k8sClient.Delete(ctx, createdDefaultSecret)
			k8sClient.Delete(ctx, createdCrd)
		})

		It("reconcile verify adopt internal secret that has lost owner ref", func() {
			By("stop existing manager as we wish to populate etcd and reconcile directly")
			shutdownControllerManager()
			defer createControllerManagerForSuite()

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.Console.Expose = true
			crd.Spec.Console.SSLEnabled = true

			By("real owner ref requirements, it must exist else we get GC")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			crdKey := types.NamespacedName{Namespace: crd.Namespace, Name: crd.Name}

			By("using typed client to retain kind info")
			restConfig.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
			typedClient, err := v1beta1.NewForConfig(restConfig)
			Expect(err).Should(BeNil())
			Eventually(func(g Gomega) {
				createdCrd, err = typedClient.ActiveMQArtemises(crdKey.Namespace).Get(crdKey.Name, metav1.GetOptions{})
				g.Expect(err).Should(BeNil())
				g.Expect(createdCrd.ResourceVersion).ShouldNot(BeEmpty())
				// kind gets whacked by the non typed (or non Reader) GET
				g.Expect(createdCrd.Kind).ShouldNot(BeEmpty())

			}, timeout, interval).Should(Succeed())

			brokerCR, convertErr := ConvertArtemisToBrokerCluster(&crd)
			Expect(convertErr).To(BeNil())

			outer := NewBrokerClusterReconciler(k8Manager, ctrl.Log, isOpenshift)
			reconcilerImpl := NewBrokerClusterReconcilerImpl(brokerCR, outer)
			reconcilerImpl.deployed = make(map[reflect.Type][]client.Object)

			namers := MakeNamers(brokerCR)
			defaultConsoleSecretName := crd.Name + "-console-secret"
			internalSecretName := defaultConsoleSecretName + "-internal"
			internalConsoleSecretKey := types.NamespacedName{Namespace: crd.Namespace, Name: internalSecretName}

			tlsSecret, err := CreateTlsSecret(defaultConsoleSecretName, defaultNamespace, "password", nil)
			Expect(err).To(BeNil())
			Expect(k8sClient.Create(ctx, tlsSecret)).To(Succeed())

			By("making abandoned internal secret")
			createdInternalSecret := &corev1.Secret{}
			createdInternalSecret.Name = internalConsoleSecretKey.Name
			createdInternalSecret.Namespace = internalConsoleSecretKey.Namespace

			Expect(k8sClient.Create(ctx, createdInternalSecret)).Should(Succeed())

			var resourceVer string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, internalConsoleSecretKey, createdInternalSecret)).Should(Succeed())
				g.Expect(len(createdInternalSecret.OwnerReferences)).Should(BeEquivalentTo(0))
				g.Expect(createdInternalSecret.ResourceVersion).ShouldNot(BeNil())
				resourceVer = createdInternalSecret.ResourceVersion
			}, timeout, interval).Should(Succeed())

			currentSS := &appsv1.StatefulSet{}
			currentSS.Name = namers.SsNameBuilder.Name()
			currentSS.Namespace = defaultNamespace

			By("reconciling expecting generation of secret and adoption of secret -internal")
			reconcilerImpl.ProcessConsole(brokerCR, *namers, k8sClient, k8sClient.Scheme(), currentSS)
			By("creating internal secret via process resources")
			reconcilerImpl.ProcessResources(brokerCR, k8sClient, k8sClient.Scheme())

			By("finding internal secret with owner ref updated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, internalConsoleSecretKey, createdInternalSecret)).Should(Succeed())
				g.Expect(len(createdInternalSecret.OwnerReferences)).Should(BeEquivalentTo(1))
				g.Expect(createdInternalSecret.ResourceVersion).ShouldNot(BeNil())
				By("on update - resource version changes" + createdInternalSecret.ResourceVersion)
				g.Expect(createdInternalSecret.ResourceVersion).ShouldNot(Equal(resourceVer))
			}, timeout, interval).Should(Succeed())

			By("cleanup")
			k8sClient.Delete(ctx, createdInternalSecret)
			k8sClient.Delete(ctx, createdCrd)
			CleanResource(tlsSecret, tlsSecret.Name, tlsSecret.Namespace)
		})
	})

	Context("PodSecurityContext Test", func() {
		It("Setting the pods PodSecurityContext", func() {
			By("Creating a CR instance with PodSecurityContext configured")

			podSecurityContext := corev1.PodSecurityContext{}
			retrievedCR := &brokerv1beta1.ActiveMQArtemis{}
			createdSS := &appsv1.StatefulSet{}

			context := context.Background()
			defaultCR := generateArtemisSpec(defaultNamespace)
			defaultCR.Spec.DeploymentPlan.PodSecurityContext = &podSecurityContext

			By("Deploying the CR " + defaultCR.ObjectMeta.Name)
			Expect(k8sClient.Create(context, &defaultCR)).Should(Succeed())

			By("Making sure that the CR gets deployed " + defaultCR.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(defaultCR.ObjectMeta.Name, defaultNamespace, retrievedCR)
			}, timeout, interval).Should(BeTrue())
			Expect(retrievedCR.Name).Should(Equal(defaultCR.ObjectMeta.Name))

			By("Checking that the StatefulSet has been created with a PodSecurityContext field " + namer.CrToSS(retrievedCR.Name))
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(retrievedCR.Name), Namespace: defaultNamespace}
				if err := k8sClient.Get(context, key, createdSS); nil != err {
					return false
				}
				return nil != createdSS.Spec.Template.Spec.SecurityContext
			})

			CleanResource(retrievedCR, retrievedCR.Name, defaultNamespace)

			By("Creating a non-default SELinuxOptions CR instance")
			nonDefaultCR := generateArtemisSpec(defaultNamespace)

			credentialSpecName := "credential-spec-name-0"
			credentialSpec := "credential-spec-0"
			runAsUserName := "user-0"
			hostProcess := false
			var runAsUser int64 = 1000
			var runAsGroup int64 = 1001
			runAsNonRoot := true
			var supplementalGroupA int64 = 2000
			var supplementalGroupB int64 = 2001
			supplementalGroups := []int64{supplementalGroupA, supplementalGroupB}
			var fsGroup int64 = 3000
			sysctlA := corev1.Sysctl{
				Name:  "sysctl-a",
				Value: "test",
			}
			sysctlB := corev1.Sysctl{
				Name:  "sysctl-b",
				Value: "test",
			}
			sysctls := []corev1.Sysctl{sysctlA, sysctlB}

			fsGCPString := "Always"
			fsGCP := corev1.PodFSGroupChangePolicy(fsGCPString)
			localhostProfile := "localhost-profile"
			seccompProfile := corev1.SeccompProfile{
				Type:             corev1.SeccompProfileTypeLocalhost,
				LocalhostProfile: &localhostProfile,
			}

			nonDefaultCR.Spec.DeploymentPlan.PodSecurityContext = &corev1.PodSecurityContext{
				SELinuxOptions: &corev1.SELinuxOptions{
					User:  "user-0",
					Role:  "role-0",
					Type:  "type-0",
					Level: "level-0",
				},
				WindowsOptions: &corev1.WindowsSecurityContextOptions{
					GMSACredentialSpecName: &credentialSpecName,
					GMSACredentialSpec:     &credentialSpec,
					RunAsUserName:          &runAsUserName,
					HostProcess:            &hostProcess,
				},
				RunAsUser:           &runAsUser,
				RunAsGroup:          &runAsGroup,
				RunAsNonRoot:        &runAsNonRoot,
				SupplementalGroups:  supplementalGroups,
				FSGroup:             &fsGroup,
				Sysctls:             sysctls,
				FSGroupChangePolicy: &fsGCP,
				SeccompProfile:      &seccompProfile,
			}

			By("Deploying the non-default CR instance named " + nonDefaultCR.Name)
			Expect(k8sClient.Create(context, &nonDefaultCR)).Should(Succeed())

			retrievednonDefaultCR := brokerv1beta1.ActiveMQArtemis{}
			By("Checking to ensure that the non-default CR was created with the right values " + nonDefaultCR.Name)
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: nonDefaultCR.Name, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(context, key, &retrievednonDefaultCR)).Should(Succeed())
				g.Expect(reflect.DeepEqual(nonDefaultCR.Spec.DeploymentPlan.PodSecurityContext,
					retrievednonDefaultCR.Spec.DeploymentPlan.PodSecurityContext)).Should(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("Checking that the StatefulSet has been created with the non-default PodSecurityContext " + namer.CrToSS(nonDefaultCR.Name))
			Eventually(func(g Gomega) {
				nonDefaultSS := &appsv1.StatefulSet{}
				key := types.NamespacedName{Name: namer.CrToSS(nonDefaultCR.Name), Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(context, key, nonDefaultSS)).Should(Succeed())

				g.Expect(nonDefaultSS.Spec.Template.Spec.SecurityContext).ShouldNot(BeNil())
				g.Expect(reflect.DeepEqual(nonDefaultCR.Spec.DeploymentPlan.PodSecurityContext, nonDefaultSS.Spec.Template.Spec.SecurityContext)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Check for non-default CR instance deletion")
			CleanResource(&retrievednonDefaultCR, retrievednonDefaultCR.Name, defaultNamespace)
		})
	})

	Context("Affinity Test", func() {
		It("setting Pod Affinity", func() {
			By("Creating a crd with pod affinity")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			labelSelector := metav1.LabelSelector{}
			labelSelector.MatchLabels = make(map[string]string)
			labelSelector.MatchLabels["key"] = "value"

			podAffinityTerm := corev1.PodAffinityTerm{
				TopologyKey: "aa",
			}
			podAffinityTerm.LabelSelector = &labelSelector
			podAffinityTerm.TopologyKey = "kubernetes.io/hostname"

			podAffinity := corev1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					podAffinityTerm,
				},
			}
			crd.Spec.DeploymentPlan.Affinity.PodAffinity = &podAffinity

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))
			By("Checking that Stateful Set is Created with the node selectors " + namer.CrToSS(createdCrd.Name))
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Affinity.PodAffinity != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the pd affinity are correct")
			Expect(createdSs.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels["key"] == "value").Should(BeTrue())

			By("Updating the CR")
			Eventually(func(g Gomega) {

				// we need to update the latest version and deal with update failures
				g.Expect(getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)).Should(BeTrue())
				original := createdCrd

				labelSelector = metav1.LabelSelector{}
				labelSelector.MatchLabels = make(map[string]string)
				labelSelector.MatchLabels["key"] = "differentvalue"

				podAffinityTerm = corev1.PodAffinityTerm{
					TopologyKey: "bb",
				}
				podAffinityTerm.LabelSelector = &labelSelector
				podAffinityTerm.TopologyKey = "kubernetes.io/hostname"
				podAffinity = corev1.PodAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						podAffinityTerm,
					},
				}
				original.Spec.DeploymentPlan.Affinity.PodAffinity = &podAffinity
				By("Redeploying the CRD")
				g.Expect(k8sClient.Update(ctx, original)).Should(Succeed())

				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				g.Expect(k8sClient.Get(ctx, key, createdSs))

				g.Expect(createdSs.Spec.Template.Spec.Affinity.PodAffinity).ShouldNot(BeNil())

				g.Expect(len(createdSs.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution)).Should(BeEquivalentTo(1))

				By("Making sure the pd affinity are correct")
				g.Expect(createdSs.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels["key"]).Should(Equal("differentvalue"))

			}, timeout, interval).Should(Succeed())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("setting Pod AntiAffinity", func() {
			By("Creating a crd with pod anti affinity")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			labelSelector := metav1.LabelSelector{}
			labelSelector.MatchLabels = make(map[string]string)
			labelSelector.MatchLabels["key"] = "value"

			podAffinityTerm := corev1.PodAffinityTerm{
				TopologyKey: "cc",
			}
			podAffinityTerm.LabelSelector = &labelSelector
			podAffinityTerm.TopologyKey = "kubernetes.io/hostname"
			podAntiAffinity := corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					podAffinityTerm,
				},
			}
			crd.Spec.DeploymentPlan.Affinity.PodAntiAffinity = &podAntiAffinity

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))
			By("Checking that Stateful Set is Created with the node selectors " + namer.CrToSS(createdCrd.Name))
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Affinity.PodAntiAffinity != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the pd affinity are correct")
			Expect(createdSs.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels["key"]).Should(Equal("value"))

			By("Updating the CR")
			Eventually(func(g Gomega) {

				g.Expect(getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)).Should(BeTrue())
				original := createdCrd

				labelSelector = metav1.LabelSelector{}
				labelSelector.MatchLabels = make(map[string]string)
				labelSelector.MatchLabels["key"] = "differentvalue"

				podAffinityTerm = corev1.PodAffinityTerm{
					TopologyKey: "dd",
				}
				podAffinityTerm.LabelSelector = &labelSelector
				podAffinityTerm.TopologyKey = "kubernetes.io/hostname"
				podAntiAffinity = corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						podAffinityTerm,
					},
				}
				original.Spec.DeploymentPlan.Affinity.PodAntiAffinity = &podAntiAffinity
				By("Redeploying the CRD")
				k8sClient.Update(ctx, original)

				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				g.Expect(createdSs.Spec.Template.Spec.Affinity.PodAntiAffinity).ShouldNot(BeNil())

				g.Expect(len(createdSs.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution)).Should(Equal(1))

				By("Making sure the pd affinity are correct")
				g.Expect(createdSs.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels["key"]).Should(Equal("differentvalue"))

			}, timeout, interval).Should(Succeed())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
		It("setting Node AntiAffinity", func() {
			By("Creating a crd with node affinity")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			nodeSelectorRequirement := corev1.NodeSelectorRequirement{
				Key:      "foo",
				Values:   make([]string, 1),
				Operator: corev1.NodeSelectorOpIn,
			}
			nodeSelectorRequirements := [1]corev1.NodeSelectorRequirement{nodeSelectorRequirement}
			nodeSelectorRequirements[0] = nodeSelectorRequirement
			nodeSelectorTerm := corev1.NodeSelectorTerm{MatchExpressions: nodeSelectorRequirements[:]}
			nodeSelectorTerms := [1]corev1.NodeSelectorTerm{nodeSelectorTerm}
			nodeSelector := corev1.NodeSelector{
				NodeSelectorTerms: nodeSelectorTerms[:],
			}
			nodeAffinity := corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &nodeSelector,
			}
			crd.Spec.DeploymentPlan.Affinity.NodeAffinity = &nodeAffinity

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))
			By("Checking that Stateful Set is Created with the node selectors " + namer.CrToSS(createdCrd.Name))
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Affinity.NodeAffinity != nil
			}, timeout, interval).Should(BeTrue())

			By("Making sure the node affinity are correct")
			Expect(createdSs.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key).Should(Equal("foo"))

			Eventually(func(g Gomega) {
				By("Updating the CR")
				g.Expect(getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)).Should(BeTrue())
				original := createdCrd

				nodeSelectorRequirement = corev1.NodeSelectorRequirement{
					Key:      "bar",
					Values:   make([]string, 2),
					Operator: corev1.NodeSelectorOpIn,
				}
				nodeSelectorRequirements = [1]corev1.NodeSelectorRequirement{nodeSelectorRequirement}
				nodeSelectorRequirements[0] = nodeSelectorRequirement
				nodeSelectorTerm = corev1.NodeSelectorTerm{MatchExpressions: nodeSelectorRequirements[:]}
				nodeSelectorTerms = [1]corev1.NodeSelectorTerm{nodeSelectorTerm}
				nodeSelector = corev1.NodeSelector{
					NodeSelectorTerms: nodeSelectorTerms[:],
				}
				nodeAffinity = corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &nodeSelector,
				}
				original.Spec.DeploymentPlan.Affinity.NodeAffinity = &nodeAffinity
				By("Redeploying the CRD")
				k8sClient.Update(ctx, original)

				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				g.Expect(len(createdSs.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms)).Should(BeEquivalentTo(1))

				By("Making sure the pod affinity is correct")
				g.Expect(createdSs.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key).Should(Equal("bar"))

			}, timeout, interval).Should(Succeed())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Node Selector Test", func() {
		It("passing in 2 labels", func() {
			By("Creating a crd with 2 selectors")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			nodeSelector := map[string]string{
				"location": "production",
				"type":     "foo",
			}
			crd.Spec.DeploymentPlan.NodeSelector = nodeSelector

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))
			By("Checking that Stateful Set is Created with the node selectors " + namer.CrToSS(createdCrd.Name))
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return len(createdSs.Spec.Template.Spec.NodeSelector) == 2
			}, timeout, interval).Should(Equal(true))

			By("Making sure the node selectors are correct")
			Expect(createdSs.Spec.Template.Spec.NodeSelector["location"] == "production").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.NodeSelector["type"] == "foo").Should(BeTrue())

			By("Updating the CR")
			Eventually(func(g Gomega) {

				g.Expect(getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)).Should(BeTrue())
				original := createdCrd

				nodeSelector = map[string]string{
					"type": "foo",
				}
				original.Spec.DeploymentPlan.NodeSelector = nodeSelector
				By("Redeploying the CRD")
				g.Expect(k8sClient.Update(ctx, original)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			Eventually(func() int {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return -1
				}
				return len(createdSs.Spec.Template.Spec.NodeSelector)
			}, timeout, interval).Should(Equal(1))

			By("Making sure the node selectors are correct")
			Expect(createdSs.Spec.Template.Spec.NodeSelector["location"] == "production").Should(BeFalse())
			Expect(createdSs.Spec.Template.Spec.NodeSelector["type"] == "foo").Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Annotations Test", Label("annotations-test"), func() {
		It("add some annotations", func() {
			By("deploy the broker with custom annotations")
			customAnnotations := make(map[string]string)
			customAnnotations["sidecar.istio.io/inject"] = "true"
			customAnnotations["promethes-prop"] = "somevalue"

			brokerCr, createdCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.Annotations = customAnnotations
			})

			By("Making sure that the CR gets deployed " + brokerCr.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(brokerCr.Name, defaultNamespace, createdCr)
			}, timeout, interval).Should(BeTrue())

			key := types.NamespacedName{Name: namer.CrToSS(createdCr.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}

			By("Checking that StatefulSet is Created with correct annotations")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
				found := 0
				for k, v := range createdSs.Spec.Template.Annotations {
					if k == "sidecar.istio.io/inject" && v == "true" {
						found++
					} else if k == "promethes-prop" && v == "somevalue" {
						found++
					}
				}
				g.Expect(found).To(Equal(2))

			}, timeout, interval).Should(Succeed())

			By("verify external mod to annotation is respected by reconcile")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
				createdSs.Spec.Template.Annotations["externalController"] = "seen it!"
				g.Expect(createdSs.GetAnnotations()).To(BeNil())
				createdSs.Annotations = map[string]string{}
				createdSs.Annotations["externalController"] = "seen it!"
				g.Expect(k8sClient.Update(ctx, createdSs)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("forcing reconcile with further annotation update")
			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(brokerCr.Name, defaultNamespace, createdCr)).Should(BeTrue())
				createdCr.Spec.DeploymentPlan.Annotations["new"] = "true"
				g.Expect(k8sClient.Update(ctx, createdCr)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verify reconcile and external annotaion present")
			createdSs = &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				_, ok := createdSs.Spec.Template.Annotations["externalController"]
				g.Expect(ok).Should(BeTrue())

				_, ok = createdSs.Spec.Template.Annotations["new"]
				g.Expect(ok).Should(BeTrue())

				_, ok = createdSs.Spec.Template.Annotations["promethes-prop"]
				g.Expect(ok).Should(BeTrue())

				_, ok = createdSs.Annotations["externalController"]
				g.Expect(ok).Should(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("verify annotation removal")
			stateFulSetKindString := "StatefulSet"
			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(brokerCr.Name, defaultNamespace, createdCr)).Should(BeTrue())
				createdCr.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
					{
						Selector: &brokerv1beta1.ResourceSelector{
							Kind: &stateFulSetKindString,
						},
						Annotations: map[string]string{
							"externalController": "-",
							"removalDone":        "true"},
					},
				}
				g.Expect(k8sClient.Update(ctx, createdCr)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verify reconcile and external annotaion removed via resource template")
			createdSs = &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				_, ok := createdSs.Annotations["removalDone"]
				g.Expect(ok).Should(BeTrue())

				_, ok = createdSs.Annotations["externalController"]
				g.Expect(ok).Should(BeFalse())

			}, timeout, interval).Should(Succeed())

			CleanResource(createdCr, createdCr.Name, defaultNamespace)
		})
	})

	Context("Labels Test", func() {
		It("passing in 2 labels", func() {
			By("Creating a crd with 2 labels, verifying only on pod template")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Labels = make(map[string]string)
			crd.Spec.DeploymentPlan.Labels["key1"] = "val1"
			crd.Spec.DeploymentPlan.Labels["key2"] = "val2"

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))
			By("Checking that Stateful Set is Created with the labels " + namer.CrToSS(createdCrd.Name))
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				g.Expect(len(createdSs.ObjectMeta.Labels)).Should(BeNumerically("==", 2))
				g.Expect(len(createdSs.Spec.Selector.MatchLabels)).Should(BeNumerically("==", 2))

				g.Expect(len(createdSs.Spec.Template.Labels)).Should(BeNumerically(">=", 4))

			}, timeout, interval).Should(Succeed())

			By("Making sure the labels are correct")
			Expect(createdSs.Spec.Template.Labels["key1"]).Should(Equal("val1"))
			Expect(createdSs.Spec.Template.Labels["key2"]).Should(Equal("val2"))

			By("check it has gone")
			Expect(k8sClient.Delete(ctx, createdCrd))

		})

		It("passing in 8 labels", func() {
			By("Creating a crd with 8 labels")
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Labels = make(map[string]string)
			crd.Spec.DeploymentPlan.Labels["key1"] = "val1"
			crd.Spec.DeploymentPlan.Labels["key2"] = "val2"
			crd.Spec.DeploymentPlan.Labels["key3"] = "val3"
			crd.Spec.DeploymentPlan.Labels["key4"] = "val4"
			crd.Spec.DeploymentPlan.Labels["key5"] = "val5"
			crd.Spec.DeploymentPlan.Labels["key6"] = "val6"
			crd.Spec.DeploymentPlan.Labels["key7"] = "val7"
			crd.Spec.DeploymentPlan.Labels["key8"] = "val8"

			brokerCR, convertErr := ConvertArtemisToBrokerCluster(&crd)
			Expect(convertErr).To(BeNil())

			namers := MakeNamers(brokerCR)
			namespacedName := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

			crd0 := brokerCR.DeepCopy()
			By("Processing status 0")
			common.ProcessStatus(crd0, k8sClient, namespacedName, *namers, nil)
			Expect(crd0.Status.ScaleLabelSelector).ShouldNot(BeEmpty())
			Expect(sort.StringsAreSorted(strings.Split(crd0.Status.ScaleLabelSelector, ","))).Should(BeTrue())

			crd1 := brokerCR.DeepCopy()
			By("Processing status 1")
			common.ProcessStatus(crd1, k8sClient, namespacedName, *namers, nil)
			Expect(crd1.Status.ScaleLabelSelector).ShouldNot(BeEmpty())
			Expect(sort.StringsAreSorted(strings.Split(crd1.Status.ScaleLabelSelector, ","))).Should(BeTrue())

			Expect(EqualBrokerClusterCRStatus(&crd0.Status, &crd1.Status)).Should(BeTrue())
		})
	})

	Context("Different namespace, deployed before start", func() {
		It("verify reconcile in own namespace", func() {

			By("By stopping suite controller that watches all namespace")
			shutdownControllerManager()

			By("By creating a new crd")
			ctx := context.Background()

			nonDefaultNamespace := "non-default"

			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nonDefaultNamespace,
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil {
				Expect(errors.IsConflict(err))
			}

			crd := generateArtemisSpec(nonDefaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, nonDefaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("By checking absence of stateful set with no matching controller")
			key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: nonDefaultNamespace}
			createdSs := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, key, createdSs)).ShouldNot(Succeed())

			By("By starting reconciler for this namespace")
			createControllerManager(true, nonDefaultNamespace)

			key = types.NamespacedName{Name: createdCrd.Name, Namespace: nonDefaultNamespace}

			Eventually(func(g Gomega) {
				By("Checking stopped status of CR, deployed with replica count 0")
				g.Expect(k8sClient.Get(ctx, key, createdCrd)).Should(Succeed())
				g.Expect(len(createdCrd.Status.PodStatus.Stopped)).Should(BeEquivalentTo(1))
				g.Expect(common.IsConditionPresentAndEqual(createdCrd.Status.Conditions, metav1.Condition{
					Type:    brokerv1beta1.DeployedConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.DeployedConditionZeroSizeReason,
					Message: common.DeployedConditionZeroSizeMessage,
				})).Should(BeTrue())
				g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType, metav1.ConditionUnknown)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())

			}, timeout, interval).Should(Succeed())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)

			By("restoring default manager")
			shutdownControllerManager()
			createControllerManagerForSuite()

			By("Deleting non-default ns")
			k8sClient.Delete(ctx, ns)
		})
	})

	Context("Tolerations Test", func() {
		It("passing in 2 tolerations", func() {

			By("Creating a crd with 2 tolerations")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Tolerations = []corev1.Toleration{
				{
					Key:    "foo",
					Value:  "bar",
					Effect: "NoSchedule",
				},
				{
					Key:    "yes",
					Value:  "No",
					Effect: "NoSchedule",
				},
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the tolerations " + namer.CrToSS(createdCrd.Name))
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return len(createdSs.Spec.Template.Spec.Tolerations) == 2
			}, timeout, interval).Should(Equal(true))
			Expect(len(createdSs.Spec.Template.Spec.Tolerations) == 2).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[0].Key == "foo").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[0].Value == "bar").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[0].Effect == "NoSchedule").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[1].Key == "yes").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[1].Value == "No").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[1].Effect == "NoSchedule").Should(BeTrue())

			By("Redeploying the CRD with different Tolerations")

			Eventually(func() bool {

				// fetch, modify and update (we compete with the status updates)

				ok := getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
				if !ok {
					return false
				}

				createdCrd.Spec.DeploymentPlan.Tolerations = []corev1.Toleration{
					{
						Key:    "yes",
						Value:  "No",
						Effect: "NoSchedule",
					},
				}

				err := k8sClient.Update(ctx, createdCrd)
				return err == nil
			}, timeout, interval).Should(Equal(true))

			By("and checking there is just a single Toleration")
			Eventually(func() (int, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return 0, err
				}
				return len(createdSs.Spec.Template.Spec.Tolerations), err
			}, timeout, interval).Should(Equal(1))
			Expect(len(createdSs.Spec.Template.Spec.Tolerations) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[0].Key == "yes").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[0].Value == "No").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Tolerations[0].Effect == "NoSchedule").Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Liveness Probe Tests", func() {
		It("Override Liveness Probe No Exec", func() {
			By("By creating a crd with Liveness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			livenessProbe := corev1.Probe{}
			livenessProbe.PeriodSeconds = 5
			livenessProbe.InitialDelaySeconds = 6
			livenessProbe.TimeoutSeconds = 7
			livenessProbe.SuccessThreshold = 1
			livenessProbe.FailureThreshold = 9
			crd.Spec.DeploymentPlan.LivenessProbe = &livenessProbe
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}

			By("Deploying the CRD")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Making sure that the CRD gets deployed")
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the Liveness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TCPSocket != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Liveness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.TCPSocket.Port.String() == "8161").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds == 5).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds == 6).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds == 7).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.SuccessThreshold == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.FailureThreshold == 9).Should(BeTrue())

			By("Updating the CR")
			Eventually(func(g Gomega) {

				g.Expect(getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)).Should(BeTrue())

				original := createdCrd

				original.Spec.DeploymentPlan.LivenessProbe.PeriodSeconds = 15
				original.Spec.DeploymentPlan.LivenessProbe.InitialDelaySeconds = 16
				original.Spec.DeploymentPlan.LivenessProbe.TimeoutSeconds = 17
				original.Spec.DeploymentPlan.LivenessProbe.SuccessThreshold = 1
				original.Spec.DeploymentPlan.LivenessProbe.FailureThreshold = 19
				exec := corev1.ExecAction{
					Command: []string{"/broker/bin/artemis check node"},
				}
				original.Spec.DeploymentPlan.LivenessProbe.Exec = &exec
				By("Redeploying the modified CRD")
				g.Expect(k8sClient.Update(ctx, original)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("Retrieving the new SS to find the modification")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds == 15
			}, timeout, interval).Should(Equal(true))

			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.Exec != nil).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.Exec.Command[0] == "/broker/bin/artemis check node").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds == 15).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds == 16).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds == 17).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.SuccessThreshold == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.FailureThreshold == 19).Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Override Liveness Probe Exec", func() {
			By("By creating a crd with Liveness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			exec := corev1.ExecAction{
				Command: []string{"/broker/bin/artemis check node"},
			}
			livenessProbe := corev1.Probe{}
			livenessProbe.PeriodSeconds = 5
			livenessProbe.InitialDelaySeconds = 6
			livenessProbe.TimeoutSeconds = 7
			livenessProbe.SuccessThreshold = 1
			livenessProbe.FailureThreshold = 9
			livenessProbe.Exec = &exec
			crd.Spec.DeploymentPlan.LivenessProbe = &livenessProbe
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			By("Deploying the CRD")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Making sure that the CRD gets deployed")
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the Liveness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.Exec != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Liveness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.Exec.Command[0] == "/broker/bin/artemis check node").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds == 5).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds == 6).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds == 7).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.SuccessThreshold == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.FailureThreshold == 9).Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Default Liveness Probe", func() {
			By("By creating a crd without Liveness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			// whack test defaults
			crd.Spec.DeploymentPlan.ReadinessProbe = nil
			crd.Spec.DeploymentPlan.LivenessProbe = nil

			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that the Liveness Probe is created")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TCPSocket != nil
			}, timeout, interval).Should(Equal(true))

			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.TCPSocket.Port.String() == "8161").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds).Should(BeEquivalentTo(5))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Override Liveness Probe Default TCPSocket", func() {
			By("By creating a crd with Liveness Probe")
			ctx := context.Background()
			_, createdCrd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				tcpSocketAction := corev1.TCPSocketAction{
					Port: intstr.FromInt(8161),
				}
				livenessProbe := corev1.Probe{}
				livenessProbe.FailureThreshold = 3
				livenessProbe.InitialDelaySeconds = 60
				livenessProbe.PeriodSeconds = 10
				livenessProbe.SuccessThreshold = 1
				livenessProbe.TCPSocket = &tcpSocketAction
				livenessProbe.TimeoutSeconds = 5

				candidate.Spec.DeploymentPlan.LivenessProbe = &livenessProbe
			})

			createdSs := &appsv1.StatefulSet{}

			By("Checking that Stateful Set is Created with the Liveness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TCPSocket != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Liveness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.TCPSocket.Port.String() == "8161").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds).Should(BeEquivalentTo(5))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.FailureThreshold).Should(BeEquivalentTo(3))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.InitialDelaySeconds).Should(BeEquivalentTo(60))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).Should(BeEquivalentTo(10))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.SuccessThreshold).Should(BeEquivalentTo(1))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Override Liveness Probe Default HTTPGet", func() {
			By("By creating a crd with Liveness Probe")
			ctx := context.Background()
			_, createdCrd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				httpGetAction := corev1.HTTPGetAction{
					Port: intstr.FromInt(8161),
				}
				livenessProbe := corev1.Probe{}
				livenessProbe.FailureThreshold = 3
				livenessProbe.HTTPGet = &httpGetAction
				livenessProbe.PeriodSeconds = 10
				livenessProbe.SuccessThreshold = 1
				livenessProbe.TimeoutSeconds = 5

				candidate.Spec.DeploymentPlan.LivenessProbe = &livenessProbe
			})

			createdSs := &appsv1.StatefulSet{}

			By("Checking that Stateful Set is Created with the Liveness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Liveness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.HTTPGet.Port.String() == "8161").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.FailureThreshold).Should(BeEquivalentTo(3))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).Should(BeEquivalentTo(10))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.SuccessThreshold).Should(BeEquivalentTo(1))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds).Should(BeEquivalentTo(5))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Override Liveness Probe Default GRPC", func() {
			By("By creating a crd with Liveness Probe")
			ctx := context.Background()
			_, createdCrd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				grpcAction := corev1.GRPCAction{
					Port: 4321,
				}
				livenessProbe := corev1.Probe{}
				livenessProbe.FailureThreshold = 3
				livenessProbe.GRPC = &grpcAction
				livenessProbe.PeriodSeconds = 10
				livenessProbe.SuccessThreshold = 1
				livenessProbe.TimeoutSeconds = 5

				candidate.Spec.DeploymentPlan.LivenessProbe = &livenessProbe
			})

			createdSs := &appsv1.StatefulSet{}

			By("Checking that Stateful Set is Created with the Liveness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.GRPC != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Liveness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.ProbeHandler.GRPC.Port == 4321).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.FailureThreshold).Should(BeEquivalentTo(3))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.PeriodSeconds).Should(BeEquivalentTo(10))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.SuccessThreshold).Should(BeEquivalentTo(1))
			Expect(createdSs.Spec.Template.Spec.Containers[0].LivenessProbe.TimeoutSeconds).Should(BeEquivalentTo(5))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Readiness Probe Tests", func() {
		It("Override Readiness Probe No Exec", func() {
			By("By creating a crd with Readiness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			readinessProbe := corev1.Probe{}
			readinessProbe.PeriodSeconds = 5
			readinessProbe.InitialDelaySeconds = 6
			readinessProbe.TimeoutSeconds = 7
			readinessProbe.SuccessThreshold = 8
			readinessProbe.FailureThreshold = 9
			crd.Spec.DeploymentPlan.ReadinessProbe = &readinessProbe
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			By("Deploying the CRD")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Making sure that the CRD gets deployed")
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the Readiness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.Exec != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Readiness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[0] == "/bin/bash").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[1] == "-c").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[2] == "/opt/amq/bin/readinessProbe.sh 1").Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Override Readiness Probe Exec", func() {
			By("By creating a crd with Readiness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			exec := corev1.ExecAction{
				Command: []string{"/broker/bin/artemis check node"},
			}
			readinessProbe := corev1.Probe{}
			readinessProbe.Exec = &exec
			crd.Spec.DeploymentPlan.ReadinessProbe = &readinessProbe
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			By("Deploying the CRD")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Making sure that the CRD gets deployed")
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the Readiness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.Exec != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Readiness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[0] == "/broker/bin/artemis check node").Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Override Readiness Probe GRPC", func() {
			By("By creating a crd with Readiness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			grpcAction := corev1.GRPCAction{
				Port: 4321,
			}
			readinessProbe := corev1.Probe{}
			readinessProbe.GRPC = &grpcAction
			crd.Spec.DeploymentPlan.ReadinessProbe = &readinessProbe
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			By("Deploying the CRD")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Making sure that the CRD gets deployed")
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the Readiness Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.GRPC != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Readiness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.GRPC.Port == 4321).Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Default Readiness Probe", func() {
			By("By creating a crd without Readiness Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that the Readiness Probe is created")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.Exec != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Readiness probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[0] == "/bin/bash").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[1] == "-c").Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler.Exec.Command[2] == "/opt/amq/bin/readinessProbe.sh 1").Should(BeTrue())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Startup Probe Tests", func() {
		It("Startup Probe with Exec", func() {
			By("By creating a crd with Startup Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			exec := corev1.ExecAction{
				Command: []string{"/broker/bin/artemis check node"},
			}
			startupProbe := corev1.Probe{}
			startupProbe.Exec = &exec
			crd.Spec.DeploymentPlan.StartupProbe = &startupProbe
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			By("Deploying the CRD")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Making sure that the CRD gets deployed")
			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that Stateful Set is Created with the Startup Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].StartupProbe.Exec != nil
			}, timeout, interval).Should(Equal(true))

			By("Making sure the Startup probe is correct")
			Expect(len(createdSs.Spec.Template.Spec.Containers) == 1).Should(BeTrue())
			Expect(createdSs.Spec.Template.Spec.Containers[0].StartupProbe.ProbeHandler.Exec.Command[0] == "/broker/bin/artemis check node").Should(BeTrue())

			By("Removing the Startup Probe")
			Eventually(func() bool {
				if getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) {
					createdCrd.Spec.DeploymentPlan.StartupProbe = nil

					err := k8sClient.Update(ctx, createdCrd)

					if err == nil {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("Checking that Stateful Set is Updated without the Startup Probe")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].StartupProbe == nil
			}, timeout, interval).Should(Equal(true))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Default Startup Probe", func() {
			By("By creating a crd without Startup Probe")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.AdminUser = "admin"
			crd.Spec.AdminPassword = "password"
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			createdSs := &appsv1.StatefulSet{}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("Checking that the Startup Probe is not created")
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err != nil {
					return false
				}
				return createdSs.Spec.Template.Spec.Containers[0].StartupProbe == nil
			}, timeout, interval).Should(Equal(true))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Status", func() {
		It("Expect pod desc", func() {
			By("By creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			// would like more status updates on createdCrd

			By("By checking the status of stateful set")
			Eventually(func() (int, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
				createdSs := &appsv1.StatefulSet{}

				err := k8sClient.Get(ctx, key, createdSs)
				if err != nil {
					return -1, err
				}

				// presence is good enough... check on this status just for kicks
				return int(createdSs.Status.Replicas), err
			}, duration, interval).Should(Equal(0))

			By("Checking stopped status of CR because we expect it to fail to deploy")
			Eventually(func() (int, error) {
				key := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: defaultNamespace}
				err := k8sClient.Get(ctx, key, createdCrd)

				if err != nil {
					return -1, err
				}

				return len(createdCrd.Status.PodStatus.Stopped), nil
			}, timeout, interval).Should(Equal(1))

			By("Checking presence of secrets")
			secretList := &corev1.SecretList{}
			opts := &client.ListOptions{
				Namespace: defaultNamespace,
			}
			Eventually(func() int {
				err := k8sClient.List(ctx, secretList, opts)
				if err != nil {
					fmt.Printf("error getting secretList! %v", err)
				}
				count := 0
				for _, s := range secretList.Items {
					if strings.Contains(s.ObjectMeta.Name, createdCrd.Name) {
						count++
					}
				}
				return count
			}, timeout, interval).Should(Equal(3))

			By("deleting crd")
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("Validation", func() {
		It("with test labels", func() {
			ctx := context.Background()
			var err error
			var deployedCrdKey types.NamespacedName
			var deployedCrd brokerv1beta1.ActiveMQArtemis
			var deployedStatefulSet *appsv1.StatefulSet
			var deployedResources map[reflect.Type][]client.Object
			statefulSetType := reflect.TypeOf(appsv1.StatefulSet{})

			By("creating invalid crd with reserved label")
			invalidCrd := generateArtemisSpec(defaultNamespace)
			invalidCrd.Spec.DeploymentPlan.Labels = map[string]string{"application": "test"}
			invalidCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)
			Expect(k8sClient.Create(ctx, &invalidCrd)).Should(Succeed())

			By("creating valid crd")
			validCrd := generateArtemisSpec(defaultNamespace)
			validCrd.Spec.DeploymentPlan.Labels = map[string]string{"test": "test"}
			validCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)
			Expect(k8sClient.Create(ctx, &validCrd)).Should(Succeed())

			By("checking valid CR")
			deployedCrdKey = types.NamespacedName{Name: validCrd.ObjectMeta.Name, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deployedCrdKey, &deployedCrd)).Should(Succeed())
				g.Expect(deployedCrd.Name).Should(Equal(validCrd.ObjectMeta.Name))
				g.Expect(len(deployedCrd.Status.PodStatus.Stopped)).Should(Equal(1))
				g.Expect(deployedCrd.Status.PodStatus.Stopped[0]).Should(Equal(namer.CrToSS(validCrd.Name)))
				g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("checking deployed resources of valid CR")
			validBrokerCR, convertErr := ConvertArtemisToBrokerCluster(&validCrd)
			Expect(convertErr).To(BeNil())
			deployedResources, err = common.GetDeployedResources(validBrokerCR, k8sClient, isOpenshift)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployedResources).ShouldNot(BeEmpty())

			By("checking template labels of valid CR")
			deployedStatefulSet = deployedResources[statefulSetType][0].(*appsv1.StatefulSet)
			Expect(deployedStatefulSet.Spec.Template.Labels["test"]).Should(Equal("test"))

			By("checking invalid CR")
			deployedCrdKey = types.NamespacedName{Name: invalidCrd.ObjectMeta.Name, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deployedCrdKey, &deployedCrd)).Should(Succeed())
				g.Expect(deployedCrd.Name).Should(Equal(invalidCrd.ObjectMeta.Name))
			}, timeout, interval).Should(Succeed())

			invalidBrokerCR, convertErr := ConvertArtemisToBrokerCluster(&invalidCrd)
			Expect(convertErr).To(BeNil())
			deployedResources, err = common.GetDeployedResources(invalidBrokerCR, k8sClient, isOpenshift)
			Expect(err).Should(Succeed())
			Expect(deployedResources).Should(BeEmpty())

			By("verify status valid false")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deployedCrdKey, &deployedCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionFalse(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				g.Expect(meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType).Reason).Should(Equal(brokerv1beta1.ValidConditionFailedReservedLabelReason))
				g.Expect(meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType).Message).Should(ContainSubstring("application"))
				g.Expect(meta.IsStatusConditionFalse(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("updating invalid crd")
			invalidCrd.Spec.DeploymentPlan.Labels = map[string]string{"test": "test"}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deployedCrdKey, &deployedCrd)).Should(Succeed())
				g.Expect(deployedCrd.Name).Should(Equal(invalidCrd.ObjectMeta.Name))
				deployedCrd.Spec.DeploymentPlan.Labels = map[string]string{"test": "test"}
				Expect(k8sClient.Update(ctx, &deployedCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("checking updated invalid CR")
			deployedCrdKey = types.NamespacedName{Name: invalidCrd.ObjectMeta.Name, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deployedCrdKey, &deployedCrd)).Should(Succeed())
				g.Expect(deployedCrd.Name).Should(Equal(invalidCrd.ObjectMeta.Name))
				g.Expect(len(deployedCrd.Status.PodStatus.Stopped)).Should(Equal(1))
				g.Expect(deployedCrd.Status.PodStatus.Stopped[0]).Should(Equal(namer.CrToSS(invalidCrd.Name)))
				By("verify status valid true")
				g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				g.Expect(meta.FindStatusCondition(deployedCrd.Status.Conditions, brokerv1beta1.ValidConditionType).Reason).Should(Equal(brokerv1beta1.ValidConditionSuccessReason))
			}, timeout, interval).Should(Succeed())

			By("checking deployed resources of updated invalid CR")
			validBrokerCR, convertErr = ConvertArtemisToBrokerCluster(&validCrd)
			Expect(convertErr).To(BeNil())
			deployedResources, err = common.GetDeployedResources(validBrokerCR, k8sClient, isOpenshift)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(deployedResources).ShouldNot(BeEmpty())

			By("checking template labels of updated invalid CR")
			deployedStatefulSet = deployedResources[statefulSetType][0].(*appsv1.StatefulSet)
			Expect(deployedStatefulSet.Spec.Template.Labels["test"]).Should(Equal("test"))

			By("Deleting crds")
			Expect(k8sClient.Delete(ctx, &validCrd)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &invalidCrd)).Should(Succeed())
		})

		It("with ingress in openshift", Label("with-ing-in-openshift"), func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" || !isOpenshift {
				Skip("Existing OpenShift cluster required")
			}
			ingressType := reflect.TypeOf(netv1.Ingress{})
			By("creating valid crd")
			acceptorName := "acceptor0"
			_, crd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:        acceptorName,
						Expose:      true,
						ExposeMode:  &brokerv1beta1.ExposeModes.Ingress,
						IngressHost: "ing.$(ITEM_NAME).$(CR_NAME)-$(BROKER_ORDINAL).$(CR_NAMESPACE).$(INGRESS_DOMAIN)",
						Port:        5555,
						Protocols:   "ALL",
					},
				}
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.IngressDomain = "apps-crc.testing"
			})

			By("checking the ingress has been tracked")
			deployed := &brokerv1beta1.ActiveMQArtemis{}
			crdKey := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, deployed)).Should(Succeed())
				g.Expect(deployed.Name).Should(Equal(crd.Name))

				deployedBrokerCR, convertErr := ConvertArtemisToBrokerCluster(deployed)
				g.Expect(convertErr).To(BeNil())
				deployedResources, err := common.GetDeployedResources(deployedBrokerCR, k8sClient, true)
				g.Expect(err).Should(Succeed())
				g.Expect(deployedResources).ShouldNot(BeEmpty())
				listOfIngress := deployedResources[ingressType]
				g.Expect(len(listOfIngress)).To(Equal(1))
				g.Expect(listOfIngress[0].GetName()).To(Equal(crd.Name + "-" + acceptorName + "-0-svc-ing"))
			}, timeout, interval).Should(Succeed())
			By("clean up")
			Expect(k8sClient.Delete(ctx, deployed)).Should(Succeed())
		})
	})

	Context("Env var updates TRIGGERED_ROLL_COUNT checksum", func() {
		It("Expect TRIGGERED_ROLL_COUNT count non 0", func() {
			By("By creating a new crd")
			var checkSum string
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.AdminUser = "Joe"
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func() bool { return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) }, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("By checking the container stateful set for TRIGGERED_ROLL_COUNT non zero")
			Eventually(func() (bool, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
				createdSs := &appsv1.StatefulSet{}

				err := k8sClient.Get(ctx, key, createdSs)
				if err != nil {
					return false, err
				}

				found := false
				for _, container := range createdSs.Spec.Template.Spec.Containers {
					for _, env := range container.Env {
						if env.Name == "TRIGGERED_ROLL_COUNT" {
							if env.Value > "0" {
								checkSum = env.Value
								found = true
							}
						}
					}
				}
				return found, err
			}, duration, interval).Should(Equal(true))

			By("update env var")
			Eventually(func() bool {

				err := k8sClient.Get(ctx, types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}, createdCrd)
				if err == nil {

					createdCrd.Spec.AdminUser = "Joseph"

					err = k8sClient.Update(ctx, createdCrd)
					if err != nil {
						fmt.Printf("error on update! %v\n", err)
					}
				}
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("verify different checksum")
			Eventually(func() (bool, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
				createdSs := &appsv1.StatefulSet{}

				err := k8sClient.Get(ctx, key, createdSs)
				if err != nil {
					return false, err
				}

				found := false
				for _, container := range createdSs.Spec.Template.Spec.Containers {
					for _, env := range container.Env {
						if env.Name == "TRIGGERED_ROLL_COUNT" {
							if env.Value != checkSum {
								found = true
							}
						}
					}
				}
				return found, err
			}, duration, interval).Should(Equal(true))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("BrokerProperties", func() {
		It("Expect vol mount via config map", func() {
			By("By creating a new crd with BrokerProperties in the spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.BrokerProperties = []string{"globalMaxSize=512m"}

			crd.Spec.DeploymentPlan.Labels = make(map[string]string)
			crd.Spec.DeploymentPlan.Labels["bla"] = "bla"

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func() bool { return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) }, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("By finding a new secret map with broker props")
			brokerPropsSecret := &corev1.Secret{}
			key := types.NamespacedName{Name: crd.ObjectMeta.Name + "-props", Namespace: crd.ObjectMeta.Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, brokerPropsSecret)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("By checking the container stateful set for java opts")
			Eventually(func() (bool, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
				createdSs := &appsv1.StatefulSet{}

				err := k8sClient.Get(ctx, key, createdSs)
				if err != nil {
					return false, err
				}

				found := false
				for _, container := range createdSs.Spec.Template.Spec.Containers {
					for _, env := range container.Env {
						if env.Name == "JDK_JAVA_OPTIONS" {
							if strings.Contains(env.Value, brokerPropertiesMatchString) {
								found = true
							}
						}
					}
				}

				return found, err
			}, duration, interval).Should(Equal(true))

			By("By checking the stateful set for volume mount path")
			Eventually(func() (bool, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
				createdSs := &appsv1.StatefulSet{}

				err := k8sClient.Get(ctx, key, createdSs)
				if err != nil {
					return false, err
				}

				found := false
				for _, container := range createdSs.Spec.Template.Spec.Containers {
					for _, vm := range container.VolumeMounts {
						// mount path can't have a .
						if strings.Contains(vm.MountPath, "-props") {
							found = true
						}
					}
				}

				return found, err
			}, duration, interval).Should(Equal(true))

			By("By checking the container stateful launch set for STATEFUL_SET_ORDINAL")
			Eventually(func() (bool, error) {
				key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
				createdSs := &appsv1.StatefulSet{}

				err := k8sClient.Get(ctx, key, createdSs)
				if err != nil {
					return false, err
				}

				found := false
				for _, container := range createdSs.Spec.Template.Spec.Containers {
					for _, command := range container.Command {
						if strings.Contains(command, "STATEFUL_SET_ORDINAL") {
							found = true
						}
					}
				}

				return found, err
			}, duration, interval).Should(Equal(true))

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Expect updated secret on update to BrokerProperties", func() {
			By("By creating a crd with BrokerProperties in the spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)
			crd.Spec.BrokerProperties = []string{"globalMaxSize=64g"}

			propsResourceName := crd.Name + "-props"
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("By eventualy finding a matching secret with broker props")
			cmResourceVersion := ""

			createdPropsResource := &corev1.Secret{}
			propsResourceKey := types.NamespacedName{Name: propsResourceName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, propsResourceKey, createdPropsResource)).Should(Succeed())
				g.Expect(createdPropsResource.ResourceVersion).ShouldNot(BeNil())
				cmResourceVersion = createdPropsResource.ResourceVersion
			}, timeout, interval).Should(Succeed())

			By("updating the crd, expect updated secret")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			By("pushing the update on the current version...")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}, createdCrd)).Should(Succeed())

				// add a new property
				createdCrd.Spec.BrokerProperties = append(createdCrd.Spec.BrokerProperties, "gen="+strconv.FormatInt(createdCrd.ObjectMeta.Generation, 10))

				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("finding the updated secret")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, propsResourceKey, createdPropsResource)).Should(Succeed())
				g.Expect(createdPropsResource.ResourceVersion).ShouldNot(BeNil())

				// verify update
				g.Expect(createdPropsResource.ResourceVersion).ShouldNot(Equal(cmResourceVersion))

			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				crdRef := types.NamespacedName{
					Namespace: crd.Namespace,
					Name:      crd.Name,
				}
				g.Expect(k8sClient.Get(ctx, crdRef, createdCrd)).Should(Succeed())

				condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).Should(Equal(metav1.ConditionUnknown))
			}, timeout, interval).Should(Succeed())

			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Upgrade brokerProps respect existing immutable config map", func() {
			By("By creating a crd with BrokerProperties in the spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.BrokerProperties = []string{"globalMaxSize=64g"}

			configMapName := crd.Name + "-props"
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("By eventualy finding a matching config map with broker props")

			createdConfigMap := &corev1.Secret{}
			configMapKey := types.NamespacedName{Name: configMapName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, configMapKey, createdConfigMap)).Should(Succeed())
				g.Expect(createdConfigMap.ResourceVersion).ShouldNot(BeNil())
			}, timeout, interval).Should(Succeed())

			By("inserting immutable config map with OwnerReference to mimic deploy upgrade")
			hexShaOriginal := hexShaHashOfMap(crd.Spec.BrokerProperties)
			immutableConfigMapKey := types.NamespacedName{Name: crd.ObjectMeta.Name + "-props-" + hexShaOriginal, Namespace: crd.ObjectMeta.Namespace}

			immutableConfigMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:         immutableConfigMapKey.Name,
					GenerateName: "",
					Namespace:    immutableConfigMapKey.Namespace,
				},
				Immutable: common.NewTrue(),
				Data:      map[string]string{},
			}

			By("getting owner!")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			crdKey := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}
			Expect(k8sClient.Get(ctx, crdKey, createdCrd)).Should(Succeed())

			By("setting owner!")
			immutableConfigMap.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: brokerv1beta1.GroupVersion.String(),
				Kind:       "ActiveMQArtemis",
				Name:       createdCrd.Name,
				UID:        createdCrd.GetUID()}}

			By("Setting matching data")
			immutableConfigMap.Data["broker.properties"] = string(createdConfigMap.Data["broker.properties"])

			By("creating immutable config map")
			Expect(k8sClient.Create(ctx, immutableConfigMap)).Should(Succeed())

			Eventually(func(g Gomega) {
				By("verifying it is present before artemis reconcile")
				createdImmutableCm := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, immutableConfigMapKey, createdImmutableCm)).Should(Succeed())
				g.Expect(createdImmutableCm.ResourceVersion).ShouldNot(BeNil())
			}, timeout, interval).Should(Succeed())

			By("pushing an update to force reconcile")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).Should(Succeed())

				// no material change, just a reconcile loop
				createdCrd.Spec.Upgrades.Enabled = true

				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("not finding the mutable config map, reverted back to immutable")
			Eventually(func(g Gomega) {
				By("waiting till mutable config map is gone!")
				g.Expect(k8sClient.Get(ctx, configMapKey, createdConfigMap)).Error().ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifing immutable still present")
			Expect(k8sClient.Get(ctx, immutableConfigMapKey, immutableConfigMap)).Should(Succeed())

			// cleanup
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		It("Expect two crs to coexist", func() {
			By("By creating two crds with BrokerProperties in the spec")
			ctx := context.Background()
			crd1 := generateArtemisSpec(defaultNamespace)
			crd2 := generateArtemisSpec(defaultNamespace)

			Expect(k8sClient.Create(ctx, &crd1)).Should(Succeed())
			Expect(k8sClient.Create(ctx, &crd2)).Should(Succeed())

			By("By eventualy finding two secrets with broker props")
			secretList := &corev1.SecretList{}
			opts := &client.ListOptions{
				Namespace: defaultNamespace,
			}
			Eventually(func() int {
				err := k8sClient.List(ctx, secretList, opts)
				if err != nil {
					fmt.Printf("error getting list of configopts map! %v", err)
				}

				ret := 0
				for _, cm := range secretList.Items {
					if strings.Contains(cm.ObjectMeta.Name, "-props") {
						if strings.Contains(cm.ObjectMeta.Name, crd1.Name) || strings.Contains(cm.ObjectMeta.Name, crd2.Name) {
							ret++
						}
					}
				}
				return ret
			}, timeout, interval).Should(BeEquivalentTo(2))

			// cleanup
			Expect(k8sClient.Delete(ctx, &crd1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &crd2)).Should(Succeed())
		})

		It("expect error message on invalid property value", func() {
			By("By creating a crd with BrokerProperties in the spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.BrokerProperties = []string{"notValid=bla"}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			crdRef := types.NamespacedName{
				Namespace: crd.Namespace,
				Name:      crd.Name,
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crdRef, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())

					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))

					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).Should(ContainSubstring("bla"))
					g.Expect(condition.Message).Should(ContainSubstring("ss-0"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			// cleanup
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())

		})
	})

	Context("BrokerVersion", func() {

		It("expect version match when version is loosly specified", func() {
			By("By creating a crd with a floating version")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			latestVersion := semver.MustParse(version.GetDefaultVersion())
			crd.Spec.Version = strconv.FormatUint(latestVersion.Major, 10)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			crdRef := types.NamespacedName{
				Namespace: crd.Namespace,
				Name:      crd.Name,
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crdRef, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.BrokerVersionAlignedConditionType)
					g.Expect(condition).NotTo(BeNil())

					if verbose {
						fmt.Printf("\nSTATUS: %v\n", createdCrd.Status)
					}

					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.BrokerVersionAlignedConditionMatchReason))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			// cleanup
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})

		It("expect version match on latest version", func() {
			By("By creating a crd with latest image and version")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan.Image = version.GetDefaultKubeImage()
			crd.Spec.Version = version.GetDefaultVersion()
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			crdRef := types.NamespacedName{
				Namespace: crd.Namespace,
				Name:      crd.Name,
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crdRef, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.BrokerVersionAlignedConditionType)
					g.Expect(condition).NotTo(BeNil())

					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.BrokerVersionAlignedConditionMatchReason))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			// cleanup
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})

		It("expect error message on wrong image version", func() {
			if len(version.SupportedActiveMQArtemisVersions) < 2 {
				Skip("The supported ActiveMQ Artemis versions are less than 2")
			}

			By("By creating a crd with latest image and wong version")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan.Image = version.GetDefaultKubeImage()
			crd.Spec.Version = version.SupportedActiveMQArtemisVersions[0]
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			crdRef := types.NamespacedName{
				Namespace: crd.Namespace,
				Name:      crd.Name,
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, crdRef, createdCrd)).Should(Succeed())

					brokerVersionAlignedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.BrokerVersionAlignedConditionType)
					g.Expect(brokerVersionAlignedCondition).NotTo(BeNil())

					g.Expect(brokerVersionAlignedCondition.Status).To(Equal(metav1.ConditionUnknown))

					g.Expect(brokerVersionAlignedCondition.Reason).Should(Equal(brokerv1beta1.BrokerVersionAlignedConditionMismatchReason))
					g.Expect(brokerVersionAlignedCondition.Message).Should(ContainSubstring(crd.Spec.Version))

					readyCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)
					g.Expect(readyCondition).NotTo(BeNil())

					g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			// cleanup
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})

	})

	Context("LoggerProperties", Label("LoggerProperties-test"), func() {

		It("test validate can pick up all internal vars misusage", Label("all-misused-internal-vars"), func() {
			By("creating a cr with all internal vars used")
			fakeSecretName := "envSecret"
			crd, createdCrd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Env = []corev1.EnvVar{
					{
						Name: javaArgsAppendEnvVarName,
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: fakeSecretName,
								},
								Key: "anyKey",
							},
						},
					},
					{
						Name: javaOptsEnvVarName,
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: fakeSecretName,
								},
								Key: "anyKey1",
							},
						},
					},
					{
						Name: debugArgsEnvVarName,
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: fakeSecretName,
								},
								Key: "anyKey2",
							},
						},
					},
				}
			})
			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(crd.Name, defaultNamespace, createdCrd)).To(BeTrue())
				deployCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(deployCondition).NotTo(BeNil())
				g.Expect(deployCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(deployCondition.Reason).To(Equal(brokerv1beta1.ValidConditionInvalidInternalVarUsage))
				g.Expect(deployCondition.Message).To(ContainSubstring("Don't use valueFrom on env vars that the operator can mutate"))
				g.Expect(deployCondition.Message).To(ContainSubstring(javaArgsAppendEnvVarName))
				g.Expect(deployCondition.Message).To(ContainSubstring(javaOptsEnvVarName))
				g.Expect(deployCondition.Message).To(ContainSubstring(debugArgsEnvVarName))
			}, timeout, interval).Should(Succeed())

			By("deleting the CR")
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("validate user directly using internal env vars", Label("invalid-internal-var-usage"), func() {
			By("By creatinging a new config map for logging")
			ctx := context.Background()

			loggingConfigMapName := "my-logging-config"

			loggingData := make(map[string]string)

			loggingData[LoggingConfigKey] = "rootLogger.level=INFO"

			configMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, configMap, &client.CreateOptions{})).To(Succeed())
			}, timeout, interval).Should(Succeed())

			By("creating a secret containing the env var")
			envSecretName := "java-args-append-secret"
			secret, _ := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = envSecretName
				candidate.StringData = map[string]string{
					"anyKey": "-Dlog4j2.debug=true",
				}
			})
			By("creating a new crd having a JAVA_ARGS_APPEND directly defined in env")
			crd, createdCrd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}
				candidate.Spec.Env = []corev1.EnvVar{
					{
						Name: javaArgsAppendEnvVarName,
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: envSecretName,
								},
								Key: "anyKey",
							},
						},
					},
				}
			})

			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(crd.Name, defaultNamespace, createdCrd)).To(BeTrue())
				deployCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(deployCondition).NotTo(BeNil())
				g.Expect(deployCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(deployCondition.Reason).To(Equal(brokerv1beta1.ValidConditionInvalidInternalVarUsage))
				g.Expect(deployCondition.Message).To(ContainSubstring("Don't use valueFrom on env vars that the operator can mutate"))
				g.Expect(deployCondition.Message).To(ContainSubstring(javaArgsAppendEnvVarName))
				g.Expect(deployCondition.Message).NotTo(ContainSubstring(javaOptsEnvVarName))
				g.Expect(deployCondition.Message).NotTo(ContainSubstring(debugArgsEnvVarName))
			}, timeout, interval).Should(Succeed())

			By("deleteing the env secret")
			CleanResource(secret, envSecretName, defaultNamespace)
			By("deleting the logging configmap")
			CleanResource(configMap, loggingConfigMapName, defaultNamespace)
			By("deleting the CR")
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("custom logging not to override JAVA_ARGS_APPEND", Label("test-java-overriden"), func() {

			By("By creatinging a new config map for logging")
			ctx := context.Background()

			loggingConfigMapName := "my-logging-config"

			loggingData := make(map[string]string)

			loggingData[LoggingConfigKey] = "rootLogger.level=INFO" + "\n" +
				"rootLogger.appenderRef.console.ref=console" + "\n" +
				"appender.console.type=Console" + "\n" +
				"appender.console.name=console" + "\n" +
				"appender.console.layout.type=PatternLayout" + "\n" +
				"appender.console.layout.pattern=%-5level i-am-here [%logger] %msg%n"

			configMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, configMap, &client.CreateOptions{})).To(Succeed())
			}, timeout, interval).Should(Succeed())

			By("creating a secret containing the env var")
			envSecretName := "java-args-append-secret"
			secret, _ := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = envSecretName
				candidate.StringData = map[string]string{
					"anyKey": "-Dlog4j2.debug=true",
				}
			})
			By("creating a new crd having a JAVA_ARGS_APPEND defined in env")
			crd, createdCrd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}
				candidate.Spec.Env = []corev1.EnvVar{
					{
						Name: "ENV_FROM_X",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: envSecretName,
								},
								Key: "anyKey",
							},
						},
					},
					{
						Name:  javaArgsAppendEnvVarName,
						Value: "$(ENV_FROM_X)",
					},
				}
			})

			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(crd.Name, defaultNamespace, createdCrd)).To(BeTrue())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			By("checking JAVA_ARGS_APPEND has the right value")
			Eventually(func(g Gomega) {
				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
					command := []string{"env"}
					//the real value is expanded in the container
					result := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					hasEnv := false
					for _, line := range strings.Split(result, "\n") {
						if strings.HasPrefix(line, "JAVA_ARGS_APPEND") {
							hasEnv = true
							g.Expect(result).Should(ContainSubstring("-Dlog4j2.debug=true -Dlog4j2.configurationFile=/amq/extra/configmaps/my-logging-config/logging.properties"))
						}
					}
					g.Expect(hasEnv).To(BeTrue())

					podLogs := LogsOfPod(podWithOrdinal, crd.Name, defaultNamespace, g)
					g.Expect(podLogs).Should(ContainSubstring("i-am-here"))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deleteing the env secret")
			CleanResource(secret, envSecretName, defaultNamespace)
			By("deleting the logging configmap")
			CleanResource(configMap, loggingConfigMapName, defaultNamespace)
			By("deleting the CR")
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("logging configmap validation", func() {

			By("By creatinging a new config map with wrong key")
			ctx := context.Background()

			loggingConfigMapName := "my-logging-config"

			loggingData := make(map[string]string)
			// it requires the key to be logging.properties
			loggingData["logging-configuration"] = "someproperty=somevalue"
			configMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
			Eventually(func() bool {
				err := k8sClient.Create(ctx, configMap, &client.CreateOptions{})
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("By creating a new crd")
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func() bool { return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) }, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			Eventually(func(g Gomega) {
				g.Expect(getPersistedVersionedCrd(crd.Name, defaultNamespace, createdCrd)).To(BeTrue())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
				g.Expect(validCondition.Message).To(ContainSubstring("must have key logging.properties"))
			}, timeout, interval).Should(Succeed())

			By("deleting the logging configmap")
			CleanResource(configMap, loggingConfigMapName, defaultNamespace)
			By("deleting the CR")
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("logging secret and configmap validation", func() {

			By("By creatinging a new secret with wrong key")
			ctx := context.Background()

			loggingSecretName := "my-secret-logging-config"
			loggingData := make(map[string][]byte)
			// it requires the key to be logging.properties
			loggingData["logging-configuration"] = []byte("someproperty=somevalue")
			loggingSecretKey := types.NamespacedName{Name: loggingSecretName, Namespace: defaultNamespace}
			loggingSecret := secrets.NewSecret(loggingSecretKey, loggingData, nil)
			Eventually(func() bool {
				err := k8sClient.Create(ctx, loggingSecret, &client.CreateOptions{})
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("By creating a new crd")
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{loggingSecretName}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			crdKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			Eventually(func() bool { return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) }, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
				g.Expect(validCondition.Message).To(ContainSubstring("must have key logging.properties"))

			}, timeout, interval).Should(Succeed())

			By("still invalid, referencing two configs, secret and config map")
			loggingConfigMapName := "my-logging-config"

			loggingStringData := make(map[string]string)
			loggingStringData[LoggingConfigKey] = "someproperty=somevalue"
			loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingStringData)
			By("creating valid config map")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, loggingConfigMap, &client.CreateOptions{})).To(Succeed())
			}, timeout, interval).Should(Succeed())

			By("CR, invalid with double ref")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				createdCrd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}
				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("fixing secret, depends on retry")
			Eventually(func(g Gomega) {
				createdSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, loggingSecretKey, createdSecret)).Should(Succeed())
				createdSecret.Data[LoggingConfigKey] = []byte(`someText`)
				g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verifying validation status reflects need for single instance")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
				g.Expect(validCondition.Message).To(ContainSubstring("once"))

			}, timeout, interval).Should(Succeed())

			By("Updating CR to make valid")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				createdCrd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{}
				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verifying valid status and resource version")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))

				g.Expect(validCondition.ObservedGeneration).Should(Equal(createdCrd.Generation))

			}, timeout, interval).Should(Succeed())

			By("deleting the logging configmap")
			Expect(k8sClient.Delete(ctx, loggingSecret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, loggingConfigMap)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		It("Expect vol mount for logging configmap deployed", func() {

			By("By creatinging a new config map with logging props")
			ctx := context.Background()

			loggingConfigMapName := "my-logging-config"

			loggingData := make(map[string]string)
			loggingData[LoggingConfigKey] = "someproperty=somevalue"
			configMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
			Eventually(func() bool {
				err := k8sClient.Create(ctx, configMap, &client.CreateOptions{})
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("By creating a new crd")
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func() bool { return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) }, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			//retrieve ss
			key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, key, createdSs)).To(Succeed())

				brokerContainer := createdSs.Spec.Template.Spec.Containers[0]
				loggingPropName := javaArgsAppendEnvVarName
				loggingPropValue := ""
				expectedLoggingPropValue := "-Dlog4j2.configurationFile=/amq/extra/configmaps/my-logging-config/logging.properties"
				for _, env := range brokerContainer.Env {
					if env.Name == loggingPropName {
						loggingPropValue = env.Value
					}
				}
				g.Expect(strings.HasSuffix(loggingPropValue, expectedLoggingPropValue)).To(BeTrue())

				mountPathFound := false
				for _, mount := range brokerContainer.VolumeMounts {
					if mount.MountPath == "/amq/extra/configmaps/my-logging-config" {
						mountPathFound = true
						break
					}
				}
				g.Expect(mountPathFound).To(BeTrue())

				volumeFound := false
				for _, vol := range createdSs.Spec.Template.Spec.Volumes {
					if vol.Name == "configmap-my-logging-config" {
						volumeFound = true
						break
					}
				}
				g.Expect(volumeFound).To(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("cleanup")
			CleanResource(configMap, loggingConfigMapName, defaultNamespace)
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})

		It("Expect vol mount for logging secret deployed", func() {

			By("By creatinging a new secret with logging props")
			ctx := context.Background()

			loggingSecretName := "my-secret-logging-config"

			loggingData := make(map[string][]byte)
			loggingData[LoggingConfigKey] = []byte("someproperty=somevalue")
			secret := secrets.NewSecret(types.NamespacedName{Name: loggingSecretName, Namespace: defaultNamespace}, loggingData, nil)
			Eventually(func() bool {
				err := k8sClient.Create(ctx, secret, &client.CreateOptions{})
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("By creating a new crd")
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{loggingSecretName}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func() bool { return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd) }, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			//retrieve ss
			key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, key, createdSs)).To(Succeed())

				brokerContainer := createdSs.Spec.Template.Spec.Containers[0]
				loggingPropName := javaArgsAppendEnvVarName
				loggingPropValue := ""
				expectedLoggingPropValue := "-Dlog4j2.configurationFile=/amq/extra/secrets/my-secret-logging-config/logging.properties"
				for _, env := range brokerContainer.Env {
					if env.Name == loggingPropName {
						loggingPropValue = env.Value
					}
				}
				g.Expect(strings.HasSuffix(loggingPropValue, expectedLoggingPropValue)).To(BeTrue())

				mountPathFound := false
				for _, mount := range brokerContainer.VolumeMounts {
					if mount.MountPath == "/amq/extra/secrets/my-secret-logging-config" {
						mountPathFound = true
						break
					}
				}
				g.Expect(mountPathFound).To(BeTrue())

				volumeFound := false
				for _, vol := range createdSs.Spec.Template.Spec.Volumes {
					if vol.Name == "secret-my-secret-logging-config" {
						volumeFound = true
						break
					}
				}
				g.Expect(volumeFound).To(BeTrue())

			}, timeout, interval).Should(Succeed())

			By("cleanup")
			CleanResource(secret, loggingSecretName, defaultNamespace)
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("With address settings via updated cr", func() {
		It("Expect ok deploy", func() {
			By("By creating a crd without address spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("updating the crd with address settings")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			crdKey := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}
			Eventually(func(g Gomega) {
				By("Verifying generation")
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).Should(Succeed())
				g.Expect(createdCrd.Generation).Should(BeNumerically("==", 1))
			}, timeout, interval).Should(Succeed())

			By("pushing the update on the current version...")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).Should(Succeed())

				// add a new property
				createdCrd.Spec.BrokerProperties = append(createdCrd.Spec.BrokerProperties, "gen="+strconv.FormatInt(createdCrd.ObjectMeta.Generation, 10))

				// add address settings, to an existing crd
				ma := "merge_all"
				dlq := "dlq"
				dlqabc := "dlqabc"
				maxSize := "10m"

				createdCrd.Spec.AddressSettings = brokerv1beta1.AddressSettingsType{
					ApplyRule: &ma,
					AddressSetting: []brokerv1beta1.AddressSettingType{
						{
							Match:             "#",
							DeadLetterAddress: &dlq,
						},
						{
							Match:             "abc#",
							DeadLetterAddress: &dlqabc,
							MaxSizeBytes:      &maxSize,
						},
					},
				}

				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("Verifying generation")
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).Should(Succeed())
				g.Expect(createdCrd.Generation).Should(BeNumerically("==", 2))
			}, timeout, interval).Should(Succeed())

			By("tracking the yaconfig init command with user_address_settings and verifying no change on further update")
			key := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
			createdSs := &appsv1.StatefulSet{}
			var initArgsString string
			Eventually(func(g Gomega) {

				createdSs := &appsv1.StatefulSet{}

				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				initArgsString = strings.Join(createdSs.Spec.Template.Spec.InitContainers[0].Args, ",")
				g.Expect(initArgsString).Should(ContainSubstring("user_address_settings"))

			}, timeout, interval).Should(Succeed())

			By("pushing another update on the current version...")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).Should(Succeed())

				createdCrd.Spec.BrokerProperties = append(createdCrd.Spec.BrokerProperties, "gen2="+strconv.FormatInt(createdCrd.ObjectMeta.Generation, 10))

				// add address settings, to an existing crd
				ma := "merge_all"
				dlq := "dlq"
				dlqabc := "dlqabc"
				maxSize := "10m"

				createdCrd.Spec.AddressSettings = brokerv1beta1.AddressSettingsType{
					ApplyRule: &ma,
					AddressSetting: []brokerv1beta1.AddressSettingType{
						{
							Match:             "#",
							DeadLetterAddress: &dlq,
						},
						{
							Match:             "abc#",
							DeadLetterAddress: &dlqabc,
							MaxSizeBytes:      &maxSize,
						},
					},
				}

				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("Verifying generation")
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}, createdCrd)).Should(Succeed())
				g.Expect(createdCrd.Generation).Should(BeNumerically("==", 3))
			}, timeout, interval).Should(Succeed())

			By("verifying init command args did not change")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
				g.Expect(strings.Join(createdSs.Spec.Template.Spec.InitContainers[0].Args, ",")).Should(Equal(initArgsString))
			}, timeout, interval).Should(Succeed())

			// cleanup
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("With address cr", func() {
		It("Expect ok deploy", func() {
			By("By creating a crd without  address spec")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			Eventually(func() bool {
				return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
			}, timeout, interval).Should(BeTrue())
			Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

			By("By deploying address cr for this namespace but not for this CR")

			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
			addressCrd.SetName("a1")
			addressCrd.SetNamespace(defaultNamespace)
			addressCrd.Spec.AddressName = "a1"
			routingTypeShouldBeOptional := "MULTICAST"
			addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional
			addressCrd.Spec.ApplyToCrNames = []string{"bong"}

			Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

			By("Checking stopped status of CR")
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(ctx, key, createdCrd)).Should(Succeed())

				g.Expect(len(createdCrd.Status.PodStatus.Stopped)).Should(Equal(1))
				g.Expect(common.IsConditionPresentAndEqual(createdCrd.Status.Conditions, metav1.Condition{
					Type:    brokerv1beta1.DeployedConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.DeployedConditionZeroSizeReason,
					Message: common.DeployedConditionZeroSizeMessage,
				})).Should(BeTrue())
				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).Should(Equal(metav1.ConditionUnknown))

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("adding an address via update")
			var updatedVersion = "bla"
			key := types.NamespacedName{Name: addressCrd.ObjectMeta.Name, Namespace: defaultNamespace}
			createdAddressCr := &brokerv1beta1.ActiveMQArtemisAddress{}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, key, createdAddressCr)).Should(Succeed())

				// ensure a1 applies to this cr
				createdAddressCr.Spec.ApplyToCrNames = append(createdAddressCr.Spec.ApplyToCrNames, createdCrd.Name)
				updatedVersion = createdAddressCr.ResourceVersion
				g.Expect(k8sClient.Update(ctx, createdAddressCr)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			// cr gets rconciled, but nothing done yet till pods create

			Eventually(func(g Gomega) {
				By("verify update of resource version: " + updatedVersion)
				g.Expect(k8sClient.Get(ctx, key, createdAddressCr)).Should(Succeed())

				g.Expect(createdAddressCr.ResourceVersion).ShouldNot(Equal(updatedVersion))

			}, timeout, interval).Should(Succeed())

			// cleanup
			CleanResource(createdCrd, createdCrd.Name, createdCrd.Namespace)
			CleanResource(createdAddressCr, createdAddressCr.Name, createdAddressCr.Namespace)
		})
	})

	Context("With toggle persistence=true", func() {
		It("Expect ok redeploy", func() {
			By("By creating a crd without persistence")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan.PersistenceEnabled = false

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("By eventualy finding a matching config map with broker props")
			createdSs := &appsv1.StatefulSet{}

			By("Making sure that the ss gets deployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				return err == nil
			}, timeout, interval).Should(Equal(true))

			initialVersion := createdSs.ObjectMeta.ResourceVersion

			By("updating the crd for persistence")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			Eventually(func() bool {

				err := k8sClient.Get(ctx, types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}, createdCrd)
				if err == nil {

					createdCrd.Spec.DeploymentPlan.PersistenceEnabled = true
					err = k8sClient.Update(ctx, createdCrd)
					if err != nil {
						fmt.Printf("error on update! %v\n", err)
					}
				}
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("Making sure that the ss gets redeployed " + crd.ObjectMeta.Name)
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

				err := k8sClient.Get(ctx, key, createdSs)

				if err == nil {
					// verify persisted with a new revision, update would have failed
					return createdSs.ObjectMeta.ResourceVersion > "0" && initialVersion != createdSs.ObjectMeta.ResourceVersion
				}
				return err == nil
			}, timeout, interval).Should(Equal(true))

			// cleanup
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("With update persistence=true single reconcile", func() {
		It("check reconcole loop", func() {
			By("By creating a crd persistence")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan.PersistenceEnabled = true
			if !isOpenshift {
				crd.Spec.IngressDomain = defaultTestIngressDomain
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerKey := types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("wait for ready indication")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("\nSTATUS: %v\n", createdCrd.Status)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				createdSs := &appsv1.StatefulSet{}
				By("finding ss resource version")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
					g.Expect(createdSs.ObjectMeta.ResourceVersion).NotTo(Equal(""))
				}, timeout, interval).Should(Succeed())

				initialVersion := createdSs.ObjectMeta.ResourceVersion

				if os.Getenv("DEPLOY_OPERATOR") == "false" {

					By("start to capture test log needs local operator")
					StartCapturingLog()
					defer StopCapturingLog()

					By("updating the crd to expose console - verify no ss mod")
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
						createdCrd.Spec.Console.Expose = true
						createdCrd.Spec.Console.ExposeMode = &brokerv1beta1.ExposeModes.Ingress
						g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
					}, timeout, interval).Should(Succeed())

					By("check ingress is created for console")
					ingKey := types.NamespacedName{
						Name:      createdCrd.Name + "-wconsj-0-svc-ing",
						Namespace: defaultNamespace,
					}
					ingress := netv1.Ingress{}
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, ingKey, &ingress)).To(Succeed())
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("wait for ready indication")
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
						if verbose {
							fmt.Printf("\nSTATUS: %v\n", createdCrd.Status)
						}
						g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
						g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("Making sure that the ss does not get redeployed " + crd.ObjectMeta.Name)
					Eventually(func(g Gomega) {
						key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
						g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
						g.Expect(initialVersion).To(Equal(createdSs.ObjectMeta.ResourceVersion))
					}, timeout, interval).Should(Succeed())

					By("finding no Updating v1.StatefulSet ")
					matches, err := FindAllInCapturingLog(`Updating \*v1.StatefulSet`)
					Expect(err).To(BeNil())
					Expect(len(matches)).To(Equal(0))
				}

			}

			// cleanup
			CleanResource(&crd, crd.Name, defaultNamespace)
		})
	})

	Context("Toggle spec.Version", func() {
		It("Expect ok update and new SS generation", func() {

			// the default/latest is applied when there is no env, which is normally the case
			// for these tests.
			// To verify upgrade, we want to deploy the previous version and provide the
			// matching image via the env vars

			// lets order and get LatestVersion - x
			orderedSemVersions := version.SupportedActiveMQArtemisSemanticVersions()
			if len(orderedSemVersions) < 4 {
				Skip("The supported ActiveMQ Artemis versions are less than 4")
			}
			previousVersion := orderedSemVersions[len(orderedSemVersions)-4]
			Expect(previousVersion.String()).ShouldNot(Equal(version.GetDefaultVersion()))

			previousCompactVersion := version.CompactActiveMQArtemisVersion(previousVersion.String())
			Expect(previousCompactVersion).ShouldNot(Equal(version.GetDefaultCompactVersion()))

			previousImageEnvVar := common.ImageNamePrefix + "KUBERNETES_" + previousCompactVersion
			previousImageEnvValue, err := getOperatorEnv(previousImageEnvVar)
			Expect(err).To(BeNil())
			os.Setenv(previousImageEnvVar, previousImageEnvValue)
			defer os.Unsetenv(previousImageEnvVar)

			perviousInitImageEnvVar := common.ImageNamePrefix + "INIT_" + previousCompactVersion
			perviousInitImageEnvValue, err := getOperatorEnv(perviousInitImageEnvVar)
			Expect(err).To(BeNil())
			os.Setenv(perviousInitImageEnvVar, perviousInitImageEnvValue)
			defer os.Unsetenv(perviousInitImageEnvVar)

			By("By creating a crd without persistence")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan.LivenessProbe = &corev1.Probe{
				InitialDelaySeconds: 2,
				PeriodSeconds:       5,
			}
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 2,
				PeriodSeconds:       5,
			}
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			crd.Spec.Version = previousVersion.String()

			// the broker init images before 2.32.0 has root user
			// that doesn't work in namespaces with the restricted policy
			crd.Spec.DeploymentPlan.PodSecurityContext = &corev1.PodSecurityContext{
				RunAsUser:      ptr.To(defaultUid),
				RunAsNonRoot:   ptr.To(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("By eventualy finding a matching config map with broker props")
			createdSs := &appsv1.StatefulSet{}

			key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

			By("Making sure that the ss gets deployed")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
				g.Expect(createdSs.ResourceVersion).ShouldNot(BeNil())
				g.Expect(createdSs.Generation).Should(BeEquivalentTo(1))

			}, timeout, interval).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Checking ready on SS")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
					g.Expect(createdSs.Status.ReadyReplicas).Should(BeEquivalentTo(2))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			initialGeneration := createdSs.ObjectMeta.Generation

			By("updating the crd spec.version")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: crd.ObjectMeta.Name, Namespace: crd.ObjectMeta.Namespace}, createdCrd)).Should(Succeed())

				createdCrd.Spec.Version = version.GetDefaultVersion()
				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("Making sure that the ss does not get redeployed " + crd.ObjectMeta.Name)

			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					g.Expect(createdSs.Status.ReadyReplicas).Should(BeEquivalentTo(2))
					g.Expect(createdSs.Status.CurrentReplicas).Should(BeEquivalentTo(2))
					g.Expect(createdSs.Status.UpdatedReplicas).Should(BeEquivalentTo(2))
				}

				By("verify new generation: " + string(rune(createdSs.ObjectMeta.Generation)))
				g.Expect(createdSs.ObjectMeta.Generation).ShouldNot(BeEquivalentTo(initialGeneration))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// cleanup
			CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		})
	})

	Context("time to ready", func() {
		It("expect ok ready is fast", func() {

			By("By creating a crd without persistence")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       2,
			}

			// about 10s to 1, 30s for 5 on minikube
			deploymentSize := 1
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(int32(deploymentSize))
			crd.Spec.DeploymentPlan.PersistenceEnabled = true

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdSs := &appsv1.StatefulSet{}
			key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Checking ready on SS")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())
					g.Expect(createdSs.Status.ReadyReplicas).Should(BeEquivalentTo(deploymentSize))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verifying started via Status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					if verbose {
						fmt.Printf("\nSTATUS: %v\n", createdCrd.Status)
					}
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(deploymentSize))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}
			// cleanup
			CleanResource(&crd, crd.Name, defaultNamespace)
		})
	})

	Context("template tests", func() {
		It("expect custom service", func() {

			By("By creating a crd with template")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.PersistenceEnabled = true

			var serviceKind = "Service"
			var ssKind = "StatefulSet"

			crd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
				{
					Selector:    &brokerv1beta1.ResourceSelector{Kind: &serviceKind},
					Annotations: map[string]string{"someKey": "someValue"},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind": "Service",
							"spec": map[string]interface{}{
								"publishNotReadyAddresses": false,
							},
						},
					}),
				},
				{
					Selector:    &brokerv1beta1.ResourceSelector{Kind: &ssKind},
					Annotations: map[string]string{"someSsKey": "someSsValue"},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind": "StatefulSet",
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{
										"containers": []interface{}{
											map[string]interface{}{
												"name": "$(CR_NAME)-container", // merge on name key
												"securityContext": map[string]interface{}{
													"runAsNonRoot": true,
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

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verifying started via Status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					//if verbose {
					fmt.Printf("\nSTATUS: %v\n", createdCrd.Status)
					//}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking customied -hdls-svc")
				createdService := &corev1.Service{}
				serviceKey := types.NamespacedName{Name: crd.Name + "-hdls-svc", Namespace: crd.Namespace}

				By("verifying started via Status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
					g.Expect(createdService.Spec.PublishNotReadyAddresses).Should(BeFalse())
					g.Expect(createdService.Annotations["someKey"]).Should(Equal("someValue"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				createdSs := &appsv1.StatefulSet{}
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

				By("Making sure that the ss gets customised")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, key, createdSs)).Should(Succeed())

					g.Expect(createdSs.Annotations["someSsKey"]).Should(Equal("someSsValue"))
					g.Expect(*createdSs.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
			// cleanup
			CleanResource(&crd, crd.Name, defaultNamespace)
		})

		It("expect Non fatal condition for unmatched resource templates", func() {

			By("By creating a crd with unmatched templates")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.PersistenceEnabled = true

			var serviceKind = "Service"
			var ssKind = "StatefulSet"
			var nonExistentKind = "NonExistentKind"

			crd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
				{
					Selector:    &brokerv1beta1.ResourceSelector{Kind: &nonExistentKind},
					Annotations: map[string]string{"should-not-apply": "template0"},
				},
				{
					Selector:    &brokerv1beta1.ResourceSelector{Kind: &serviceKind},
					Annotations: map[string]string{"someKey": "someValue"},
				},
				{
					Selector:    &brokerv1beta1.ResourceSelector{Kind: &ssKind},
					Annotations: map[string]string{"someSsKey": "someSsValue"},
				},
				{
					Selector:    &brokerv1beta1.ResourceSelector{Name: ptr.To("no-such-resource.*")},
					Annotations: map[string]string{"should-not-apply": "template3"},
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verifying deployment succeeds despite unmatched templates")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying matched templates were applied")
				createdService := &corev1.Service{}
				serviceKey := types.NamespacedName{Name: crd.Name + "-hdls-svc", Namespace: crd.Namespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
					g.Expect(createdService.Annotations["someKey"]).Should(Equal("someValue"))
					g.Expect(createdService.Annotations["should-not-apply"]).Should(BeEmpty())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				createdSs := &appsv1.StatefulSet{}
				ssKey := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
					g.Expect(createdSs.Annotations["someSsKey"]).Should(Equal("someSsValue"))
					g.Expect(createdSs.Annotations["should-not-apply"]).Should(BeEmpty())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying UnmatchedResourceTemplate condition is set with correct indices")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionUnknownReason))
					g.Expect(condition.Message).To(ContainSubstring("0"))
					g.Expect(condition.Message).NotTo(ContainSubstring("3"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("removing unmatched templates")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					createdCrd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
						{
							Selector:    &brokerv1beta1.ResourceSelector{Kind: &serviceKind},
							Annotations: map[string]string{"someKey": "someValue"},
						},
						{
							Selector:    &brokerv1beta1.ResourceSelector{Kind: &ssKind},
							Annotations: map[string]string{"someSsKey": "someSsValue"},
						},
					}
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying non fatal condition is updated to true")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					g.Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionSuccessReason))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			CleanResource(&crd, crd.Name, defaultNamespace)
		})

		testApplyingPatchWithincompatibleStructureToStatefulSet := func(invalidValue string) {

			By("By creating a crd with template")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
				{
					Selector: &brokerv1beta1.ResourceSelector{Kind: ptr.To("StatefulSet")},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind": "StatefulSet",
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{
										// invalid value for containers, it should be a slice
										"containers": invalidValue,
									},
								},
							},
						},
					}),
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

			By("verifying deployment error via Status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				deployedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)
				g.Expect(deployedCondition).NotTo(BeNil())
				g.Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Reason).To(Equal(brokerv1beta1.DeployedConditionCrudKindErrorReason))
				g.Expect(deployedCondition.Message).To(ContainSubstring("error applying strategic merge patch"))
				g.Expect(deployedCondition.Message).To(ContainSubstring("cannot restore slice from string"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(&crd, crd.Name, defaultNamespace)
		}

		It("expect error applying strategic merge patch with incompatible structure to StatefulSet", func() {
			testApplyingPatchWithincompatibleStructureToStatefulSet("INVALID_VALUE")
		})

		It("expect error applying strategic merge patch with variable incompatible structure to StatefulSet", func() {
			testApplyingPatchWithincompatibleStructureToStatefulSet("INVALID_VALUE_$(CR_NAME)")
		})

		testCreatingPatchedStatefulSetWithInvalidFieldValue := func(invalidValue string,
			getExpectedInvalidValue func(*brokerv1beta1.ActiveMQArtemis) string) {

			By("By creating a crd with template")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
				{
					Selector: &brokerv1beta1.ResourceSelector{Kind: ptr.To("StatefulSet")},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind": "StatefulSet",
							"spec": map[string]interface{}{
								// invalid value for podManagementPolicy, allowed values are "OrderedReady" or "Parallel"
								"podManagementPolicy": invalidValue,
							},
						},
					}),
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

			By("verifying deployment error via Status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				deployedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)
				g.Expect(deployedCondition).NotTo(BeNil())
				g.Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Reason).To(Equal(brokerv1beta1.DeployedConditionCrudKindErrorReason))
				g.Expect(deployedCondition.Message).To(ContainSubstring("failed to create new *v1.StatefulSet"))
				g.Expect(deployedCondition.Message).To(ContainSubstring(getExpectedInvalidValue(&crd)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(&crd, crd.Name, defaultNamespace)
		}

		It("expect error creating patched StatefulSet with invalid field value", func() {
			testCreatingPatchedStatefulSetWithInvalidFieldValue("INVALID_VALUE",
				func(crd *brokerv1beta1.ActiveMQArtemis) string {
					return "INVALID_VALUE"
				})
		})

		It("expect error creating patched StatefulSet with variable invalid field value", func() {
			testCreatingPatchedStatefulSetWithInvalidFieldValue("INVALID_VALUE_$(CR_NAME)",
				func(crd *brokerv1beta1.ActiveMQArtemis) string {
					return "INVALID_VALUE_" + crd.Name
				})
		})
	})

	Context("With deployed controller - acceptor", func() {

		It("Add acceptor via update", func() {
			By("By creating a new crd with no acceptor")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			acceptorName := "added-acceptor"
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verifying started")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					By("Updating .... add acceptor")

					g.Expect(getPersistedVersionedCrd(brokerKey.Name, brokerKey.Namespace, createdCrd)).Should(BeTrue())

					createdCrd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
						{
							Name: acceptorName,
							Port: 61666,
						},
					}

					By("Redeploying the CRD")
					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(createdCrd.Generation).Should(BeNumerically("==", 2))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying content of broker.xml props")
				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command := []string{"cat", "amq-broker/etc/broker.xml"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					if verbose {
						fmt.Printf("\na1 - cat:\n%s", stdOutContent)
					}
					g.Expect(stdOutContent).Should(ContainSubstring(acceptorName))
					ConfigAppliedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition).NotTo(BeNil())
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			CleanResource(&crd, crd.Name, defaultNamespace)
		})

		It("Checking acceptor service and route/ingress while toggle expose", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("By creating a new crd")
				ctx := context.Background()
				crd := generateArtemisSpec(defaultNamespace)

				crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
					Size: common.Int32ToPtr(1),
				}
				crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:   "new-acceptor",
						Port:   61616,
						Expose: false,
					},
				}
				crd.Spec.Connectors = []brokerv1beta1.ConnectorType{
					{
						Name:   "new-connector",
						Port:   61616,
						Expose: false,
					},
				}
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				Eventually(func() bool {
					key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
					err := k8sClient.Get(ctx, key, &crd)
					return err == nil
				}, timeout, interval).Should(BeTrue())

				Eventually(func() bool {
					key := types.NamespacedName{Name: crd.Name + "-" + "new-acceptor-0-svc", Namespace: defaultNamespace}
					acceptorService := &corev1.Service{}
					err := k8sClient.Get(context.Background(), key, acceptorService)
					return err == nil
				}, timeout, interval).Should(BeTrue())

				Eventually(func() bool {
					key := types.NamespacedName{Name: crd.Name + "-" + "new-connector-0-svc", Namespace: defaultNamespace}
					connectorService := &corev1.Service{}
					err := k8sClient.Get(context.Background(), key, connectorService)
					return err == nil
				}, timeout, interval).Should(BeTrue())

				By("updating with expose=true")
				Eventually(func() bool {
					key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
					err := k8sClient.Get(ctx, key, &crd)
					if err == nil {
						crd.Spec.Acceptors[0].Expose = true
						crd.Spec.Connectors[0].Expose = true

						if !isOpenshift {
							crd.Spec.IngressDomain = defaultTestIngressDomain
						}

						err = k8sClient.Update(ctx, &crd)
					}
					return err == nil
				}, timeout, interval).Should(BeTrue())

				var acceptorExposureKindGeneration int64
				var connectorExposureKindGeneration int64
				var acceptorKey types.NamespacedName
				var connectorKey types.NamespacedName

				if isOpenshift {

					acceptorKey = types.NamespacedName{Name: crd.Name + "-" + "new-acceptor-0-svc-rte", Namespace: defaultNamespace}
					connectorKey = types.NamespacedName{Name: crd.Name + "-" + "new-connector-0-svc-rte", Namespace: defaultNamespace}

					Eventually(func(g Gomega) {
						exposure := &routev1.Route{}
						g.Expect(k8sClient.Get(context.Background(), acceptorKey, exposure)).Should(Succeed())
						g.Expect(exposure.ResourceVersion).ShouldNot(BeEmpty())
						acceptorExposureKindGeneration = exposure.Generation
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					Eventually(func(g Gomega) {
						exposure := &routev1.Route{}
						g.Expect(k8sClient.Get(context.Background(), connectorKey, exposure)).Should(Succeed())
						g.Expect(exposure.ResourceVersion).ShouldNot(BeEmpty())
						connectorExposureKindGeneration = exposure.Generation
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				} else {

					acceptorKey = types.NamespacedName{Name: crd.Name + "-" + "new-acceptor-0-svc-ing", Namespace: defaultNamespace}
					connectorKey = types.NamespacedName{Name: crd.Name + "-" + "new-connector-0-svc-ing", Namespace: defaultNamespace}

					Eventually(func(g Gomega) {
						exposure := &netv1.Ingress{}
						g.Expect(k8sClient.Get(context.Background(), acceptorKey, exposure)).Should(Succeed())
						g.Expect(exposure.ResourceVersion).ShouldNot(BeEmpty())
						acceptorExposureKindGeneration = exposure.Generation
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					Eventually(func(g Gomega) {
						exposure := &netv1.Ingress{}
						g.Expect(k8sClient.Get(context.Background(), connectorKey, exposure)).Should(Succeed())
						g.Expect(exposure.ResourceVersion).ShouldNot(BeEmpty())
						connectorExposureKindGeneration = exposure.Generation
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("updating with expose=true and tls")
				sslSecretName := crd.Name + "-" + crd.Spec.Acceptors[0].Name + "-secret"
				sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", nil)
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())

				sslSecretName1 := crd.Name + "-" + crd.Spec.Connectors[0].Name + "-secret"
				sslSecret1, err := CreateTlsSecret(sslSecretName1, defaultNamespace, "password", nil)
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, sslSecret1)).Should(Succeed())

				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
					g.Expect(k8sClient.Get(ctx, key, &crd)).Should(Succeed())

					crd.Spec.Acceptors[0].SSLEnabled = true
					crd.Spec.Connectors[0].SSLEnabled = true

					g.Expect(k8sClient.Update(ctx, &crd)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify reconcile - new generation of ingress or route")

				if isOpenshift {

					// type metadata and generation not visible to this kube client for Route
					//g.Expect(exposure.Generation).ShouldNot(Equal(acceptorExposureKindGeneration))

					Eventually(func(g Gomega) {
						exposure := &routev1.Route{}
						g.Expect(k8sClient.Get(context.Background(), acceptorKey, exposure)).Should(Succeed())

						g.Expect(exposure.Spec.TLS).ShouldNot(BeNil())
						g.Expect(exposure.Spec.TLS.Termination).Should(Equal(routev1.TLSTerminationPassthrough))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					Eventually(func(g Gomega) {
						exposure := &routev1.Route{}
						g.Expect(k8sClient.Get(context.Background(), connectorKey, exposure)).Should(Succeed())
						g.Expect(exposure.Spec.TLS).ShouldNot(BeNil())
						g.Expect(exposure.Spec.TLS.Termination).Should(Equal(routev1.TLSTerminationPassthrough))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				} else {

					Eventually(func(g Gomega) {
						exposure := &netv1.Ingress{}
						g.Expect(k8sClient.Get(context.Background(), acceptorKey, exposure)).Should(Succeed())
						g.Expect(exposure.Generation).ShouldNot(Equal(acceptorExposureKindGeneration))
						g.Expect(exposure.Spec.TLS).ShouldNot(BeNil())

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					Eventually(func(g Gomega) {
						exposure := &netv1.Ingress{}
						g.Expect(k8sClient.Get(context.Background(), connectorKey, exposure)).Should(Succeed())
						g.Expect(exposure.Generation).ShouldNot(Equal(connectorExposureKindGeneration))
						g.Expect(exposure.Spec.TLS).ShouldNot(BeNil())

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(sslSecret, sslSecret.Name, defaultNamespace)
				CleanResource(sslSecret1, sslSecret1.Name, defaultNamespace)
			}
		})
	})

	Context("acceptor/connector update no owner ref", func() {
		// an eariler operator would not set an owner ref on services and we can clash
		// on create when trying to create a service with the same name..
		// verify that we find and fix the owner ref before reconcile

		It("with existing acceptor service", func() {
			By("by creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			acceptorName := "existing"
			crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name: acceptorName,
					Port: 61665,
				},
			}

			By("deploying an existing acceptor service w/o owner ref")

			var dudPort = int32(23)
			serviceKey := types.NamespacedName{Name: crd.Name + "-" + acceptorName + "-0-svc", Namespace: defaultNamespace}
			existingServiceWithOutOwnerRef := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceKey.Name, Namespace: serviceKey.Namespace},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Port: dudPort}},
				},
			}

			Expect(k8sClient.Create(ctx, &existingServiceWithOutOwnerRef)).Should(Succeed())
			retrievedService := corev1.Service{}
			By("ensure it is present before the artemis cr")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
				g.Expect(retrievedService.ResourceVersion).Should(Not(BeEmpty()))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verify service still exists and is updated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
				g.Expect(len(retrievedService.GetOwnerReferences())).Should(BeEquivalentTo(1))
				g.Expect(*retrievedService.GetOwnerReferences()[0].Controller).Should(BeTrue())
				g.Expect(retrievedService.GetOwnerReferences()[0].BlockOwnerDeletion).Should(BeNil())
				g.Expect(retrievedService.Spec.Ports[0].Port).Should(Not(BeEquivalentTo(dudPort)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("verify service gets owned and deleted")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Not(Succeed()))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		})

		It("with existing connector service", func() {
			By("by creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			connectorName := "existing-connector"
			crd.Spec.Connectors = []brokerv1beta1.ConnectorType{
				{
					Name: connectorName,
					Port: 62665,
				},
			}

			By("deploying an existing connector service w/o owner ref")

			var dudPort = int32(24)
			serviceKey := types.NamespacedName{Name: crd.Name + "-" + connectorName + "-0-svc", Namespace: defaultNamespace}
			existingServiceWithOutOwnerRef := corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: serviceKey.Name, Namespace: serviceKey.Namespace},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Port: dudPort}},
				},
			}

			Expect(k8sClient.Create(ctx, &existingServiceWithOutOwnerRef)).Should(Succeed())
			retrievedService := corev1.Service{}
			By("ensure it is present before the artemis cr")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
				g.Expect(retrievedService.ResourceVersion).Should(Not(BeEmpty()))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verify service still exists and is updated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Succeed())
				g.Expect(len(retrievedService.GetOwnerReferences())).Should(BeEquivalentTo(1))
				g.Expect(*retrievedService.GetOwnerReferences()[0].Controller).Should(BeTrue())
				g.Expect(retrievedService.GetOwnerReferences()[0].BlockOwnerDeletion).Should(BeNil())
				g.Expect(retrievedService.Spec.Ports[0].Port).Should(Not(BeEquivalentTo(dudPort)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("verify service gets owned and deleted")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, &retrievedService)).Should(Not(Succeed()))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		})
	})

	Context("With deployed controller", func() {
		It("Testing acceptor bindToAllInterfaces default", func() {
			By("By creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name: "new-acceptor",
					Port: 61666,
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			Eventually(func() bool {
				key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
				err := k8sClient.Get(ctx, key, &crd)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				g.Expect(len(currentStatefulSet.Spec.Template.Spec.InitContainers)).Should(BeEquivalentTo(1))
				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]

				//check AMQ_ACCEPTORS value
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}
						secret, err := secrets.RetriveSecret(namespaceName, make(map[string]string), k8sClient)
						g.Expect(err).To(BeNil())
						data := secret.Data[envVar.ValueFrom.SecretKeyRef.Key]
						//the value is a string of acceptors in xml format:
						//<acceptor name="new-acceptor">...</acceptor><another one>...
						//we need to locate our target acceptor and do the check
						//we use the port as a clue
						g.Expect(strings.Contains(string(data), "ACCEPTOR_IP:61666")).To(BeTrue())
					}
				}
			}, timeout, interval).Should(Succeed())

			CleanResource(&crd, crd.Name, defaultNamespace)
		})

		It("Testing acceptor bindToAllInterfaces being false", func() {
			By("By creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			bindTo := false
			crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:                "new-acceptor",
					Port:                61666,
					BindToAllInterfaces: &bindTo,
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			Eventually(func() bool {
				key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
				err := k8sClient.Get(ctx, key, &crd)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				g.Expect(len(currentStatefulSet.Spec.Template.Spec.InitContainers)).Should(BeEquivalentTo(1))
				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]

				//check AMQ_ACCEPTORS value
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}
						secret, err := secrets.RetriveSecret(namespaceName, make(map[string]string), k8sClient)
						g.Expect(err).To(BeNil())
						data := secret.Data[envVar.ValueFrom.SecretKeyRef.Key]
						//the value is a string of acceptors in xml format:
						//<acceptor name="new-acceptor">...</acceptor><another one>...
						//we need to locate our target acceptor and do the check
						//we use the port as a clue
						g.Expect(strings.Contains(string(data), "ACCEPTOR_IP:61666")).To(BeTrue())
					}
				}
			}, timeout, interval).Should(Succeed())

			CleanResource(&crd, crd.Name, defaultNamespace)
		})

		It("Testing acceptor bindToAllInterfaces being true", func() {
			By("By creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			bindToAll := true
			notbindToAll := false
			crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:                "new-acceptor",
					Port:                61666,
					BindToAllInterfaces: &bindToAll,
				},
				{
					Name: "new-acceptor-1",
					Port: 61777,
				},
				{
					Name:                "new-acceptor-2",
					Port:                61888,
					BindToAllInterfaces: &notbindToAll,
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			Eventually(func() bool {
				key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
				err := k8sClient.Get(ctx, key, &crd)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}

			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				g.Expect(len(currentStatefulSet.Spec.Template.Spec.InitContainers)).Should(BeEquivalentTo(1))
				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]
				//check AMQ_ACCEPTORS value
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}
						secret, err := secrets.RetriveSecret(namespaceName, make(map[string]string), k8sClient)
						g.Expect(err).To(BeNil())
						data := secret.Data[envVar.ValueFrom.SecretKeyRef.Key]
						//the value is a string of acceptors in xml format:
						//<acceptor name="new-acceptor">...</acceptor><another one>...
						//we need to locate our target acceptor and do the check
						//we use the port as a clue
						g.Expect(strings.Contains(string(data), "0.0.0.0:61666")).To(BeTrue())
						//the other one not affected
						g.Expect(strings.Contains(string(data), "ACCEPTOR_IP:61777")).To(BeTrue())
						g.Expect(strings.Contains(string(data), "ACCEPTOR_IP:61888")).To(BeTrue())
					}
				}

			}, timeout, interval).Should(Succeed())

			CleanResource(&crd, crd.Name, defaultNamespace)
		})
	})

	Context("With a deployed controller", func() {
		//TODO: Remove the 4x duplication and add all acceptor settings

		It("Testing acceptor keyStoreProvider being set", func() {
			By("By creating a new custom resource instance")
			ctx := context.Background()
			cr := generateArtemisSpec(defaultNamespace)

			cr.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			keyStoreProvider := "SunJCE"
			acceptorName := "new-acceptor"
			cr.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:             acceptorName,
					Port:             61666,
					SSLEnabled:       true,
					KeyStoreProvider: keyStoreProvider,
				},
			}
			sslSecretName := cr.Name + "-" + acceptorName + "-secret"
			sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", nil)
			Expect(err).To(BeNil())
			Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, &cr)).Should(Succeed())

			ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(cr.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				len := len(currentStatefulSet.Spec.Template.Spec.InitContainers)
				g.Expect(len).Should(BeEquivalentTo(1))

				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]
				By("check AMQ_ACCEPTORS value")
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}
						secret, err := secrets.RetriveSecret(namespaceName, make(map[string]string), k8sClient)
						g.Expect(err).To(BeNil())
						data := secret.Data[envVar.ValueFrom.SecretKeyRef.Key]
						By("Checking data:" + string(data))
						g.Expect(strings.Contains(string(data), "ACCEPTOR_IP:61666")).To(BeTrue())
						checkSecretHasCorrectKeyValue(g, namespaceName, envVar.ValueFrom.SecretKeyRef.Key, "keyStoreProvider=SunJCE")
					}
				}

			}, timeout, interval).Should(Succeed())

			CleanResource(&cr, cr.Name, defaultNamespace)
			CleanResource(sslSecret, sslSecret.Name, defaultNamespace)
		})

		It("Testing acceptor trustStoreType being set and unset", func() {
			By("By creating a new custom resource instance")
			ctx := context.Background()
			cr := generateArtemisSpec(defaultNamespace)

			cr.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			trustStoreType := "JCEKS"
			acceptorName := "new-acceptor"
			cr.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:           "new-acceptor",
					Port:           61666,
					SSLEnabled:     true,
					TrustStoreType: trustStoreType,
				},
			}
			sslSecretName := cr.Name + "-" + acceptorName + "-secret"
			sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", nil)
			Expect(err).To(BeNil())
			Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())
			Expect(k8sClient.Create(ctx, &cr)).Should(Succeed())

			ssResourceVersionWithSslEnabled := ""

			ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(cr.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				len := len(currentStatefulSet.Spec.Template.Spec.InitContainers)
				g.Expect(len).Should(Equal(1))

				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]

				found := false
				//check AMQ_ACCEPTORS value
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}
						//the value is a string of acceptors in xml format:
						//<acceptor name="new-acceptor">...</acceptor><another one>...
						//we need to locate our target acceptor and do the check
						//we use the port as a clue
						checkSecretHasCorrectKeyValue(g, namespaceName, envVar.ValueFrom.SecretKeyRef.Key, "trustStoreType=JCEKS")
						found = true
					}
				}
				g.Expect(found).Should(BeTrue())
				ssResourceVersionWithSslEnabled = currentStatefulSet.ResourceVersion

			}, timeout, interval).Should(Succeed())

			By("test Updating the CR back to sslEnabled=false")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {

				g.Expect(getPersistedVersionedCrd(cr.ObjectMeta.Name, defaultNamespace, createdCrd)).Should(BeTrue())

				createdCrd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:           "new-acceptor",
						Port:           61666,
						SSLEnabled:     false,
						TrustStoreType: trustStoreType,
					},
				}

				By("Redeploying the CRD")
				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				len := len(currentStatefulSet.Spec.Template.Spec.InitContainers)
				g.Expect(len).Should(Equal(1))

				g.Expect(currentStatefulSet.ResourceVersion).ShouldNot(Equal(ssResourceVersionWithSslEnabled))

				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]
				//check AMQ_ACCEPTORS value
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}

						secret, err := secrets.RetriveSecret(namespaceName, make(map[string]string), k8sClient)
						g.Expect(err).Should(BeNil())

						data := secret.Data[envVar.ValueFrom.SecretKeyRef.Key]
						g.Expect(string(data)).ShouldNot(ContainSubstring("trustStoreType=JCEKS"))
					}
				}
			}, timeout, interval).Should(Succeed())

			CleanResource(&cr, cr.Name, defaultNamespace)
			CleanResource(sslSecret, sslSecret.Name, defaultNamespace)
		})

		It("Testing acceptor trustStoreProvider being set", func() {
			By("By creating a new custom resource instance")
			ctx := context.Background()
			cr := generateArtemisSpec(defaultNamespace)

			cr.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size: common.Int32ToPtr(1),
			}
			trustStoreProvider := "SUN"
			acceptorName := "new-acceptor"
			cr.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:               "new-acceptor",
					Port:               61666,
					SSLEnabled:         true,
					TrustStoreProvider: trustStoreProvider,
				},
			}
			sslSecretName := cr.Name + "-" + acceptorName + "-secret"
			sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", nil)
			Expect(err).To(BeNil())
			Expect(k8sClient.Create(ctx, sslSecret)).Should(Succeed())

			Expect(k8sClient.Create(ctx, &cr)).Should(Succeed())

			ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(cr.Name), Namespace: defaultNamespace}

			Eventually(func(g Gomega) {
				currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
				g.Expect(err).To(BeNil())
				len := len(currentStatefulSet.Spec.Template.Spec.InitContainers)
				g.Expect(len).Should(Equal(1))

				initContainer := currentStatefulSet.Spec.Template.Spec.InitContainers[0]
				//check AMQ_ACCEPTORS value
				for _, envVar := range initContainer.Env {
					if envVar.Name == "AMQ_ACCEPTORS" {
						secretName := envVar.ValueFrom.SecretKeyRef.Name
						namespaceName := types.NamespacedName{
							Name:      secretName,
							Namespace: defaultNamespace,
						}
						//the value is a string of acceptors in xml format:
						//<acceptor name="new-acceptor">...</acceptor><another one>...
						//we need to locate our target acceptor and do the check
						//we use the port as a clue
						checkSecretHasCorrectKeyValue(g, namespaceName, envVar.ValueFrom.SecretKeyRef.Key, "trustStoreProvider=SUN")
					}
				}
			}, timeout, interval).Should(Succeed())

			CleanResource(&cr, cr.Name, defaultNamespace)
			CleanResource(sslSecret, sslSecret.Name, defaultNamespace)
		})
	})

	Context("With deployed controller", func() {
		It("verify old ver support", func() {
			By("By creating an old crd")
			ctx := context.Background()

			spec := brokerv2alpha4.ActiveMQArtemisSpec{}
			crd := brokerv2alpha4.ActiveMQArtemis{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemis",
					APIVersion: brokerv2alpha4.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
				Spec: spec,
			}

			crd.Spec.DeploymentPlan = brokerv2alpha4.DeploymentPlanType{
				Size:               1,
				PersistenceEnabled: true,
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			createdSs := &appsv1.StatefulSet{}
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				err := k8sClient.Get(ctx, key, createdSs)
				return err == nil
			}, timeout, interval).Should(Equal(true))

			CleanResource(&crd, crd.Name, defaultNamespace)
		})
	})

	Context("With deployed controller", func() {

		It("Checking storageClassName is configured", func() {

			By("By creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size:               common.Int32ToPtr(1),
				PersistenceEnabled: true,
				Storage: brokerv1beta1.StorageType{
					StorageClassName: "some-storage-class",
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, key, &crd)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			createdSs := &appsv1.StatefulSet{}
			Eventually(func() bool {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				err := k8sClient.Get(ctx, key, createdSs)
				return err == nil
			}, timeout, interval).Should(Equal(true))

			volumeTemplates := createdSs.Spec.VolumeClaimTemplates
			Expect(len(volumeTemplates)).To(Equal(1))

			storageClassName := volumeTemplates[0].Spec.StorageClassName
			Expect(*storageClassName).To(Equal("some-storage-class"))

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("verifying not yet ready status - has pod status digest with reference to pvc")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, key, &crd)).Should(Succeed())
					By("verify starting status" + fmt.Sprintf("%v", crd.Status.PodStatus))
					g.Expect(len(crd.Status.PodStatus.Starting)).Should(BeEquivalentTo(1))

					g.Expect(common.IsConditionPresentAndEqualIgnoringMessage(crd.Status.Conditions, metav1.Condition{
						Type:   brokerv1beta1.DeployedConditionType,
						Status: metav1.ConditionFalse,
						Reason: brokerv1beta1.DeployedConditionNotReadyReason,
					})).Should(BeTrue())

					condition := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.DeployedConditionType)
					g.Expect(condition).NotTo(BeNil())

					By("checking message" + fmt.Sprintf("%v", condition.Message))
					// not a chance!
					//g.Expect(condition.Message).To(ContainSubstring(*storageClassName))
					g.Expect(condition.Message).To(ContainSubstring("PersistentVolumeClaims"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			CleanResource(&crd, crd.Name, defaultNamespace)
		})

		It("Checking storage size mal configured", func() {

			By("By creating a new crd")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			crd.Spec.DeploymentPlan = brokerv1beta1.DeploymentPlanType{
				Size:               common.Int32ToPtr(1),
				PersistenceEnabled: true,
				Storage: brokerv1beta1.StorageType{
					Size: "2GI", // note wrong capital I
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, key, &crd)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			By("verifying status")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, key, &crd)).Should(Succeed())

				g.Expect(common.IsConditionPresentAndEqualIgnoringMessage(crd.Status.Conditions, metav1.Condition{
					Type:   brokerv1beta1.DeployedConditionType,
					Status: metav1.ConditionFalse,
					Reason: brokerv1beta1.DeployedConditionValidationFailedReason,
				})).Should(BeTrue())

				condition := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(condition).NotTo(BeNil())

				By("checking message" + fmt.Sprintf("%v", condition.Message))
				g.Expect(condition.Message).To(ContainSubstring("Size"))
			}, timeout, interval).Should(Succeed())

			CleanResource(&crd, crd.Name, defaultNamespace)
		})
	})

	It("populateValidatedUser", func() {

		ctx := context.Background()
		crd := generateArtemisSpec(defaultNamespace)
		crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
		}
		crd.Spec.DeploymentPlan.RequireLogin = true
		crd.Spec.BrokerProperties = []string{
			"securityEnabled=true",
			"rejectEmptyValidatedUser=true",
			"populateValidatedUser=true",
		}

		propLoginModules := make([]brokerv1beta1.PropertiesLoginModuleType, 1)
		pwd := "activemq"
		user := "Jay"
		moduleName := "prop-module"
		flag := "sufficient"
		propLoginModules[0] = brokerv1beta1.PropertiesLoginModuleType{
			Name: moduleName,
			Users: []brokerv1beta1.UserType{
				{Name: user,
					Password: &pwd,
					Roles:    []string{"jay-role"}},
			},
		}

		brokerDomainName := "activemq"
		loginModules := make([]brokerv1beta1.LoginModuleReferenceType, 1)
		loginModules[0] = brokerv1beta1.LoginModuleReferenceType{
			Name: &moduleName,
			Flag: &flag,
		}
		brokerDomain := brokerv1beta1.BrokerDomainType{
			Name:         &brokerDomainName,
			LoginModules: loginModules,
		}

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

			By("Deploying security spec")
			_, deployedSecCrd := DeploySecurity("for-jay", defaultNamespace, func(secCrdToDeploy *brokerv1beta1.ActiveMQArtemisSecurity) {
				secCrdToDeploy.Spec.LoginModules.PropertiesLoginModules = propLoginModules
				secCrdToDeploy.Spec.SecurityDomains.BrokerDomain = brokerDomain
				secCrdToDeploy.Spec.SecuritySettings.Broker =
					[]brokerv1beta1.BrokerSecuritySettingType{
						{Match: "#",
							Permissions: []brokerv1beta1.PermissionType{
								{OperationType: "send", Roles: []string{"jay-role"}},
								{OperationType: "browse", Roles: []string{"jay-role"}},
							},
						},
					}
			})

			By("Deploying a broker")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Checking ready on SS")
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				sfsFound := &appsv1.StatefulSet{}

				g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
				g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Sending 1")

			podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
			command := []string{"amq-broker/bin/artemis", "producer", "--user", user, "--password", pwd, "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}

			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Consuming 1")

			command = []string{"amq-broker/bin/artemis", "browser", "--user", user, "--password", pwd, "--url", "tcp://" + podWithOrdinal + ":61616", "--destination", "queue://DLQ", "--verbose"}

			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)

				Expect(stdOutContent).Should(ContainSubstring("messageID="))
				Expect(stdOutContent).Should(ContainSubstring("_AMQ_VALIDATED_USER=" + user))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// cleanup
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, deployedSecCrd)).Should(Succeed())

		}

	})

	It("populateValidatedUser as auto generated guest", func() {

		ctx := context.Background()
		crd := generateArtemisSpec(defaultNamespace)
		crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
		}
		crd.Spec.BrokerProperties = []string{
			"securityEnabled=true",
			"rejectEmptyValidatedUser=true",
			"populateValidatedUser=true",
		}

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

			By("Deploying a broker")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("Checking ready on SS")
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				sfsFound := &appsv1.StatefulSet{}

				g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
				g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Sending 1")

			podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
			command := []string{"amq-broker/bin/artemis", "producer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}

			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Consuming 1")

			command = []string{"amq-broker/bin/artemis", "browser", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podWithOrdinal + ":61616", "--destination", "queue://DLQ", "--verbose"}

			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)

				// ...ActiveMQMessage[ID:3a356c2a-e7e1-11ec-ae1c-5ec4168c91b2]:PERSISTENT/ClientMessageImpl[messageID=19, durable=true, address=DLQ,userID=3a356c2a-e7e1-11ec-ae1c-5ec4168c91b2,properties=TypedProperties[__AMQ_CID=3a2f9fc7-e7e1-11ec-ae1c-5ec4168c91b2,_AMQ_ROUTING_TYPE=1,_AMQ_VALIDATED_USER=73ykuMrb,count=0,ThreadSent=Producer ActiveMQQueue[DLQ], thread=0]]
				Expect(stdOutContent).Should(ContainSubstring("messageID="))
				Expect(stdOutContent).Should(ContainSubstring("_AMQ_VALIDATED_USER="))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// cleanup
			k8sClient.Delete(ctx, &crd)

		}
	})

	It("credential secret manually created", func() {

		ctx := context.Background()
		crd := generateArtemisSpec(defaultNamespace)
		crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(0)

		credentialsSecret := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Labels:    crd.Labels,
				Name:      crd.Name + "-credentials-secret",
				Namespace: crd.Namespace,
			},
			StringData: map[string]string{
				"AMQ_USER":             "admin",
				"AMQ_PASSWORD":         "admin",
				"AMQ_CLUSTER_USER":     "admin",
				"AMQ_CLUSTER_PASSWORD": "admin",
			},
		}

		By("Deploying credentialsSecret")
		Expect(k8sClient.Create(ctx, &credentialsSecret)).Should(Succeed())

		StartCapturingLog()
		defer StopCapturingLog()

		By("Deploying a broker")
		Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

		createdCrd := &brokerv1beta1.ActiveMQArtemis{}
		key := types.NamespacedName{Name: crd.Name, Namespace: defaultNamespace}

		Eventually(func(g Gomega) {
			By("Checking stopped status of CR, deployed with replica count 0")
			g.Expect(k8sClient.Get(ctx, key, createdCrd)).Should(Succeed())
			g.Expect(len(createdCrd.Status.PodStatus.Stopped)).Should(BeEquivalentTo(1))
		}, timeout, interval).Should(Succeed())

		Expect(MatchInCapturingLog("Failed to create new \\*v1.Secret")).Should(BeFalse())

		// cleanup
		k8sClient.Delete(ctx, &crd)
		k8sClient.Delete(ctx, &credentialsSecret)
	})

	It("deploy security cr while broker is not yet ready", func() {

		By("Creating broker with custom probe that relies on security")
		ctx := context.Background()
		randString := NextSpecResourceName()
		crd := generateOriginalArtemisSpec(defaultNamespace, "br-"+randString)

		crd.Spec.DeploymentPlan.ReadinessProbe = nil
		crd.Spec.AdminUser = "admin"
		crd.Spec.AdminPassword = "secret"
		crd.Spec.DeploymentPlan.LivenessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/home/jboss/amq-broker/bin/artemis",
						"check",
						"queue",
						"--name",
						"readinessqueue",
						"--produce",
						"1",
						"--consume",
						"1",
						"--silent",
						"--user",
						"user1",
						"--password",
						"ok",
					},
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      5,
			PeriodSeconds:       5,
		}
		crd.Spec.DeploymentPlan.Clustered = &boolFalse
		crd.Spec.DeploymentPlan.RequireLogin = boolTrue
		crd.Spec.DeploymentPlan.JolokiaAgentEnabled = false
		crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
			{
				Name:      "internal",
				Protocols: "core,openwire",
				Port:      61616,
			},
		}

		By("Deploying the CRD " + crd.ObjectMeta.Name)
		Expect(k8sClient.Create(ctx, crd)).Should(Succeed())

		createdCrd := &brokerv1beta1.ActiveMQArtemis{}
		createdSs := &appsv1.StatefulSet{}

		By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
		Eventually(func() bool {
			return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
		}, timeout, interval).Should(BeTrue())
		Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

		ssKey := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
		By("Checking that Stateful Set is Created " + ssKey.Name)

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			By("Checking ss " + ssKey.Name + " resource version" + createdSs.ResourceVersion)
			g.Expect(createdSs.ResourceVersion).ShouldNot(BeNil())
		}, timeout, interval).Should(Succeed())

		brokerKey := types.NamespacedName{Name: createdCrd.Name, Namespace: createdCrd.Namespace}
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

			By("verifying not yet ready status - probe failed")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				By("verify starting status" + fmt.Sprintf("%v", createdCrd.Status.PodStatus))
				g.Expect(len(createdCrd.Status.PodStatus.Starting)).Should(BeEquivalentTo(1))

				g.Expect(common.IsConditionPresentAndEqualIgnoringMessage(createdCrd.Status.Conditions, metav1.Condition{
					Type:   brokerv1beta1.DeployedConditionType,
					Status: metav1.ConditionFalse,
					Reason: brokerv1beta1.DeployedConditionNotReadyReason,
				})).Should(BeTrue())
				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		}

		By("Deploying security")
		_, createdSecCrd := DeploySecurity("sec-"+randString, defaultNamespace,
			func(secCrdToDeploy *brokerv1beta1.ActiveMQArtemisSecurity) {
				secCrdToDeploy.Spec.ApplyToCrNames = []string{createdCrd.Name}
			})

		By("Checking that Stateful Set is updated with sec commands, " + ssKey.Name)
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())

			initContainer := createdSs.Spec.Template.Spec.InitContainers[0]
			securityFound := false
			By("Checking init container args on ss " + ssKey.Name + " resource version" + createdSs.ResourceVersion)

			for _, argStr := range initContainer.Args {
				if strings.Contains(argStr, "/opt/amq-broker/script/cfg/config-security.sh") {
					securityFound = true
					break
				}
			}
			g.Expect(securityFound).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

			// looks like we must force a rollback of the old ss rollout as it won't complete
			// due to the failure of the readiness probe
			// https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#forced-rollback

			Eventually(func(g Gomega) {
				By("deleting existing pod that is stuck on rollout..")
				pod := &corev1.Pod{}
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name) + "-0", Namespace: defaultNamespace}
				zeroGracePeriodSeconds := int64(0) // immediate delete
				g.Expect(k8sClient.Get(ctx, key, pod)).Should(Succeed())
				g.Expect(pod.Status.Phase).Should(Equal(corev1.PodRunning))
				foundReadyFalse := false
				for _, pc := range pod.Status.Conditions {
					if pc.Type == corev1.ContainersReady && pc.Status == corev1.ConditionFalse {
						foundReadyFalse = true
					}
				}
				g.Expect(foundReadyFalse).Should(BeTrue())
				By("Deleting pod: " + key.Name)
				g.Expect(k8sClient.Delete(ctx, pod, &client.DeleteOptions{GracePeriodSeconds: &zeroGracePeriodSeconds})).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying ready with new SS rollout")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				By("verify ready status" + fmt.Sprintf("%v", createdCrd.Status.PodStatus))
				g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		}

		By("check it has gone")
		CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
		CleanResource(createdSecCrd, createdSecCrd.Name, defaultNamespace)
	})

	It("cascade delete foreground test", func() {

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

			By("Creating a simple broker")
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 1,
				PeriodSeconds:       2,
			}

			By("deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("checking Owner ref on stateful set")
			createdSs := &appsv1.StatefulSet{}
			ssKey := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
				g.Expect(*createdSs.GetOwnerReferences()[0].Controller).Should(BeTrue())
				g.Expect(createdSs.GetOwnerReferences()[0].BlockOwnerDeletion).Should(BeNil())
			}, timeout, interval).Should(Succeed())

			By("cascade deleting CR")
			cascade_foreground_policy := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, createdCrd, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())

			By("verifying deletion completed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, brokerKey, createdCrd)
				g.Expect(err).ShouldNot(BeNil())
				g.Expect(errors.IsNotFound(err)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		}
	})

	It("managementRBACEnabled is false", func() {

		ctx := context.Background()
		crd := generateArtemisSpec(defaultNamespace)
		crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
		}
		crd.Spec.DeploymentPlan.ManagementRBACEnabled = false

		By("Deploying the CRD " + crd.ObjectMeta.Name)
		Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

		createdCrd := &brokerv1beta1.ActiveMQArtemis{}
		createdSs := &appsv1.StatefulSet{}

		By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
		Eventually(func() bool {
			return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
		}, timeout, interval).Should(BeTrue())
		Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

		ssKey := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
		By("Checking that Stateful Set is Created " + ssKey.Name)

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			By("Checking ss resource version" + createdSs.ResourceVersion)
			g.Expect(createdSs.ResourceVersion).ShouldNot(BeNil())
		}, timeout, interval).Should(Succeed())

		By("By checking the container stateful set for AMQ_ENABLE_MANAGEMENT_RBAC")
		Eventually(func() (bool, error) {
			err := k8sClient.Get(ctx, ssKey, createdSs)
			if err != nil {
				return false, err
			}

			found := false
			for _, container := range createdSs.Spec.Template.Spec.InitContainers {
				for _, env := range container.Env {
					if env.Name == "AMQ_ENABLE_MANAGEMENT_RBAC" {
						if strings.Contains(env.Value, "false") {
							found = true
						}
					}
				}
			}

			return found, err
		}, duration, interval).Should(Equal(true))

		By("By checking it has gone")
		CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
	})

	It("env Var", func() {
		ctx := context.Background()
		crd := generateArtemisSpec(defaultNamespace)
		crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 5,
			TimeoutSeconds:      5,
			PeriodSeconds:       5,
		}
		javaOptsValue := "-verbose:class"
		crd.Spec.Env = []corev1.EnvVar{
			{Name: "TZ", Value: "en_IE"},
			{Name: "JAVA_OPTS", Value: javaOptsValue},
			{Name: "JDK_JAVA_OPTIONS", Value: "-XshowSettings:system"},
		}
		crd.Spec.BrokerProperties = []string{"globalMaxSize=512m"}

		By("Deploying the CRD " + crd.ObjectMeta.Name)
		Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

		createdCrd := &brokerv1beta1.ActiveMQArtemis{}
		createdSs := &appsv1.StatefulSet{}

		By("Making sure that the CRD gets deployed " + crd.ObjectMeta.Name)
		Eventually(func() bool {
			return getPersistedVersionedCrd(crd.ObjectMeta.Name, defaultNamespace, createdCrd)
		}, timeout, interval).Should(BeTrue())
		Expect(createdCrd.Name).Should(Equal(crd.ObjectMeta.Name))

		ssKey := types.NamespacedName{Name: namer.CrToSS(createdCrd.Name), Namespace: defaultNamespace}
		By("Checking that Stateful Set is Created " + ssKey.Name)

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			By("Checking ss resource version" + createdSs.ResourceVersion)
			g.Expect(createdSs.ResourceVersion).ShouldNot(BeNil())
		}, timeout, interval).Should(Succeed())

		By("By checking the init container stateful set for env var TZ")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			found := false
			for _, container := range createdSs.Spec.Template.Spec.InitContainers {
				for _, env := range container.Env {
					By("Checking init container env: " + env.Name + "::" + env.Value)
					if env.Name == "TZ" {
						if strings.Contains(env.Value, "en_IE") {
							found = true
						}
					}
				}
			}
			g.Expect(found).Should(Equal(true))
		}, duration, interval*4).Should(Succeed())

		By("By checking the init container stateful set for env var JAVA_OPTS append")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			found := 0
			for _, container := range createdSs.Spec.Template.Spec.InitContainers {
				for _, env := range container.Env {
					By("Checking init container env: " + env.Name + "::" + env.Value)
					if env.Name == "JAVA_OPTS" {
						if strings.Contains(env.Value, brokerPropertiesMatchString) {
							found++
						}
						if strings.Contains(env.Value, javaOptsValue) {
							found++
						}
					}
				}
			}
			g.Expect(found).Should(Equal(2))
		}, duration, interval*4).Should(Succeed())

		By("By checking the container stateful set for env var TZ")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, createdSs)).Should(Succeed())
			found := false
			for _, container := range createdSs.Spec.Template.Spec.Containers {
				for _, env := range container.Env {
					By("Checking container env: " + env.Name + "::" + env.Value)
					if env.Name == "TZ" {
						if strings.Contains(env.Value, "en_IE") {
							found = true
						}
					}
				}
			}
			g.Expect(found).Should(Equal(true))
		}, duration, interval).Should(Succeed())

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			By("verifying verbose:gc via logs")

			podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
			Eventually(func(g Gomega) {
				stdOutContent := LogsOfPod(podWithOrdinal, crd.Name, defaultNamespace, g)
				// from JDK_JAVA_OPTIONS
				g.Expect(stdOutContent).Should(ContainSubstring("Operating System Metrics"))
				// from JAVA_OPTS munged via artemis create
				g.Expect(stdOutContent).Should(ContainSubstring("class,load"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		}

		By("By checking it has gone")
		CleanResource(createdCrd, createdCrd.Name, defaultNamespace)
	})

	It("enable JVM metrics by using broker properties", func() {
		ctx := context.Background()
		crd := generateArtemisSpec(defaultNamespace)
		enableMetricsPlugin := true
		crd.Spec.Console.Expose = true
		crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
		crd.Spec.DeploymentPlan.EnableMetricsPlugin = &enableMetricsPlugin
		crd.Spec.BrokerProperties = []string{
			"metricsConfiguration.jvmGc=true",
			"metricsConfiguration.jvmThread=true",
		}

		if !isOpenshift {
			crd.Spec.IngressDomain = defaultTestIngressDomain
		}

		By("Deploying the CRD " + crd.ObjectMeta.Name)
		Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			By("verifying started")
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics")
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				podName := namer.CrToSS(crd.Name) + "-0"
				podNamespacedName := types.NamespacedName{Name: podName, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(ctx, podNamespacedName, pod)).Should(Succeed())

				resp, err := http.Get("http://" + pod.Status.PodIP + ":8161/metrics/")
				g.Expect(err).Should(Succeed())

				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				g.Expect(err).Should(Succeed())

				g.Expect(body).Should(ContainSubstring("jvm_gc"))
				g.Expect(body).Should(ContainSubstring("jvm_threads"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		}

		By("By checking it has gone")
		Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
	})

	Context("config projection", Label("slow"), func() {

		It("ordinal broker properties", func() {
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(3)
			crd.Spec.BrokerProperties = []string{
				"name=BROKER_ORDINAL_0",
				"broker-1.name=BROKER_ORDINAL_1",
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(3))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					// setting name from non default amq-broker causes JMX error and unknown is expected
					g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType, metav1.ConditionUnknown)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying ordinal config via logs")

				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				Eventually(func(g Gomega) {
					stdOutContent := LogsOfPod(podWithOrdinal, crd.Name, defaultNamespace, g)
					g.Expect(stdOutContent).Should(ContainSubstring("BROKER_ORDINAL_0"))
					g.Expect(stdOutContent).Should(ContainSubstring("broker-0 does not exist"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				podWithOrdinal = namer.CrToSS(crd.Name) + "-1"
				Eventually(func(g Gomega) {
					stdOutContent := LogsOfPod(podWithOrdinal, crd.Name, defaultNamespace, g)
					g.Expect(stdOutContent).Should(ContainSubstring("BROKER_ORDINAL_1"))
					g.Expect(stdOutContent).ShouldNot(ContainSubstring("broker-1 does not exist"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				podWithOrdinal = namer.CrToSS(crd.Name) + "-2"
				Eventually(func(g Gomega) {
					stdOutContent := LogsOfPod(podWithOrdinal, crd.Name, defaultNamespace, g)
					g.Expect(stdOutContent).Should(ContainSubstring("BROKER_ORDINAL_0"))
					g.Expect(stdOutContent).Should(ContainSubstring("broker-2 does not exist"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("By checking it has gone")
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("ordinal broker properties with other secret", func() {
			ctx := context.Background()

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			crd.Spec.BrokerProperties = []string{
				"broker-0.connectorConfigurations.f1.params.HOST=" + crd.Name + "-ss-1." + crd.Name + "-hdls-svc",
				"broker-1.connectorConfigurations.f1.params.HOST=" + crd.Name + "-ss-0." + crd.Name + "-hdls-svc",
			}

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-bits",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{"some-bits": "stuff"}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the secret " + secret.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("invalid ordinal prefix broker properties", func() {
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.BrokerProperties = []string{
				"globalMaxSize=512m",
				"broker-1globalMaxSize=612m",
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).Should(ContainSubstring("612m"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("By checking it has gone")
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("brokerProperties with escapes", func() {

			loggingConfigMapName := "my-logging-config-s"
			loggingData := make(map[string]string)
			loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
		appender.stdout.type = Console
		rootLogger = info, STDOUT
		logger.activemq.name=org.apache.activemq.artemis.core.config.impl.ConfigurationImpl
        logger.activemq.level=TRACE`

			loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, loggingConfigMap, &client.CreateOptions{})).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			toCreate := generateArtemisSpec(defaultNamespace)

			toCreate.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}

			toCreate.Spec.BrokerProperties = []string{
				"addressesSettings.#.redeliveryMultiplier=2.3",
				"addressesSettings.#.redeliveryCollisionAvoidanceFactor=1.2",
				// note, values formated for a java properties file encoding, space in name escaped
				// https://docs.oracle.com/javase/6/docs/api/java/util/Properties.html#load%28java.io.Reader%29
				`addressesSettings.Some\ value\ with\ space.redeliveryCollisionAvoidanceFactor=1.2`,
				`addressesSettings.FF\:\:QN.redeliveryCollisionAvoidanceFactor=1.2`,
				`addressesSettings.NameWith\=Equals.redeliveryCollisionAvoidanceFactor=1.2`,
			}

			Expect(k8sClient.Create(context.TODO(), &toCreate)).Should(Succeed())

			key := types.NamespacedName{Name: toCreate.Name, Namespace: toCreate.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, key, createdCrd)).Should(Succeed())

					g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType, metav1.ConditionTrue)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, loggingConfigMap)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &toCreate)).Should(Succeed())
		})

		It("mod ordinal broker properties with error and update", func() {
			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			crd.Spec.BrokerProperties = []string{
				"globalMaxSize=512m",
				"broker-1.populateValidatedUser=true",
				"broker-0.populateValidatedUser=true",
				"broker-2.populateValidatedUser=true",
				"broker-1.nonGlobalNonExistSholdError=7",
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeFalse())

					g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).Should(ContainSubstring("broker-1.broker.properties"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				var ssGeneration int64 = 0

				ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
					g.Expect(err).To(BeNil())
					ssGeneration = currentStatefulSet.Generation
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("update Cr to fix error")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					createdCrd.Spec.BrokerProperties = []string{
						"globalMaxSize=512m",
						"broker-1.populateValidatedUser=true",
						"broker-0.populateValidatedUser=true",
						"broker-2.populateValidatedUser=true",
						"broker-1.globalMaxSize=612m",
					}

					g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())
				}, timeout, interval).Should(Succeed())

				By("verify config applied is good")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval*5).Should(Succeed())

				By("verify same ss generation")
				Eventually(func(g Gomega) {
					currentStatefulSet, err := ss.RetrieveStatefulSet(ssNamespacedName.Name, ssNamespacedName, nil, k8sClient)
					g.Expect(err).To(BeNil())
					g.Expect(currentStatefulSet.Generation).To(Equal(ssGeneration))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			By("By checking it has gone")
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraMount.configMap projection update", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 2,
				TimeoutSeconds:      5,
				PeriodSeconds:       5,
			}

			configMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:         "jaas-bits",
					GenerateName: "",
					Namespace:    crd.ObjectMeta.Namespace,
				},
				// mutable
			}

			configMap.Data = map[string]string{"_a.props": "a=a1"}

			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{configMap.Name}

			By("Deploying the configMap " + configMap.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying content of configmap props")
				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command := []string{"cat", "/amq/extra/configmaps/jaas-bits/_a.props"}
				statCommand := []string{"stat", "/amq/extra/configmaps/jaas-bits/"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("a1"))

					stdOutContent = ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, statCommand, g)
					if verbose {
						fmt.Printf("\na1 - Stat:\n%s", stdOutContent)
					}

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("updating config map")
				createdConfigMap := &corev1.ConfigMap{}
				configMapKey := types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, configMapKey, createdConfigMap)).Should(Succeed())
					createdConfigMap.Data = map[string]string{"_a.props": "a=a2"}

					g.Expect(k8sClient.Update(ctx, createdConfigMap)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying updated content of configmap props")

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("a2"))

					stdOutContent = ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, statCommand, g)
					if verbose {
						fmt.Printf("\na2 - Stat:\n%s", stdOutContent)
					}
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			By("By checking it has gone")
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraMount.secret jaas-config validation", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{"some": "stuff"}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verifying invalid via status")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType, metav1.ConditionFalse)).Should(BeTrue())

				deployedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)
				g.Expect(deployedCondition.Reason).Should(Equal(brokerv1beta1.DeployedConditionValidationFailedReason))

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				validationCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validationCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
				g.Expect(validationCondition.Message).To(ContainSubstring(JaasConfigKey))

			}, timeout, interval).Should(Succeed())

			By("update secret map")
			createdSecret := &corev1.Secret{}
			secretName := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())
				createdSecret.Data[JaasConfigKey] = []byte(`a_realm { a_good_login_module sufficient noOp=true; };`)
				g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("verifying now valid")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Invalidating again to verify status update to false")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())
				createdSecret.Data[JaasConfigKey] = []byte(`a_realm { a_login_module_missing_val sufficient noOp=; };`)
				g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("verifying now in valid again")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraMount jaas-config once validation", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v2-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{JaasConfigKey: `a_realm { a_good_login_module sufficient noOp=true; };`}

			secret2 := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v3-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret2.StringData = map[string]string{JaasConfigKey: `a_realm { a_good_login_module sufficient noOp=true; };`}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name, secret2.Name}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the jaas secret two " + secret2.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret2)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verifying invalid via status")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())

				validationCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validationCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
				g.Expect(validationCondition.Message).To(ContainSubstring("once"))

			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, secret2)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraMount.secret x-jaas-config single realm update status", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			loggingConfigMapName := "my-logging-config"
			loggingData := make(map[string]string)
			loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
		appender.stdout.type = Console
		rootLogger = info, STDOUT
		logger.activemq.name=org.apache.activemq.artemis.spi.core.security.jaas
        logger.activemq.level=TRACE
`

			loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, loggingConfigMapName, loggingData)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, loggingConfigMap, &client.CreateOptions{})).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "x-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
				// mutable
			}

			userPropsKey := "users.properties"

			secret.StringData = map[string]string{JaasConfigKey: `
		    // a full login.config, property files with unique names to match keys
		    activemq {
			    org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
					reload=true
					debug=true
					org.apache.activemq.jaas.properties.user="users.properties"
					org.apache.activemq.jaas.properties.role="roles.properties";

				// ensure the operator can connect to the broker by referencing the existing properties config
				// operatorAuth = plain
				org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
					reload=false
					org.apache.activemq.jaas.properties.user="artemis-users.properties"
					org.apache.activemq.jaas.properties.role="artemis-roles.properties"
					baseDir="/home/jboss/amq-broker/etc";

			};`,
				userPropsKey: `
			tom=tom
			peter=peter`,
				"roles.properties": `admin = joe`, // this is a cheat to allow joe, when added as a user, to access the DLQ
			}

			// extra bits - prefixed with '_' not read by the broker - won't be in brokerStatus
			secret.StringData["_a.props"] = "a=a1"

			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMapName}
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				var originalResourceVersion string
				var updatedResourceVersion string

				By("verifying via status")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

					ConfigAppliedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)).Should(BeTrue())

					ConfigAppliedCondition = meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))

					g.Expect(len(createdCrd.Status.ExternalConfigs)).Should(BeEquivalentTo(1))
					g.Expect(createdCrd.Status.ExternalConfigs[0].Name).Should(ContainSubstring("x"))

					originalResourceVersion = createdCrd.Status.ExternalConfigs[0].ResourceVersion

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("updating custom map with new user joe")
				createdSecret := &corev1.Secret{}
				secretName := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())

					createdSecret.Data[userPropsKey] = []byte(`tom=tom
				peter=peter
				joe=joe`)

					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

					g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())
					updatedResourceVersion = createdSecret.ResourceVersion
					g.Expect(updatedResourceVersion).ShouldNot(Equal(originalResourceVersion))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("forcing a new login to reload custom user")

				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command := []string{"amq-broker/bin/artemis", "producer", "--user", "joe", "--password", "joe", "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
				}, existingClusterTimeout, existingClusterInterval*2).Should(Succeed())

				By("verifying updated content via status in sync")

				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

					g.Expect(createdCrd.Status.ExternalConfigs[0].ResourceVersion).Should(BeEquivalentTo(updatedResourceVersion))
					g.Expect(len(createdCrd.Status.ExternalConfigs)).Should(BeEquivalentTo(1))
					g.Expect(createdCrd.Status.ExternalConfigs[0].Name).Should(ContainSubstring("x"))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify extra config status after projection update of non referenced key, the only status update is extraConfig")
				originalResourceVersion = updatedResourceVersion
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())

					createdSecret.Data["_a.props"] = []byte(`a=a2`)

					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())
					g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())
					updatedResourceVersion = createdSecret.ResourceVersion
					g.Expect(updatedResourceVersion).ShouldNot(Equal(originalResourceVersion))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

					g.Expect(createdCrd.Status.ExternalConfigs[0].ResourceVersion).Should(BeEquivalentTo(updatedResourceVersion))
					g.Expect(len(createdCrd.Status.ExternalConfigs)).Should(BeEquivalentTo(1))
					g.Expect(createdCrd.Status.ExternalConfigs[0].Name).Should(ContainSubstring("x"))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, loggingConfigMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraMount.secret y-jaas-config mgmt realm ok connect and status check", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "y-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			userPropsKey := "users.properties"

			secret.StringData = map[string]string{JaasConfigKey: `
		    // a full login.config
		    activemq {
			    org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule required
					reload=true
					debug=true
					org.apache.activemq.jaas.properties.user="users.properties"
					org.apache.activemq.jaas.properties.role="roles.properties";
			};

			console {

				// ensure the operator can connect to the mgmt console by referencing the existing properties config
				// operatorAuth = plain
				// hawtio.realm = console
				org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule required
					reload=true
					debug=true
					org.apache.activemq.jaas.properties.user="artemis-users.properties"
					org.apache.activemq.jaas.properties.role="artemis-roles.properties"
					baseDir="/home/jboss/amq-broker/etc";

			};`,
				userPropsKey:       `tom=tom`,
				"roles.properties": `admin=tom`,
			}

			crd.Spec.Env = []corev1.EnvVar{
				{Name: "JAVA_ARGS_APPEND", Value: "-Dhawtio.realm=console"},
			}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying ready status")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())

					// secret status won't be visible till activemq realm is exercised, ready true but with unknown condition
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType, metav1.ConditionUnknown)).Should(BeTrue())

					ConfigAppliedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition.Message).To(ContainSubstring("not visible"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("forcing a new login to exercise activemq realm")

				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command := []string{"amq-broker/bin/artemis", "producer", "--user", "tom", "--password", "tom", "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
				}, existingClusterTimeout, existingClusterInterval*2).Should(Succeed())

				By("verifying via status")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

					ConfigAppliedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))

					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)).Should(BeTrue())

					ConfigAppliedCondition = meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))

					g.Expect(len(createdCrd.Status.ExternalConfigs)).Should(BeEquivalentTo(1))
					g.Expect(createdCrd.Status.ExternalConfigs[0].Name).Should(ContainSubstring("y"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("CR.brokerProperties and -bp duplicate validation", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "v-bp",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{"a.properties": "journalMinFiles=10\njournalMinFiles=20"}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verifying invalid via status")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType, metav1.ConditionFalse)).Should(BeTrue())

				deployedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)
				g.Expect(deployedCondition.Reason).Should(Equal(brokerv1beta1.DeployedConditionValidationFailedReason))

				g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
				validationCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validationCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedExtraMountReason))
				g.Expect(validationCondition.Message).To(ContainSubstring("a.properties"))

			}, timeout, interval).Should(Succeed())

			By("update secret map")
			createdSecret := &corev1.Secret{}
			secretName := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())
				createdSecret.StringData = map[string]string{"a.properties": "journalMinFiles=20"}
				g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("verifying now valid")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			By("Invalidating again via brokerProperties")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				createdCrd.Spec.BrokerProperties = []string{
					"journalMinFiles=30",
					"journalMinFiles=40",
				}

				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("verifying now invalid with cr.brokerPropertiues dups")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeFalse())

				validationCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validationCondition.Reason).To(Equal(brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey))
				g.Expect(validationCondition.Message).To(ContainSubstring("journalMinFiles"))

			}, timeout, interval).Should(Succeed())

			By("remove duplicate")
			Eventually(func(g Gomega) {

				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				createdCrd.Spec.BrokerProperties = []string{
					"journalMinFiles=50",
				}

				g.Expect(k8sClient.Update(ctx, createdCrd)).Should(Succeed())

			}, timeout, interval).Should(Succeed())

			By("verifying now valid")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("onboarding - jaas-config new user queue rbac", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "w-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{JaasConfigKey: `
		    // a full login.config
		    activemq {

				// ensure the operator can connect to the mgmt console by referencing the existing properties config
				org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
					reload=true
					debug=true
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
					tom=tom
					foo=foo
				`,
				"roles.properties": `
					toms=tom
					the\ foos=foo
				`,
			}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			// avoiding SecurityCR and AddressCR
			crd.Spec.BrokerProperties = []string{
				`# create tom's work queue`,
				`addressConfigurations.TOMS_WORK_QUEUE.queueConfigs.TOMS_WORK_QUEUE.routingType=ANYCAST`,
				`addressConfigurations.TOMS_WORK_QUEUE.queueConfigs.TOMS_WORK_QUEUE.durable=true`,

				`# rbac, give tom's role send/consume access`,
				`securityRoles.TOMS_WORK_QUEUE.toms.send=true`,
				`securityRoles.TOMS_WORK_QUEUE.toms.consume=true`,
				`securityRoles.TOMS_WORK_QUEUE."the\ foos".send=true`,
				`securityRoles.TOMS_WORK_QUEUE."the\ foos".consume=true`,
			}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					By("verifying ready status")
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					// user/role custom properties won't yet be in memory on the broker
					g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType, metav1.ConditionUnknown)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("tom doing his thing")

				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"

				for _, user := range []string{"tom", "foo"} {
					command := []string{"amq-broker/bin/artemis", "producer", "--user", user, "--password", user, "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://TOMS_WORK_QUEUE", "--verbose"}
					By(user + " producing")
					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
					}, existingClusterTimeout, existingClusterInterval*2).Should(Succeed())

					By(user + " consuming")
					command = []string{"amq-broker/bin/artemis", "consumer", "--user", user, "--password", user, "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://TOMS_WORK_QUEUE", "--receive-timeout", "10000", "--break-on-null", "--verbose"}
					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring("JMS Message ID:"))
					}, existingClusterTimeout, existingClusterInterval*2).Should(Succeed())
				}

				Eventually(func(g Gomega) {
					By("verifying jaas status")
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					// user/role custom properties have been loaded
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("jaas-config not allowed in config map", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crdKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

			By("verify -jaas-config in config map is invalid")
			invalidJaasCm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cm-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}
			invalidJaasCm.Data = map[string]string{JaasConfigKey: `content not verified as not a secret`}
			Expect(k8sClient.Create(ctx, invalidJaasCm)).To(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{invalidJaasCm.Name}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verifying ivalid status and resource version")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCondition.Message).To(ContainSubstring("secret"))
				g.Expect(validCondition.ObservedGeneration).Should(Equal(createdCrd.Generation))
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, invalidJaasCm)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		It("jaas-config syntax check", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crdKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

			By("verify -jaas-config in config map is invalid")
			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "check-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}
			secret.StringData = map[string]string{JaasConfigKey: `{;`}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("verifying ivalid status and resource version")
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crdKey, createdCrd)).To(Succeed())
				validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(validCondition.Message).To(ContainSubstring("syntax"))
				g.Expect(validCondition.ObservedGeneration).Should(Equal(createdCrd.Generation))
			}, timeout, interval).Should(Succeed())

			By("deleting the logging configmap")
			Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		It("ordinal status - jaas-config", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "x-jaas-config",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{JaasConfigKey: `
		    // a full login.config
		    activemq {

				// ensure the operator can connect to the mgmt console by referencing the existing properties config
				org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
					reload=true
					debug=true
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
				"users.properties": `tom=tom`,
				"roles.properties": `toms=tom`,
			}

			crd.Spec.BrokerProperties = []string{
				"# create tom's work queue",
				"addressConfigurations.TOMS_WORK_QUEUE.queueConfigs.TOMS_WORK_QUEUE.routingType=ANYCAST",
				"addressConfigurations.TOMS_WORK_QUEUE.queueConfigs.TOMS_WORK_QUEUE.durable=true",

				"# rbac, give tom's role send access",
				"securityRoles.TOMS_WORK_QUEUE.toms.send=true",
			}

			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the jaas secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					By("verifying ready, jaas out of sync status")
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					// user/role custom properties won't yet be in memory on the broker
					g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType, metav1.ConditionUnknown)).Should(BeTrue())

					jaasAppliedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)
					g.Expect(jaasAppliedCondition).ShouldNot(BeNil())
					g.Expect(jaasAppliedCondition.Message).Should(ContainSubstring("-ss-0"))
					g.Expect(jaasAppliedCondition.Message).Should(ContainSubstring("LoginModule"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("tom doing his thing on 0")

				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command := []string{"amq-broker/bin/artemis", "producer", "--user", "tom", "--password", "tom", "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://TOMS_WORK_QUEUE", "--verbose"}

				By("producing to 0")
				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
				}, existingClusterTimeout, existingClusterInterval*2).Should(Succeed())

				Eventually(func(g Gomega) {

					By("verifying out of sync status on 1")
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					// user/role custom properties won't yet be in memory on the broker
					g.Expect(meta.IsStatusConditionPresentAndEqual(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType, metav1.ConditionUnknown)).Should(BeTrue())
					jaasAppliedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)
					g.Expect(jaasAppliedCondition).ShouldNot(BeNil())
					g.Expect(jaasAppliedCondition.Message).Should(ContainSubstring("-ss-1"))
					g.Expect(jaasAppliedCondition.Message).Should(ContainSubstring("LoginModule"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				podWithOrdinal = namer.CrToSS(crd.Name) + "-1"
				command = []string{"amq-broker/bin/artemis", "producer", "--user", "tom", "--password", "tom", "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "queue://TOMS_WORK_QUEUE", "--verbose"}

				By("producing to 1")
				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring("Produced: 1 messages"))
				}, existingClusterTimeout, existingClusterInterval*2).Should(Succeed())

				Eventually(func(g Gomega) {
					By("verifying jaas status")
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					// user/role custom properties have been loaded
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.JaasConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraSecret with broker properties -bp suffix", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "x-bp",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.StringData = map[string]string{
				"address.properties":  `bla=bla`,
				"acceptor.properties": `acceptorConfigurations.artemis.params.router=autoShard`,
			}

			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the -bp secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())

					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))

					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).Should(ContainSubstring("bla"))

					// find some reference to the secret source
					g.Expect(condition.Message).Should(ContainSubstring("x-bp"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("fix -bp secret via update")
				createdSecret := &corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      secret.ObjectMeta.Name,
					Namespace: defaultNamespace,
				}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, createdSecret)).To(Succeed())

					createdSecret.Data["address.properties"] = []byte(`addressesSettings.#.redeliveryMultiplier=2.3`)
					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify status ok")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("extraSecret with JSON broker properties -bp suffix", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "x-bp",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			addressJsonMap := map[string]interface{}{
				"bla": "bla",
			}

			addressJsonBytes, err := json.Marshal(addressJsonMap)
			Expect(err).Should(Succeed())

			acceptorJsonMap := map[string]interface{}{
				"acceptorConfigurations": map[string]interface{}{
					"artemis": map[string]interface{}{
						"params": map[string]interface{}{
							"router": "autoShard",
						},
					},
				},
			}

			acceptorJsonBytes, err := json.Marshal(acceptorJsonMap)
			Expect(err).Should(Succeed())

			secret.Data = map[string][]byte{
				"address.json":  addressJsonBytes,
				"acceptor.json": acceptorJsonBytes,
			}

			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			By("Deploying the -bp secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())

					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))

					g.Expect(condition.Reason).Should(Equal(brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason))
					g.Expect(condition.Message).Should(ContainSubstring("bla"))

					// find some reference to the secret source
					g.Expect(condition.Message).Should(ContainSubstring("x-bp"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("fix -bp secret via update")
				createdSecret := &corev1.Secret{}
				secretKey := types.NamespacedName{
					Name:      secret.ObjectMeta.Name,
					Namespace: defaultNamespace,
				}

				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, secretKey, createdSecret)).To(Succeed())

					addressJsonMap := map[string]interface{}{
						"addressesSettings": map[string]interface{}{
							"#": map[string]interface{}{
								"redeliveryMultiplier": "2.3",
							},
						},
					}

					addressJsonBytes, err := json.Marshal(addressJsonMap)
					Expect(err).Should(Succeed())

					createdSecret.Data["address.json"] = addressJsonBytes
					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify status ok")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("complex extraSecret with JSON broker properties with quote and -bp suffix", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)

			loggingData := make(map[string]string)
			loggingData[LoggingConfigKey] = `appender.stdout.name = STDOUT
		appender.stdout.type = Console
		rootLogger = info, STDOUT
		logger.activemq.name=org.apache.activemq.artemis.core.config.impl.ConfigurationImpl
        logger.activemq.level=TRACE`

			loggingConfigMap := configmaps.MakeConfigMap(defaultNamespace, "config-impl-logging-config", loggingData)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Create(ctx, loggingConfigMap, &client.CreateOptions{})).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "y-bp",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			secret.Data = map[string][]byte{
				"config.json": []byte(`{}`),
			}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}
			//loggingConfigMap
			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{loggingConfigMap.Name}

			By("Deploying the -bp secret " + secret.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verify status ok")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-ss-0."+crd.Name+"-hdls-svc.test.svc.cluster.local", "8161", "/console/jolokia", "", "", "http")

				var hasFileChecksum bool = false
				By("verify file checksum")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					// check for fileAlder32
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\"/Status")

					if verbose {
						fmt.Printf("\nStatus ERROR: %v : %v\n", data, err)
					}
					g.Expect(err).To(BeNil())
					hasFileChecksum = strings.Contains(data.Value, "fileAlder32")
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				if hasFileChecksum {

					By("Update json config with quote value")
					createdSecret := corev1.Secret{}
					Eventually(func(g Gomega) {

						g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: secret.Name, Namespace: secret.Namespace}, &createdSecret)).To(Succeed())

						createdSecret.Data["config.json"] = []byte(`{
						"key.surround": "$$",
						"addressSettings": {
						  "a": {
							"maxDeliveryAttempts": 0
						  },
						  "$$e.\"\"e$$": {
							"maxDeliveryAttempts": 1
						  },
						  "b.b": {
							"maxDeliveryAttempts": 2
						  }
						}
					  }`)
						g.Expect(k8sClient.Update(ctx, &createdSecret)).To(Succeed())

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("verify status ok")
					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

						condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
						g.Expect(condition).NotTo(BeNil())

						g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

						By("check applied address settings via jolokia")
						for index, addr := range []string{"a", `e.\"\"e`, "b.b"} {
							data, err := jolokia.Exec("", `{ "type":"EXEC","mbean":"org.apache.activemq.artemis:broker=\"amq-broker\"","operation":"getAddressSettingsAsJSON(java.lang.String)","arguments":["`+addr+`"] }`)

							if verbose {
								fmt.Printf("\nSettings ERROR: %s : %v\n", addr, err)
							}
							g.Expect(err).To(BeNil())
							if verbose {
								fmt.Printf("\nSettings: %s : %v\n", addr, data.Value)
							}
							g.Expect(data.Value).Should(ContainSubstring(`"maxDeliveryAttempts":`+strconv.Itoa(index)), data.Value)
						}

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}
			}

			Expect(k8sClient.Delete(ctx, loggingConfigMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

		It("-bp suffix secret broker-n support", Label("broker-n-bp-secret"), func() {

			ctx := context.Background()

			_, bpSecret1 := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = "config-1-bp"
				candidate.StringData = map[string]string{
					"journal1.properties":           `journalFileSize=12345`,
					"broker-0.globalMem.properties": `globalMaxSize=512M`,
				}
			})

			_, bpSecret2 := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = "config-2-bp"
				candidate.StringData = map[string]string{
					"journal2.properties":           `journalMinFiles=3`,
					"broker-1.globalMem.properties": `globalMaxSize=12M`,
				}
			})

			By("Deploying the CRD")
			_, crd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				candidate.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{bpSecret1.Name, bpSecret2.Name}
			})

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(crd.Name),
					Namespace: defaultNamespace,
				}

				By("checking statefulset is ready")
				currentSS := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
					g.Expect(currentSS.Status.ReadyReplicas).To(Equal(int32(2)))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking pod 0 status that has properties applied")
				podWithOrdinal0 := namer.CrToSS(crd.Name) + "-0"

				curlUrl := "http://" + podWithOrdinal0 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/Status"
				curlCmd := []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*result).To(ContainSubstring("journal1.properties"))
					g.Expect(*result).To(ContainSubstring("broker-0.globalMem.properties"))
					g.Expect(*result).To(ContainSubstring("journal2.properties"))
					g.Expect(*result).NotTo(ContainSubstring("broker-1.globalMem.properties"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				curlUrl = "http://" + podWithOrdinal0 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/GlobalMaxSize"
				curlCmd = []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					// 512M = 512 * 1024 * 1024
					g.Expect(*result).To(ContainSubstring("\"value\":536870912"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking pod 1 status that has properties applied")
				podWithOrdinal1 := namer.CrToSS(crd.Name) + "-1"

				curlUrl = "http://" + podWithOrdinal1 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/Status"
				curlCmd = []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal1, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*result).To(ContainSubstring("journal1.properties"))
					g.Expect(*result).To(ContainSubstring("broker-1.globalMem.properties"))
					g.Expect(*result).To(ContainSubstring("journal2.properties"))
					g.Expect(*result).NotTo(ContainSubstring("broker-0.globalMem.properties"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				curlUrl = "http://" + podWithOrdinal1 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/GlobalMaxSize"
				curlCmd = []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal1, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					// 12M = 12 * 1024 * 1024
					g.Expect(*result).To(ContainSubstring("\"value\":12582912"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verify status ok")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, crd)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bpSecret1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bpSecret2)).To(Succeed())
		})

		It("-bp suffix secret broker-n JSON support", Label("broker-n-bp-secret"), func() {

			ctx := context.Background()

			_, bpSecret1 := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = "config-1-bp"
				candidate.StringData = map[string]string{
					"journal1.json":           `{"journalFileSize":12345}`,
					"broker-0.globalMem.json": `{"globalMaxSize":"512M"}`,
				}
			})

			_, bpSecret2 := DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
				candidate.Name = "config-2-bp"
				candidate.StringData = map[string]string{
					"journal2.json":           `{"journalMinFiles":3}`,
					"broker-1.globalMem.json": `{"globalMaxSize":"12M"}`,
				}
			})

			By("Deploying the CRD")
			_, crd := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				candidate.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{bpSecret1.Name, bpSecret2.Name}
			})

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(crd.Name),
					Namespace: defaultNamespace,
				}

				By("checking statefulset is ready")
				currentSS := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
					g.Expect(currentSS.Status.ReadyReplicas).To(Equal(int32(2)))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking pod 0 status that has properties applied")
				podWithOrdinal0 := namer.CrToSS(crd.Name) + "-0"

				curlUrl := "http://" + podWithOrdinal0 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/Status"
				curlCmd := []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*result).To(ContainSubstring("journal1.json"))
					g.Expect(*result).To(ContainSubstring("broker-0.globalMem.json"))
					g.Expect(*result).To(ContainSubstring("journal2.json"))
					g.Expect(*result).NotTo(ContainSubstring("broker-1.globalMem.json"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				curlUrl = "http://" + podWithOrdinal0 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/GlobalMaxSize"
				curlCmd = []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal0, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					// 512M = 512 * 1024 * 1024
					g.Expect(*result).To(ContainSubstring("\"value\":536870912"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking pod 1 status that has properties applied")
				podWithOrdinal1 := namer.CrToSS(crd.Name) + "-1"

				curlUrl = "http://" + podWithOrdinal1 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/Status"
				curlCmd = []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal1, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					g.Expect(*result).To(ContainSubstring("journal1.json"))
					g.Expect(*result).To(ContainSubstring("broker-1.globalMem.json"))
					g.Expect(*result).To(ContainSubstring("journal2.json"))
					g.Expect(*result).NotTo(ContainSubstring("broker-0.globalMem.json"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				curlUrl = "http://" + podWithOrdinal1 + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/GlobalMaxSize"
				curlCmd = []string{"curl", "-s", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
				Eventually(func(g Gomega) {
					result, err := RunCommandInPod(podWithOrdinal1, crd.Name+"-container", curlCmd)
					g.Expect(err).To(BeNil())
					// 12M = 12 * 1024 * 1024
					g.Expect(*result).To(ContainSubstring("\"value\":12582912"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("verify status ok")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					condition := meta.FindStatusCondition(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			}

			Expect(k8sClient.Delete(ctx, crd)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bpSecret1)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bpSecret2)).To(Succeed())
		})

		It("extraMount.configMap logging config manually", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 2,
				TimeoutSeconds:      5,
				PeriodSeconds:       5,
			}

			configMap := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:         crd.Name + "-loggingconfg", // note extramounts "-logging-confg" postfix awareness trumps this
					GenerateName: "",
					Namespace:    crd.ObjectMeta.Namespace,
				},
				// mutable
			}

			customLogFilePropertiesFileName := "customLogging.properties"
			configMap.Data = map[string]string{
				customLogFilePropertiesFileName: `appender.stdout.name = STDOUT
				appender.stdout.type = Console
				rootLogger = INFO, STDOUT
				logger.tooVerbose.name=io.hawt.web
				logger.tooVerbose.level=TRACE
				`,
			}

			crd.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{configMap.Name}
			crd.Spec.Env = []corev1.EnvVar{
				{Name: "LOG4J_SIMPLELOG_SHOW_SHORT_LOGNAME", Value: "false"},
				{Name: javaArgsAppendEnvVarName, Value: "-Dlog4j2.configurationFile=file:/amq/extra/configmaps/" + configMap.Name + "/" + customLogFilePropertiesFileName},
			}

			By("Deploying the configMap " + configMap.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("verifying started")
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(len(createdCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying logging via custom map")
				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"

				Eventually(func(g Gomega) {
					stdOutContent := LogsOfPod(podWithOrdinal, crd.Name, defaultNamespace, g)
					if verbose {
						fmt.Printf("\nLOG of Pod:\n%s", stdOutContent)
					}
					g.Expect(stdOutContent).ShouldNot(ContainSubstring("INFO"))
					g.Expect(stdOutContent).ShouldNot(ContainSubstring("broker-0 does not exist"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("By checking it has gone")
			Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).To(Succeed())
		})

	})

	Context("cluster", Label("cluster"), func() {
		It("secure connections with wildcard DNS name", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				crd := generateArtemisSpec(defaultNamespace)

				tlsSecretName := crd.Name + "tls-secret"
				tlsSecret, err := CreateTlsSecret(tlsSecretName, defaultNamespace, defaultPassword, []string{
					// ref https://datatracker.ietf.org/doc/html/rfc6125#section-6.4.3
					// any ordinal of a single service
					"*." + crd.Name + "-hdls-svc." + defaultNamespace + ".svc.cluster.local",
					// any service
					"*." + defaultNamespace + ".svc.cluster.local",
				})
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, tlsSecret)).Should(Succeed())

				crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "artemis",
						Port:       61616,
						SSLEnabled: true,
						SSLSecret:  tlsSecretName,
					},
				}

				crd.Spec.BrokerProperties = []string{
					"connectorConfigurations.artemis.params.sslEnabled=true",
					"connectorConfigurations.artemis.params.forceSSLParameters=true",
					"connectorConfigurations.artemis.params.trustStorePath=/etc/" + tlsSecretName + "-volume/broker.ks",
					"connectorConfigurations.artemis.params.trustStorePassword=" + defaultPassword,
				}

				By("Deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("validating jolokia ordinal 0")
				Eventually(func(g Gomega) {
					jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-ss-0."+crd.Name+"-hdls-svc.test.svc.cluster.local", "8161", "/console/jolokia", "", "", "http")
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",component=cluster-connections,name=\"my-cluster\"/Nodes")
					g.Expect(err).To(BeNil())
					g.Expect(data.Value).Should(ContainSubstring(crd.Name+"-ss-1"), data.Value)

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("validating jolokia ordinal 1")
				Eventually(func(g Gomega) {
					jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-ss-1."+crd.Name+"-hdls-svc.test.svc.cluster.local", "8161", "/console/jolokia", "", "", "http")
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",component=cluster-connections,name=\"my-cluster\"/Nodes")
					g.Expect(err).To(BeNil())
					g.Expect(data.Value).Should(ContainSubstring(crd.Name+"-ss-0"), data.Value)

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("validating jolokia service")
				Eventually(func(g Gomega) {
					jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-hdls-svc.test.svc.cluster.local", "8161", "/console/jolokia", "", "", "http")
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",component=cluster-connections,name=\"my-cluster\"/Nodes")
					g.Expect(err).To(BeNil())
					g.Expect(data.Value).Should(ContainSubstring(crd.Name+"-ss-"), data.Value)

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(tlsSecret, tlsSecret.Name, defaultNamespace)
			}
		})

		It("secure connections with openshift serving certificate", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" && isOpenshift {
				crd := generateArtemisSpec(defaultNamespace)

				tlsSecretName := crd.Name + "-ptls"
				crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "artemis",
						Port:       61616,
						SSLEnabled: true,
						SSLSecret:  tlsSecretName,
					},
				}

				crd.Spec.BrokerProperties = []string{
					"connectorConfigurations.artemis.params.sslEnabled=true",
					"connectorConfigurations.artemis.params.forceSSLParameters=true",
					"connectorConfigurations.artemis.params.trustStorePath=/etc/" + tlsSecretName + "-volume/tls.crt",
					"connectorConfigurations.artemis.params.trustStoreType=PEM",
				}

				crd.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
					{
						Selector: &brokerv1beta1.ResourceSelector{
							Kind: ptr.To("Service"),
							Name: ptr.To(crd.Name + "-hdls-svc"),
						},
						Annotations: map[string]string{
							"service.beta.openshift.io/serving-cert-secret-name": tlsSecretName,
						},
					},
				}

				By("Deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				Eventually(func(g Gomega) {
					jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-ss-0."+crd.Name+"-hdls-svc."+defaultNamespace+".svc.cluster.local", "8161", "/console/jolokia", "", "", "http")
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",component=cluster-connections,name=\"my-cluster\"/Nodes")
					g.Expect(err).To(BeNil())
					g.Expect(data.Value).Should(ContainSubstring(crd.Name+"-ss-1"), data.Value)

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(&crd, crd.Name, defaultNamespace)
			}
		})

		It("secure connections with multiple DNS names", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				crd := generateArtemisSpec(defaultNamespace)

				tlsSecretName := crd.Name + "tls-secret"
				tlsSecret, err := CreateTlsSecret(tlsSecretName, defaultNamespace, defaultPassword, []string{
					crd.Name + "-ss-0." + crd.Name + "-hdls-svc." + defaultNamespace + ".svc.cluster.local",
					crd.Name + "-ss-1." + crd.Name + "-hdls-svc." + defaultNamespace + ".svc.cluster.local",
				})
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, tlsSecret)).Should(Succeed())

				crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "artemis",
						Port:       61616,
						SSLEnabled: true,
						SSLSecret:  tlsSecretName,
					},
				}

				crd.Spec.BrokerProperties = []string{
					"connectorConfigurations.artemis.params.sslEnabled=true",
					"connectorConfigurations.artemis.params.forceSSLParameters=true",
					"connectorConfigurations.artemis.params.trustStorePath=/etc/" + tlsSecretName + "-volume/broker.ks",
					"connectorConfigurations.artemis.params.trustStorePassword=" + defaultPassword,
				}

				By("Deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				Eventually(func(g Gomega) {
					jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-ss-0."+crd.Name+"-hdls-svc."+defaultNamespace+".svc.cluster.local", "8161", "/console/jolokia", "", "", "http")
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",component=cluster-connections,name=\"my-cluster\"/Nodes")
					g.Expect(err).To(BeNil())
					g.Expect(data.Value).Should(ContainSubstring(crd.Name+"-ss-1"), data.Value)

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(tlsSecret, tlsSecret.Name, defaultNamespace)
			}
		})

		It("secure connections with verify host disabled", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				crd := generateArtemisSpec(defaultNamespace)

				tlsSecretName := crd.Name + "tls-secret"
				tlsSecret, err := CreateTlsSecret(tlsSecretName, defaultNamespace, defaultPassword, []string{})
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, tlsSecret)).Should(Succeed())

				crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
				crd.Spec.Acceptors = []brokerv1beta1.AcceptorType{
					{
						Name:       "artemis",
						Port:       61616,
						SSLEnabled: true,
						SSLSecret:  tlsSecretName,
					},
				}

				crd.Spec.BrokerProperties = []string{
					"connectorConfigurations.artemis.params.sslEnabled=true",
					"connectorConfigurations.artemis.params.forceSSLParameters=true",
					"connectorConfigurations.artemis.params.trustStorePath=/etc/" + tlsSecretName + "-volume/broker.ks",
					"connectorConfigurations.artemis.params.trustStorePassword=" + defaultPassword,
					"connectorConfigurations.artemis.params.verifyHost=false",
				}

				By("Deploying broker" + crd.Name)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				Eventually(func(g Gomega) {
					jolokia := jolokia.GetJolokia(k8sClient, crd.Name+"-ss-0."+crd.Name+"-hdls-svc."+defaultNamespace+".svc.cluster.local", "8161", "/console/jolokia", "", "", "http")
					data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",component=cluster-connections,name=\"my-cluster\"/Nodes")
					g.Expect(err).To(BeNil())
					g.Expect(data.Value).Should(ContainSubstring(crd.Name+"-ss-1"), data.Value)

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				CleanResource(&crd, crd.Name, defaultNamespace)
				CleanResource(tlsSecret, tlsSecret.Name, defaultNamespace)
			}
		})
	})
})
