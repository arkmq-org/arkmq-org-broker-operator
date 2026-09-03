package chainoftrust

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestChainOfTrust(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ChainOfTrust Suite")
}
