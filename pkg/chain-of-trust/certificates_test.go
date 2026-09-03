package chainoftrust

import (
	"testing"
	"time"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildRootCACertificate(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "my-svc",
		ServiceNamespace: "my-ns",
	}

	cert := BuildRootCACertificate(cfg)

	if !cert.Spec.IsCA {
		t.Error("root CA must have IsCA=true")
	}
	if cert.Name != "my-svc-root-cert" {
		t.Errorf("name = %q, want %q", cert.Name, "my-svc-root-cert")
	}
	if cert.Spec.SecretName != "my-svc-root-cert-secret" {
		t.Errorf("secretName = %q, want %q", cert.Spec.SecretName, "my-svc-root-cert-secret")
	}
	if cert.Spec.IssuerRef.Name != "my-svc-root-issuer" {
		t.Errorf("issuerRef = %q, want %q", cert.Spec.IssuerRef.Name, "my-svc-root-issuer")
	}
	if cert.Spec.IssuerRef.Kind != "Issuer" {
		t.Errorf("issuerRef.Kind = %q, want Issuer", cert.Spec.IssuerRef.Kind)
	}
	if cert.Spec.CommonName != "my-svc.root.ca" {
		t.Errorf("commonName = %q, want %q", cert.Spec.CommonName, "my-svc.root.ca")
	}
	if cert.Spec.Duration.Duration != DefaultCADuration {
		t.Errorf("duration = %v, want %v", cert.Spec.Duration.Duration, DefaultCADuration)
	}
	if cert.Spec.RenewBefore.Duration != DefaultCARenewBefore {
		t.Errorf("renewBefore = %v, want %v", cert.Spec.RenewBefore.Duration, DefaultCARenewBefore)
	}
}

func TestBuildBrokerCertificate(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "my-svc",
		ServiceNamespace: "my-ns",
	}

	cert := BuildBrokerCertificate(cfg, "cluster.local")

	if cert.Name != "my-svc-broker-cert" {
		t.Errorf("name = %q", cert.Name)
	}
	if cert.Spec.SecretName != "my-svc-broker-cert" {
		t.Errorf("secretName = %q", cert.Spec.SecretName)
	}
	if cert.Spec.IssuerRef.Name != "my-svc-ca-issuer" {
		t.Errorf("issuerRef = %q", cert.Spec.IssuerRef.Name)
	}
	if cert.Spec.IsCA {
		t.Error("broker cert must not be CA")
	}

	expectedDNS := []string{
		"my-svc",
		"my-svc.my-ns",
		"my-svc.my-ns.svc.cluster.local",
		"*.my-svc-hdls-svc.my-ns.svc.cluster.local",
	}
	if len(cert.Spec.DNSNames) != len(expectedDNS) {
		t.Fatalf("dnsNames count = %d, want %d", len(cert.Spec.DNSNames), len(expectedDNS))
	}
	for i, dns := range cert.Spec.DNSNames {
		if dns != expectedDNS[i] {
			t.Errorf("dnsNames[%d] = %q, want %q", i, dns, expectedDNS[i])
		}
	}
	if cert.Spec.Duration.Duration != DefaultCertDuration {
		t.Errorf("duration = %v, want %v", cert.Spec.Duration.Duration, DefaultCertDuration)
	}
}

func TestBuildOperatorCertificate(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "svc",
		ServiceNamespace: "ns",
	}

	cert := BuildOperatorCertificate(cfg)

	if cert.Name != "svc-operator-cert" {
		t.Errorf("name = %q", cert.Name)
	}
	if cert.Spec.CommonName != "arkmq-org-broker-operator" {
		t.Errorf("commonName = %q", cert.Spec.CommonName)
	}
	if cert.Spec.IssuerRef.Name != "svc-ca-issuer" {
		t.Errorf("issuerRef = %q", cert.Spec.IssuerRef.Name)
	}
}

func TestBuildPrometheusCertificate(t *testing.T) {
	cfg := &MetricsPKIConfig{
		ServiceName:      "svc",
		ServiceNamespace: "ns",
	}

	cert := BuildPrometheusCertificate(cfg)

	if cert.Name != "svc-prometheus-cert" {
		t.Errorf("name = %q", cert.Name)
	}
	if cert.Spec.CommonName != "prometheus" {
		t.Errorf("commonName = %q", cert.Spec.CommonName)
	}
	if cert.Spec.IssuerRef.Name != "svc-metrics-ca-issuer" {
		t.Errorf("issuerRef = %q, want svc-metrics-ca-issuer", cert.Spec.IssuerRef.Name)
	}
}

func TestApplyTemplate_NilIsNoOp(t *testing.T) {
	spec := &cmv1.CertificateSpec{
		Duration:    &metav1.Duration{Duration: DefaultCertDuration},
		RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
	}
	applyTemplate(spec, nil)

	if spec.Duration.Duration != DefaultCertDuration {
		t.Errorf("duration changed from default: %v", spec.Duration.Duration)
	}
	if spec.RenewBefore.Duration != DefaultCertRenewBefore {
		t.Errorf("renewBefore changed from default: %v", spec.RenewBefore.Duration)
	}
}

func TestApplyTemplate_OverridesDuration(t *testing.T) {
	spec := &cmv1.CertificateSpec{
		Duration:    &metav1.Duration{Duration: DefaultCertDuration},
		RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
	}

	customDuration := 5 * time.Minute
	customRenew := 2 * time.Minute
	tmpl := &v1beta2.CertificateTemplate{
		Duration:    &metav1.Duration{Duration: customDuration},
		RenewBefore: &metav1.Duration{Duration: customRenew},
	}
	applyTemplate(spec, tmpl)

	if spec.Duration.Duration != customDuration {
		t.Errorf("duration = %v, want %v", spec.Duration.Duration, customDuration)
	}
	if spec.RenewBefore.Duration != customRenew {
		t.Errorf("renewBefore = %v, want %v", spec.RenewBefore.Duration, customRenew)
	}
}

func TestApplyTemplate_OverridesPrivateKey(t *testing.T) {
	spec := &cmv1.CertificateSpec{
		Duration: &metav1.Duration{Duration: DefaultCertDuration},
	}

	tmpl := &v1beta2.CertificateTemplate{
		PrivateKey: &v1beta2.CertificatePrivateKey{
			Algorithm:      "ECDSA",
			Size:           384,
			RotationPolicy: "Always",
		},
	}
	applyTemplate(spec, tmpl)

	if spec.PrivateKey == nil {
		t.Fatal("privateKey should be set")
	}
	if spec.PrivateKey.Algorithm != cmv1.ECDSAKeyAlgorithm {
		t.Errorf("algorithm = %q, want ECDSA", spec.PrivateKey.Algorithm)
	}
	if spec.PrivateKey.Size != 384 {
		t.Errorf("size = %d, want 384", spec.PrivateKey.Size)
	}
	if spec.PrivateKey.RotationPolicy != cmv1.RotationPolicyAlways {
		t.Errorf("rotationPolicy = %q, want Always", spec.PrivateKey.RotationPolicy)
	}
}

func TestApplyTemplate_DoesNotOverrideStructuralFields(t *testing.T) {
	spec := &cmv1.CertificateSpec{
		SecretName: "must-not-change",
		CommonName: "must-not-change",
		IsCA:       true,
	}

	tmpl := &v1beta2.CertificateTemplate{
		Duration: &metav1.Duration{Duration: 10 * time.Minute},
	}
	applyTemplate(spec, tmpl)

	if spec.SecretName != "must-not-change" {
		t.Error("secretName was overridden")
	}
	if spec.CommonName != "must-not-change" {
		t.Error("commonName was overridden")
	}
	if !spec.IsCA {
		t.Error("isCA was overridden")
	}
}

func TestApplyTemplate_AdditionalDNSNames_Appends(t *testing.T) {
	baseDNS := []string{"svc.test", "svc.test.svc.cluster.local"}
	spec := &cmv1.CertificateSpec{
		DNSNames: baseDNS,
	}
	tmpl := &v1beta2.CertificateTemplate{
		AdditionalDNSNames: []string{"broker.example.com", "mqtt.example.com"},
	}
	applyTemplate(spec, tmpl)

	expected := []string{"svc.test", "svc.test.svc.cluster.local", "broker.example.com", "mqtt.example.com"}
	if len(spec.DNSNames) != len(expected) {
		t.Fatalf("DNSNames length = %d, want %d", len(spec.DNSNames), len(expected))
	}
	for i, name := range expected {
		if spec.DNSNames[i] != name {
			t.Errorf("DNSNames[%d] = %q, want %q", i, spec.DNSNames[i], name)
		}
	}
}

func TestApplyTemplate_AdditionalDNSNames_EmptyIsNoOp(t *testing.T) {
	baseDNS := []string{"svc.test"}
	spec := &cmv1.CertificateSpec{
		DNSNames: baseDNS,
	}
	tmpl := &v1beta2.CertificateTemplate{
		AdditionalDNSNames: nil,
	}
	applyTemplate(spec, tmpl)

	if len(spec.DNSNames) != 1 || spec.DNSNames[0] != "svc.test" {
		t.Errorf("empty AdditionalDNSNames should not modify DNSNames, got %v", spec.DNSNames)
	}
}

func TestApplyTemplate_AdditionalDNSNames_DuplicatesPreserved(t *testing.T) {
	spec := &cmv1.CertificateSpec{
		DNSNames: []string{"svc.test"},
	}
	tmpl := &v1beta2.CertificateTemplate{
		AdditionalDNSNames: []string{"svc.test"},
	}
	applyTemplate(spec, tmpl)

	// Deduplication is cert-manager's responsibility, not ours.
	if len(spec.DNSNames) != 2 {
		t.Errorf("duplicates should be preserved (cert-manager deduplicates), got %v", spec.DNSNames)
	}
}

func TestBuildAppServerCertificate_WithAdditionalDNSNames(t *testing.T) {
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
		LeafTemplate: &v1beta2.CertificateTemplate{
			AdditionalDNSNames: []string{"broker.example.com"},
		},
	}
	cert := BuildAppServerCertificate(cfg, "my-svc", "svc-ns", "cluster.local")

	found := false
	for _, name := range cert.Spec.DNSNames {
		if name == "broker.example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("additionalDNSNames not appended, got %v", cert.Spec.DNSNames)
	}
	// Base names must still be present
	if cert.Spec.DNSNames[0] != "my-svc" {
		t.Errorf("base DNS name missing, got %v", cert.Spec.DNSNames)
	}
}

func TestBuildRootCACertificate_Hardcoded(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "my-svc",
		ServiceNamespace: "my-ns",
	}

	cert := BuildRootCACertificate(cfg)

	if cert.Spec.Duration.Duration != DefaultCADuration {
		t.Errorf("operator CA must use hardcoded duration, got %v", cert.Spec.Duration.Duration)
	}
	if !cert.Spec.IsCA {
		t.Error("root CA must have IsCA=true")
	}
}

func TestBuildMetricsRootCACertificate_WithTemplate(t *testing.T) {
	customDuration := 5 * time.Minute
	cfg := &MetricsPKIConfig{
		ServiceName:      "my-svc",
		ServiceNamespace: "my-ns",
		CATemplate: &v1beta2.CertificateTemplate{
			Duration: &metav1.Duration{Duration: customDuration},
		},
	}

	cert := BuildMetricsRootCACertificate(cfg)

	if cert.Spec.Duration.Duration != customDuration {
		t.Errorf("metrics CA duration = %v, want %v", cert.Spec.Duration.Duration, customDuration)
	}
	if !cert.Spec.IsCA {
		t.Error("template must not override IsCA")
	}
	if cert.Spec.SecretName != "my-svc-metrics-root-cert-secret" {
		t.Errorf("secretName = %q, want %q", cert.Spec.SecretName, "my-svc-metrics-root-cert-secret")
	}
	if cert.Spec.IssuerRef.Name != "my-svc-metrics-self-signed-issuer" {
		t.Errorf("issuerRef = %q", cert.Spec.IssuerRef.Name)
	}
}

func TestBuildPrometheusCertificate_WithLeafTemplate(t *testing.T) {
	customDuration := 3 * time.Minute
	customRenew := 90 * time.Second
	cfg := &MetricsPKIConfig{
		ServiceName:      "my-svc",
		ServiceNamespace: "my-ns",
		LeafTemplate: &v1beta2.CertificateTemplate{
			Duration:    &metav1.Duration{Duration: customDuration},
			RenewBefore: &metav1.Duration{Duration: customRenew},
		},
	}

	cert := BuildPrometheusCertificate(cfg)

	if cert.Spec.Duration.Duration != customDuration {
		t.Errorf("duration = %v, want %v", cert.Spec.Duration.Duration, customDuration)
	}
	if cert.Spec.RenewBefore.Duration != customRenew {
		t.Errorf("renewBefore = %v, want %v", cert.Spec.RenewBefore.Duration, customRenew)
	}
}

func TestBuildAppCACertificate(t *testing.T) {
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
	}

	cert := BuildAppCACertificate(cfg)

	if !cert.Spec.IsCA {
		t.Error("app CA must have IsCA=true")
	}
	if cert.Name != "my-app-root-cert" {
		t.Errorf("name = %q, want my-app-root-cert", cert.Name)
	}
	if cert.Spec.SecretName != "my-app-root-cert-secret" {
		t.Errorf("secretName = %q", cert.Spec.SecretName)
	}
	if cert.Spec.CommonName != "my-app.app.ca" {
		t.Errorf("commonName = %q", cert.Spec.CommonName)
	}
	if cert.Spec.IssuerRef.Name != "my-app-self-signed-issuer" {
		t.Errorf("issuerRef = %q", cert.Spec.IssuerRef.Name)
	}
}

func TestBuildAppClientCertificate(t *testing.T) {
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
	}

	cert := BuildAppClientCertificate(cfg)

	if cert.Name != "my-app-app-cert" {
		t.Errorf("name = %q", cert.Name)
	}
	if cert.Spec.CommonName != "my-app" {
		t.Errorf("commonName = %q", cert.Spec.CommonName)
	}
	if cert.Spec.IssuerRef.Name != "my-app-ca-issuer" {
		t.Errorf("issuerRef = %q, want my-app-ca-issuer", cert.Spec.IssuerRef.Name)
	}
	if cert.Namespace != "app-ns" {
		t.Errorf("namespace = %q, want app-ns", cert.Namespace)
	}
}

func TestBuildAppServerCertificate(t *testing.T) {
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
	}

	cert := BuildAppServerCertificate(cfg, "my-svc", "svc-ns", "cluster.local")

	if cert.Name != "my-app-server-cert" {
		t.Errorf("name = %q", cert.Name)
	}
	if cert.Spec.CommonName != "my-app.server" {
		t.Errorf("commonName = %q", cert.Spec.CommonName)
	}
	if cert.Spec.IssuerRef.Name != "my-app-ca-issuer" {
		t.Errorf("issuerRef = %q", cert.Spec.IssuerRef.Name)
	}
	expectedDNS := []string{
		"my-svc",
		"my-svc.svc-ns",
		"my-svc.svc-ns.svc.cluster.local",
		"*.my-svc-hdls-svc.svc-ns.svc.cluster.local",
	}
	if len(cert.Spec.DNSNames) != len(expectedDNS) {
		t.Fatalf("dnsNames count = %d, want %d", len(cert.Spec.DNSNames), len(expectedDNS))
	}
	for i, dns := range cert.Spec.DNSNames {
		if dns != expectedDNS[i] {
			t.Errorf("dnsNames[%d] = %q, want %q", i, dns, expectedDNS[i])
		}
	}
}

func TestBuildMetricsCertificate(t *testing.T) {
	cfg := &MetricsPKIConfig{
		ServiceName:      "my-svc",
		ServiceNamespace: "svc-ns",
	}

	cert := BuildMetricsCertificate(cfg, "my-app", "app-ns")

	if cert.Name != "my-app-metrics-cert" {
		t.Errorf("name = %q", cert.Name)
	}
	if cert.Spec.CommonName != "my-app" {
		t.Errorf("commonName = %q", cert.Spec.CommonName)
	}
	if cert.Namespace != "svc-ns" {
		t.Errorf("namespace = %q, want svc-ns", cert.Namespace)
	}
	if cert.Spec.IssuerRef.Name != "my-svc-metrics-ca-issuer" {
		t.Errorf("issuerRef = %q", cert.Spec.IssuerRef.Name)
	}
	if cert.Spec.Subject == nil || len(cert.Spec.Subject.OrganizationalUnits) == 0 {
		t.Fatal("missing OU in subject")
	}
	if cert.Spec.Subject.OrganizationalUnits[0] != "app-ns" {
		t.Errorf("OU = %q, want app-ns", cert.Spec.Subject.OrganizationalUnits[0])
	}
}

func TestBuildAppCACertificate_WithTemplate(t *testing.T) {
	customDuration := 2 * time.Hour
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
		CATemplate: &v1beta2.CertificateTemplate{
			Duration: &metav1.Duration{Duration: customDuration},
		},
	}

	cert := BuildAppCACertificate(cfg)

	if cert.Spec.Duration.Duration != customDuration {
		t.Errorf("duration = %v, want %v", cert.Spec.Duration.Duration, customDuration)
	}
	if !cert.Spec.IsCA {
		t.Error("template must not override IsCA")
	}
	if cert.Spec.SecretName != "my-app-root-cert-secret" {
		t.Error("template must not override SecretName")
	}
}

func TestBuildAppClientCertificate_WithTemplate(t *testing.T) {
	customDuration := 5 * time.Minute
	customRenew := 4*time.Minute + 55*time.Second
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
		LeafTemplate: &v1beta2.CertificateTemplate{
			Duration:    &metav1.Duration{Duration: customDuration},
			RenewBefore: &metav1.Duration{Duration: customRenew},
			PrivateKey: &v1beta2.CertificatePrivateKey{
				RotationPolicy: "Always",
			},
		},
	}

	cert := BuildAppClientCertificate(cfg)

	if cert.Spec.Duration.Duration != customDuration {
		t.Errorf("duration = %v, want %v", cert.Spec.Duration.Duration, customDuration)
	}
	if cert.Spec.RenewBefore.Duration != customRenew {
		t.Errorf("renewBefore = %v, want %v", cert.Spec.RenewBefore.Duration, customRenew)
	}
	if cert.Spec.PrivateKey == nil || cert.Spec.PrivateKey.RotationPolicy != cmv1.RotationPolicyAlways {
		t.Error("RotationPolicy not applied from template")
	}
	if cert.Spec.CommonName != "my-app" {
		t.Error("template must not override CommonName")
	}
}

func TestBuildAppServerCertificate_WithTemplate(t *testing.T) {
	customDuration := 5 * time.Minute
	cfg := &AppPKIConfig{
		AppName:      "my-app",
		AppNamespace: "app-ns",
		LeafTemplate: &v1beta2.CertificateTemplate{
			Duration: &metav1.Duration{Duration: customDuration},
		},
	}

	cert := BuildAppServerCertificate(cfg, "my-svc", "svc-ns", "cluster.local")

	if cert.Spec.Duration.Duration != customDuration {
		t.Errorf("duration = %v, want %v", cert.Spec.Duration.Duration, customDuration)
	}
	if cert.Spec.CommonName != "my-app.server" {
		t.Error("template must not override CommonName")
	}
	if len(cert.Spec.DNSNames) != 4 {
		t.Errorf("expected 4 DNS names, got %d", len(cert.Spec.DNSNames))
	}
}

func TestEnsureOperatorPKI_ResourceCount(t *testing.T) {
	cfg := &PKIConfig{
		ServiceName:      "svc",
		ServiceNamespace: "ns",
	}
	objects := EnsureOperatorPKI(cfg, "cluster.local")

	if len(objects) != 5 {
		t.Errorf("EnsureOperatorPKI returned %d objects, want 5 (issuer, CA, CA-issuer, broker-cert, operator-cert)", len(objects))
	}
}
