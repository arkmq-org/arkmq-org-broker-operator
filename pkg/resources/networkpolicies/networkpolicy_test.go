package networkpolicies

import (
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NetworkPolicy", func() {

	Describe("NewNetworkPolicy", func() {

		Context("with no acceptors and non-restricted mode", func() {
			It("sets metadata and selector correctly", func() {
				netpol := NewNetworkPolicy(nil, "my-broker", "test-ns", false, nil)

				Expect(netpol.Name).To(Equal("my-broker-netpol"))
				Expect(netpol.Namespace).To(Equal("test-ns"))
				Expect(netpol.Kind).To(Equal("NetworkPolicy"))
				Expect(netpol.APIVersion).To(Equal("networking.k8s.io/v1"))

				Expect(netpol.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("ActiveMQArtemis", "my-broker"))
				Expect(netpol.Spec.PolicyTypes).To(ContainElement(netv1.PolicyTypeIngress))
				Expect(netpol.Spec.PolicyTypes).NotTo(ContainElement(netv1.PolicyTypeEgress))

				ports := collectPorts(netpol)
				Expect(ports).To(BeEmpty(), "no ports without acceptors in non-restricted mode")
			})

			It("produces deny-all with empty ingress rules", func() {
				netpol := NewNetworkPolicy(nil, "my-broker", "test-ns", false, nil)

				Expect(netpol.Spec.Ingress).To(BeEmpty(), "no ingress rules means deny-all, not allow-all")
			})
		})

		It("does not include egress rules", func() {
			netpol := NewNetworkPolicy(nil, "broker", "ns", false, nil)

			Expect(netpol.Spec.Egress).To(BeEmpty(), "no egress rules — ingress-only policy")
			Expect(netpol.Spec.PolicyTypes).NotTo(ContainElement(netv1.PolicyTypeEgress))
		})

		It("includes declared acceptor ports", func() {
			netpol := NewNetworkPolicy(nil, "broker", "ns", false, []int32{5672, 1883})
			ports := collectPorts(netpol)

			Expect(ports).To(ContainElement(int32(5672)))
			Expect(ports).To(ContainElement(int32(1883)))
			Expect(ports).To(HaveLen(2), "only declared acceptor ports")
		})

		It("deduplicates ports", func() {
			netpol := NewNetworkPolicy(nil, "broker", "ns", false, []int32{5672, 5672})
			ports := collectPorts(netpol)

			Expect(ports).To(HaveLen(1), "duplicate acceptor port should appear exactly once")
			Expect(ports).To(ContainElement(int32(5672)))
		})

		Context("in restricted mode", func() {
			It("includes only jolokia and prometheus ports", func() {
				netpol := NewNetworkPolicy(nil, "broker", "ns", true, nil)
				ports := collectPorts(netpol)

				Expect(ports).To(ContainElement(int32(8778)), "jolokia agent")
				Expect(ports).To(ContainElement(int32(8888)), "prometheus agent")
				Expect(ports).NotTo(ContainElement(int32(7800)), "no jgroups in restricted")
				Expect(ports).NotTo(ContainElement(int32(8161)), "no console in restricted")
				Expect(ports).NotTo(ContainElement(int32(61616)), "no all-protocols in restricted")
				Expect(ports).To(HaveLen(2))
			})

			It("adds extra ports alongside restricted base ports", func() {
				netpol := NewNetworkPolicy(nil, "broker", "ns", true, []int32{5672, 1883})
				ports := collectPorts(netpol)

				Expect(ports).To(ContainElement(int32(8778)), "jolokia base port")
				Expect(ports).To(ContainElement(int32(8888)), "prometheus base port")
				Expect(ports).To(ContainElement(int32(5672)), "extra port included")
				Expect(ports).To(ContainElement(int32(1883)), "extra port included")
				Expect(ports).NotTo(ContainElement(int32(61616)), "no default ports in restricted mode")
				Expect(ports).To(HaveLen(4))
			})
		})

		It("preserves resource version from existing object", func() {
			existing := &netv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "broker-netpol",
					Namespace:       "ns",
					ResourceVersion: "12345",
				},
			}

			netpol := NewNetworkPolicy(existing, "broker", "ns", false, nil)

			Expect(netpol.ResourceVersion).To(Equal("12345"), "should preserve existing resource version")
			Expect(netpol.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("ActiveMQArtemis", "broker"))
		})
	})

	Describe("BuildNetworkPolicySpec", func() {
		It("builds restricted spec with extra ports", func() {
			spec := BuildNetworkPolicySpec("my-svc", true, []int32{61617, 61618})

			var ports []int32
			for _, rule := range spec.Ingress {
				for _, p := range rule.Ports {
					if p.Port != nil {
						ports = append(ports, int32(p.Port.IntValue()))
					}
				}
			}

			Expect(ports).To(ContainElement(int32(8778)))
			Expect(ports).To(ContainElement(int32(8888)))
			Expect(ports).To(ContainElement(int32(61617)))
			Expect(ports).To(ContainElement(int32(61618)))
			Expect(ports).To(HaveLen(4))

			Expect(spec.PodSelector.MatchLabels).To(HaveKeyWithValue("ActiveMQArtemis", "my-svc"))
			Expect(spec.PolicyTypes).To(ContainElement(netv1.PolicyTypeIngress))
			Expect(spec.PolicyTypes).NotTo(ContainElement(netv1.PolicyTypeEgress))
			Expect(spec.Egress).To(BeEmpty())
		})
	})
})

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
