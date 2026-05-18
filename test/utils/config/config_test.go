package config_test

import (
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfigUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Config Utils Suite")
}

var _ = Describe("Config Util Test", func() {
	Context("TestAddressSettingsEqual", func() {
		dlq1 := "DLQ"
		dlq2 := "DLQABC"
		dlq3 := "jmsdlq"
		dlq4 := "DLQoutgoingxxxx"

		dlq1a := "DLQ"
		dlq2a := "DLQABC"
		dlq3a := "jmsdlq"
		dlq4a := "SomethingDifferent"

		mergeAll := "merge_all"
		mergeReplace := "merge_replace"

		enableMetrics1 := true
		defaultConsumerWindowSize1 := int32(2048000)
		maxSizeBytes1 := "10m"
		defaultGroupBuckets1 := int32(10)

		var addressSettings = [...]v2alpha3.AddressSettingType{
			{
				Match:             "#",
				DeadLetterAddress: &dlq1,
				EnableMetrics:     &enableMetrics1,
			},
			{
				Match:                     "abc#",
				DeadLetterAddress:         &dlq2,
				DefaultConsumerWindowSize: &defaultConsumerWindowSize1,
				MaxSizeBytes:              &maxSizeBytes1,
			},
			{
				Match:             "jms",
				DeadLetterAddress: &dlq3,
			},
			{
				Match:               "outgoingxx",
				DeadLetterAddress:   &dlq4,
				DefaultGroupBuckets: &defaultGroupBuckets1,
			},
		}

		var addressSettings2 = [...]v2alpha3.AddressSettingType{
			{
				Match:             "#",
				DeadLetterAddress: &dlq1a,
				EnableMetrics:     &enableMetrics1,
			},
			{
				Match:                     "abc#",
				DeadLetterAddress:         &dlq2a,
				DefaultConsumerWindowSize: &defaultConsumerWindowSize1,
				MaxSizeBytes:              &maxSizeBytes1,
			},
			{
				Match:             "jms",
				DeadLetterAddress: &dlq3a,
			},
			{
				Match:               "outgoingxx",
				DeadLetterAddress:   &dlq4a,
				DefaultGroupBuckets: &defaultGroupBuckets1,
			},
		}

		It("Testing equal", func() {
			result := config.IsEqual(addressSettings[:], addressSettings[:])
			Expect(result).To(BeTrue())
			addressSettingsType := v2alpha3.AddressSettingsType{
				ApplyRule:      &mergeAll,
				AddressSetting: addressSettings[:],
			}
			newAddressSettingsType := v2alpha3.AddressSettingsType{
				ApplyRule:      &mergeReplace,
				AddressSetting: []v2alpha3.AddressSettingType{},
			}
			result = (*addressSettingsType.ApplyRule) == (*newAddressSettingsType.ApplyRule)
			Expect(result).To(BeFalse())

			addressSettingsType.DeepCopyInto(&newAddressSettingsType)

			result = (*addressSettingsType.ApplyRule) == (*newAddressSettingsType.ApplyRule)
			Expect(result).To(BeTrue())

			result = config.IsEqual(addressSettings[:], newAddressSettingsType.AddressSetting)
			Expect(result).To(BeTrue())

			result = config.IsEqual(addressSettings2[:], addressSettings[:])
			Expect(result).To(BeFalse())
		})
	})
})
