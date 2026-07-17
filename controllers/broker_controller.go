// Package controllers implements Kubernetes controllers for broker resources.
package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"sort"

	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	brokerstatus "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/broker/status"
	brokerversion "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/broker/version"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/containers"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/persistentvolumeclaims"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/pods"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/serviceports"
	ss "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/statefulsets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	ctrl "sigs.k8s.io/controller-runtime"

	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/volumes"

	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"

	"strconv"
	"strings"

	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

// BrokerReconciler reconciles a Broker object (broker.arkmq.org/v1beta2)
type BrokerReconciler struct {
	rtclient.Client
	Scheme        *runtime.Scheme
	log           logr.Logger
	isOnOpenShift bool
}

func NewBrokerReconciler(cluster cluster.Cluster, logger logr.Logger, isOpenShift bool) *BrokerReconciler {
	return &BrokerReconciler{
		isOnOpenShift: isOpenShift,
		Client:        cluster.GetClient(),
		Scheme:        cluster.GetScheme(),
		log:           logger,
	}
}

//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokers/finalizers,verbs=update

func (r *BrokerReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "Broker")

	customResource := &v1beta2.Broker{}
	namespacedName := request.NamespacedName

	result := ctrl.Result{}

	err := r.Get(ctx, namespacedName, customResource)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			reqLogger.V(1).Info("Broker Controller Reconcile encountered a IsNotFound, for request NamespacedName " + namespacedName.String())
			return result, nil
		}
		reqLogger.Error(err, "unable to retrieve the Broker")
		return result, err
	}

	var reconcileBlocked = false
	if val, present := customResource.Annotations[common.BlockReconcileAnnotation]; present {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			reconcileBlocked = boolVal
		}
	}

	namer := MakeNamersForBroker(customResource)
	reconciler := NewBrokerReconcilerImpl(customResource, r)

	valid, requeueRequest := reconciler.validate(customResource, r.Client)
	if valid {

		if !reconcileBlocked {
			err = reconciler.Process(customResource, *namer, r.Client, r.Scheme)
		}
		if reconciler.ProcessBrokerStatus(customResource, r.Client, r.Scheme) {
			requeueRequest = true
		}
	}

	brokerstatus.UpdateBlockedStatus(customResource, reconcileBlocked)
	brokerstatus.ProcessStatus(customResource, r.Client, namespacedName, *namer, err)

	crStatusUpdateErr := r.UpdateBrokerCRStatus(customResource, r.Client, namespacedName)
	if crStatusUpdateErr != nil {
		requeueRequest = true
	}

	if !requeueRequest && !reconcileBlocked && hasExtraMountsForBroker(customResource) {
		reqLogger.V(1).Info("resource has extraMounts, requeuing")
		requeueRequest = true
	}

	if requeueRequest {
		reqLogger.V(1).Info("requeue reconcile")
		result = ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}
	}

	if valid && err == nil && crStatusUpdateErr == nil {
		reqLogger.V(1).Info("resource successfully reconciled")
	}
	return result, err
}

func (r *BrokerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta2.Broker{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&netv1.Ingress{}).
		Owns(&policyv1.PodDisruptionBudget{})

	if r.isOnOpenShift {
		builder.Owns(&routev1.Route{})
	}

	return builder.Complete(r)
}

func (r *BrokerReconciler) UpdateBrokerCRStatus(desired *v1beta2.Broker, client rtclient.Client, namespacedName types.NamespacedName) error {

	common.SetReadyCondition(&desired.Status.Conditions)

	current := &v1beta2.Broker{}

	err := client.Get(context.TODO(), namespacedName, current)
	if err != nil {
		r.log.Error(err, "unable to retrieve current resource", "Broker", namespacedName)
		return err
	}

	if !EqualBrokerCRStatus(&desired.Status, &current.Status) {
		r.log.V(1).Info("cr.status update", "Namespace", desired.Namespace, "Name", desired.Name, "Observed status", desired.Status)
		return resources.UpdateStatus(client, desired)
	}

	return nil
}

func EqualBrokerCRStatus(s1, s2 *v1beta2.BrokerStatus) bool {
	if s1.DeploymentPlanSize != s2.DeploymentPlanSize ||
		s1.ScaleLabelSelector != s2.ScaleLabelSelector ||
		!reflect.DeepEqual(s1.Version, s2.Version) ||
		len(s2.ExternalConfigs) != len(s1.ExternalConfigs) ||
		brokerExternalConfigsModified(s2.ExternalConfigs, s1.ExternalConfigs) ||
		!reflect.DeepEqual(s1.PodStatus, s2.PodStatus) ||
		len(s1.Conditions) != len(s2.Conditions) ||
		conditionsModified(s2.Conditions, s1.Conditions) {

		return false
	}

	return true
}

func brokerExternalConfigsModified(desiredExternalConfigs []v1beta2.ExternalConfigStatus, currentExternalConfigs []v1beta2.ExternalConfigStatus) bool {
	if len(desiredExternalConfigs) >= 0 {
		for _, cfg := range desiredExternalConfigs {
			for _, curCfg := range currentExternalConfigs {
				if curCfg.Name == cfg.Name && curCfg.ResourceVersion != cfg.ResourceVersion {
					return true
				}
			}
		}
	}
	return false
}

// the helper script looks for "/amq/scripts/post-config.sh"
// and run it if exists.

// default ApplyRule for address-settings

type BrokerReconcilerImpl struct {
	requestedResources map[reflect.Type]map[string]rtclient.Object
	deployed           map[reflect.Type][]rtclient.Object
	log                logr.Logger
	customResource     *v1beta2.Broker
	scheme             *runtime.Scheme
	isOnOpenShift      bool
	jolokiaEndpoints   []*jolokia_client.JkInfo
	cachedBrokerStatus map[string]any
	matchedTemplates   map[int]bool
}

func countOfRequestedBroker(reconciler *BrokerReconcilerImpl) (total int) {
	for _, v := range reconciler.requestedResources {
		total += len(v)
	}
	return total
}

func countOfDeployedBroker(reconciler *BrokerReconcilerImpl) (total int) {
	for _, v := range reconciler.deployed {
		total += len(v)
	}
	return total
}

func NewBrokerReconcilerImpl(customResource *v1beta2.Broker, parent *BrokerReconciler) *BrokerReconcilerImpl {
	return &BrokerReconcilerImpl{
		log:                parent.log,
		customResource:     customResource,
		scheme:             parent.Scheme,
		requestedResources: make(map[reflect.Type]map[string]rtclient.Object),
		isOnOpenShift:      parent.isOnOpenShift,
		cachedBrokerStatus: make(map[string]any),
		matchedTemplates:   make(map[int]bool),
	}
}

func (reconciler *BrokerReconcilerImpl) Process(customResource *v1beta2.Broker, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme) error {

	reconciler.log.V(1).Info("Reconciler Processing...", "Operator version", version.Version, "ActiveMQArtemis release", customResource.Spec.Version)
	reconciler.log.V(2).Info("Reconciler Processing...", "CRD.Name", customResource.Name, "CRD ver", customResource.ResourceVersion, "CRD Gen", customResource.Generation)

	reconciler.CurrentDeployedResources(customResource, client)

	// currentStateful Set is a clone of what exists if already deployed
	// what follows should transform the resources using the crd
	// if the transformation results in some change, process resources will respect that
	// comparisons should not be necessary, leave that to process resources
	desiredStatefulSet, err := reconciler.ProcessStatefulSet(customResource, namer, client)
	if err != nil {
		//reconciler.log.Error(err, "Error processing stafulset")
		return fmt.Errorf("failed to process stateful set, %w", err)
	}

	reconciler.ProcessDeploymentPlan(customResource, namer, client, scheme, desiredStatefulSet)

	// mods to env var values sourced from secrets are not detected by process resources
	// track updates in trigger env var that has a total checksum
	trackSecretCheckSumInEnvVar(common.ToResourceList(reconciler.requestedResources), desiredStatefulSet.Spec.Template.Spec.Containers)

	reconciler.trackDesired(desiredStatefulSet)

	// this will apply any deltas/updates
	err = reconciler.ProcessResources(customResource, client, scheme)

	if err != nil {
		reconciler.log.Error(err, "error processing resources")
	}

	reconciler.log.V(1).Info("Reconciler Processing... complete", "CRD ver:", customResource.ResourceVersion, "CRD Gen:", customResource.Generation)

	// we dont't requeue
	return err
}

func (reconciler *BrokerReconcilerImpl) cloneOfDeployed(kind reflect.Type, name string) rtclient.Object {
	obj := reconciler.getFromDeployed(kind, name)
	if obj != nil {
		return obj.DeepCopyObject().(rtclient.Object)
	}
	return nil
}

func (reconciler *BrokerReconcilerImpl) getFromDeployed(kind reflect.Type, name string) rtclient.Object {
	for _, obj := range reconciler.deployed[kind] {
		if obj.GetName() == name {
			return obj
		}
	}
	return nil
}

func (reconciler *BrokerReconcilerImpl) ProcessStatefulSet(customResource *v1beta2.Broker, namer common.Namers, client rtclient.Client) (*appsv1.StatefulSet, error) {

	reqLogger := reconciler.log.WithName(customResource.Name)

	ssNamespacedName := types.NamespacedName{
		Namespace: customResource.Namespace,
		Name:      namer.SsNameBuilder.Name(),
	}

	var err error
	var currentStatefulSet *appsv1.StatefulSet
	obj := reconciler.cloneOfDeployed(reflect.TypeFor[appsv1.StatefulSet](), ssNamespacedName.Name)
	if obj != nil {
		currentStatefulSet = obj.(*appsv1.StatefulSet)
	}

	reqLogger.V(2).Info("Reconciling desired statefulset", "name", ssNamespacedName, "current", currentStatefulSet)
	currentStatefulSet, err = reconciler.StatefulSetForCR(customResource, namer, currentStatefulSet, client)
	if err != nil {
		//reqLogger.Error(err, "Error creating new stafulset")
		return nil, fmt.Errorf("error creating stateful set, %w", err)
	}

	var headlessServiceDefinition *corev1.Service
	headlesServiceName := namer.SvcHeadlessNameBuilder.Name()
	obj = reconciler.cloneOfDeployed(reflect.TypeFor[corev1.Service](), headlesServiceName)
	if obj != nil {
		headlessServiceDefinition = obj.(*corev1.Service)
	}

	labels := namer.LabelBuilder.Labels()
	headlessServiceDefinition = svc.NewHeadlessServiceForCR2(client, headlesServiceName, ssNamespacedName.Namespace, serviceports.GetDefaultPorts(true), labels, headlessServiceDefinition)
	reconciler.trackDesired(headlessServiceDefinition)

	if customResource.Spec.DeploymentPlan.RevisionHistoryLimit != nil {
		currentStatefulSet.Spec.RevisionHistoryLimit = customResource.Spec.DeploymentPlan.RevisionHistoryLimit
	}
	return currentStatefulSet, nil
}

func (reconciler *BrokerReconcilerImpl) ProcessDeploymentPlan(customResource *v1beta2.Broker, theNamer common.Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	deploymentPlan := &customResource.Spec.DeploymentPlan

	reconciler.log.V(2).Info("Processing deployment plan", "plan", deploymentPlan, "broker cr", customResource.Name)

	reqestedReplicas := brokerstatus.GetDeploymentSize(customResource)
	currentStatefulSet.Spec.Replicas = &reqestedReplicas

	if customResource.Spec.DeploymentPlan.PodDisruptionBudget != nil {
		reconciler.applyPodDisruptionBudget(customResource)
	}
}

func (reconciler *BrokerReconcilerImpl) applyPodDisruptionBudget(customResource *v1beta2.Broker) {

	var desired *policyv1.PodDisruptionBudget
	obj := reconciler.cloneOfDeployed(reflect.TypeFor[policyv1.PodDisruptionBudget](), customResource.Name+"-pdb")

	if obj != nil {
		desired = obj.(*policyv1.PodDisruptionBudget)
	} else {
		desired = &policyv1.PodDisruptionBudget{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "policy/v1",
				Kind:       "PodDisruptionBudget",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      customResource.Name + "-pdb",
				Namespace: customResource.Namespace,
			},
		}
	}
	desired.Spec = *customResource.Spec.DeploymentPlan.PodDisruptionBudget.DeepCopy()
	matchLabels := map[string]string{customResource.Kind: customResource.Name}

	desired.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: matchLabels,
	}

	reconciler.trackDesired(desired)
}

func (reconciler *BrokerReconcilerImpl) ServiceDefinitionForCR(serviceName types.NamespacedName, client rtclient.Client, nameSuffix string, portNumber int32, selectorLabels map[string]string, labels map[string]string) *corev1.Service {
	var serviceDefinition *corev1.Service
	obj := reconciler.cloneOfDeployed(reflect.TypeFor[corev1.Service](), serviceName.Name)
	if obj != nil {
		serviceDefinition = obj.(*corev1.Service)
	}
	return svc.NewServiceDefinitionForCR(serviceName, client, nameSuffix, portNumber, selectorLabels, labels, serviceDefinition)
}

func (reconciler *BrokerReconcilerImpl) trackDesired(desired rtclient.Object) {
	desiredType := reflect.TypeOf(desired)
	if reconciler.requestedResources == nil {
		reconciler.requestedResources = make(map[reflect.Type]map[string]rtclient.Object)
	}
	resMap, ok := reconciler.requestedResources[desiredType]
	if !ok {
		resMap = make(map[string]rtclient.Object)
		reconciler.requestedResources[desiredType] = resMap
	}
	resName := desired.GetName()
	resMap[resName] = desired
}

func (reconciler *BrokerReconcilerImpl) getFromDesired(kind reflect.Type, name string) rtclient.Object {
	obj, found := reconciler.requestedResources[kind][name]
	if found {
		return obj
	}
	return nil
}

func (reconciler *BrokerReconcilerImpl) applyTemplates(desired rtclient.Object) (err error) {
	for index, template := range reconciler.customResource.Spec.ResourceTemplates {
		if err = reconciler.applyTemplate(index, template, desired); err != nil {
			break
		}
	}
	return err
}

func (reconciler *BrokerReconcilerImpl) applyTemplate(index int, template v1beta2.ResourceTemplate, target rtclient.Object) error {
	if match(template, target) {

		reconciler.matchedTemplates[index] = true

		ordinal := extractOrdinal(target)
		itemName := extractItemName(target)
		resType := extractResType(target)
		if len(template.Annotations) > 0 {
			modified := make(map[string]string)
			maps.Copy(modified, target.GetAnnotations())
			for key, value := range template.Annotations {
				reconciler.applyFormattedKeyValue(modified, ordinal, itemName, resType, key, value)
			}
			target.SetAnnotations(modified)
		}
		if len(template.Labels) > 0 {
			modified := make(map[string]string)
			maps.Copy(modified, target.GetLabels())
			for key, value := range template.Labels {
				reconciler.applyFormattedKeyValue(modified, ordinal, itemName, resType, key, value)
			}
			target.SetLabels(modified)
		}

		if len(template.Patch.Raw) > 0 {

			// apply any patch
			converter := runtime.DefaultUnstructuredConverter

			var err error
			var targetAsUnstructured map[string]any

			patchMap := make(map[string]any)
			if err := json.Unmarshal(template.Patch.Raw, &patchMap); err != nil {
				return fmt.Errorf("error unmarshalling patch from template[%d], got %v", index, err)
			}

			if targetAsUnstructured, err = converter.ToUnstructured(target); err == nil {
				// patch, part of our CR, needs to be mutable
				patch := formatTemplatedObjectForBroker(reconciler.customResource, patchMap, ordinal, itemName, resType).(map[string]any)
				reconciler.log.V(1).Info("Applying strategic merge patch", "formattedPatch", patch)

				var patched strategicpatch.JSONMap
				if patched, err = strategicpatch.StrategicMergeMapPatch(targetAsUnstructured, patch, target); err == nil {
					err = converter.FromUnstructuredWithValidation(patched, target, true)
				}
			}
			if err != nil {
				return fmt.Errorf("error applying strategic merge patch from template[%d] to %s, got %v", index, target.GetName(), err)
			}
		}
	}
	return nil
}

func formatTemplatedObjectForBroker(customResource *v1beta2.Broker, object any, ordinal string, itemName string, resType string) any {
	if objectMap, isObjectMap := object.(map[string]any); isObjectMap {
		targetMap := make(map[string]any)
		for objectMapKey, objectMapValue := range objectMap {
			targetMap[objectMapKey] = formatTemplatedObjectForBroker(customResource, objectMapValue, ordinal, itemName, resType)
		}
		return targetMap
	} else if objectArray, isObjectArray := object.([]any); isObjectArray {
		targetArray := make([]any, len(objectArray))
		for objectArrayIndex, objectArrayValue := range objectArray {
			targetArray[objectArrayIndex] = formatTemplatedObjectForBroker(customResource, objectArrayValue, ordinal, itemName, resType)
		}
		return targetArray
	} else if objectString, isObjectString := object.(string); isObjectString {
		return formatTemplatedStringForBroker(customResource, objectString, ordinal, itemName, resType)
	}
	return object
}

func (reconciler *BrokerReconcilerImpl) applyFormattedKeyValue(collection map[string]string, ordinal string, itemName string, resType string, key string, value string) {
	formattedKey := formatTemplatedStringForBroker(reconciler.customResource, key, ordinal, itemName, resType)
	if value == RemoveKeySpecialValue {
		delete(collection, formattedKey)
	} else {
		collection[formattedKey] = formatTemplatedStringForBroker(reconciler.customResource, value, ordinal, itemName, resType)
	}
}

func formatTemplatedStringForBroker(customResource *v1beta2.Broker, template string, brokerOrdinal string, itemName string, resType string) string {
	if template != "" {
		template = strings.ReplaceAll(template, "$(CR_NAME)", customResource.Name)
		template = strings.ReplaceAll(template, "$(CR_NAMESPACE)", customResource.Namespace)
		template = strings.ReplaceAll(template, "$(BROKER_ORDINAL)", brokerOrdinal)
		template = strings.ReplaceAll(template, "$(ITEM_NAME)", itemName)
		template = strings.ReplaceAll(template, "$(RES_TYPE)", resType)
	}
	return template
}

func (reconciler *BrokerReconcilerImpl) CurrentDeployedResources(customResource *v1beta2.Broker, client rtclient.Client) {
	reqLogger := reconciler.log.WithValues("ActiveMQArtemis Name", customResource.Name)

	var err error
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		reconciler.checkExistingPersistentVolumes(customResource, client)
	}

	reconciler.deployed, err = brokerstatus.GetDeployedResources(customResource, client, reconciler.isOnOpenShift)
	if err != nil {
		reqLogger.Error(err, "error getting deployed resources")
		return
	}

	// track persisted cr secret
	for _, secret := range reconciler.deployed[reflect.TypeFor[corev1.Secret]()] {
		if strings.HasPrefix(secret.GetName(), "secret-broker-") {
			// track this as it is managed by the controller state machine, not by reconcile
			reconciler.trackDesired(secret)
		}
	}

	for t, objs := range reconciler.deployed {
		for _, obj := range objs {
			reqLogger.V(2).Info("Deployed ", "Type", t, "Name", obj.GetName())
		}
	}
}

func (reconciler *BrokerReconcilerImpl) ProcessResources(customResource *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme) (err error) {

	reqLogger := reconciler.log.WithValues("ActiveMQArtemis Name", customResource.Name)

	for _, requested := range common.ToResourceList(reconciler.requestedResources) {
		requested.SetNamespace(customResource.Namespace)
		if err = reconciler.applyTemplates(requested); err != nil {
			return err
		}
	}

	reqLogger.V(1).Info("Processing resources", "num requested", countOfRequestedBroker(reconciler), "num current", countOfDeployedBroker(reconciler))

	requested := compare.NewMapBuilder().Add(common.ToResourceList(reconciler.requestedResources)...).ResourceMap()

	comparator := compare.MapComparator{
		Comparator: compare.SimpleComparator(),
	}
	comparator.Comparator.SetDefaultComparator(reconciler.CompareMetaAndSpec)
	comparator.Comparator.SetComparator(reflect.TypeFor[corev1.Secret](), reconciler.CompareSecret)
	comparator.Comparator.SetComparator(reflect.TypeFor[corev1.ConfigMap](), reconciler.CompareConfigMap)

	var compositeError []error
	deltas := comparator.Compare(reconciler.deployed, requested)
	for _, resourceType := range getOrderedTypeList() {
		delta, ok := deltas[resourceType]
		if !ok {
			// not all types will have deltas
			continue
		}
		reqLogger.V(1).Info("", "instances of ", resourceType, "Will create ", len(delta.Added), "update ", len(delta.Updated), "and delete", len(delta.Removed))

		for index := range delta.Added {
			resourceToAdd := delta.Added[index]
			trackError(&compositeError, reconciler.createResource(customResource, client, scheme, resourceToAdd, resourceType))
		}
		for index := range delta.Updated {
			resourceToUpdate := delta.Updated[index]
			trackError(&compositeError, reconciler.updateResource(client, resourceToUpdate, resourceType))
		}
		for index := range delta.Removed {
			resourceToRemove := delta.Removed[index]
			trackError(&compositeError, reconciler.deleteResource(client, resourceToRemove, resourceType))
		}
	}

	// Check for matched resource templates and status condition update
	var unmatchedIndices []int
	for i := range customResource.Spec.ResourceTemplates {
		if !reconciler.matchedTemplates[i] {
			unmatchedIndices = append(unmatchedIndices, i)
		}
	}

	if len(unmatchedIndices) > 0 {
		validationCondition := meta.FindStatusCondition(customResource.Status.Conditions, v1beta2.ValidConditionType)

		// Only set to Unknown if there is no fatal validation error
		if validationCondition == nil || validationCondition.Status != metav1.ConditionFalse {
			message := fmt.Sprintf("ResourceTemplate at index %d did not match any operator-generated resources", unmatchedIndices[0])
			meta.SetStatusCondition(&customResource.Status.Conditions, metav1.Condition{
				Type:               v1beta2.ValidConditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             v1beta2.ValidConditionUnknownReason,
				Message:            message,
				ObservedGeneration: customResource.Generation,
			})
		}
	}

	if len(compositeError) == 0 {
		return nil
	} else {
		// maybe errors.Join in go1.20
		// using %q(uote) to keep errors separate
		return fmt.Errorf("%q", compositeError)
	}
}

func (reconciler *BrokerReconcilerImpl) CompareMetaAndSpec(deployed, requested rtclient.Object) bool {

	isEqual := equalObjectMeta(deployed, requested) &&
		equality.Semantic.DeepEqual(specOf(deployed), specOf(requested)) &&
		reconciler.ensureOwnerReferenceAPIVersion(reconciler.customResource, deployed, requested)
	if !isEqual {
		reconciler.log.V(2).Info("unequal", "deployed", &deployed, "requested", &requested)
	}
	return isEqual
}

func (reconciler *BrokerReconcilerImpl) CompareSecret(deployed, requested rtclient.Object) bool {

	isEqual := equalObjectMeta(deployed, requested) &&
		reconciler.ensureOwnerReferenceAPIVersion(reconciler.customResource, deployed, requested)
	if isEqual {
		deployedSecret := deployed.(*corev1.Secret)
		requestedSecret := requested.(*corev1.Secret)
		// TODO - remove all use of SecretData, just use Data and we can do away with this merge
		deployedSecret = mergeSecretStringDataToData(deployedSecret)
		requestedSecret = mergeSecretStringDataToData(requestedSecret)
		var pairs [][2]any
		pairs = append(pairs, [2]any{deployedSecret.Data, requestedSecret.Data})
		isEqual = compare.EqualPairs(pairs)
	}

	if !isEqual {
		reconciler.log.V(2).Info("unequal secret", "deployed", deployed, "requested", requested)
	}
	return isEqual
}

func (reconciler *BrokerReconcilerImpl) CompareConfigMap(deployed, requested rtclient.Object) bool {
	// our single configMap is immutable, the name indicates a change
	return deployed.GetName() == requested.GetName() &&
		reconciler.ensureOwnerReferenceAPIVersion(reconciler.customResource, deployed, requested)
}

// resourceTemplate means we can modify labels and annotatins so we need to
// respect those in our comparison logic

func (reconciler *BrokerReconcilerImpl) createResource(customResource *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Adding delta resources, i.e. creating ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.createRequestedResource(customResource, client, scheme, requested, kind)
}

func (reconciler *BrokerReconcilerImpl) updateResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Updating delta resources, i.e. updating ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.updateRequestedResource(client, requested, kind)

}

func (reconciler *BrokerReconcilerImpl) deleteResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Deleting delta resources, i.e. removing ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.deleteRequestedResource(client, requested, kind)
}

func (reconciler *BrokerReconcilerImpl) createRequestedResource(customResource *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Creating ", "kind ", kind, "named ", requested.GetName())
	return resources.Create(customResource, client, scheme, requested)
}

func (reconciler *BrokerReconcilerImpl) updateRequestedResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {
	var updateError error
	if updateError = resources.Update(client, requested); updateError == nil {
		reconciler.log.V(1).Info("updated", "kind ", kind, "named ", requested.GetName())
	} else {
		reconciler.log.V(0).Info("updated Failed", "kind ", kind, "named ", requested.GetName(), "error ", updateError)
	}
	return updateError
}

func (reconciler *BrokerReconcilerImpl) deleteRequestedResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {

	var deleteError error
	if deleteError := resources.Delete(client, requested); deleteError == nil {
		reconciler.log.V(2).Info("deleted", "kind", kind, " named ", requested.GetName())
	} else {
		reconciler.log.Error(deleteError, "delete Failed", "kind", kind, " named ", requested.GetName())
	}
	return deleteError
}

// older version of the operator would drop the owner reference, we need to adopt such secrets and update them
func (reconciler *BrokerReconcilerImpl) ensureOwnerReferenceAPIVersion(cr *v1beta2.Broker, existing rtclient.Object, candidate rtclient.Object) bool {
	ownerRefs := existing.GetOwnerReferences()
	if len(ownerRefs) > 0 {
		for i := range ownerRefs {
			if ownerRefs[i].Kind == "ActiveMQArtemis" && ownerRefs[i].Name == cr.Name {
				if ownerRefs[i].APIVersion != cr.APIVersion {
					reconciler.log.V(1).Info("Updating owner reference APIVersion",
						"resource", existing.GetName(),
						"from", ownerRefs[i].APIVersion,
						"to", cr.APIVersion)
					ownerRefs[i].APIVersion = cr.APIVersion
					candidate.SetOwnerReferences(ownerRefs)
					return false
				}
			}
		}
	}
	return true
}

func (reconciler *BrokerReconcilerImpl) checkExistingPersistentVolumes(instance *v1beta2.Broker, client rtclient.Client) {
	pvcKey := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name + "-" + namer.CrToSS(instance.Name) + "-0"}
	pvc := &corev1.PersistentVolumeClaim{}
	err := client.Get(context.TODO(), pvcKey, pvc)

	if err == nil {
		if len(pvc.OwnerReferences) > 0 {
			found := false
			newOwnerReferences := make([]metav1.OwnerReference, 0)
			for _, oref := range pvc.OwnerReferences {
				if oref.UID == instance.UID {
					found = true
				} else {
					newOwnerReferences = append(newOwnerReferences, oref)
				}
			}
			if found {
				reconciler.log.V(1).Info("removing owner ref from pvc to avoid potential data loss")
				pvc.OwnerReferences = newOwnerReferences
				if er := client.Update(context.TODO(), pvc); er != nil {
					reconciler.log.Error(er, "failed to remove ownerReference from pvc", "pvc", *pvc)
				}
			}
		}
	} else if !k8serrors.IsNotFound(err) {
		reconciler.log.Error(err, "got error in getting pvc")
	}
}

func (reconciler *BrokerReconcilerImpl) MakeVolumes(customResource *v1beta2.Broker, namer common.Namers) ([]corev1.Volume, error) {

	volumeDefinitions := []corev1.Volume{}
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		basicCRVolume := volumes.MakePersistentVolume(customResource.Name)
		volumeDefinitions = append(volumeDefinitions, basicCRVolume...)
	} else {
		emptyDirData := volumes.MakeEmptyDirVolumeFor(customResource.Name)
		volumeDefinitions = append(volumeDefinitions, emptyDirData)
	}

	volumeDefinitions = append(volumeDefinitions, customResource.Spec.DeploymentPlan.ExtraVolumes...)

	for _, epvc := range customResource.Spec.DeploymentPlan.ExtraVolumeClaimTemplates {
		epvcVolume := volumes.MakePersistentVolume(epvc.Name)
		volumeDefinitions = append(volumeDefinitions, epvcVolume...)
	}

	return volumeDefinitions, nil
}

// MakeExtraVolumeMountsForBroker creates volume mounts for ExtraVolumes and ExtraVolumeClaimTemplates.
// This is used by both the main container and init container.
func MakeExtraVolumeMountsForBroker(customResource *v1beta2.Broker) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{}

	for _, volume := range customResource.Spec.DeploymentPlan.ExtraVolumes {
		var volumeMount corev1.VolumeMount
		found := false
		for _, vm := range customResource.Spec.DeploymentPlan.ExtraVolumeMounts {
			if vm.Name == volume.Name {
				volumeMount = vm
				if volumeMount.MountPath == "" {
					volumeMount.MountPath = volumes.GetDefaultMountPath(&volume)
				}
				found = true
				break
			}
		}
		if !found {
			volumeMount = *volumes.MakeVolumeMountForVolume(&volume)
		}
		volumeMounts = append(volumeMounts, volumeMount)
	}

	for _, epvc := range customResource.Spec.DeploymentPlan.ExtraVolumeClaimTemplates {
		var vMount corev1.VolumeMount
		found := false
		for _, mount := range customResource.Spec.DeploymentPlan.ExtraVolumeMounts {
			if epvc.Name == mount.Name {
				vMount = mount
				found = true
				break
			}
		}
		if !found {
			vMount = *volumes.NewVolumeMountForPVC(epvc.Name)
		}
		volumeMounts = append(volumeMounts, vMount)
	}

	return volumeMounts
}

func (reconciler *BrokerReconcilerImpl) MakeVolumeMounts(customResource *v1beta2.Broker, namer common.Namers) ([]corev1.VolumeMount, error) {

	volumeMounts := []corev1.VolumeMount{}
	persistentCRVlMnt := volumes.MakePersistentVolumeMount(customResource.Name, getDataMountPathForBroker())
	volumeMounts = append(volumeMounts, persistentCRVlMnt...)

	// Add extra volumes and extra volume claim templates
	extraVolumeMounts := MakeExtraVolumeMountsForBroker(customResource)
	volumeMounts = append(volumeMounts, extraVolumeMounts...)

	return volumeMounts, nil
}

func getDataMountPathForBroker() string {
	return "/app"
}
func MakeContainerPortsForBroker(cr *v1beta2.Broker) []corev1.ContainerPort {

	containerPorts := []corev1.ContainerPort{}
	if cr.Spec.DeploymentPlan.JolokiaAgentEnabled {
		jolokiaContainerPort := corev1.ContainerPort{
			Name:          "jolokia",
			ContainerPort: 8778,
			Protocol:      "TCP",
		}
		containerPorts = append(containerPorts, jolokiaContainerPort)
	}
	consoleContainerPort := corev1.ContainerPort{
		Name:          "wconsj",
		ContainerPort: 8161,
		Protocol:      "TCP",
	}
	containerPorts = append(containerPorts, consoleContainerPort)

	return containerPorts
}

func (reconciler *BrokerReconcilerImpl) PodTemplateSpecForCR(customResource *v1beta2.Broker, namer common.Namers, currentStatefulSet *appsv1.StatefulSet, client rtclient.Client) (*corev1.PodTemplateSpec, error) {

	reqLogger := reconciler.log.WithName(customResource.Name)

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	current := &currentStatefulSet.Spec.Template

	terminationGracePeriodSeconds := int64(60)

	// custom labels provided in CR applied only to the pod template spec
	// note: work with a clone of the default labels to not modify defaults
	labels := make(map[string]string)
	maps.Copy(labels, namer.LabelBuilder.Labels())
	if customResource.Spec.DeploymentPlan.Labels != nil {
		maps.Copy(labels, customResource.Spec.DeploymentPlan.Labels)
	}

	pts := pods.MakePodTemplateSpec(current, namespacedName, labels, customResource.Spec.DeploymentPlan.Annotations)
	podSpec := &pts.Spec

	podSpec.ImagePullSecrets = customResource.Spec.DeploymentPlan.ImagePullSecrets

	container := containers.MakeContainer(podSpec, customResource.Name, brokerversion.ResolveImage(customResource, common.BrokerImageKey), MakeEnvVarArrayForCRForBroker(customResource, namer))

	container.Resources = customResource.Spec.DeploymentPlan.Resources

	reconciler.configureContianerSecurityContext(container, customResource.Spec.DeploymentPlan.ContainerSecurityContext)

	container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError

	containerPorts := MakeContainerPortsForBroker(customResource)
	if len(containerPorts) > 0 {
		reqLogger.V(1).Info("Adding new ports to main", "len", len(containerPorts))
		container.Ports = containerPorts
	}

	reqLogger.V(2).Info("Checking out extraMounts", "extra config", customResource.Spec.DeploymentPlan.ExtraMounts)

	configMapsToMount := customResource.Spec.DeploymentPlan.ExtraMounts.ConfigMaps
	secretsToMount := customResource.Spec.DeploymentPlan.ExtraMounts.Secrets
	brokerPropertiesResourceName, isSecret, brokerPropertiesMapData, serr := reconciler.addResourceForBrokerProperties(customResource, namer)
	if serr != nil {
		return nil, serr
	}
	if isSecret {
		secretsToMount = append(secretsToMount, brokerPropertiesResourceName)
	} else {
		configMapsToMount = append(configMapsToMount, brokerPropertiesResourceName)
	}

	additionalSystemProps := []string{}
	{
		mountPathRoot := common.SecretPathBase + getPropertiesResourceNsNameForBroker(customResource).Name
		securityProperties := NewPropsWithHeader()
		fmt.Fprintf(securityProperties, "login.config.url.1=file:%s/login.config\n", mountPathRoot)
		fmt.Fprintf(securityProperties, "security.provider.13=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")
		fmt.Fprintf(securityProperties, "fips.provider.8=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")

		brokerPropertiesMapData["_security.config"] = securityProperties.Bytes()

		additionalSystemProps = append(additionalSystemProps, fmt.Sprintf("-Djava.security.properties=%s/_security.config", mountPathRoot))

		loginConfig := newBufferWithHeader("//")
		fmt.Fprintf(loginConfig, "%s {\n", common.HttpAuthenticatorRealm)
		fmt.Fprintln(loginConfig, "  org.apache.activemq.artemis.spi.core.security.jaas.TextFileCertificateLoginModule required")
		fmt.Fprintln(loginConfig, "   reload=true")
		fmt.Fprintln(loginConfig, "   debug=true")
		fmt.Fprintf(loginConfig, "   org.apache.activemq.jaas.textfiledn.user=%s\n", common.GetCertUsersKey(common.HttpAuthenticatorRealm))
		fmt.Fprintf(loginConfig, "   org.apache.activemq.jaas.textfiledn.role=%s\n", common.GetCertRolesKey(common.HttpAuthenticatorRealm))
		fmt.Fprintf(loginConfig, "   baseDir=\"%v\"\n", mountPathRoot)
		fmt.Fprintln(loginConfig, "  ;")
		fmt.Fprintln(loginConfig, "};")
		brokerPropertiesMapData["login.config"] = loginConfig.Bytes()

		operandCertSecretName := common.GetOperandCertSecretName(customResource, client)
		operandCertSecret, err := common.GetNamespacedSecret(client, operandCertSecretName, customResource.Namespace)
		if err != nil {
			return nil, err
		}

		operandCertSubject, err := common.ExtractCertSubjectFromSecret(operandCertSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to extract operand subject from certificate, %w", err)
		}

		var caCertSecret *corev1.Secret
		if caCertSecret, err = common.GetOperatorCASecret(client); err != nil {
			return nil, fmt.Errorf("failed to get operator ca secret, %w", err)
		}

		caSecretKey, err := common.GetOperatorCASecretKey(client, caCertSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to get operator ca secret key, %w", err)
		}

		var operatorCert *tls.Certificate
		if operatorCert, err = common.GetOperatorClientCertificate(client, nil); err != nil {
			return nil, fmt.Errorf("failed to get operator client cert, %w", err)
		}

		var operatorCertSubject *pkix.Name
		if operatorCertSubject, err = common.ExtractCertSubject(operatorCert); err != nil {
			return nil, fmt.Errorf("failed to extract operator subject from client cert, %w", err)
		}

		prometheusCertSecretName := common.GetPrometheusCertSecretName(customResource, client)
		prometheusCertSecret, err := common.GetNamespacedSecret(client, prometheusCertSecretName, customResource.Namespace)
		var prometheusCertSubject *pkix.Name
		if err == nil {
			prometheusCertSubject, err = common.ExtractCertSubjectFromSecret(prometheusCertSecret)
			if err != nil {
				return nil, err
			}
		} else {
			ctrl.Log.V(1).Info("prometheus secret not found", "err", err)
		}

		// TODO - make configuable
		// support <crNname->control-plane-auth-secret, maybe a suffix for the http_server_authenticator realm login.config

		certUser := NewPropsWithHeader()
		fmt.Fprintln(certUser, "hawtio=/CN = hawtio-online\\.hawtio\\.svc.*/")
		fmt.Fprintf(certUser, "operator=/.*%s.*/\n", operatorCertSubject.CommonName) // regexp syntax start and with /
		// can and should use the full DN after https://issues.apache.org/jira/browse/ARTEMIS-5102
		fmt.Fprintf(certUser, "probe=/.*%s.*/\n", operandCertSubject.CommonName)
		if prometheusCertSubject != nil {
			fmt.Fprintf(certUser, "prometheus=/.*%s.*/\n", prometheusCertSubject.CommonName)
		}
		brokerPropertiesMapData[common.GetCertUsersKey(common.HttpAuthenticatorRealm)] = certUser.Bytes()

		certRoles := NewPropsWithHeader()
		fmt.Fprintln(certRoles, "status=operator,probe")
		fmt.Fprintln(certRoles, "metrics=operator,prometheus")
		fmt.Fprintln(certRoles, "hawtio=hawtio")
		brokerPropertiesMapData[common.GetCertRolesKey(common.HttpAuthenticatorRealm)] = certRoles.Bytes()

		foundationalProps := NewPropsWithHeader()
		fmt.Fprintf(foundationalProps, "name=%s\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintln(foundationalProps, "criticalAnalyzer=false")
		fmt.Fprintln(foundationalProps, "literalMatchMarkers=()")

		// with cert or token, jaas is cheap and a token will be cached while valid
		// TODO - avoid AMQP SASL login and server login duplication, verify
		fmt.Fprintln(foundationalProps, "authenticationCacheSize=0")

		fmt.Fprintln(foundationalProps, "messageCounterEnabled=false")
		fmt.Fprintln(foundationalProps, "journalDirectory=/app/data")
		fmt.Fprintln(foundationalProps, "bindingsDirectory=/app/data/bindings")
		fmt.Fprintln(foundationalProps, "largeMessagesDirectory=/app/data/largemessages")
		fmt.Fprintln(foundationalProps, "pagingDirectory=/app/data/paging")

		brokerPropertiesMapData["aa_restricted.properties"] = foundationalProps.Bytes()

		rbac := NewPropsWithHeader()
		// operator status check
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getStatus\".status.view=true")

		// jmx_exporter metrics perms
		fmt.Fprintln(rbac, "securityRoles.\"mops.mbeanserver.queryMBeans\".metrics.view=true")
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker\".metrics.view=true") // for query remove filter
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getTotalMessageCount\".metrics.view=true")
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getTotalMessagesAcknowledged\".metrics.view=true")
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getTotalMessagesAdded\".metrics.view=true")

		brokerPropertiesMapData["aa_rbac.properties"] = rbac.Bytes()

		secretsToMount = append(secretsToMount, operandCertSecretName)
		caSecret := common.GetOperatorCASecretName()
		secretsToMount = append(secretsToMount, caSecret)

		jolokiaConfig := NewPropsWithHeader()
		fmt.Fprintln(jolokiaConfig, "protocol=https")
		fmt.Fprintln(jolokiaConfig, "authClass=org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator")
		fmt.Fprintf(jolokiaConfig, "caCert=%s%s/%s\n", common.SecretPathBase, caSecret, caSecretKey)
		fmt.Fprintf(jolokiaConfig, "serverCert=%s%s/tls.crt\n", common.SecretPathBase, operandCertSecretName)
		fmt.Fprintf(jolokiaConfig, "serverKey=%s%s/tls.key\n", common.SecretPathBase, operandCertSecretName)
		fmt.Fprintln(jolokiaConfig, "port=8778")
		// https://github.com/jolokia/jolokia/issues/751 at some point host=$(env:HOSTNAME), host= is on the command line below
		fmt.Fprintln(jolokiaConfig, "useSslClientAuthentication=true")
		fmt.Fprintln(jolokiaConfig, "disabledServices=org.jolokia.service.history.HistoryMBeanRequestInterceptor")
		fmt.Fprintln(jolokiaConfig, "disableDetectors=true")
		fmt.Fprintln(jolokiaConfig, "debug=false")

		brokerPropertiesMapData["_jolokia.config"] = jolokiaConfig.Bytes()

		pemCfg := NewPropsWithHeader()

		fmt.Fprintf(pemCfg, "alias=alias\n")
		fmt.Fprintf(pemCfg, "source.cert=%s%s/tls.crt\n", common.SecretPathBase, operandCertSecretName)
		fmt.Fprintf(pemCfg, "source.key=%s%s/tls.key\n", common.SecretPathBase, operandCertSecretName)
		brokerPropertiesMapData["_cert.pemcfg"] = pemCfg.Bytes()

		prometheusConfig := NewPropsWithHeader() // yaml
		fmt.Fprintf(prometheusConfig, "httpServer:\n")
		fmt.Fprintf(prometheusConfig, "  authentication:\n")
		fmt.Fprintf(prometheusConfig, "    plugin:\n")
		fmt.Fprintf(prometheusConfig, "      class: org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator\n")
		fmt.Fprintf(prometheusConfig, "      subjectAttributeName: org.jolokia.jaasSubject\n") // match -DhttpServerAuthenticator.requestSubjectAttribute
		fmt.Fprintf(prometheusConfig, "  ssl:\n")
		fmt.Fprintf(prometheusConfig, "    mutualTLS: true\n")
		fmt.Fprintf(prometheusConfig, "    keyStore:\n")
		fmt.Fprintf(prometheusConfig, "      filename: %s/_cert.pemcfg\n", mountPathRoot)
		fmt.Fprintf(prometheusConfig, "      type: PEMCFG\n")
		fmt.Fprintf(prometheusConfig, "    trustStore:\n")
		fmt.Fprintf(prometheusConfig, "      filename: %s%s/%s\n", common.SecretPathBase, caSecret, caSecretKey)
		fmt.Fprintf(prometheusConfig, "      type: PEMCA\n")
		fmt.Fprintf(prometheusConfig, "    certificate:\n")
		fmt.Fprintf(prometheusConfig, "      alias: alias\n")
		// the collector/scraper config
		fmt.Fprintf(prometheusConfig, "lowercaseOutputName: true\n")
		fmt.Fprintf(prometheusConfig, "lowercaseOutputLabelNames: true\n")
		fmt.Fprintf(prometheusConfig, "includeObjectNames: [org.apache.activemq.artemis:broker=\"%s\"]\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheusConfig, "includeObjectNameAttributes:\n")
		fmt.Fprintf(prometheusConfig, "  'org.apache.activemq.artemis:broker=\"%s\"':\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheusConfig, "    - \"TotalMessageCount\"\n")
		fmt.Fprintf(prometheusConfig, "    - \"TotalMessagesAdded\"\n")
		fmt.Fprintf(prometheusConfig, "    - \"TotalMessagesAcknowledged\"\n")
		fmt.Fprintf(prometheusConfig, "rules:\n")
		fmt.Fprintf(prometheusConfig, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessageCount'\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheusConfig, "    help: Number of pending messages\n")
		fmt.Fprintf(prometheusConfig, "    name: artemis_total_pending_message_count\n")
		fmt.Fprintf(prometheusConfig, "    type: GAUGE\n")
		fmt.Fprintf(prometheusConfig, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessagesAcknowledged'\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheusConfig, "    help: Number of messages consumed since start\n")
		fmt.Fprintf(prometheusConfig, "    name: artemis_total_consumed_message_count\n")
		fmt.Fprintf(prometheusConfig, "    type: COUNTER\n")
		fmt.Fprintf(prometheusConfig, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessagesAdded'\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheusConfig, "    help: Number of messages produced since start\n")
		fmt.Fprintf(prometheusConfig, "    name: artemis_total_produced_message_count\n")
		fmt.Fprintf(prometheusConfig, "    type: COUNTER\n")

		brokerPropertiesMapData[PrometheusConfigFileName] = prometheusConfig.Bytes()

		// Apply control plane overrides if they exist
		if err := applyControlPlaneOverridesForBroker(customResource, client, brokerPropertiesMapData); err != nil {
			return nil, err
		}

		// adapt jolokia and prometheus authentication
		additionalSystemProps = append(additionalSystemProps, "-DhttpServerAuthenticator.requestSubjectAttribute=org.jolokia.jaasSubject")

		// install mbean server guard
		additionalSystemProps = append(additionalSystemProps, "-Dlog4j2.disableJmx=true -Djavax.management.builder.initial=org.apache.activemq.artemis.core.server.management.ArtemisRbacMBeanServerBuilder")

		// install jolokia agent
		additionalSystemProps = append(additionalSystemProps, fmt.Sprintf("-javaagent:/opt/agents/jolokia.jar=host=$HOSTNAME,config=%s/_jolokia.config", mountPathRoot))

		// install prometheus agent
		additionalSystemProps = append(additionalSystemProps, fmt.Sprintf("-javaagent:/opt/agents/prometheus.jar=$HOSTNAME:8888:%s/%s", mountPathRoot, PrometheusConfigFileName))

		// non boot jar isolation classpath
		additionalSystemProps = append(additionalSystemProps, "-classpath /opt/amq/lib/*:/opt/amq/lib/extra/*")

		// temp volume
		additionalSystemProps = append(additionalSystemProps, "-Djava.io.tmpdir=/app/tmp")

		// jvm options
		additionalSystemProps = append(additionalSystemProps, "-XX:InitialRAMPercentage=70.0 -XX:MaxRAMPercentage=70.0 -XX:AutoBoxCacheMax=20000 -XX:+PrintClassHistogram -XX:+UseG1GC -XX:+UseStringDeduplication -Djava.net.preferIPv4Stack=true")

		if customResource.Spec.DeploymentPlan.LivenessProbe == nil {
			container.LivenessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/bin/bash",
							"-c",
							// use curl with mtls as the broker-cert to pull the status to find start state using dns
							fmt.Sprintf(`export STATEFUL_SET_ORDINAL=${HOSTNAME##*-};curl --cacert %s%s/%s --cert %s%s/tls.crt --key %s%s/tls.key  https://%s:8778/jolokia/read/org.apache.activemq.artemis:broker=%%22%s%%22/Status | grep -w -P "(START|STOPP)(ED|ING)"`, common.SecretPathBase, caSecret, caSecretKey, common.SecretPathBase, operandCertSecretName, common.SecretPathBase, operandCertSecretName, common.OrdinalStringFQDNS(customResource.Name, customResource.Namespace, "$STATEFUL_SET_ORDINAL"), environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name)),
						},
					},
				},
				InitialDelaySeconds:           1,
				TimeoutSeconds:                5,
				PeriodSeconds:                 5,
				SuccessThreshold:              1,
				FailureThreshold:              2,
				TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			}
		} else {
			// use the value from the CR
			container.LivenessProbe = reconciler.configureLivenessProbe(container, customResource.Spec.DeploymentPlan.LivenessProbe)
		}
	}
	extraVolumes, extraVolumeMounts, err := reconciler.createExtraConfigmapsAndSecretsVolumeMounts(configMapsToMount, secretsToMount, brokerPropertiesResourceName, brokerPropertiesMapData, client)
	if err != nil {
		return nil, fmt.Errorf("failed to createExtraConfigmapsAndSecretsVolumeMounts, %w", err)
	}

	reqLogger.V(2).Info("Extra volumes", "volumes", extraVolumes)
	reqLogger.V(2).Info("Extra mounts", "mounts", extraVolumeMounts)

	container.VolumeMounts, err = reconciler.MakeVolumeMounts(customResource, namer)
	if err != nil {
		return nil, fmt.Errorf("failed to make volume mounts, %w", err)
	}
	if len(extraVolumeMounts) > 0 {
		container.VolumeMounts = append(container.VolumeMounts, extraVolumeMounts...)
	}

	container.StartupProbe = reconciler.configureStartupProbe(container, customResource.Spec.DeploymentPlan.StartupProbe)
	container.ReadinessProbe = reconciler.configureReadinessProbe(container, customResource.Spec.DeploymentPlan.ReadinessProbe)

	if len(customResource.Spec.DeploymentPlan.NodeSelector) > 0 {
		reqLogger.V(1).Info("Adding Node Selectors", "len", len(customResource.Spec.DeploymentPlan.NodeSelector))
		podSpec.NodeSelector = customResource.Spec.DeploymentPlan.NodeSelector
	}

	reconciler.configureAffinity(podSpec, &customResource.Spec.DeploymentPlan.Affinity)

	if len(customResource.Spec.DeploymentPlan.Tolerations) > 0 {
		reqLogger.V(1).Info("Adding Tolerations", "len", len(customResource.Spec.DeploymentPlan.Tolerations))
		podSpec.Tolerations = customResource.Spec.DeploymentPlan.Tolerations
	}

	newContainersArray := []corev1.Container{}
	podSpec.Containers = append(newContainersArray, *container)
	brokerVolumes, err := reconciler.MakeVolumes(customResource, namer)
	if err != nil {
		return nil, fmt.Errorf("failed to make volumes, %w", err)
	}
	if len(extraVolumes) > 0 {
		brokerVolumes = append(brokerVolumes, extraVolumes...)
	}
	if len(brokerVolumes) > 0 {
		podSpec.Volumes = brokerVolumes
	}
	podSpec.TerminationGracePeriodSeconds = &terminationGracePeriodSeconds

	//tell container don't config
	envConfigBroker := corev1.EnvVar{
		Name:  "CONFIG_BROKER",
		Value: "false",
	}
	environments.Create(podSpec.Containers, &envConfigBroker)

	envBrokerCustomInstanceDir := corev1.EnvVar{
		Name:  "CONFIG_INSTANCE_DIR",
		Value: brokerConfigRoot,
	}
	environments.Create(podSpec.Containers, &envBrokerCustomInstanceDir)

	// JAAS Config
	if jaasConfigPath, found := getJaasConfigExtraMountPathForBroker(customResource); found {
		debugArgs := corev1.EnvVar{
			Name:  getJaasConfigEnvVarNameForBroker(),
			Value: fmt.Sprintf("-Djava.security.auth.login.config=%v", jaasConfigPath),
		}
		environments.CreateOrAppend(podSpec.Containers, &debugArgs)
	}

	if loggingConfigPath, found := getLoggingConfigExtraMountPathForBroker(customResource); found {
		loggerOpts := corev1.EnvVar{
			Name:  getLoginConfigEnvVarNameForBroker(),
			Value: fmt.Sprintf("-Dlog4j2.configurationFile=%v", loggingConfigPath),
		}
		environments.CreateOrAppend(podSpec.Containers, &loggerOpts)
	} else {
		loggerOpts := corev1.EnvVar{
			Name:  getLoginConfigEnvVarNameForBroker(),
			Value: "-Dlog4j2.level=INFO",
		}
		environments.CreateOrAppend(podSpec.Containers, &loggerOpts)
	}

	// add TopologySpreadConstraints config
	podSpec.TopologySpreadConstraints = customResource.Spec.DeploymentPlan.TopologySpreadConstraints

	compactVersionToUse, verr := brokerversion.DetermineCompactVersionToUse(customResource)
	if verr != nil {
		reqLogger.Error(verr, "failed to get compact version", "Spec.Version", customResource.Spec.Version)
		return nil, verr
	}
	yacfgProfileVersion = version.YacfgProfileVersionFromFullVersion[version.FullVersionFromCompactVersion[compactVersionToUse]]

	var mountPoint = common.SecretPathBase
	if !isSecret {
		mountPoint = cfgMapPathBase
	}
	brokerPropsValue := reconciler.brokerPropertiesConfigSystemPropValue(mountPoint, brokerPropertiesResourceName, brokerPropertiesMapData)

	jdkJavaOpts := corev1.EnvVar{
		Name:  jdkJavaOptionsEnvVarName,
		Value: brokerPropsValue,
	}
	environments.CreateOrAppend(podSpec.Containers, &jdkJavaOpts)

	reconciler.configPodSecurity(podSpec, &customResource.Spec.DeploymentPlan.PodSecurity)
	reconciler.configurePodSecurityContext(podSpec, customResource.Spec.DeploymentPlan.PodSecurityContext)

	pts.Spec = *podSpec
	pts.Spec.InitContainers = nil

	reEvalJdkOpts := generateReEvalOrdinaEnvReplacement(customResource.Spec.Env)

	pts.Spec.Containers[0].Command = []string{
		"/bin/bash", "-c",
		fmt.Sprintf("export STATEFUL_SET_ORDINAL=${HOSTNAME##*-}; %s exec java %s $JAVA_ARGS_APPEND org.apache.activemq.artemis.core.server.embedded.Main", reEvalJdkOpts, strings.Join(additionalSystemProps, " ")),
	}

	reqLogger.V(2).Info("Final Init spec", "Detail", podSpec.InitContainers)

	return pts, nil
}

// support ${STATEFUL_SET_ORDINAL} replacement in JDK options from CR env if necessary

func getJaasConfigEnvVarNameForBroker() string {
	return jdkJavaOptionsEnvVarName
}

func getLoginConfigEnvVarNameForBroker() string {
	return jdkJavaOptionsEnvVarName
}

func (reconciler *BrokerReconcilerImpl) brokerPropertiesConfigSystemPropValue(mountPoint, resourceName string, brokerPropertiesData map[string][]byte) string {
	var result string
	if len(brokerPropertiesData) == 1 {
		result = fmt.Sprintf("-Dbroker.properties=%s%s/%s", mountPoint, resourceName, BrokerPropertiesName)
	} else {
		result = fmt.Sprintf("-Dbroker.properties=%s%s/,%s%s/%s${STATEFUL_SET_ORDINAL}/", mountPoint, resourceName, mountPoint, resourceName, OrdinalPrefix)
	}

	for _, extraSecretName := range reconciler.customResource.Spec.DeploymentPlan.ExtraMounts.Secrets {
		if strings.HasSuffix(extraSecretName, common.BrokerPropsSuffix) {
			result = fmt.Sprintf("%s,%s%s/,%s%s/%s${STATEFUL_SET_ORDINAL}/", result, common.SecretPathBase, extraSecretName, common.SecretPathBase, extraSecretName, OrdinalPrefix)
		}
	}

	return result
}

func getJaasConfigExtraMountPathForBroker(customResource *v1beta2.Broker) (string, bool) {
	if t, name, found := getConfigExtraMountForBroker(customResource, jaasConfigSuffix); found {
		return fmt.Sprintf("/amq/extra/%v/%v/login.config", t, name), true
	}
	return "", false
}

func getLoggingConfigExtraMountPathForBroker(customResource *v1beta2.Broker) (string, bool) {
	if t, name, found := getConfigExtraMountForBroker(customResource, loggingConfigSuffix); found {
		return fmt.Sprintf("/amq/extra/%v/%v/logging.properties", t, name), true
	}
	return "", false
}

func getConfigExtraMountForBroker(customResource *v1beta2.Broker, suffix string) (string, string, bool) {
	for _, cm := range customResource.Spec.DeploymentPlan.ExtraMounts.ConfigMaps {
		if strings.HasSuffix(cm, suffix) {
			return "configmaps", cm, true
		}
	}
	for _, s := range customResource.Spec.DeploymentPlan.ExtraMounts.Secrets {
		if strings.HasSuffix(s, suffix) {
			return "secrets", s, true
		}
	}
	return "", "", false
}

func (reconciler *BrokerReconcilerImpl) configureStartupProbe(container *corev1.Container, probeFromCr *corev1.Probe) *corev1.Probe {

	var startupProbe = container.StartupProbe
	reconciler.log.V(1).Info("Configuring Startup Probe", "existing", startupProbe)

	if probeFromCr != nil {
		if startupProbe == nil {
			startupProbe = &corev1.Probe{}
		}

		conditionallyApplyValuesToPreserveDefaults(startupProbe, probeFromCr)
		startupProbe.ProbeHandler = probeFromCr.ProbeHandler
	} else {
		startupProbe = nil
	}

	return startupProbe
}

func (reconciler *BrokerReconcilerImpl) configureLivenessProbe(container *corev1.Container, probeFromCr *corev1.Probe) *corev1.Probe {
	var livenessProbe = container.LivenessProbe
	reconciler.log.V(1).Info("Configuring Liveness Probe", "existing", livenessProbe)

	if livenessProbe == nil {
		livenessProbe = &corev1.Probe{}
	}

	if probeFromCr != nil {
		conditionallyApplyValuesToPreserveDefaults(livenessProbe, probeFromCr)

		// not complete in this case!
		if probeFromCr.GRPC == nil && probeFromCr.Exec == nil && probeFromCr.HTTPGet == nil && probeFromCr.TCPSocket == nil {
			reconciler.log.V(1).Info("Adding default TCP check")
			livenessProbe.ProbeHandler = corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(TCPLivenessPort),
				},
			}
		} else {
			reconciler.log.V(1).Info("Using user provided Liveness Probe Handler" + probeFromCr.ProbeHandler.String())
			livenessProbe.ProbeHandler = probeFromCr.ProbeHandler
		}
	}

	return livenessProbe
}

func (reconciler *BrokerReconcilerImpl) configureReadinessProbe(container *corev1.Container, probeFromCr *corev1.Probe) *corev1.Probe {

	var readinessProbe = container.ReadinessProbe
	reconciler.log.V(1).Info("Configuring Readyness Probe", "existing", readinessProbe)

	if readinessProbe == nil {
		readinessProbe = &corev1.Probe{}
	}

	if probeFromCr != nil {
		conditionallyApplyValuesToPreserveDefaults(readinessProbe, probeFromCr)
		if probeFromCr.GRPC == nil && probeFromCr.Exec == nil && probeFromCr.HTTPGet == nil && probeFromCr.TCPSocket == nil {
			reconciler.log.V(2).Info("adding default handler to user provided readiness Probe")

			// respect existing command where already deployed
			if readinessProbe.Exec != nil && reflect.DeepEqual(readinessProbe.Exec.Command, command) {
				// leave it be so we don't force a reconcile
			} else {
				// upgrade to betterCommand!
				readinessProbe.ProbeHandler = corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: betterCommand,
					},
				}
			}
		} else {
			readinessProbe.ProbeHandler = probeFromCr.ProbeHandler
		}
	} else {
		readinessProbe = nil
	}

	return readinessProbe
}

// when the CR has a full Spec, the intent is that the Spec is fully formed, such that there are not server side defaults in the mix.
// For probes, we historically allow a partial spec, so we need to be careful to not overide server side applied defaults with empty values

// applyControlPlaneOverrides applies control plane configuration overrides from secrets.
// It first checks for CR-specific override secret ([cr-name]-control-plane-override),
// then falls back to shared override secret (control-plane-override).
// Each key in the override secret completely replaces the corresponding key in brokerPropertiesMapData.
func applyControlPlaneOverridesForBroker(customResource *v1beta2.Broker, client rtclient.Client, brokerPropertiesMapData map[string][]byte) error {
	ctx := context.Background()

	// Try CR-specific override secret first
	crSpecificSecretName := customResource.Name + "-control-plane-override"
	overrideSecret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Name:      crSpecificSecretName,
		Namespace: customResource.Namespace,
	}

	err := client.Get(ctx, secretKey, overrideSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Try shared override secret as fallback
			secretKey.Name = "control-plane-override"
			err = client.Get(ctx, secretKey, overrideSecret)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					// No override secret found, this is OK
					return nil
				}
				return err
			}
		} else {
			return err
		}
	}

	// Apply overrides - complete replacement per key
	maps.Copy(brokerPropertiesMapData, overrideSecret.Data)

	return nil
}

func getPropertiesResourceNsNameForBroker(artemis *v1beta2.Broker) types.NamespacedName {
	return types.NamespacedName{
		Namespace: artemis.Namespace,
		Name:      artemis.Name + "-props",
	}
}

func (reconciler *BrokerReconcilerImpl) addResourceForBrokerProperties(customResource *v1beta2.Broker, namer common.Namers) (string, bool, map[string][]byte, error) {

	// fetch and do idempotent transform based on CR

	// deal with upgrade to mutable secret, only upgrade to mutable on not found
	alder32Bytes := alder32Of(customResource.Spec.BrokerProperties)
	shaOfMap := hex.EncodeToString(alder32Bytes)
	resourceName := types.NamespacedName{
		Namespace: customResource.Namespace,
		Name:      customResource.Name + "-props-" + shaOfMap,
	}

	obj := reconciler.cloneOfDeployed(reflect.TypeFor[corev1.ConfigMap](), resourceName.Name)
	if obj != nil {
		existing := obj.(*corev1.ConfigMap)
		// found existing (immuable) map with sha in the name
		reconciler.log.V(1).Info("Requesting configMap for broker properties", "name", resourceName.Name)
		reconciler.trackDesired(existing)

		return resourceName.Name, false, existing.BinaryData, nil
	}

	var desired *corev1.Secret
	resourceName = getPropertiesResourceNsNameForBroker(customResource)

	obj = reconciler.cloneOfDeployed(reflect.TypeFor[corev1.Secret](), resourceName.Name)
	if obj != nil {
		desired = obj.(*corev1.Secret)
	}

	data := BrokerPropertiesData(reconciler.customResource.Spec.BrokerProperties)

	if desired == nil {
		reconciler.log.V(1).Info("desired brokerprop secret nil, create new one", "name", resourceName.Name)
		secret := secrets.MakeSecret(resourceName, data, namer.LabelBuilder.Labels())
		desired = &secret
	} else {
		desired.Data = data
	}

	reconciler.trackDesired(desired)

	return resourceName.Name, true, data, nil
}

func (reconciler *BrokerReconcilerImpl) configureAffinity(podSpec *corev1.PodSpec, affinity *v1beta2.AffinityConfig) {
	if affinity != nil {
		podSpec.Affinity = &corev1.Affinity{}
		if affinity.PodAffinity != nil {
			reconciler.log.V(1).Info("Adding Pod Affinity")
			podSpec.Affinity.PodAffinity = affinity.PodAffinity
		}
		if affinity.PodAntiAffinity != nil {
			reconciler.log.V(1).Info("Adding Pod AntiAffinity")
			podSpec.Affinity.PodAntiAffinity = affinity.PodAntiAffinity
		}
		if affinity.NodeAffinity != nil {
			reconciler.log.V(1).Info("Adding Node Affinity")
			podSpec.Affinity.NodeAffinity = affinity.NodeAffinity
		}
	}
}

func (reconciler *BrokerReconcilerImpl) configurePodSecurityContext(podSpec *corev1.PodSpec, podSecurityContext *corev1.PodSecurityContext) {
	reconciler.log.V(1).Info("Configuring PodSecurityContext")

	if nil != podSecurityContext {
		reconciler.log.V(2).Info("Incoming podSecurityContext is NOT nil, assigning")
		podSpec.SecurityContext = podSecurityContext
	} else {
		reconciler.log.V(2).Info("Incoming podSecurityContext is nil, creating with default values")
		runAsNonRoot := true
		seccompProfile := corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
		podSpec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot:   &runAsNonRoot,
			SeccompProfile: &seccompProfile,
		}
	}
}

func (reconciler *BrokerReconcilerImpl) configureContianerSecurityContext(container *corev1.Container, containerSecurityContext *corev1.SecurityContext) {
	reconciler.log.V(1).Info("Configuring Container SecurityContext")

	if nil != containerSecurityContext {
		reconciler.log.V(2).Info("Incoming Container SecurityContext is NOT nil, assigning")
		container.SecurityContext = containerSecurityContext
	} else {
		reconciler.log.V(2).Info("Incoming Container SecurityContext is nil, creating with default values")
		readOnlyRootFilesystem := true
		runAsNonRoot := true
		allowPrivilegeEscalation := false
		capabilities := corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}
		seccompProfile := corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
		securityContext := corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			Capabilities:             &capabilities,
			SeccompProfile:           &seccompProfile,
			RunAsNonRoot:             &runAsNonRoot,
			ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
		}
		container.SecurityContext = &securityContext
	}
}

// generic version?

func (reconciler *BrokerReconcilerImpl) configPodSecurity(podSpec *corev1.PodSpec, podSecurity *v1beta2.PodSecurityType) {
	if podSecurity.ServiceAccountName != nil {
		reconciler.log.V(2).Info("Pod serviceAccountName specified", "existing", podSpec.ServiceAccountName, "new", *podSecurity.ServiceAccountName)
		podSpec.ServiceAccountName = *podSecurity.ServiceAccountName
	} else {
		autoMount := false
		podSpec.AutomountServiceAccountToken = &autoMount
	}
	if podSecurity.RunAsUser != nil {
		reconciler.log.V(2).Info("Pod runAsUser specified", "runAsUser", *podSecurity.RunAsUser)
		if podSpec.SecurityContext == nil {
			runAsNonRoot := true
			seccompProfile := corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
			secCtxt := corev1.PodSecurityContext{
				RunAsUser:      podSecurity.RunAsUser,
				RunAsNonRoot:   &runAsNonRoot,
				SeccompProfile: &seccompProfile,
			}
			podSpec.SecurityContext = &secCtxt
		} else {
			podSpec.SecurityContext.RunAsUser = podSecurity.RunAsUser
		}
	}
}

func (reconciler *BrokerReconcilerImpl) createExtraConfigmapsAndSecretsVolumeMounts(configMaps []string, secrets []string, brokePropertiesResourceName string, brokerPropsData map[string][]byte, client rtclient.Client) ([]corev1.Volume, []corev1.VolumeMount, error) {

	var extraVolumes []corev1.Volume
	var extraVolumeMounts []corev1.VolumeMount

	if len(configMaps) > 0 {
		for _, cfgmap := range configMaps {
			if cfgmap == "" {
				reconciler.log.V(1).Info("No ConfigMap name specified, ignore", "configMap", cfgmap)
				continue
			}
			cfgmapPath := cfgMapPathBase + cfgmap
			reconciler.log.V(2).Info("Resolved configMap path", "path", cfgmapPath)
			//now we have a config map. First create a volume
			cfgmapVol := volumes.MakeVolumeForConfigMap(cfgmap)
			cfgmapVolumeMount := volumes.MakeVolumeMountForCfg(cfgmapVol.Name, cfgmapPath, true)
			extraVolumes = append(extraVolumes, cfgmapVol)
			extraVolumeMounts = append(extraVolumeMounts, cfgmapVolumeMount)
		}
	}

	if len(secrets) > 0 {
		for _, secret := range secrets {
			if secret == "" {
				reconciler.log.V(2).Info("No Secret name specified, ignore", "Secret", secret)
				continue
			}
			secretPath := common.SecretPathBase + secret
			//now we have a secret. First create a volume
			secretVol := volumes.MakeVolumeForSecret(secret)

			if secret == brokePropertiesResourceName && hasOrdinalPropertieKeyInData(brokerPropsData) {
				// place ordinal data in subpath in order
				for _, key := range sortedKeysStringKeyByteValue(brokerPropsData) {
					matches := ParseBrokerPropertyWithOrdinal(key)
					if len(matches) > 0 {
						subPath := matches[1]
						secretVol.Secret.Items = append(secretVol.Secret.Items, corev1.KeyToPath{Key: key, Path: fmt.Sprintf("%s/%s", subPath, key)})
					} else {
						secretVol.Secret.Items = append(secretVol.Secret.Items, corev1.KeyToPath{Key: key, Path: key})
					}
				}
			}

			if strings.HasSuffix(secret, common.BrokerPropsSuffix) {
				bpSecret := &corev1.Secret{}
				bpSecretKey := types.NamespacedName{
					Name:      secret,
					Namespace: reconciler.customResource.Namespace,
				}
				if err := resources.Retrieve(bpSecretKey, client, bpSecret); err != nil {
					return nil, nil, err
				}

				if len(bpSecret.Data) > 0 && hasOrdinalPropertieKeyInData(bpSecret.Data) {
					for _, key := range sortedKeysStringKeyByteValue(bpSecret.Data) {
						matches := ParseBrokerPropertyWithOrdinal(key)
						if len(matches) > 0 {
							subPath := matches[1]
							secretVol.Secret.Items = append(secretVol.Secret.Items, corev1.KeyToPath{Key: key, Path: fmt.Sprintf("%s/%s", subPath, key)})
						} else {
							secretVol.Secret.Items = append(secretVol.Secret.Items, corev1.KeyToPath{Key: key, Path: key})
						}
					}
				}

			}
			secretVolumeMount := volumes.MakeVolumeMountForCfg(secretVol.Name, secretPath, true)
			extraVolumes = append(extraVolumes, secretVol)
			extraVolumeMounts = append(extraVolumeMounts, secretVolumeMount)
		}
	}

	return extraVolumes, extraVolumeMounts, nil
}

func (reconciler *BrokerReconcilerImpl) StatefulSetForCR(customResource *v1beta2.Broker, namer common.Namers, currentStateFullSet *appsv1.StatefulSet, client rtclient.Client) (*appsv1.StatefulSet, error) {

	//	reqLogger := reconciler.log.WithName(customResource.Name)

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	replicas := brokerstatus.GetDeploymentSize(customResource)
	currentStateFullSet = ss.MakeStatefulSet(currentStateFullSet, namer.SsNameBuilder.Name(), namer.SvcHeadlessNameBuilder.Name(), namespacedName, nil, namer.LabelBuilder.Labels(), &replicas)

	podTemplateSpec, err := reconciler.PodTemplateSpecForCR(customResource, namer, currentStateFullSet, client)
	if err != nil {
		//reqLogger.Error(err, "error creating pod template")
		return nil, fmt.Errorf("error creating pod template, %w", err)
	}
	currentStateFullSet.Spec.Template = *podTemplateSpec

	pvcTemplates, err := reconciler.PersistentVolumeClaimArrayForCR(customResource, namer, currentStateFullSet.Spec)
	if err != nil {
		return nil, fmt.Errorf("error creating volume claim templates, %w", err)
	}
	currentStateFullSet.Spec.VolumeClaimTemplates = pvcTemplates

	return currentStateFullSet, nil
}

func (reconciler *BrokerReconcilerImpl) PersistentVolumeClaimArrayForCR(customResource *v1beta2.Broker, namer common.Namers, spec appsv1.StatefulSetSpec) ([]corev1.PersistentVolumeClaim, error) {

	var existing, current *corev1.PersistentVolumeClaim
	pvcArray := make([]corev1.PersistentVolumeClaim, 0)

	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		capacity := "2Gi"
		if customResource.Spec.DeploymentPlan.Storage.Size != "" {
			capacity = customResource.Spec.DeploymentPlan.Storage.Size
		}

		tempateClaim := &v1beta2.VolumeClaimTemplate{
			ObjectMeta: v1beta2.ObjectMeta{
				Name:   customResource.Name,
				Labels: namer.LabelBuilder.Labels(),
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName(corev1.ResourceStorage): resource.MustParse(capacity),
					},
				},
			},
		}
		if customResource.Spec.DeploymentPlan.Storage.StorageClassName != "" {
			tempateClaim.Spec.StorageClassName = &customResource.Spec.DeploymentPlan.Storage.StorageClassName
		}

		existing = findExistingByName(spec.VolumeClaimTemplates, tempateClaim)
		current = persistentvolumeclaims.PersistentVolumeClaim(customResource.Namespace, existing, tempateClaim)
		pvcArray = append(pvcArray, *current)
	}

	for _, epvc := range customResource.Spec.DeploymentPlan.ExtraVolumeClaimTemplates {
		existing = findExistingByName(spec.VolumeClaimTemplates, &epvc)
		current = persistentvolumeclaims.PersistentVolumeClaim(customResource.Namespace, existing, &epvc)
		pvcArray = append(pvcArray, *current)
	}

	for index := range pvcArray {
		if err := reconciler.applyTemplates(&pvcArray[index]); err != nil {
			return nil, err
		}
	}

	if len(pvcArray) > 0 {
		return pvcArray, nil
	}
	return nil, nil
}

func MakeEnvVarArrayForCRForBroker(customResource *v1beta2.Broker, namer common.Namers) []corev1.EnvVar {

	var requireLogin string
	if customResource.Spec.DeploymentPlan.RequireLogin {
		requireLogin = "true"
	} else {
		requireLogin = "false"
	}

	var journalType string
	if strings.ToLower(customResource.Spec.DeploymentPlan.JournalType) == "aio" {
		journalType = "aio"
	} else {
		journalType = "nio"
	}

	var jolokiaAgentEnabled string
	if customResource.Spec.DeploymentPlan.JolokiaAgentEnabled {
		jolokiaAgentEnabled = "true"
	} else {
		jolokiaAgentEnabled = "false"
	}

	var managementRBACEnabled string
	if customResource.Spec.DeploymentPlan.ManagementRBACEnabled {
		managementRBACEnabled = "true"
	} else {
		managementRBACEnabled = "false"
	}

	var metricsPluginEnabled string
	if customResource.Spec.DeploymentPlan.EnableMetricsPlugin != nil {
		metricsPluginEnabled = strconv.FormatBool(*customResource.Spec.DeploymentPlan.EnableMetricsPlugin)
	}

	envVar := []corev1.EnvVar{}
	envVarArrayForBasic := environments.AddEnvVarForBasic(requireLogin, journalType, namer.SvcPingNameBuilder.Name())
	envVar = append(envVar, envVarArrayForBasic...)
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		envVarArrayForPresistent := environments.AddEnvVarForPersistent(customResource.Name)
		envVar = append(envVar, envVarArrayForPresistent...)
	}

	envVarArrayForCluster := environments.AddEnvVarForCluster(false)
	envVar = append(envVar, envVarArrayForCluster...)

	envVarArrayForJolokia := environments.AddEnvVarForJolokia(jolokiaAgentEnabled)
	envVar = append(envVar, envVarArrayForJolokia...)

	envVarArrayForManagement := environments.AddEnvVarForManagement(managementRBACEnabled)
	envVar = append(envVar, envVarArrayForManagement...)

	envVarArrayForMetricsPlugin := environments.AddEnvVarForMetricsPlugin(metricsPluginEnabled)
	envVar = append(envVar, envVarArrayForMetricsPlugin...)

	// Env from CR will override
	envVar = environments.ReplaceOrAppend(envVar, customResource.Spec.Env...)

	return envVar
}

func (reconciler *BrokerReconcilerImpl) ProcessBrokerStatus(cr *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme) (retry bool) {
	var condition metav1.Condition

	err := AssertBrokersAvailableForBroker(cr)
	if err != nil {
		condition = trapErrorAsCondition(err, v1beta2.ConfigAppliedConditionType)
		meta.SetStatusCondition(&cr.Status.Conditions, condition)
		retry = retry || err.Requeue()
		return retry
	}

	err = reconciler.AssertBrokerImageVersion(cr, client)
	if err == nil {
		condition = metav1.Condition{
			Type:   v1beta2.BrokerVersionAlignedConditionType,
			Status: metav1.ConditionTrue,
			Reason: v1beta2.BrokerVersionAlignedConditionMatchReason,
		}
	} else {
		condition = trapErrorAsCondition(err, v1beta2.BrokerVersionAlignedConditionType)
		retry = retry || err.Requeue()
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	err = reconciler.AssertBrokerPropertiesStatus(cr, client, scheme)
	if err == nil {
		condition = metav1.Condition{
			Type:   v1beta2.ConfigAppliedConditionType,
			Status: metav1.ConditionTrue,
			Reason: v1beta2.ConfigAppliedConditionSynchedReason,
		}
	} else {
		condition = trapErrorAsCondition(err, v1beta2.ConfigAppliedConditionType)
		retry = retry || err.Requeue()
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	if _, _, found := getConfigExtraMountForBroker(cr, jaasConfigSuffix); found {
		err = reconciler.AssertJaasPropertiesStatus(cr, client, scheme)
		if err == nil {
			condition = metav1.Condition{
				Type:   v1beta2.JaasConfigAppliedConditionType,
				Status: metav1.ConditionTrue,
				Reason: v1beta2.ConfigAppliedConditionSynchedReason,
			}
		} else {
			condition = trapErrorAsCondition(err, v1beta2.JaasConfigAppliedConditionType)
			retry = retry || err.Requeue()
		}

		meta.SetStatusCondition(&cr.Status.Conditions, condition)
	}

	return retry
}

func AssertBrokersAvailableForBroker(cr *v1beta2.Broker) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	// pre-condition, we must be deployed, avoid broker status roundtrip till ready
	DeployedCondition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.DeployedConditionType)
	if DeployedCondition == nil || DeployedCondition.Status == metav1.ConditionFalse {
		reqLogger.V(2).Info("There are no available brokers from DeployedCondition", "condition", DeployedCondition)
		return NewArtemisStatusError(errors.New("no available brokers from deployed condition"), false)
	}
	return nil
}

func (reconciler *BrokerReconcilerImpl) AssertBrokerPropertiesStatus(cr *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	secretProjection, err := reconciler.getSecretProjection(getPropertiesResourceNsNameForBroker(cr), client)
	if err != nil {
		reqLogger.V(2).Info("error retrieving config resources.")
		return NewArtemisStatusError(err, false)
	}

	errorStatus := reconciler.checkProjectionStatus(cr, client, secretProjection, func(BrokerStatus *brokerStatus, FileName string) (propertiesStatus, bool) {
		current, present := BrokerStatus.BrokerConfigStatus.PropertiesStatus[FileName]
		return current, present
	})

	if errorStatus == nil {
		for _, extraSecretName := range cr.Spec.DeploymentPlan.ExtraMounts.Secrets {
			if strings.HasSuffix(extraSecretName, common.BrokerPropsSuffix) {
				secretProjection, err = reconciler.getSecretProjection(types.NamespacedName{Name: extraSecretName, Namespace: cr.Namespace}, client)
				if err != nil {
					reqLogger.V(2).Info("error retrieving -bp extra mount resource.")
					return NewArtemisStatusError(err, false)
				}
				errorStatus = reconciler.checkProjectionStatus(cr, client, secretProjection, func(BrokerStatus *brokerStatus, FileName string) (propertiesStatus, bool) {
					current, present := BrokerStatus.BrokerConfigStatus.PropertiesStatus[FileName]
					return current, present
				})
				if errorStatus == nil {
					updateExtraConfigStatusForBroker(cr, secretProjection)
				} else {
					// report the first error
					break
				}
			}
		}
	}

	return errorStatus
}

func (reconciler *BrokerReconcilerImpl) AssertJaasPropertiesStatus(cr *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	Projection, err := reconciler.getConfigMappedJaasProperties(cr, client)
	if err != nil {
		reqLogger.V(2).Info("error retrieving config resources.")
		return NewArtemisStatusError(err, false)
	}

	statusError := reconciler.checkProjectionStatus(cr, client, Projection, func(BrokerStatus *brokerStatus, FileName string) (propertiesStatus, bool) {
		current, present := BrokerStatus.ServerStatus.Jaas.PropertiesStatus[FileName]
		return current, present
	})

	if statusError == nil {
		updateExtraConfigStatusForBroker(cr, Projection)
	}

	return statusError
}

func (reconciler *BrokerReconcilerImpl) AssertBrokerImageVersion(cr *v1beta2.Broker, client rtclient.Client) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	// The ResolveBrokerVersionFromCR should never fail because validation succeeded
	resolvedFullVersion, _ := brokerversion.ResolveBrokerVersionFromCR(cr)

	statusError := reconciler.CheckStatus(cr, client, func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {

		if brokerStatus.ServerStatus.Version != resolvedFullVersion {
			err := errors.Errorf("broker version non aligned on pod %s-%s, the detected version [%s] doesn't match the spec.version [%s] resolved as [%s]",
				namer.CrToSS(cr.Name), jk.Ordinal, brokerStatus.ServerStatus.Version, cr.Spec.Version, resolvedFullVersion)
			reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", cr.Spec.Version)
			return NewVersionMismatchError(err)
		}

		return nil
	})

	return statusError
}

func (reconciler *BrokerReconcilerImpl) CheckStatus(cr *v1beta2.Broker, client rtclient.Client, checkBrokerStatus func(BrokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError) ArtemisError {

	reconciler.resolveJolokiaEndpoints(cr, client)

	if len(reconciler.jolokiaEndpoints) == 0 {
		reconciler.log.V(1).Info("no Jolokia Clients available. requeing")
		return NewJolokiaClientsNotFoundError(errors.New("Waiting for Jolokia Clients to become available"))
	}

	return reconciler.CheckStatusFromJolokia(reconciler.jolokiaEndpoints[0], checkBrokerStatus)
}

func (reconciler *BrokerReconcilerImpl) CheckStatusFromJolokia(jk *jolokia_client.JkInfo, checkBrokerStatus func(BrokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError) ArtemisError {

	brokerStatus, artemisError := reconciler.GetAndCacheBrokerStatus(jk)
	if artemisError != nil {
		return artemisError
	}

	artemisError = checkBrokerStatus(brokerStatus, jk)
	if artemisError != nil {
		return artemisError
	}
	return nil
}

func (reconciler *BrokerReconcilerImpl) GetAndCacheBrokerStatus(jk *jolokia_client.JkInfo) (*brokerStatus, ArtemisError) {

	if cached, exists := reconciler.cachedBrokerStatus[jk.Ordinal]; exists {
		switch v := cached.(type) {
		case ArtemisError:
			return nil, v
		case brokerStatus:
			return &v, nil
		}
	}

	currentJSON, err := jk.Artemis.GetStatus()

	if err != nil {
		reconciler.log.V(1).Info("error getting broker status with Jolokia", "IP", jk.IP, "Ordinal", jk.Ordinal, "error", err)
		artemisError := NewArtemisStatusError(err, true)
		reconciler.cachedBrokerStatus[jk.Ordinal] = artemisError
		return nil, artemisError
	}

	reconciler.log.V(2).Info("raw json status", "IP", jk.IP, "ordinal", jk.Ordinal, "status json", currentJSON)

	brokerStatus, err := unmarshallStatus(currentJSON)
	if err != nil {
		reconciler.log.Error(err, "unable to unmarshall broker status", "json", currentJSON)
		artemisError := NewArtemisStatusError(err, false)
		reconciler.cachedBrokerStatus[jk.Ordinal] = artemisError
		return nil, artemisError
	}

	reconciler.log.V(2).Info("cached broker status", "ordinal", jk.Ordinal, "status", brokerStatus)
	reconciler.cachedBrokerStatus[jk.Ordinal] = brokerStatus

	return &brokerStatus, nil

}

func (reconciler *BrokerReconcilerImpl) resolveJolokiaEndpoints(cr *v1beta2.Broker, client rtclient.Client) {
	if reconciler.jolokiaEndpoints == nil {
		reconciler.jolokiaEndpoints = jolokia_client.GetMinimalJolokiaAgentsForBroker(cr, client)
	}
}

func (reconciler *BrokerReconcilerImpl) checkProjectionStatus(cr *v1beta2.Broker, client rtclient.Client, secretProjection *projection, extractStatus func(BrokerStatus *brokerStatus, FileName string) (propertiesStatus, bool)) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	reqLogger.V(2).Info("in sync check", "projection", secretProjection)

	checkErr := reconciler.CheckStatus(cr, client, func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {

		var current propertiesStatus
		var present bool
		var err error
		missingKeys := []string{}
		var applyError *inSyncApplyError = nil

		for name, file := range secretProjection.Files {

			current, present = extractStatus(brokerStatus, name)

			if !present {
				// with ordinal prefix or extras in the map this can be the case
				matches := ParseBrokerPropertyWithOrdinal(name)
				if name != JaasConfigKey && !strings.HasPrefix(name, UncheckedPrefix) && len(matches) == 0 {
					missingKeys = append(missingKeys, name)
				}
				continue
			}

			if current.Alder32 == "" && current.FileAlder32 == "" {
				err = errors.Errorf("out of sync on pod %s-%s, property file %s has an empty checksum",
					namer.CrToSS(cr.Name), jk.Ordinal, name)
				reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
				return NewStatusOutOfSyncError(err)
			}

			if current.FileAlder32 != "" {
				if file.FileAlder32 != current.FileAlder32 {
					err = errors.Errorf("out of sync on pod %s-%s, mismatched file checksum on property file %s, expected: %s, current: %s. A delay can occur before a volume mount projection is refreshed.",
						namer.CrToSS(cr.Name), jk.Ordinal, name, file.FileAlder32, current.FileAlder32)
					reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
					return NewStatusOutOfSyncError(err)
				}
			} else if file.Alder32 != current.Alder32 {
				err = errors.Errorf("out of sync on pod %s-%s, mismatched checksum on property file %s, expected: %s, current: %s. A delay can occur before a volume mount projection is refreshed.",
					namer.CrToSS(cr.Name), jk.Ordinal, name, file.Alder32, current.Alder32)
				reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
				return NewStatusOutOfSyncError(err)
			}

			// check for apply errors
			if len(current.ApplyErrors) > 0 {
				// some props did not apply for k
				if applyError == nil {
					applyError = NewInSyncWithError(secretProjection, fmt.Sprintf("%s-%s", namer.CrToSS(cr.Name), jk.Ordinal))
				}
				applyError.ErrorApplyDetail(name, marshallApplyErrors(current.ApplyErrors))
			}
		}

		if applyError != nil {
			reqLogger.V(1).Info("in sync with apply error", "error", applyError)
			return *applyError
		}

		if len(missingKeys) > 0 {
			// sort missingKeys to generate a stable error message because it is used to update
			// the config applied conditions and unstable messages cause unnecessaray resource updates
			sort.Strings(missingKeys)

			if strings.HasSuffix(secretProjection.Name, jaasConfigSuffix) {
				err = errors.Errorf("out of sync on pod %s-%s, property files are not visible on the broker: %v. Reloadable JAAS LoginModule property files are only visible after the first login attempt that references them. If the property files are for a third party LoginModule or not reloadable, prefix the property file names with an underscore to exclude them from this condition",
					namer.CrToSS(cr.Name), jk.Ordinal, missingKeys)
			} else {
				err = errors.Errorf("out of sync on pod %s-%s, configuration property files are not visible on the broker: %v. A delay can occur before a volume mount projection is refreshed.",
					namer.CrToSS(cr.Name), jk.Ordinal, missingKeys)
			}
			reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
			return NewStatusOutOfSyncMissingKeyError(err)
		}

		// this oridinal is happy
		secretProjection.Ordinals = append(secretProjection.Ordinals, jk.Ordinal)

		return nil
	})

	if checkErr != nil {
		return checkErr
	}

	reqLogger.V(1).Info("successfully synced with brokers", "status", statusMessageFromProjection(secretProjection))

	return nil
}

func updateExtraConfigStatusForBroker(cr *v1beta2.Broker, Projection *projection) {
	if len(cr.Status.ExternalConfigs) > 0 {
		for index, s := range cr.Status.ExternalConfigs {
			if s.Name == Projection.Name {
				cr.Status.ExternalConfigs[index].ResourceVersion = Projection.ResourceVersion
				return // update complete
			}
		}
	}

	// add an entry
	cr.Status.ExternalConfigs = append(cr.Status.ExternalConfigs,
		v1beta2.ExternalConfigStatus{Name: Projection.Name, ResourceVersion: Projection.ResourceVersion})
}

func (reconciler *BrokerReconcilerImpl) getSecretProjection(secretName types.NamespacedName, client rtclient.Client) (*projection, error) {
	resource := &corev1.Secret{}

	// check our latest desired content
	desired := reconciler.getFromDesired(reflect.TypeFor[*corev1.Secret](), secretName.Name)
	if desired != nil {
		resource = desired.(*corev1.Secret)
	} else {
		err := client.Get(context.TODO(), secretName, resource)
		if err != nil {
			return nil, errors.Wrap(err, "unable to retrieve secret projection")
		}
	}
	return newProjectionFromByteValues(resource.ObjectMeta, resource.Data), nil
}

func (reconciler *BrokerReconcilerImpl) getConfigMappedJaasProperties(cr *v1beta2.Broker, client rtclient.Client) (*projection, error) {
	if _, name, found := getConfigExtraMountForBroker(cr, jaasConfigSuffix); found {
		return reconciler.getSecretProjection(types.NamespacedName{Namespace: cr.Namespace, Name: name}, client)
	}
	return nil, nil
}

func (reconciler *BrokerReconcilerImpl) validate(customResource *v1beta2.Broker, client rtclient.Client) (bool, retry bool) {
	validationCondition := metav1.Condition{
		Type:   v1beta2.ValidConditionType,
		Status: metav1.ConditionTrue,
		Reason: v1beta2.ValidConditionSuccessReason,
	}

	condition, retry := validateExtraMountsForBroker(customResource, client)
	if condition != nil {
		validationCondition = *condition
	}

	if validationCondition.Status != metav1.ConditionFalse && customResource.Spec.DeploymentPlan.PodDisruptionBudget != nil {
		condition := validatePodDisruptionForBroker(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = validateNoDupKeysInBrokerPropertiesForBroker(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = reconciler.validateStorage()
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition := brokerversion.ValidateBrokerImageVersion(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition := validateReservedLabelsForBroker(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = validateEnvVarsForBroker(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = reconciler.validateRequiredSecrets(client)
		if condition != nil {
			validationCondition = *condition
		}
	}
	brokerstatus.SetStatusConditionWithGeneration(customResource, validationCondition)

	return validationCondition.Status != metav1.ConditionFalse, retry
}

func validateNoDupKeysInBrokerPropertiesForBroker(customResource *v1beta2.Broker) (*metav1.Condition, bool) {
	if len(customResource.Spec.BrokerProperties) > 0 {
		if duplicateKey := DuplicateKeyIn(customResource.Spec.BrokerProperties); duplicateKey != "" {
			return &metav1.Condition{
				Type:    v1beta2.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey,
				Message: fmt.Sprintf(".Spec.BrokerProperties has a duplicate key for %v", duplicateKey),
			}, false
		}

	}
	return nil, false
}

func validateReservedLabelsForBroker(customResource *v1beta2.Broker) *metav1.Condition {
	if customResource.Spec.DeploymentPlan.Labels != nil {
		for key := range customResource.Spec.DeploymentPlan.Labels {
			if key == selectors.LabelAppKey || key == selectors.LabelBrokerKey {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionFailedReservedLabelReason,
					Message: fmt.Sprintf("'%s' is a reserved label, it is not allowed in Spec.DeploymentPlan.Labels", key),
				}
			}
		}
	}
	for index, template := range customResource.Spec.ResourceTemplates {
		for key := range template.Labels {
			if key == selectors.LabelAppKey || key == selectors.LabelBrokerKey {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionFailedReservedLabelReason,
					Message: fmt.Sprintf("'%s' is a reserved label, it is not allowed in Spec.DeploymentPlan.Templates[%d].Labels", key, index),
				}
			}
		}
	}
	return nil
}

func validateEnvVarsForBroker(customResource *v1beta2.Broker) (*metav1.Condition, bool) {

	internalVarNames := map[string]string{
		debugArgsEnvVarName:      debugArgsEnvVarName,
		javaOptsEnvVarName:       javaOptsEnvVarName,
		javaArgsAppendEnvVarName: javaArgsAppendEnvVarName,
	}

	invalidVars := []string{}

	for _, envVar := range customResource.Spec.Env {
		if _, ok := internalVarNames[envVar.Name]; ok {
			if envVar.ValueFrom != nil {
				invalidVars = append(invalidVars, envVar.Name)
			}
		}
	}

	if len(invalidVars) > 0 {
		return &metav1.Condition{
			Type:    v1beta2.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.ValidConditionInvalidInternalVarUsage,
			Message: fmt.Sprintf("Don't use valueFrom on env vars that the operator can mutate: %v. Instead use a different var and refernece it in its value field.", invalidVars),
		}, false
	}
	return nil, false
}

func (reconciler *BrokerReconcilerImpl) validateRequiredSecrets(client rtclient.Client) (*metav1.Condition, bool) {
	retry := true
	if _, err := common.GetOperatorClientCertSecret(client); err != nil {
		return &metav1.Condition{
			Type:    v1beta2.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.ValidConditionMissingResourcesReason,
			Message: fmt.Sprintf("operator failed to locate necessary operator client certificate secret, %v", err),
		}, retry
	}
	if _, err := common.GetOperatorCASecret(client); err != nil {
		return &metav1.Condition{
			Type:    v1beta2.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.ValidConditionMissingResourcesReason,
			Message: fmt.Sprintf("operator failed to locate necessary operator ca secret, %v", err),
		}, retry
	}
	operandCertSecretName := common.GetOperandCertSecretName(reconciler.customResource, client)
	if _, err := common.GetNamespacedSecret(client, operandCertSecretName, reconciler.customResource.Namespace); err != nil {
		return &metav1.Condition{
			Type:    v1beta2.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.ValidConditionMissingResourcesReason,
			Message: fmt.Sprintf("operator failed to locate necessary operand cert secret, %v", err),
		}, retry
	}
	return nil, false
}

func (reconciler *BrokerReconcilerImpl) validateStorage() (*metav1.Condition, bool) {

	if reconciler.customResource.Spec.DeploymentPlan.PersistenceEnabled {
		if reconciler.customResource.Spec.DeploymentPlan.Storage.Size != "" {
			_, err := resource.ParseQuantity(reconciler.customResource.Spec.DeploymentPlan.Storage.Size)
			if err != nil {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionFailureReason,
					Message: fmt.Sprintf(".Spec.DeploymentPlan.Storage.Size quantity string is invalid, %v", err),
				}, false
			}
		}
	}
	return nil, false
}

func validatePodDisruptionForBroker(customResource *v1beta2.Broker) *metav1.Condition {
	pdb := customResource.Spec.DeploymentPlan.PodDisruptionBudget
	if pdb.Selector != nil {
		return &metav1.Condition{
			Type:    v1beta2.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.ValidConditionPDBNonNilSelectorReason,
			Message: common.PDBNonNilSelectorMessage,
		}
	}
	return nil
}

func validateExtraMountsForBroker(customResource *v1beta2.Broker, client rtclient.Client) (*metav1.Condition, bool) {

	instanceCounts := map[string]int{}
	var Condition *metav1.Condition
	var retry = true
	var ContextMessage = ".Spec.DeploymentPlan.ExtraMounts.ConfigMaps,"
	for _, cm := range customResource.Spec.DeploymentPlan.ExtraMounts.ConfigMaps {
		configMap := corev1.ConfigMap{}
		found := retrieveResource(cm, customResource.Namespace, &configMap, client)
		if !found {
			return &metav1.Condition{
				Type:    v1beta2.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  v1beta2.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf("%v missing required configMap %v", ContextMessage, cm),
			}, retry
		}
		if strings.HasSuffix(cm, loggingConfigSuffix) {
			Condition = AssertConfigMapContainsKey(configMap, LoggingConfigKey, ContextMessage)
			instanceCounts[loggingConfigSuffix]++
		} else if strings.HasSuffix(cm, jaasConfigSuffix) {
			Condition = &metav1.Condition{
				Type:    v1beta2.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  v1beta2.ValidConditionFailedExtraMountReason,
				Message: fmt.Sprintf("%v entry %v with suffix %v must be a secret", ContextMessage, cm, jaasConfigSuffix),
			}
			retry = false // Cr needs an update
		}
		if Condition != nil {
			return Condition, retry
		}
	}

	ContextMessage = ".Spec.DeploymentPlan.ExtraMounts.Secrets,"
	for _, s := range customResource.Spec.DeploymentPlan.ExtraMounts.Secrets {
		secret := corev1.Secret{}
		found := retrieveResource(s, customResource.Namespace, &secret, client)
		if !found {
			return &metav1.Condition{
				Type:    v1beta2.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  v1beta2.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf("%v missing required secret %v", ContextMessage, s),
			}, retry
		}
		if strings.HasSuffix(s, loggingConfigSuffix) {
			Condition = AssertSecretContainsKey(secret, LoggingConfigKey, ContextMessage)
			instanceCounts[loggingConfigSuffix]++
		} else if strings.HasSuffix(s, jaasConfigSuffix) {
			Condition = AssertSecretContainsKey(secret, JaasConfigKey, ContextMessage)
			if Condition == nil {
				Condition = AssertSyntaxOkOnLoginConfigData(secret.Data[JaasConfigKey], s, ContextMessage)
			}
			instanceCounts[jaasConfigSuffix]++
		} else if strings.HasSuffix(s, common.BrokerPropsSuffix) {
			Condition = AssertNoDupKeyInProperties(secret, ContextMessage)
		}
		if Condition != nil {
			return Condition, retry
		}
	}
	Condition = AssertInstanceCounts(instanceCounts)
	if Condition != nil {
		return Condition, false // CR needs update
	}

	return nil, false
}

func hasExtraMountsForBroker(cr *v1beta2.Broker) bool {
	if cr == nil {
		return false
	}
	if len(cr.Spec.DeploymentPlan.ExtraMounts.ConfigMaps) > 0 {
		return true
	}
	return len(cr.Spec.DeploymentPlan.ExtraMounts.Secrets) > 0
}

func MakeNamersForBroker(customResource *v1beta2.Broker) *common.Namers {
	newNamers := common.Namers{
		SsGlobalName:                  "",
		SsNameBuilder:                 namer.NamerData{},
		SvcHeadlessNameBuilder:        namer.NamerData{},
		SvcPingNameBuilder:            namer.NamerData{},
		PodsNameBuilder:               namer.NamerData{},
		SecretsCredentialsNameBuilder: namer.NamerData{},
		SecretsConsoleNameBuilder:     namer.NamerData{},
		SecretsNettyNameBuilder:       namer.NamerData{},
		LabelBuilder:                  selectors.LabelerData{},
		GLOBAL_DATA_PATH:              "/opt/" + customResource.Name + "/data",
	}
	newNamers.SsNameBuilder.Base(customResource.Name).Suffix("ss").Generate()
	newNamers.SsGlobalName = customResource.Name
	newNamers.SvcHeadlessNameBuilder.Prefix(customResource.Name).Base("hdls").Suffix("svc").Generate()
	newNamers.SvcPingNameBuilder.Prefix(customResource.Name).Base("ping").Suffix("svc").Generate()
	newNamers.PodsNameBuilder.Base(customResource.Name).Suffix("container").Generate()
	newNamers.SecretsCredentialsNameBuilder.Prefix(customResource.Name).Base("credentials").Suffix("secret").Generate()
	newNamers.SecretsConsoleNameBuilder.Prefix(customResource.Name).Base("console").Suffix("secret").Generate()
	newNamers.SecretsNettyNameBuilder.Prefix(customResource.Name).Base("netty").Suffix("secret").Generate()

	newNamers.LabelBuilder.Base(customResource.Name).Suffix("app").ResourceKey(selectors.LabelBrokerKey).Generate()

	return &newNamers
}

func GetDefaultLabelsForBroker(cr *v1beta2.Broker) map[string]string {
	defaultLabelData := selectors.LabelerData{}
	defaultLabelData.Base(cr.Name).Suffix("app").ResourceKey(selectors.LabelBrokerKey).Generate()
	return defaultLabelData.Labels()
}

// Controller Errors
