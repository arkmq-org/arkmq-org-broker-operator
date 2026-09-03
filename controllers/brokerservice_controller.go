/*
Copyright 2026.

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
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/appselector"
	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	servicemetrics "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/metrics"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gopkcs12 "software.sslmate.com/src/go-pkcs12"
)

type BrokerServiceReconciler struct {
	*ReconcilerLoop
}

type BrokerServiceInstanceReconciler struct {
	*BrokerServiceReconciler
	instance *broker.BrokerService
	status   *broker.BrokerServiceStatus
}

func NewBrokerServiceReconciler(client client.Client, scheme *runtime.Scheme, config *rest.Config, logger logr.Logger) *BrokerServiceReconciler {
	reconciler := BrokerServiceReconciler{
		ReconcilerLoop: &ReconcilerLoop{KubeBits: &KubeBits{client, scheme, config, logger}},
	}
	reconciler.ReconcilerLoopType = &reconciler
	return &reconciler
}

//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps,verbs=get;list;watch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps/status,verbs=get;list;watch
//+kubebuilder:rbac:groups=cert-manager.io,namespace=arkmq-org-broker-operator,resources=issuers;certificates,verbs=get;list;watch;create;update;patch;delete

func (reconciler *BrokerServiceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := reconciler.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "BrokerService")

	instance := &broker.BrokerService{}
	var err = reconciler.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Clean up metrics when service is deleted
			servicemetrics.DeleteServiceMetrics(request.Name, request.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	localLoop := &ReconcilerLoop{
		KubeBits:           reconciler.KubeBits,
		ReconcilerLoopType: reconciler,
	}

	processor := BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{
			ReconcilerLoop: localLoop,
		},
		instance: instance,
		status:   instance.Status.DeepCopy(),
	}

	reqLogger.V(2).Info("Reconciler Processing...", "CRD.Name", instance.Name, "CRD ver", instance.ObjectMeta.ResourceVersion, "CRD Gen", instance.ObjectMeta.Generation)

	// Validate spec first, before doing any work
	if err = processor.validateSpec(); err == nil {
		if err = processor.InitDeployed(instance, processor.getOwned()...); err == nil {
			if err = processor.processSpec(); err == nil {
				err = processor.SyncDesiredWithDeployed(instance)
			}
		}
	}

	reqLogger.V(2).Info("Reconciler Processed...", "CRD.Name", instance.Name, "CRD ver", instance.ObjectMeta.ResourceVersion, "CRD Gen", instance.ObjectMeta.Generation, "error", err)

	statusErr := processor.processStatus(err)
	if err != nil {
		// Handle reconcile error based on type
		if _, ok := err.(*ValidationError); ok {
			// Validation error - don't retry (wait for spec change)
			return ctrl.Result{}, nil
		}

		// exponential backoff retry
		return ctrl.Result{}, err
	}
	if statusErr != nil {
		return ctrl.Result{}, fmt.Errorf("Failed to update status: error %v", statusErr)
	}

	// Success — pending AppsProvisioned is observed via Owns(Broker) after
	// the sidecar signals config reload. Do not timer-requeue.
	return ctrl.Result{}, nil
}

// instance specifics for a reconciler loop
func (r *BrokerServiceReconciler) getOwned() []client.ObjectList {
	return []client.ObjectList{
		&cmv1.IssuerList{},
		&cmv1.CertificateList{},
		&corev1.SecretList{},
		&broker.BrokerList{},
		&corev1.ServiceList{}}
}

func (r *BrokerServiceReconciler) getOrderedTypeList() []reflect.Type {
	return []reflect.Type{
		reflect.TypeOf(cmv1.Issuer{}),
		reflect.TypeOf(cmv1.Certificate{}),
		reflect.TypeOf(corev1.Secret{}),
		reflect.TypeOf(broker.Broker{}),
		reflect.TypeOf(corev1.Service{})}
}

func (reconciler *BrokerServiceInstanceReconciler) validateSpec() error {
	// Validate resource name
	if err := ValidateResourceName(reconciler.instance.Name); err != nil {
		return err
	}

	if common.CertManagerDegraded() {
		return NewValidationError(
			broker.ValidConditionCertManagerUnavailable,
			"cert-manager CRDs no longer available; certificate renewal disabled")
	}

	if clashes := cot.DetectPKIClash(context.TODO(), reconciler.Client,
		reconciler.instance.Name, reconciler.instance.Namespace); len(clashes) > 0 {
		return NewValidationError(
			broker.ValidConditionPKINameClash,
			"%s", cot.FormatPKIClashMessage(clashes))
	}

	// Validate CEL expression if provided
	if reconciler.instance.Spec.AppSelectorExpression != "" {
		if err := appselector.ValidateExpression(reconciler.instance.Spec.AppSelectorExpression); err != nil {
			return NewValidationError(
				broker.ValidConditionSpecSelectorError,
				"invalid appSelectorExpression: %v", err)
		}
	}

	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) processSpec() (err error) {
	if err = reconciler.ensurePKI(); err != nil {
		return err
	}
	if err = reconciler.processBroker(); err != nil {
		return err
	}
	return reconciler.processService()
}

func (reconciler *BrokerServiceInstanceReconciler) ensurePKI() error {
	opCfg := &cot.PKIConfig{
		ServiceName:      reconciler.instance.Name,
		ServiceNamespace: reconciler.instance.Namespace,
		Owner:            reconciler.instance,
	}
	for _, obj := range cot.EnsureOperatorPKI(opCfg, common.GetClusterDomain()) {
		reconciler.TrackDesired(obj)
	}

	metCfg := reconciler.metricsConfig()
	objects, err := cot.EnsureMetricsPKI(context.TODO(), reconciler.Client, metCfg)
	if err != nil {
		return err
	}
	for _, obj := range objects {
		reconciler.TrackDesired(obj)
	}
	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) metricsConfig() *cot.MetricsPKIConfig {
	svcOwnerGVK := metav1.GroupVersionKind{
		Group:   broker.GroupVersion.Group,
		Version: broker.GroupVersion.Version,
		Kind:    "BrokerService",
	}
	cfg := &cot.MetricsPKIConfig{
		ServiceName:      reconciler.instance.Name,
		ServiceNamespace: reconciler.instance.Namespace,
		ClusterDomain:    common.GetClusterDomain(),
		Owner:            reconciler.instance,
		OwnerGVK:         svcOwnerGVK,
	}
	if pki := reconciler.instance.Spec.PKI; pki != nil && pki.Metrics != nil {
		cfg.CATemplate = pki.Metrics.CA
		cfg.LeafTemplate = pki.Metrics.Leaf
	}
	return cfg
}

func (reconciler *BrokerServiceInstanceReconciler) processBroker() (err error) {

	var desired *broker.Broker
	obj := reconciler.CloneOfDeployed(reflect.TypeOf(broker.Broker{}), reconciler.instance.Name)
	if obj != nil {
		desired = obj.(*broker.Broker)
	} else {
		desired = common.GenerateBroker(reconciler.instance.Name, reconciler.instance.Namespace)
	}
	desired.Spec.PersistenceEnabled = false
	desired.Spec.Labels = map[string]string{
		// Standard Kubernetes labels
		common.LabelAppKubernetesInstance:  reconciler.instance.Name,
		common.LabelAppKubernetesComponent: "broker-service",
		common.LabelAppKubernetesManagedBy: "arkmq-org-broker-operator",
		// Domain-specific labels
		common.LabelBrokerService:   reconciler.instance.Name,
		common.LabelBrokerPeerIndex: "0",
	}
	desired.Spec.Env = reconciler.instance.Spec.Env
	desired.Spec.Resources = reconciler.instance.Spec.Resources

	if reconciler.instance.Spec.Image != nil {
		desired.Spec.Image = *reconciler.instance.Spec.Image
	}

	desired.Spec.ExtraMounts.Secrets = []string{
		reconciler.appPropertiesSecretName(),
		reconciler.appCertsSecretName(),
	}

	err = reconciler.processAppSecrets()

	reconciler.TrackDesired(desired)

	return err
}

func (reconciler *BrokerServiceInstanceReconciler) processAppSecrets() (err error) {
	// avoid restart for app onboarding with existing mount points
	// TODO potentially N app-secrets to overcome 1Mb size limit
	propsName := types.NamespacedName{
		Namespace: reconciler.instance.Namespace,
		Name:      reconciler.appPropertiesSecretName(),
	}
	certsName := types.NamespacedName{
		Namespace: reconciler.instance.Namespace,
		Name:      reconciler.appCertsSecretName(),
	}

	var propsSecret *corev1.Secret
	obj := reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Secret{}), propsName.Name)
	if obj != nil {
		propsSecret = obj.(*corev1.Secret)
	} else {
		propsSecret = secrets.NewSecret(propsName, nil, nil)
	}

	var certsSecret *corev1.Secret
	obj = reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Secret{}), certsName.Name)
	if obj != nil {
		certsSecret = obj.(*corev1.Secret)
	} else {
		certsSecret = secrets.NewSecret(certsName, nil, nil)
	}

	// find all apps that select this service
	apps := &broker.BrokerAppList{}
	key := reconciler.instance.Namespace + ":" + reconciler.instance.Name
	if err = reconciler.Client.List(context.TODO(), apps, client.MatchingFields{common.AppServiceBindingField: key}); err != nil {
		return err
	}

	// Preserve stable PKCS12 data across resets to avoid non-deterministic
	// regeneration (PKCS12 uses random salts/IVs on each encode).
	savedP12 := propsSecret.Data["_prometheus-cert.p12"]
	savedP12FP := propsSecret.Data["_prometheus-cert.p12.fp"]

	// reset data
	propsSecret.Data = make(map[string][]byte)
	certsSecret.Data = make(map[string][]byte)

	if savedP12 != nil {
		propsSecret.Data["_prometheus-cert.p12"] = savedP12
		propsSecret.Data["_prometheus-cert.p12.fp"] = savedP12FP
	}
	appIdentities := make([]string, 0, len(apps.Items))
	rejectedApps := make([]broker.RejectedApp, 0)
	validApps := make([]broker.BrokerApp, 0, len(apps.Items))

	for _, app := range apps.Items {
		// Phase gate: only pack apps that have completed cert provisioning.
		// The BrokerApp reconciler is the sole Phase writer; by the time Phase
		// reaches Provisioning all cross-ns secret copies are confirmed.
		if app.Status.Phase != broker.BrokerAppPhaseProvisioning &&
			app.Status.Phase != broker.BrokerAppPhaseProvisioned {
			reconciler.log.V(1).Info("skipping app not in Provisioning/Provisioned phase",
				"app", appName(&app), "phase", app.Status.Phase)
			continue
		}

		valid, rejectionReason := reconciler.validateAppForProvisioning(&app, key)
		if !valid {
			rejectedApps = append(rejectedApps, broker.RejectedApp{
				Name:      app.Name,
				Namespace: app.Namespace,
				Reason:    rejectionReason,
			})
			continue
		}

		if err = common.ValidateResourceName(app.Name); err != nil {
			reconciler.log.Error(err, "invalid app name", "app", app.Name)
			break
		}
		if err = reconciler.processCapabilities(propsSecret, &app); err != nil {
			reconciler.log.Error(err, "failed to process capabilities for app", "app", app.Name)
			break
		}
		if err = reconciler.processAcceptor(propsSecret, &app); err != nil {
			reconciler.log.Error(err, "failed to process acceptor for app", "app", app.Name)
			break
		}
		if err = reconciler.packAppCertData(certsSecret, &app); err != nil {
			// Phase guarantees secrets exist, so this should not happen.
			// Log as error for visibility and skip the app defensively.
			reconciler.log.Error(err, "BUG: cert data unavailable for app in Provisioning phase", "app", app.Name)
			err = nil
			continue
		}
		appIdentities = append(appIdentities, AppIdentity(&app))
		validApps = append(validApps, app)
	}

	// Pack prometheus cert data into the props secret (not certs) to keep the
	// PEMCFG file in the same mount that the JMX exporter reads, matching the
	// original layout that the SSLFactory expects.
	reconciler.packPrometheusCertData(propsSecret)

	sort.Strings(appIdentities)
	if propsSecret.Annotations == nil {
		propsSecret.Annotations = make(map[string]string)
	}
	propsSecret.Annotations[common.ProvisionedAppsAnnotation] = strings.Join(appIdentities, ",")

	reconciler.status.RejectedApps = rejectedApps

	reconciler.TrackDesired(propsSecret)
	reconciler.TrackDesired(certsSecret)

	if err == nil {
		err = reconciler.processControlPlaneOverrideSecret(validApps)
	}

	return err
}

func (reconciler *BrokerServiceInstanceReconciler) packAppCertData(certsSecret *corev1.Secret, app *broker.BrokerApp) error {
	ns := app.Namespace
	name := app.Name
	svcNs := reconciler.instance.Namespace

	serverCertKey := types.NamespacedName{Name: cot.AppServerCertName(name), Namespace: svcNs}
	serverCert := &corev1.Secret{}
	if err := reconciler.Get(context.TODO(), serverCertKey, serverCert); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("server cert %s not yet available", serverCertKey)
		}
		return err
	}

	caTrustKey := types.NamespacedName{Name: cot.AppCATrustSecretName(name), Namespace: svcNs}
	caTrust := &corev1.Secret{}
	if err := reconciler.Get(context.TODO(), caTrustKey, caTrust); err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("CA trust %s not yet available", caTrustKey)
		}
		return err
	}

	prefix := fmt.Sprintf("%s--%s", ns, name)
	certsSecret.Data[prefix+"--ca--tls.crt"] = caTrust.Data["tls.crt"]
	certsSecret.Data[prefix+"--broker-cert--tls.key"] = serverCert.Data["tls.key"]
	certsSecret.Data[prefix+"--broker-cert--tls.crt"] = serverCert.Data["tls.crt"]

	certsMount := fmt.Sprintf("/amq/extra/secrets/%s", reconciler.appCertsSecretName())
	pemcfg := fmt.Sprintf("source.key=%s/%s--broker-cert--tls.key\nsource.cert=%s/%s--broker-cert--tls.crt\n",
		certsMount, prefix, certsMount, prefix)
	certsSecret.Data[prefix+"--broker-cert--tls.pemcfg"] = []byte(pemcfg)

	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) packPrometheusCertData(propsSecret *corev1.Secret) {
	svcName := reconciler.instance.Name
	svcNs := reconciler.instance.Namespace

	metricsCAKey := types.NamespacedName{Name: cot.MetricsRootCertSecretName(svcName), Namespace: svcNs}
	metricsCASec := &corev1.Secret{}
	if err := reconciler.Get(context.TODO(), metricsCAKey, metricsCASec); err != nil {
		reconciler.log.V(1).Info("metrics CA not yet available, falling back to operator CA", "error", err)
		reconciler.packPrometheusLegacyFallback(propsSecret)
		return
	}

	promCertKey := types.NamespacedName{Name: cot.PrometheusCertName(svcName), Namespace: svcNs}
	promSec := &corev1.Secret{}
	if err := reconciler.Get(context.TODO(), promCertKey, promSec); err != nil {
		reconciler.log.V(1).Info("prometheus cert not yet available, falling back to operator CA", "error", err)
		reconciler.packPrometheusLegacyFallback(propsSecret)
		return
	}

	propsSecret.Data["_prometheus-ca-tls.crt"] = metricsCASec.Data["tls.crt"]

	p12, err := stableP12(propsSecret, "_prometheus-cert.p12",
		promSec.Data["tls.crt"], promSec.Data["tls.key"], prometheusP12Password)
	if err != nil {
		reconciler.log.Error(err, "failed to convert prometheus cert to PKCS12, falling back")
		reconciler.packPrometheusLegacyFallback(propsSecret)
		return
	}
	propsSecret.Data["_prometheus-cert.p12"] = p12
}

func (reconciler *BrokerServiceInstanceReconciler) packPrometheusLegacyFallback(propsSecret *corev1.Secret) {
	svcName := reconciler.instance.Name
	svcNs := reconciler.instance.Namespace

	opCAKey := types.NamespacedName{Name: cot.RootCertSecretName(svcName), Namespace: svcNs}
	opCASec := &corev1.Secret{}
	if err := reconciler.Get(context.TODO(), opCAKey, opCASec); err != nil {
		return
	}

	brokerCertKey := types.NamespacedName{Name: cot.BrokerCertName(svcName), Namespace: svcNs}
	brokerSec := &corev1.Secret{}
	if err := reconciler.Get(context.TODO(), brokerCertKey, brokerSec); err != nil {
		return
	}

	propsSecret.Data["_prometheus-ca-tls.crt"] = opCASec.Data["tls.crt"]

	p12, err := stableP12(propsSecret, "_prometheus-cert.p12",
		brokerSec.Data["tls.crt"], brokerSec.Data["tls.key"], prometheusP12Password)
	if err != nil {
		return
	}
	propsSecret.Data["_prometheus-cert.p12"] = p12
}

const prometheusP12Password = "changeit"

func pemFingerprint(certPEM, keyPEM []byte) string {
	h := sha256.New()
	h.Write(certPEM)
	h.Write(keyPEM)
	return hex.EncodeToString(h.Sum(nil))
}

// stableP12 returns the existing PKCS12 bytes from the secret if the source
// PEM hasn't changed, avoiding non-deterministic regeneration that would cause
// an infinite reconcile loop (PKCS12 encryption uses random salts/IVs).
func stableP12(secret *corev1.Secret, p12Key string, certPEM, keyPEM []byte, password string) ([]byte, error) {
	fp := pemFingerprint(certPEM, keyPEM)
	fpKey := p12Key + ".fp"

	if existing, ok := secret.Data[p12Key]; ok {
		if string(secret.Data[fpKey]) == fp {
			return existing, nil
		}
	}

	p12, err := pemToP12(certPEM, keyPEM, password)
	if err != nil {
		return nil, err
	}
	secret.Data[fpKey] = []byte(fp)
	return p12, nil
}

func pemToP12(certPEM, keyPEM []byte, password string) ([]byte, error) {
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse cert/key: %w", err)
	}
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse leaf cert: %w", err)
	}
	return gopkcs12.Modern.Encode(tlsCert.PrivateKey, leaf, nil, password)
}

func (reconciler *BrokerServiceInstanceReconciler) appPropertiesSecretName() string {
	return AppPropertiesSecretName(reconciler.instance.Name)
}

func (reconciler *BrokerServiceInstanceReconciler) appCertsSecretName() string {
	return cot.AppCertsSecretName(reconciler.instance.Name)
}

func AppPropertiesSecretName(name string) string {
	return fmt.Sprintf("%s-app%s", name, common.BrokerPropsSuffix)
}

func PropertiesSecretName(name string) string {
	return fmt.Sprintf("%s%s", name, common.BrokerPropsSuffix)
}

func (reconciler *BrokerServiceInstanceReconciler) setValidCondition(err error) {
	condition := metav1.Condition{
		Type:   broker.ValidConditionType,
		Status: metav1.ConditionTrue,
		Reason: broker.ValidConditionSuccessReason,
	}

	if validErr, ok := err.(*ValidationError); ok {
		condition.Status = metav1.ConditionFalse
		condition.Reason = validErr.ConditionReason()
		condition.Message = validErr.Error()
	}

	meta.SetStatusCondition(&reconciler.status.Conditions, condition)
}

func (reconciler *BrokerServiceInstanceReconciler) processStatus(reconcilerError error) (err error) {
	// Set Valid condition (always updated)
	reconciler.setValidCondition(reconcilerError)

	var deployedCondition metav1.Condition = metav1.Condition{
		Type:   broker.DeployedConditionType,
		Status: metav1.ConditionFalse,
		Reason: broker.DeployedConditionNotReadyReason,
	}

	var appsProvisionedCondition metav1.Condition = metav1.Condition{
		Type:   broker.AppsProvisionedConditionType,
		Status: metav1.ConditionFalse,
		Reason: broker.AppsProvisionedConditionWaitingReason,
	}

	if reconcilerError != nil {
		// Check if error is a TransientError with a specific reason
		if transErr, ok := reconcilerError.(*TransientError); ok {
			deployedCondition.Status = metav1.ConditionFalse
			deployedCondition.Reason = transErr.ConditionReason()
			deployedCondition.Message = transErr.Error()
		} else if validErr, ok := reconcilerError.(*ValidationError); ok {
			deployedCondition.Status = metav1.ConditionFalse
			deployedCondition.Reason = validErr.ConditionReason()
			deployedCondition.Message = validErr.Error()
		} else {
			// Generic error (API errors, update errors, etc.)
			deployedCondition.Status = metav1.ConditionUnknown
			deployedCondition.Reason = broker.DeployedConditionCrudKindErrorReason
			deployedCondition.Message = fmt.Sprintf("error on resource crud %v", reconcilerError)
		}
		appsProvisionedCondition.Reason = broker.AppsProvisionedConditionNotReadyReason
	} else {
		obj := reconciler.CloneOfDeployed(reflect.TypeOf(broker.Broker{}), reconciler.instance.Name)
		if obj != nil {
			deployed := obj.(*broker.Broker)
			brokerDeployed := meta.FindStatusCondition(deployed.Status.Conditions, broker.DeployedConditionType)

			if brokerDeployed != nil {
				if brokerDeployed.Status == metav1.ConditionTrue {
					// Broker is deployed
					deployedCondition.Status = metav1.ConditionTrue
					deployedCondition.Reason = broker.ReadyConditionReason
				} else {
					deployedCondition.Message = fmt.Sprintf("not ready broker status %v", deployed.Status)
				}
			}

			brokerReady := meta.FindStatusCondition(deployed.Status.Conditions, broker.ReadyConditionType)
			if brokerReady != nil && brokerReady.Status == metav1.ConditionTrue {

				appPropsSecretName := AppPropertiesSecretName(reconciler.instance.Name)
				var appliedSecretVersion string
				for _, ec := range deployed.Status.ExternalConfigs {
					if ec.Name == appPropsSecretName {
						appliedSecretVersion = ec.ResourceVersion
						break
					}
				}
				if appliedSecretVersion != "" {
					secret := &corev1.Secret{}
					secretKey := types.NamespacedName{Name: appPropsSecretName, Namespace: reconciler.instance.Namespace}
					if getErr := reconciler.Client.Get(context.TODO(), secretKey, secret); getErr == nil {
						if secret.ResourceVersion == appliedSecretVersion {
							appsProvisionedCondition.Status = metav1.ConditionTrue
							appsProvisionedCondition.Reason = broker.AppsProvisionedConditionSyncedReason
							if applied, ok := secret.Annotations[common.ProvisionedAppsAnnotation]; ok && applied != "" {
								reconciler.status.ProvisionedApps = strings.Split(applied, ",")
							} else {
								reconciler.status.ProvisionedApps = nil
							}
						}
					}
				}
			}
		}
	}
	meta.SetStatusCondition(&reconciler.status.Conditions, deployedCondition)
	meta.SetStatusCondition(&reconciler.status.Conditions, appsProvisionedCondition)

	common.SetReadyCondition(&reconciler.status.Conditions)

	if !reflect.DeepEqual(reconciler.instance.Status, *reconciler.status) {
		reconciler.instance.Status = *reconciler.status
		err = resources.UpdateStatus(reconciler.Client, reconciler.instance)
	}

	servicemetrics.UpdateServiceMetrics(
		reconciler.instance.Name,
		reconciler.instance.Namespace,
		len(reconciler.status.ProvisionedApps),
	)

	return err
}

// appName returns the formatted name of an app for logging (namespace/name).
func appName(app *broker.BrokerApp) string {
	return app.Namespace + "/" + app.Name
}

// serviceName returns the formatted name of a service for logging (namespace/name).
func serviceName(service *broker.BrokerService) string {
	return service.Namespace + "/" + service.Name
}

// getBindingValue returns the string value of a service binding, or "<none>" if nil.
func getBindingValue(service *broker.BrokerServiceBindingStatus) string {
	if service == nil {
		return "<none>"
	}
	return service.Key()
}

// appMatchesSelector validates that an app matches this service's appSelectorExpression.
// This is a security check to prevent apps from bypassing access control by manually
// setting the status.serviceBinding field.
func (reconciler *BrokerServiceInstanceReconciler) appMatchesSelector(app *broker.BrokerApp) bool {
	matches, err := appselector.Matches(app, reconciler.instance, reconciler.Client)
	if err != nil {
		return false
	}
	return matches
}

// validateAppForProvisioning performs all security checks to ensure an app is authorized
// to be provisioned by this service.
// Returns (valid, reason):
//   - (true, "") if app should be provisioned
//   - (false, reason) if app fails validation and should be rejected
func (reconciler *BrokerServiceInstanceReconciler) validateAppForProvisioning(app *broker.BrokerApp, serviceKey string) (bool, string) {
	// App's label selector matches service labels
	if app.Spec.ServiceSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(app.Spec.ServiceSelector)
		if err != nil {
			reconciler.log.Error(err, "Rejecting app with invalid label selector",
				"app", appName(app))
			return false, "invalid label selector"
		}
		if !selector.Matches(labels.Set(reconciler.instance.Labels)) {
			reconciler.log.Info("Rejecting app that does not match service labels (status.serviceBinding manually set?)",
				"app", appName(app),
				"service", serviceName(reconciler.instance),
				"appSelector", app.Spec.ServiceSelector,
				"serviceLabels", reconciler.instance.Labels)
			return false, "does not match service labels"
		}
	}

	// App matches CEL selector expression
	if !reconciler.appMatchesSelector(app) {
		reconciler.log.Info("Rejecting app that does not match appSelectorExpression (status.serviceBinding manually set?)",
			"app", appName(app),
			"service", serviceName(reconciler.instance),
			"expression", reconciler.instance.Spec.AppSelectorExpression)
		return false, "does not match appSelectorExpression"
	}

	return true, ""
}

func (reconciler *BrokerServiceInstanceReconciler) processService() error {

	var desired *corev1.Service

	obj := reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Service{}), reconciler.instance.Name)
	if obj != nil {
		desired = obj.(*corev1.Service)
	} else {
		desired = &corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      reconciler.instance.Name,
				Namespace: reconciler.instance.Namespace,
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: corev1.ClusterIPNone,
			},
		}
	}

	desired.Spec.Selector = map[string]string{
		common.LabelBrokerService: reconciler.instance.Name,
	}
	reconciler.TrackDesired(desired)
	return nil
}

// appToServiceHandler handles BrokerApp events and enqueues the affected BrokerService(s).
// On Update, it enqueues both the old and new service if the binding changed.
type appToServiceHandler struct{}

func (h *appToServiceHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	if req := h.getServiceRequest(evt.Object); req != nil {
		q.Add(*req)
	}
}

func (h *appToServiceHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	oldApp := evt.ObjectOld.(*broker.BrokerApp)
	newApp := evt.ObjectNew.(*broker.BrokerApp)

	oldService := oldApp.Status.Service
	newService := newApp.Status.Service

	// Enqueue old service if binding existed
	if oldService != nil {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: oldService.Namespace,
				Name:      oldService.Name,
			},
		})
	}

	// Enqueue new service if binding exists and is different from old
	if newService != nil && !sameService(oldService, newService) {
		q.Add(reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: newService.Namespace,
				Name:      newService.Name,
			},
		})
	}
}

func sameService(a, b *broker.BrokerServiceBindingStatus) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Namespace == b.Namespace && a.Name == b.Name
}

func (h *appToServiceHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if req := h.getServiceRequest(evt.Object); req != nil {
		q.Add(*req)
	}
}

func (h *appToServiceHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	if req := h.getServiceRequest(evt.Object); req != nil {
		q.Add(*req)
	}
}

func (h *appToServiceHandler) getServiceRequest(obj client.Object) *reconcile.Request {
	app := obj.(*broker.BrokerApp)
	if app.Status.Service != nil {
		return &reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: app.Status.Service.Namespace,
				Name:      app.Status.Service.Name,
			},
		}
	}
	return nil
}

func (r *BrokerServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Note: Namespace informer is set up in main.go for CEL evaluation

	// Index BrokerApp by status.service for efficient lookup
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &broker.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
		app := rawObj.(*broker.BrokerApp)
		if app.Status.Service != nil {
			return []string{app.Status.Service.Key()}
		}
		return nil
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&broker.BrokerService{}).
		Owns(&broker.Broker{}).
		Owns(&cmv1.Issuer{}).
		Owns(&cmv1.Certificate{}).
		Watches(&broker.BrokerApp{}, &appToServiceHandler{}).
		Complete(r)
}

type AddressConfig struct {
	senderRoles     map[string]string
	consumerRoles   map[string]string
	subscriberRoles map[string]string

	// isOwned indicates this app should generate addressConfigurations for this address.
	// True when appNamespace/appName are empty (local reference).
	// False when appNamespace/appName are set (cross-app reference - owner generates config).
	isOwned bool

	isMulticast bool
}

type AddressTracker struct {
	names map[string]*AddressConfig
}

func newAddressTracker() *AddressTracker {
	return &AddressTracker{names: map[string]*AddressConfig{}}
}

func (t *AddressTracker) newAddressConfig() *AddressConfig {
	return &AddressConfig{senderRoles: map[string]string{}, consumerRoles: map[string]string{},
		subscriberRoles: map[string]string{}, isOwned: false, isMulticast: false}
}

func (t *AddressTracker) track(address *broker.AddressRef) *AddressConfig {
	entry, present := t.names[address.Address]
	if !present {
		entry = t.newAddressConfig()
		t.names[address.Address] = entry
	}

	// If AppNamespace and AppName are empty, this app owns the address
	if address.AppNamespace == "" && address.AppName == "" {
		entry.isOwned = true
	}

	return entry
}

func (t *AddressTracker) trackAddressType(addrType *broker.AddressType) *AddressConfig {
	localAddr := &broker.AddressRef{
		Address: addrType.Address,
		// AppNamespace and AppName empty = owned/local address
	}
	addressConfig := t.track(localAddr)
	if isMulticastAddress(addrType.PubSub, addrType.Subscriptions) {
		addressConfig.isMulticast = true
		for _, subName := range addrType.Subscriptions {
			fqqnEntry := t.track(&broker.AddressRef{Address: addrType.Address + FQQNSeparator + subName})
			fqqnEntry.isMulticast = true
		}
	}
	return addressConfig
}

func (reconciler *BrokerServiceInstanceReconciler) processCapabilities(secret *corev1.Secret, app *broker.BrokerApp) (err error) {
	addressTracker := newAddressTracker()

	role := AppIdentity(app)

	// First, track addresses declared in spec.addresses (private, owned by this app)
	// This ensures addressConfigurations are generated even if the app has no capabilities
	for _, addrType := range app.Spec.Addresses {
		addressTracker.trackAddressType(&addrType)
	}

	// Also track addresses declared in spec.sharedAddresses (public, owned by this app)
	for _, addrType := range app.Spec.SharedAddresses {
		addressTracker.trackAddressType(&addrType)
	}

	// Then, process capabilities to find inline addresses and capture roles
	for _, capability := range app.Spec.Capabilities {

		var entry *AddressConfig

		// Process ProducerOf
		for _, addressRef := range capability.ProducerOf {
			entry = addressTracker.track(&addressRef)
			entry.senderRoles[role] = role

			// Handle pub/sub declaration (can be explicit via pubSub:true or inferred from subscriptions)
			if isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
				// pubSub=true (explicit) or has subscriptions means "this is a MULTICAST address"
				// Validation already ensures subscriptions is empty (no queue names allowed for producers)
				entry.isMulticast = true
			}
		}

		// Process ConsumerOf
		for _, addressRef := range capability.ConsumerOf {
			entry = addressTracker.track(&addressRef)

			if !isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
				// ANYCAST - direct queue consumption (no pubSub, no subscriptions)
				entry.consumerRoles[role] = role

				// Validate idempotency: check if address was already marked as MULTICAST
				if entry.isOwned && entry.isMulticast {
					return fmt.Errorf(
						"address '%s' is referenced with both pubSub and non pubSub semantics. "+
							"This creates a conflict. Use consistent semantics for the same address",
						addressRef.Address)
				}
			} else {
				// MULTICAST - multicast queue-based consumption (pubSub=true or has subscriptions)
				// Validation already ensures len > 0 (empty subscriptions not allowed for consumers)

				// Validate idempotency: check if address was already used with ANYCAST
				if entry.isOwned && len(entry.consumerRoles) > 0 && !entry.isMulticast {
					return fmt.Errorf(
						"address '%s' is referenced with both pubSub and non pubSub semantics. "+
							"This creates a conflict. Use consistent semantics for the same address",
						addressRef.Address)
				}

				entry.isMulticast = true

				// Generate FQQN for each multicast queue
				for _, queueName := range addressRef.Subscriptions {
					fqqn := addressRef.Address + FQQNSeparator + queueName
					queueEntry := addressTracker.track(&broker.AddressRef{
						Address:      fqqn,
						AppNamespace: addressRef.AppNamespace,
						AppName:      addressRef.AppName,
					})
					queueEntry.subscriberRoles[role] = role
					queueEntry.isMulticast = true
				}
			}
		}
	}

	props := map[string]string{} // need to dedup

	// Track all queue names for metrics generation
	queueNamesForMetrics := make(map[string]bool)

	for addressName, addr := range addressTracker.names {
		escapedAddressName := escapeForProperties(addressName)

		var address, queueName string
		fqqn := strings.SplitN(addressName, FQQNSeparator, 2)
		isFQQN := len(fqqn) > 1
		if isFQQN {
			address = escapeForProperties(fqqn[0])
			queueName = escapeForProperties(fqqn[1])
		} else {
			address = escapedAddressName
			queueName = escapedAddressName
		}

		// Only generate routingTypes for addresses owned by this app
		// (not cross-app references where AppNamespace/AppName are set)
		if addr.isOwned {
			if addr.isMulticast {
				props[fmt.Sprintf("addressConfigurations.\"%s\".routingTypes=MULTICAST\n", address)] = ""
			} else {
				props[fmt.Sprintf("addressConfigurations.\"%s\".routingTypes=ANYCAST\n", address)] = ""
			}
		}

		// Generate ANYCAST queue configs for all non-multicast addresses
		// (both owned and referenced with consumer capability)
		if !addr.isMulticast {
			props[fmt.Sprintf("addressConfigurations.\"%s\".queueConfigs.\"%s\".routingType=ANYCAST\n", address, queueName)] = ""
			props[fmt.Sprintf("addressConfigurations.\"%s\".queueConfigs.\"%s\".address=%s\n", address, queueName, address)] = ""
			queueNamesForMetrics[queueName] = true
		}

		// Generate MULTICAST queue configs for FQQN (subscription) addresses
		if isFQQN {
			props[fmt.Sprintf("addressConfigurations.\"%s\".queueConfigs.\"%s\".routingType=MULTICAST\n", address, queueName)] = ""
			props[fmt.Sprintf("addressConfigurations.\"%s\".queueConfigs.\"%s\".address=%s\n", address, queueName, address)] = ""
			queueNamesForMetrics[queueName] = true
		}

		// Always generate RBAC roles (for both owned and referenced addresses)
		// use fqqn/escapedAddressName as is for RBAC
		for _, role := range addr.senderRoles {
			props[fmt.Sprintf("securityRoles.\"%s\".\"%s\".send=true\n", escapedAddressName, producerRole(role))] = ""

		}
		for _, role := range addr.consumerRoles {
			props[fmt.Sprintf("securityRoles.\"%s\".\"%s\".consume=true\n", escapedAddressName, consumerRole(role))] = ""
		}

		// 2026-05-12T17:17:22.906184695Z Thread-0 (activemq-brokerservice-mqtt119a) INFO AMQ601264: User test-mqtt-app(test-mqtt-app-consumer,test-mqtt-app-producer)@10.244.12.28:45426 gets security check failure, reason = AMQ229213:
		// User: test-mqtt-app does not have permission='CONSUME' for queue my-client.mytopic.* on address mytopic.*
		// securityRoles."(mytopic.*\:\:my-client.mytopic.*)"."test-mqtt-app-consumer".consume=true

		for _, role := range addr.subscriberRoles {
			// but security store does not support literal match markers!
			// https://issues.apache.org/jira/browse/ARTEMIS-6057
			//			props[fmt.Sprintf("securityRoles.\"(%s)\".\"%s\".consume=true\n", escapedAddressName, consumerRole(role))] = ""
			props[fmt.Sprintf("securityRoles.\"%s\".\"%s\".consume=true\n", escapedAddressName, consumerRole(role))] = ""
		}
	}

	// Generate metrics roles for all queues
	for queueName := range queueNamesForMetrics {
		for _, rbacRole := range []string{"metrics", metricsRole(AppIdentity(app))} {
			// mbean server query
			props[fmt.Sprintf("securityRoles.\"mops.queue.%s\".\"%s\".view=true\n", queueName, rbacRole)] = ""

			// attributes
			props[fmt.Sprintf("securityRoles.\"mops.queue.%s.getMessageCount\".\"%s\".view=true\n", queueName, rbacRole)] = ""
			props[fmt.Sprintf("securityRoles.\"mops.queue.%s.getConsumerCount\".\"%s\".view=true\n", queueName, rbacRole)] = ""
			props[fmt.Sprintf("securityRoles.\"mops.queue.%s.getDeliveringCount\".\"%s\".view=true\n", queueName, rbacRole)] = ""
			props[fmt.Sprintf("securityRoles.\"mops.queue.%s.getPersistentSize\".\"%s\".view=true\n", queueName, rbacRole)] = ""
		}
	}

	buf := brokerproperties.NewPropsWithHeader()
	for _, k := range brokerproperties.SortedKeys(props) {
		fmt.Fprint(buf, k)
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[AppIdentityPrefixed(app, "capabilities.properties")] = buf.Bytes()

	return err
}

func (reconciler *BrokerServiceInstanceReconciler) processAcceptor(serverConfigPropertiesSecret *corev1.Secret, app *broker.BrokerApp) (err error) {
	certsPath := fmt.Sprintf("/amq/extra/secrets/%s", reconciler.appCertsSecretName())
	appPrefix := fmt.Sprintf("%s--%s", app.Namespace, app.Name)
	trustStorePath := fmt.Sprintf("%s/%s--ca--tls.crt", certsPath, appPrefix)
	keyStorePath := fmt.Sprintf("%s/%s--broker-cert--tls.pemcfg", certsPath, appPrefix)

	namespacedName := AppIdentity(app)

	realmName := jaasConfigRealmName(app)

	// process authN cert login module params

	/* TODO: pull down full DN from app cert

	var appCert *tls.Certificate
	if appCert, err = common.ExtractCertFromSecret(...); err != nil {
		return nil, err
	}

	var appCertSubject *pkix.Name
	if operatorCertSubject, err = common.ExtractCertSubject(appCert); err != nil {
		return nil, err
	}
	*/
	usersBuf := brokerproperties.NewPropsWithHeader()
	// Escape app name for safe use in regex pattern to prevent regex injection
	// The namespacedName format is namespace-name which is already validated
	escapedAppName := common.EscapeForRegex(app.Name)
	fmt.Fprintf(usersBuf, "%s=/.*%s.*/\n", namespacedName, escapedAppName)

	certUsersCfgKey := UnderscoreAppIdentityPrefixed(app, common.GetCertUsersKey(realmName))
	serverConfigPropertiesSecret.Data[certUsersCfgKey] = usersBuf.Bytes()

	dedupMap := map[string]string{}
	for _, capability := range app.Spec.Capabilities {

		if len(capability.ConsumerOf) > 0 {
			dedupMap[fmt.Sprintf("%s=%s\n", consumerRole(namespacedName), namespacedName)] = ""
		}
		if len(capability.ProducerOf) > 0 {
			dedupMap[fmt.Sprintf("%s=%s\n", producerRole(namespacedName), namespacedName)] = ""
		}
	}

	rolesBuf := brokerproperties.NewPropsWithHeader()
	for _, k := range brokerproperties.SortedKeys(dedupMap) {
		fmt.Fprint(rolesBuf, k)
	}

	certRolesCfgKey := UnderscoreAppIdentityPrefixed(app, common.GetCertRolesKey(realmName))
	serverConfigPropertiesSecret.Data[certRolesCfgKey] = rolesBuf.Bytes()

	acceptorCfgKey := AppIdentityPrefixed(app, "acceptor.properties")

	buf := brokerproperties.NewPropsWithHeader()

	if app.Status.Service == nil {
		return fmt.Errorf("app %s has no service binding", AppIdentity(app))
	}
	port := app.Status.Service.AssignedPort
	if port == UnassignedPort {
		return fmt.Errorf("app %s has no assigned port", AppIdentity(app))
	}

	name := fmt.Sprintf("%d", port)
	fmt.Fprintln(buf, "# tls acceptor")

	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".factoryClassName=org.apache.activemq.artemis.core.remoting.impl.netty.NettyAcceptorFactory\n", name)

	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.securityDomain=%s\n", name, realmName)

	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.host=${HOSTNAME}\n", name)
	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.port=%d\n", name, port)

	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.sslEnabled=true\n", name)

	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.needClientAuth=true\n", name)
	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.saslMechanisms=EXTERNAL\n", name)

	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.keyStoreType=PEMCFG\n", name)
	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.keyStorePath=%s\n", name, keyStorePath)
	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.trustStoreType=PEMCA\n", name)
	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.trustStorePath=%s\n", name, trustStorePath)

	// need a matching realm
	fmt.Fprintf(buf, "jaasConfigs.\"%s\".modules.cert.loginModuleClass=org.apache.activemq.artemis.spi.core.security.jaas.TextFileCertificateLoginModule\n", realmName)
	fmt.Fprintf(buf, "jaasConfigs.\"%s\".modules.cert.controlFlag=required\n", realmName)
	fmt.Fprintf(buf, "jaasConfigs.\"%s\".modules.cert.params.\"org.apache.activemq.jaas.textfiledn.role\"=%s\n", realmName, certRolesCfgKey)
	fmt.Fprintf(buf, "jaasConfigs.\"%s\".modules.cert.params.\"org.apache.activemq.jaas.textfiledn.user\"=%s\n", realmName, certUsersCfgKey)
	fmt.Fprintf(buf, "jaasConfigs.\"%s\".modules.cert.params.baseDir=%s%s\n", realmName, common.SecretPathBase, AppPropertiesSecretName(reconciler.instance.Name))

	serverConfigPropertiesSecret.Data[acceptorCfgKey] = buf.Bytes()

	return err
}

func jaasConfigRealmName(app *broker.BrokerApp) string {
	port := int32(DefaultStartPort)
	if app.Status.Service != nil && app.Status.Service.AssignedPort != UnassignedPort {
		port = app.Status.Service.AssignedPort
	}
	return fmt.Sprintf("port-%d", port)
}

func escapeForProperties(s string) string {
	s = strings.Replace(s, "::", "\\:\\:", 1)
	s = strings.Replace(s, "=", "\\=", -1)
	s = strings.Replace(s, " ", "\\ ", -1)
	return s
}

func producerRole(prefix string) string {
	return fmt.Sprintf("%s-producer", prefix)
}

func consumerRole(prefix string) string {
	return fmt.Sprintf("%s-consumer", prefix)
}

func metricsRole(prefix string) string {
	return fmt.Sprintf("%s-metrics", prefix)
}

func AppIdentity(app *broker.BrokerApp) string {
	return NameSpacedValue(app, app.Name)
}

func AppIdentityPrefixed(app *broker.BrokerApp, v string) string {
	return DashPrefixValue(AppIdentity(app), v)
}

func UnderscoreAppIdentityPrefixed(app *broker.BrokerApp, v string) string {
	return fmt.Sprintf("_%s", AppIdentityPrefixed(app, v))
}

func NameSpacedValue(app *broker.BrokerApp, v string) string {
	return DashPrefixValue(app.Namespace, v)
}

func DashPrefixValue(prefix, value string) string {
	return fmt.Sprintf("%s-%s", prefix, value)
}

func (reconciler *BrokerServiceInstanceReconciler) controlPlaneOverrideSecretName() string {
	return reconciler.instance.Name + "-control-plane-override"
}

// appQueueSet collects the queues for a specific app.
type appQueueSet struct {
	appName     string
	appIdentity string // namespace-prefixed, matches metricsRole in capabilities.properties
	queues      map[string]bool
}

func (reconciler *BrokerServiceInstanceReconciler) processControlPlaneOverrideSecret(validApps []broker.BrokerApp) error {
	allQueues := make(map[string]bool)
	perAppQueues := make([]appQueueSet, 0, len(validApps))

	for _, app := range validApps {
		aqs := appQueueSet{appName: app.Name, appIdentity: AppIdentity(&app), queues: make(map[string]bool)}
		for _, capability := range app.Spec.Capabilities {
			for _, addressRef := range capability.ConsumerOf {
				if !isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
					aqs.queues[addressRef.Address] = true
				} else {
					for _, queueName := range addressRef.Subscriptions {
						aqs.queues[addressRef.Address+FQQNSeparator+queueName] = true
					}
				}
			}
			for _, addressRef := range capability.ProducerOf {
				if !isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
					aqs.queues[addressRef.Address] = true
				} else {
					for _, queueName := range addressRef.Subscriptions {
						aqs.queues[addressRef.Address+FQQNSeparator+queueName] = true
					}
				}
			}
		}
		perAppQueues = append(perAppQueues, aqs)
		for q := range aqs.queues {
			allQueues[q] = true
		}
	}

	resourceName := types.NamespacedName{
		Namespace: reconciler.instance.Namespace,
		Name:      reconciler.controlPlaneOverrideSecretName(),
	}

	var desired *corev1.Secret
	obj := reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Secret{}), resourceName.Name)
	if obj != nil {
		desired = obj.(*corev1.Secret)
	} else {
		desired = secrets.NewSecret(resourceName, nil, nil)
	}

	if desired.Data == nil {
		desired.Data = make(map[string][]byte)
	}

	desired.Data[PrometheusConfigFileName] = reconciler.generatePrometheusConfig(allQueues)
	desired.Data[common.GetCertUsersKey(common.HttpAuthenticatorRealm)] = reconciler.generateCertUsersOverride(perAppQueues)
	desired.Data[common.GetCertRolesKey(common.HttpAuthenticatorRealm)] = reconciler.generateCertRolesOverride(perAppQueues)
	desired.Data["aa_rbac.properties"] = reconciler.generateRbacOverride(perAppQueues)

	reconciler.TrackDesired(desired)
	return nil
}

// generateCertUsersOverride builds the full cert_users.properties file for the
// control-plane override, mapping CNs to roles for all planes (operator, probe,
// prometheus, per-app metrics).
func (reconciler *BrokerServiceInstanceReconciler) generateCertUsersOverride(perAppQueues []appQueueSet) []byte {
	svcName := reconciler.instance.Name
	svcNs := reconciler.instance.Namespace

	buf := brokerproperties.NewPropsWithHeader()
	fmt.Fprintln(buf, "hawtio=/CN = hawtio-online\\.hawtio\\.svc.*/")

	if subject := resolveSubjectFromSecret(reconciler.Client, cot.OperatorCertName(svcName), svcNs); subject != nil {
		fmt.Fprintf(buf, "operator=/.*%s.*/\n", subject.CommonName)
	}
	if subject := resolveSubjectFromSecret(reconciler.Client, cot.BrokerCertName(svcName), svcNs); subject != nil {
		fmt.Fprintf(buf, "probe=/.*%s.*/\n", subject.CommonName)
	}
	if subject := resolveSubjectFromSecret(reconciler.Client, cot.PrometheusCertName(svcName), svcNs); subject != nil {
		fmt.Fprintf(buf, "prometheus=/.*%s.*/\n", subject.CommonName)
	}

	for _, aqs := range perAppQueues {
		roleName := appMetricsRole(aqs.appName)
		if subject := resolveSubjectFromSecret(reconciler.Client, cot.MetricsCertName(aqs.appName), svcNs); subject != nil {
			fmt.Fprintf(buf, "%s=/.*%s.*/\n", roleName, subject.CommonName)
		}
	}

	return buf.Bytes()
}

// generateCertRolesOverride builds cert_roles.properties mapping groups to
// their members. Format: group=member1,member2,...
//
// Per-app cert roles need membership in TWO groups:
//   - their own group (for queryMBeans from aa_rbac.properties)
//   - the capabilities group (namespace-prefixed, for per-queue RBAC
//     from capabilities.properties)
func (reconciler *BrokerServiceInstanceReconciler) generateCertRolesOverride(perAppQueues []appQueueSet) []byte {
	buf := brokerproperties.NewPropsWithHeader()
	fmt.Fprintln(buf, "status=operator,probe")
	fmt.Fprintln(buf, "metrics=operator,prometheus")
	fmt.Fprintln(buf, "hawtio=hawtio")

	for _, aqs := range perAppQueues {
		certRole := appMetricsRole(aqs.appName)
		capabilitiesGroup := metricsRole(aqs.appIdentity)
		fmt.Fprintf(buf, "%s=%s\n", certRole, certRole)
		fmt.Fprintf(buf, "%s=%s\n", capabilitiesGroup, certRole)
	}

	return buf.Bytes()
}

// generateRbacOverride builds aa_rbac.properties with global RBAC grants.
// Per-queue MBean access is handled by capabilities.properties (generated
// in processAppCapabilities), which grants per-app groups explicit VIEW
// on each queue MBean + its attributes. This file only needs to provide:
//   - status check for operator/probe
//   - broad metrics access for operator/prometheus
//   - queryMBeans gate for per-app cert roles
func (reconciler *BrokerServiceInstanceReconciler) generateRbacOverride(perAppQueues []appQueueSet) []byte {
	buf := brokerproperties.NewPropsWithHeader()

	// operator status
	fmt.Fprintln(buf, "securityRoles.\"mops.broker.getStatus\".status.view=true")

	// full metrics access for operator and prometheus (via "metrics" group)
	fmt.Fprintln(buf, "securityRoles.\"mops.mbeanserver.queryMBeans\".metrics.view=true")
	fmt.Fprintln(buf, "securityRoles.\"mops.broker\".metrics.view=true")
	fmt.Fprintln(buf, "securityRoles.\"mops.broker.getTotalMessageCount\".metrics.view=true")
	fmt.Fprintln(buf, "securityRoles.\"mops.broker.getTotalMessagesAcknowledged\".metrics.view=true")
	fmt.Fprintln(buf, "securityRoles.\"mops.broker.getTotalMessagesAdded\".metrics.view=true")

	for _, aqs := range perAppQueues {
		certRole := appMetricsRole(aqs.appName)
		capabilitiesGroup := metricsRole(aqs.appIdentity)

		// queryMBeans gate — required for the JMX exporter to call queryMBeans.
		// Grant to both the cert role's own group and the capabilities group.
		// Role names are quoted to prevent dots being parsed as path separators.
		fmt.Fprintf(buf, "securityRoles.\"mops.mbeanserver.queryMBeans\".\"%s\".view=true\n", certRole)
		fmt.Fprintf(buf, "securityRoles.\"mops.mbeanserver.queryMBeans\".\"%s\".view=true\n", capabilitiesGroup)
	}

	return buf.Bytes()
}

func appMetricsRole(appName string) string {
	return appName + "-metrics"
}

func resolveSubjectFromSecret(cl client.Client, secretName, namespace string) *pkix.Name {
	secret, err := common.GetNamespacedSecret(cl, secretName, namespace)
	if err != nil {
		return nil
	}
	subject, err := common.ExtractCertSubjectFromSecret(secret)
	if err != nil {
		return nil
	}
	return subject
}

// generatePrometheusConfig builds the JMX exporter YAML that configures the
// broker's metrics endpoint (port 8888).
//
// The server identity and trust anchor use the metrics plane PKI:
//   - keyStore  → prometheus cert (PKCS12, packed into the app-props secret)
//   - trustStore → metrics root CA (packed into the app-props secret)
//
// Per-app metrics isolation is enforced via cert_users/cert_roles/RBAC in the
// control-plane override — each app's metrics cert CN maps to a per-app role
// with scoped MBean access. The trustStore accepts any cert signed by the
// metrics CA (all per-app certs use the same CA).
func (reconciler *BrokerServiceInstanceReconciler) generatePrometheusConfig(appQueues map[string]bool) []byte {
	buf := brokerproperties.NewPropsWithHeader() // yaml

	propsPath := fmt.Sprintf("%s%s", common.SecretPathBase, reconciler.appPropertiesSecretName())
	keyStorePath := propsPath + "/_prometheus-cert.p12"
	trustStorePath := propsPath + "/_prometheus-ca-tls.crt"

	fmt.Fprintf(buf, "httpServer:\n")
	fmt.Fprintf(buf, "  authentication:\n")
	fmt.Fprintf(buf, "    plugin:\n")
	fmt.Fprintf(buf, "      class: org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator\n")
	fmt.Fprintf(buf, "      subjectAttributeName: org.jolokia.jaasSubject\n")
	fmt.Fprintf(buf, "  ssl:\n")
	fmt.Fprintf(buf, "    mutualTLS: true\n")
	fmt.Fprintf(buf, "    keyStore:\n")
	fmt.Fprintf(buf, "      filename: %s\n", keyStorePath)
	fmt.Fprintf(buf, "      type: PKCS12\n")
	fmt.Fprintf(buf, "      password: %s\n", prometheusP12Password)
	fmt.Fprintf(buf, "    trustStore:\n")
	fmt.Fprintf(buf, "      filename: %s\n", trustStorePath)
	fmt.Fprintf(buf, "      type: PEMCA\n")
	fmt.Fprintf(buf, "    certificate:\n")
	fmt.Fprintf(buf, "      alias: \"1\"\n")

	// Collector/scraper config

	fmt.Fprintf(buf, "attrNameSnakeCase: true\n")

	// just queues, rbac will limit values returned
	fmt.Fprintf(buf, "includeObjectNames:\n")
	fmt.Fprintf(buf, "  - \"org.apache.activemq.artemis:broker=*,component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*\"\n")

	brokerName := reconciler.instance.Name // Use service name as broker name for restricted mode

	// Add queue-level attributes for specific queues with exact ObjectNames (include quotes) for canonical string match, this restricts the attribute load.
	// Keys are sorted to ensure deterministic output — without this, map iteration
	// order randomness causes the comparator to detect "changes" on every cycle,
	// thrashing the override secret and triggering continuous broker config reloads.
	if len(appQueues) > 0 {
		sortedQueues := make([]string, 0, len(appQueues))
		for address := range appQueues {
			sortedQueues = append(sortedQueues, address)
		}
		sort.Strings(sortedQueues)

		fmt.Fprintf(buf, "includeObjectNameAttributes:\n")
		for _, address := range sortedQueues {
			fqqn := strings.SplitN(address, "::", 2)
			if len(fqqn) > 1 {
				fmt.Fprintf(buf, "  org.apache.activemq.artemis:broker=\"%s\",component=addresses,address=\"%s\",subcomponent=queues,routing-type=\"multicast\",queue=\"%s\":\n",
					brokerName, fqqn[0], fqqn[1])
			} else {
				fmt.Fprintf(buf, "  org.apache.activemq.artemis:broker=\"%s\",component=addresses,address=\"%s\",subcomponent=queues,routing-type=\"anycast\",queue=\"%s\":\n",
					brokerName, address, address)
			}
			fmt.Fprintf(buf, "    - MessageCount\n")
			fmt.Fprintf(buf, "    - ConsumerCount\n")
			fmt.Fprintf(buf, "    - DeliveringCount\n")
			fmt.Fprintf(buf, "    - PersistentSize\n")
		}
	}

	// regex for matchName='org.apache.activemq.artemis<broker="brokerservice617a", component=addresses, address="METRICS.QUEUE.TWO", subcomponent=queues, routing-type="anycast", queue="METRICS.QUEUE.TWO"><>MessageCount: 0'
	// Rules for queue metrics generation
	fmt.Fprintf(buf, "rules:\n")
	fmt.Fprintf(buf, `  - pattern: "org.apache.activemq.artemis<broker=\"([^\"]+)\", component=addresses, address=\"([^\"]+)\", subcomponent=queues, routing-type=\"([^\"]+)\", queue=\"([^\"]+)\"><>([^:]+):"`+"\n")
	fmt.Fprintf(buf, "    name: broker_queue_$5\n")
	fmt.Fprintf(buf, "    help: $5\n") // non descriptive help - default contains too much unrelated info (TODO: potentially clean up and extract the help info, could have a rule per attribute)
	fmt.Fprintf(buf, "    attrNameSnakeCase: true\n")
	fmt.Fprintf(buf, "    type: GAUGE\n")
	fmt.Fprintf(buf, "    labels:\n")
	fmt.Fprintf(buf, "      broker: \"$1\"\n")
	fmt.Fprintf(buf, "      address: \"$2\"\n")
	fmt.Fprintf(buf, "      routing_type: \"$3\"\n")
	fmt.Fprintf(buf, "      queue: \"$4\"\n")

	return buf.Bytes()
}
