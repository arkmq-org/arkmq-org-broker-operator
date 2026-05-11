package networkpolicies

import (
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/serviceports"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/selectors"
)

const (
	RestrictedJolokiaPort    = 8778
	RestrictedPrometheusPort = 8888
)

// NewNetworkPolicyForCR builds or updates a NetworkPolicy that restricts ingress
// traffic to the broker pods, allowing only the ports the operator knows about.
// When existing is non-nil its metadata is preserved so the update path keeps the
// resource version; otherwise a fresh object is created.
func NewNetworkPolicyForCR(existing *netv1.NetworkPolicy, cr *v1beta2.Broker) *netv1.NetworkPolicy {
	var desired *netv1.NetworkPolicy
	if existing != nil {
		desired = existing
	} else {
		desired = &netv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "networking.k8s.io/v1",
				Kind:       "NetworkPolicy",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name + "-netpol",
				Namespace: cr.Namespace,
			},
		}
	}

	desired.Spec = netv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{selectors.LabelResourceKey: cr.Name},
		},
		PolicyTypes: []netv1.PolicyType{netv1.PolicyTypeIngress},
		Ingress: []netv1.NetworkPolicyIngressRule{
			{
				Ports: buildIngressPorts(cr),
			},
		},
	}

	return desired
}

// NewNetworkPolicyFromSpec builds a NetworkPolicy using the spec provided in the
// Broker CR directly, bypassing auto-generation. When existing is non-nil its
// metadata is preserved for the update path.
func NewNetworkPolicyFromSpec(existing *netv1.NetworkPolicy, cr *v1beta2.Broker) *netv1.NetworkPolicy {
	var desired *netv1.NetworkPolicy
	if existing != nil {
		desired = existing
	} else {
		desired = &netv1.NetworkPolicy{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "networking.k8s.io/v1",
				Kind:       "NetworkPolicy",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cr.Name + "-netpol",
				Namespace: cr.Namespace,
			},
		}
	}

	desired.Spec = *cr.Spec.NetworkPolicy
	return desired
}

// buildIngressPorts collects the TCP ports that the NetworkPolicy should allow.
// In restricted mode only the operator-managed Jolokia and Prometheus agent ports
// are included. Otherwise the default headless service ports are combined with any
// acceptor ports declared in the CR, deduplicating as it goes.
// Connector ports are excluded because they represent outbound connections; the
// receiving broker's acceptor already opens the corresponding ingress port.
// Ports configured solely through broker properties are intentionally excluded;
// users must open those via ResourceTemplates.
func buildIngressPorts(cr *v1beta2.Broker) []netv1.NetworkPolicyPort {
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

	restricted := common.IsRestricted(cr)

	if restricted {
		addPort(RestrictedJolokiaPort)
		addPort(RestrictedPrometheusPort)
	} else {
		for _, sp := range *serviceports.GetDefaultPorts(false) {
			addPort(sp.Port)
		}
	}

	if !restricted {
		for _, acceptor := range cr.Spec.Acceptors {
			addPort(acceptor.Port)
		}
	}

	return ports
}
