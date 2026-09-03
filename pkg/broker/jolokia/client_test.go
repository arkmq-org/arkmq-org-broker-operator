package jolokia

import (
	"testing"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewStatusClient_WithServiceOwner(t *testing.T) {
	broker := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-broker",
			Namespace: "test-ns",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "BrokerService",
					Name: "my-service",
				},
			},
		},
	}

	sc := NewStatusClient(broker, nil)

	if sc.serviceOwner != "my-service" {
		t.Errorf("expected serviceOwner=%q, got %q", "my-service", sc.serviceOwner)
	}
	if sc.namespace != "test-ns" {
		t.Errorf("expected namespace=%q, got %q", "test-ns", sc.namespace)
	}
	if sc.brokerName != "my-broker" {
		t.Errorf("expected brokerName=%q, got %q", "my-broker", sc.brokerName)
	}
}

func TestNewStatusClient_NoOwner(t *testing.T) {
	broker := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "standalone-broker",
			Namespace: "default",
		},
	}

	sc := NewStatusClient(broker, nil)

	if sc.serviceOwner != "" {
		t.Errorf("expected empty serviceOwner, got %q", sc.serviceOwner)
	}
}

func TestNewStatusClient_CustomBrokerName(t *testing.T) {
	broker := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-broker",
			Namespace: "test-ns",
		},
		Spec: v1beta2.BrokerSpec{
			Env: []corev1.EnvVar{
				{Name: "AMQ_NAME", Value: "custom-name"},
			},
		},
	}

	sc := NewStatusClient(broker, nil)

	if sc.brokerName != "custom-name" {
		t.Errorf("expected brokerName=%q, got %q", "custom-name", sc.brokerName)
	}
}

func TestOwningServiceName(t *testing.T) {
	tests := []struct {
		name     string
		ownerRef []metav1.OwnerReference
		want     string
	}{
		{
			name:     "no owner",
			ownerRef: nil,
			want:     "",
		},
		{
			name: "non-service owner",
			ownerRef: []metav1.OwnerReference{
				{Kind: "SomethingElse", Name: "foo"},
			},
			want: "",
		},
		{
			name: "broker service owner",
			ownerRef: []metav1.OwnerReference{
				{Kind: "BrokerService", Name: "svc-1"},
			},
			want: "svc-1",
		},
		{
			name: "multiple owners picks broker service",
			ownerRef: []metav1.OwnerReference{
				{Kind: "Other", Name: "other"},
				{Kind: "BrokerService", Name: "svc-2"},
			},
			want: "svc-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := &v1beta2.Broker{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: tt.ownerRef,
				},
			}
			got := owningServiceName(broker)
			if got != tt.want {
				t.Errorf("owningServiceName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCertNamingConsistency(t *testing.T) {
	serviceName := "my-service"
	broker := &v1beta2.Broker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "BrokerService", Name: serviceName},
			},
		},
	}

	sc := NewStatusClient(broker, nil)

	if sc.serviceOwner != serviceName {
		t.Fatalf("serviceOwner mismatch")
	}
	expectedCA := cot.RootCertSecretName(serviceName)
	expectedCert := cot.OperatorCertName(serviceName)

	if expectedCA != serviceName+"-root-cert-secret" {
		t.Errorf("CA naming mismatch: %s", expectedCA)
	}
	if expectedCert != serviceName+"-operator-cert" {
		t.Errorf("operator cert naming mismatch: %s", expectedCert)
	}
}
