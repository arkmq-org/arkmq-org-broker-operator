package common_test

import (
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestConfigUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Common Utils Suite")
}

var _ = Describe("Common Util Test", func() {
	Context("TestCompareResources: Equals", func() {

		podResource1 := getResource("500m", "1024Mi", "250m", "512Mi")
		podResource2 := getResource("500m", "1024Mi", "250m", "512Mi")

		It("Testing 2 resources are equal", func() {
			result := common.CompareRequiredResources(&podResource1, &podResource2)
			Expect(result).To(BeTrue())
		})
	})
	Context("TestCompareResources: Different1", func() {

		podResource1 := getResource("500m", "1024Mi", "250m", "512Mi")
		podResource2 := getResource("501m", "1024Mi", "250m", "512Mi")

		It("Testing 2 resources are different", func() {
			result := common.CompareRequiredResources(&podResource1, &podResource2)
			Expect(result).To(BeFalse())
		})
	})
	Context("TestCompareResources: Different2", func() {

		podResource1 := getResource("500m", "1024Mi", "250m", "512Mi")
		podResource2 := getResource("500m", "1025Mi", "250m", "512Mi")

		It("Testing 2 resources are different", func() {
			result := common.CompareRequiredResources(&podResource1, &podResource2)
			Expect(result).To(BeFalse())
		})
	})
	Context("TestCompareResources: Different3", func() {

		podResource1 := getResource("500m", "1024Mi", "250m", "512Mi")
		podResource2 := getResource("500m", "1024Mi", "260m", "512Mi")

		It("Testing 2 resources are different", func() {
			result := common.CompareRequiredResources(&podResource1, &podResource2)
			Expect(result).To(BeFalse())
		})
	})
	Context("TestCompareResources: Different3", func() {

		podResource1 := getResource("500m", "1024Mi", "250m", "512Mi")
		podResource2 := getResource("500m", "1024Mi", "250m", "524Mi")

		It("Testing 2 resources are different", func() {
			result := common.CompareRequiredResources(&podResource1, &podResource2)
			Expect(result).To(BeFalse())
		})
	})
})

func getResource(lcpu_v string, lmem_v string, rcpu_v string, rmem_v string) corev1.ResourceRequirements {

	lcpu, err := resource.ParseQuantity(lcpu_v)
	Expect(err).To(BeNil())
	lmem, err := resource.ParseQuantity(lmem_v)
	Expect(err).To(BeNil())

	rcpu, err := resource.ParseQuantity(rcpu_v)
	Expect(err).To(BeNil())
	rmem, err := resource.ParseQuantity(rmem_v)
	Expect(err).To(BeNil())

	requirement := corev1.ResourceRequirements{
		Limits: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceCPU:    lcpu,
			corev1.ResourceMemory: lmem,
		},
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceCPU:    rcpu,
			corev1.ResourceMemory: rmem,
		},
	}
	return requirement
}
