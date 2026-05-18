/*
Copyright 2022.
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

package common

import (
	"testing"
	"time"

	. "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConditions(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "common suite")
}

var _ = Describe("Common Conditions", func() {
	Describe("SetReadyCondition", func() {
		It("is Ready when there are no other conditions other than deployed", func() {
			conditions := []metav1.Condition{{Type: DeployedConditionType, Status: metav1.ConditionTrue}}
			SetReadyCondition(&conditions)
			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
		})
		It("changes back to Ready when there are no other conditions, apart from deployed, and Ready was false", func() {
			conditions := []metav1.Condition{
				{
					Type:    ReadyConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  "ReplaceMe",
					Message: "replace this message",
				},
				{Type: DeployedConditionType, Status: metav1.ConditionTrue},
			}
			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(ReadyConditionReason))
			Expect(ready.Message).To(BeEmpty())
		})
		It("is Ready when all conditions are True", func() {
			conditions := []metav1.Condition{
				{
					Type:   "FooCondition",
					Status: metav1.ConditionTrue,
					Reason: "FooIsOK",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{Type: DeployedConditionType, Status: metav1.ConditionTrue},
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(4))
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(ReadyConditionReason))
			Expect(ready.Message).To(BeEmpty())
		})
		It("is not Ready when one condition is False", func() {
			conditions := []metav1.Condition{
				{
					Type:    "FooCondition",
					Status:  metav1.ConditionFalse,
					Reason:  "FooHasErrors",
					Message: "Test",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				{Type: DeployedConditionType, Status: metav1.ConditionTrue},
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionFalse(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(5))
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(NotReadyConditionReason))
			Expect(ready.Message).To(Equal(NotReadyConditionMessage))
		})
		It("changes to not Ready when one condition is False and was Ready", func() {
			conditions := []metav1.Condition{
				{
					Type:    "FooCondition",
					Status:  metav1.ConditionFalse,
					Reason:  "FooHasErrors",
					Message: "Test",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				NewReadyCondition(),
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionFalse(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(4))
			ready := meta.FindStatusCondition(conditions, ReadyConditionType)
			Expect(ready.Reason).To(Equal(NotReadyConditionReason))
			Expect(ready.Message).To(Equal(NotReadyConditionMessage))
		})
		It("ignores condition in Unknown state", func() {
			conditions := []metav1.Condition{
				{
					Type:    "FooCondition",
					Status:  metav1.ConditionUnknown,
					Reason:  "Unknown condition",
					Message: "Test",
				},
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				{Type: DeployedConditionType, Status: metav1.ConditionTrue},
			}

			SetReadyCondition(&conditions)

			Expect(meta.IsStatusConditionTrue(conditions, ReadyConditionType)).To(BeTrue())
			Expect(conditions).To(HaveLen(5))
		})
	})

	Describe("IsConditionPresentAndEquals", func() {
		It("returns false when there are no conditions to compare with", func() {
			Expect(IsConditionPresentAndEqual([]metav1.Condition{}, metav1.Condition{})).To(BeFalse())
		})
		It("returns false when one field is different", func() {
			current := metav1.Condition{
				Type:               "TestCondition",
				Status:             metav1.ConditionFalse,
				Reason:             "TestHasErrors",
				Message:            "Test",
				LastTransitionTime: metav1.Now(),
			}
			other := *current.DeepCopy()
			other.Message = "OtherMessage"
			conditions := []metav1.Condition{
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				NewReadyCondition(),
				current,
			}

			Expect(IsConditionPresentAndEqual(conditions, other)).To(BeFalse())
		})
		It("returns True when only time is different", func() {
			current := metav1.Condition{
				Type:               "TestCondition",
				Status:             metav1.ConditionFalse,
				Reason:             "TestHasErrors",
				Message:            "Test",
				LastTransitionTime: metav1.Now(),
			}
			other := *current.DeepCopy()
			other.LastTransitionTime = metav1.NewTime(current.LastTransitionTime.Add(5 * time.Hour))
			conditions := []metav1.Condition{
				current,
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				NewReadyCondition(),
			}

			Expect(IsConditionPresentAndEqual(conditions, other)).To(BeTrue())
		})
	})

	Describe("IsConditionPresentAndEqualsIgnoreMessage", func() {

		It("returns false when there are no conditions to compare with", func() {
			Expect(IsConditionPresentAndEqualIgnoringMessage([]metav1.Condition{}, metav1.Condition{})).To(BeFalse())
		})
		It("returns true when message field is different", func() {
			current := metav1.Condition{
				Type:               "TestCondition",
				Status:             metav1.ConditionFalse,
				Reason:             "TestHasErrors",
				Message:            "Test",
				LastTransitionTime: metav1.Now(),
			}
			other := *current.DeepCopy()
			other.Message = "OtherMessage"
			conditions := []metav1.Condition{
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				NewReadyCondition(),
				current,
			}

			Expect(IsConditionPresentAndEqualIgnoringMessage(conditions, other)).To(BeTrue())
		})
		It("returns True when only time is different", func() {
			current := metav1.Condition{
				Type:               "TestCondition",
				Status:             metav1.ConditionFalse,
				Reason:             "TestHasErrors",
				Message:            "Test",
				LastTransitionTime: metav1.Now(),
			}
			other := *current.DeepCopy()
			other.LastTransitionTime = metav1.NewTime(current.LastTransitionTime.Add(5 * time.Hour))
			conditions := []metav1.Condition{
				current,
				{
					Type:   "BarCondition",
					Status: metav1.ConditionTrue,
					Reason: "BarIsOK",
				},
				{
					Type:   "BazCondition",
					Status: metav1.ConditionTrue,
					Reason: "BazIsOK",
				},
				NewReadyCondition(),
			}

			Expect(IsConditionPresentAndEqualIgnoringMessage(conditions, other)).To(BeTrue())
		})
	})

	Describe("PodStartingStatusDigestMessage", func() {

		It("ok on empty", func() {
			Expect(PodStartingStatusDigestMessage("empty", corev1.PodStatus{})).To(Equal("{empty}"))
		})
		It("ok on no conditions", func() {
			Expect(PodStartingStatusDigestMessage("empty", corev1.PodStatus{Phase: corev1.PodPending})).To(Equal("{empty: Pending}"))
		})
		It("ok on simple conditions no status", func() {
			Expect(PodStartingStatusDigestMessage("s",
				corev1.PodStatus{
					Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
						{
							Type: "c",
						},
					}})).To(Equal("{s: Pending [{c}]}"))
		})

		It("ok on simple condition status reason", func() {
			Expect(PodStartingStatusDigestMessage("s",
				corev1.PodStatus{
					Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
						{
							Type:   "b",
							Status: corev1.ConditionTrue,
							Reason: "BazIsOK",
						},
					}})).To(Equal("{s: Pending [{b=True BazIsOK}]}"))
		})

		It("ok on simple condition status reason message", func() {
			Expect(PodStartingStatusDigestMessage("s",
				corev1.PodStatus{
					Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
						{
							Type:    "b",
							Status:  corev1.ConditionTrue,
							Reason:  "BazIsOK",
							Message: "m",
						},
					}})).To(Equal("{s: Pending [{b=True BazIsOK m}]}"))
		})

		It("ok on simple condition status message", func() {
			Expect(PodStartingStatusDigestMessage("s",
				corev1.PodStatus{
					Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
						{
							Type:    "b",
							Status:  corev1.ConditionTrue,
							Message: "m",
						},
					}})).To(Equal("{s: Pending [{b=True m}]}"))
		})

		It("ok on twp condition status message", func() {
			Expect(PodStartingStatusDigestMessage("s",
				corev1.PodStatus{
					Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
						{
							Type:    "b",
							Status:  corev1.ConditionTrue,
							Message: "m",
						},
						{
							Type:    "c",
							Status:  corev1.ConditionTrue,
							Message: "m2",
						},
					}})).To(Equal("{s: Pending [{b=True m}{c=True m2}]}"))
		})
	})
})
