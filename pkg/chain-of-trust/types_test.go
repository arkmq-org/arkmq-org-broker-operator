package chainoftrust

import (
	"testing"
)

func TestNamingFunctions(t *testing.T) {
	tests := []struct {
		fn       func(string) string
		input    string
		expected string
	}{
		// Operator plane
		{RootIssuerName, "my-svc", "my-svc-root-issuer"},
		{RootCertName, "my-svc", "my-svc-root-cert"},
		{RootCertSecretName, "my-svc", "my-svc-root-cert-secret"},
		{CAIssuerName, "my-svc", "my-svc-ca-issuer"},
		{BrokerCertName, "my-svc", "my-svc-broker-cert"},
		{OperatorCertName, "my-svc", "my-svc-operator-cert"},
		{CATrustSecretName, "my-svc", "my-svc-ca-trust"},

		// Metrics plane
		{MetricsIssuerName, "my-svc", "my-svc-metrics-self-signed-issuer"},
		{MetricsRootCertName, "my-svc", "my-svc-metrics-root-cert"},
		{MetricsRootCertSecretName, "my-svc", "my-svc-metrics-root-cert-secret"},
		{MetricsCAIssuerName, "my-svc", "my-svc-metrics-ca-issuer"},
		{MetricsCATrustSecretName, "my-svc", "my-svc-metrics-ca-trust"},
		{PrometheusCertName, "my-svc", "my-svc-prometheus-cert"},
		{MetricsCertName, "my-app", "my-app-metrics-cert"},

		// App plane
		{AppIssuerName, "my-app", "my-app-self-signed-issuer"},
		{AppRootCertName, "my-app", "my-app-root-cert"},
		{AppRootCertSecretName, "my-app", "my-app-root-cert-secret"},
		{AppCAIssuerName, "my-app", "my-app-ca-issuer"},
		{AppCertName, "my-app", "my-app-app-cert"},
		{AppCATrustSecretName, "my-app", "my-app-ca-trust"},
		{AppServerCertName, "my-app", "my-app-server-cert"},

		// Cert-data secret
		{AppCertsSecretName, "my-svc", "my-svc-certs"},
	}

	for _, tt := range tests {
		got := tt.fn(tt.input)
		if got != tt.expected {
			t.Errorf("naming(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSvcCATrustForAppName(t *testing.T) {
	got := SvcCATrustForAppName("my-app", "my-svc")
	if got != "my-app-my-svc-ca-trust" {
		t.Errorf("SvcCATrustForAppName = %q, want %q", got, "my-app-my-svc-ca-trust")
	}
}

func TestNamingWithLongNames(t *testing.T) {
	long := "this-is-a-very-long-service-name-that-tests-edge-cases"
	if got := RootIssuerName(long); got != long+"-root-issuer" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestNamingWithSpecialChars(t *testing.T) {
	name := "svc-with-numbers-123"
	if got := BrokerCertName(name); got != "svc-with-numbers-123-broker-cert" {
		t.Errorf("unexpected: %s", got)
	}
}
