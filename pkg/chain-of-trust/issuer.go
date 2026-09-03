package chainoftrust

import (
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func BuildSelfSignedIssuer(cfg *PKIConfig) *cmv1.Issuer {
	return &cmv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      RootIssuerName(cfg.ServiceName),
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				SelfSigned: &cmv1.SelfSignedIssuer{},
			},
		},
	}
}

func BuildCAIssuer(cfg *PKIConfig) *cmv1.Issuer {
	return &cmv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CAIssuerName(cfg.ServiceName),
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				CA: &cmv1.CAIssuer{
					SecretName: RootCertSecretName(cfg.ServiceName),
				},
			},
		},
	}
}

// --- Metrics plane issuers ---

func BuildMetricsSelfSignedIssuer(cfg *MetricsPKIConfig) *cmv1.Issuer {
	return &cmv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MetricsIssuerName(cfg.ServiceName),
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				SelfSigned: &cmv1.SelfSignedIssuer{},
			},
		},
	}
}

func BuildMetricsCAIssuer(cfg *MetricsPKIConfig) *cmv1.Issuer {
	return &cmv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MetricsCAIssuerName(cfg.ServiceName),
			Namespace: cfg.ServiceNamespace,
			Labels:    managedLabels(cfg.ServiceName),
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				CA: &cmv1.CAIssuer{
					SecretName: MetricsRootCertSecretName(cfg.ServiceName),
				},
			},
		},
	}
}

// --- App plane issuers ---

func BuildAppSelfSignedIssuer(cfg *AppPKIConfig) *cmv1.Issuer {
	return &cmv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AppIssuerName(cfg.AppName),
			Namespace: cfg.AppNamespace,
			Labels:    managedLabels(""),
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				SelfSigned: &cmv1.SelfSignedIssuer{},
			},
		},
	}
}

func BuildAppCAIssuer(cfg *AppPKIConfig) *cmv1.Issuer {
	return &cmv1.Issuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Issuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AppCAIssuerName(cfg.AppName),
			Namespace: cfg.AppNamespace,
			Labels:    managedLabels(""),
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				CA: &cmv1.CAIssuer{
					SecretName: AppRootCertSecretName(cfg.AppName),
				},
			},
		},
	}
}

func managedLabels(serviceName string) map[string]string {
	labels := map[string]string{
		LabelManagedBy: ManagedByValue,
	}
	if serviceName != "" {
		labels[LabelCertService] = serviceName
	}
	return labels
}

func provisioningLabels(serviceName, appName, appNamespace string) map[string]string {
	labels := managedLabels(serviceName)
	if appName != "" {
		labels[LabelAppName] = appName
	}
	if appNamespace != "" {
		labels[LabelAppNamespace] = appNamespace
	}
	return labels
}
