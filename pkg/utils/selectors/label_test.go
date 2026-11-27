/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package selectors

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8slabels "k8s.io/apimachinery/pkg/labels"
)

var _ = Describe("Labeler", func() {
	It("uses ActiveMQArtemis tracking label and keeps application=<name>-app", func() {
		labeler := NewActiveMQArtemisLabeler()
		labeler.Base("my-broker").Suffix("app").Generate()
		generated := labeler.Labels()

		Expect(generated).To(HaveKeyWithValue(LabelActiveMQArtemisKey, "my-broker"))
		Expect(generated).To(HaveKeyWithValue(LabelAppKey, "my-broker-app"))
		Expect(generated).To(HaveKeyWithValue(LabelPartOfKey, LabelPartOfValue))
		Expect(generated).NotTo(HaveKey(LabelBrokerKey))
		Expect(generated).NotTo(HaveKey(LabelAppKubernetesInstance))
	})

	It("builds recommended labels for Broker directly", func() {
		generated := BrokerLabels("my-broker")

		Expect(generated).To(HaveKeyWithValue(LabelAppKubernetesName, BrokerLabelAppKubernetesNameValue))
		Expect(generated).To(HaveKeyWithValue(LabelAppKubernetesInstance, "my-broker"))
		Expect(generated).To(HaveKeyWithValue(LabelPartOfKey, LabelPartOfValue))
		Expect(generated).NotTo(HaveKey(LabelBrokerKey))
		Expect(generated).NotTo(HaveKey(LabelAppKey))
		Expect(generated).NotTo(HaveKey(LabelActiveMQArtemisKey))
	})

	It("wraps precomputed labels for Namers storage", func() {
		labeler := NewStaticLabeler(BrokerLabels("my-broker"))
		Expect(labeler.Labels()).To(Equal(BrokerLabels("my-broker")))
	})

	It("keeps ActiveMQArtemis as default in GetLabels", func() {
		generated := GetLabels("ex-aao")

		Expect(generated).To(HaveKeyWithValue(LabelActiveMQArtemisKey, "ex-aao"))
		Expect(generated).To(HaveKeyWithValue(LabelAppKey, "ex-aao-app"))
		Expect(generated).To(HaveKeyWithValue(LabelPartOfKey, LabelPartOfValue))
		Expect(generated).NotTo(HaveKey(LabelBrokerKey))
	})

	It("builds a Pod cache selector for the shared part-of label", func() {
		selector := OperatorPodLabelSelector()

		Expect(selector.Matches(k8slabels.Set{
			LabelPartOfKey:          LabelPartOfValue,
			LabelAppKey:             "my-broker-app",
			LabelActiveMQArtemisKey: "my-broker",
		})).To(BeTrue())
		Expect(selector.Matches(k8slabels.Set{
			LabelPartOfKey:             LabelPartOfValue,
			LabelAppKubernetesName:     BrokerLabelAppKubernetesNameValue,
			LabelAppKubernetesInstance: "my-broker",
		})).To(BeTrue())
		Expect(selector.Matches(k8slabels.Set{
			LabelAppKey: "my-broker-app",
		})).To(BeFalse())
	})

	It("identifies Broker reserved label keys", func() {
		Expect(IsBrokerReservedLabelKey(LabelAppKubernetesInstance)).To(BeTrue())
		Expect(IsBrokerReservedLabelKey(LabelAppKubernetesName)).To(BeTrue())
		Expect(IsBrokerReservedLabelKey(LabelPartOfKey)).To(BeTrue())
		Expect(IsBrokerReservedLabelKey(LabelAppKey)).To(BeTrue())
		Expect(IsBrokerReservedLabelKey(LabelBrokerKey)).To(BeTrue())
		Expect(IsBrokerReservedLabelKey("team")).To(BeFalse())
		Expect(IsBrokerReservedLabelKey(LabelAppKubernetesComponent)).To(BeFalse())
	})
})
