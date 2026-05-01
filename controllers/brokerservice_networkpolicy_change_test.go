/*
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

package controllers

import (
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestNetworkPolicyRuntimeChange simulates a NetworkPolicy becoming more restrictive
// at runtime and validates that apps with orphaned ports are rejected
func TestNetworkPolicyRuntimeChange(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Stage 1: Initial NetworkPolicy allows 3 ports
	tcpProto := corev1.ProtocolTCP
	networkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broker-policy",
			Namespace: ns,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"broker.arkmq.org/service": serviceName,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61616}},
						{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61617}},
						{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61618}},
					},
				},
			},
		},
	}

	// StatefulSet with pod template labels
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-ss",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelAppKubernetesInstance: serviceName,
						common.LabelBrokerService:         serviceName,
					},
				},
			},
		},
	}

	// BrokerService
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	// Broker CR with Deployed=True
	brokerCR := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// Create fake client with initial state
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, brokerCR, ss, networkPolicy).
		WithStatusSubresource(service).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	reconciler := &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{
					Client: fakeClient,
					Scheme: scheme,
					log:    ctrl.Log.WithName("test"),
				},
			},
		},
		instance: service,
		status:   &v1beta2.BrokerServiceStatus{},
	}

	// First discovery - should find 3 ports
	poolInfo, err := reconciler.discoverAvailablePorts()
	assert.NoError(t, err)
	assert.NotNil(t, poolInfo)
	assert.Equal(t, "NetworkPolicy", poolInfo.Source)
	assert.Equal(t, []int32{61616, 61617, 61618}, poolInfo.Ports)

	// Simulate apps being assigned to those ports
	app1 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app1",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         serviceName,
				Namespace:    ns,
				AssignedPort: 61616,
			},
		},
	}

	app2 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app2",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         serviceName,
				Namespace:    ns,
				AssignedPort: 61617,
			},
		},
	}

	app3 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app3",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         serviceName,
				Namespace:    ns,
				AssignedPort: 61618, // This will become orphaned
			},
		},
	}

	// Add apps to client
	_ = fakeClient.Create(nil, app1)
	_ = fakeClient.Create(nil, app2)
	_ = fakeClient.Create(nil, app3)
	_ = fakeClient.Status().Update(nil, app1)
	_ = fakeClient.Status().Update(nil, app2)
	_ = fakeClient.Status().Update(nil, app3)

	// All apps valid initially
	reconciler.status.AvailablePorts = poolInfo
	err = reconciler.validateAssignedPorts(poolInfo)
	assert.NoError(t, err)
	assert.Empty(t, reconciler.status.RejectedApps, "No apps should be rejected with initial policy")

	// Stage 2: NetworkPolicy changes - NOW only allows 2 ports (more restrictive)
	networkPolicy.Spec.Ingress[0].Ports = []networkingv1.NetworkPolicyPort{
		{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61616}},
		{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61617}},
		// 61618 removed!
	}

	// Update the NetworkPolicy in the fake client
	err = fakeClient.Update(nil, networkPolicy)
	assert.NoError(t, err)

	// Re-run discovery (simulates next reconcile after NetworkPolicy change)
	poolInfo2, err := reconciler.discoverAvailablePorts()
	assert.NoError(t, err)
	assert.NotNil(t, poolInfo2)
	assert.Equal(t, "NetworkPolicy", poolInfo2.Source)
	assert.Equal(t, []int32{61616, 61617}, poolInfo2.Ports, "Should discover updated restricted ports")

	// Validate assigned ports with new pool
	reconciler.status.AvailablePorts = poolInfo2
	reconciler.status.RejectedApps = nil // Clear previous state
	err = reconciler.validateAssignedPorts(poolInfo2)
	assert.NoError(t, err)

	// app3 should now be rejected because 61618 is no longer in the pool
	assert.Len(t, reconciler.status.RejectedApps, 1, "One app should be rejected after policy change")
	assert.Equal(t, "app3", reconciler.status.RejectedApps[0].Name)
	assert.Equal(t, ns, reconciler.status.RejectedApps[0].Namespace)
	assert.Contains(t, reconciler.status.RejectedApps[0].Reason, "61618")
	assert.Contains(t, reconciler.status.RejectedApps[0].Reason, "no longer in allowed pool")
}

// TestNetworkPolicyBecomesUnrestricted tests the opposite - NetworkPolicy
// changes from restrictive to permissive
func TestNetworkPolicyBecomesUnrestricted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Stage 1: NetworkPolicy with specific ports
	tcpProto := corev1.ProtocolTCP
	networkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broker-policy",
			Namespace: ns,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"broker.arkmq.org/service": serviceName,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61616}},
						{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61617}},
					},
				},
			},
		},
	}

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-ss",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelAppKubernetesInstance: serviceName,
						common.LabelBrokerService:         serviceName,
					},
				},
			},
		},
	}

	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	brokerCR := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, brokerCR, ss, networkPolicy).
		Build()

	reconciler := &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{
					Client: fakeClient,
					Scheme: scheme,
					log:    ctrl.Log.WithName("test"),
				},
			},
		},
		instance: service,
		status:   &v1beta2.BrokerServiceStatus{},
	}

	// Initial discovery - bounded pool
	poolInfo, err := reconciler.discoverAvailablePorts()
	assert.NoError(t, err)
	assert.NotNil(t, poolInfo)
	assert.Equal(t, "NetworkPolicy", poolInfo.Source)
	assert.Equal(t, []int32{61616, 61617}, poolInfo.Ports)

	// Stage 2: NetworkPolicy changes to allow all ports (empty ingress rule)
	networkPolicy.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{}, // Empty rule = allow all ports
	}

	err = fakeClient.Update(nil, networkPolicy)
	assert.NoError(t, err)

	// Re-run discovery
	poolInfo2, err := reconciler.discoverAvailablePorts()
	assert.NoError(t, err)
	assert.NotNil(t, poolInfo2)
	assert.Equal(t, "NetworkPolicy", poolInfo2.Source)
	assert.Nil(t, poolInfo2.Ports, "Should be unbounded after policy allows all ports")
}

// TestNetworkPolicyDeleted tests what happens when NetworkPolicy is deleted
func TestNetworkPolicyDeleted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Stage 1: NetworkPolicy exists
	tcpProto := corev1.ProtocolTCP
	networkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broker-policy",
			Namespace: ns,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"broker.arkmq.org/service": serviceName,
				},
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					Ports: []networkingv1.NetworkPolicyPort{
						{Protocol: &tcpProto, Port: &intstr.IntOrString{IntVal: 61616}},
					},
				},
			},
		},
	}

	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-ss",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelAppKubernetesInstance: serviceName,
						common.LabelBrokerService:         serviceName,
					},
				},
			},
		},
	}

	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	brokerCR := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, brokerCR, ss, networkPolicy).
		Build()

	reconciler := &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{
					Client: fakeClient,
					Scheme: scheme,
					log:    ctrl.Log.WithName("test"),
				},
			},
		},
		instance: service,
		status:   &v1beta2.BrokerServiceStatus{},
	}

	// Initial discovery - finds NetworkPolicy
	poolInfo, err := reconciler.discoverAvailablePorts()
	assert.NoError(t, err)
	assert.NotNil(t, poolInfo)
	assert.Equal(t, "NetworkPolicy", poolInfo.Source)
	assert.Equal(t, []int32{61616}, poolInfo.Ports)

	// Stage 2: NetworkPolicy deleted
	err = fakeClient.Delete(nil, networkPolicy)
	assert.NoError(t, err)

	// Re-run discovery
	poolInfo2, err := reconciler.discoverAvailablePorts()
	assert.NoError(t, err)
	assert.NotNil(t, poolInfo2)
	assert.Equal(t, "Default", poolInfo2.Source, "Should fall back to default pool when NetworkPolicy deleted")
	assert.Nil(t, poolInfo2.Ports, "Should not have explicit port list")
	assert.NotNil(t, poolInfo2.PortRange, "Should have port range for default pool")
	assert.Equal(t, int32(61616), poolInfo2.PortRange.Start)
	assert.Equal(t, int32(62615), poolInfo2.PortRange.End)
}
