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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDiscoverAvailablePorts_NoPods(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Create BrokerService
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	// Create fake client with no Broker CR (not deployed yet)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service).
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

	// Test discovery when Broker not deployed
	poolInfo, err := reconciler.discoverAvailablePorts()

	assert.NoError(t, err, "Should not error when Broker not deployed")
	assert.Nil(t, poolInfo, "Should return nil when Broker not deployed")
}

func TestDiscoverAvailablePorts_PodsExist_NoNetworkPolicy(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Create BrokerService
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	// Create Broker CR with Deployed=True
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

	// Create StatefulSet with pod template labels
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-ss",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelAppKubernetesInstance:  serviceName,
						common.LabelAppKubernetesComponent: "broker-service",
						common.LabelAppKubernetesManagedBy: "arkmq-org-broker-operator",
						common.LabelBrokerService:          serviceName,
						common.LabelBrokerPeerIndex:        "0",
					},
				},
			},
		},
	}

	// Create fake client with Broker CR, StatefulSet, but no NetworkPolicy
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, brokerCR, ss).
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

	// Test discovery
	poolInfo, err := reconciler.discoverAvailablePorts()

	assert.NoError(t, err)
	assert.NotNil(t, poolInfo, "Should return pool info")
	assert.Equal(t, "Default", poolInfo.Source)
	assert.Nil(t, poolInfo.Ports, "Should not have explicit port list")
	assert.NotNil(t, poolInfo.PortRange, "Should have port range for default pool")
	assert.Equal(t, int32(61616), poolInfo.PortRange.Start)
	assert.Equal(t, int32(62615), poolInfo.PortRange.End)
}

func TestDiscoverAvailablePorts_PodsExist_WithNetworkPolicy(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Create BrokerService
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	// Create Broker CR with Deployed=True
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

	// Create StatefulSet with pod template labels
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-ss",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						common.LabelAppKubernetesInstance:  serviceName,
						common.LabelAppKubernetesComponent: "broker-service",
						common.LabelAppKubernetesManagedBy: "arkmq-org-broker-operator",
						common.LabelBrokerService:          serviceName,
						common.LabelBrokerPeerIndex:        "0",
					},
				},
			},
		},
	}

	// Create NetworkPolicy matching the pod
	tcpProto := corev1.ProtocolTCP
	netpol := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "broker-policy",
			Namespace: ns,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					common.LabelBrokerService: serviceName,
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

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, brokerCR, ss, netpol).
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

	// Test discovery
	poolInfo, err := reconciler.discoverAvailablePorts()

	assert.NoError(t, err)
	assert.NotNil(t, poolInfo, "Should return pool info")
	assert.Equal(t, "NetworkPolicy", poolInfo.Source)
	assert.Equal(t, []int32{61616, 61617, 61618}, poolInfo.Ports)
}

func TestDiscoverAvailablePorts_NetworkPolicyDenyAll(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	serviceName := "my-service"

	// Create BrokerService
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ns,
		},
	}

	// Create Broker CR with Deployed=True
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

	// Create StatefulSet
	ss := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName + "-ss",
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/instance": serviceName,
						"broker.arkmq.org/service":   serviceName,
					},
				},
			},
		},
	}

	// Create deny-all NetworkPolicy (empty ingress list)
	netpol := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deny-all",
			Namespace: ns,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					common.LabelBrokerService: serviceName,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{}, // Empty = deny all
		},
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, brokerCR, ss, netpol).
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

	// Test discovery - should return error for deny-all policy
	poolInfo, err := reconciler.discoverAvailablePorts()

	assert.Error(t, err, "Should error when NetworkPolicy denies all")
	assert.Contains(t, err.Error(), "deny ingress")
	assert.Nil(t, poolInfo)
}
