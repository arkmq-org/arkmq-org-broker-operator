package networkpolicies

import (
	"testing"

	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
)

func TestNewNetworkPolicy_NoAcceptors(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "my-broker", "test-ns", false, nil)

	assert.Equal(t, "my-broker-netpol", netpol.Name)
	assert.Equal(t, "test-ns", netpol.Namespace)
	assert.Equal(t, "NetworkPolicy", netpol.Kind)
	assert.Equal(t, "networking.k8s.io/v1", netpol.APIVersion)

	assert.Equal(t, "my-broker", netpol.Spec.PodSelector.MatchLabels["ActiveMQArtemis"])
	assert.Contains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeIngress)
	assert.NotContains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeEgress)

	ports := collectPorts(netpol)
	assert.Empty(t, ports, "no ports without acceptors in non-restricted mode")
}

func TestNewNetworkPolicy_NoEgressRule(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", false, nil)

	assert.Empty(t, netpol.Spec.Egress, "no egress rules — ingress-only policy")
	assert.NotContains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeEgress)
}

func TestNewNetworkPolicy_WithAcceptorPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", false, []int32{5672, 1883})
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(5672))
	assert.Contains(t, ports, int32(1883))
	assert.Len(t, ports, 2, "only declared acceptor ports")
}

func TestNewNetworkPolicy_DeduplicatesPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", false, []int32{5672, 5672})
	ports := collectPorts(netpol)

	assert.Len(t, ports, 1, "duplicate acceptor port should appear exactly once")
	assert.Contains(t, ports, int32(5672))
}

func TestNewNetworkPolicy_Restricted(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", true, nil)
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(8778), "jolokia agent")
	assert.Contains(t, ports, int32(8888), "prometheus agent")
	assert.NotContains(t, ports, int32(7800), "no jgroups in restricted")
	assert.NotContains(t, ports, int32(8161), "no console in restricted")
	assert.NotContains(t, ports, int32(61616), "no all-protocols in restricted")
	assert.Len(t, ports, 2)
}

func TestNewNetworkPolicy_RestrictedWithExtraPorts(t *testing.T) {
	netpol := NewNetworkPolicy(nil, "broker", "ns", true, []int32{5672, 1883})
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(8778), "jolokia base port")
	assert.Contains(t, ports, int32(8888), "prometheus base port")
	assert.Contains(t, ports, int32(5672), "extra port included")
	assert.Contains(t, ports, int32(1883), "extra port included")
	assert.NotContains(t, ports, int32(61616), "no default ports in restricted mode")
	assert.Len(t, ports, 4)
}

func TestBuildNetworkPolicySpec_RestrictedWithExtraPorts(t *testing.T) {
	spec := BuildNetworkPolicySpec("my-svc", true, []int32{61617, 61618})

	var ports []int32
	for _, rule := range spec.Ingress {
		for _, p := range rule.Ports {
			if p.Port != nil {
				ports = append(ports, int32(p.Port.IntValue()))
			}
		}
	}

	assert.Contains(t, ports, int32(8778))
	assert.Contains(t, ports, int32(8888))
	assert.Contains(t, ports, int32(61617))
	assert.Contains(t, ports, int32(61618))
	assert.Len(t, ports, 4)

	assert.Equal(t, "my-svc", spec.PodSelector.MatchLabels["ActiveMQArtemis"])
	assert.Contains(t, spec.PolicyTypes, netv1.PolicyTypeIngress)
	assert.NotContains(t, spec.PolicyTypes, netv1.PolicyTypeEgress)
	assert.Empty(t, spec.Egress)
}

func TestNewNetworkPolicy_ReusesExisting(t *testing.T) {
	existing := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "broker-netpol",
			Namespace:       "ns",
			ResourceVersion: "12345",
		},
	}

	netpol := NewNetworkPolicy(existing, "broker", "ns", false, nil)

	assert.Equal(t, "12345", netpol.ResourceVersion, "should preserve existing resource version")
	assert.Equal(t, "broker", netpol.Spec.PodSelector.MatchLabels["ActiveMQArtemis"])
}

func collectPorts(netpol *netv1.NetworkPolicy) []int32 {
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
