package controllers

import (
	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("shouldPropagateWatchForReferencedApp", func() {
	Context("watch propagation", func() {
		It("should propagate when Valid=True and Deployed=True", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.ValidConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.ValidConditionSuccessReason,
						},
						{
							Type:   broker.DeployedConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.DeployedConditionProvisionedReason,
						},
					},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeTrue(), "Should propagate when Valid=True AND Deployed=True")
		})
		It("should not propagate when Valid=False", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.ValidConditionType,
							Status: v1.ConditionFalse,
							Reason: broker.ValidConditionAddressTypeError,
						},
						{
							Type:   broker.DeployedConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.DeployedConditionProvisionedReason,
						},
					},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeFalse(), "Should NOT propagate when Valid=False (prevents premature consumer unbinding)")
		})
		It("should not propagate when Deployed=False", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.ValidConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.ValidConditionSuccessReason,
						},
						{
							Type:   broker.DeployedConditionType,
							Status: v1.ConditionFalse,
							Reason: broker.DeployedConditionProvisioningPendingReason,
						},
					},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeFalse(), "Should NOT propagate when Deployed=False (app not yet applied to broker)")

		})
		It("should propagate when the app has a deletion timestamp", func() {
			now := v1.Now()
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:              "owner-app",
					Namespace:         "default",
					DeletionTimestamp: &now,
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.ValidConditionType,
							Status: v1.ConditionFalse,
							Reason: broker.ValidConditionAddressTypeError,
						},
						{
							Type:   broker.DeployedConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.DeployedConditionProvisionedReason,
						},
					},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeTrue(), "Should propagate when being deleted (consumers must unbind)")
		})
		It("should not propagate when app has no conditions", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeFalse(), "Should NOT propagate when app has no conditions (not yet reconciled)")
		})

		It("should not propagate when the Valid condition is absent", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.DeployedConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.DeployedConditionProvisionedReason,
						},
					},
				},
			}
			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeFalse(), "Should NOT propagate when Valid condition is missing")
		})
		It("should not propagate when the Deployed condition is absent", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.ValidConditionType,
							Status: v1.ConditionTrue,
							Reason: broker.ValidConditionSuccessReason,
						},
					},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeFalse(), "Should NOT propagate when Deployed condition is missing")
		})
		It("should not propagate when both Valid and Deployed are False", func() {
			app := &broker.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "owner-app",
					Namespace: "default",
				},
				Status: broker.BrokerAppStatus{
					Conditions: []v1.Condition{
						{
							Type:   broker.ValidConditionType,
							Status: v1.ConditionFalse,
							Reason: broker.ValidConditionAddressTypeError,
						},
						{
							Type:   broker.DeployedConditionType,
							Status: v1.ConditionFalse,
							Reason: broker.DeployedConditionProvisioningPendingReason,
						},
					},
				},
			}

			result := shouldPropagateWatchForReferencedApp(app)
			Expect(result).To(BeFalse(), "Should NOT propagate when both Valid=False AND Deployed=False")

		})
	})
})
