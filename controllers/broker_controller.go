// Package controllers implements Kubernetes controllers for broker resources.
package controllers

import (
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"sort"

	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	brokerjolokia "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/broker/jolokia"
	brokerstatus "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/broker/status"
	brokerversion "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/broker/version"
	brokerproperties "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokervolumes"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/containers"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/persistentvolumeclaims"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/pods"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/serviceports"
	ss "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/statefulsets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
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

	"sigs.k8s.io/controller-runtime/pkg/builder"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/volumes"

	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	routev1 "github.com/openshift/api/route/v1"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"

	"strconv"
	"strings"

	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
)

//go:embed broker_status_script.sh
var brokerStatusScript string

const (
	brokerStatusScriptSuffix  = "-status-script"
	brokerStatusScriptKey     = "broker-status.sh"
	brokerLoggingConfigSuffix = "-logging-config"
	sidecarContainerName      = "broker-status"

	log4j2ConfigurationFileFlag = "-Dlog4j2.configurationFile="
	defaultLog4j2ConfigURI      = "classpath:log4j2.properties"

	// ReconcileMeAnnotationKey is the Pod annotation the sidecar script writes
	// when it detects AMQ221007 (server active) or AMQ221087 (config reload
	// completed). The operator watches for this annotation and triggers a
	// reconcile + Jolokia status fetch.
	ReconcileMeAnnotationKey = "broker.arkmq.org/request-reconcile"
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
//+kubebuilder:rbac:groups="",namespace=arkmq-org-broker-operator,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=arkmq-org-broker-operator,resources=roles;rolebindings,verbs=create;delete;get;list;patch;watch

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
		reconciler.ProcessBrokerStatus(customResource, r.Client, r.Scheme)
	}

	brokerstatus.UpdateBlockedStatus(customResource, reconcileBlocked)
	brokerstatus.ProcessStatus(customResource, r.Client, namespacedName, *namer, err)

	crStatusUpdateErr := r.UpdateBrokerCRStatus(customResource, r.Client, namespacedName)
	if crStatusUpdateErr != nil {
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
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&netv1.Ingress{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.mapPodToBrokerCR),
			builder.WithPredicates(reconcileMeAnnotationPredicate()),
		)

	if r.isOnOpenShift {
		builder.Owns(&routev1.Route{})
	}

	return builder.Complete(r)
}

// reconcileMeAnnotationPredicate filters Pod events to only those where the
// request-reconcile annotation was added or changed.
func reconcileMeAnnotationPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, exists := e.Object.GetAnnotations()[ReconcileMeAnnotationKey]
			return exists
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldVal := e.ObjectOld.GetAnnotations()[ReconcileMeAnnotationKey]
			newVal := e.ObjectNew.GetAnnotations()[ReconcileMeAnnotationKey]
			return newVal != "" && newVal != oldVal
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// mapPodToBrokerCR maps a Pod event to the owning Broker CR by traversing the
// ownership chain: Pod -> StatefulSet -> Broker CR. All lookups hit the
// informer cache (no API round-trips).
func (r *BrokerReconciler) mapPodToBrokerCR(ctx context.Context, obj rtclient.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}

	for _, ownerRef := range pod.GetOwnerReferences() {
		if ownerRef.Kind != "StatefulSet" {
			continue
		}
		ss := &appsv1.StatefulSet{}
		if err := r.Get(ctx, types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      ownerRef.Name,
		}, ss); err != nil {
			return nil
		}
		for _, ssOwner := range ss.GetOwnerReferences() {
			if ssOwner.Kind == "Broker" {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: pod.Namespace,
						Name:      ssOwner.Name,
					},
				}}
			}
		}
	}
	return nil
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
	if s1.ScaleLabelSelector != s2.ScaleLabelSelector ||
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
	statusClient       *brokerjolokia.StatusClient
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

func withCRContext(logger logr.Logger, name, namespace string) logr.Logger {
	return logger.WithValues("CRD.Name", name, "CRD.Namespace", namespace)
}

func NewBrokerReconcilerImpl(customResource *v1beta2.Broker, parent *BrokerReconciler) *BrokerReconcilerImpl {
	return &BrokerReconcilerImpl{
		log:                withCRContext(parent.log, customResource.Name, customResource.Namespace),
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
	reconciler.log.V(2).Info("Reconciler Processing...", "CRD ver", customResource.ResourceVersion, "CRD Gen", customResource.Generation)

	reconciler.CurrentDeployedResources(customResource, client)

	reconciler.ensureStatusScriptConfigMap()
	reconciler.ensureReloadLoggingConfigMap()
	if err := reconciler.ensureStatusScriptRBAC(client); err != nil {
		return err
	}

	// currentStateful Set is a clone of what exists if already deployed
	// what follows should transform the resources using the crd
	// if the transformation results in some change, process resources will respect that
	// comparisons should not be necessary, leave that to process resources
	desiredStatefulSet, err := reconciler.ProcessStatefulSet(customResource, namer, client)
	if err != nil {
		//reconciler.log.Error(err, "Error processing stafulset")
		return fmt.Errorf("failed to process stateful set, %w", err)
	}

	requestedReplicas := common.DefaultDeploymentSize
	desiredStatefulSet.Spec.Replicas = &requestedReplicas
	if customResource.Spec.PodDisruptionBudget != nil {
		reconciler.applyPodDisruptionBudget(customResource)
	}

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

	// we don't requeue
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

	if customResource.Spec.RevisionHistoryLimit != nil {
		currentStatefulSet.Spec.RevisionHistoryLimit = customResource.Spec.RevisionHistoryLimit
	}
	return currentStatefulSet, nil
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
	desired.Spec = *customResource.Spec.PodDisruptionBudget.DeepCopy()
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
	var err error
	if customResource.Spec.PersistenceEnabled {
		pvcKey := types.NamespacedName{
			Namespace: customResource.Namespace,
			Name:      customResource.Name + "-" + namer.CrToSS(customResource.Name) + "-0",
		}
		brokervolumes.RemovePVCOwnerRef(pvcKey, customResource.UID, client, reconciler.log)
	}

	reconciler.deployed, err = brokerstatus.GetDeployedResources(customResource, client, reconciler.isOnOpenShift)
	if err != nil {
		reconciler.log.Error(err, "error getting deployed resources")
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
			reconciler.log.V(2).Info("Deployed ", "Type", t, "Name", obj.GetName())
		}
	}
}

func (reconciler *BrokerReconcilerImpl) ProcessResources(customResource *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme) (err error) {

	for _, requested := range common.ToResourceList(reconciler.requestedResources) {
		requested.SetNamespace(customResource.Namespace)
		if err = reconciler.applyTemplates(requested); err != nil {
			return err
		}
	}

	reconciler.log.V(1).Info("Processing resources", "num requested", countOfRequestedBroker(reconciler), "num current", countOfDeployedBroker(reconciler))

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
		reconciler.log.V(1).Info("", "instances of ", resourceType, "Will create ", len(delta.Added), "update ", len(delta.Updated), "and delete", len(delta.Removed))

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

func MakeContainerPortsForBroker(cr *v1beta2.Broker) []corev1.ContainerPort {

	containerPorts := []corev1.ContainerPort{
		{
			Name:          "jolokia",
			ContainerPort: 8778,
			Protocol:      "TCP",
		},
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
	if customResource.Spec.Labels != nil {
		maps.Copy(labels, customResource.Spec.Labels)
	}

	pts := pods.MakePodTemplateSpec(current, namespacedName, labels, customResource.Spec.Annotations)
	podSpec := &pts.Spec

	podSpec.ImagePullSecrets = customResource.Spec.ImagePullSecrets

	container := containers.MakeContainer(podSpec, customResource.Name, brokerversion.ResolveImage(customResource, common.BrokerImageKey), MakeEnvVarArrayForCRForBroker(customResource, namer))

	container.Resources = customResource.Spec.Resources

	reconciler.configureContainerSecurityContext(container, customResource.Spec.ContainerSecurityContext)

	container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError

	containerPorts := MakeContainerPortsForBroker(customResource)
	if len(containerPorts) > 0 {
		reqLogger.V(1).Info("Adding new ports to main", "len", len(containerPorts))
		container.Ports = containerPorts
	}

	reqLogger.V(2).Info("Checking out extraMounts", "extra config", customResource.Spec.ExtraMounts)

	configMapsToMount := customResource.Spec.ExtraMounts.ConfigMaps
	secretsToMount := customResource.Spec.ExtraMounts.Secrets
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
		securityProperties := brokerproperties.NewPropsWithHeader()
		fmt.Fprintf(securityProperties, "login.config.url.1=file:%s/login.config\n", mountPathRoot)
		fmt.Fprintf(securityProperties, "security.provider.13=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")
		fmt.Fprintf(securityProperties, "fips.provider.8=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")

		brokerPropertiesMapData["_security.config"] = securityProperties.Bytes()

		additionalSystemProps = append(additionalSystemProps, fmt.Sprintf("-Djava.security.properties=%s/_security.config", mountPathRoot))

		loginConfig := brokerproperties.NewBufferWithHeader("//")
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

		certIDs, err := cot.ResolveCertIdentities(customResource, client, getOwningServiceName(customResource))
		if err != nil {
			return nil, err
		}

		// TODO - make configuable
		// support <crNname->control-plane-auth-secret, maybe a suffix for the http_server_authenticator realm login.config

		certUser := brokerproperties.NewPropsWithHeader()
		fmt.Fprintln(certUser, "hawtio=/CN = hawtio-online\\.hawtio\\.svc.*/")
		fmt.Fprintf(certUser, "operator=/.*%s.*/\n", certIDs.OperatorSubject.CommonName) // regexp syntax start and with /
		// can and should use the full DN after https://issues.apache.org/jira/browse/ARTEMIS-5102
		fmt.Fprintf(certUser, "probe=/.*%s.*/\n", certIDs.OperandSubject.CommonName)
		if certIDs.PrometheusSubject != nil {
			fmt.Fprintf(certUser, "prometheus=/.*%s.*/\n", certIDs.PrometheusSubject.CommonName)
		}
		brokerPropertiesMapData[common.GetCertUsersKey(common.HttpAuthenticatorRealm)] = certUser.Bytes()

		certRoles := brokerproperties.NewPropsWithHeader()
		fmt.Fprintln(certRoles, "status=operator,probe")
		fmt.Fprintln(certRoles, "metrics=operator,prometheus")
		fmt.Fprintln(certRoles, "hawtio=hawtio")
		brokerPropertiesMapData[common.GetCertRolesKey(common.HttpAuthenticatorRealm)] = certRoles.Bytes()

		foundationalProps := brokerproperties.NewPropsWithHeader()
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

		rbac := brokerproperties.NewPropsWithHeader()
		// operator status check
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getStatus\".status.view=true")

		// jmx_exporter metrics perms
		fmt.Fprintln(rbac, "securityRoles.\"mops.mbeanserver.queryMBeans\".metrics.view=true")
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker\".metrics.view=true") // we need view permission on the broker in order to locate through a query and retrieve it.
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getTotalMessageCount\".metrics.view=true")
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getTotalMessagesAcknowledged\".metrics.view=true")
		fmt.Fprintln(rbac, "securityRoles.\"mops.broker.getTotalMessagesAdded\".metrics.view=true")

		brokerPropertiesMapData["aa_rbac.properties"] = rbac.Bytes()

		secretsToMount = append(secretsToMount, certIDs.OperandCertSecretName)
		secretsToMount = append(secretsToMount, certIDs.CASecretName)

		jolokiaConfig := brokerproperties.NewPropsWithHeader()
		fmt.Fprintln(jolokiaConfig, "protocol=https")
		fmt.Fprintln(jolokiaConfig, "authClass=org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator")
		fmt.Fprintf(jolokiaConfig, "caCert=%s%s/%s\n", common.SecretPathBase, certIDs.CASecretName, certIDs.CASecretKey)
		fmt.Fprintf(jolokiaConfig, "serverCert=%s%s/tls.crt\n", common.SecretPathBase, certIDs.OperandCertSecretName)
		fmt.Fprintf(jolokiaConfig, "serverKey=%s%s/tls.key\n", common.SecretPathBase, certIDs.OperandCertSecretName)
		fmt.Fprintln(jolokiaConfig, "port=8778")
		// https://github.com/jolokia/jolokia/issues/751 at some point host=$(env:HOSTNAME), host= is on the command line below
		fmt.Fprintln(jolokiaConfig, "useSslClientAuthentication=true")
		fmt.Fprintln(jolokiaConfig, "disabledServices=org.jolokia.service.history.HistoryMBeanRequestInterceptor")
		fmt.Fprintln(jolokiaConfig, "disableDetectors=true")
		fmt.Fprintln(jolokiaConfig, "debug=false")

		brokerPropertiesMapData["_jolokia.config"] = jolokiaConfig.Bytes()

		pemCfg := brokerproperties.NewPropsWithHeader()

		fmt.Fprintf(pemCfg, "alias=alias\n")
		fmt.Fprintf(pemCfg, "source.cert=%s%s/tls.crt\n", common.SecretPathBase, certIDs.OperandCertSecretName)
		fmt.Fprintf(pemCfg, "source.key=%s%s/tls.key\n", common.SecretPathBase, certIDs.OperandCertSecretName)
		brokerPropertiesMapData["_cert.pemcfg"] = pemCfg.Bytes()

		prometheusConfig := brokerproperties.NewPropsWithHeader() // yaml
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
		fmt.Fprintf(prometheusConfig, "      filename: %s%s/%s\n", common.SecretPathBase, certIDs.CASecretName, certIDs.CASecretKey)
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

		if customResource.Spec.LivenessProbe == nil {
			container.LivenessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/bin/bash",
							"-c",
							// use curl with mtls as the broker-cert to pull the status to find start state using dns
							fmt.Sprintf(`export STATEFUL_SET_ORDINAL=${HOSTNAME##*-};curl --cacert %s%s/%s --cert %s%s/tls.crt --key %s%s/tls.key  https://%s:8778/jolokia/read/org.apache.activemq.artemis:broker=%%22%s%%22/Status | grep -w -P "(START|STOPP)(ED|ING)"`, common.SecretPathBase, certIDs.CASecretName, certIDs.CASecretKey, common.SecretPathBase, certIDs.OperandCertSecretName, common.SecretPathBase, certIDs.OperandCertSecretName, common.OrdinalStringFQDNS(customResource.Name, customResource.Namespace, "$STATEFUL_SET_ORDINAL"), environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name)),
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
			container.LivenessProbe = reconciler.configureLivenessProbe(container, customResource.Spec.LivenessProbe)
		}
	}
	extraVolumes, extraVolumeMounts, err := reconciler.createExtraConfigmapsAndSecretsVolumeMounts(configMapsToMount, secretsToMount, brokerPropertiesResourceName, brokerPropertiesMapData, client)
	if err != nil {
		return nil, fmt.Errorf("failed to createExtraConfigmapsAndSecretsVolumeMounts, %w", err)
	}

	reqLogger.V(2).Info("Extra volumes", "volumes", extraVolumes)
	reqLogger.V(2).Info("Extra mounts", "mounts", extraVolumeMounts)

	container.VolumeMounts = brokervolumes.MakeVolumeMounts(customResource.Name, customResource.Spec.ExtraVolumes, customResource.Spec.ExtraVolumeMounts, customResource.Spec.ExtraVolumeClaimTemplates)
	if len(extraVolumeMounts) > 0 {
		container.VolumeMounts = append(container.VolumeMounts, extraVolumeMounts...)
	}

	container.StartupProbe = reconciler.configureStartupProbe(container, customResource.Spec.StartupProbe)
	container.ReadinessProbe = reconciler.configureReadinessProbe(container, customResource.Spec.ReadinessProbe)

	if len(customResource.Spec.NodeSelector) > 0 {
		reqLogger.V(1).Info("Adding Node Selectors", "len", len(customResource.Spec.NodeSelector))
		podSpec.NodeSelector = customResource.Spec.NodeSelector
	}

	reconciler.configureAffinity(podSpec, &customResource.Spec.Affinity)

	if len(customResource.Spec.Tolerations) > 0 {
		reqLogger.V(1).Info("Adding Tolerations", "len", len(customResource.Spec.Tolerations))
		podSpec.Tolerations = customResource.Spec.Tolerations
	}

	statusScriptCMName := customResource.Name + brokerStatusScriptSuffix
	statusScriptCMPath := cfgMapPathBase + statusScriptCMName
	statusScriptVolume := volumes.MakeVolumeForConfigMap(statusScriptCMName)
	statusScriptVolumeMount := volumes.MakeVolumeMountForCfg(statusScriptVolume.Name, statusScriptCMPath, true)
	extraVolumes = append(extraVolumes, statusScriptVolume)

	loggingCMName := customResource.Name + brokerLoggingConfigSuffix
	loggingCMPath := cfgMapPathBase + loggingCMName
	loggingVolume := volumes.MakeVolumeForConfigMap(loggingCMName)
	loggingVolumeMount := volumes.MakeVolumeMountForCfg(loggingVolume.Name, loggingCMPath, true)
	container.VolumeMounts = append(container.VolumeMounts, loggingVolumeMount)
	extraVolumes = append(extraVolumes, loggingVolume)

	newContainersArray := []corev1.Container{}
	podSpec.Containers = append(newContainersArray, *container)
	brokerVolumes := brokervolumes.MakeVolumes(customResource.Name, customResource.Spec.PersistenceEnabled, customResource.Spec.ExtraVolumes, customResource.Spec.ExtraVolumeClaimTemplates)
	if !customResource.Spec.PersistenceEnabled {
		brokerVolumes = append([]corev1.Volume{volumes.MakeEmptyDirVolumeFor(customResource.Name)}, brokerVolumes...)
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
	if jaasConfigPath, found := brokerproperties.GetJaasConfigExtraMountPath(customResource.Spec.ExtraMounts); found {
		debugArgs := corev1.EnvVar{
			Name:  getJaasConfigEnvVarNameForBroker(),
			Value: fmt.Sprintf("-Djava.security.auth.login.config=%v", jaasConfigPath),
		}
		environments.CreateOrAppend(podSpec.Containers, &debugArgs)
	}

	operatorLoggingURI := loggingCMPath + "/" + LoggingConfigKey
	baseLoggingURI := defaultLog4j2ConfigURI
	overlayLog4j2Level := true
	if loggingConfigPath, found := brokerproperties.GetLoggingConfigExtraMountPath(customResource.Spec.ExtraMounts); found {
		baseLoggingURI = loggingConfigPath
		overlayLog4j2Level = false
	}
	applyReloadLoggingJavaOpts(podSpec.Containers, baseLoggingURI, operatorLoggingURI, overlayLog4j2Level)

	// add TopologySpreadConstraints config
	podSpec.TopologySpreadConstraints = customResource.Spec.TopologySpreadConstraints

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

	reconciler.configPodSecurity(podSpec, &customResource.Spec.PodSecurity)
	reconciler.configurePodSecurityContext(podSpec, customResource.Spec.PodSecurityContext)

	pts.Spec = *podSpec
	pts.Spec.InitContainers = nil

	reEvalJdkOpts := generateReEvalOrdinaEnvReplacement(customResource.Spec.Env)

	pts.Spec.Containers[0].Command = []string{
		"/bin/bash", "-c",
		fmt.Sprintf("export STATEFUL_SET_ORDINAL=${HOSTNAME##*-}; %s exec java %s $JAVA_ARGS_APPEND org.apache.activemq.artemis.core.server.embedded.Main", reEvalJdkOpts, strings.Join(additionalSystemProps, " ")),
	}

	// The sidecar reuses the broker image to avoid an additional image pull.
	// Since the main container already pulls this image, all layers are cached
	// on the node — the sidecar rootfs is a zero-cost overlay mount. The
	// command override runs only the status script, not the broker. Both
	// containers share the same pod (network, SA token, volumes), so using
	// the broker image does not widen the attack surface.
	sidecarRestartPolicy := corev1.ContainerRestartPolicyAlways
	runAsNonRoot := true
	sidecarContainer := corev1.Container{
		Name:          sidecarContainerName,
		Image:         brokerversion.ResolveImage(customResource, common.BrokerImageKey),
		Command:       []string{"/bin/bash", statusScriptCMPath + "/" + brokerStatusScriptKey},
		RestartPolicy: &sidecarRestartPolicy,
		Env: []corev1.EnvVar{
			{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"},
			}},
			{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"},
			}},
			{Name: "RELOAD_LOG_PATH", Value: brokervolumes.DataMountPath + "/log/reload.log"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: customResource.Name, MountPath: brokervolumes.DataMountPath, ReadOnly: true},
			statusScriptVolumeMount,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("32Mi"),
				corev1.ResourceCPU:    resource.MustParse("50m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
				corev1.ResourceCPU:    resource.MustParse("100m"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             &runAsNonRoot,
			RunAsUser:                func() *int64 { uid := int64(1000); return &uid }(),
			AllowPrivilegeEscalation: func() *bool { b := false; return &b }(),
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}
	pts.Spec.InitContainers = []corev1.Container{sidecarContainer}

	reqLogger.V(2).Info("Final Init spec", "Detail", pts.Spec.InitContainers)

	return pts, nil
}

// support ${STATEFUL_SET_ORDINAL} replacement in JDK options from CR env if necessary

func getJaasConfigEnvVarNameForBroker() string {
	return jdkJavaOptionsEnvVarName
}

func getLoginConfigEnvVarNameForBroker() string {
	return jdkJavaOptionsEnvVarName
}

// withCompositeLog4j2Config ensures operatorURI is the last file in
// -Dlog4j2.configurationFile. If that property is already present, operatorURI
// is appended to its comma-separated list. Otherwise the property is added as
// baseURI,operatorURI (or just operatorURI when baseURI is empty).
func withCompositeLog4j2Config(existingOpts, baseURI, operatorURI string) string {
	if operatorURI == "" {
		return existingOpts
	}
	if strings.Contains(existingOpts, log4j2ConfigurationFileFlag) {
		return appendLog4j2ConfigurationFileURI(existingOpts, operatorURI)
	}
	configValue := operatorURI
	if baseURI != "" {
		configValue = baseURI + "," + operatorURI
	}
	addition := log4j2ConfigurationFileFlag + configValue
	if strings.TrimSpace(existingOpts) == "" {
		return addition
	}
	return strings.TrimSpace(existingOpts) + " " + addition
}

func appendLog4j2ConfigurationFileURI(opts, uri string) string {
	idx := strings.Index(opts, log4j2ConfigurationFileFlag)
	if idx < 0 {
		return opts
	}
	valueStart := idx + len(log4j2ConfigurationFileFlag)
	valueEnd := valueStart
	for valueEnd < len(opts) && opts[valueEnd] != ' ' && opts[valueEnd] != '\t' {
		valueEnd++
	}
	value := opts[valueStart:valueEnd]
	for _, part := range strings.Split(value, ",") {
		if part == uri {
			return opts
		}
	}
	return opts[:valueStart] + value + "," + uri + opts[valueEnd:]
}

func applyReloadLoggingJavaOpts(containers []corev1.Container, baseURI, operatorURI string, overlayLevelInfo bool) {
	loggingEnvName := getLoginConfigEnvVarNameForBroker()
	jdkOpts := ""
	if existing := environments.Retrieve(containers, loggingEnvName); existing != nil {
		jdkOpts = existing.Value
	}
	javaArgs := ""
	if existing := environments.Retrieve(containers, javaArgsAppendEnvVarName); existing != nil {
		javaArgs = existing.Value
	}

	userOwnsConfigFile := strings.Contains(jdkOpts, log4j2ConfigurationFileFlag) || strings.Contains(javaArgs, log4j2ConfigurationFileFlag)
	if overlayLevelInfo && !userOwnsConfigFile && !strings.Contains(jdkOpts, "-Dlog4j2.level=") {
		if strings.TrimSpace(jdkOpts) == "" {
			jdkOpts = "-Dlog4j2.level=INFO"
		} else {
			jdkOpts = strings.TrimSpace(jdkOpts) + " -Dlog4j2.level=INFO"
		}
	}

	jdkOpts = withCompositeLog4j2Config(jdkOpts, baseURI, operatorURI)
	if existing := environments.Retrieve(containers, loggingEnvName); existing != nil {
		existing.Value = jdkOpts
	} else {
		environments.CreateOrAppend(containers, &corev1.EnvVar{Name: loggingEnvName, Value: jdkOpts})
	}

	if strings.Contains(javaArgs, log4j2ConfigurationFileFlag) {
		if existing := environments.Retrieve(containers, javaArgsAppendEnvVarName); existing != nil {
			existing.Value = withCompositeLog4j2Config(javaArgs, baseURI, operatorURI)
		}
	}
}

func (reconciler *BrokerReconcilerImpl) brokerPropertiesConfigSystemPropValue(mountPoint, resourceName string, brokerPropertiesData map[string][]byte) string {
	var result string
	if len(brokerPropertiesData) == 1 {
		result = fmt.Sprintf("-Dbroker.properties=%s%s/%s", mountPoint, resourceName, BrokerPropertiesName)
	} else {
		result = fmt.Sprintf("-Dbroker.properties=%s%s/,%s%s/%s${STATEFUL_SET_ORDINAL}/", mountPoint, resourceName, mountPoint, resourceName, OrdinalPrefix)
	}

	for _, extraSecretName := range reconciler.customResource.Spec.ExtraMounts.Secrets {
		if strings.HasSuffix(extraSecretName, common.BrokerPropsSuffix) {
			result = fmt.Sprintf("%s,%s%s/,%s%s/%s${STATEFUL_SET_ORDINAL}/", result, common.SecretPathBase, extraSecretName, common.SecretPathBase, extraSecretName, OrdinalPrefix)
		}
	}

	return result
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
	alder32Bytes := brokerproperties.Alder32Of(customResource.Spec.BrokerProperties)
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

	data := brokerproperties.BrokerPropertiesData(reconciler.customResource.Spec.BrokerProperties)

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

func (reconciler *BrokerReconcilerImpl) configureContainerSecurityContext(container *corev1.Container, containerSecurityContext *corev1.SecurityContext) {
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
		// The sidecar script needs the service account token to patch pod annotations via the K8s API
		autoMount := true
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

			if secret == brokePropertiesResourceName && brokerproperties.HasOrdinalPropertiesInData(brokerPropsData) {
				// place ordinal data in subpath in order
				for _, key := range brokerproperties.SortedKeysByteValue(brokerPropsData) {
					matches := brokerproperties.ParseBrokerPropertyWithOrdinal(key)
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

				if len(bpSecret.Data) > 0 && brokerproperties.HasOrdinalPropertiesInData(bpSecret.Data) {
					for _, key := range brokerproperties.SortedKeysByteValue(bpSecret.Data) {
						matches := brokerproperties.ParseBrokerPropertyWithOrdinal(key)
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
	replicas := common.DefaultDeploymentSize
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

	if customResource.Spec.PersistenceEnabled {
		capacity := "2Gi"
		if customResource.Spec.Storage.Size != "" {
			capacity = customResource.Spec.Storage.Size
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
		if customResource.Spec.Storage.StorageClassName != "" {
			tempateClaim.Spec.StorageClassName = &customResource.Spec.Storage.StorageClassName
		}

		existing = findExistingByName(spec.VolumeClaimTemplates, tempateClaim)
		current = persistentvolumeclaims.PersistentVolumeClaim(customResource.Namespace, existing, tempateClaim)
		pvcArray = append(pvcArray, *current)
	}

	for _, epvc := range customResource.Spec.ExtraVolumeClaimTemplates {
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

	const requireLogin = "false"
	const journalType = "nio"
	const jolokiaAgentEnabled = "true"
	const managementRBACEnabled = "true"

	var metricsPluginEnabled string
	if customResource.Spec.EnableMetricsPlugin != nil {
		metricsPluginEnabled = strconv.FormatBool(*customResource.Spec.EnableMetricsPlugin)
	}

	envVar := []corev1.EnvVar{}
	envVarArrayForBasic := environments.AddEnvVarForBasic(requireLogin, journalType, namer.SvcPingNameBuilder.Name())
	envVar = append(envVar, envVarArrayForBasic...)
	if customResource.Spec.PersistenceEnabled {
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

	if _, _, found := brokerproperties.GetConfigExtraMount(cr.Spec.ExtraMounts, jaasConfigSuffix); found {
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
	reqLogger := withCRContext(ctrl.Log, cr.Name, cr.Namespace)

	// pre-condition, we must be deployed, avoid broker status roundtrip till ready
	DeployedCondition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.DeployedConditionType)
	if DeployedCondition == nil || DeployedCondition.Status == metav1.ConditionFalse {
		reqLogger.V(2).Info("There are no available brokers from DeployedCondition", "condition", DeployedCondition)
		return NewArtemisStatusError(errors.New("no available brokers from deployed condition"), false)
	}
	return nil
}

func (reconciler *BrokerReconcilerImpl) AssertBrokerPropertiesStatus(cr *v1beta2.Broker, client rtclient.Client, scheme *runtime.Scheme) ArtemisError {
	reqLogger := reconciler.log

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
		for _, extraSecretName := range cr.Spec.ExtraMounts.Secrets {
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
	reqLogger := reconciler.log

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
	reqLogger := reconciler.log

	// The ResolveBrokerVersionFromCR should never fail because validation succeeded
	resolvedFullVersion, _ := brokerversion.ResolveBrokerVersionFromCR(cr)

	statusError := reconciler.CheckStatus(cr, client, func(brokerStatus *brokerStatus) ArtemisError {

		if brokerStatus.ServerStatus.Version != resolvedFullVersion {
			err := errors.Errorf("broker version non aligned on pod %s-%s, the detected version [%s] doesn't match the spec.version [%s] resolved as [%s]",
				namer.CrToSS(cr.Name), "0", brokerStatus.ServerStatus.Version, cr.Spec.Version, resolvedFullVersion)
			reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", cr.Spec.Version)
			return NewVersionMismatchError(err)
		}

		return nil
	})

	return statusError
}

func (reconciler *BrokerReconcilerImpl) CheckStatus(cr *v1beta2.Broker, client rtclient.Client, checkBrokerStatus func(bs *brokerStatus) ArtemisError) ArtemisError {

	reconciler.ensureStatusClient(cr, client)

	bs, artemisError := reconciler.getAndCacheBrokerStatus(cr, client)
	if artemisError != nil {
		return artemisError
	}

	return checkBrokerStatus(bs)
}

func (reconciler *BrokerReconcilerImpl) ensureStatusClient(cr *v1beta2.Broker, client rtclient.Client) {
	if reconciler.statusClient == nil {
		reconciler.statusClient = brokerjolokia.NewStatusClient(cr, client)
	}
}

func (reconciler *BrokerReconcilerImpl) getAndCacheBrokerStatus(_ *v1beta2.Broker, _ rtclient.Client) (*brokerStatus, ArtemisError) {
	const ordinal = "0"

	if cached, exists := reconciler.cachedBrokerStatus[ordinal]; exists {
		switch v := cached.(type) {
		case ArtemisError:
			return nil, v
		case brokerStatus:
			return &v, nil
		}
	}

	currentJSON, err := reconciler.statusClient.GetStatus()

	if err != nil {
		reconciler.log.V(1).Info("error getting broker status with Jolokia", "error", err)
		artemisError := NewArtemisStatusError(err, true)
		reconciler.cachedBrokerStatus[ordinal] = artemisError
		return nil, artemisError
	}

	reconciler.log.V(2).Info("raw json status", "ordinal", ordinal, "status json", currentJSON)

	bs, err := unmarshallStatus(currentJSON)
	if err != nil {
		reconciler.log.Error(err, "unable to unmarshall broker status", "json", currentJSON)
		artemisError := NewArtemisStatusError(err, false)
		reconciler.cachedBrokerStatus[ordinal] = artemisError
		return nil, artemisError
	}

	reconciler.log.V(2).Info("cached broker status", "ordinal", ordinal, "status", bs)
	reconciler.cachedBrokerStatus[ordinal] = bs

	return &bs, nil
}

func (reconciler *BrokerReconcilerImpl) checkProjectionStatus(cr *v1beta2.Broker, client rtclient.Client, secretProjection *projection, extractStatus func(BrokerStatus *brokerStatus, FileName string) (propertiesStatus, bool)) ArtemisError {
	reqLogger := reconciler.log

	reqLogger.V(2).Info("in sync check", "projection", secretProjection)

	checkErr := reconciler.CheckStatus(cr, client, func(brokerStatus *brokerStatus) ArtemisError {

		var current propertiesStatus
		var present bool
		var err error
		missingKeys := []string{}
		var applyError *inSyncApplyError = nil

		for name, file := range secretProjection.Files {

			current, present = extractStatus(brokerStatus, name)

			if !present {
				matches := brokerproperties.ParseBrokerPropertyWithOrdinal(name)
				if name != JaasConfigKey && !strings.HasPrefix(name, UncheckedPrefix) && len(matches) == 0 {
					missingKeys = append(missingKeys, name)
				}
				continue
			}

			if current.Alder32 == "" && current.FileAlder32 == "" {
				err = errors.Errorf("out of sync on pod %s-%s, property file %s has an empty checksum",
					namer.CrToSS(cr.Name), "0", name)
				reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
				return NewStatusOutOfSyncError(err)
			}

			if current.FileAlder32 != "" {
				if file.FileAlder32 != current.FileAlder32 {
					err = errors.Errorf("out of sync on pod %s-%s, mismatched file checksum on property file %s, expected: %s, current: %s. A delay can occur before a volume mount projection is refreshed.",
						namer.CrToSS(cr.Name), "0", name, file.FileAlder32, current.FileAlder32)
					reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
					return NewStatusOutOfSyncError(err)
				}
			} else if file.Alder32 != current.Alder32 {
				err = errors.Errorf("out of sync on pod %s-%s, mismatched checksum on property file %s, expected: %s, current: %s. A delay can occur before a volume mount projection is refreshed.",
					namer.CrToSS(cr.Name), "0", name, file.Alder32, current.Alder32)
				reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
				return NewStatusOutOfSyncError(err)
			}

			// check for apply errors
			if len(current.ApplyErrors) > 0 {
				// some props did not apply for k
				if applyError == nil {
					applyError = NewInSyncWithError(secretProjection, fmt.Sprintf("%s-%s", namer.CrToSS(cr.Name), "0"))
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
					namer.CrToSS(cr.Name), "0", missingKeys)
			} else {
				err = errors.Errorf("out of sync on pod %s-%s, configuration property files are not visible on the broker: %v. A delay can occur before a volume mount projection is refreshed.",
					namer.CrToSS(cr.Name), "0", missingKeys)
			}
			reqLogger.V(1).Info(err.Error(), "status", brokerStatus, "tracked", secretProjection)
			return NewStatusOutOfSyncMissingKeyError(err)
		}

		secretProjection.Ordinals = append(secretProjection.Ordinals, "0")

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
	if _, name, found := brokerproperties.GetConfigExtraMount(cr.Spec.ExtraMounts, jaasConfigSuffix); found {
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

	if validationCondition.Status != metav1.ConditionFalse && customResource.Spec.PodDisruptionBudget != nil {
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
		if duplicateKey := brokerproperties.DuplicateKeyIn(customResource.Spec.BrokerProperties); duplicateKey != "" {
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
	if customResource.Spec.Labels != nil {
		for key := range customResource.Spec.Labels {
			if key == selectors.LabelAppKey || key == selectors.LabelResourceKey {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionFailedReservedLabelReason,
					Message: fmt.Sprintf("'%s' is a reserved label, it is not allowed in Spec.Labels", key),
				}
			}
		}
	}
	for index, template := range customResource.Spec.ResourceTemplates {
		for key := range template.Labels {
			if key == selectors.LabelAppKey || key == selectors.LabelResourceKey {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionFailedReservedLabelReason,
					Message: fmt.Sprintf("'%s' is a reserved label, it is not allowed in Spec.ResourceTemplates[%d].Labels", key, index),
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

	if serviceName := getOwningServiceName(reconciler.customResource); serviceName != "" {
		ns := reconciler.customResource.Namespace
		for _, secretName := range []string{
			cot.OperatorCertName(serviceName),
			cot.RootCertSecretName(serviceName),
			cot.BrokerCertName(serviceName),
		} {
			if _, err := common.GetNamespacedSecret(client, secretName, ns); err != nil {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionMissingResourcesReason,
					Message: fmt.Sprintf("waiting for chain-of-trust secret %s: %v", secretName, err),
				}, retry
			}
		}
		return nil, false
	}

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

func getOwningServiceName(broker *v1beta2.Broker) string {
	for _, ref := range broker.OwnerReferences {
		if ref.Kind == "BrokerService" {
			return ref.Name
		}
	}
	return ""
}

func (reconciler *BrokerReconcilerImpl) validateStorage() (*metav1.Condition, bool) {

	if reconciler.customResource.Spec.PersistenceEnabled {
		if reconciler.customResource.Spec.Storage.Size != "" {
			_, err := resource.ParseQuantity(reconciler.customResource.Spec.Storage.Size)
			if err != nil {
				return &metav1.Condition{
					Type:    v1beta2.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  v1beta2.ValidConditionFailureReason,
					Message: fmt.Sprintf(".Spec.Storage.Size quantity string is invalid, %v", err),
				}, false
			}
		}
	}
	return nil, false
}

func validatePodDisruptionForBroker(customResource *v1beta2.Broker) *metav1.Condition {
	pdb := customResource.Spec.PodDisruptionBudget
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
	var ContextMessage = ".Spec.ExtraMounts.ConfigMaps,"
	for _, cm := range customResource.Spec.ExtraMounts.ConfigMaps {
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
			Condition = brokerproperties.AssertConfigMapContainsKey(configMap, LoggingConfigKey, ContextMessage)
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

	ContextMessage = ".Spec.ExtraMounts.Secrets,"
	for _, s := range customResource.Spec.ExtraMounts.Secrets {
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
			Condition = brokerproperties.AssertSecretContainsKey(secret, LoggingConfigKey, ContextMessage)
			instanceCounts[loggingConfigSuffix]++
		} else if strings.HasSuffix(s, jaasConfigSuffix) {
			Condition = brokerproperties.AssertSecretContainsKey(secret, JaasConfigKey, ContextMessage)
			if Condition == nil {
				Condition = brokerproperties.AssertSyntaxOkOnLoginConfigData(secret.Data[JaasConfigKey], s, ContextMessage)
			}
			instanceCounts[jaasConfigSuffix]++
		} else if strings.HasSuffix(s, common.BrokerPropsSuffix) {
			Condition = brokerproperties.AssertNoDupKeyInProperties(secret, ContextMessage)
		}
		if Condition != nil {
			return Condition, retry
		}
	}
	Condition = brokerproperties.AssertInstanceCounts(instanceCounts, "Spec.ExtraMounts,")
	if Condition != nil {
		return Condition, false // CR needs update
	}

	return nil, false
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

	newNamers.LabelBuilder.Base(customResource.Name).Suffix("app").Generate()

	return &newNamers
}

func GetDefaultLabelsForBroker(cr *v1beta2.Broker) map[string]string {
	defaultLabelData := selectors.LabelerData{}
	defaultLabelData.Base(cr.Name).Suffix("app").Generate()
	return defaultLabelData.Labels()
}

const brokerReloadLogAppenderFragment = `
appender.reload_trigger.type = RollingFile
appender.reload_trigger.name = ReloadTrigger
appender.reload_trigger.fileName = /app/log/reload.log
appender.reload_trigger.filePattern = /app/log/reload.log.%i
appender.reload_trigger.layout.type = PatternLayout
appender.reload_trigger.layout.pattern = %d %-5level [%logger] %msg%n
appender.reload_trigger.policies.type = Policies
appender.reload_trigger.policies.size.type = SizeBasedTriggeringPolicy
appender.reload_trigger.policies.size.size = 1MB
appender.reload_trigger.strategy.type = DefaultRolloverStrategy
appender.reload_trigger.strategy.max = 1
logger.reload.name = org.apache.activemq.artemis.core.server
logger.reload.appenderRef.reload_trigger.ref = ReloadTrigger
`

func (reconciler *BrokerReconcilerImpl) ensureStatusScriptConfigMap() {
	cr := reconciler.customResource
	cmName := cr.Name + brokerStatusScriptSuffix

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cr.Namespace,
		},
		Data: map[string]string{
			brokerStatusScriptKey: brokerStatusScript,
		},
	}

	reconciler.trackDesired(desired)
}

func (reconciler *BrokerReconcilerImpl) ensureReloadLoggingConfigMap() {
	cr := reconciler.customResource
	cmName := cr.Name + brokerLoggingConfigSuffix

	loggingContent := brokerReloadLogAppenderFragment

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cr.Namespace,
		},
		Data: map[string]string{
			LoggingConfigKey: loggingContent,
		},
	}

	reconciler.trackDesired(desired)
}

func (reconciler *BrokerReconcilerImpl) ensureStatusScriptRBAC(client rtclient.Client) error {
	cr := reconciler.customResource
	ctx := context.TODO()
	roleName := cr.Name + "-status-script"

	ownerRef := metav1.OwnerReference{
		APIVersion: cr.APIVersion,
		Kind:       cr.Kind,
		Name:       cr.Name,
		UID:        cr.UID,
	}
	if (ownerRef.APIVersion == "" || ownerRef.Kind == "") && reconciler.scheme != nil {
		gvks, _, _ := reconciler.scheme.ObjectKinds(cr)
		if len(gvks) > 0 {
			ownerRef.APIVersion = gvks[0].GroupVersion().String()
			ownerRef.Kind = gvks[0].Kind
		}
	}

	size := int32(1)
	ssName := cr.Name + "-ss"
	podNames := make([]string, size)
	for i := int32(0); i < size; i++ {
		podNames[i] = fmt.Sprintf("%s-%d", ssName, i)
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:            roleName,
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"pods"},
				ResourceNames: podNames,
				Verbs:         []string{"patch"},
			},
		},
	}

	existing := &rbacv1.Role{}
	if err := client.Get(ctx, types.NamespacedName{Name: roleName, Namespace: cr.Namespace}, existing); err != nil {
		if k8serrors.IsNotFound(err) {
			if err := client.Create(ctx, role); err != nil && !k8serrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create status-script Role: %w", err)
			}
		} else {
			return err
		}
	} else if !reflect.DeepEqual(existing.Rules, role.Rules) {
		patch := rtclient.MergeFrom(existing.DeepCopy())
		existing.Rules = role.Rules
		if err := client.Patch(ctx, existing, patch); err != nil {
			return fmt.Errorf("failed to patch status-script Role: %w", err)
		}
	}

	bindingName := cr.Name + "-status-script"
	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            bindingName,
			Namespace:       cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "default",
				Namespace: cr.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleName,
		},
	}

	existingBinding := &rbacv1.RoleBinding{}
	if err := client.Get(ctx, types.NamespacedName{Name: bindingName, Namespace: cr.Namespace}, existingBinding); err != nil {
		if k8serrors.IsNotFound(err) {
			if err := client.Create(ctx, binding); err != nil && !k8serrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create status-script RoleBinding: %w", err)
			}
		} else {
			return err
		}
	}

	return nil
}

// Controller Errors
