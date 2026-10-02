package networkpolicies

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNetworkPolicies(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NetworkPolicies Suite")
}
