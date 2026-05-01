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

// TDD Tests for NetworkPolicy Union Semantics
// These tests demonstrate the CORRECT behavior per Kubernetes documentation:
// https://kubernetes.io/docs/concepts/services-networking/network-policies/
//
// Key principle: "Network policies are additive"
// If any policy or policies apply to a given pod for a given direction,
// the connections allowed in that direction from that pod is the UNION
// of what the applicable policies allow.

var _ = Describe("NetworkPolicy Union Semantics (TDD - Failing Tests)", func() {

	Context("Basic Union Behavior", func() {
		It("BUG DEMO: deny-all + allow-specific should allow the specific ports (union)", func() {
			policies := []networkingv1.NetworkPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deny-all-default",
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
						Name:      "allow-broker-port",
						Namespace: "default",
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

			// Per K8s docs: policies are additive (union)
			// deny-all contributes NOTHING (empty ingress list)
			// allow-broker-port allows port 61616
			// EXPECTED RESULT: port 61616 should be allowed
			//
			// CURRENT BUG: Returns error "deny ingress" because it
			// processes deny-all first and returns immediately
			Expect(err).ToNot(HaveOccurred(), "Union of deny-all + allow-specific should succeed")
			Expect(ports).To(Equal([]int32{61616}), "Should allow port 61616 from the allow policy")
			Expect(foundPolicy).To(BeTrue())
		})

		It("BUG DEMO: order independence - allow-specific + deny-all should give same result", func() {
			policies := []networkingv1.NetworkPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-broker-port",
						Namespace: "default",
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
						Name:      "deny-all-default",
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

			// Same policies as above, just reversed order
			// Should get IDENTICAL result - union should be order-independent
			//
			// CURRENT BUG: May pass if allow comes first, but behavior is
			// order-dependent which violates K8s semantics
			Expect(err).ToNot(HaveOccurred(), "Union should be order-independent")
			Expect(ports).To(Equal([]int32{61616}), "Should allow port 61616 regardless of policy order")
			Expect(foundPolicy).To(BeTrue())
		})
	})

	Context("Complex Union Scenarios", func() {
		It("BUG DEMO: multiple deny-alls + one allow should allow the specific ports", func() {
			policies := []networkingv1.NetworkPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deny-all-1",
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
						Name:      "deny-all-2",
						Namespace: "default",
					},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"tier": "backend"},
						},
						PolicyTypes: []networkingv1.PolicyType{
							networkingv1.PolicyTypeIngress,
						},
						Ingress: []networkingv1.NetworkPolicyIngressRule{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-messaging",
						Namespace: "default",
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
						},
					},
				},
			}

			podLabels := map[string]string{
				"app":  "broker",
				"tier": "backend",
			}

			ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

			// Two deny-all policies match the pod (by different labels)
			// One policy allows specific ports
			// EXPECTED: Union of {}, {}, {61616, 61617} = {61616, 61617}
			//
			// CURRENT BUG: Returns error if any deny-all is encountered first
			Expect(err).ToNot(HaveOccurred(), "Union with multiple deny-alls should still allow ports from allow policy")
			Expect(ports).To(ConsistOf(int32(61616), int32(61617)), "Should allow ports from the allow-messaging policy")
			Expect(foundPolicy).To(BeTrue())
		})

		It("BUG DEMO: deny-all + allow-all should allow all ports", func() {
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
						Name:      "allow-all-ingress",
						Namespace: "default",
					},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "broker"},
						},
						Ingress: []networkingv1.NetworkPolicyIngressRule{
							{}, // Empty rule = allow all ports
						},
					},
				},
			}

			podLabels := map[string]string{"app": "broker"}

			ports, foundPolicy, err := DiscoverPorts(policies, podLabels)

			// Union of deny-all + allow-all = allow-all
			// deny-all contributes nothing, allow-all wins
			// EXPECTED: nil (unbounded, all ports allowed)
			//
			// CURRENT BUG: May fail if deny-all processed first
			Expect(err).ToNot(HaveOccurred(), "Union of deny-all + allow-all should allow all")
			Expect(ports).To(BeNil(), "nil indicates all ports allowed")
			Expect(foundPolicy).To(BeTrue())
		})

		It("BUG DEMO: deny-all sandwiched between two allow policies", func() {
			policies := []networkingv1.NetworkPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "allow-8080",
					},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "broker"},
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deny-all-middle",
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
						Name: "allow-61616",
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

			// Union of {8080}, {}, {61616} = {8080, 61616}
			// The deny-all in the middle contributes nothing
			//
			// CURRENT BUG: Returns error when encountering deny-all in the middle
			Expect(err).ToNot(HaveOccurred(), "deny-all in middle should be ignored in union")
			Expect(ports).To(ConsistOf(int32(8080), int32(61616)), "Should union ports from both allow policies")
			Expect(foundPolicy).To(BeTrue())
		})

		It("BUG DEMO: three different allow policies should union all ports", func() {
			policies := []networkingv1.NetworkPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "policy-a",
					},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "broker"},
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "policy-b",
					},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"tier": "backend"},
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
						Name: "policy-c",
					},
					Spec: networkingv1.NetworkPolicySpec{
						PodSelector: metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "broker"},
						},
						Ingress: []networkingv1.NetworkPolicyIngressRule{
							{
								Ports: []networkingv1.NetworkPolicyPort{
									{Protocol: ptrTo(corev1.ProtocolTCP), Port: ptrTo(intstr.FromInt(9090))},
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

			// All three policies match and allow different ports
			// Union: {8080} ∪ {61616} ∪ {9090} = {8080, 61616, 9090}
			//
			// This should pass with current implementation, but including for completeness
			Expect(err).ToNot(HaveOccurred())
			Expect(ports).To(ConsistOf(int32(8080), int32(9090), int32(61616)))
			Expect(foundPolicy).To(BeTrue())
		})
	})

	Context("Edge Cases with Deny-All", func() {
		It("should error when ONLY deny-all policies exist (no union possible)", func() {
			policies := []networkingv1.NetworkPolicy{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deny-all-1",
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
						Name:      "deny-all-2",
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

			// Union of {} ∪ {} = {} (empty set)
			// ALL matching policies deny all = complete denial
			// This SHOULD error
			Expect(err).To(HaveOccurred(), "When all policies deny all, should error")
			Expect(err.Error()).To(ContainSubstring("deny ingress"))
			Expect(ports).To(BeNil())
			Expect(foundPolicy).To(BeTrue())
		})
	})
})
