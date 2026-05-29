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

// isMulticastAddress determines if an address uses pubSub semantics
// based on explicit PubSub flag or implicit inference from Subscriptions presence
func isMulticastAddress(pubSub *bool, subscriptions []string) bool {
	// Explicitly disabled
	if pubSub != nil && !*pubSub {
		return false
	}
	// Explicitly enabled OR inferred from subscriptions presence
	return (pubSub != nil && *pubSub) || len(subscriptions) > 0
}

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
//  1. Explicitly declared in spec.sharedAddresses (shareable with other apps)
//  2. Explicitly declared in spec.addresses (private, not shareable)
//  3. Implicitly used in capabilities with empty appNamespace/appName (private, not shareable)
//
// Both types count as "owned" to prevent address clashes between apps on the same service,
// but only explicitly declared addresses (type 1) can be referenced by other apps via addressRef.
//
// This function is used for:
//   - Clash detection: prevent two apps from using the same address name
//   - AddressConfigurations generation: app needs broker config for all addresses it uses
//
// This function is NOT used for:
// collectOwnedAddresses returns all addresses owned by this app (lifecycle tied to this app).
// This is the union of:
//   - spec.addresses (private addresses - cannot be referenced by other apps)
//   - spec.sharedAddresses (public addresses - can be referenced by other apps)
//   - addresses declared inline in capabilities (local addresses only)
//
// Sharing validation: checkAddressRefCapacity() checks spec.sharedAddresses (only explicit shared addresses are referenceable)
func collectOwnedAddresses(app *broker.BrokerApp) map[string]bool {
	addresses := make(map[string]bool)

	// Add from spec.addresses (private)
	for _, addrType := range app.Spec.Addresses {
		addresses[addrType.Address] = true
	}

	// Add from spec.sharedAddresses (public)
	for _, addrType := range app.Spec.SharedAddresses {
		addresses[addrType.Address] = true
	}

	// Add from capabilities (local addresses only - where appNamespace and appName are empty)
	for _, capability := range app.Spec.Capabilities {
		// Handle ProducerOf and ConsumerOf
		allAddresses := [][]broker.AddressRef{
			capability.ProducerOf,
			capability.ConsumerOf,
		}

		for _, addrList := range allAddresses {
			for _, addressRef := range addrList {
				// Only count as owned if this is a local reference (no cross-app fields)
				if addressRef.AppNamespace == "" && addressRef.AppName == "" {
					baseAddr := extractBaseAddress(addressRef.Address)
					addresses[baseAddr] = true
				}
				// Note: subscriptions array contains queue names, not addresses
				// The address itself is owned, not the queue names
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
	if err := ValidateResourceName(reconciler.instance.Name); err != nil {
		return err
	}

	// Validate capability address types (structural checks only)
	if err := reconciler.verifyCapabilityAddressType(); err != nil {
		return err
	}

	// Validate that Addresses and SharedAddresses don't overlap
	if err := reconciler.validateAddressesDisjoint(); err != nil {
		return err
	}

	// Validate that declared addresses match their usage in capabilities
	return reconciler.validateAddressCapabilityConsistency()
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

	// Update status with conditions
	statusErr := processor.processStatus(err)
	reqLogger.V(2).Info("Reconciler Processed...", "CRD.Name", instance.Name, "CRD ver", instance.ObjectMeta.ResourceVersion, "CRD Gen", instance.ObjectMeta.Generation, "error", err)

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
	// Success
	return ctrl.Result{}, nil
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
		return NewValidationError(
			broker.ValidConditionSpecSelectorError,
			"failed to evaluate Spec.Selector: %v", err)
	}
	err = reconciler.Client.List(context.TODO(), list, &client.ListOptions{LabelSelector: opts})
	if err != nil {
		// API error, not a CR validation issue - wrap as TransientError
		return NewTransientErrorWithCause(
			broker.DeployedConditionCrudKindErrorReason,
			"failed to list BrokerServices",
			err)
	}

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

		// if we found the service and if it still matches, is it still valid
		// the findservice with capactity does the same checks, here we use them to validate the current service
		// which avoids choosing a different service unless really necessary
		// REVISIT: can this logic be combined in a single place?
		if service != nil {
			// Check if app is in service's RejectedApps
			if reconciler.isAppRejectedByService(service) {
				reconciler.log.V(1).Info("App rejected by service, reassigning",
					"app", reconciler.instance.Name,
					"service", deployedTo,
					"currentPort", reconciler.status.Service.AssignedPort)
				reconciler.status.Service = nil
				service = nil
				needsServiceAssignment = true
			}

			if service != nil {
				matches, matchErr := reconciler.matchesServiceSelector(service)
				if !matches || matchErr != nil {
					reconciler.log.V(1).Info("App no longer matches service selector, removing binding",
						"app", reconciler.instance.Name,
						"service", deployedTo,
						"error", matchErr,
					)
					reconciler.status.Service = nil
					service = nil
					needsServiceAssignment = true
				}
			}

			// Check if addressRef dependencies are still valid
			if service != nil {
				if addrRefErr := reconciler.checkAddressRefCapacity(service); addrRefErr != nil {
					reconciler.log.V(1).Info("AddressRef dependencies no longer valid, reassigning",
						"app", reconciler.instance.Name,
						"service", deployedTo,
						"error", addrRefErr)
					reconciler.status.Service = nil
					service = nil
					needsServiceAssignment = true
				}

				// Check for address clashes with apps already on this service
				if service != nil {
					if clashErr := reconciler.checkAddressClashOnService(service); clashErr != nil {
						reconciler.log.V(1).Info("address clash detected, reassigning",
							"app", reconciler.instance.Name,
							"service", deployedTo,
							"error", clashErr)
						reconciler.status.Service = nil
						service = nil
						needsServiceAssignment = true
					}
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
			return NewTransientError(
				broker.DeployedConditionNoMatchingServiceReason,
				fmt.Sprintf("no matching services available for selector %v", opts))
		}

		var assignedPort int32
		service, assignedPort, err = reconciler.findServiceWithCapacity(list)
		if err != nil {
			// If findServiceWithCapacity returned a TransientError, preserve it
			if _, isTransient := err.(*TransientError); !isTransient {
				// Otherwise wrap with NoServiceCapacity
				err = NewTransientError(
					broker.DeployedConditionNoServiceCapacityReason,
					fmt.Sprintf("no service with capacity available for selector %v, %v", opts, err))
			}
		}
		if service != nil {
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
				"port", assignedPort)
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
		candidatePort, portErr := assignNextAvailablePort(usedPorts)
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
			return NewTransientError(
				broker.DeployedConditionSelectorEvaluationError,
				fmt.Sprintf("no services match app selector: %s", celErrMsg))
		}
		return NewTransientError(
			broker.DeployedConditionNoMatchingServiceReason,
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
// IMPORTANT: Only addresses explicitly declared in spec.sharedAddresses can be referenced by other apps.
// Both spec.addresses and spec.sharedAddresses define owned addresses (lifecycle tied to the app),
// but only sharedAddresses grant sharing permission.
//
// For example:
//
//	App A: addresses: ["orders"], capabilities.producerOf[{address: "orders"}]  -> PRIVATE
//	App B: capabilities.consumerOf[{address: "orders", appNamespace: "ns", appName: "A"}] -> REJECTED
//
//	App A: sharedAddresses: ["orders"], capabilities.producerOf[{address: "orders"}] -> PUBLIC
//	App B: capabilities.consumerOf[{address: "orders", appNamespace: "ns", appName: "A"}] -> ALLOWED
func (reconciler *BrokerAppInstanceReconciler) checkAddressRefCapacity(service *broker.BrokerService) error {
	ctx := context.Background()

	for _, capability := range reconciler.instance.Spec.Capabilities {
		// Check ProducerOf and ConsumerOf cross-app references
		allAddresses := [][]broker.AddressRef{
			capability.ProducerOf,
			capability.ConsumerOf,
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
				//baseAddr := extractBaseAddress(addressRef.Address)

				// Check if the referenced app declares this address in spec.sharedAddresses
				addressShared := false
				var ownerIsMulticast = false
				for _, sharedAddrType := range referencedApp.Spec.SharedAddresses {
					if sharedAddrType.Address == addressRef.Address {
						addressShared = true
						ownerIsMulticast = isMulticastAddress(sharedAddrType.PubSub, sharedAddrType.Subscriptions)
						break
					}
				}

				if !addressShared {
					return fmt.Errorf("referenced app %s/%s does not share address '%s' (add to spec.sharedAddresses)",
						addressRef.AppNamespace, addressRef.AppName, addressRef.Address)
				}

				// Check for routing type conflict
				currentIsMulticast := isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions)

				// Detect conflict
				if currentIsMulticast && !ownerIsMulticast {
					return fmt.Errorf(
						"referenced app %s/%s declares address '%s' without pubSub, "+
							"but this app uses it with pubSub. "+
							"Shared addresses must use consistent semantics across all apps",
						addressRef.AppNamespace, addressRef.AppName, addressRef.Address)
				}
				if !currentIsMulticast && ownerIsMulticast {
					return fmt.Errorf(
						"referenced app %s/%s declares address '%s' with pubSub, "+
							"but this app references it without pubSub. "+
							"Shared addresses must use consistent semantics across all apps",
						addressRef.AppNamespace, addressRef.AppName, addressRef.Address)
				}
			}
		}
	}

	return nil
}

func (reconciler *BrokerAppInstanceReconciler) verifyCapabilityAddressType() (err error) {

	for _, capability := range reconciler.instance.Spec.Capabilities {
		// Validate ProducerOf
		for index, addressRef := range capability.ProducerOf {
			// Address field is required
			if addressRef.Address == "" {
				err = fmt.Errorf("Spec.Capability.ProducerOf[%d].address must be specified", index)
				break
			}

			// Address should NOT use FQQN format - FQQN is generated internally
			if strings.Contains(addressRef.Address, FQQNSeparator) {
				err = fmt.Errorf("Spec.Capability.ProducerOf[%d].address should not use FQQN format (no '::'). Use plain address name", index)
				break
			}

			// Validate cross-app reference consistency
			hasNamespace := addressRef.AppNamespace != ""
			hasAppName := addressRef.AppName != ""
			if hasNamespace != hasAppName {
				err = fmt.Errorf("Spec.Capability.ProducerOf[%d]: appNamespace and appName must both be set (for cross-app reference) or both empty (for local reference)", index)
				break
			}

			// Validate pubSub and subscriptions consistency
			if addressRef.PubSub != nil && !*addressRef.PubSub && len(addressRef.Subscriptions) > 0 {
				err = fmt.Errorf("Spec.Capability.ProducerOf[%d]: pubSub cannot be false when subscriptions are specified", index)
				break
			}

			// ProducerOf with pubSub: can declare intent but cannot create queues
			if isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
				if len(addressRef.Subscriptions) > 0 {
					err = fmt.Errorf("Spec.Capability.ProducerOf[%d]: subscriptions cannot contain queue names (producers don't create queues). Use empty array or omit", index)
					break
				}
			}
		}
		if err != nil {
			break
		}

		// Validate ConsumerOf
		for index, addressRef := range capability.ConsumerOf {
			// Address field is required
			if addressRef.Address == "" {
				err = fmt.Errorf("Spec.Capability.ConsumerOf[%d].address must be specified", index)
				break
			}

			// Address should NOT use FQQN format - FQQN is generated internally
			if strings.Contains(addressRef.Address, FQQNSeparator) {
				err = fmt.Errorf("Spec.Capability.ConsumerOf[%d].address should not use FQQN format (no '::'). Use plain address name", index)
				break
			}

			// Validate cross-app reference consistency
			hasNamespace := addressRef.AppNamespace != ""
			hasAppName := addressRef.AppName != ""
			if hasNamespace != hasAppName {
				err = fmt.Errorf("Spec.Capability.ConsumerOf[%d]: appNamespace and appName must both be set (for cross-app reference) or both empty (for local reference)", index)
				break
			}

			// Validate pubSub and subscriptions consistency
			if addressRef.PubSub != nil && !*addressRef.PubSub && len(addressRef.Subscriptions) > 0 {
				err = fmt.Errorf("Spec.Capability.ConsumerOf[%d]: pubSub cannot be false when subscriptions are specified", index)
				break
			}

			// ConsumerOf with pubSub: must specify at least one queue name
			if isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
				if len(addressRef.Subscriptions) == 0 {
					err = fmt.Errorf("Spec.Capability.ConsumerOf[%d]: pubSub consumers must specify at least one subscription queue name", index)
					break
				}

				// Validate multicast queue names
				for subIdx, queueName := range addressRef.Subscriptions {
					if queueName == "" {
						err = fmt.Errorf("Spec.Capability.ConsumerOf[%d].subscriptions[%d]: queue name cannot be empty", index, subIdx)
						break
					}

					// Queue names should NOT use FQQN format
					if strings.Contains(queueName, FQQNSeparator) {
						err = fmt.Errorf("Spec.Capability.ConsumerOf[%d].subscriptions[%d]: queue name should not use FQQN format (no '::')", index, subIdx)
						break
					}
				}
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			break
		}

		// Validate routing type consistency within this capability
		// Check for same-app conflicts: address used with consistent semantics
		addressRoutingTypes := make(map[string]bool) // address -> isMulticast

		// Check ProducerOf addresses
		for _, addressRef := range capability.ProducerOf {
			if addressRef.AppNamespace == "" && addressRef.AppName == "" {
				if isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions) {
					// Explicitly MULTICAST (pubSub=true or has subscriptions)
					if prevType, exists := addressRoutingTypes[addressRef.Address]; exists && !prevType {
						err = fmt.Errorf(
							"address '%s' is referenced with both pubSub and non pubSub semantics in the same capability. "+
								"Shared addresses must use consistent semantics",
							addressRef.Address)
						break
					}
					addressRoutingTypes[addressRef.Address] = true
				}
				// Not multicast in ProducerOf means routing type not specified by producer
			}
		}
		if err != nil {
			break
		}

		// Check ConsumerOf addresses
		for _, addressRef := range capability.ConsumerOf {
			if addressRef.AppNamespace == "" && addressRef.AppName == "" {
				isMulticast := isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions)

				if prevType, exists := addressRoutingTypes[addressRef.Address]; exists {
					if prevType != isMulticast {
						err = fmt.Errorf(
							"address '%s' is referenced with both pubSub and non pubSub semantics in the same capability. "+
								"Shared addresses must use consistent semantics",
							addressRef.Address)
						break
					}
				}
				addressRoutingTypes[addressRef.Address] = isMulticast
			}
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		return NewValidationError(broker.ValidConditionAddressTypeError, "%v", err)
	}
	return nil
}

// validateAddressesDisjoint ensures Addresses and SharedAddresses don't overlap.
// An address cannot be both private (Addresses) and public (SharedAddresses).
func (reconciler *BrokerAppInstanceReconciler) validateAddressesDisjoint() error {
	if len(reconciler.instance.Spec.Addresses) == 0 || len(reconciler.instance.Spec.SharedAddresses) == 0 {
		return nil // No overlap possible
	}

	// Build a set from Addresses
	privateAddresses := make(map[string]bool)
	for _, addr := range reconciler.instance.Spec.Addresses {
		privateAddresses[addr.Address] = true
	}

	// Check for overlap with SharedAddresses
	for _, sharedAddr := range reconciler.instance.Spec.SharedAddresses {
		if privateAddresses[sharedAddr.Address] {
			return NewValidationError(
				broker.ValidConditionAddressTypeError,
				"address '%s' appears in both spec.addresses and spec.sharedAddresses (cannot be both private and public)",
				sharedAddr.Address)
		}
	}

	return nil
}

// validateAddressCapabilityConsistency ensures that addresses declared in spec.addresses
// or spec.sharedAddresses are used consistently in capabilities (same routing type).
func (reconciler *BrokerAppInstanceReconciler) validateAddressCapabilityConsistency() error {
	// Build a map of declared addresses with their routing types
	declaredAddresses := make(map[string]bool) // address -> isMulticast

	// Collect from spec.addresses (private)
	for _, addrType := range reconciler.instance.Spec.Addresses {
		isMulticast := isMulticastAddress(addrType.PubSub, addrType.Subscriptions)
		declaredAddresses[addrType.Address] = isMulticast
	}

	// Collect from spec.sharedAddresses (public)
	for _, addrType := range reconciler.instance.Spec.SharedAddresses {
		isMulticast := isMulticastAddress(addrType.PubSub, addrType.Subscriptions)
		declaredAddresses[addrType.Address] = isMulticast
	}

	// If no declared addresses, nothing to validate
	if len(declaredAddresses) == 0 {
		return nil
	}

	// Check all capability references to local addresses (appNamespace/appName empty)
	for _, capability := range reconciler.instance.Spec.Capabilities {
		// Check ProducerOf
		for _, addressRef := range capability.ProducerOf {
			// Only check local addresses (not cross-app references)
			if addressRef.AppNamespace == "" && addressRef.AppName == "" {
				// Check if this address was declared
				if declaredIsMulticast, isDeclared := declaredAddresses[addressRef.Address]; isDeclared {
					// Check if usage matches declaration
					usageIsMulticast := isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions)

					if declaredIsMulticast && !usageIsMulticast {
						return NewValidationError(
							broker.ValidConditionAddressTypeError,
							"address '%s' is declared with pubSub semantics in spec.addresses or spec.sharedAddresses, "+
								"but is used without pubSub semantics in capabilities.producerOf. "+
								"Address declaration and usage must have consistent routing semantics",
							addressRef.Address)
					}

					if !declaredIsMulticast && usageIsMulticast {
						return NewValidationError(
							broker.ValidConditionAddressTypeError,
							"address '%s' is declared without pubSub semantics in spec.addresses or spec.sharedAddresses, "+
								"but is used with pubSub semantics in capabilities.producerOf. "+
								"Address declaration and usage must have consistent routing semantics",
							addressRef.Address)
					}
				}
			}
		}

		// Check ConsumerOf
		for _, addressRef := range capability.ConsumerOf {
			// Only check local addresses (not cross-app references)
			if addressRef.AppNamespace == "" && addressRef.AppName == "" {
				// Check if this address was declared
				if declaredIsMulticast, isDeclared := declaredAddresses[addressRef.Address]; isDeclared {
					// Check if usage matches declaration
					usageIsMulticast := isMulticastAddress(addressRef.PubSub, addressRef.Subscriptions)

					if declaredIsMulticast && !usageIsMulticast {
						return NewValidationError(
							broker.ValidConditionAddressTypeError,
							"address '%s' is declared with pubSub semantics in spec.addresses or spec.sharedAddresses, "+
								"but is used without pubSub semantics in capabilities.consumerOf. "+
								"Address declaration and usage must have consistent routing semantics",
							addressRef.Address)
					}

					if !declaredIsMulticast && usageIsMulticast {
						return NewValidationError(
							broker.ValidConditionAddressTypeError,
							"address '%s' is declared without pubSub semantics in spec.addresses or spec.sharedAddresses, "+
								"but is used with pubSub semantics in capabilities.consumerOf. "+
								"Address declaration and usage must have consistent routing semantics",
							addressRef.Address)
					}
				}
			}
		}
	}

	return nil
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

func (reconciler *BrokerAppInstanceReconciler) processStatus(reconcilerError error) error {
	// Set Valid condition (always updated with current generation)
	reconciler.setValidCondition(reconcilerError)

	// Set Deployed condition (only updated when validation passes)
	reconciler.setDeployedCondition(reconcilerError)

	// Set Ready condition (always reflects current generation)
	reconciler.setReadyCondition()

	// Update status-level observedGeneration
	reconciler.status.ObservedGeneration = reconciler.instance.Generation

	// Update status if changed
	if !reflect.DeepEqual(reconciler.instance.Status, *reconciler.status) {
		reconciler.instance.Status = *reconciler.status
		return resources.UpdateStatus(reconciler.Client, reconciler.instance)
	}

	return nil
}

func (reconciler *BrokerAppInstanceReconciler) setValidCondition(err error) {
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

		// Add note if app is already deployed on previous generation
		deployedCond := meta.FindStatusCondition(reconciler.status.Conditions, broker.DeployedConditionType)
		if deployedCond != nil && deployedCond.Status == metav1.ConditionTrue &&
			deployedCond.ObservedGeneration < reconciler.instance.Generation {
			condition.Message += fmt.Sprintf(" (reconcile blocked, app continues running at generation %d)",
				deployedCond.ObservedGeneration)
		}
	}

	meta.SetStatusCondition(&reconciler.status.Conditions, condition)
}

func (reconciler *BrokerAppInstanceReconciler) setDeployedCondition(err error) {
	// If validation failed, check if we need to initialize Deployed condition
	if _, isValidationErr := err.(*ValidationError); isValidationErr {
		// Check if Deployed condition exists
		existing := meta.FindStatusCondition(reconciler.status.Conditions, broker.DeployedConditionType)
		if existing != nil {
			// Deployed condition exists - don't update it
			// Keeps old observedGeneration to show old spec still running
			return
		}

		// No existing Deployed condition - create one for new apps
		// Set with observedGeneration=0 to indicate never attempted deployment
		condition := metav1.Condition{
			Type:               broker.DeployedConditionType,
			Status:             metav1.ConditionFalse,
			Reason:             broker.DeployedConditionValidationFailedReason,
			Message:            "Cannot deploy due to validation failure",
			ObservedGeneration: 0, // 0 indicates no deployment attempted
		}
		meta.SetStatusCondition(&reconciler.status.Conditions, condition)
		return
	}

	// Validation passed, we attempted deployment - update with current generation
	condition := metav1.Condition{
		Type:               broker.DeployedConditionType,
		ObservedGeneration: reconciler.instance.Generation,
	}

	// Determine deployment result
	if transErr, ok := err.(*TransientError); ok {
		// Transient error (capacity, routing, API error)
		condition.Status = metav1.ConditionFalse
		condition.Reason = transErr.ConditionReason()
		condition.Message = transErr.Error()
	} else if err != nil {
		// Other error
		condition.Status = metav1.ConditionFalse
		condition.Reason = broker.DeployedConditionCrudKindErrorReason
		condition.Message = err.Error()
	} else if reconciler.status.Service != nil && reconciler.service != nil {
		// Check if actually deployed
		appIdentity := AppIdentity(reconciler.instance)
		isProvisioned := false
		for _, appliedApp := range reconciler.service.Status.ProvisionedApps {
			if appliedApp == appIdentity {
				isProvisioned = true
				break
			}
		}

		if isProvisioned {
			condition.Status = metav1.ConditionTrue
			condition.Reason = broker.DeployedConditionProvisionedReason
			condition.Message = "Application provisioned to broker"
		} else {
			condition.Status = metav1.ConditionFalse
			condition.Reason = broker.DeployedConditionProvisioningPendingReason
			condition.Message = "Waiting for broker to provision"
		}
	} else if reconciler.status.Service != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = broker.DeployedConditionMatchedServiceNotFoundReason
		condition.Message = fmt.Sprintf("matching service %s not found", reconciler.status.Service.Key())
	} else {
		// No matching service
		condition.Status = metav1.ConditionFalse
		condition.Reason = broker.DeployedConditionNoMatchingServiceReason
		condition.Message = "No matching BrokerService found"
	}

	meta.SetStatusCondition(&reconciler.status.Conditions, condition)
}

func (reconciler *BrokerAppInstanceReconciler) setReadyCondition() {
	condition := metav1.Condition{
		Type:               broker.ReadyConditionType,
		ObservedGeneration: reconciler.instance.Generation,
	}

	validCond := meta.FindStatusCondition(reconciler.status.Conditions, broker.ValidConditionType)
	deployedCond := meta.FindStatusCondition(reconciler.status.Conditions, broker.DeployedConditionType)

	// Ready = Valid AND Deployed, both at current generation
	if validCond != nil && validCond.Status == metav1.ConditionTrue &&
		validCond.ObservedGeneration == reconciler.instance.Generation &&
		deployedCond != nil && deployedCond.Status == metav1.ConditionTrue &&
		deployedCond.ObservedGeneration == reconciler.instance.Generation {
		condition.Status = metav1.ConditionTrue
		condition.Reason = broker.ReadyConditionReason
		condition.Message = "BrokerApp is ready"
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = broker.NotReadyConditionReason
		condition.Message = "Waiting for Valid and Deployed conditions at current generation"
	}

	meta.SetStatusCondition(&reconciler.status.Conditions, condition)
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

// shouldPropagateWatchForReferencedApp determines if watch events for a referenced app
// should propagate to consumer apps. Only propagates when:
// 1. App is being deleted (consumers need to unbind)
// 2. App is Valid=True AND Deployed=True (safe to reference)
func shouldPropagateWatchForReferencedApp(app *broker.BrokerApp) bool {
	isDeleted := app.DeletionTimestamp != nil
	if isDeleted {
		return true
	}

	validCond := meta.FindStatusCondition(app.Status.Conditions, broker.ValidConditionType)
	deployedCond := meta.FindStatusCondition(app.Status.Conditions, broker.DeployedConditionType)

	// Only propagate when current generation is both valid and deployed
	// This prevents reconciling cross-app references on stale or invalid configurations
	isValidAndDeployedAtCurrentGen := validCond != nil &&
		validCond.Status == metav1.ConditionTrue &&
		validCond.ObservedGeneration == app.Generation &&
		deployedCond != nil &&
		deployedCond.Status == metav1.ConditionTrue &&
		deployedCond.ObservedGeneration == app.Generation

	return isValidAndDeployedAtCurrentGen
}

func (r *BrokerAppReconciler) enqueueAppsForReferencedApp() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		changedApp := obj.(*broker.BrokerApp)
		changedKey := types.NamespacedName{
			Namespace: changedApp.Namespace,
			Name:      changedApp.Name,
		}

		// Only propagate watch events when appropriate
		if !shouldPropagateWatchForReferencedApp(changedApp) {
			// App is in an intermediate/invalid state - don't propagate to consumers
			// When it becomes valid again, another watch event will fire
			return nil
		}

		// Find all BrokerApps that reference this app via addressRef
		appList := &broker.BrokerAppList{}
		if err := r.Client.List(ctx, appList); err != nil {
			r.log.Error(err, "Failed to list BrokerApps for addressRef watch", "app", changedKey)
			return nil
		}

		requests := make([]reconcile.Request, 0)
		for _, app := range appList.Items {
			// Skip the changed app itself
			if app.Namespace == changedApp.Namespace && app.Name == changedApp.Name {
				continue
			}

			// Check if this app references the changed app
			if hasAddressRefTo(&app, changedKey) {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: app.Namespace,
						Name:      app.Name,
					},
				})
			}
		}
		return requests
	})
}

// hasAddressRefTo checks if app has any addressRef pointing to targetApp
func hasAddressRefTo(app *broker.BrokerApp, targetApp types.NamespacedName) bool {
	for _, capability := range app.Spec.Capabilities {
		for _, addressRef := range capability.ProducerOf {
			if addressRef.AppNamespace == targetApp.Namespace && addressRef.AppName == targetApp.Name {
				return true
			}
		}
		for _, addressRef := range capability.ConsumerOf {
			if addressRef.AppNamespace == targetApp.Namespace && addressRef.AppName == targetApp.Name {
				return true
			}
		}
	}
	return false
}

func (r *BrokerAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Note: Namespace informer is set up in main.go for CEL evaluation
	return ctrl.NewControllerManagedBy(mgr).
		For(&broker.BrokerApp{}).
		Owns(&corev1.Secret{}).
		Watches(&broker.BrokerService{}, r.enqueueAppsForService()).
		Watches(&broker.BrokerApp{}, r.enqueueAppsForReferencedApp()).
		WithOptions(controller.Options{
			// capacity allocation requires serial processing
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}
