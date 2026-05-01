/*
Copyright 2026.

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

package networkpolicy

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func ptrTo[T any](v T) *T {
	return &v
}

var _ = Describe("NetworkPolicy Port Discovery", func() {

	Describe("DiscoverPorts", func() {
		Context("when no policies match", func() {
			It("should return nil ports and empty policy name", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "policy1",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "web"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(8080))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(BeNil())
				Expect(foundPolicy).To(BeFalse())
			})
		})

		Context("with single policy and single port", func() {
			It("should discover the port", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "broker-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with multiple ports in same rule", func() {
			It("should extract all TCP ports", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "multi-port-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"tier": "backend"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61617))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61618))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"tier": "backend"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616, 61617, 61618}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with port range using EndPort", func() {
			It("should expand range from start to end", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "range-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"service": "messaging"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{
											Protocol: ptrTo(corev1.ProtocolTCP),
											Port:     ptrTo(intstr.FromInt(61616)),
											EndPort:  ptrTo(int32(61620)),
										},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"service": "messaging"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(Equal([]int32{61616, 61617, 61618, 61619, 61620}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with mixed protocols", func() {
			It("should only include TCP ports", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "mixed-proto-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
										{Protocol: ptrTo(corev1.ProtocolUDP), Port: ptrTo(intstr.FromInt(53))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61617))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(Equal([]int32{61616, 61617}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("when multiple policies match same pod", func() {
			It("should combine ports from all matching policies", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "policy1",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "policy2",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"tier": "backend"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61617))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61618))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{
					"app":  "broker",
					"tier": "backend",
				}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(ConsistOf(int32(61616), int32(61617), int32(61618)))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with duplicate ports across rules", func() {
			It("should deduplicate ports", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "dup-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61617))},
									},
								},
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61618))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(Equal([]int32{61616, 61617, 61618}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with empty ingress rule", func() {
			It("should return nil for unrestricted policy", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "allow-all-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(BeNil())
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with match expressions", func() {
			It("should match using label expressions", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "expr-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "environment",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"prod", "staging"},
									},
								},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"environment": "prod"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})

			It("should not match when expression doesn't match", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "expr-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "environment",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"prod", "staging"},
									},
								},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"environment": "dev"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(BeNil())
				Expect(foundPolicy).To(BeFalse())
			})
		})

		Context("with empty pod selector", func() {
			It("should match all pods", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "all-pods-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"any": "label"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("with unsorted ports", func() {
			It("should return sorted ports", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "unsorted-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61620))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61618))},
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61617))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(Equal([]int32{61616, 61617, 61618, 61620}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("when no protocol specified", func() {
			It("should return nil when no TCP ports found", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "no-proto-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())

				Expect(ports).To(BeNil())
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("PolicyTypes handling", func() {
			It("should ignore egress-only policies", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "egress-only",
							Namespace: "default",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							PolicyTypes: []networkingv1.PolicyType{
								networkingv1.PolicyTypeEgress,
							},
							Egress: []networkingv1.NetworkPolicyEgressRule{
								{},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(BeNil())
				Expect(foundPolicy).To(BeFalse())
			})

			It("should process policies with explicit Ingress policyType", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "explicit-ingress",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							PolicyTypes: []networkingv1.PolicyType{
								networkingv1.PolicyTypeIngress,
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})

			It("should process policies with both Ingress and Egress policyTypes", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "both-types",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							PolicyTypes: []networkingv1.PolicyType{
								networkingv1.PolicyTypeIngress,
								networkingv1.PolicyTypeEgress,
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
							Egress: []networkingv1.NetworkPolicyEgressRule{
								{},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})

			It("should process policies with no policyTypes (defaults to Ingress)", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "default-ingress",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							// No PolicyTypes specified - defaults to ["Ingress"]
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("Deny-all policies", func() {
			It("should return error for ONLY deny-all (single policy, empty ingress list)", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "deny-all-ingress",
							Namespace: "default",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							PolicyTypes: []networkingv1.PolicyType{
								networkingv1.PolicyTypeIngress,
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("deny ingress"))
				Expect(ports).To(BeNil())
				Expect(foundPolicy).To(BeTrue())
			})

			It("should allow ports from union when deny-all exists with allow policy (K8s additive semantics)", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "deny-all",
							Namespace: "default",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							PolicyTypes: []networkingv1.PolicyType{
								networkingv1.PolicyTypeIngress,
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "allow-some",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				// Per K8s NetworkPolicy docs: policies are additive (union)
				// deny-all contributes {}, allow-some contributes {61616}
				// Union: {} ∪ {61616} = {61616}
				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})
		})

		Context("Invalid label selectors", func() {
			It("should skip policies with invalid selectors and continue", func() {
				policies := []networkingv1.NetworkPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "invalid-selector",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: "InvalidOp", // Invalid operator
										Values:   []string{"broker"},
									},
								},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(9999))},
									},
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "valid-policy",
						},
						Spec: networkingv1.NetworkPolicySpec{
							PodSelector: metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "broker"},
							},
							Ingress: []networkingv1.NetworkPolicyIngressRule{
								{
									Ports: []networkingv1.NetworkPolicyPort{
										{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(61616))},
									},
								},
							},
						},
					},
				}

				podLabels := map[string]string{"app": "broker"}

				ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

				Expect(err).ToNot(HaveOccurred())
				Expect(ports).To(Equal([]int32{61616}))
				Expect(foundPolicy).To(BeTrue())
			})
		})
	})

	Describe("extractIngressPorts", func() {
		Context("with empty rules", func() {
			It("should return empty slice", func() {
				policy := networkingv1.NetworkPolicy{
					Spec: networkingv1.NetworkPolicySpec{
						Ingress: []networkingv1.NetworkPolicyIngressRule{},
					},
				}

				ports := extractIngressPorts(policy)

				Expect(ports).To(BeEmpty())
			})
		})

		Context("with rules having no ports", func() {
			It("should return empty slice", func() {
				policy := networkingv1.NetworkPolicy{
					Spec: networkingv1.NetworkPolicySpec{
						Ingress: []networkingv1.NetworkPolicyIngressRule{
							{},
						},
					},
				}

				ports := extractIngressPorts(policy)

				Expect(ports).To(BeEmpty())
			})
		})
	})

	Describe("dedupAndSort", func() {
		It("should handle empty slice", func() {
			ports := dedupAndSort([]int32{})
			Expect(ports).To(BeEmpty())
		})

		It("should handle single port", func() {
			ports := dedupAndSort([]int32{61616})
			Expect(ports).To(Equal([]int32{61616}))
		})

		It("should preserve already sorted slice", func() {
			ports := dedupAndSort([]int32{61616, 61617, 61618})
			Expect(ports).To(Equal([]int32{61616, 61617, 61618}))
		})

		It("should deduplicate and sort", func() {
			ports := dedupAndSort([]int32{61618, 61616, 61617, 61616, 61618, 61617})
			Expect(ports).To(Equal([]int32{61616, 61617, 61618}))
		})

		It("should sort in ascending order", func() {
			ports := dedupAndSort([]int32{61620, 61619, 61618, 61617, 61616})
			Expect(ports).To(Equal([]int32{61616, 61617, 61618, 61619, 61620}))
		})
	})
})
