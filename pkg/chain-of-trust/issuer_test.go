package chainoftrust

import (
	"testing"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

func TestBuildSelfSignedIssuer(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "test-svc",
		ServiceNamespace: "test-ns",
	}

	issuer := BuildSelfSignedIssuer(cfg)

	if issuer.Name != "test-svc-root-issuer" {
		t.Errorf("name = %q, want %q", issuer.Name, "test-svc-root-issuer")
	}
	if issuer.Namespace != "test-ns" {
		t.Errorf("namespace = %q, want %q", issuer.Namespace, "test-ns")
	}
	if issuer.Spec.SelfSigned == nil {
		t.Fatal("SelfSigned issuer config must not be nil")
	}
	if issuer.Spec.CA != nil {
		t.Error("CA issuer config must be nil for self-signed")
	}
	if issuer.Kind != "Issuer" {
		t.Errorf("kind = %q, want Issuer", issuer.Kind)
	}
	if issuer.APIVersion != cmv1.SchemeGroupVersion.String() {
		t.Errorf("apiVersion = %q, want %q", issuer.APIVersion, cmv1.SchemeGroupVersion.String())
	}
	if issuer.Labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("missing managed-by label")
	}
}

func TestBuildCAIssuer(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "prod-svc",
		ServiceNamespace: "prod-ns",
	}

	issuer := BuildCAIssuer(cfg)

	if issuer.Name != "prod-svc-ca-issuer" {
		t.Errorf("name = %q, want %q", issuer.Name, "prod-svc-ca-issuer")
	}
	if issuer.Namespace != "prod-ns" {
		t.Errorf("namespace = %q, want %q", issuer.Namespace, "prod-ns")
	}
	if issuer.Spec.SelfSigned != nil {
		t.Error("SelfSigned must be nil for CA issuer")
	}
	if issuer.Spec.CA == nil {
		t.Fatal("CA config must not be nil")
	}
	if issuer.Spec.CA.SecretName != "prod-svc-root-cert-secret" {
		t.Errorf("CA.SecretName = %q, want %q", issuer.Spec.CA.SecretName, "prod-svc-root-cert-secret")
	}
}

func TestBuildMetricsSelfSignedIssuer(t *testing.T) {
	cfg := &MetricsPKIConfig{
		ServiceName:      "svc",
		ServiceNamespace: "ns",
	}

	issuer := BuildMetricsSelfSignedIssuer(cfg)

	if issuer.Name != "svc-metrics-self-signed-issuer" {
		t.Errorf("name = %q", issuer.Name)
	}
	if issuer.Namespace != "ns" {
		t.Errorf("namespace = %q", issuer.Namespace)
	}
	if issuer.Spec.SelfSigned == nil {
		t.Fatal("SelfSigned must not be nil")
	}
}

func TestBuildMetricsCAIssuer(t *testing.T) {
	cfg := &MetricsPKIConfig{
		ServiceName:      "svc",
		ServiceNamespace: "ns",
	}

	issuer := BuildMetricsCAIssuer(cfg)

	if issuer.Name != "svc-metrics-ca-issuer" {
		t.Errorf("name = %q", issuer.Name)
	}
	if issuer.Spec.CA == nil {
		t.Fatal("CA config must not be nil")
	}
	if issuer.Spec.CA.SecretName != "svc-metrics-root-cert-secret" {
		t.Errorf("CA.SecretName = %q", issuer.Spec.CA.SecretName)
	}
}

func TestBuildAppSelfSignedIssuer(t *testing.T) {
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
	}

	issuer := BuildAppSelfSignedIssuer(cfg)

	if issuer.Name != "my-app-self-signed-issuer" {
		t.Errorf("name = %q", issuer.Name)
	}
	if issuer.Namespace != "app-ns" {
		t.Errorf("namespace = %q", issuer.Namespace)
	}
	if issuer.Spec.SelfSigned == nil {
		t.Fatal("SelfSigned must not be nil")
	}
}

func TestBuildAppCAIssuer(t *testing.T) {
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
	}

	issuer := BuildAppCAIssuer(cfg)

	if issuer.Name != "my-app-ca-issuer" {
		t.Errorf("name = %q", issuer.Name)
	}
	if issuer.Namespace != "app-ns" {
		t.Errorf("namespace = %q", issuer.Namespace)
	}
	if issuer.Spec.CA == nil {
		t.Fatal("CA config must not be nil")
	}
	if issuer.Spec.CA.SecretName != "my-app-root-cert-secret" {
		t.Errorf("CA.SecretName = %q", issuer.Spec.CA.SecretName)
	}
}
