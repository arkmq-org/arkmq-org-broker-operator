/*
Copyright 2024.

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
	"strings"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/appselector"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// FQQNSeparator is the separator used in Fully Qualified Queue Names (address-name::queue-name)
	FQQNSeparator = "::"
)

// extractBaseAddress extracts the base address from a potentially FQQN format address
// E.g., "events::queue1" returns "events", "orders" returns "orders"
func extractBaseAddress(address string) string {
	if strings.Contains(address, FQQNSeparator) {
		parts := strings.SplitN(address, FQQNSeparator, 2)
		return parts[0]
	}
	return address
}

// collectOwnedAddresses returns all addresses "owned" by an app for clash detection purposes.
//
// An app owns an address if:
//  1. Explicitly declared in spec.addresses (shareable with other apps)
//  2. Implicitly used in capabilities with empty appNamespace/appName (private, not shareable)
//
// Both types count as "owned" to prevent address clashes between apps on the same service,
// but only explicitly declared addresses (type 1) can be referenced by other apps via addressRef.
//
// This function is used for:
//   - Clash detection: prevent two apps from using the same address name
//   - AddressConfigurations generation: app needs broker config for all addresses it uses
//
// This function is NOT used for:
//   - Sharing validation: checkAddressRefCapacity() checks spec.addresses directly (only explicit addresses are shareable)
func collectOwnedAddresses(app *broker.BrokerApp) map[string]bool {
	addresses := make(map[string]bool)

	// Add from spec.addresses
	for _, addrType := range app.Spec.Addresses {
		addresses[addrType.Address] = true
	}

	// Add from capabilities (local addresses only - where appNamespace and appName are empty)
	for _, capability := range app.Spec.Capabilities {
		allAddresses := [][]broker.AddressRef{
			capability.ProducerOf,
			capability.ConsumerOf,
			capability.SubscriberOf,
		}

		for _, addrList := range allAddresses {
			for _, addressRef := range addrList {
				// Only count as owned if this is a local reference (no cross-app fields)
				if addressRef.AppNamespace == "" && addressRef.AppName == "" {
					baseAddr := extractBaseAddress(addressRef.Address)
					addresses[baseAddr] = true
				}
			}
		}
	}

	return addresses
}

type BrokerAppReconciler struct {
	*ReconcilerLoop
}

type BrokerAppInstanceReconciler struct {
	*BrokerAppReconciler
	instance *broker.BrokerApp
	service  *broker.BrokerService
	status   *broker.BrokerAppStatus
}

func (reconciler BrokerAppInstanceReconciler) validateSpec() error {
	// Validate resource name
	if err := ValidateResourceNameAndSetCondition(reconciler.instance.Name, &reconciler.status.Conditions); err != nil {
		return err
	}

	// Validate capability address types (structural checks only)
	if err := reconciler.verifyCapabilityAddressType(); err != nil {
		return err
	}

	return nil
}

func (reconciler BrokerAppInstanceReconciler) processBindingSecret() error {

	// Only manage binding secret if app has been bound to a service (status field exists)
	if reconciler.status.Service == nil {
		return nil
	}

	bindingSecretNsName := types.NamespacedName{
		Namespace: reconciler.instance.Namespace,
		Name:      BindingsSecretName(reconciler.instance.Name),
	}

	var desired *corev1.Secret

	obj := reconciler.CloneOfDeployed(reflect.TypeOf(corev1.Secret{}), bindingSecretNsName.Name)
	if obj != nil {
		desired = obj.(*corev1.Secret)
	} else {
		desired = secrets.NewSecret(bindingSecretNsName, nil, nil)
	}

	port := reconciler.status.Service.AssignedPort
	if port == UnassignedPort {
		return fmt.Errorf("no port assigned for app %s", reconciler.instance.Name)
	}

	desired.Data = map[string][]byte{
		// host as FQQN to work everywhere in the cluster
		"host": []byte(fmt.Sprintf("%s.%s.svc.%s", reconciler.status.Service.Name, reconciler.status.Service.Namespace, common.GetClusterDomain())),
		"port": []byte(fmt.Sprintf("%d", port)),
		"uri":  []byte(fmt.Sprintf("amqps://%s.%s.svc.%s:%d", reconciler.status.Service.Name, reconciler.status.Service.Namespace, common.GetClusterDomain(), port)),
	}
	reconciler.TrackDesired(desired)
	return nil
}

func NewBrokerAppReconciler(client client.Client, scheme *runtime.Scheme, config *rest.Config, logger logr.Logger) *BrokerAppReconciler {
	reconciler := BrokerAppReconciler{ReconcilerLoop: &ReconcilerLoop{KubeBits: &KubeBits{
		Client: client, Scheme: scheme, Config: config, log: logger}}}
	reconciler.ReconcilerLoopType = &reconciler
	return &reconciler
}

//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerservices,verbs=get;list;watch;update

func (reconciler *BrokerAppReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := reconciler.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "BrokerApp")

	instance := &broker.BrokerApp{}
	var err = reconciler.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	localLoop := &ReconcilerLoop{
		KubeBits:           reconciler.KubeBits,
		ReconcilerLoopType: reconciler,
	}

	processor := BrokerAppInstanceReconciler{
		BrokerAppReconciler: &BrokerAppReconciler{ReconcilerLoop: localLoop},
		instance:            instance,
		status:              instance.Status.DeepCopy(),
	}

	reqLogger.V(2).Info("Reconciler Processing...", "CRD.Name", instance.Name, "CRD ver", instance.ObjectMeta.ResourceVersion, "CRD Gen", instance.ObjectMeta.Generation)
	if err = processor.validateSpec(); err == nil {
		if err = processor.resolveBrokerService(); err == nil {
			if err = processor.InitDeployed(instance, processor.getOwned()...); err == nil {
				if err = processor.processBindingSecret(); err == nil {
					err = processor.SyncDesiredWithDeployed(processor.instance)
				}
			}
		}
	}

	statusErr, retry := processor.processStatus(err)

	if _, isCondErr := AsConditionError(err); isCondErr {
		// reflected in status, don't return to the event loop
		err = nil
	}
	if err == nil {
		err = statusErr
	}
	reqLogger.V(2).Info("Reconciler Processed...", "CRD.Name", instance.Name, "CRD ver", instance.ObjectMeta.ResourceVersion, "CRD Gen", instance.ObjectMeta.Generation, "error", err)
	if err == nil && retry {
		return ctrl.Result{Requeue: true, RequeueAfter: common.GetReconcileResyncPeriod()}, nil
	}
	return ctrl.Result{}, err
}

// instance specifics for a reconciler loop
func (r *BrokerAppReconciler) getOwned() []client.ObjectList {
	return []client.ObjectList{&corev1.SecretList{}}
}

func (r *BrokerAppReconciler) getOrderedTypeList() []reflect.Type {
	types := make([]reflect.Type, 1)
	types[0] = reflect.TypeOf(corev1.Secret{})
	return types
}

func (reconciler *BrokerAppInstanceReconciler) resolveBrokerService() error {

	// find the matching service to find the matching brokers
	var list = &broker.BrokerServiceList{}
	var opts, err = metav1.LabelSelectorAsSelector(reconciler.instance.Spec.ServiceSelector)
	if err != nil {
		err = fmt.Errorf("failed to evaluate Spec.Selector %v", err)
		meta.SetStatusCondition(&reconciler.status.Conditions, metav1.Condition{
			Type:    broker.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  broker.ValidConditionSpecSelectorError,
			Message: err.Error(),
		})
		return err
	}
	err = reconciler.Client.List(context.TODO(), list, &client.ListOptions{LabelSelector: opts})
	if err != nil {
		// API error, not a CR validation issue - let processStatus handle it in Deployed condition
		return fmt.Errorf("Spec.Selector list error %v", err)
	}

	// CR spec is valid (selector syntax was valid)
	// Set this early so it's always set even if we return errors below
	// Runtime issues (no matching services, no capacity, API errors) are handled in Deployed condition
	meta.SetStatusCondition(&reconciler.status.Conditions, metav1.Condition{
		Type:   broker.ValidConditionType,
		Status: metav1.ConditionTrue,
		Reason: broker.ValidConditionSuccessReason,
	})

	var service *broker.BrokerService
	needsServiceAssignment := false

	// Check if we have an existing binding in status
	hasBinding := reconciler.status.Service != nil

	if hasBinding {
		deployedTo := reconciler.status.Service.Key()
		// Try to find the bound service in the list
		for index, candidate := range list.Items {
			if candidate.Namespace == reconciler.status.Service.Namespace && candidate.Name == reconciler.status.Service.Name {
				service = &list.Items[index]
				break
			}
		}

		// Check if we found the service and if it still matches
		if service != nil {
			// Check if app is in service's RejectedApps (orphaned port detection)
			if reconciler.isAppRejectedByService(service) {
				reconciler.log.V(1).Info("App rejected by service (orphaned port), reassigning",
					"app", reconciler.instance.Name,
					"service", deployedTo,
					"currentPort", reconciler.status.Service.AssignedPort)
				reconciler.status.Service = nil
				service = nil
				needsServiceAssignment = true
			} else {
				matches, matchErr := reconciler.matchesServiceSelector(service)
				if matchErr != nil {
					// CEL evaluation error - surface to user via status
					return NewConditionError(broker.DeployedConditionSelectorEvaluationError,
						"error evaluating service selector: %v", matchErr)
				}
				if !matches {
					reconciler.log.V(1).Info("App no longer matches service selector, removing binding",
						"app", reconciler.instance.Name,
						"service", deployedTo)
					reconciler.status.Service = nil
					service = nil
					needsServiceAssignment = true
				}
			}
		}

		// If bound service not found, selector may have changed
		if service == nil && !needsServiceAssignment {
			if len(list.Items) > 0 {
				reconciler.log.V(1).Info("Bound service not found in selector matches, reassigning",
					"app", reconciler.instance.Name,
					"old-binding", deployedTo,
					"matching-services", len(list.Items))
				needsServiceAssignment = true
			}
			// else: no services match current selector, processStatus will handle it
		}
	} else {
		// No binding yet, need initial assignment
		needsServiceAssignment = true
	}

	// Assign service if needed (initial assignment or reassignment due to selector change)
	if needsServiceAssignment {

		if len(list.Items) == 0 {
			// No matching services is a runtime issue, not a CR validation issue
			// Let processStatus handle it in Deployed condition
			return NewConditionError(broker.DeployedConditionNoMatchingServiceReason,
				"no matching services available for selector %v", opts)
		}

		var assignedPort int32
		service, assignedPort, err = reconciler.findServiceWithCapacity(list)
		if service != nil {
			// findServiceWithCapacity already found a service and assigned a port
			// Set service binding including assigned port
			reconciler.status.Service = &broker.BrokerServiceBindingStatus{
				Name:         service.Name,
				Namespace:    service.Namespace,
				Secret:       BindingsSecretName(reconciler.instance.Name),
				AssignedPort: assignedPort,
			}
			reconciler.log.V(1).Info("Assigned port to app",
				"app", reconciler.instance.Name,
				"service", service.Name,
				"port", assignedPort,
				"poolSource", service.Status.AvailablePorts.Source)
		} else {
			// Check if error is already a ConditionError (e.g., AppSelectorNoMatch)
			if _, isCondErr := AsConditionError(err); isCondErr {
				// Already a ConditionError, return as-is
				return err
			}
			// No service with capacity is a runtime issue, not a CR validation issue
			// Let processStatus handle it in Deployed condition
			err = NewConditionError(broker.DeployedConditionNoServiceCapacityReason,
				"no service with capacity available for selector %v, %v", opts, err)
		}
	}

	reconciler.service = service
	return err
}

func BindingsSecretName(crName string) string {
	return fmt.Sprintf("%s-binding-secret", crName)
}

// serviceKey returns the field indexer key for a BrokerService
func serviceKey(service *broker.BrokerService) string {
	return service.Namespace + ":" + service.Name
}

// matchesServiceSelector checks if the app matches the service's appSelectorExpression.
// Returns (matches, nil) if evaluation succeeds.
// Returns (false, error) if CEL evaluation fails - caller should surface this to user.
func (reconciler *BrokerAppInstanceReconciler) matchesServiceSelector(service *broker.BrokerService) (bool, error) {
	matches, err := appselector.Matches(reconciler.instance, service, reconciler.Client)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate appSelectorExpression on service %s/%s: %v",
			service.Namespace, service.Name, err)
	}

	// Additional debug logging for BrokerApp controller
	if reconciler.log.V(2).Enabled() {
		expression := service.Spec.AppSelectorExpression
		if expression == "" {
			expression = appselector.DefaultExpression
		}
		reconciler.log.V(2).Info("App selector check result",
			"expression", expression,
			"app", reconciler.instance.Namespace+"/"+reconciler.instance.Name,
			"service", service.Namespace+"/"+service.Name,
			"matches", matches)
	}

	return matches, nil
}

// RejectionCategory categorizes why a service cannot accept an app
type RejectionCategory int

const (
	RejectionNotDeployed   RejectionCategory = iota // Service not deployed yet
	RejectionSelector                               // Service selector doesn't match app
	RejectionSelectorError                          // CEL evaluation error
	RejectionAddressRef                             // AddressRef dependency not satisfied
	RejectionAddressClash                           // Address name conflict with existing app
	RejectionMemory                                 // Insufficient memory capacity
	RejectionPortPool                               // Port pool exhausted or not configured
	RejectionOther                                  // Other errors
)

// ServiceRejection tracks why a specific service rejected an app
type ServiceRejection struct {
	ServiceName string
	Category    RejectionCategory
	Message     string
}

func (reconciler *BrokerAppInstanceReconciler) findServiceWithCapacity(list *broker.BrokerServiceList) (chosen *broker.BrokerService, assignedPort int32, err error) {
	if len(list.Items) == 0 {
		return nil, UnassignedPort, fmt.Errorf("no services in list")
	}

	// Get the app's resource requirements
	appMemoryRequest := reconciler.instance.Spec.Resources.Requests.Memory()

	var bestService *broker.BrokerService
	var bestServicePort int32 = UnassignedPort
	var maxAvailable int64 = -1

	// Track why services were rejected for better error messages
	var rejections []ServiceRejection

	for i := range list.Items {
		service := &list.Items[i]

		// Only consider deployed services (port discovery must be complete)
		deployedCond := meta.FindStatusCondition(service.Status.Conditions, broker.DeployedConditionType)
		if deployedCond == nil || deployedCond.Status != metav1.ConditionTrue {
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionNotDeployed,
				Message:     "service not deployed yet (port discovery pending)",
			})
			continue
		}

		// Check selector match first
		matches, matchErr := reconciler.matchesServiceSelector(service)
		if matchErr != nil {
			// CEL evaluation error - continue to check other services
			reconciler.log.V(1).Info("Failed to evaluate selector for service",
				"service", service.Name,
				"error", matchErr)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionSelectorError,
				Message:     fmt.Sprintf("selector evaluation failed: %v", matchErr),
			})
			continue
		}
		if !matches {
			reconciler.log.V(1).Info("App does not match service selector",
				"service", service.Name,
				"app-namespace", reconciler.instance.Namespace)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionSelector,
				Message:     "does not match selector",
			})
			continue
		}

		// Check addressRef dependencies (cross-app address sharing)
		if addrRefErr := reconciler.checkAddressRefCapacity(service); addrRefErr != nil {
			reconciler.log.V(1).Info("Service does not satisfy addressRef dependencies",
				"service", service.Name,
				"error", addrRefErr)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionAddressRef,
				Message:     addrRefErr.Error(),
			})
			continue
		}

		// Check for address clashes with apps already on this service
		if clashErr := reconciler.checkAddressClashOnService(service); clashErr != nil {
			reconciler.log.V(1).Info("Service has address clash",
				"service", service.Name,
				"error", clashErr)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionAddressClash,
				Message:     clashErr.Error(),
			})
			continue
		}

		// Check memory capacity
		available, checkErr := reconciler.getAvailableMemory(service)
		if checkErr != nil {
			reconciler.log.V(1).Info("Failed to check capacity for service",
				"service", service.Name,
				"error", checkErr)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionOther,
				Message:     fmt.Sprintf("error checking capacity: %v", checkErr),
			})
			continue
		}

		if appMemoryRequest != nil && available < appMemoryRequest.Value() {
			reconciler.log.V(1).Info("Service has insufficient memory capacity",
				"service", service.Name,
				"available", available,
				"required", appMemoryRequest.Value())
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionMemory,
				Message:     fmt.Sprintf("insufficient memory (available: %d, required: %d)", available, appMemoryRequest.Value()),
			})
			continue
		}

		// Check port capacity - must have port pool configured
		if service.Status.AvailablePorts == nil {
			reconciler.log.V(1).Info("Service has no port pool configured yet",
				"service", service.Name)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionPortPool,
				Message:     "port pool not yet configured",
			})
			continue
		}

		// Check if we have an available port and assign it
		apps, listErr := reconciler.listOtherAppsForService(service)
		if listErr != nil {
			reconciler.log.V(1).Info("Failed to list apps for port capacity check",
				"service", service.Name,
				"error", listErr)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionOther,
				Message:     fmt.Sprintf("error checking port capacity: %v", listErr),
			})
			continue
		}

		usedPorts := collectUsedPorts(apps, reconciler.instance)
		candidatePort, portErr := assignPortFromPool(service.Status.AvailablePorts, usedPorts)
		if portErr != nil {
			reconciler.log.V(1).Info("Service has no available ports",
				"service", service.Name,
				"error", portErr)
			rejections = append(rejections, ServiceRejection{
				ServiceName: service.Name,
				Category:    RejectionPortPool,
				Message:     fmt.Sprintf("port pool exhausted: %v", portErr),
			})
			continue
		}

		// Track service with most available capacity
		if available > maxAvailable {
			maxAvailable = available
			bestService = service
			bestServicePort = candidatePort
		}
	}

	if bestService == nil {
		return nil, UnassignedPort, reconciler.buildCapacityError(rejections, appMemoryRequest)
	}

	reconciler.log.V(1).Info("Selected service with capacity",
		"service", bestService.Name,
		"available-memory", maxAvailable,
		"assigned-port", bestServicePort)
	return bestService, bestServicePort, nil
}

// buildCapacityError analyzes the structured rejection data and constructs an informative error message
func (reconciler *BrokerAppInstanceReconciler) buildCapacityError(
	rejections []ServiceRejection,
	appMemoryRequest *resource.Quantity,
) error {
	if len(rejections) == 0 {
		return fmt.Errorf("no services available")
	}

	// Count rejections by category to determine primary blocking issue
	categoryCounts := make(map[RejectionCategory]int)
	categoryServices := make(map[RejectionCategory][]string)
	for _, r := range rejections {
		categoryCounts[r.Category]++
		categoryServices[r.Category] = append(categoryServices[r.Category], r.ServiceName)
	}

	totalServices := len(rejections)

	// Special case: all services rejected due to selector issues (evaluation error or no match)
	selectorIssues := categoryCounts[RejectionSelector] + categoryCounts[RejectionSelectorError]
	if selectorIssues == totalServices {
		// If all are CEL errors, return specific condition
		if categoryCounts[RejectionSelectorError] == totalServices {
			// Extract first CEL error message for the condition
			var celErrMsg string
			for _, r := range rejections {
				if r.Category == RejectionSelectorError {
					celErrMsg = r.Message
					break
				}
			}
			return NewConditionError(broker.DeployedConditionSelectorEvaluationError,
				"no services match app selector: %s", celErrMsg)
		}
		return NewConditionError(broker.DeployedConditionNoMatchingServiceReason,
			"no services match app selector")
	}

	// Special case: all services rejected due to port pool exhaustion
	if categoryCounts[RejectionPortPool] == totalServices {
		return fmt.Errorf("all services have exhausted their port pools")
	}

	// Determine primary blocking issue based on priority
	// Priority: AddressRef > AddressClash > Memory > PortPool > Other
	var primaryMessage string

	switch {
	case categoryCounts[RejectionAddressRef] > 0:
		primaryMessage = "addressRef dependency not satisfied"

	case categoryCounts[RejectionAddressClash] > 0:
		primaryMessage = "address clash with existing apps"

	case categoryCounts[RejectionMemory] > 0:
		memoryStr := "unknown"
		if appMemoryRequest != nil && !appMemoryRequest.IsZero() {
			memoryStr = appMemoryRequest.String()
		}
		primaryMessage = fmt.Sprintf("insufficient memory capacity (app requires %s)", memoryStr)

	case categoryCounts[RejectionPortPool] > 0:
		primaryMessage = "port pool exhausted"

	default:
		primaryMessage = "other compatibility issues"
	}

	// Build comprehensive error message with all rejection details grouped by category
	var errMsg strings.Builder
	errMsg.WriteString(fmt.Sprintf("no service available: %s\n", primaryMessage))

	// Helper to format service list
	formatServices := func(services []string) string {
		if len(services) == 1 {
			return services[0]
		}
		return fmt.Sprintf("[%s]", strings.Join(services, ", "))
	}

	// Add details for each rejection category (in priority order)
	if len(categoryServices[RejectionAddressRef]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - AddressRef issues: %s\n",
			formatServices(categoryServices[RejectionAddressRef])))
		// Include specific messages for addressRef failures
		for _, r := range rejections {
			if r.Category == RejectionAddressRef {
				errMsg.WriteString(fmt.Sprintf("      %s: %s\n", r.ServiceName, r.Message))
			}
		}
	}

	if len(categoryServices[RejectionAddressClash]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - Address clashes: %s\n",
			formatServices(categoryServices[RejectionAddressClash])))
		for _, r := range rejections {
			if r.Category == RejectionAddressClash {
				errMsg.WriteString(fmt.Sprintf("      %s: %s\n", r.ServiceName, r.Message))
			}
		}
	}

	if len(categoryServices[RejectionMemory]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - Insufficient memory: %s\n",
			formatServices(categoryServices[RejectionMemory])))
	}

	if len(categoryServices[RejectionPortPool]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - Port pool exhausted: %s\n",
			formatServices(categoryServices[RejectionPortPool])))
	}

	if len(categoryServices[RejectionSelector]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - Selector mismatch: %s\n",
			formatServices(categoryServices[RejectionSelector])))
	}

	if len(categoryServices[RejectionNotDeployed]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - Not deployed: %s\n",
			formatServices(categoryServices[RejectionNotDeployed])))
	}

	if len(categoryServices[RejectionOther]) > 0 {
		errMsg.WriteString(fmt.Sprintf("  - Other issues: %s\n",
			formatServices(categoryServices[RejectionOther])))
		for _, r := range rejections {
			if r.Category == RejectionOther {
				errMsg.WriteString(fmt.Sprintf("      %s: %s\n", r.ServiceName, r.Message))
			}
		}
	}

	return fmt.Errorf("%s", strings.TrimSpace(errMsg.String()))
}

func (reconciler *BrokerAppInstanceReconciler) listOtherAppsForService(service *broker.BrokerService) ([]broker.BrokerApp, error) {
	apps := &broker.BrokerAppList{}
	key := serviceKey(service)
	if err := reconciler.Client.List(context.TODO(), apps, client.MatchingFields{common.AppServiceBindingField: key}); err != nil {
		return nil, err
	}

	// Filter out ourselves
	result := make([]broker.BrokerApp, 0, len(apps.Items))
	for _, app := range apps.Items {
		if app.Namespace == reconciler.instance.Namespace && app.Name == reconciler.instance.Name {
			continue
		}
		result = append(result, app)
	}
	return result, nil
}

func (reconciler *BrokerAppInstanceReconciler) getAvailableMemory(service *broker.BrokerService) (int64, error) {
	// Get service's total memory limit (0 if not specified means unlimited)
	serviceMemory := service.Spec.Resources.Limits.Memory()
	if serviceMemory == nil || serviceMemory.IsZero() {
		// No limit specified, treat as unlimited
		return int64(^uint64(0) >> 1), nil // max int64
	}
	totalMemory := serviceMemory.Value()

	// Find all other apps currently provisioned on this service
	apps, err := reconciler.listOtherAppsForService(service)
	if err != nil {
		return 0, err
	}

	// Sum up memory requests of all provisioned apps
	var usedMemory int64 = 0
	for _, app := range apps {
		appMemory := app.Spec.Resources.Requests.Memory()
		if appMemory != nil {
			usedMemory += appMemory.Value()
		}
	}

	available := totalMemory - usedMemory
	if available < 0 {
		available = 0
	}

	return available, nil
}

// checkAddressRefCapacity verifies that all cross-app addressRefs in this app's capabilities
// can be satisfied on the given service (referenced apps exist and are provisioned on same service).
// Returns an error with details if any addressRef cannot be satisfied.
// checkAddressClashOnService checks if this app's direct addresses conflict with
// apps already provisioned on the given service
func (reconciler *BrokerAppInstanceReconciler) checkAddressClashOnService(service *broker.BrokerService) error {
	myDirectAddresses := collectOwnedAddresses(reconciler.instance)

	// If this app doesn't use any direct addresses, no clash possible
	if len(myDirectAddresses) == 0 {
		return nil
	}

	// Get apps already provisioned on this service
	apps, listErr := reconciler.listOtherAppsForService(service)
	if listErr != nil {
		return fmt.Errorf("failed to list apps for clash detection: %v", listErr)
	}

	// Check each provisioned app for address conflicts
	for _, otherApp := range apps {
		otherAddresses := collectOwnedAddresses(&otherApp)

		// Check for clashes
		for myAddr := range myDirectAddresses {
			if otherAddresses[myAddr] {
				return fmt.Errorf("address '%s' already declared by %s/%s (use addressRef to share addresses)",
					myAddr, otherApp.Namespace, otherApp.Name)
			}
		}
	}

	return nil
}

// checkAddressRefCapacity validates that cross-app addressRefs can be satisfied by the referenced apps.
//
// IMPORTANT: Only addresses explicitly declared in spec.addresses can be shared.
// Addresses used implicitly in capabilities (without spec.addresses declaration) are private
// to that app and cannot be referenced by other apps. This prevents accidental address sharing.
//
// For example:
//
//	App A: capabilities.producerOf[{address: "orders"}]  -> PRIVATE (not in spec.addresses)
//	App B: capabilities.consumerOf[{address: "orders", appNamespace: "ns", appName: "A"}] -> REJECTED
//
//	App A: spec.addresses: ["orders"], capabilities.producerOf[{address: "orders"}] -> PUBLIC
//	App B: capabilities.consumerOf[{address: "orders", appNamespace: "ns", appName: "A"}] -> ALLOWED
func (reconciler *BrokerAppInstanceReconciler) checkAddressRefCapacity(service *broker.BrokerService) error {
	ctx := context.Background()

	for _, capability := range reconciler.instance.Spec.Capabilities {
		allAddresses := [][]broker.AddressRef{
			capability.ProducerOf,
			capability.ConsumerOf,
			capability.SubscriberOf,
		}

		for _, addrList := range allAddresses {
			for _, addressRef := range addrList {
				// Only check cross-app references (where appNamespace and appName are set)
				if addressRef.AppNamespace == "" || addressRef.AppName == "" {
					continue // local reference, no dependency check needed
				}

				// Look up the referenced app
				referencedApp := &broker.BrokerApp{}
				refKey := types.NamespacedName{
					Namespace: addressRef.AppNamespace,
					Name:      addressRef.AppName,
				}
				if getErr := reconciler.Client.Get(ctx, refKey, referencedApp); getErr != nil {
					if errors.IsNotFound(getErr) {
						return fmt.Errorf("referenced app %s/%s not found",
							addressRef.AppNamespace, addressRef.AppName)
					}
					return fmt.Errorf("failed to lookup referenced app %s/%s: %v",
						addressRef.AppNamespace, addressRef.AppName, getErr)
				}

				// Check if the referenced app is provisioned on this same service
				if referencedApp.Status.Service == nil {
					return fmt.Errorf("referenced app %s/%s not yet provisioned on any service",
						addressRef.AppNamespace, addressRef.AppName)
				}

				refServiceKey := referencedApp.Status.Service.Key()
				thisServiceKey := serviceKey(service)
				if refServiceKey != thisServiceKey {
					// Verify the referenced service still exists to provide better error message
					refService := &broker.BrokerService{}
					refServiceName := types.NamespacedName{
						Namespace: referencedApp.Status.Service.Namespace,
						Name:      referencedApp.Status.Service.Name,
					}
					if getErr := reconciler.Client.Get(ctx, refServiceName, refService); getErr != nil {
						if errors.IsNotFound(getErr) {
							return fmt.Errorf("referenced app %s/%s was provisioned on service %s which no longer exists",
								addressRef.AppNamespace, addressRef.AppName, refServiceKey)
						}
						return fmt.Errorf("failed to lookup service %s for referenced app %s/%s: %v",
							refServiceKey, addressRef.AppNamespace, addressRef.AppName, getErr)
					}
					return fmt.Errorf("referenced app %s/%s is provisioned on different service %s (this app would bind to: %s)",
						addressRef.AppNamespace, addressRef.AppName, refServiceKey, thisServiceKey)
				}

				// Extract base address from FQQN if present
				baseAddr := extractBaseAddress(addressRef.Address)

				// Check if the referenced app declares this address in spec.addresses
				addressDeclared := false
				for _, declaredAddrType := range referencedApp.Spec.Addresses {
					if declaredAddrType.Address == baseAddr {
						addressDeclared = true
						break
					}
				}

				if !addressDeclared {
					return fmt.Errorf("referenced app %s/%s does not declare address '%s'",
						addressRef.AppNamespace, addressRef.AppName, baseAddr)
				}
			}
		}
	}

	return nil
}

func (reconciler *BrokerAppInstanceReconciler) verifyCapabilityAddressType() (err error) {

	for _, capability := range reconciler.instance.Spec.Capabilities {
		// Validate all address refs in capabilities
		allAddresses := []struct {
			addresses []broker.AddressRef
			capType   string
		}{
			{capability.ProducerOf, "ProducerOf"},
			{capability.ConsumerOf, "ConsumerOf"},
			{capability.SubscriberOf, "SubscriberOf"},
		}

		for _, addrGroup := range allAddresses {
			for index, addressRef := range addrGroup.addresses {
				// Address field is required
				if addressRef.Address == "" {
					err = fmt.Errorf("Spec.Capability.%s[%d].address must be specified", addrGroup.capType, index)
					break
				}

				// Validate cross-app reference consistency
				// Both appNamespace and appName must be set together, or both must be empty
				hasNamespace := addressRef.AppNamespace != ""
				hasAppName := addressRef.AppName != ""

				if hasNamespace != hasAppName {
					err = fmt.Errorf("Spec.Capability.%s[%d]: appNamespace and appName must both be set (for cross-app reference) or both empty (for local reference)", addrGroup.capType, index)
					break
				}

				// SubscriberOf validation: must use FQQN format
				if addrGroup.capType == "SubscriberOf" {
					if !strings.Contains(addressRef.Address, FQQNSeparator) {
						err = fmt.Errorf("Spec.Capability.SubscriberOf[%d].address must use FQQN format (address::queue)", index)
						break
					}
					// Validate exactly one separator with non-empty parts
					parts := strings.SplitN(addressRef.Address, FQQNSeparator, 3)
					if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
						err = fmt.Errorf("Spec.Capability.SubscriberOf[%d].address must have valid FQQN format 'address::queue'", index)
						break
					}
				}
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		meta.SetStatusCondition(&reconciler.status.Conditions, metav1.Condition{
			Type:    broker.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  broker.ValidConditionAddressTypeError,
			Message: err.Error(),
		})
	}
	return err
}

// isAppRejectedByService checks if this app appears in the service's RejectedApps list
func (reconciler *BrokerAppInstanceReconciler) isAppRejectedByService(service *broker.BrokerService) bool {
	appKey := reconciler.instance.Namespace + "/" + reconciler.instance.Name
	for _, rejected := range service.Status.RejectedApps {
		if rejected.Namespace+"/"+rejected.Name == appKey {
			return true
		}
	}
	return false
}

func (reconciler *BrokerAppInstanceReconciler) processStatus(reconcilerError error) (err error, retry bool) {

	var deployedCondition metav1.Condition = metav1.Condition{
		Type: broker.DeployedConditionType,
	}
	if reconciler.status.Service != nil {
		serviceName := reconciler.status.Service.Key()

		deployedCondition.Status = metav1.ConditionFalse
		deployedCondition.Reason = broker.DeployedConditionProvisioningPendingReason
		deployedCondition.Message = "Waiting for broker to apply application properties"

		if reconciler.service != nil {
			appIdentity := AppIdentity(reconciler.instance)
			for _, appliedApp := range reconciler.service.Status.ProvisionedApps {
				if appliedApp == appIdentity {
					deployedCondition.Status = metav1.ConditionTrue
					deployedCondition.Reason = broker.DeployedConditionProvisionedReason
					deployedCondition.Message = ""
					break
				}
			}
		} else {
			deployedCondition.Status = metav1.ConditionFalse
			deployedCondition.Reason = broker.DeployedConditionMatchedServiceNotFoundReason
			deployedCondition.Message = fmt.Sprintf("matching service from status binding %s not found", serviceName)
		}

	} else if reconcilerError != nil {
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
	} else {
		// No binding and no error means we haven't tried to assign yet
		deployedCondition.Status = metav1.ConditionFalse
		deployedCondition.Reason = broker.DeployedConditionNoMatchingServiceReason
	}
	meta.SetStatusCondition(&reconciler.status.Conditions, deployedCondition)
	common.SetReadyCondition(&reconciler.status.Conditions)

	if !reflect.DeepEqual(reconciler.instance.Status, *reconciler.status) {
		reconciler.instance.Status = *reconciler.status
		err = resources.UpdateStatus(reconciler.Client, reconciler.instance)
	}
	retry = meta.IsStatusConditionTrue(reconciler.instance.Status.Conditions, broker.ValidConditionType) &&
		meta.IsStatusConditionFalse(reconciler.instance.Status.Conditions, broker.DeployedConditionType)
	return err, retry
}

func (r *BrokerAppReconciler) enqueueAppsForService() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		service := obj.(*broker.BrokerService)
		svcKey := serviceKey(service)

		// Find all BrokerApps that reference this service
		appList := &broker.BrokerAppList{}
		if err := r.Client.List(ctx, appList, client.MatchingFields{common.AppServiceBindingField: svcKey}); err != nil {
			r.log.Error(err, "Failed to list BrokerApps for service watch", "service", svcKey)
			return nil
		}

		requests := make([]reconcile.Request, 0, len(appList.Items))
		for _, app := range appList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: app.Namespace,
					Name:      app.Name,
				},
			})
		}
		return requests
	})
}

func (r *BrokerAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Note: Namespace informer is set up in main.go for CEL evaluation
	return ctrl.NewControllerManagedBy(mgr).
		For(&broker.BrokerApp{}).
		Owns(&corev1.Secret{}).
		Watches(&broker.BrokerService{}, r.enqueueAppsForService()).
		WithOptions(controller.Options{
			// capacity allocation requires serial processing
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}
