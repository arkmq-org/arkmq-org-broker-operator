package chainoftrust

import (
	"context"
	"fmt"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = ctrl.Log.WithName("chain-of-trust")

// --- Operator plane ---

// EnsurePKI returns operator-plane cert-manager resources. Kept as the public
// entry point; callers don't need to know about the internal split.
func EnsurePKI(cfg *PKIConfig, clusterDomain string) []client.Object {
	return EnsureOperatorPKI(cfg, clusterDomain)
}

// EnsureOperatorPKI returns operator-plane resources (hardcoded, no template).
func EnsureOperatorPKI(cfg *PKIConfig, clusterDomain string) []client.Object {
	return []client.Object{
		BuildSelfSignedIssuer(cfg),
		BuildRootCACertificate(cfg),
		BuildCAIssuer(cfg),
		BuildBrokerCertificate(cfg, clusterDomain),
		BuildOperatorCertificate(cfg),
	}
}

// --- Metrics plane ---

// EnsureMetricsPKI returns metrics-plane resources (optional template).
func EnsureMetricsPKI(ctx context.Context, cl client.Client, cfg *MetricsPKIConfig) ([]client.Object, error) {
	objects := []client.Object{
		BuildMetricsSelfSignedIssuer(cfg),
		BuildMetricsRootCACertificate(cfg),
		BuildMetricsCAIssuer(cfg),
		BuildPrometheusCertificate(cfg),
	}

	caTrust, err := buildMetricsCATrust(ctx, cl, cfg)
	if err != nil {
		return objects, fmt.Errorf("failed to build metrics CA trust: %w", err)
	}
	if caTrust != nil {
		objects = append(objects, caTrust)
	}

	return objects, nil
}

func buildMetricsCATrust(ctx context.Context, cl client.Client, cfg *MetricsPKIConfig) (client.Object, error) {
	rootSecretKey := types.NamespacedName{
		Name:      MetricsRootCertSecretName(cfg.ServiceName),
		Namespace: cfg.ServiceNamespace,
	}
	destKey := types.NamespacedName{
		Name:      MetricsCATrustSecretName(cfg.ServiceName),
		Namespace: cfg.ServiceNamespace,
	}
	return buildCATrustDesired(ctx, cl, rootSecretKey, destKey, cfg.Owner, cfg.OwnerGVK)
}

// --- App plane ---

// EnsureAppPKI returns app-plane resources created at BrokerApp creation time
// (before provisioning). Returns issuers, CA cert, client cert, and ca-trust.
func EnsureAppPKI(ctx context.Context, cl client.Client, cfg *AppPKIConfig) ([]client.Object, error) {
	objects := []client.Object{
		BuildAppSelfSignedIssuer(cfg),
		BuildAppCACertificate(cfg),
		BuildAppCAIssuer(cfg),
		BuildAppClientCertificate(cfg),
	}

	caTrust, err := buildAppCATrust(ctx, cl, cfg)
	if err != nil {
		return objects, fmt.Errorf("failed to build app CA trust: %w", err)
	}
	if caTrust != nil {
		objects = append(objects, caTrust)
	}

	return objects, nil
}

func buildAppCATrust(ctx context.Context, cl client.Client, cfg *AppPKIConfig) (client.Object, error) {
	rootSecretKey := types.NamespacedName{
		Name:      AppRootCertSecretName(cfg.AppName),
		Namespace: cfg.AppNamespace,
	}
	destKey := types.NamespacedName{
		Name:      AppCATrustSecretName(cfg.AppName),
		Namespace: cfg.AppNamespace,
	}
	return buildCATrustDesired(ctx, cl, rootSecretKey, destKey, cfg.Owner, cfg.OwnerGVK)
}

// --- Provisioning-time app certs ---

// EnsureAppCert handles app certificate lifecycle. For same-namespace apps
// it returns Certificates as desired objects (tracked via ownerRef).
// For cross-namespace apps it creates resources directly and copies
// secrets between namespaces.
func EnsureAppCert(ctx context.Context, cl client.Client, cfg *AppCertConfig) ([]client.Object, error) {
	if cfg.SameNamespace {
		return ensureAppCertSameNS(ctx, cl, cfg)
	}
	return nil, ensureAppCertCrossNS(ctx, cl, cfg)
}

func ensureAppCertSameNS(_ context.Context, _ client.Client, cfg *AppCertConfig) ([]client.Object, error) {
	appPKICfg := &AppPKIConfig{
		AppName:      cfg.AppName,
		AppNamespace: cfg.AppNamespace,
		Owner:        cfg.AppOwner,
		OwnerGVK:     cfg.AppOwnerGVK,
		LeafTemplate: cfg.Template,
	}
	serverCert := BuildAppServerCertificate(appPKICfg, cfg.ServiceName, cfg.ServiceNamespace, cfg.ClusterDomain)

	metricsCfg := &MetricsPKIConfig{
		ServiceName:      cfg.ServiceName,
		ServiceNamespace: cfg.ServiceNamespace,
	}
	metricsCert := BuildMetricsCertificate(metricsCfg, cfg.AppName, cfg.ServiceNamespace)

	return []client.Object{serverCert, metricsCert}, nil
}

func ensureAppCertCrossNS(ctx context.Context, cl client.Client, cfg *AppCertConfig) error {
	// 1. App-plane server cert — created in the app namespace (where the
	//    app CA issuer lives), signed by AppCAIssuerName.
	appPKICfg := &AppPKIConfig{
		AppName:      cfg.AppName,
		AppNamespace: cfg.AppNamespace,
		Owner:        cfg.AppOwner,
		OwnerGVK:     cfg.AppOwnerGVK,
		LeafTemplate: cfg.Template,
	}
	serverCert := BuildAppServerCertificate(appPKICfg, cfg.ServiceName, cfg.ServiceNamespace, cfg.ClusterDomain)
	if err := createIfNotExists(ctx, cl, serverCert); err != nil {
		return fmt.Errorf("failed to ensure cross-ns server cert %s: %w", serverCert.Name, err)
	}

	// 2. Metrics-plane cert — created in the service namespace (where the
	//    metrics CA issuer lives), signed by MetricsCAIssuerName.
	metricsCfg := &MetricsPKIConfig{
		ServiceName:      cfg.ServiceName,
		ServiceNamespace: cfg.ServiceNamespace,
	}
	metricsCert := BuildMetricsCertificate(metricsCfg, cfg.AppName, cfg.ServiceNamespace)
	if err := createIfNotExists(ctx, cl, metricsCert); err != nil {
		return fmt.Errorf("failed to ensure cross-ns metrics cert %s: %w", metricsCert.Name, err)
	}

	// 3. Copy app-plane secrets from app-ns → service-ns so the broker
	//    pod can mount them. No owner ref (cross-namespace).
	appToService := []struct {
		name string
		src  types.NamespacedName
		dst  types.NamespacedName
	}{
		{
			"server cert",
			types.NamespacedName{Name: AppServerCertName(cfg.AppName), Namespace: cfg.AppNamespace},
			types.NamespacedName{Name: AppServerCertName(cfg.AppName), Namespace: cfg.ServiceNamespace},
		},
		{
			"app CA trust",
			types.NamespacedName{Name: AppCATrustSecretName(cfg.AppName), Namespace: cfg.AppNamespace},
			types.NamespacedName{Name: AppCATrustSecretName(cfg.AppName), Namespace: cfg.ServiceNamespace},
		},
	}
	for _, c := range appToService {
		if err := CopyTLSSecret(ctx, cl, c.src, c.dst, nil, metav1.GroupVersionKind{}); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to copy %s for app %s: %w", c.name, cfg.AppName, err)
			}
			log.V(1).Info(c.name+" secret not yet available, will retry", "app", cfg.AppName)
			return fmt.Errorf("%s %s not yet available: %w", c.name, c.src, err)
		}
	}

	// 4. Copy metrics-plane secrets from service-ns → app-ns so the app
	//    can scrape metrics using its per-app cert and trust the endpoint.
	ownerGVK := cfg.AppOwnerGVK
	serviceToApp := []struct {
		name string
		src  types.NamespacedName
		dst  types.NamespacedName
	}{
		{
			"metrics cert",
			types.NamespacedName{Name: MetricsCertName(cfg.AppName), Namespace: cfg.ServiceNamespace},
			types.NamespacedName{Name: MetricsCertName(cfg.AppName), Namespace: cfg.AppNamespace},
		},
		{
			"metrics CA trust",
			types.NamespacedName{Name: MetricsCATrustSecretName(cfg.ServiceName), Namespace: cfg.ServiceNamespace},
			types.NamespacedName{Name: MetricsCATrustSecretName(cfg.ServiceName), Namespace: cfg.AppNamespace},
		},
	}
	for _, c := range serviceToApp {
		if err := CopyTLSSecret(ctx, cl, c.src, c.dst, cfg.AppOwner, ownerGVK); err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to copy %s for app %s: %w", c.name, cfg.AppName, err)
			}
			log.V(1).Info(c.name+" secret not yet available, will retry", "app", cfg.AppName)
			return fmt.Errorf("%s %s not yet available: %w", c.name, c.src, err)
		}
	}

	return nil
}

// createIfNotExists creates a cert-manager Certificate if it doesn't already exist.
func createIfNotExists(ctx context.Context, cl client.Client, cert *cmv1.Certificate) error {
	existing := &cmv1.Certificate{}
	key := types.NamespacedName{Name: cert.Name, Namespace: cert.Namespace}
	err := cl.Get(ctx, key, existing)
	if errors.IsNotFound(err) {
		if createErr := cl.Create(ctx, cert); createErr != nil {
			return createErr
		}
		log.V(1).Info("created certificate", "name", cert.Name, "namespace", cert.Namespace)
		return nil
	}
	return err
}

// --- Cleanup ---

// CleanupAppCert removes cross-namespace app cert-manager Certificate CRs.
func CleanupAppCert(ctx context.Context, cl client.Client, appName, serviceNamespace string) error {
	certs := []types.NamespacedName{
		{Name: AppCertName(appName), Namespace: serviceNamespace},
		{Name: AppServerCertName(appName), Namespace: serviceNamespace},
		{Name: MetricsCertName(appName), Namespace: serviceNamespace},
	}

	for _, certKey := range certs {
		cert := &cmv1.Certificate{}
		if err := cl.Get(ctx, certKey, cert); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get cross-ns cert for cleanup: %w", err)
		}
		if err := cl.Delete(ctx, cert); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete cross-ns cert %s: %w", certKey, err)
		}
		log.V(1).Info("cleaned up cross-ns app cert", "name", certKey.Name, "namespace", certKey.Namespace)
	}
	return nil
}

// CleanupAppProvisioningSecrets removes all cross-namespace provisioning
// secrets in both directions. Called from BrokerApp finalizer.
func CleanupAppProvisioningSecrets(ctx context.Context, cl client.Client, appName, appNamespace, serviceName, serviceNamespace string) error {
	toDelete := []types.NamespacedName{
		// App-plane copies in service namespace
		{Name: AppServerCertName(appName), Namespace: serviceNamespace},
		{Name: AppCATrustSecretName(appName), Namespace: serviceNamespace},
		// Metrics-plane copies in app namespace
		{Name: MetricsCertName(appName), Namespace: appNamespace},
		{Name: MetricsCATrustSecretName(serviceName), Namespace: appNamespace},
		// Legacy operator CA trust (migration cleanup)
		{Name: SvcCATrustForAppName(appName, serviceName), Namespace: appNamespace},
	}

	for _, key := range toDelete {
		secret := &corev1.Secret{}
		if err := cl.Get(ctx, key, secret); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get secret %s for cleanup: %w", key, err)
		}
		if err := cl.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete secret %s: %w", key, err)
		}
		log.V(1).Info("cleaned up provisioning secret", "name", key.Name, "namespace", key.Namespace)
	}

	certKey := types.NamespacedName{
		Name:      MetricsCertName(appName),
		Namespace: serviceNamespace,
	}
	cert := &cmv1.Certificate{}
	if err := cl.Get(ctx, certKey, cert); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get metrics cert CR for cleanup: %w", err)
		}
	} else {
		if err := cl.Delete(ctx, cert); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete metrics cert CR %s: %w", certKey, err)
		}
		log.V(1).Info("cleaned up metrics cert CR", "name", certKey.Name, "namespace", certKey.Namespace)
	}

	return nil
}

// --- Helpers ---

func buildCATrustDesired(ctx context.Context, cl client.Client, rootCertSecretKey, destKey types.NamespacedName, owner metav1.Object, ownerGVK metav1.GroupVersionKind) (client.Object, error) {
	source := &corev1.Secret{}
	if err := cl.Get(ctx, rootCertSecretKey, source); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	caCrt, ok := source.Data["tls.crt"]
	if !ok {
		return nil, nil
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      destKey.Name,
			Namespace: destKey.Namespace,
			Labels:    managedLabels(""),
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(owner, ownerGVK),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"tls.crt": caCrt,
		},
	}, nil
}

// DetectPKIClash checks whether any cert-manager Certificate or Issuer
// resources exist in the service namespace with names the operator would
// auto-generate, but without the managed-by label. Returns a human-readable
// description of each clash, or nil if no clashes are found.
func DetectPKIClash(ctx context.Context, cl client.Client, serviceName, namespace string) []string {
	certNames := []string{
		RootCertName(serviceName),
		BrokerCertName(serviceName),
		OperatorCertName(serviceName),
		MetricsRootCertName(serviceName),
		PrometheusCertName(serviceName),
	}
	issuerNames := []string{
		RootIssuerName(serviceName),
		CAIssuerName(serviceName),
		MetricsIssuerName(serviceName),
		MetricsCAIssuerName(serviceName),
	}

	var clashes []string

	for _, name := range certNames {
		cert := &cmv1.Certificate{}
		key := types.NamespacedName{Name: name, Namespace: namespace}
		if err := cl.Get(ctx, key, cert); err != nil {
			continue
		}
		if cert.Labels[LabelManagedBy] != ManagedByValue {
			clashes = append(clashes, "Certificate/"+name)
		}
	}

	for _, name := range issuerNames {
		issuer := &cmv1.Issuer{}
		key := types.NamespacedName{Name: name, Namespace: namespace}
		if err := cl.Get(ctx, key, issuer); err != nil {
			continue
		}
		if issuer.Labels[LabelManagedBy] != ManagedByValue {
			clashes = append(clashes, "Issuer/"+name)
		}
	}

	return clashes
}

// FormatPKIClashMessage produces a status-friendly message listing the
// conflicting resources.
func FormatPKIClashMessage(clashes []string) string {
	return "manual PKI resources conflict with operator-managed names: " +
		strings.Join(clashes, ", ") +
		". Remove or rename these resources to allow reconciliation."
}
