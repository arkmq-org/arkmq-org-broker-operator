package v1beta2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CertificateTemplate exposes cert-manager Certificate fields that customers
// can tune. The operator always controls the base structural fields (secretName,
// commonName, dnsNames, issuerRef, isCA) to ensure chain-of-trust integrity.
// AdditionalDNSNames is the one exception: it appends to the operator-generated
// dnsNames without replacing them.
type CertificateTemplate struct {
	// Duration is the requested lifetime of the certificate.
	// Defaults differ by context: 10 years for CA certificates, 90 days for
	// leaf certificates.
	//+optional
	Duration *metav1.Duration `json:"duration,omitempty"`

	// RenewBefore is the amount of time before expiry to trigger renewal.
	// Defaults differ by context: 1 year for CA, 30 days for leaf certificates.
	//+optional
	RenewBefore *metav1.Duration `json:"renewBefore,omitempty"`

	// PrivateKey controls the algorithm, size, and rotation policy of the
	// certificate's private key.
	//+optional
	PrivateKey *CertificatePrivateKey `json:"privateKey,omitempty"`

	// AdditionalDNSNames appends extra SANs to the certificate.
	// The operator always generates the base set of cluster-internal DNS names;
	// this field allows adding external hostnames (e.g. ingress hosts) without
	// overriding the base set.
	// Each entry must be a valid DNS name (RFC 1123 subdomain).
	//+optional
	//+kubebuilder:validation:items:Pattern=`^[a-zA-Z0-9]([a-zA-Z0-9\-\.]*[a-zA-Z0-9])?$`
	AdditionalDNSNames []string `json:"additionalDNSNames,omitempty"`
}

// CertificatePrivateKey mirrors cert-manager's CertificatePrivateKey with
// the subset of fields the operator exposes.
type CertificatePrivateKey struct {
	// Algorithm is the private key algorithm. Allowed: RSA, ECDSA, Ed25519.
	//+optional
	//+kubebuilder:validation:Enum=RSA;ECDSA;Ed25519
	Algorithm string `json:"algorithm,omitempty"`

	// Size is the key bit size (e.g. 2048, 4096 for RSA; 256, 384 for ECDSA).
	//+optional
	Size int `json:"size,omitempty"`

	// RotationPolicy controls whether private keys are regenerated on renewal.
	// Allowed: Never, Always. Default: Never.
	//+optional
	//+kubebuilder:validation:Enum=Never;Always
	RotationPolicy string `json:"rotationPolicy,omitempty"`
}

// PKISpec configures the chain-of-trust PKI provisioned by the BrokerService.
// The operator plane (broker-cert, operator-cert) uses hardcoded defaults
// and is not configurable.
type PKISpec struct {
	// Metrics overrides for the observability plane certificates.
	// Controls the metrics CA and leaf certs (prometheus-cert, per-app metrics-cert).
	//+optional
	Metrics *MetricsPKISpec `json:"metrics,omitempty"`
}

// MetricsPKISpec configures the metrics/observability plane PKI.
type MetricsPKISpec struct {
	// CA overrides for the metrics root CA certificate.
	//+optional
	CA *CertificateTemplate `json:"ca,omitempty"`

	// Leaf overrides for metrics leaf certificates (prometheus-cert,
	// per-app metrics-cert).
	//+optional
	Leaf *CertificateTemplate `json:"leaf,omitempty"`
}

// AppPKISpec configures the app's data plane PKI.
type AppPKISpec struct {
	// CA overrides for the app's root CA certificate.
	//+optional
	CA *CertificateTemplate `json:"ca,omitempty"`

	// Leaf overrides for app leaf certificates (client cert, server cert).
	//+optional
	Leaf *CertificateTemplate `json:"leaf,omitempty"`
}
