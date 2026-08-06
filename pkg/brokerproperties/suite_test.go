package brokerproperties

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBrokerProperties(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BrokerProperties Suite")
}
