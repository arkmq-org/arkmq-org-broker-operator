package chainoftrust

import (
	"fmt"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- Operator plane certificates (hardcoded, no template) ---

func BuildRootCACertificate(cfg *PKIConfig) *cmv1.Certificate {
	return &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      RootCertName(cfg.ServiceName),
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.CertificateSpec{
			IsCA:        true,
			CommonName:  cfg.ServiceName + ".root.ca",
			SecretName:  RootCertSecretName(cfg.ServiceName),
			Duration:    &metav1.Duration{Duration: DefaultCADuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCARenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: RootIssuerName(cfg.ServiceName),
				Kind: "Issuer",
			},
		},
	}
}

func BuildBrokerCertificate(cfg *PKIConfig, clusterDomain string) *cmv1.Certificate {
	name := BrokerCertName(cfg.ServiceName)
	return &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.CertificateSpec{
			SecretName:  name,
			CommonName:  cfg.ServiceName,
			DNSNames:    brokerDNSNames(cfg.ServiceName, cfg.ServiceNamespace, clusterDomain),
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: CAIssuerName(cfg.ServiceName),
				Kind: "Issuer",
			},
		},
	}
}

func BuildOperatorCertificate(cfg *PKIConfig) *cmv1.Certificate {
	name := OperatorCertName(cfg.ServiceName)
	return &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.CertificateSpec{
			SecretName:  name,
			CommonName:  "arkmq-org-broker-operator",
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: CAIssuerName(cfg.ServiceName),
				Kind: "Issuer",
			},
		},
	}
}

// --- Metrics plane certificates (optional template) ---

func BuildMetricsRootCACertificate(cfg *MetricsPKIConfig) *cmv1.Certificate {
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MetricsRootCertName(cfg.ServiceName),
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.CertificateSpec{
			IsCA:        true,
			CommonName:  cfg.ServiceName + ".metrics.ca",
			SecretName:  MetricsRootCertSecretName(cfg.ServiceName),
			Duration:    &metav1.Duration{Duration: DefaultCADuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCARenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: MetricsIssuerName(cfg.ServiceName),
				Kind: "Issuer",
			},
		},
	}
	applyTemplate(&cert.Spec, cfg.CATemplate)
	return cert
}

func BuildPrometheusCertificate(cfg *MetricsPKIConfig) *cmv1.Certificate {
	name := PrometheusCertName(cfg.ServiceName)
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.CertificateSpec{
			SecretName:  name,
			CommonName:  "prometheus",
			DNSNames:    brokerDNSNames(cfg.ServiceName, cfg.ServiceNamespace, cfg.ClusterDomain),
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: MetricsCAIssuerName(cfg.ServiceName),
				Kind: "Issuer",
			},
		},
	}
	applyTemplate(&cert.Spec, cfg.LeafTemplate)
	return cert
}

func BuildMetricsCertificate(cfg *MetricsPKIConfig, appName, appNamespace string) *cmv1.Certificate {
	name := MetricsCertName(appName)
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.ServiceNamespace,
			Labels:    provisioningLabels(cfg.ServiceName, appName, appNamespace),
		},
		Spec: cmv1.CertificateSpec{
			SecretName: name,
			CommonName: appName,
			Subject: &cmv1.X509Subject{
				OrganizationalUnits: []string{appNamespace},
			},
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: MetricsCAIssuerName(cfg.ServiceName),
				Kind: "Issuer",
			},
		},
	}
	applyTemplate(&cert.Spec, cfg.LeafTemplate)
	return cert
}

// --- App plane certificates (full template) ---

func BuildAppCACertificate(cfg *AppPKIConfig) *cmv1.Certificate {
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AppRootCertName(cfg.AppName),
			Namespace: cfg.AppNamespace,
			Labels:    managedLabels(""),
		},
		Spec: cmv1.CertificateSpec{
			IsCA:        true,
			CommonName:  cfg.AppName + ".app.ca",
			SecretName:  AppRootCertSecretName(cfg.AppName),
			Duration:    &metav1.Duration{Duration: DefaultCADuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCARenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: AppIssuerName(cfg.AppName),
				Kind: "Issuer",
			},
		},
	}
	applyTemplate(&cert.Spec, cfg.CATemplate)
	return cert
}

func BuildAppClientCertificate(cfg *AppPKIConfig) *cmv1.Certificate {
	name := AppCertName(cfg.AppName)
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.AppNamespace,
			Labels:    managedLabels(""),
		},
		Spec: cmv1.CertificateSpec{
			SecretName: name,
			CommonName: cfg.AppName,
			Subject: &cmv1.X509Subject{
				OrganizationalUnits: []string{cfg.AppNamespace},
			},
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: AppCAIssuerName(cfg.AppName),
				Kind: "Issuer",
			},
		},
	}
	applyTemplate(&cert.Spec, cfg.LeafTemplate)
	return cert
}

func BuildAppServerCertificate(cfg *AppPKIConfig, serviceName, serviceNamespace, clusterDomain string) *cmv1.Certificate {
	name := AppServerCertName(cfg.AppName)
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cfg.AppNamespace,
			Labels:    provisioningLabels(serviceName, cfg.AppName, cfg.AppNamespace),
		},
		Spec: cmv1.CertificateSpec{
			SecretName:  name,
			CommonName:  cfg.AppName + ".server",
			DNSNames:    brokerDNSNames(serviceName, serviceNamespace, clusterDomain),
			Duration:    &metav1.Duration{Duration: DefaultCertDuration},
			RenewBefore: &metav1.Duration{Duration: DefaultCertRenewBefore},
			IssuerRef: cmmetav1.ObjectReference{
				Name: AppCAIssuerName(cfg.AppName),
				Kind: "Issuer",
			},
		},
	}
	applyTemplate(&cert.Spec, cfg.LeafTemplate)
	return cert
}

// applyTemplate overlays customer-provided certificate fields onto the
// cert-manager CertificateSpec. Structural fields (secretName, commonName,
// issuerRef, isCA) are never overridden. AdditionalDNSNames is the one
// exception for dnsNames: it appends to the operator-generated base set
// without replacing it.
func applyTemplate(spec *cmv1.CertificateSpec, tmpl *v1beta2.CertificateTemplate) {
	if tmpl == nil {
		return
	}
	if tmpl.Duration != nil {
		spec.Duration = tmpl.Duration
	}
	if tmpl.RenewBefore != nil {
		spec.RenewBefore = tmpl.RenewBefore
	}
	if tmpl.PrivateKey != nil {
		if spec.PrivateKey == nil {
			spec.PrivateKey = &cmv1.CertificatePrivateKey{}
		}
		if tmpl.PrivateKey.Algorithm != "" {
			spec.PrivateKey.Algorithm = cmv1.PrivateKeyAlgorithm(tmpl.PrivateKey.Algorithm)
		}
		if tmpl.PrivateKey.Size != 0 {
			spec.PrivateKey.Size = tmpl.PrivateKey.Size
		}
		if tmpl.PrivateKey.RotationPolicy != "" {
			spec.PrivateKey.RotationPolicy = cmv1.PrivateKeyRotationPolicy(tmpl.PrivateKey.RotationPolicy)
		}
	}
	if len(tmpl.AdditionalDNSNames) > 0 {
		spec.DNSNames = append(spec.DNSNames, tmpl.AdditionalDNSNames...)
	}
}

func brokerDNSNames(serviceName, namespace, clusterDomain string) []string {
	return []string{
		serviceName,
		fmt.Sprintf("%s.%s", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, clusterDomain),
		fmt.Sprintf("*.%s-hdls-svc.%s.svc.%s", serviceName, namespace, clusterDomain),
	}
}
