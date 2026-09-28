package environments

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEnvironments(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Environments Suite")
}
