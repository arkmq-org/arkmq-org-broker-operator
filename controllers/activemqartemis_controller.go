/*
Copyright 2021.

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
	"regexp"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/certutil"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/namer"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/pkg/errors"

	brokerv1beta1 "github.com/arkmq-org/activemq-artemis-operator/v2/api/v1beta1"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/selectors"
)

var namespaceToConfigHandler = make(map[types.NamespacedName]common.ActiveMQArtemisConfigHandler)

func GetBrokerConfigHandler(brokerNamespacedName types.NamespacedName) (handler common.ActiveMQArtemisConfigHandler) {
	for _, handler := range namespaceToConfigHandler {
		if handler.IsApplicableFor(brokerNamespacedName) {
			return handler
		}
	}
	return nil
}

func (r *ActiveMQArtemisReconciler) UpdatePodForSecurity(securityHandlerNamespacedName types.NamespacedName, handler common.ActiveMQArtemisConfigHandler) error {

	existingCrs := &brokerv1beta1.ActiveMQArtemisList{}
	var err error
	opts := &rtclient.ListOptions{}
	if err = r.Client.List(context.TODO(), existingCrs, opts); err == nil {
		var candidate types.NamespacedName
		for index, artemis := range existingCrs.Items {
			candidate.Name = artemis.Name
			candidate.Namespace = artemis.Namespace
			if handler.IsApplicableFor(candidate) {
				r.log.V(1).Info("force reconcile for security", "handler", securityHandlerNamespacedName, "CR", candidate)
				r.events <- event.GenericEvent{Object: &existingCrs.Items[index]}
			}
		}
	}
	return err
}

func (r *ActiveMQArtemisReconciler) RemoveBrokerConfigHandler(namespacedName types.NamespacedName) {
	r.log.V(2).Info("Removing config handler", "name", namespacedName)
	oldHandler, ok := namespaceToConfigHandler[namespacedName]
	if ok {
		delete(namespaceToConfigHandler, namespacedName)
		r.log.V(1).Info("Handler removed", "name", namespacedName)
		r.UpdatePodForSecurity(namespacedName, oldHandler)
	}
}

func (r *ActiveMQArtemisReconciler) AddBrokerConfigHandler(namespacedName types.NamespacedName, handler common.ActiveMQArtemisConfigHandler, toReconcile bool) error {
	if _, ok := namespaceToConfigHandler[namespacedName]; ok {
		r.log.V(2).Info("There is an old config handler, it'll be replaced")
	}
	namespaceToConfigHandler[namespacedName] = handler
	r.log.V(2).Info("A new config handler has been added for security " + namespacedName.Namespace + "/" + namespacedName.Name)
	if toReconcile {
		r.log.V(1).Info("Updating broker security")
		return r.UpdatePodForSecurity(namespacedName, handler)
	}
	return nil
}

// ActiveMQArtemisReconciler reconciles a ActiveMQArtemis object
type ActiveMQArtemisReconciler struct {
	rtclient.Client
	Scheme        *runtime.Scheme
	events        chan event.GenericEvent
	log           logr.Logger
	isOnOpenShift bool
}

func NewActiveMQArtemisReconciler(cluster cluster.Cluster, logger logr.Logger, isOpenShift bool) *ActiveMQArtemisReconciler {
	return &ActiveMQArtemisReconciler{
		isOnOpenShift: isOpenShift,
		Client:        cluster.GetClient(),
		Scheme:        cluster.GetScheme(),
		log:           logger,
	}
}

//run 'make manifests' after changing the following rbac markers

//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemises,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemises/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=activemqartemises/finalizers,verbs=update
//+kubebuilder:rbac:groups=broker.amq.io,namespace=activemq-artemis-operator,resources=pods,verbs=get;list
//+kubebuilder:rbac:groups="",namespace=activemq-artemis-operator,resources=pods;services;endpoints;persistentvolumeclaims;events;configmaps;secrets;routes;serviceaccounts,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups="",namespace=activemq-artemis-operator,resources=namespaces,verbs=get
//+kubebuilder:rbac:groups=apps,namespace=activemq-artemis-operator,resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=networking.k8s.io,namespace=activemq-artemis-operator,resources=ingresses,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=route.openshift.io,namespace=activemq-artemis-operator,resources=routes;routes/custom-host;routes/status,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=monitoring.coreos.com,namespace=activemq-artemis-operator,resources=servicemonitors,verbs=get;create
//+kubebuilder:rbac:groups=apps,namespace=activemq-artemis-operator,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=activemq-artemis-operator,resources=roles;rolebindings,verbs=create;get;delete
//+kubebuilder:rbac:groups=policy,namespace=activemq-artemis-operator,resources=poddisruptionbudgets,verbs=create;get;delete;list;update;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ActiveMQArtemis object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *ActiveMQArtemisReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "ActiveMQArtemis")

	customResource := &brokerv1beta1.ActiveMQArtemis{}

	result := ctrl.Result{}

	err := r.Get(context.TODO(), request.NamespacedName, customResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			reqLogger.V(1).Info("ActiveMQArtemis Controller Reconcile encountered a IsNotFound, for request NamespacedName " + request.NamespacedName.String())
			return result, nil
		}
		reqLogger.Error(err, "unable to retrieve the ActiveMQArtemis")
		return result, err
	}
	var reconcileBlocked bool = false
	if val, present := customResource.Annotations[common.BlockReconcileAnnotation]; present {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			reconcileBlocked = boolVal
		}
	}

	namer := MakeNamers(customResource)
	reconciler := NewActiveMQArtemisReconcilerImpl(customResource, r)

	var requeueRequest bool = false
	var valid bool = false
	if valid, requeueRequest = reconciler.validate(customResource, r.Client, *namer); valid {

		if !reconcileBlocked {
			err = reconciler.Process(customResource, *namer, r.Client, r.Scheme)
		}
		if reconciler.ProcessBrokerStatus(customResource, r.Client, r.Scheme) {
			requeueRequest = true
		}
	}

	common.UpdateBlockedStatus(customResource, reconcileBlocked)
	common.ProcessStatus(customResource, r.Client, request.NamespacedName, *namer, err)

	crStatusUpdateErr := r.UpdateCRStatus(customResource, r.Client, request.NamespacedName)
	if crStatusUpdateErr != nil {
		requeueRequest = true
	}

	if !requeueRequest && !reconcileBlocked && hasExtraMounts(customResource) {
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

func (r *ActiveMQArtemisReconcilerImpl) validate(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, namer common.Namers) (bool, retry bool) {
	// Do additional validation here
	validationCondition := metav1.Condition{
		Type:   brokerv1beta1.ValidConditionType,
		Status: metav1.ConditionTrue,
		Reason: brokerv1beta1.ValidConditionSuccessReason,
	}

	condition, retry := validateExtraMounts(customResource, client)
	if condition != nil {
		validationCondition = *condition
	}

	if validationCondition.Status != metav1.ConditionFalse && customResource.Spec.DeploymentPlan.PodDisruptionBudget != nil {
		condition := validatePodDisruption(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = validateNoDupKeysInBrokerProperties(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = r.validateStorage()
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = validateAcceptorPorts(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = validateSSLEnabledSecrets(customResource, client, namer)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition := common.ValidateBrokerImageVersion(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition := validateReservedLabels(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = r.validateExposeModes(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	if validationCondition.Status != metav1.ConditionFalse {
		condition, retry = r.validateEnvVars(customResource)
		if condition != nil {
			validationCondition = *condition
		}
	}

	common.SetStatusConditionWithGeneration(customResource, validationCondition)

	return validationCondition.Status != metav1.ConditionFalse, retry
}

func validateNoDupKeysInBrokerProperties(customResource *brokerv1beta1.ActiveMQArtemis) (*metav1.Condition, bool) {
	if len(customResource.Spec.BrokerProperties) > 0 {
		if duplicateKey := DuplicateKeyIn(customResource.Spec.BrokerProperties); duplicateKey != "" {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey,
				Message: fmt.Sprintf(".Spec.BrokerProperties has a duplicate key for %v", duplicateKey),
			}, false
		}

	}
	return nil, false
}

func validateReservedLabels(customResource *brokerv1beta1.ActiveMQArtemis) *metav1.Condition {
	if customResource.Spec.DeploymentPlan.Labels != nil {
		for key := range customResource.Spec.DeploymentPlan.Labels {
			if key == selectors.LabelAppKey || key == selectors.LabelResourceKey {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailedReservedLabelReason,
					Message: fmt.Sprintf("'%s' is a reserved label, it is not allowed in Spec.DeploymentPlan.Labels", key),
				}
			}
		}
	}
	for index, template := range customResource.Spec.ResourceTemplates {
		for key := range template.Labels {
			if key == selectors.LabelAppKey || key == selectors.LabelResourceKey {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailedReservedLabelReason,
					Message: fmt.Sprintf("'%s' is a reserved label, it is not allowed in Spec.DeploymentPlan.Templates[%d].Labels", key, index),
				}
			}
		}
	}
	return nil
}

func validateAcceptorPorts(customResource *brokerv1beta1.ActiveMQArtemis) (*metav1.Condition, bool) {
	portMap := map[int32]string{}

	for _, acceptor := range customResource.Spec.Acceptors {
		if acceptor.Port > 0 { // port == 0 is server chooses free port
			if existingName, duplicate := portMap[acceptor.Port]; duplicate {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailedDuplicateAcceptorPort,
					Message: fmt.Sprintf(".Spec.Acceptors %q and %q contain a duplicate port %v", acceptor.Name, existingName, acceptor.Port),
				}, false
			}
			portMap[acceptor.Port] = acceptor.Name
		}
	}
	return nil, false
}

func (r *ActiveMQArtemisReconcilerImpl) validateExposeModes(customResource *brokerv1beta1.ActiveMQArtemis) (*metav1.Condition, bool) {

	if !r.isOnOpenShift {
		for _, acceptor := range customResource.Spec.Acceptors {
			if acceptor.Expose && acceptor.ExposeMode != nil && *acceptor.ExposeMode == brokerv1beta1.ExposeModes.Route {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailedInvalidExposeMode,
					Message: fmt.Sprintf(".Spec.Acceptors %q has invalid expose mode route, it is only supported on OpenShift", acceptor.Name),
				}, false
			}
		}

		for _, connector := range customResource.Spec.Connectors {
			if connector.Expose && connector.ExposeMode != nil && *connector.ExposeMode == brokerv1beta1.ExposeModes.Route {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailedInvalidExposeMode,
					Message: fmt.Sprintf(".Spec.Connectors %q has invalid expose mode route, it is only supported on OpenShift", connector.Name),
				}, false
			}
		}

		console := customResource.Spec.Console
		if console.Expose && console.ExposeMode != nil && *console.ExposeMode == brokerv1beta1.ExposeModes.Route {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedInvalidExposeMode,
				Message: ".Spec.Console has invalid expose mode route, it is only supported on OpenShift",
			}, false
		}
	}

	for _, acceptor := range customResource.Spec.Acceptors {
		if acceptor.Expose && (acceptor.ExposeMode != nil && *acceptor.ExposeMode == brokerv1beta1.ExposeModes.Ingress || !r.isOnOpenShift) &&
			customResource.Spec.IngressDomain == "" && acceptor.IngressHost == "" {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedInvalidIngressSettings,
				Message: fmt.Sprintf(".Spec.Acceptors %q has invalid ingress settings, IngressHost unspecified and no Spec.IngressDomain default domain provided", acceptor.Name),
			}, false
		}
	}

	for _, connector := range customResource.Spec.Connectors {
		if connector.Expose && (connector.ExposeMode != nil && *connector.ExposeMode == brokerv1beta1.ExposeModes.Ingress || !r.isOnOpenShift) &&
			customResource.Spec.IngressDomain == "" && connector.IngressHost == "" {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedInvalidIngressSettings,
				Message: fmt.Sprintf(".Spec.Connectors %q has invalid ingress settings, IngressHost unspecified and no Spec.IngressDomain default domain provided", connector.Name),
			}, false
		}
	}

	console := customResource.Spec.Console
	if console.Expose && (console.ExposeMode != nil && *console.ExposeMode == brokerv1beta1.ExposeModes.Ingress || !r.isOnOpenShift) &&
		customResource.Spec.IngressDomain == "" && console.IngressHost == "" {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedInvalidIngressSettings,
			Message: ".Spec.Console has invalid ingress settings, IngressHost unspecified and no Spec.IngressDomain default domain provided",
		}, false
	}

	return nil, false
}

func (r *ActiveMQArtemisReconcilerImpl) validateEnvVars(customResource *brokerv1beta1.ActiveMQArtemis) (*metav1.Condition, bool) {

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
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionInvalidInternalVarUsage,
			Message: fmt.Sprintf("Don't use valueFrom on env vars that the operator can mutate: %v. Instead use a different var and refernece it in its value field.", invalidVars),
		}, false
	}
	return nil, false
}

func (r *ActiveMQArtemisReconcilerImpl) validateStorage() (*metav1.Condition, bool) {

	if r.customResource.Spec.DeploymentPlan.PersistenceEnabled {
		if r.customResource.Spec.DeploymentPlan.Storage.Size != "" {
			_, err := resource.ParseQuantity(r.customResource.Spec.DeploymentPlan.Storage.Size)
			if err != nil {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailureReason,
					Message: fmt.Sprintf(".Spec.DeploymentPlan.Storage.Size quantity string is invalid, %v", err),
				}, false
			}
		}
	}
	return nil, false
}

func validateSSLEnabledSecrets(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, namer common.Namers) (*metav1.Condition, bool) {

	var retry = true
	if customResource.Spec.Console.SSLEnabled {

		secretName := namer.SecretsConsoleNameBuilder.Name()
		if customResource.Spec.Console.SSLSecret != "" {
			secretName = customResource.Spec.Console.SSLSecret
		}

		secret := corev1.Secret{}
		found := retrieveResource(secretName, customResource.Namespace, &secret, client)
		if !found {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf(".Spec.Console.SSLEnabled is true but required secret %v is not found", secretName),
			}, retry
		}

		contextMessage := ".Spec.Console.SSLEnabled is true but required"
		for _, key := range []string{
			"keyStorePassword",
			"trustStorePassword",
		} {
			Condition := AssertSecretContainsKey(secret, key, contextMessage)
			if Condition != nil {
				return Condition, retry
			}
		}

		Condition := AssertSecretContainsOneOf(secret, []string{
			"keyStorePath",
			"broker.ks"}, contextMessage)
		if Condition != nil {
			return Condition, retry
		}

		Condition = AssertSecretContainsOneOf(secret, []string{
			"trustStorePath",
			"client.ts"}, contextMessage)
		if Condition != nil {
			return Condition, retry
		}
	}
	return nil, false
}

func validatePodDisruption(customResource *brokerv1beta1.ActiveMQArtemis) *metav1.Condition {
	pdb := customResource.Spec.DeploymentPlan.PodDisruptionBudget
	if pdb.Selector != nil {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionPDBNonNilSelectorReason,
			Message: common.PDBNonNilSelectorMessage,
		}
	}
	return nil
}

func validateExtraMounts(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) (*metav1.Condition, bool) {

	instanceCounts := map[string]int{}
	var Condition *metav1.Condition
	var retry bool = true
	var ContextMessage = ".Spec.DeploymentPlan.ExtraMounts.ConfigMaps,"
	for _, cm := range customResource.Spec.DeploymentPlan.ExtraMounts.ConfigMaps {
		configMap := corev1.ConfigMap{}
		found := retrieveResource(cm, customResource.Namespace, &configMap, client)
		if !found {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionMissingResourcesReason,
				Message: fmt.Sprintf("%v missing required configMap %v", ContextMessage, cm),
			}, retry
		}
		if strings.HasSuffix(cm, loggingConfigSuffix) {
			Condition = AssertConfigMapContainsKey(configMap, LoggingConfigKey, ContextMessage)
			instanceCounts[loggingConfigSuffix]++
		} else if strings.HasSuffix(cm, jaasConfigSuffix) {
			Condition = &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
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
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionMissingResourcesReason,
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
		} else if strings.HasSuffix(s, brokerPropsSuffix) {
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

func AssertSyntaxOkOnLoginConfigData(SecretContentForLoginConfigKey []byte, name string, contextMessage string) *metav1.Condition {

	if !MatchBytesAgainsLoginConfigRegexp(SecretContentForLoginConfigKey) {

		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
			Message: fmt.Sprintf("%s content of login.config key in secret %v does not match supported jaas config file syntax", contextMessage, name),
		}
	}

	return nil
}

var loginConfigSyntaxMatcher *regexp.Regexp

func MatchBytesAgainsLoginConfigRegexp(buffer []byte) bool {
	syntaxMatchRegEx := common.GetJaasConfigSyntaxMatchRegEx()
	if syntaxMatchRegEx == "" {
		// disabled
		return true
	}

	if loginConfigSyntaxMatcher == nil {
		loginConfigSyntaxMatcher, _ = regexp.Compile(syntaxMatchRegEx)
	}
	return loginConfigSyntaxMatcher.Match(buffer)
}

func AssertInstanceCounts(instanceCounts map[string]int) *metav1.Condition {
	for key, v := range instanceCounts {
		if v > 1 {
			return &metav1.Condition{
				Type:    brokerv1beta1.ValidConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
				Message: fmt.Sprintf("Spec.DeploymentPlan.ExtraMounts, entry with suffix %v can only be supplied once", key),
			}
		}
	}
	return nil
}

func AssertConfigMapContainsKey(configMap corev1.ConfigMap, key string, contextMessage string) *metav1.Condition {
	if _, present := configMap.Data[key]; !present {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
			Message: fmt.Sprintf("%s configmap %v must have key %v", contextMessage, configMap.Name, key),
		}
	}
	return nil
}

func AssertNoDupKeyInProperties(secret corev1.Secret, contextMessage string) *metav1.Condition {
	for key, data := range secret.Data {
		if !strings.HasPrefix(key, UncheckedPrefix) && strings.HasSuffix(key, PropertiesSuffix) {
			if duplicateKey := DuplicateKeyInPropertiesContent(data); duplicateKey != "" {
				return &metav1.Condition{
					Type:    brokerv1beta1.ValidConditionType,
					Status:  metav1.ConditionFalse,
					Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
					Message: fmt.Sprintf("%s properties secret %v entry %v has a duplicate key for %v", contextMessage, secret.Name, key, duplicateKey),
				}
			}
		}
	}
	return nil
}

func DuplicateKeyInPropertiesContent(keyValues []byte) string {
	return DuplicateKeyIn(KeyValuePairs(keyValues))
}

func DuplicateKeyIn(keyValues []string) string {
	keysMap := map[string]string{}

	for _, keyAndValue := range keyValues {
		if key, _, found := strings.Cut(keyAndValue, "="); found {
			_, duplicate := keysMap[key]
			if !(duplicate) {
				keysMap[key] = key
			} else {
				return key
			}
		}
	}

	return ""
}

func AssertSecretContainsKey(secret corev1.Secret, key string, contextMessage string) *metav1.Condition {
	isCertSecret, isValid := certutil.IsSecretFromCert(&secret)
	if isCertSecret {
		if isValid {
			return nil
		}
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionInvalidCertSecretReason,
			Message: fmt.Sprintf("%s certificate secret %s not valid, must have keys ca.crt tls.crt tls.key", contextMessage, secret.Name),
		}
	}
	if _, present := secret.Data[key]; !present {
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
			Message: fmt.Sprintf("%s secret %v must have key %v", contextMessage, secret.Name, key),
		}
	}
	return nil
}

func AssertSecretContainsOneOf(secret corev1.Secret, keys []string, contextMessage string) *metav1.Condition {
	ok, valid := certutil.IsSecretFromCert(&secret)
	if ok {
		if valid {
			return nil
		}
		return &metav1.Condition{
			Type:    brokerv1beta1.ValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ValidConditionInvalidCertSecretReason,
			Message: fmt.Sprintf("%s secret %s must contain keys %v", contextMessage, secret.Name, "ca.crt,tls.crt,tls.key"),
		}
	}
	for _, key := range keys {
		_, present := secret.Data[key]
		if present {
			return nil
		}
	}
	return &metav1.Condition{
		Type:    brokerv1beta1.ValidConditionType,
		Status:  metav1.ConditionFalse,
		Reason:  brokerv1beta1.ValidConditionFailedExtraMountReason,
		Message: fmt.Sprintf("%s secret %v must contain one of following keys %v", contextMessage, secret.Name, keys),
	}
}

func retrieveResource(name, namespace string, obj rtclient.Object, client rtclient.Client) bool {
	err := client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, obj)
	return err == nil
}

func hasExtraMounts(cr *brokerv1beta1.ActiveMQArtemis) bool {
	if cr == nil {
		return false
	}
	if len(cr.Spec.DeploymentPlan.ExtraMounts.ConfigMaps) > 0 {
		return true
	}
	return len(cr.Spec.DeploymentPlan.ExtraMounts.Secrets) > 0
}

func MakeNamers(customResource *brokerv1beta1.ActiveMQArtemis) *common.Namers {
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
	if customResource.Spec.Console.SSLSecret != "" {
		newNamers.SecretsConsoleNameBuilder.SetName(customResource.Spec.Console.SSLSecret)
	} else {
		newNamers.SecretsConsoleNameBuilder.Prefix(customResource.Name).Base("console").Suffix("secret").Generate()
	}
	newNamers.SecretsNettyNameBuilder.Prefix(customResource.Name).Base("netty").Suffix("secret").Generate()

	newNamers.LabelBuilder.Base(customResource.Name).Suffix("app").Generate()

	return &newNamers
}

func GetDefaultLabels(cr *brokerv1beta1.ActiveMQArtemis) map[string]string {
	defaultLabelData := selectors.LabelerData{}
	defaultLabelData.Base(cr.Name).Suffix("app").Generate()
	return defaultLabelData.Labels()
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveMQArtemisReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1beta1.ActiveMQArtemis{}).
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

	var err error
	controller, err := builder.Build(r)
	if err == nil {
		r.events = make(chan event.GenericEvent)
		err = controller.Watch(
			&source.Channel{Source: r.events},
			&handler.EnqueueRequestForObject{},
		)
	}
	return err
}

func (r *ActiveMQArtemisReconciler) UpdateCRStatus(desired *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, namespacedName types.NamespacedName) error {

	common.SetReadyCondition(&desired.Status.Conditions)

	current := &brokerv1beta1.ActiveMQArtemis{}

	err := client.Get(context.TODO(), namespacedName, current)
	if err != nil {
		r.log.Error(err, "unable to retrieve current resource", "ActiveMQArtemis", namespacedName)
		return err
	}

	if !EqualCRStatus(&desired.Status, &current.Status) {
		r.log.V(1).Info("cr.status update", "Namespace", desired.Namespace, "Name", desired.Name, "Observed status", desired.Status)
		return resources.UpdateStatus(client, desired)
	}

	return nil
}

func EqualCRStatus(s1, s2 *brokerv1beta1.ActiveMQArtemisStatus) bool {
	if s1.DeploymentPlanSize != s2.DeploymentPlanSize ||
		s1.ScaleLabelSelector != s2.ScaleLabelSelector ||
		!reflect.DeepEqual(s1.Version, s2.Version) ||
		len(s2.ExternalConfigs) != len(s1.ExternalConfigs) ||
		externalConfigsModified(s2.ExternalConfigs, s1.ExternalConfigs) ||
		!reflect.DeepEqual(s1.PodStatus, s2.PodStatus) ||
		len(s1.Conditions) != len(s2.Conditions) ||
		conditionsModified(s2.Conditions, s1.Conditions) {

		return false
	}

	return true
}

func conditionsModified(desiredConditions []metav1.Condition, currentConditions []metav1.Condition) bool {
	for _, c := range desiredConditions {
		if !common.IsConditionPresentAndEqual(currentConditions, c) {
			return true
		}
	}
	return false
}

func externalConfigsModified(desiredExternalConfigs []brokerv1beta1.ExternalConfigStatus, currentExternalConfigs []brokerv1beta1.ExternalConfigStatus) bool {
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

// Controller Errors

type ArtemisError interface {
	Error() string
	Requeue() bool
}

type artemisStatusError struct {
	cause     error
	transient bool
}

type jolokiaClientNotFoundError struct {
	cause error
}

type statusOutOfSyncError struct {
	cause string
}

type statusOutOfSyncMissingKeyError struct {
	cause string
}

type versionMismatchError struct {
	cause string
}

func NewArtemisStatusError(err error, transient bool) artemisStatusError {
	return artemisStatusError{
		err,
		transient,
	}
}

func (e artemisStatusError) Error() string {
	return e.cause.Error()
}

func (e artemisStatusError) Requeue() bool {
	return e.transient
}

func NewJolokiaClientsNotFoundError(err error) jolokiaClientNotFoundError {
	return jolokiaClientNotFoundError{
		err,
	}
}

func (e jolokiaClientNotFoundError) Error() string {
	return errors.Wrap(e.cause, "no available Jolokia Clients found").Error()
}

func (e jolokiaClientNotFoundError) Requeue() bool {
	return true
}

func NewStatusOutOfSyncError(err error) statusOutOfSyncError {
	return statusOutOfSyncError{err.Error()}
}

func (e statusOutOfSyncError) Error() string {
	return e.cause
}

func (e statusOutOfSyncError) Requeue() bool {
	return true
}

func NewStatusOutOfSyncMissingKeyError(err error) statusOutOfSyncMissingKeyError {
	return statusOutOfSyncMissingKeyError{err.Error()}
}

func (e statusOutOfSyncMissingKeyError) Error() string {
	return e.cause
}

func (e statusOutOfSyncMissingKeyError) Requeue() bool {
	return true
}

type inSyncApplyError struct {
	cause  error
	detail map[string]string
}

const inSyncWithErrorCause = "some properties from %v resulted in error on pod %s"

func NewInSyncWithError(secretProjection *projection, pod string) *inSyncApplyError {
	return &inSyncApplyError{
		cause:  errors.Errorf(inSyncWithErrorCause, secretProjection.Name, pod),
		detail: map[string]string{},
	}
}

func (e inSyncApplyError) Requeue() bool {
	return false
}

func (e inSyncApplyError) Error() string {
	return fmt.Sprintf("%s : reasons: %v", e.cause.Error(), e.detail)
}

func (e *inSyncApplyError) ErrorApplyDetail(container string, reason string) {
	existing, present := e.detail[container]
	if present {
		e.detail[container] = fmt.Sprintf("%s, %s", existing, reason)
	} else {
		e.detail[container] = reason
	}
}

func NewVersionMismatchError(err error) versionMismatchError {
	return versionMismatchError{err.Error()}
}

func (e versionMismatchError) Error() string {
	return e.cause
}

func (e versionMismatchError) Requeue() bool {
	return false
}
