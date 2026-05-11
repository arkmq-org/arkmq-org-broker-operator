package networkpolicies

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNetworkpolicies(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Networkpolicies Suite")
}
