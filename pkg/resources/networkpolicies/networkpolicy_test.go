package networkpolicies

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
)

func TestNewNetworkPolicyForCR_DefaultBroker(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-broker",
			Namespace: "test-ns",
		},
		Spec: v1beta2.BrokerSpec{},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)

	assert.Equal(t, "my-broker-netpol", netpol.Name)
	assert.Equal(t, "test-ns", netpol.Namespace)
	assert.Equal(t, "NetworkPolicy", netpol.TypeMeta.Kind)
	assert.Equal(t, "networking.k8s.io/v1", netpol.TypeMeta.APIVersion)

	assert.Equal(t, "my-broker", netpol.Spec.PodSelector.MatchLabels["ActiveMQArtemis"])
	assert.Contains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeIngress)
	assert.NotContains(t, netpol.Spec.PolicyTypes, netv1.PolicyTypeEgress)

	ports := collectPorts(netpol)
	assert.Contains(t, ports, int32(7800))
	assert.Contains(t, ports, int32(8161))
	assert.Contains(t, ports, int32(8778))
	assert.Contains(t, ports, int32(61616))
}

func TestNewNetworkPolicyForCR_WithAcceptors(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec: v1beta2.BrokerSpec{
			Acceptors: []v1beta2.AcceptorType{
				{Name: "amqp", Port: 5672},
				{Name: "mqtt", Port: 1883},
			},
		},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(5672))
	assert.Contains(t, ports, int32(1883))
	assert.Contains(t, ports, int32(61616), "default port still present")
}

func TestNewNetworkPolicyForCR_ConnectorsExcluded(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec: v1beta2.BrokerSpec{
			Connectors: []v1beta2.ConnectorType{
				{Name: "custom", Host: "localhost", Port: 61617},
			},
		},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	assert.NotContains(t, ports, int32(61617), "connector ports are outbound, not ingress")
}

func TestNewNetworkPolicyForCR_DeduplicatesPorts(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec: v1beta2.BrokerSpec{
			Acceptors: []v1beta2.AcceptorType{
				{Name: "all", Port: 61616},
			},
		},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	count := 0
	for _, p := range ports {
		if p == 61616 {
			count++
		}
	}
	assert.Equal(t, 1, count, "port 61616 should appear exactly once")
}

func TestNewNetworkPolicyForCR_Restricted(t *testing.T) {
	restricted := true
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec: v1beta2.BrokerSpec{
			Restricted: &restricted,
		},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(8778), "jolokia agent")
	assert.Contains(t, ports, int32(8888), "prometheus agent")
	assert.NotContains(t, ports, int32(7800), "no jgroups in restricted")
	assert.NotContains(t, ports, int32(8161), "no console in restricted")
	assert.NotContains(t, ports, int32(61616), "no all-protocols in restricted")
	assert.Len(t, ports, 2)
}

func TestNewNetworkPolicyForCR_RestrictedIgnoresCRAcceptors(t *testing.T) {
	restricted := true
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec: v1beta2.BrokerSpec{
			Restricted: &restricted,
			Acceptors: []v1beta2.AcceptorType{
				{Name: "amqp", Port: 5672},
			},
		},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	assert.NotContains(t, ports, int32(5672), "CR acceptors ignored in restricted mode")
	assert.Contains(t, ports, int32(8778))
	assert.Contains(t, ports, int32(8888))
}

func TestNewNetworkPolicyForCR_ConsolePortInDefaults(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec:       v1beta2.BrokerSpec{},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(8161), "console port is always in defaults")
}

func TestNewNetworkPolicyForCR_ReusesExisting(t *testing.T) {
	existing := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "broker-netpol",
			Namespace:       "ns",
			ResourceVersion: "12345",
		},
	}

	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec:       v1beta2.BrokerSpec{},
	}

	netpol := NewNetworkPolicyForCR(existing, cr)

	assert.Equal(t, "12345", netpol.ResourceVersion, "should preserve existing resource version")
	assert.Equal(t, "broker", netpol.Spec.PodSelector.MatchLabels["ActiveMQArtemis"])
}

func TestNewNetworkPolicyFromSpec_UsesProvidedSpec(t *testing.T) {
	tcp := corev1.ProtocolTCP
	p8778 := intstr.FromInt32(8778)
	p61617 := intstr.FromInt32(61617)
	spec := &netv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{"ActiveMQArtemis": "my-broker"},
		},
		PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
		Ingress: []netv1.NetworkPolicyIngressRule{
			{
				Ports: []netv1.NetworkPolicyPort{
					{Protocol: &tcp, Port: &p8778},
					{Protocol: &tcp, Port: &p61617},
				},
			},
		},
	}

	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "my-broker", Namespace: "ns"},
		Spec:       v1beta2.BrokerSpec{NetworkPolicy: spec},
	}

	netpol := NewNetworkPolicyFromSpec(nil, cr)

	assert.Equal(t, "my-broker-netpol", netpol.Name)
	assert.Equal(t, "ns", netpol.Namespace)
	ports := collectPorts(netpol)
	assert.Contains(t, ports, int32(8778))
	assert.Contains(t, ports, int32(61617))
	assert.Len(t, ports, 2)
}

func TestNewNetworkPolicyFromSpec_PreservesExistingMetadata(t *testing.T) {
	existing := &netv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-broker-netpol",
			Namespace:       "ns",
			ResourceVersion: "99999",
		},
	}

	tcp := corev1.ProtocolTCP
	p8888 := intstr.FromInt32(8888)
	spec := &netv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{"ActiveMQArtemis": "my-broker"},
		},
		PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
		Ingress: []netv1.NetworkPolicyIngressRule{
			{
				Ports: []netv1.NetworkPolicyPort{
					{Protocol: &tcp, Port: &p8888},
				},
			},
		},
	}

	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "my-broker", Namespace: "ns"},
		Spec:       v1beta2.BrokerSpec{NetworkPolicy: spec},
	}

	netpol := NewNetworkPolicyFromSpec(existing, cr)

	assert.Equal(t, "99999", netpol.ResourceVersion, "preserves resource version")
	ports := collectPorts(netpol)
	assert.Contains(t, ports, int32(8888))
}

func TestNewNetworkPolicyForCR_FallsBackWhenSpecNil(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{Name: "broker", Namespace: "ns"},
		Spec:       v1beta2.BrokerSpec{},
	}

	netpol := NewNetworkPolicyForCR(nil, cr)
	ports := collectPorts(netpol)

	assert.Contains(t, ports, int32(61616), "auto-generated defaults when spec.networkPolicy is nil")
	assert.Contains(t, ports, int32(8161))
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
