package chainoftrust

import (
	"time"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	LabelAppName      = "broker.arkmq.org/app-name"
	LabelAppNamespace = "broker.arkmq.org/app-namespace"
	LabelManagedBy    = "broker.arkmq.org/managed-by"
	LabelCertService  = "broker.arkmq.org/certificate"
	ManagedByValue    = "chain-of-trust"
	FinalizerName     = "broker.arkmq.org/chain-of-trust-cleanup"

	DefaultCertDuration    = 90 * 24 * time.Hour // 90 days
	DefaultCertRenewBefore = 30 * 24 * time.Hour // 30 days before expiry
	DefaultCADuration      = 10 * 365 * 24 * time.Hour
	DefaultCARenewBefore   = 365 * 24 * time.Hour

	// MaxAppsPerCertsSecret is the practical ceiling imposed by the Kubernetes
	// 1MB Secret size limit. Each app adds ~6KB to the {svc}-certs secret
	// (server key + cert + CA + PEMCFG). 1MB / 6KB ≈ 170, with margin → 150.
	MaxAppsPerCertsSecret = 150
)

// PKIConfig — operator plane, no customization.
type PKIConfig struct {
	ServiceName      string
	ServiceNamespace string
	Owner            metav1.Object
	OwnerGVK         metav1.GroupVersionKind
}

// MetricsPKIConfig — metrics/observability plane, optional customization.
type MetricsPKIConfig struct {
	ServiceName      string
	ServiceNamespace string
	ClusterDomain    string
	Owner            metav1.Object
	OwnerGVK         metav1.GroupVersionKind
	CATemplate       *v1beta2.CertificateTemplate
	LeafTemplate     *v1beta2.CertificateTemplate
}

// AppPKIConfig — app data plane, full customization.
type AppPKIConfig struct {
	AppName      string
	AppNamespace string
	Owner        metav1.Object
	OwnerGVK     metav1.GroupVersionKind
	CATemplate   *v1beta2.CertificateTemplate
	LeafTemplate *v1beta2.CertificateTemplate
}

// AppCertConfig is used for provisioning-time cert operations (server cert, cross-ns copies).
type AppCertConfig struct {
	AppName          string
	AppNamespace     string
	ServiceName      string
	ServiceNamespace string
	ClusterDomain    string
	AppOwner         metav1.Object
	AppOwnerGVK      metav1.GroupVersionKind
	SameNamespace    bool
	Template         *v1beta2.CertificateTemplate
}

// --- Operator plane naming ---

func RootIssuerName(serviceName string) string {
	return serviceName + "-root-issuer"
}

func RootCertName(serviceName string) string {
	return serviceName + "-root-cert"
}

func RootCertSecretName(serviceName string) string {
	return serviceName + "-root-cert-secret"
}

func CAIssuerName(serviceName string) string {
	return serviceName + "-ca-issuer"
}

func BrokerCertName(serviceName string) string {
	return serviceName + "-broker-cert"
}

func OperatorCertName(serviceName string) string {
	return serviceName + "-operator-cert"
}

func CATrustSecretName(serviceName string) string {
	return serviceName + "-ca-trust"
}

func SvcCATrustForAppName(appName, serviceName string) string {
	return appName + "-" + serviceName + "-ca-trust"
}

// --- Metrics plane naming ---

func MetricsIssuerName(serviceName string) string {
	return serviceName + "-metrics-self-signed-issuer"
}

func MetricsRootCertName(serviceName string) string {
	return serviceName + "-metrics-root-cert"
}

func MetricsRootCertSecretName(serviceName string) string {
	return serviceName + "-metrics-root-cert-secret"
}

func MetricsCAIssuerName(serviceName string) string {
	return serviceName + "-metrics-ca-issuer"
}

func MetricsCATrustSecretName(serviceName string) string {
	return serviceName + "-metrics-ca-trust"
}

func PrometheusCertName(serviceName string) string {
	return serviceName + "-prometheus-cert"
}

func MetricsCertName(appName string) string {
	return appName + "-metrics-cert"
}

// --- App plane naming ---

func AppIssuerName(appName string) string {
	return appName + "-self-signed-issuer"
}

func AppRootCertName(appName string) string {
	return appName + "-root-cert"
}

func AppRootCertSecretName(appName string) string {
	return appName + "-root-cert-secret"
}

func AppCAIssuerName(appName string) string {
	return appName + "-ca-issuer"
}

func AppCertName(appName string) string {
	return appName + "-app-cert"
}

func AppCATrustSecretName(appName string) string {
	return appName + "-ca-trust"
}

func AppServerCertName(appName string) string {
	return appName + "-server-cert"
}

// --- Cert-data secret naming ---

func AppCertsSecretName(serviceName string) string {
	return serviceName + "-certs"
}
