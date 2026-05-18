package controllers

import (
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestShouldPropagateWatch_ValidAndDeployed verifies that watches propagate
// when the referenced app is Valid=True AND Deployed=True
func TestShouldPropagateWatch_ValidAndDeployed(t *testing.T) {
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
	assert.True(t, result, "Should propagate when Valid=True AND Deployed=True")
}

// TestShouldPropagateWatch_Invalid verifies that watches do NOT propagate
// when the referenced app is Valid=False (even if Deployed=True)
func TestShouldPropagateWatch_Invalid(t *testing.T) {
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
	assert.False(t, result, "Should NOT propagate when Valid=False (prevents premature consumer unbinding)")
}

// TestShouldPropagateWatch_NotDeployed verifies that watches do NOT propagate
// when the referenced app is Valid=True but Deployed=False
func TestShouldPropagateWatch_NotDeployed(t *testing.T) {
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
	assert.False(t, result, "Should NOT propagate when Deployed=False (app not yet applied to broker)")
}

// TestShouldPropagateWatch_Deleted verifies that watches DO propagate
// when the referenced app is being deleted (even if invalid)
func TestShouldPropagateWatch_Deleted(t *testing.T) {
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
	assert.True(t, result, "Should propagate when being deleted (consumers must unbind)")
}

// TestShouldPropagateWatch_NoConditions verifies that watches do NOT propagate
// when the app has no status conditions (initial state)
func TestShouldPropagateWatch_NoConditions(t *testing.T) {
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
	assert.False(t, result, "Should NOT propagate when app has no conditions (not yet reconciled)")
}

// TestShouldPropagateWatch_MissingValidCondition verifies that watches do NOT propagate
// when Valid condition is missing (even if Deployed=True)
func TestShouldPropagateWatch_MissingValidCondition(t *testing.T) {
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
	assert.False(t, result, "Should NOT propagate when Valid condition is missing")
}

// TestShouldPropagateWatch_MissingDeployedCondition verifies that watches do NOT propagate
// when Deployed condition is missing (even if Valid=True)
func TestShouldPropagateWatch_MissingDeployedCondition(t *testing.T) {
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
	assert.False(t, result, "Should NOT propagate when Deployed condition is missing")
}

// TestShouldPropagateWatch_BothInvalid verifies that watches do NOT propagate
// when both Valid=False AND Deployed=False
func TestShouldPropagateWatch_BothInvalid(t *testing.T) {
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
	assert.False(t, result, "Should NOT propagate when both Valid=False AND Deployed=False")
}
