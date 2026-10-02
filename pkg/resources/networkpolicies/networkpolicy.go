package networkpolicies

import (
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

const (
	RestrictedJolokiaPort    = 8778
	RestrictedPrometheusPort = 8888
)

// NewNetworkPolicy builds or updates a NetworkPolicy that restricts ingress
// traffic to the broker pods, allowing only the ports the operator knows about.
// When existing is non-nil its metadata is preserved so the update path keeps
// the resource version.
func NewNetworkPolicy(existing *netv1.NetworkPolicy, name, namespace string, restricted bool, acceptorPorts []int32) *netv1.NetworkPolicy {
	desired := baseNetworkPolicy(existing, name, namespace)
	desired.Spec = *BuildNetworkPolicySpec(name, restricted, acceptorPorts)
	return desired
}

// BuildNetworkPolicySpec returns the NetworkPolicySpec used by NewNetworkPolicy.
func BuildNetworkPolicySpec(brokerName string, restricted bool, extraPorts []int32) *netv1.NetworkPolicySpec {
	return &netv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{selectors.LabelActiveMQArtemisKey: brokerName},
		},
		PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
		Ingress: []netv1.NetworkPolicyIngressRule{
			{
				Ports: buildIngressPorts(restricted, extraPorts),
			},
		},
	}
}

func baseNetworkPolicy(existing *netv1.NetworkPolicy, name, namespace string) *netv1.NetworkPolicy {
	if existing != nil {
		return existing
	}
	return &netv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-netpol",
			Namespace: namespace,
		},
	}
}

func buildIngressPorts(restricted bool, acceptorPorts []int32) []netv1.NetworkPolicyPort {
	tcp := corev1.ProtocolTCP
	seen := make(map[int32]bool)
	var ports []netv1.NetworkPolicyPort

	addPort := func(port int32) {
		if port <= 0 || seen[port] {
			return
		}
		seen[port] = true
		p := intstr.FromInt32(port)
		ports = append(ports, netv1.NetworkPolicyPort{
			Protocol: &tcp,
			Port:     &p,
		})
	}

	if restricted {
		addPort(RestrictedJolokiaPort)
		addPort(RestrictedPrometheusPort)
	}

	for _, port := range acceptorPorts {
		addPort(port)
	}

	return ports
}
