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
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"sort"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/monitoring"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"strings"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/appselector"
	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	servicemetrics "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/metrics"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/networkpolicies"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/templates"
	mgmt "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/artemis"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	jolokia_client "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type BrokerServiceReconciler struct {
	*ReconcilerLoop
}

type BrokerServiceInstanceReconciler struct {
	*BrokerServiceReconciler
	instance              *broker.BrokerService
	status                *broker.BrokerServiceStatus
	pendingEmptyAddresses map[string]map[string]bool
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
//+kubebuilder:rbac:groups=monitoring.coreos.com,namespace=arkmq-org-broker-operator,resources=servicemonitors,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerservices/finalizers,verbs=update
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps/finalizers,verbs=update

func (reconciler *BrokerServiceReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := reconciler.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "BrokerService")

	instance := &broker.BrokerService{}
	var err = reconciler.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			servicemetrics.DeleteServiceMetrics(request.Name, request.Namespace)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if handled, err := reconciler.handlePendingEmptyFinalizer(ctx, instance); handled {
		return ctrl.Result{}, err
	}

	localLoop := &ReconcilerLoop{
		KubeBits:           reconciler.KubeBits,
		ReconcilerLoopType: reconciler,
	}

	processor := BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{ReconcilerLoop: localLoop},
		instance:                instance,
		status:                  instance.Status.DeepCopy(),
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

	statusErr, retry := processor.processStatus(err)
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
	if retry {
		return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
	}

	// Success
	return ctrl.Result{}, nil
}

func (reconciler *BrokerServiceReconciler) handlePendingEmptyFinalizer(ctx context.Context, instance *broker.BrokerService) (bool, error) {
	if instance.DeletionTimestamp == nil || !controllerutil.ContainsFinalizer(instance, broker.AddressesPendingEmptyFinalizer) {
		return false, nil
	}

	apps := &broker.BrokerAppList{}
	svcKey := instance.Namespace + ":" + instance.Name
	if listErr := reconciler.List(ctx, apps, client.MatchingFields{common.AppServiceBindingField: svcKey}); listErr != nil {
		return true, listErr
	}
	for i := range apps.Items {
		if controllerutil.ContainsFinalizer(&apps.Items[i], broker.AddressesPendingEmptyFinalizer) {
			return false, nil
		}
	}

	controllerutil.RemoveFinalizer(instance, broker.AddressesPendingEmptyFinalizer)
	if updateErr := reconciler.Update(ctx, instance); updateErr != nil {
		return true, updateErr
	}
	servicemetrics.DeleteServiceMetrics(instance.Name, instance.Namespace)
	return true, nil
}

func (reconciler *BrokerServiceInstanceReconciler) ensureServicePendingEmptyFinalizer(apps *broker.BrokerAppList) error {
	hasContentApps := false
	for i := range apps.Items {
		if hasOwnedContent(&apps.Items[i]) {
			hasContentApps = true
			break
		}
	}
	if hasContentApps && controllerutil.AddFinalizer(reconciler.instance, broker.AddressesPendingEmptyFinalizer) {
		if updateErr := reconciler.Update(context.TODO(), reconciler.instance); updateErr != nil {
			return fmt.Errorf("failed to add pending-empty finalizer to service %s: %v", reconciler.instance.Name, updateErr)
		}
	} else if !hasContentApps && reconciler.instance.DeletionTimestamp == nil &&
		controllerutil.RemoveFinalizer(reconciler.instance, broker.AddressesPendingEmptyFinalizer) {
		if updateErr := reconciler.Update(context.TODO(), reconciler.instance); updateErr != nil {
			return fmt.Errorf("failed to remove pending-empty finalizer from service %s: %v", reconciler.instance.Name, updateErr)
		}
	}
	return nil
}

// instance specifics for a reconciler loop
func (r *BrokerServiceReconciler) getOwned() []client.ObjectList {
	owned := []client.ObjectList{
		&corev1.SecretList{},
		&broker.BrokerList{},
		&corev1.ServiceList{}}

	// only when the kind is served: read.ListAll errors outright on one that is
	// not, which would abort the reconcile
	if monitoring.IsPrometheusAvailable(r.RESTMapper()) {
		owned = append(owned, &monitoringv1.ServiceMonitorList{})
	}

	return owned
}

func (r *BrokerServiceReconciler) getOrderedTypeList() []reflect.Type {
	// we want to create/update in this order
	return []reflect.Type{
		reflect.TypeOf(corev1.Secret{}),
		reflect.TypeOf(broker.Broker{}),
		reflect.TypeOf(corev1.Service{}),
		reflect.TypeOf(monitoringv1.ServiceMonitor{})}
}

func (reconciler *BrokerServiceInstanceReconciler) validateSpec() error {
	// Validate resource name
	if err := ValidateResourceName(reconciler.instance.Name); err != nil {
		return err
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

// processMonitoring generates what a Prometheus needs to scrape this service's
// brokers under the prometheus identity, which the broker grants the broad
// "metrics" role and which therefore sees every app's queues. Each app queue's
// series carry the owning app's namespace, so this one scrape serves every
// tenant, and broker-wide series stay with the service.
//
// Silently generates nothing when the ServiceMonitor kind is not served; the
// BrokerService is perfectly functional without it.
func (reconciler *BrokerServiceInstanceReconciler) processMonitoring() {
	instance := reconciler.instance

	if !monitoring.IsPrometheusAvailable(reconciler.RESTMapper()) {
		reconciler.log.V(1).Info("prometheus-operator is not available, skipping ServiceMonitor generation",
			"service", instance.Name)
		return
	}

	// Without a prometheus certificate there is no identity to scrape as, and the
	// broker would reject the connection. Not an error: the same absence is
	// tolerated when resolving the control plane CNs.
	promCertSecret := common.GetPrometheusCertSecretNameFor(instance.Name, instance.Namespace, reconciler.Client)
	if _, err := common.GetNamespacedSecret(reconciler.Client, promCertSecret, instance.Namespace); err != nil {
		reconciler.log.V(1).Info("no prometheus cert, generating no ServiceMonitor",
			"service", instance.Name, "secret", promCertSecret)
		return
	}

	caRef := monitoring.CARef(reconciler.Client)
	if caRef == nil {
		reconciler.log.V(1).Info("no ca bundle, generating no ServiceMonitor", "service", instance.Name)
		return
	}

	var existing *monitoringv1.ServiceMonitor
	if obj := reconciler.CloneOfDeployed(reflect.TypeOf(monitoringv1.ServiceMonitor{}), monitoring.WiringName(instance.Name)); obj != nil {
		existing = obj.(*monitoringv1.ServiceMonitor)
	}

	reconciler.TrackDesired(monitoring.BuildServiceMonitor(monitoring.ScrapeTarget{
		Owner:           instance,
		ServiceSelector: map[string]string{selectors.LabelBrokerService: instance.Name},
		// The operand certificate is issued for the service name and for
		// *.<svc>-hdls-svc.<ns>.svc.<domain>. The scrape reaches a pod by IP, and
		// only the wildcard covers a name that is the pod's, so verify against one
		// of those: every pod presents the same certificate.
		ServerName:       common.OrdinalFQDNS(instance.Name, instance.Namespace, 0),
		ClientCertSecret: promCertSecret,
		CA:               caRef,
		Labels:           monitoring.Labels("broker-service", instance.Name, instance.Name),
	}, existing))
}

func (reconciler *BrokerServiceInstanceReconciler) processSpec() (err error) {
	if err = reconciler.processBroker(); err != nil {
		return err
	}

	// Process service
	if err := reconciler.processService(); err != nil {
		return err
	}

	reconciler.processMonitoring()

	return nil
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
		// App-specific recommended labels (name/instance/part-of are set by the Broker labeler).
		selectors.LabelAppKubernetesComponent: "broker-service",
		selectors.LabelAppKubernetesManagedBy: common.OperatorName,
		// Domain-specific labels
		selectors.LabelBrokerService:   reconciler.instance.Name,
		selectors.LabelBrokerPeerIndex: "0",
	}
	desired.Spec.Env = reconciler.instance.Spec.Env
	desired.Spec.Resources = reconciler.instance.Spec.Resources

	if reconciler.instance.Spec.Image != nil {
		desired.Spec.Image = *reconciler.instance.Spec.Image
	}

	desired.Spec.ExtraMounts.Secrets = []string{
		reconciler.appPropertiesSecretName(),
	}

	appPorts, err := reconciler.processAppSecrets()
	if err != nil {
		return err
	}

	if common.BrokerNetworkPolicyEnabled() && len(appPorts) > 0 {
		desired.Spec.ResourceTemplates = appendNetworkPolicyTemplate(desired.Spec.ResourceTemplates, appPorts)
	}

	reconciler.TrackDesired(desired)

	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) processAppSecrets() (appPorts []int32, err error) {
	// avoid restart for app onboarding with existing mount points
	// TODO potentially N app-secrets to overcome 1Mb size limit
	resourceName := types.NamespacedName{
		Namespace: reconciler.instance.Namespace,
		Name:      reconciler.appPropertiesSecretName(),
	}

	var desired *corev1.Secret

	deployedSecret := reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Secret{}), resourceName.Name)
	if deployedSecret != nil {
		desired = deployedSecret.(*corev1.Secret)
	} else {
		desired = secrets.NewSecret(resourceName, nil, nil)
	}

	// find all apps that select this service
	boundApps := &broker.BrokerAppList{}
	bindingKey := reconciler.instance.Namespace + ":" + reconciler.instance.Name
	if err = reconciler.List(context.TODO(), boundApps, client.MatchingFields{common.AppServiceBindingField: bindingKey}); err != nil {
		return nil, err
	}

	if err = reconciler.ensureServicePendingEmptyFinalizer(boundApps); err != nil {
		return nil, err
	}

	// snapshot old data before reset — used to detect removed addresses pending empty
	previousSecretData := desired.Data
	desired.Data = make(map[string][]byte)
	appIdentities := make([]string, 0, len(boundApps.Items))
	provisionedApps := make([]broker.BrokerApp, 0, len(boundApps.Items))
	reconciler.status.PendingEmpty = nil

	type appWithTracker struct {
		app     broker.BrokerApp
		tracker *AddressTracker
	}
	activeAppTrackers := make([]appWithTracker, 0, len(boundApps.Items))

	for _, app := range boundApps.Items {
		if err = common.ValidateResourceName(app.Name); err != nil {
			reconciler.log.Error(err, "invalid app name", "app", app.Name)
			break
		}
		var tracker *AddressTracker
		tracker, err = reconciler.buildAddressTracker(&app)
		if err != nil {
			reconciler.log.Error(err, "failed to build address tracker for app", "app", app.Name)
			break
		}
		var pendingEmptyComplete bool
		pendingEmptyComplete, err = reconciler.processPendingEmpty(&app, tracker, previousSecretData)
		if err != nil {
			reconciler.log.Error(err, "failed to process pending empty for app", "app", app.Name)
			break
		}
		if pendingEmptyComplete {
			continue
		}
		activeAppTrackers = append(activeAppTrackers, appWithTracker{app: app, tracker: tracker})
	}

	// all active apps must be known before filtering cross-app roles
	activeApps := make(map[string]bool, len(activeAppTrackers))
	for i := range activeAppTrackers {
		activeApps[AppIdentity(&activeAppTrackers[i].app)] = true
	}

	if err == nil {
		for i := range activeAppTrackers {
			app := &activeAppTrackers[i].app
			tracker := activeAppTrackers[i].tracker

			reconciler.filterCrossAppRoles(app, tracker, activeApps)

			if err = reconciler.processCapabilities(desired, app, tracker); err != nil {
				reconciler.log.Error(err, "failed to process capabilities for app", "app", app.Name)
				break
			}
			if err = reconciler.processAcceptor(desired, app); err != nil {
				reconciler.log.Error(err, "failed to process acceptor for app", "app", app.Name)
				break
			}
			appIdentities = append(appIdentities, AppIdentityWithGeneration(app))
			provisionedApps = append(provisionedApps, activeAppTrackers[i].app)
			if app.Status.Service != nil && app.Status.Service.AssignedPort != UnassignedPort {
				appPorts = append(appPorts, app.Status.Service.AssignedPort)
			}
		}
	}

	sort.Strings(appIdentities)
	if desired.Annotations == nil {
		desired.Annotations = make(map[string]string)
	}
	desired.Annotations[common.ProvisionedAppsAnnotation] = strings.Join(appIdentities, ",")

	reconciler.TrackDesired(desired)

	// Update prometheus config in control-plane-override secret with queue-level metrics
	if err == nil {
		err = reconciler.processControlPlaneOverrideSecret(provisionedApps)
	}

	return appPorts, err
}

func (reconciler *BrokerServiceInstanceReconciler) appPropertiesSecretName() string {
	return AppPropertiesSecretName(reconciler.instance.Name)
}

func appendNetworkPolicyTemplate(existing []broker.ResourceTemplate, appPorts []int32) []broker.ResourceTemplate {
	netpolKind := "NetworkPolicy"

	ports := []interface{}{
		map[string]interface{}{"port": int64(networkpolicies.RestrictedJolokiaPort), "protocol": "TCP"},
		map[string]interface{}{"port": int64(networkpolicies.RestrictedPrometheusPort), "protocol": "TCP"},
	}
	for _, p := range appPorts {
		ports = append(ports, map[string]interface{}{"port": int64(p), "protocol": "TCP"})
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"ingress": []interface{}{
				map[string]interface{}{"ports": ports},
			},
		},
	}
	patchBytes, _ := json.Marshal(patch)

	// replace any existing NP template rather than appending duplicates
	filtered := make([]broker.ResourceTemplate, 0, len(existing))
	for _, rt := range existing {
		if rt.Selector != nil && rt.Selector.Kind != nil && *rt.Selector.Kind == netpolKind {
			continue
		}
		filtered = append(filtered, rt)
	}

	return append(filtered, broker.ResourceTemplate{
		Selector: &broker.ResourceSelector{Kind: &netpolKind},
		Patch:    runtime.RawExtension{Raw: patchBytes},
	})
}

func AppPropertiesSecretName(name string) string {
	return fmt.Sprintf("%s-app%s", name, common.BrokerPropsSuffix)
}

func PropertiesSecretName(name string) string {
	return fmt.Sprintf("%s%s", name, common.BrokerPropsSuffix)
}

func certSecretName(cr *broker.BrokerService) string {
	return fmt.Sprintf("%s-%s", cr.Name, common.DefaultOperandCertSecretName)
}

func (reconciler *BrokerServiceInstanceReconciler) setValidCondition(err error) {
	condition := metav1.Condition{
		Type:               broker.ValidConditionType,
		Status:             metav1.ConditionTrue,
		Reason:             broker.ValidConditionSuccessReason,
		ObservedGeneration: reconciler.instance.Generation,
	}

	if validErr, ok := err.(*ValidationError); ok {
		condition.Status = metav1.ConditionFalse
		condition.Reason = validErr.ConditionReason()
		condition.Message = validErr.Error()
	}

	meta.SetStatusCondition(&reconciler.status.Conditions, condition)
}

func (reconciler *BrokerServiceInstanceReconciler) setReadyCondition(generation int64) {
	condition := metav1.Condition{
		Type:               broker.ReadyConditionType,
		Reason:             broker.ReadyConditionReason,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
	}

	ready := meta.IsStatusConditionTrue(reconciler.status.Conditions, broker.DeployedConditionType)
	for _, c := range reconciler.status.Conditions {
		if c.Type != broker.ReadyConditionType && c.Status == metav1.ConditionFalse {
			ready = false
		}
	}
	if !ready {
		condition.Status = metav1.ConditionFalse
		condition.Reason = broker.NotReadyConditionReason
		condition.Message = common.NotReadyConditionMessage
	}

	meta.SetStatusCondition(&reconciler.status.Conditions, condition)
}

func (reconciler *BrokerServiceInstanceReconciler) processStatus(reconcilerError error) (err error, retry bool) {
	// Set Valid condition (always updated)
	reconciler.setValidCondition(reconcilerError)

	generation := reconciler.instance.Generation

	var deployedCondition metav1.Condition = metav1.Condition{
		Type:               broker.DeployedConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             broker.DeployedConditionNotReadyReason,
		ObservedGeneration: generation,
	}

	var appsProvisionedCondition metav1.Condition = metav1.Condition{
		Type:               broker.AppsProvisionedConditionType,
		Status:             metav1.ConditionFalse,
		Reason:             broker.AppsProvisionedConditionWaitingReason,
		ObservedGeneration: generation,
	}

	if reconcilerError != nil {
		// Check if error is a TransientError with a specific reason
		if transErr, ok := reconcilerError.(*TransientError); ok {
			deployedCondition.Status = metav1.ConditionFalse
			deployedCondition.Reason = transErr.ConditionReason()
			deployedCondition.Message = transErr.Error()
		} else if _, ok := reconcilerError.(*ValidationError); ok {
			existing := meta.FindStatusCondition(reconciler.status.Conditions, broker.DeployedConditionType)
			if existing != nil {
				deployedCondition = *existing
			} else {
				deployedCondition.Status = metav1.ConditionFalse
				deployedCondition.Reason = broker.DeployedConditionValidationFailedReason
				deployedCondition.Message = "Cannot deploy due to validation failure"
				deployedCondition.ObservedGeneration = 0
			}
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
					deployedCondition.Message = fmt.Sprintf("awaiting deployed condition, broker status %v", deployed.Status)
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
					if getErr := reconciler.Get(context.TODO(), secretKey, secret); getErr == nil {
						if secret.ResourceVersion == appliedSecretVersion {
							appsProvisionedCondition.Status = metav1.ConditionTrue
							appsProvisionedCondition.Reason = broker.AppsProvisionedConditionSyncedReason
							if applied, ok := secret.Annotations[common.ProvisionedAppsAnnotation]; ok && applied != "" {
								reconciler.status.ProvisionedApps = strings.Split(applied, ",")
							} else {
								reconciler.status.ProvisionedApps = nil
							}
						} else {
							appsProvisionedCondition.Message = fmt.Sprintf("externalConfig version mismatch, expected: %s, current: %s, broker status: %v", secret.ResourceVersion, appliedSecretVersion, deployed.Status)
						}
					}
				} else {
					appsProvisionedCondition.Message = fmt.Sprintf("pending externalConfig, broker status: %v", deployed.Status)
				}
			} else {
				appsProvisionedCondition.Message = fmt.Sprintf("not ready, broker status: %v", deployed.Status)
			}
		} else {
			appsProvisionedCondition.Message = "broker not found"
		}
	}
	meta.SetStatusCondition(&reconciler.status.Conditions, deployedCondition)
	meta.SetStatusCondition(&reconciler.status.Conditions, appsProvisionedCondition)

	reconciler.setReadyCondition(generation)

	if reconciler.instance.DeletionTimestamp != nil && controllerutil.ContainsFinalizer(reconciler.instance, broker.AddressesPendingEmptyFinalizer) {
		message := "Deletion pending, addresses pending empty"
		if len(reconciler.status.PendingEmpty) > 0 {
			pendingApps := make([]string, 0, len(reconciler.status.PendingEmpty))
			for _, ds := range reconciler.status.PendingEmpty {
				pendingApps = append(pendingApps, ds.App)
			}
			message = fmt.Sprintf("Deletion pending, addresses pending empty for apps: %s", strings.Join(pendingApps, ", "))
		}
		meta.SetStatusCondition(&reconciler.status.Conditions, metav1.Condition{
			Type:               broker.ReadyConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             broker.ReadyConditionAddressesPendingEmpty,
			Message:            message,
			ObservedGeneration: generation,
		})
	}

	reconciler.status.ObservedGeneration = generation

	if !reflect.DeepEqual(reconciler.instance.Status, *reconciler.status) {
		reconciler.instance.Status = *reconciler.status
		err = resources.UpdateStatus(reconciler.Client, reconciler.instance)
	}

	servicemetrics.UpdateServiceMetrics(
		reconciler.instance.Name,
		reconciler.instance.Namespace,
		len(reconciler.status.ProvisionedApps),
	)

	if len(reconciler.status.PendingEmpty) > 0 {
		retry = true
	}

	return err, retry
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

func isAppUnbinding(app *broker.BrokerApp) bool {
	deployedCond := meta.FindStatusCondition(app.Status.Conditions, broker.DeployedConditionType)
	return deployedCond != nil && deployedCond.Reason == broker.DeployedConditionUnbindingReason
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
		selectors.LabelBrokerService: reconciler.instance.Name,
	}
	// for the generated ServiceMonitor to find this Service and its metrics port
	desired.Labels = serviceLabels(reconciler.instance)
	desired.Spec.Ports = []corev1.ServicePort{{
		Name:       monitoring.PortName,
		Port:       monitoring.Port,
		TargetPort: intstr.FromInt32(monitoring.Port),
	}}
	reconciler.TrackDesired(desired)
	return nil
}

// serviceLabels carries the recommended labels its ServiceMonitor does, but not
// app.kubernetes.io/name, which marks what the Broker of the same name manages.
// The ServiceMonitor selects on broker.arkmq.org/service alone: the Broker's own
// Services share the instance label.
func serviceLabels(instance *broker.BrokerService) map[string]string {
	return map[string]string{
		selectors.LabelAppKubernetesInstance:  instance.Name,
		selectors.LabelAppKubernetesComponent: "broker-service",
		selectors.LabelAppKubernetesManagedBy: common.OperatorName,
		selectors.LabelPartOfKey:              selectors.LabelPartOfValue,
		selectors.LabelBrokerService:          instance.Name,
	}
}

// appToServiceHandler handles BrokerApp events and enqueues the affected BrokerService(s).
// On Update, it enqueues both the old and new service if the binding changed.
type appToServiceHandler struct{}

func (h *appToServiceHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if req := h.getServiceRequest(evt.Object); req != nil {
		q.Add(*req)
	}
}

func (h *appToServiceHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
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

func (h *appToServiceHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if req := h.getServiceRequest(evt.Object); req != nil {
		q.Add(*req)
	}
}

func (h *appToServiceHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
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

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&broker.BrokerService{}).
		Owns(&broker.Broker{}).
		Watches(&broker.BrokerApp{}, &appToServiceHandler{})

	// see the note in the BrokerApp reconciler: Owns on a kind the API server does
	// not serve takes the manager down at start
	if monitoring.IsPrometheusAvailable(mgr.GetRESTMapper()) {
		builder = builder.Owns(&monitoringv1.ServiceMonitor{})
	}

	return builder.Complete(r)
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

// buildAddressTracker creates an AddressTracker populated from the app's current
// spec. For deleting or departing apps the tracker is left empty so that
// detectDrainingAddresses treats all previously-deployed addresses as removed.
func (reconciler *BrokerServiceInstanceReconciler) buildAddressTracker(app *broker.BrokerApp) (*AddressTracker, error) {
	addressTracker := newAddressTracker()

	role := AppIdentity(app)

	if app.DeletionTimestamp != nil || isAppUnbinding(app) {
		return addressTracker, nil
	}

	for _, addrType := range app.Spec.Addresses {
		addressTracker.trackAddressType(&addrType)
	}

	for _, addrType := range app.Spec.SharedAddresses {
		addressTracker.trackAddressType(&addrType)
	}

	for _, capability := range app.Spec.Capabilities {

		var entry *AddressConfig

		for _, addressRef := range capability.ProducerOf {
			entry = addressTracker.track(&addressRef)
			entry.senderRoles[role] = role

			if isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
				entry.isMulticast = true
			}
		}

		for _, addressRef := range capability.ConsumerOf {
			entry = addressTracker.track(&addressRef)

			if !isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
				entry.consumerRoles[role] = role

				if entry.isOwned && entry.isMulticast {
					return nil, fmt.Errorf(
						"address '%s' is referenced with both pubSub and non pubSub semantics. "+
							"This creates a conflict. Use consistent semantics for the same address",
						addressRef.Address)
				}
			} else {
				if entry.isOwned && len(entry.consumerRoles) > 0 && !entry.isMulticast {
					return nil, fmt.Errorf(
						"address '%s' is referenced with both pubSub and non pubSub semantics. "+
							"This creates a conflict. Use consistent semantics for the same address",
						addressRef.Address)
				}

				entry.isMulticast = true

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

	return addressTracker, nil
}

// processPendingEmpty manages the pending-empty finalizer lifecycle for a single app.
// It mutates the tracker to include any addresses that still have pending messages,
// and records them on the reconciler so filterCrossAppRoles can revoke cross-app
// send permissions.
// Returns complete=true when the app has no remaining pending messages and the
// caller should not generate config for it.
func (reconciler *BrokerServiceInstanceReconciler) processPendingEmpty(app *broker.BrokerApp, tracker *AddressTracker, oldSecretData map[string][]byte) (complete bool, err error) {
	role := AppIdentity(app)

	if app.DeletionTimestamp == nil && hasOwnedContent(app) && controllerutil.AddFinalizer(app, broker.AddressesPendingEmptyFinalizer) {
		if updateErr := reconciler.Update(context.TODO(), app); updateErr != nil {
			return false, fmt.Errorf("failed to add pending-empty finalizer to app %s: %v", app.Name, updateErr)
		}
	}

	if pendingAddresses := reconciler.detectDrainingAddresses(app, tracker, oldSecretData); len(pendingAddresses) > 0 {
		for _, da := range pendingAddresses {
			addrType := broker.AddressType{
				Address:       da.Address,
				Subscriptions: da.Subscriptions,
			}
			tracker.trackAddressType(&addrType)
			if len(da.Subscriptions) == 0 && da.HadConsumeRole {
				entry := tracker.names[da.Address]
				entry.consumerRoles[role] = role
			}
			for _, subName := range da.Subscriptions {
				fqqn := da.Address + FQQNSeparator + subName
				if qe, exists := tracker.names[fqqn]; exists {
					qe.subscriberRoles[role] = role
				}
			}
		}
		if reconciler.pendingEmptyAddresses == nil {
			reconciler.pendingEmptyAddresses = make(map[string]map[string]bool)
		}
		if reconciler.pendingEmptyAddresses[role] == nil {
			reconciler.pendingEmptyAddresses[role] = make(map[string]bool)
		}
		for _, da := range pendingAddresses {
			reconciler.pendingEmptyAddresses[role][da.Address] = true
		}
		appStatus := broker.PendingEmptyAddressStatus{
			App:      role,
			Messages: make(map[string]int64),
		}
		for _, da := range pendingAddresses {
			for fqqn, count := range da.MessageCounts {
				appStatus.Messages[fqqn] = count
			}
		}
		reconciler.status.PendingEmpty = append(reconciler.status.PendingEmpty, appStatus)
		return false, nil
	}

	// deletion and all empty — remove finalizer so deletion is not blocked
	if app.DeletionTimestamp != nil {
		if controllerutil.RemoveFinalizer(app, broker.AddressesPendingEmptyFinalizer) {
			if updateErr := reconciler.Update(context.TODO(), app); updateErr != nil {
				return false, fmt.Errorf("failed to remove pending-empty finalizer from app %s: %v", app.Name, updateErr)
			}
		}
		return true, nil
	}

	// live unbinding app, addresses empty — remove finalizer so binding can be cleared
	if isAppUnbinding(app) && controllerutil.RemoveFinalizer(app, broker.AddressesPendingEmptyFinalizer) {
		if updateErr := reconciler.Update(context.TODO(), app); updateErr != nil {
			return false, fmt.Errorf("failed to remove pending-empty finalizer from app %s: %v", app.Name, updateErr)
		}
	}

	// live app no longer has owned content — clean up stale finalizer
	if !hasOwnedContent(app) && controllerutil.RemoveFinalizer(app, broker.AddressesPendingEmptyFinalizer) {
		if updateErr := reconciler.Update(context.TODO(), app); updateErr != nil {
			return false, fmt.Errorf("failed to remove pending-empty finalizer from app %s: %v", app.Name, updateErr)
		}
	}

	return false, nil
}

func (reconciler *BrokerServiceInstanceReconciler) filterCrossAppRoles(app *broker.BrokerApp, tracker *AddressTracker, activeApps map[string]bool) {
	role := AppIdentity(app)

	resolveOwner := func(addressRef *broker.AddressRef) string {
		ownerNs := addressRef.AppNamespace
		if ownerNs == "" {
			ownerNs = app.Namespace
		}
		return SlashPrefixValue(ownerNs, addressRef.AppName)
	}

	for _, capability := range app.Spec.Capabilities {
		for i := range capability.ProducerOf {
			addressRef := &capability.ProducerOf[i]
			if addressRef.AppName == "" {
				continue
			}
			entry, exists := tracker.names[addressRef.Address]
			if !exists || entry.isOwned {
				continue
			}
			ownerIdentity := resolveOwner(addressRef)
			if !activeApps[ownerIdentity] {
				delete(entry.senderRoles, role)
			} else if addrs, ok := reconciler.pendingEmptyAddresses[ownerIdentity]; ok && addrs[addressRef.Address] {
				delete(entry.senderRoles, role)
			}
		}

		// Consumers keep their roles when the owner is pending empty — they
		// need to stay active to drain the remaining messages. Producers are
		// already revoked above so no new messages will arrive.
		for i := range capability.ConsumerOf {
			addressRef := &capability.ConsumerOf[i]
			if addressRef.AppName == "" {
				continue
			}
			entry, exists := tracker.names[addressRef.Address]
			if !exists || entry.isOwned {
				continue
			}
			ownerIdentity := resolveOwner(addressRef)
			if !activeApps[ownerIdentity] {
				delete(entry.consumerRoles, role)
				for _, subName := range addressRef.Subscriptions {
					fqqn := addressRef.Address + FQQNSeparator + subName
					if fqqnEntry, fqqnExists := tracker.names[fqqn]; fqqnExists {
						delete(fqqnEntry.subscriberRoles, role)
					}
				}
			}
		}
	}

	for name, entry := range tracker.names {
		if !entry.isOwned && len(entry.senderRoles) == 0 && len(entry.consumerRoles) == 0 && len(entry.subscriberRoles) == 0 {
			delete(tracker.names, name)
		}
	}
}

func (reconciler *BrokerServiceInstanceReconciler) processCapabilities(secret *corev1.Secret, app *broker.BrokerApp, addressTracker *AddressTracker) (err error) {
	cfg := &brokerproperties.CapabilitiesJSON{
		AddressConfigurations: map[string]*brokerproperties.AddressConfiguration{},
		SecurityRoles:         map[string]map[string]*brokerproperties.RolePermissions{},
	}

	metricsRoles := []string{"metrics", metricsRole(AppIdentity(app))}
	grantQueueMetrics := func(queueName string) {
		for _, rbacRole := range metricsRoles {
			cfg.EnsureSecurityRole("mops.queue."+queueName, rbacRole).View = true
			cfg.EnsureSecurityRole("mops.queue."+queueName+".getMessageCount", rbacRole).View = true
			cfg.EnsureSecurityRole("mops.queue."+queueName+".getConsumerCount", rbacRole).View = true
			cfg.EnsureSecurityRole("mops.queue."+queueName+".getDeliveringCount", rbacRole).View = true
			cfg.EnsureSecurityRole("mops.queue."+queueName+".getPersistentSize", rbacRole).View = true
		}
	}

	hasQueues := false
	for addressName, addr := range addressTracker.names {
		var addressKey, queueKey string
		fqqn := strings.SplitN(addressName, FQQNSeparator, 2)
		isFQQN := len(fqqn) > 1
		if isFQQN {
			addressKey = fqqn[0]
			queueKey = fqqn[1]
		} else {
			addressKey = addressName
			queueKey = addressName
		}

		addrCfg := cfg.EnsureAddressConfig(addressKey)

		if addr.isOwned {
			if addr.isMulticast {
				addrCfg.RoutingTypes = brokerproperties.RoutingTypeMulticast
			} else {
				addrCfg.RoutingTypes = brokerproperties.RoutingTypeAnycast
			}
		}

		if !addr.isMulticast {
			qc := addrCfg.EnsureQueueConfig(queueKey)
			qc.RoutingType = brokerproperties.RoutingTypeAnycast
			qc.Address = addressKey
			grantQueueMetrics(queueKey)
			hasQueues = true
		}

		if isFQQN {
			qc := addrCfg.EnsureQueueConfig(queueKey)
			qc.RoutingType = brokerproperties.RoutingTypeMulticast
			qc.Address = addressKey
			grantQueueMetrics(queueKey)
			hasQueues = true
		}

		for _, role := range addr.senderRoles {
			cfg.EnsureSecurityRole(addressName, producerRole(role)).Send = true
		}
		for _, role := range addr.consumerRoles {
			cfg.EnsureSecurityRole(addressName, consumerRole(role)).Consume = true
		}

		// 2026-05-12T17:17:22.906184695Z Thread-0 (activemq-brokerservice-mqtt119a) INFO AMQ601264: User test-mqtt-app(test-mqtt-app-consumer,test-mqtt-app-producer)@10.244.12.28:45426 gets security check failure, reason = AMQ229213:
		// User: test-mqtt-app does not have permission='CONSUME' for queue my-client.mytopic.* on address mytopic.*
		// securityRoles."(mytopic.*\:\:my-client.mytopic.*)"."test-mqtt-app-consumer".consume=true

		for _, role := range addr.subscriberRoles {
			// but security store does not support literal match markers!
			// https://issues.apache.org/jira/browse/ARTEMIS-6057
			cfg.EnsureSecurityRole(addressName, consumerRole(role)).Consume = true
		}
	}

	if hasQueues {
		cfg.EnsureSecurityRole("mops.mbeanserver.queryMBeans", metricsRole(AppIdentity(app))).View = true
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("capabilities JSON for %s: %w", AppIdentity(app), err)
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[AppIdentityPrefixed(app, "capabilities.json")] = data

	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) processAcceptor(serverConfigPropertiesSecret *corev1.Secret, app *broker.BrokerApp) (err error) {

	// TODO: need data plane trust store, this is access to the control plane trust store
	// they could be the same but we need two accessors
	trustStorePath, err := reconciler.getTrustStorePath(reconciler.instance)
	if err != nil {
		return err
	}

	namespacedName := AppIdentity(app)

	pemCfgkey := UnderscoreAppIdentityPrefixed(app, "tls.pemcfg")
	if serverConfigPropertiesSecret.Data[pemCfgkey], err = reconciler.makePemCfgProps(reconciler.instance); err != nil {
		return err
	}

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
	if len(reconciler.pendingEmptyAddresses[AppIdentity(app)]) > 0 {
		dedupMap[fmt.Sprintf("%s=%s\n", consumerRole(namespacedName), namespacedName)] = ""
	}

	rolesBuf := brokerproperties.NewPropsWithHeader()
	for _, k := range brokerproperties.SortedKeys(dedupMap) {
		fmt.Fprint(rolesBuf, k)
	}

	certRolesCfgKey := UnderscoreAppIdentityPrefixed(app, common.GetCertRolesKey(realmName))
	serverConfigPropertiesSecret.Data[certRolesCfgKey] = rolesBuf.Bytes()

	if app.Status.Service == nil {
		return fmt.Errorf("app %s has no service binding", AppIdentity(app))
	}
	port := app.Status.Service.AssignedPort
	if port == UnassignedPort {
		return fmt.Errorf("app %s has no assigned port", AppIdentity(app))
	}

	name := fmt.Sprintf("%d", port)
	secretsBase := path.Join(common.SecretPathBase, AppPropertiesSecretName(reconciler.instance.Name))

	acceptorJSON := &brokerproperties.AcceptorJSON{
		AcceptorConfigurations: map[string]*brokerproperties.AcceptorConfiguration{
			name: {
				FactoryClassName: brokerproperties.NettyAcceptorFactory,
				Params: brokerproperties.AcceptorParams{
					SecurityDomain: realmName,
					Host:           "${HOSTNAME}",
					Port:           port,
					SslEnabled:     true,
					NeedClientAuth: true,
					SaslMechanisms: brokerproperties.SaslExternal,
					KeyStoreType:   brokerproperties.KeyStoreTypePEMCFG,
					KeyStorePath:   path.Join(secretsBase, pemCfgkey),
					TrustStoreType: brokerproperties.TrustStoreTypePEMCA,
					TrustStorePath: trustStorePath,
				},
			},
		},
		JaasConfigs: map[string]*brokerproperties.JaasRealmConfig{
			realmName: {
				Modules: brokerproperties.JaasModules{
					Cert: brokerproperties.JaasLoginModule{
						LoginModuleClass: brokerproperties.TextFileCertLoginModule,
						ControlFlag:      brokerproperties.ControlFlagRequired,
						Params: brokerproperties.JaasModuleParams{
							TextFileDNRole: certRolesCfgKey,
							TextFileDNUser: certUsersCfgKey,
							BaseDir:        secretsBase,
							Reload:         true,
						},
					},
				},
			},
		},
	}

	acceptorData, err := json.Marshal(acceptorJSON)
	if err != nil {
		return fmt.Errorf("acceptor JSON for %s: %w", AppIdentity(app), err)
	}
	serverConfigPropertiesSecret.Data[AppIdentityPrefixed(app, "acceptor.json")] = acceptorData

	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) getTrustStorePath(_ *broker.BrokerService) (string, error) {

	var caCertSecret *corev1.Secret
	var caSecretKey string
	var err error
	if caCertSecret, err = common.GetOperatorCASecret(reconciler.Client); err == nil {
		if caSecretKey, err = common.GetOperatorCASecretKey(reconciler.Client, caCertSecret); err == nil {
			return fmt.Sprintf("/amq/extra/secrets/%s/%s", caCertSecret.Name, caSecretKey), nil
		}
	}
	return "", err
}

func (reconciler *BrokerServiceInstanceReconciler) makePemCfgProps(service *broker.BrokerService) ([]byte, error) {
	certSecret := certSecretName(service)
	return templates.Render(templates.PemCfg, templates.PemCfgConfig{
		CertKeyPath: path.Join(common.SecretPathBase, certSecret, "tls.key"),
		CertCrtPath: path.Join(common.SecretPathBase, certSecret, "tls.crt"),
	})
}

func jaasConfigRealmName(app *broker.BrokerApp) string {
	port := int32(DefaultStartPort)
	if app.Status.Service != nil && app.Status.Service.AssignedPort != UnassignedPort {
		port = app.Status.Service.AssignedPort
	}
	return fmt.Sprintf("port-%d", port)
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

func AppIdentityPrefix(app *broker.BrokerApp) string {
	return AppIdentity(app) + "@"
}

func AppIdentityWithGeneration(app *broker.BrokerApp) string {
	return fmt.Sprintf("%s%d", AppIdentityPrefix(app), app.Generation)
}

func FileAppIdentity(app *broker.BrokerApp) string {
	return DashPrefixValue(app.Namespace, app.Name)
}

func AppIdentityPrefixed(app *broker.BrokerApp, v string) string {
	return DashPrefixValue(FileAppIdentity(app), v)
}

func UnderscoreAppIdentityPrefixed(app *broker.BrokerApp, v string) string {
	return fmt.Sprintf("_%s", AppIdentityPrefixed(app, v))
}

func NameSpacedValue(app *broker.BrokerApp, v string) string {
	return SlashPrefixValue(app.Namespace, v)
}

func DashPrefixValue(prefix, value string) string {
	return fmt.Sprintf("%s-%s", prefix, value)
}

func SlashPrefixValue(prefix, value string) string {
	return fmt.Sprintf("%s/%s", prefix, value)
}

func (reconciler *BrokerServiceInstanceReconciler) controlPlaneOverrideSecretName() string {
	return reconciler.instance.Name + "-control-plane-override"
}

func (reconciler *BrokerServiceInstanceReconciler) processControlPlaneOverrideSecret(validApps []broker.BrokerApp) error {
	appQueues := queueOwners(validApps, reconciler.log)

	// Get or create the control-plane-override secret
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

	// Generate prometheus exporter yaml with queue-level metrics
	prometheusConfig, err := reconciler.generatePrometheusConfig(appQueues)
	if err != nil {
		return err
	}
	desired.Data[PrometheusConfigFileName] = prometheusConfig

	// Give each app an identity on the metrics endpoint. applyControlPlaneOverridesForBroker
	// replaces whole keys, so these are rendered complete, control-plane entries included.
	appEntries, err := reconciler.appIdentityEntries(validApps)
	if err != nil {
		return err
	}

	cns, err := common.ResolveControlPlaneCNs(reconciler.Client,
		common.GenerateBroker(reconciler.instance.Name, reconciler.instance.Namespace))
	if err != nil {
		return err
	}

	certUsers, err := templates.Render(templates.AppCertUsers, templates.AppCertUsersConfig{
		CertUsersConfig: templates.CertUsersConfig(cns),
		Apps:            appEntries,
	})
	if err != nil {
		return err
	}
	desired.Data[common.GetCertUsersKey(common.HttpAuthenticatorRealm)] = certUsers

	certRoles, err := templates.Render(templates.AppCertRoles, templates.AppCertRolesConfig{
		Apps: appEntries,
	})
	if err != nil {
		return err
	}
	desired.Data[common.GetCertRolesKey(common.HttpAuthenticatorRealm)] = certRoles

	for k, v := range desired.Data {
		reconciler.log.V(1).Info("Override", "Key", k, "Value", string(v))
	}
	reconciler.TrackDesired(desired)
	return nil
}

// appIdentityEntries maps the provisioned apps to their control-plane
// identities, sorted so the rendered override is stable.
func (reconciler *BrokerServiceInstanceReconciler) appIdentityEntries(validApps []broker.BrokerApp) ([]templates.AppIdentityEntry, error) {
	entries := make([]templates.AppIdentityEntry, 0, len(validApps))
	for i := range validApps {
		app := &validApps[i]

		cn, err := reconciler.appCertCN(app)
		if err != nil {
			return nil, err
		}

		entries = append(entries, templates.AppIdentityEntry{
			Identity:  AppIdentity(app),
			CNPattern: common.EscapeForRegex(cn),
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Identity < entries[j].Identity })
	return entries, nil
}

// appCertCN returns the common name of the app's client certificate, found by
// the <app>-app-cert convention, as ResolveControlPlaneCNs does for the other
// control-plane identities.
func (reconciler *BrokerServiceInstanceReconciler) appCertCN(app *broker.BrokerApp) (string, error) {
	secretName := app.Name + common.AppCertSecretSuffix

	secret, err := common.GetNamespacedSecret(reconciler.Client, secretName, app.Namespace)
	if err != nil {
		return "", err
	}

	subject, err := common.ExtractCertSubjectFromSecret(secret)
	if err != nil {
		return "", fmt.Errorf("failed to extract subject from app cert %s, %w", secretName, err)
	}

	return subject.CommonName, nil
}

func (reconciler *BrokerServiceInstanceReconciler) wasAppEverProvisioned(app *broker.BrokerApp) bool {
	prefix := AppIdentityPrefix(app)
	for _, pa := range reconciler.status.ProvisionedApps {
		if strings.HasPrefix(pa, prefix) {
			return true
		}
	}
	return false
}

// detectDrainingAddresses compares the current spec addresses against the previously
// deployed capabilities.json to find addresses that were removed. It checks Jolokia
// to determine if removed addresses still have undrained messages. Returns only addresses
// that still need draining.
func (reconciler *BrokerServiceInstanceReconciler) detectDrainingAddresses(app *broker.BrokerApp, currentTracker *AddressTracker, oldSecretData map[string][]byte) []DrainingAddressInfo {
	if !reconciler.wasAppEverProvisioned(app) {
		return nil
	}

	capKey := AppIdentityPrefixed(app, "capabilities.json")
	oldCapData, exists := oldSecretData[capKey]
	if !exists || len(oldCapData) == 0 {
		return nil
	}

	var oldCaps brokerproperties.CapabilitiesJSON
	if err := json.Unmarshal(oldCapData, &oldCaps); err != nil {
		reconciler.log.V(1).Info("Failed to unmarshal old capabilities, skipping drain detection",
			"app", app.Name, "error", err)
		return nil
	}

	if len(oldCaps.AddressConfigurations) == 0 {
		return nil
	}

	// Build set of currently owned addresses (exclude FQQN entries from the
	// tracker — those are per-queue entries derived from the address config)
	currentOwned := make(map[string]bool)
	for name, config := range currentTracker.names {
		if config.isOwned && !strings.Contains(name, FQQNSeparator) {
			currentOwned[name] = true
		}
	}

	// Find addresses in old config that are no longer in current spec
	appConsumerRole := consumerRole(AppIdentity(app))
	var candidates []DrainingAddressInfo
	for name, oldCfg := range oldCaps.AddressConfigurations {
		if currentOwned[name] {
			continue
		}
		dai := DrainingAddressInfo{Address: name}
		if addrRoles, ok := oldCaps.SecurityRoles[name]; ok {
			if perms, ok := addrRoles[appConsumerRole]; ok && perms.Consume {
				dai.HadConsumeRole = true
			}
		}
		// Extract subscription queue names from the old config
		if oldCfg.RoutingTypes == brokerproperties.RoutingTypeMulticast && oldCfg.QueueConfigs != nil {
			for queueName, qc := range oldCfg.QueueConfigs {
				if qc.RoutingType == brokerproperties.RoutingTypeMulticast && queueName != name {
					dai.Subscriptions = append(dai.Subscriptions, queueName)
				}
			}
			sort.Strings(dai.Subscriptions)
		}
		candidates = append(candidates, dai)
	}

	if len(candidates) == 0 {
		return nil
	}

	// Check Jolokia for drain status
	drainResults, err := reconciler.checkAddressDrainStatus(context.TODO(), candidates)
	if err != nil {
		reconciler.log.V(1).Info("Error checking address drain status, preserving all candidates",
			"app", app.Name, "error", err)
		return candidates
	}

	var undrained []DrainingAddressInfo
	for _, candidate := range candidates {
		if dr, checked := drainResults[candidate.Address]; checked && dr.drained {
			reconciler.log.V(1).Info("Address drain complete", "app", app.Name, "address", candidate.Address)
		} else {
			reconciler.log.V(1).Info("Address drain in progress", "app", app.Name, "address", candidate.Address)
			if checked {
				candidate.MessageCounts = dr.messageCounts
			}
			undrained = append(undrained, candidate)
		}
	}
	return undrained
}

type DrainingAddressInfo struct {
	Address        string           `json:"address"`
	Subscriptions  []string         `json:"subscriptions,omitempty"`
	HadConsumeRole bool             `json:"-"`
	MessageCounts  map[string]int64 `json:"-"`
}

type addressDrainResult struct {
	drained       bool
	messageCounts map[string]int64
}

func (reconciler *BrokerServiceInstanceReconciler) checkAddressDrainStatus(ctx context.Context, addresses []DrainingAddressInfo) (map[string]addressDrainResult, error) {
	brokerCR := &broker.Broker{}
	brokerKey := types.NamespacedName{
		Name:      reconciler.instance.Name,
		Namespace: reconciler.instance.Namespace,
	}
	if err := reconciler.Get(ctx, brokerKey, brokerCR); err != nil {
		return nil, fmt.Errorf("failed to get Broker CR for address drain check: %v", err)
	}

	jkInfos := jolokia_client.GetMinimalJolokiaAgentsForBroker(brokerCR, reconciler.Client)
	if len(jkInfos) == 0 {
		return nil, fmt.Errorf("no Jolokia agents found for broker %s", brokerKey.Name)
	}

	return evaluateAddressDrainStatus(reconciler.log, jkInfos[0].Artemis, addresses), nil
}

func evaluateAddressDrainStatus(reqLogger logr.Logger, artemis *mgmt.Artemis, addresses []DrainingAddressInfo) map[string]addressDrainResult {
	queueCounts, err := artemis.GetAllQueueMessageCounts()
	if err != nil {
		reqLogger.V(1).Info("Pattern read for queue counts failed, marking all addresses as undrained", "error", err)
	}

	var queueCountMap map[string]int64
	if len(queueCounts) > 0 {
		queueCountMap = make(map[string]int64, len(queueCounts))
		for _, qc := range queueCounts {
			key := qc.Address + "::" + qc.Queue + "::" + qc.RoutingType
			queueCountMap[key] = qc.Count
		}
	}

	result := make(map[string]addressDrainResult, len(addresses))
	for _, addr := range addresses {
		dr := addressDrainResult{messageCounts: make(map[string]int64)}
		if queueCountMap != nil {
			dr.drained = true
			if len(addr.Subscriptions) > 0 {
				for _, sub := range addr.Subscriptions {
					key := addr.Address + "::" + sub + "::multicast"
					fqqn := addr.Address + "::" + sub
					if count, found := queueCountMap[key]; found {
						dr.messageCounts[fqqn] = count
						if count > 0 {
							dr.drained = false
						}
					}
					// queue not found in map = queue no longer exists on the broker = drained
				}
			} else {
				key := addr.Address + "::" + addr.Address + "::anycast"
				if count, found := queueCountMap[key]; found {
					dr.messageCounts[addr.Address] = count
					if count > 0 {
						dr.drained = false
					}
				}
				// queue not found in map = queue no longer exists on the broker = drained
			}
		} else {
			dr.drained = false
		}
		result[addr.Address] = dr
		reqLogger.V(1).Info("Address drain status", "address", addr.Address, "drained", dr.drained)
	}
	return result
}

// queueOwners maps every queue the validated apps produce to or consume from to
// the app that owns it, whose namespace its series are labelled with. An
// anycast queue belongs to the app that declares the address, which a
// referencing app names in its AddressRef. A subscription queue is created for
// the subscriber and named by it, so it belongs to the subscriber, whoever owns
// the address.
func queueOwners(validApps []broker.BrokerApp, log logr.Logger) map[string]templates.QueueOwner {
	apps := make([]*broker.BrokerApp, len(validApps))
	for i := range validApps {
		apps[i] = &validApps[i]
	}
	// the first claim wins, so claims are made in a stable order
	sort.Slice(apps, func(i, j int) bool { return AppIdentity(apps[i]) < AppIdentity(apps[j]) })

	owners := make(map[string]templates.QueueOwner)
	claim := func(queue string, owner templates.QueueOwner) {
		if current, found := owners[queue]; found {
			if current != owner {
				log.V(1).Info("queue claimed by more than one app, keeping the first",
					"queue", queue, "kept", current, "ignored", owner)
			}
			return
		}
		owners[queue] = owner
	}

	for _, app := range apps {
		self := templates.QueueOwner{OwnerNamespace: app.Namespace, OwnerApp: app.Name}
		for _, capability := range app.Spec.Capabilities {
			for _, refs := range [][]broker.AddressRef{capability.ConsumerOf, capability.ProducerOf} {
				for _, addressRef := range refs {
					if isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
						for _, queueName := range addressRef.Subscriptions {
							claim(addressRef.Address+FQQNSeparator+queueName, self)
						}
						continue
					}
					owner := self
					if addressRef.AppName != "" {
						owner.OwnerApp = addressRef.AppName
						if addressRef.AppNamespace != "" {
							owner.OwnerNamespace = addressRef.AppNamespace
						}
					}
					claim(addressRef.Address, owner)
				}
			}
		}
	}

	return owners
}

func (reconciler *BrokerServiceInstanceReconciler) generatePrometheusConfig(appQueues map[string]templates.QueueOwner) ([]byte, error) {
	var caSecret, caSecretKey string
	if caCertSecret, err := common.GetOperatorCASecret(reconciler.Client); err == nil {
		caSecret = caCertSecret.Name
		if key, err := common.GetOperatorCASecretKey(reconciler.Client, caCertSecret); err == nil {
			caSecretKey = key
		}
	}
	mountPathRoot := path.Join(common.SecretPathBase, reconciler.instance.Name+"-props")
	return templates.RenderServicePrometheus(templates.ServicePrometheusConfig{
		PemCfgPath:       path.Join(mountPathRoot, "_cert.pemcfg"),
		CATrustStorePath: path.Join(common.SecretPathBase, caSecret, caSecretKey),
		BrokerName:       reconciler.instance.Name,
	}, appQueues)
}
