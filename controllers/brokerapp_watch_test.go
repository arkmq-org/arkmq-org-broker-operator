package controllers

import (
	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("brokerapp watch propagation", func() {

	It("propagates when valid and deployed", Label(unitLabel), func() {
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

	It("does not propagate when invalid", Label(unitLabel), func() {
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
		Expect(result).To(BeFalse(), "Should NOT propagate when Valid=False")
	})

	It("does not propagate when not deployed", Label(unitLabel), func() {
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
		Expect(result).To(BeFalse(), "Should NOT propagate when Deployed=False")
	})

	It("propagates when being deleted", Label(unitLabel), func() {
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
		Expect(result).To(BeTrue(), "Should propagate when being deleted")
	})

	It("does not propagate with no conditions", Label(unitLabel), func() {
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
		Expect(result).To(BeFalse(), "Should NOT propagate when app has no conditions")
	})

	It("does not propagate with missing valid condition", Label(unitLabel), func() {
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

	It("does not propagate with missing deployed condition", Label(unitLabel), func() {
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

	It("does not propagate when both invalid and not deployed", Label(unitLabel), func() {
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
