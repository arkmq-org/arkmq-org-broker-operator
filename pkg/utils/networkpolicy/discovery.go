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
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// DiscoverPorts extracts allowed ports from NetworkPolicies matching pod labels.
// Returns:
//   - ports: List of allowed ports (nil = unbounded/no restrictions)
//   - foundPolicy: true if any NetworkPolicy selected this pod (isolated), false if non-isolated
//   - error: If pod is isolated but no ports allowed (deny-all), or parse errors
//
// NetworkPolicy semantics:
//   - Policies are additive (union) - if ANY allows traffic, it's permitted
//   - A pod is isolated for ingress only if a policy with "Ingress" in policyTypes selects it
//   - Empty ingress list (ingress: []) means that policy contributes nothing to the union
//   - Ingress rule with no ports means allow all ports
//   - If policyTypes omitted, defaults to ["Ingress"]
//   - Only error if ALL matching policies have empty ingress lists (complete denial)
func DiscoverPorts(policies []networkingv1.NetworkPolicy, podLabels map[string]string) ([]int32, bool, error) {
	var allPorts []int32
	var foundIngressPolicy bool
	var hasAllowAllPolicy bool
	var allPoliciesAreDenyAll = true

	for _, policy := range policies {
		// Only consider policies that affect ingress
		if !hasIngressPolicyType(policy) {
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		if err != nil {
			// Invalid selector in NetworkPolicy - skipping as it won't apply
			continue
		}

		if selector.Matches(labels.Set(podLabels)) {
			foundIngressPolicy = true

			// Empty ingress list = this policy allows nothing (contributes {} to union)
			// Don't return early - other policies may still allow traffic
			if len(policy.Spec.Ingress) == 0 {
				continue // Skip this policy, process others
			}

			// This policy allows something (not deny-all)
			allPoliciesAreDenyAll = false

			ports := extractIngressPorts(policy)
			if len(ports) > 0 {
				// Policy specifies specific ports
				allPorts = append(allPorts, ports...)
			} else {
				// Ingress rules exist but no port restrictions = allow all ports
				hasAllowAllPolicy = true
				// Could optimize with early return here, but continue for completeness
			}
		}
	}

	// No policies selected this pod for ingress = non-isolated, allow all
	if !foundIngressPolicy {
		return nil, false, nil
	}

	// If any policy allows all ports, the union allows all
	if hasAllowAllPolicy {
		return nil, true, nil
	}

	// If specific ports collected from union, return them
	if len(allPorts) > 0 {
		return dedupAndSort(allPorts), true, nil
	}

	// All matching policies had ingress: [] = complete denial
	if allPoliciesAreDenyAll {
		return nil, true, fmt.Errorf("all matching NetworkPolicies deny ingress (empty ingress lists)")
	}

	// Defensive - shouldn't reach here
	return nil, true, fmt.Errorf("NetworkPolicy matched but no ports allowed")
}

// hasIngressPolicyType checks if the policy affects ingress traffic.
// Per K8s docs: if policyTypes is omitted, defaults to ["Ingress"]
// (or ["Ingress", "Egress"] if egress rules exist)
func hasIngressPolicyType(policy networkingv1.NetworkPolicy) bool {
	// If policyTypes not specified, defaults to Ingress
	if len(policy.Spec.PolicyTypes) == 0 {
		return true
	}

	for _, pt := range policy.Spec.PolicyTypes {
		if pt == networkingv1.PolicyTypeIngress {
			return true
		}
	}
	return false
}

func extractIngressPorts(policy networkingv1.NetworkPolicy) []int32 {
	var ports []int32

	for _, ingressRule := range policy.Spec.Ingress {
		for _, port := range ingressRule.Ports {
			if port.Port != nil && port.Protocol != nil && *port.Protocol == corev1.ProtocolTCP {
				ports = append(ports, port.Port.IntVal)
				if port.EndPort != nil {
					for p := port.Port.IntVal + 1; p <= *port.EndPort; p++ {
						ports = append(ports, p)
					}
				}
			}
		}
	}

	return ports
}

func dedupAndSort(ports []int32) []int32 {
	if len(ports) == 0 {
		return ports
	}

	seen := make(map[int32]bool)
	var unique []int32

	for _, port := range ports {
		if !seen[port] {
			seen[port] = true
			unique = append(unique, port)
		}
	}

	sort.Slice(unique, func(i, j int) bool {
		return unique[i] < unique[j]
	})

	return unique
}
