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
	"fmt"
	"reflect"
	"sort"
	"strings"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/appselector"
	servicemetrics "github.com/arkmq-org/arkmq-org-broker-operator/pkg/metrics"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
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

	statusErr, retry := processor.processStatus(err)
	if err == nil {
		err = statusErr
	}
	if err == nil && retry {
		return ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}, nil
	}
	return ctrl.Result{}, err
}

// instance specifics for a reconciler loop
func (r *BrokerServiceReconciler) getOwned() []client.ObjectList {
	return []client.ObjectList{
		&corev1.SecretList{},
		&broker.BrokerList{},
		&corev1.ServiceList{}}
}

func (r *BrokerServiceReconciler) getOrderedTypeList() []reflect.Type {
	// we want to create/update in this order
	return []reflect.Type{
		reflect.TypeOf(corev1.Secret{}),
		reflect.TypeOf(broker.Broker{}),
		reflect.TypeOf(corev1.Service{})}
}

func (reconciler *BrokerServiceInstanceReconciler) validateSpec() error {
	// Validate resource name
	if err := ValidateResourceNameAndSetCondition(reconciler.instance.Name, &reconciler.status.Conditions); err != nil {
		return err
	}

	// Validate CEL expression if provided
	if reconciler.instance.Spec.AppSelectorExpression != "" {
		if err := appselector.ValidateExpression(reconciler.instance.Spec.AppSelectorExpression); err != nil {
			meta.SetStatusCondition(&reconciler.status.Conditions, metav1.Condition{
				Type:    broker.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  broker.ValidConditionSpecSelectorError,
				Message: fmt.Sprintf("invalid appSelectorExpression: %v", err),
			})
			return err
		}
	}

	// All validations passed - set Valid=True
	meta.SetStatusCondition(&reconciler.status.Conditions, metav1.Condition{
		Type:   broker.ValidConditionType,
		Status: metav1.ConditionTrue,
		Reason: broker.ValidConditionSuccessReason,
	})
	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) processSpec() (err error) {
	if err = reconciler.processBroker(); err != nil {
		return err
	}

	// Process service
	return reconciler.processService()
}

func (reconciler *BrokerServiceInstanceReconciler) processBroker() (err error) {

	var desired *broker.Broker
	obj := reconciler.CloneOfDeployed(reflect.TypeOf(broker.Broker{}), reconciler.instance.Name)
	if obj != nil {
		desired = obj.(*broker.Broker)
	} else {
		desired = common.GenerateArtemis(reconciler.instance.Name, reconciler.instance.Namespace)
	}
	desired.Spec.Restricted = common.NewTrue()
	desired.Spec.DeploymentPlan.PersistenceEnabled = false
	desired.Spec.DeploymentPlan.Clustered = common.NewFalse()
	desired.Spec.DeploymentPlan.Labels = map[string]string{
		// Standard Kubernetes labels
		common.LabelAppKubernetesInstance:  reconciler.instance.Name,
		common.LabelAppKubernetesComponent: "broker-service",
		common.LabelAppKubernetesManagedBy: "arkmq-org-broker-operator",
		// Domain-specific labels
		common.LabelBrokerService:   reconciler.instance.Name,
		common.LabelBrokerPeerIndex: "0",
	}
	desired.Spec.Env = reconciler.instance.Spec.Env
	desired.Spec.DeploymentPlan.Resources = reconciler.instance.Spec.Resources

	if reconciler.instance.Spec.Image != nil {
		desired.Spec.DeploymentPlan.Image = *reconciler.instance.Spec.Image
	}

	desired.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{
		reconciler.appPropertiesSecretName(),
	}

	err = reconciler.processAppSecrets()

	reconciler.TrackDesired(desired)

	return err
}

func (reconciler *BrokerServiceInstanceReconciler) processAppSecrets() (err error) {
	// avoid restart for app onboarding with existing mount points
	// TODO potentially N app-secrets to overcome 1Mb size limit
	resourceName := types.NamespacedName{
		Namespace: reconciler.instance.Namespace,
		Name:      reconciler.appPropertiesSecretName(),
	}

	var desired *corev1.Secret

	obj := reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Secret{}), resourceName.Name)
	if obj != nil {
		desired = obj.(*corev1.Secret)
	} else {
		desired = secrets.NewSecret(resourceName, nil, nil)
	}

	// find all apps that select this service
	apps := &broker.BrokerAppList{}
	key := reconciler.instance.Namespace + ":" + reconciler.instance.Name
	if err = reconciler.Client.List(context.TODO(), apps, client.MatchingFields{common.AppServiceBindingField: key}); err != nil {
		return err
	}

	// reset data
	desired.Data = make(map[string][]byte)
	appIdentities := make([]string, 0, len(apps.Items))
	rejectedApps := make([]broker.RejectedApp, 0)
	validApps := make([]broker.BrokerApp, 0, len(apps.Items))

	for _, app := range apps.Items {
		valid, rejectionReason := reconciler.validateAppForProvisioning(&app, key)
		if !valid {
			// App failed validation - track it for user visibility
			rejectedApps = append(rejectedApps, broker.RejectedApp{
				Name:      app.Name,
				Namespace: app.Namespace,
				Reason:    rejectionReason,
			})
			continue
		}

		// Validate app name for safe file path construction
		if err = common.ValidateResourceName(app.Name); err != nil {
			reconciler.log.Error(err, "invalid app name", "app", app.Name)
			break
		}
		if err = reconciler.processCapabilities(desired, &app); err != nil {
			reconciler.log.Error(err, "failed to process capabilities for app", "app", app.Name)
			break
		}
		if err = reconciler.processAcceptor(desired, &app); err != nil {
			reconciler.log.Error(err, "failed to process acceptor for app", "app", app.Name)
			break
		}
		appIdentities = append(appIdentities, AppIdentity(&app))
		validApps = append(validApps, app)
	}

	sort.Strings(appIdentities)
	if desired.Annotations == nil {
		desired.Annotations = make(map[string]string)
	}
	desired.Annotations[common.ProvisionedAppsAnnotation] = strings.Join(appIdentities, ",")

	// Track rejected apps in status for user visibility
	reconciler.status.RejectedApps = rejectedApps

	reconciler.TrackDesired(desired)

	// Update prometheus config in control-plane-override secret with queue-level metrics
	if err == nil {
		err = reconciler.processControlPlaneOverrideSecret(validApps)
	}

	return err
}

func (reconciler *BrokerServiceInstanceReconciler) appPropertiesSecretName() string {
	return AppPropertiesSecretName(reconciler.instance.Name)
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

func (reconciler *BrokerServiceInstanceReconciler) processStatus(reconcilerError error) (err error, retry bool) {

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
		// Check if error is a ConditionError with a specific reason
		if condErr, ok := AsConditionError(reconcilerError); ok {
			deployedCondition.Status = metav1.ConditionFalse
			deployedCondition.Reason = condErr.Reason
			deployedCondition.Message = condErr.Message
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

	buf := NewPropsWithHeader()
	for _, k := range sortedKeys(props) {
		fmt.Fprint(buf, k)
	}

	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[AppIdentityPrefixed(app, "capabilities.properties")] = buf.Bytes()

	return err
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
	serverConfigPropertiesSecret.Data[pemCfgkey] = reconciler.makePemCfgProps(reconciler.instance)

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
	usersBuf := NewPropsWithHeader()
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

	rolesBuf := NewPropsWithHeader()
	for _, k := range sortedKeys(dedupMap) {
		fmt.Fprint(rolesBuf, k)
	}

	certRolesCfgKey := UnderscoreAppIdentityPrefixed(app, common.GetCertRolesKey(realmName))
	serverConfigPropertiesSecret.Data[certRolesCfgKey] = rolesBuf.Bytes()

	acceptorCfgKey := AppIdentityPrefixed(app, "acceptor.properties")

	buf := NewPropsWithHeader()

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
	fmt.Fprintf(buf, "acceptorConfigurations.\"%s\".params.keyStorePath=/amq/extra/secrets/%s/%s\n", name, AppPropertiesSecretName(reconciler.instance.Name), pemCfgkey)
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

func (reconciler *BrokerServiceInstanceReconciler) makePemCfgProps(service *broker.BrokerService) []byte {

	buf := NewPropsWithHeader()

	certSecretName := certSecretName(service)

	fmt.Fprintf(buf, "source.key=/amq/extra/secrets/%s/tls.key\n", certSecretName)
	fmt.Fprintf(buf, "source.cert=/amq/extra/secrets/%s/tls.crt\n", certSecretName)

	return buf.Bytes()
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

func (reconciler *BrokerServiceInstanceReconciler) processControlPlaneOverrideSecret(validApps []broker.BrokerApp) error {
	// Collect all unique ConsumerOf addresses from validated apps only
	consumerAddresses := make(map[string]bool)
	for _, app := range validApps {
		for _, capability := range app.Spec.Capabilities {
			for _, addressRef := range capability.ConsumerOf {
				if !isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
					// ANYCAST - direct queue address
					consumerAddresses[addressRef.Address] = true
				} else {
					// MULTICAST - generate FQQN for each multicast queue
					for _, queueName := range addressRef.Subscriptions {
						fqqn := addressRef.Address + FQQNSeparator + queueName
						consumerAddresses[fqqn] = true
					}
				}
			}
		}
	}

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
	prometheusConfig := reconciler.generatePrometheusConfig(consumerAddresses)
	desired.Data["_prometheus_exporter.yaml"] = prometheusConfig

	reconciler.TrackDesired(desired)
	return nil
}

func (reconciler *BrokerServiceInstanceReconciler) generatePrometheusConfig(consumerAddresses map[string]bool) []byte {
	buf := NewPropsWithHeader() // yaml

	// HTTP server config with mTLS
	var caSecret string
	var caSecretKey string
	if caCertSecret, err := common.GetOperatorCASecret(reconciler.Client); err == nil {
		caSecret = caCertSecret.Name
		if key, err := common.GetOperatorCASecretKey(reconciler.Client, caCertSecret); err == nil {
			caSecretKey = key
		}
	}

	// Broker reconciler creates broker properties secret with "-props" suffix
	brokerPropsSecretName := reconciler.instance.Name + "-props"
	mountPathRoot := fmt.Sprintf("%s%s", common.SecretPathBase, brokerPropsSecretName)

	fmt.Fprintf(buf, "httpServer:\n")
	fmt.Fprintf(buf, "  authentication:\n")
	fmt.Fprintf(buf, "    plugin:\n")
	fmt.Fprintf(buf, "      class: org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator\n")
	fmt.Fprintf(buf, "      subjectAttributeName: org.jolokia.jaasSubject\n")
	fmt.Fprintf(buf, "  ssl:\n")
	fmt.Fprintf(buf, "    mutualTLS: true\n")
	fmt.Fprintf(buf, "    keyStore:\n")
	fmt.Fprintf(buf, "      filename: %s/_cert.pemcfg\n", mountPathRoot)
	fmt.Fprintf(buf, "      type: PEMCFG\n")
	fmt.Fprintf(buf, "    trustStore:\n")
	fmt.Fprintf(buf, "      filename: %s%s/%s\n", common.SecretPathBase, caSecret, caSecretKey)
	fmt.Fprintf(buf, "      type: PEMCA\n")
	fmt.Fprintf(buf, "    certificate:\n")
	fmt.Fprintf(buf, "      alias: alias\n")

	// Collector/scraper config

	fmt.Fprintf(buf, "attrNameSnakeCase: true\n")

	// just queues, rbac will limit values returned
	fmt.Fprintf(buf, "includeObjectNames:\n")
	fmt.Fprintf(buf, "  - \"org.apache.activemq.artemis:broker=*,component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*\"\n")

	brokerName := reconciler.instance.Name // Use service name as broker name for restricted mode

	// Add queue-level attributes for specific queues with exact ObjectNames (include quotes) for canonocial string match, this restricts the attribute load
	if len(consumerAddresses) > 0 {
		fmt.Fprintf(buf, "includeObjectNameAttributes:\n")
		for address := range consumerAddresses {
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
