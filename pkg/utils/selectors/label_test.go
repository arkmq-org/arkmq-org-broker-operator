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
	It("uses ActiveMQArtemis tracking label and Artemis application value", func() {
		labeler := NewActiveMQArtemisLabeler()
		labeler.Base("my-broker").Suffix("app").Generate()
		generated := labeler.Labels()

		Expect(generated).To(HaveKeyWithValue(LabelActiveMQArtemisKey, "my-broker"))
		Expect(generated).To(HaveKeyWithValue(LabelAppKey, LabelAppValueArtemis))
		Expect(generated).NotTo(HaveKey(LabelBrokerKey))
	})

	It("uses Broker tracking label and Broker application value", func() {
		labeler := NewBrokerLabeler()
		labeler.Base("my-broker").Suffix("app").Generate()
		generated := labeler.Labels()

		Expect(generated).To(HaveKeyWithValue(LabelBrokerKey, "my-broker"))
		Expect(generated).To(HaveKeyWithValue(LabelAppKey, LabelAppValueBroker))
		Expect(generated).NotTo(HaveKey(LabelActiveMQArtemisKey))
	})

	It("keeps ActiveMQArtemis as default in GetLabels", func() {
		generated := GetLabels("ex-aao")

		Expect(generated).To(HaveKeyWithValue(LabelActiveMQArtemisKey, "ex-aao"))
		Expect(generated).To(HaveKeyWithValue(LabelAppKey, LabelAppValueArtemis))
		Expect(generated).NotTo(HaveKey(LabelBrokerKey))
	})

	It("builds a Pod cache selector for application in (Artemis, Broker)", func() {
		selector := OperatorPodLabelSelector()

		Expect(selector.Matches(k8slabels.Set{
			LabelAppKey:             LabelAppValueArtemis,
			LabelActiveMQArtemisKey: "my-broker",
		})).To(BeTrue())
		Expect(selector.Matches(k8slabels.Set{
			LabelAppKey:    LabelAppValueBroker,
			LabelBrokerKey: "my-broker",
		})).To(BeTrue())
		Expect(selector.Matches(k8slabels.Set{
			LabelAppKey: "unrelated",
		})).To(BeFalse())
	})
})
