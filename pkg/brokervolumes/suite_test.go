package brokervolumes

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBrokerVolumes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BrokerVolumes Suite")
}
