package controllers

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/adler32"
	"regexp"
	"slices"
	"sort"
	"unicode"

	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/containers"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/persistentvolumeclaims"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/pods"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/routes"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/serviceports"
	ss "github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/statefulsets"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/certutil"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/cr2jinja2"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/random"
	"github.com/arkmq-org/activemq-artemis-operator/v2/version"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	ctrl "sigs.k8s.io/controller-runtime"

	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/environments"
	svc "github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/resources/volumes"

	"reflect"

	routev1 "github.com/openshift/api/route/v1"
	netv1 "k8s.io/api/networking/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	brokerv1beta1 "github.com/arkmq-org/activemq-artemis-operator/v2/api/v1beta1"

	"strconv"
	"strings"

	"os"

	policyv1 "k8s.io/api/policy/v1"
)

const (
	defaultLivenessProbeInitialDelay = 5
	TCPLivenessPort                  = 8161
	jaasConfigSuffix                 = "-jaas-config"
	loggingConfigSuffix              = "-logging-config"
	brokerPropsSuffix                = "-bp"

	cfgMapPathBase = "/amq/extra/configmaps/"
	secretPathBase = "/amq/extra/secrets/"

	OrdinalPrefix            = "broker-"
	OrdinalPrefixSep         = "."
	UncheckedPrefix          = "_"
	PropertiesSuffix         = ".properties"
	JsonSuffix               = ".json"
	BrokerPropertiesName     = "broker" + PropertiesSuffix
	JaasConfigKey            = "login.config"
	LoggingConfigKey         = "logging" + PropertiesSuffix
	PodNameLabelKey          = "statefulset.kubernetes.io/pod-name"
	ServiceTypePostfix       = "svc"
	RouteTypePostfix         = "rte"
	IngressTypePostfix       = "ing"
	RemoveKeySpecialValue    = "-"
	javaArgsAppendEnvVarName = "JAVA_ARGS_APPEND"
	debugArgsEnvVarName      = "DEBUG_ARGS"
	javaOptsEnvVarName       = "JAVA_OPTS"
	jdkJavaOptionsEnvVarName = "JDK_JAVA_OPTIONS"

	ScaleDownConfigTrigger        = "HAPolicyConfiguration.scaleDownConfiguration.enabled=false"
	ScaleDownConfigTriggerOn      = "HAPolicyConfiguration.scaleDownConfiguration.enabled=true"
	OrdinalPropertiesSuffix       = "for_ordinal_"
	OrdinalPropertiesSuffixEnd    = "_only"
	BrokerPropertiesOrdinalFormat = UncheckedPrefix + "%s" + OrdinalPrefixSep + OrdinalPropertiesSuffix + "%d" + OrdinalPropertiesSuffixEnd
)

var defaultMessageMigration bool = true

// the helper script looks for "/amq/scripts/post-config.sh"
// and run it if exists.
var initHelperScript = "/opt/amq-broker/script/default.sh"
var brokerConfigRoot = "/amq/init/config"
var configCmd = "/opt/amq/bin/launch.sh"

// default ApplyRule for address-settings
var defApplyRule string = "merge_all"
var yacfgProfileVersion = version.YacfgProfileVersionFromFullVersion[version.GetDefaultVersion()]

type ActiveMQArtemisReconcilerImpl struct {
	requestedResources map[reflect.Type]map[string]rtclient.Object
	deployed           map[reflect.Type][]rtclient.Object
	log                logr.Logger
	customResource     *brokerv1beta1.ActiveMQArtemis
	scheme             *runtime.Scheme
	isOnOpenShift      bool
	jolokiaEndpoints   []*jolokia_client.JkInfo
	cachedBrokerStatus map[string]any
	matchedTemplates   map[int]bool
}

func NewActiveMQArtemisReconcilerImpl(customResource *brokerv1beta1.ActiveMQArtemis, parent *ActiveMQArtemisReconciler) *ActiveMQArtemisReconcilerImpl {
	return &ActiveMQArtemisReconcilerImpl{
		log:                parent.log,
		customResource:     customResource,
		scheme:             parent.Scheme,
		requestedResources: make(map[reflect.Type]map[string]rtclient.Object),
		isOnOpenShift:      parent.isOnOpenShift,
		cachedBrokerStatus: make(map[string]any),
		matchedTemplates:   make(map[int]bool),
	}
}

type ValueInfo struct {
	Value    string
	AutoGen  bool
	Internal bool //if true put this value to the internal secret
}

type ActiveMQArtemisIReconciler interface {
	Process(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme) error
	ProcessStatefulSet(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, log logr.Logger) (*appsv1.StatefulSet, error)
	ProcessCredentials(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) uint32
	ProcessDeploymentPlan(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet, firstTime bool) uint32
	ProcessAcceptorsAndConnectors(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) uint32
	ProcessConsole(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet)
	ProcessResources(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) uint8
}

func (reconciler *ActiveMQArtemisReconcilerImpl) Process(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme) error {

	reconciler.log.V(1).Info("Reconciler Processing...", "Operator version", version.Version, "ActiveMQArtemis release", customResource.Spec.Version)
	reconciler.log.V(2).Info("Reconciler Processing...", "CRD.Name", customResource.Name, "CRD ver", customResource.ObjectMeta.ResourceVersion, "CRD Gen", customResource.ObjectMeta.Generation)

	reconciler.CurrentDeployedResources(customResource, client)

	// currentStateful Set is a clone of what exists if already deployed
	// what follows should transform the resources using the crd
	// if the transformation results in some change, process resources will respect that
	// comparisons should not be necessary, leave that to process resources
	desiredStatefulSet, err := reconciler.ProcessStatefulSet(customResource, namer, client)
	if err != nil {
		reconciler.log.Error(err, "Error processing stafulset")
		return err
	}

	reconciler.ProcessDeploymentPlan(customResource, namer, client, scheme, desiredStatefulSet)

	reconciler.ProcessCredentials(customResource, namer, client, scheme, desiredStatefulSet)

	err = reconciler.ProcessAcceptorsAndConnectors(customResource, namer, client, scheme, desiredStatefulSet)

	if err != nil {
		reconciler.log.Error(err, "error processing acceptors and connectors")
		return err
	}

	err = reconciler.ProcessConsole(customResource, namer, client, scheme, desiredStatefulSet)

	if err != nil {
		reconciler.log.Error(err, "Error processing console")
		return err
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

	reconciler.log.V(1).Info("Reconciler Processing... complete", "CRD ver:", customResource.ObjectMeta.ResourceVersion, "CRD Gen:", customResource.ObjectMeta.Generation)

	// we dont't requeue
	return err
}

func trackSecretCheckSumInEnvVar(requestedResources []rtclient.Object, container []corev1.Container) {
	// the requestedResources need to be sorted because they are extracted
	// from a map and adler32 depends on the prder of the bytes
	sort.Slice(requestedResources, func(i, j int) bool {
		return requestedResources[i].GetName() < requestedResources[j].GetName()
	})

	// find desired secrets and checksum their 'sorted' values
	digest := adler32.New()
	for _, obj := range requestedResources {
		if secret, ok := obj.(*corev1.Secret); ok {
			// ignore secret for persistence of cr
			if strings.HasSuffix(secret.Name, "-secret") {
				// note use of StringData to match MakeSecret for the initial create case
				if len(secret.StringData) > 0 {
					for _, k := range sortedKeys(secret.StringData) {
						digest.Write([]byte(secret.StringData[k]))
					}
				} else {
					for _, k := range sortedKeysStringKeyByteValue(secret.Data) {
						digest.Write(secret.Data[k])
					}
				}
			}
		}
	}
	environments.TrackSecretCheckSumInRollCount(hex.EncodeToString(digest.Sum(nil)), container)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) cloneOfDeployed(kind reflect.Type, name string) rtclient.Object {
	obj := reconciler.getFromDeployed(kind, name)
	if obj != nil {
		return obj.DeepCopyObject().(rtclient.Object)
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) getFromDeployed(kind reflect.Type, name string) rtclient.Object {
	for _, obj := range reconciler.deployed[kind] {
		if obj.GetName() == name {
			return obj
		}
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) addToDeployed(kind reflect.Type, obj rtclient.Object) {
	reconciler.deployed[kind] = append(reconciler.deployed[kind], obj)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessStatefulSet(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client) (*appsv1.StatefulSet, error) {

	reqLogger := reconciler.log.WithName(customResource.Name)

	ssNamespacedName := types.NamespacedName{
		Namespace: customResource.Namespace,
		Name:      namer.SsNameBuilder.Name(),
	}

	var err error
	var currentStatefulSet *appsv1.StatefulSet
	obj := reconciler.cloneOfDeployed(reflect.TypeOf(appsv1.StatefulSet{}), ssNamespacedName.Name)
	if obj != nil {
		currentStatefulSet = obj.(*appsv1.StatefulSet)
	}

	reqLogger.V(2).Info("Reconciling desired statefulset", "name", ssNamespacedName, "current", currentStatefulSet)
	currentStatefulSet, err = reconciler.StatefulSetForCR(customResource, namer, currentStatefulSet, client)
	if err != nil {
		reqLogger.Error(err, "Error creating new stafulset")
		return nil, err
	}

	var headlessServiceDefinition *corev1.Service
	headlesServiceName := namer.SvcHeadlessNameBuilder.Name()
	obj = reconciler.cloneOfDeployed(reflect.TypeOf(corev1.Service{}), headlesServiceName)
	if obj != nil {
		headlessServiceDefinition = obj.(*corev1.Service)
	}

	labels := namer.LabelBuilder.Labels()
	headlessServiceDefinition = svc.NewHeadlessServiceForCR2(client, headlesServiceName, ssNamespacedName.Namespace, serviceports.GetDefaultPorts(common.IsRestricted(customResource)), labels, headlessServiceDefinition)
	reconciler.trackDesired(headlessServiceDefinition)

	if isClustered(customResource) {
		pingServiceName := namer.SvcPingNameBuilder.Name()
		var pingServiceDefinition *corev1.Service
		obj = reconciler.cloneOfDeployed(reflect.TypeOf(corev1.Service{}), pingServiceName)
		if obj != nil {
			pingServiceDefinition = obj.(*corev1.Service)
		}
		pingServiceDefinition = svc.NewPingServiceDefinitionForCR2(client, pingServiceName, ssNamespacedName.Namespace, labels, labels, pingServiceDefinition)
		reconciler.trackDesired(pingServiceDefinition)
	}

	if customResource.Spec.DeploymentPlan.RevisionHistoryLimit != nil {
		currentStatefulSet.Spec.RevisionHistoryLimit = customResource.Spec.DeploymentPlan.RevisionHistoryLimit
	}
	return currentStatefulSet, nil
}

func isClustered(customResource *brokerv1beta1.ActiveMQArtemis) bool {
	if common.IsRestricted(customResource) {
		return false
	}

	if customResource.Spec.DeploymentPlan.Clustered != nil {
		return *customResource.Spec.DeploymentPlan.Clustered
	}
	return true
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessCredentials(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	if common.IsRestricted(customResource) {
		return
	}
	reconciler.log.V(1).Info("ProcessCredentials")

	envVars := make(map[string]ValueInfo)

	adminUser := ValueInfo{
		Value:   "",
		AutoGen: false,
	}
	adminPassword := ValueInfo{
		Value:   "",
		AutoGen: false,
	}
	// TODO: Remove singular admin level user and password in favour of at least guest and admin access
	secretName := namer.SecretsCredentialsNameBuilder.Name()
	adminUser.Value = customResource.Spec.AdminUser
	if adminUser.Value == "" {
		if amqUserEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.Containers, "AMQ_USER"); nil != amqUserEnvVar {
			adminUser.Value = amqUserEnvVar.Value
		}
		if adminUser.Value == "" {
			adminUser.Value = random.GenerateRandomString(8)
			adminUser.AutoGen = true
		}
	}
	envVars["AMQ_USER"] = adminUser

	adminPassword.Value = customResource.Spec.AdminPassword
	if adminPassword.Value == "" {
		if amqPasswordEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.Containers, "AMQ_PASSWORD"); nil != amqPasswordEnvVar {
			adminPassword.Value = amqPasswordEnvVar.Value
		}
		if adminPassword.Value == "" {
			adminPassword.Value = random.GenerateRandomString(8)
			adminPassword.AutoGen = true
		}
	}
	envVars["AMQ_PASSWORD"] = adminPassword

	envVars["AMQ_CLUSTER_USER"] = ValueInfo{
		Value:   random.GenerateRandomString(8),
		AutoGen: true,
	}
	envVars["AMQ_CLUSTER_PASSWORD"] = ValueInfo{
		Value:   random.GenerateRandomString(8),
		AutoGen: true,
	}

	reconciler.sourceEnvVarFromSecret(customResource, namer, currentStatefulSet, &envVars, secretName, client)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessDeploymentPlan(customResource *brokerv1beta1.ActiveMQArtemis, theNamer common.Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) {

	deploymentPlan := &customResource.Spec.DeploymentPlan

	reconciler.log.V(2).Info("Processing deployment plan", "plan", deploymentPlan, "broker cr", customResource.Name)

	reqestedReplicas := common.GetDeploymentSize(customResource)

	if nil == customResource.Spec.DeploymentPlan.MessageMigration {
		customResource.Spec.DeploymentPlan.MessageMigration = &defaultMessageMigration
	}

	if reconciler.CrConfiguredForControllerManagedScaleDown() {
		reconciler.controllerManagedScaledownViaConditions(customResource, currentStatefulSet, reqestedReplicas, client)
	} else {
		// Ensure the StatefulSet size is the same as the spec
		currentStatefulSet.Spec.Replicas = &reqestedReplicas

		reconciler.log.V(2).Info("Now sync Message migration", "for cr", customResource.Name)
		reconciler.syncMessageMigration(customResource, theNamer, client, scheme)
	}

	if customResource.Spec.DeploymentPlan.PodDisruptionBudget != nil {
		reconciler.applyPodDisruptionBudget(customResource)
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) controllerManagedScaledownViaConditions(customResource *brokerv1beta1.ActiveMQArtemis, currentStatefulSet *appsv1.StatefulSet, reqestedReplicas int32, client rtclient.Client) {
	if isClustered(customResource) &&
		*customResource.Spec.DeploymentPlan.MessageMigration &&
		currentStatefulSet != nil &&
		currentStatefulSet.Spec.Replicas != nil &&
		*currentStatefulSet.Spec.Replicas > 1 &&
		reqestedReplicas < *currentStatefulSet.Spec.Replicas {

		ordinalToDrain := OrdinalToDrain(currentStatefulSet)

		// state transition in the condition reason, so we need to find any existing to know the current state
		condition := meta.FindStatusCondition(customResource.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType)
		if condition == nil {
			condition = &metav1.Condition{
				Type:    brokerv1beta1.ScaleDownPendingConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  brokerv1beta1.ScaleDownPendingConditionPendingConfigAppliedReason,
				Message: reconciler.ScaleDownConditionPodNameMessage(ordinalToDrain),
			}
			reconciler.log.Info(fmt.Sprintf("scaledown starting on %s", condition.Message))
		}

		switch condition.Reason {

		case brokerv1beta1.ScaleDownPendingConditionPendingEmptyReason:
			{
				count, err := reconciler.GetTotalMessageCount(customResource, client, ordinalToDrain)
				if err != nil {
					condition.Message = reconciler.ScaleDownConditionMessage(ordinalToDrain, fmt.Sprintf("failed to get total message count, reason: %v", err))
				} else if count == 0 {
					// effect scaledown/resize by one
					currentStatefulSet.Spec.Replicas = &ordinalToDrain
					// transition
					condition.Reason = brokerv1beta1.ScaleDownPendingConditionPendingDeleteReason
					condition.Message = reconciler.ScaleDownConditionPodNameMessage(ordinalToDrain)
					reconciler.log.Info(fmt.Sprintf("scaledown complete on %s, empty pending delete", condition.Message))
				} else {
					condition.Message = reconciler.ScaleDownConditionMessage(ordinalToDrain, fmt.Sprintf("total pending count: %d", count))
					reconciler.log.Info(fmt.Sprintf("scaledown progressing %s", condition.Message))
					if err := reconciler.shutdownWithScaledown(client, ordinalToDrain); err != nil {
						condition.Message = reconciler.ScaleDownConditionMessage(ordinalToDrain, fmt.Sprintf("failed to migrate messages via scaledown, pending message count %d, reason: %v", count, err))
					}
				}
			}
		case brokerv1beta1.ScaleDownPendingConditionPendingDeleteReason:
			{
				if condition.Message == reconciler.ScaleDownConditionPodNameMessage(ordinalToDrain) {
					// resize of ss still pending
					currentStatefulSet.Spec.Replicas = &ordinalToDrain
				} else {
					// transition for next ordinal
					condition.Reason = brokerv1beta1.ScaleDownPendingConditionPendingConfigAppliedReason
					condition.Message = reconciler.ScaleDownConditionPodNameMessage(ordinalToDrain)
					reconciler.log.Info(fmt.Sprintf("scaledown starting on %s", condition.Message))
				}
			}
		}
		meta.SetStatusCondition(&customResource.Status.Conditions, *condition)

	} else {

		meta.RemoveStatusCondition(&customResource.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType)

		// Ensure the StatefulSet size is the same as the spec
		currentStatefulSet.Spec.Replicas = &reqestedReplicas
	}
}

func OrdinalToDrain(currentStatefulSet *appsv1.StatefulSet) int32 {
	return *currentStatefulSet.Spec.Replicas - 1
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CrConfiguredForControllerManagedScaleDown() bool {
	return slices.Contains(reconciler.customResource.Spec.BrokerProperties, ScaleDownConfigTrigger)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) shutdownWithScaledown(client rtclient.Client, ordinalToDrain int32) error {

	if err := reconciler.resolveAndValidateJolokia(client, ordinalToDrain); err != nil {
		return err
	}

	jk := reconciler.jolokiaEndpoints[ordinalToDrain]

	if _, err := jk.Artemis.ScaleDown(); err != nil {
		reconciler.log.Error(err, "failed to scaledown", "ordinal", ordinalToDrain)
		return fmt.Errorf("error on scaledown %w ", err)
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) GetTotalMessageCount(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, ordinalToDrain int32) (int64, error) {
	if err := reconciler.resolveAndValidateJolokia(client, ordinalToDrain); err != nil {
		return -1, err
	}
	jk := reconciler.jolokiaEndpoints[ordinalToDrain]
	result, err := jk.Artemis.GetTotalMessageCount()
	if err != nil {
		return -1, NewJolokiaClientsNotFoundError(fmt.Errorf("error on get total message count %w ", err))
	}

	return strconv.ParseInt(result, 10, 64)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) resolveAndValidateJolokia(client rtclient.Client, ordinalOfInterest int32) error {
	reconciler.resolveJolokiaEndpoints(reconciler.customResource, client)
	if len(reconciler.jolokiaEndpoints) == 0 {
		return NewJolokiaClientsNotFoundError(errors.New("waiting for jolokia endpoints to become available"))
	}
	if len(reconciler.jolokiaEndpoints) <= int(ordinalOfInterest) {
		return NewJolokiaClientsNotFoundError(errors.New(fmt.Sprintf("waiting for jolokia endpoint for ordinal %d to become available", ordinalOfInterest)))
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) applyPodDisruptionBudget(customResource *brokerv1beta1.ActiveMQArtemis) {

	var desired *policyv1.PodDisruptionBudget
	obj := reconciler.cloneOfDeployed(reflect.TypeOf(policyv1.PodDisruptionBudget{}), customResource.Name+"-pdb")

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
	matchLabels := map[string]string{"ActiveMQArtemis": customResource.Name}

	desired.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: matchLabels,
	}

	reconciler.trackDesired(desired)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessAcceptorsAndConnectors(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) error {

	if common.IsRestricted(customResource) {
		return nil
	}

	acceptorEntry, err := reconciler.generateAcceptorsString(customResource, client, currentStatefulSet)
	if err != nil {
		return err
	}
	connectorEntry, err := reconciler.generateConnectorsString(customResource, client, currentStatefulSet)
	if err != nil {
		return err
	}

	reconciler.configureAcceptorsExposure(customResource, namer, client)
	reconciler.configureConnectorsExposure(customResource, namer, client)

	envVars := make(map[string]ValueInfo)

	envVars["AMQ_ACCEPTORS"] = ValueInfo{
		Value:   acceptorEntry,
		AutoGen: false,
	}

	envVars["AMQ_CONNECTORS"] = ValueInfo{
		Value:   connectorEntry,
		AutoGen: false,
	}

	secretName := namer.SecretsNettyNameBuilder.Name()
	reconciler.sourceEnvVarFromSecret(customResource, namer, currentStatefulSet, &envVars, secretName, client)
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessConsole(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme, currentStatefulSet *appsv1.StatefulSet) error {

	reconciler.configureConsoleExposure(customResource, namer, client)
	if !customResource.Spec.Console.SSLEnabled || common.IsRestricted(customResource) {
		return nil
	}

	var err error

	secretName := namer.SecretsConsoleNameBuilder.Name()
	if customResource.Spec.Console.SSLSecret != "" {
		secretName = customResource.Spec.Console.SSLSecret
	}
	sslArgs, sslFlags, err := reconciler.generateCommonSSLFlags(customResource, secretName, customResource.Spec.Console.TrustSecret, "", client, true)
	if err != nil {
		return err
	}

	if customResource.Spec.Console.UseClientAuth {
		sslFlags = sslFlags + " --use-client-auth"
		sslArgs.NeedClientAuth = true
	}

	if len(sslArgs.PemCfgs) > 2 {
		reconciler.addPemConfigFileSecret(currentStatefulSet, sslArgs.PemCfgs)
		reconciler.appendSystemPropertiesForConsole(currentStatefulSet, sslArgs.ToSystemProperties())
	} else {
		envVars := map[string]ValueInfo{"AMQ_CONSOLE_ARGS": {
			Value:    sslFlags,
			AutoGen:  true,
			Internal: true,
		}}

		reconciler.sourceEnvVarFromSecret(customResource, namer, currentStatefulSet, &envVars, secretName, client)
	}

	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) appendSystemPropertiesForConsole(currentSS *appsv1.StatefulSet, systemArgs string) {
	reconciler.log.V(1).Info("Appending console system prop", "value", systemArgs)
	consoleProps := corev1.EnvVar{
		Name:  javaArgsAppendEnvVarName,
		Value: systemArgs,
	}
	environments.CreateOrAppend(currentSS.Spec.Template.Spec.Containers, &consoleProps)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) syncMessageMigration(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client, scheme *runtime.Scheme) {

	var err error = nil
	var retrieveError error = nil

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	ssNames := make(map[string]string)
	ssNames["CRNAMESPACE"] = customResource.Namespace
	ssNames["CRNAME"] = customResource.Name
	ssNames["HEADLESSSVCNAMEVALUE"] = namer.SvcHeadlessNameBuilder.Name()
	ssNames["PINGSVCNAMEVALUE"] = namer.SvcPingNameBuilder.Name()
	ssNames["SERVICE_ACCOUNT"] = os.Getenv("SERVICE_ACCOUNT")
	ssNames["SERVICE_ACCOUNT_NAME"] = os.Getenv("SERVICE_ACCOUNT")
	ssNames["AMQ_CREDENTIALS_SECRET_NAME"] = namer.SecretsCredentialsNameBuilder.Name()

	scaledown := &brokerv1beta1.ActiveMQArtemisScaledown{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ActiveMQArtemisScaledown",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:      namer.LabelBuilder.Labels(),
			Name:        customResource.Name,
			Namespace:   customResource.Namespace,
			Annotations: ssNames,
		},
		Spec: brokerv1beta1.ActiveMQArtemisScaledownSpec{
			LocalOnly: isLocalOnly(),
			Resources: customResource.Spec.DeploymentPlan.Resources,
		},
		Status: brokerv1beta1.ActiveMQArtemisScaledownStatus{},
	}

	if nil == customResource.Spec.DeploymentPlan.MessageMigration {
		customResource.Spec.DeploymentPlan.MessageMigration = &defaultMessageMigration
	}

	clustered := isClustered(customResource)

	if *customResource.Spec.DeploymentPlan.MessageMigration && clustered {
		if !customResource.Spec.DeploymentPlan.PersistenceEnabled {
			reconciler.log.V(2).Info("Won't set up scaledown for non persistent deployment")
			return
		}
		reconciler.log.V(2).Info("we need scaledown for this cr", "crName", customResource.Name, "scheme", scheme)
		if err = resources.Retrieve(namespacedName, client, scaledown); err != nil {
			// err means not found so create
			reconciler.log.V(2).Info("Creating builtin drainer CR ", "scaledown", scaledown)
			if retrieveError = resources.Create(customResource, client, scheme, scaledown); retrieveError == nil {
				reconciler.log.V(2).Info("drainer created successfully", "drainer", scaledown)
			} else {
				reconciler.log.Error(retrieveError, "we have error retrieving drainer", "drainer", scaledown, "scheme", scheme)
			}
		}
	} else {
		if err = resources.Retrieve(namespacedName, client, scaledown); err == nil {
			//	ReleaseController(customResource.Name)
			// err means not found so delete
			resources.Delete(client, scaledown)
		}
	}
}

func isLocalOnly() bool {
	oprNamespace := os.Getenv("OPERATOR_NAMESPACE")
	watchNamespace := os.Getenv("OPERATOR_WATCH_NAMESPACE")
	return oprNamespace == watchNamespace
}

func (reconciler *ActiveMQArtemisReconcilerImpl) sourceEnvVarFromSecret(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, currentStatefulSet *appsv1.StatefulSet, envVars *map[string]ValueInfo, secretName string, client rtclient.Client) {

	var log = reconciler.log.WithName("controller_v1beta1activemqartemis").WithName("sourceEnvVarFromSecret")

	namespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: currentStatefulSet.Namespace,
	}
	// Attempt to retrieve the secret
	dataMap := make(map[string][]byte)
	internalDataMap := make(map[string][]byte)

	for k, v := range *envVars {
		if v.Internal {
			internalDataMap[k] = []byte(v.Value)
		} else {
			dataMap[k] = []byte(v.Value)
		}
	}

	desired := false
	secretDefinition := secrets.NewSecret(namespacedName, dataMap, namer.LabelBuilder.Labels())

	if err := resources.Retrieve(namespacedName, client, secretDefinition); err != nil {
		if k8serrors.IsNotFound(err) {
			log.V(1).Info("Did not find secret", "key", namespacedName)
		} else {
			log.Error(err, "Error while retrieving secret", "key", namespacedName)
		}
		if len(dataMap) > 0 {
			// we need to create and track
			desired = true
		}
	} else {
		log.V(2).Info("updating from " + secretName)
		for k, envVar := range *envVars {
			if envVar.Internal {
				// goes in internal secret
				continue
			}
			elem, exists := secretDefinition.Data[k]
			if strings.Compare(string(elem), (*envVars)[k].Value) != 0 || !exists {
				log.V(2).Info("key value not equals or does not exist", "key", k, "exists", exists)
				if !(*envVars)[k].AutoGen || string(elem) == "" {
					secretDefinition.Data[k] = []byte((*envVars)[k].Value)
				}
			}
		}
		//if operator doesn't own it, don't track
		if len(secretDefinition.OwnerReferences) > 0 {
			for _, or := range secretDefinition.OwnerReferences {
				if or.Kind == "ActiveMQArtemis" && or.Name == customResource.Name {
					desired = true
				}
			}
		}

		if !desired {
			log.V(1).Info("The secret " + secretName + " is ignored because its onwer references doesn't include ActiveMQArtemis/" + customResource.Name)
		}
	}

	if desired {
		// ensure processResources sees it
		reconciler.trackDesired(secretDefinition)
	}

	internalSecretName := secretName + "-internal"
	internalNamespacedName := types.NamespacedName{
		Name:      internalSecretName,
		Namespace: currentStatefulSet.Namespace,
	}

	if len(internalDataMap) > 0 {

		var internalSecretDefinition *corev1.Secret
		obj := reconciler.cloneOfDeployed(reflect.TypeOf(corev1.Secret{}), internalSecretName)
		if obj != nil {
			log.V(2).Info("updating from " + internalSecretName)
			internalSecretDefinition = obj.(*corev1.Secret)
		} else {
			internalSecretDefinition = secrets.NewSecret(internalNamespacedName, internalDataMap, namer.LabelBuilder.Labels())
			reconciler.adoptExistingSecretWithNoOwnerRefForUpdate(customResource, internalSecretDefinition, client)
		}

		// apply our desired data
		internalSecretDefinition.ObjectMeta.Labels = namer.LabelBuilder.Labels()
		internalSecretDefinition.Data = internalDataMap

		reconciler.trackDesired(internalSecretDefinition)
	}

	log.V(2).Info("Populating env vars references, in order, from secret " + secretName)

	// sort the keys for consistency of reconcilitation as iteration source is a map
	sortedKeys := make([]string, 0, len(*envVars))
	for k := range *envVars {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, envVarName := range sortedKeys {
		envVarInfo := (*envVars)[envVarName]
		secretNameToUse := secretName
		if envVarInfo.Internal {
			secretNameToUse = internalSecretName
		}
		envVarSource := &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretNameToUse,
				},
				Key:      envVarName,
				Optional: nil,
			},
		}

		envVarDefinition := &corev1.EnvVar{
			Name:      envVarName,
			Value:     "",
			ValueFrom: envVarSource,
		}
		if retrievedEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.Containers, envVarName); nil == retrievedEnvVar {
			log.V(2).Info("containers: failed to retrieve " + envVarName + " creating")
			environments.Create(currentStatefulSet.Spec.Template.Spec.Containers, envVarDefinition)
		}

		//custom init container
		if len(currentStatefulSet.Spec.Template.Spec.InitContainers) > 0 {
			if retrievedEnvVar := environments.Retrieve(currentStatefulSet.Spec.Template.Spec.InitContainers, envVarName); nil == retrievedEnvVar {
				log.V(2).Info("init_containers: failed to retrieve " + envVarName + " creating")
				environments.Create(currentStatefulSet.Spec.Template.Spec.InitContainers, envVarDefinition)
			}
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) processSSLSecret(secretName string, customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) (*corev1.Secret, error) {

	if strings.HasSuffix(secretName, certutil.Cert_provided_secret_suffix) {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName}}, nil
	}

	var trackedSecret *corev1.Secret = nil

	// validate if secret exists
	secretKey := types.NamespacedName{Name: secretName, Namespace: customResource.Namespace}
	theSecret := &corev1.Secret{}
	if err := resources.Retrieve(secretKey, client, theSecret); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("required secret not exist %s", secretName)
	}

	trackedSecret = theSecret
	//if operator doesn't own it, don't track
	if len(trackedSecret.OwnerReferences) > 0 {
		for _, or := range trackedSecret.OwnerReferences {
			if or.Kind == "ActiveMQArtemis" && or.Name == customResource.Name {
				reconciler.trackDesired(trackedSecret)
			}
		}
	}
	return trackedSecret, nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) generateAcceptorsString(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, currentSS *appsv1.StatefulSet) (string, error) {

	// TODO: Optimize for the single broker configuration
	ensureCOREOn61616Exists := true // as clustered is no longer an option but true by default

	acceptorEntry := ""
	defaultArgs := "tcpSendBufferSize=1048576;tcpReceiveBufferSize=1048576;useEpoll=true;amqpCredits=1000;amqpMinCredits=300"

	var portIncrement int32 = 10
	var currentPortIncrement int32 = 0
	var port61616InUse bool = false
	var i uint32 = 0
	for _, acceptor := range customResource.Spec.Acceptors {
		if acceptor.Port == 0 {
			acceptor.Port = 61626 + currentPortIncrement
			currentPortIncrement += portIncrement
			customResource.Spec.Acceptors[i].Port = acceptor.Port
		}
		if acceptor.Protocols == "" ||
			strings.ToLower(acceptor.Protocols) == "all" {
			acceptor.Protocols = "AMQP,CORE,HORNETQ,MQTT,OPENWIRE,STOMP"
		}
		bindAddress := "ACCEPTOR_IP"
		if acceptor.BindToAllInterfaces != nil && *acceptor.BindToAllInterfaces {
			bindAddress = "0.0.0.0"
		}
		acceptorEntry = acceptorEntry + "<acceptor name=\"" + acceptor.Name + "\">"
		acceptorEntry = acceptorEntry + "tcp:" + "\\/\\/" + bindAddress + ":"
		acceptorEntry = acceptorEntry + fmt.Sprintf("%d", acceptor.Port)
		acceptorEntry = acceptorEntry + "?protocols=" + strings.ToUpper(acceptor.Protocols)
		// TODO: Evaluate more dynamic messageMigration
		if acceptor.Port == 61616 {
			port61616InUse = true
		}
		if ensureCOREOn61616Exists &&
			(acceptor.Port == 61616) &&
			!strings.Contains(strings.ToUpper(acceptor.Protocols), "CORE") {
			acceptorEntry = acceptorEntry + ",CORE"
		}

		if acceptor.SSLEnabled {
			secretName := customResource.Name + "-" + acceptor.Name + "-secret"
			if acceptor.SSLSecret != "" {
				secretName = acceptor.SSLSecret
			}
			secretToUse, err := reconciler.processSSLSecret(secretName, customResource, client)
			if err != nil {
				return "", err
			}

			sslArgs, sslFlags, err := reconciler.generateCommonSSLFlags(customResource, secretToUse.Name, acceptor.TrustSecret, acceptor.TrustStoreType, client, false)
			if err != nil {
				return "", err
			}
			acceptorEntry = acceptorEntry + ";" + sslFlags

			if len(sslArgs.PemCfgs) > 2 {
				reconciler.addPemConfigFileSecret(currentSS, sslArgs.PemCfgs)
			}

			sslOptionalArguments := reconciler.generateAcceptorSSLOptionalArguments(acceptor)

			if sslOptionalArguments != "" {
				acceptorEntry = acceptorEntry + ";" + sslOptionalArguments
			}
		}

		if acceptor.AnycastPrefix != "" {
			safeAnycastPrefix := strings.Replace(acceptor.AnycastPrefix, "/", "\\/", -1)
			acceptorEntry = acceptorEntry + ";" + "anycastPrefix=" + safeAnycastPrefix
		}
		if acceptor.MulticastPrefix != "" {
			safeMulticastPrefix := strings.Replace(acceptor.MulticastPrefix, "/", "\\/", -1)
			acceptorEntry = acceptorEntry + ";" + "multicastPrefix=" + safeMulticastPrefix
		}
		if acceptor.ConnectionsAllowed > 0 {
			acceptorEntry = acceptorEntry + ";" + "connectionsAllowed=" + fmt.Sprintf("%d", acceptor.ConnectionsAllowed)
		}
		if (acceptor.AMQPMinLargeMessageSize > 0) ||
			(acceptor.AMQPMinLargeMessageSize == -1) {
			acceptorEntry = acceptorEntry + ";" + "amqpMinLargeMessageSize=" + fmt.Sprintf("%d", acceptor.AMQPMinLargeMessageSize)
		}
		if acceptor.SupportAdvisory != nil {
			acceptorEntry = acceptorEntry + ";" + "supportAdvisory=" + strconv.FormatBool(*acceptor.SupportAdvisory)
		}
		if acceptor.SuppressInternalManagementObjects != nil {
			acceptorEntry = acceptorEntry + ";" + "suppressInternalManagementObjects=" + strconv.FormatBool(*acceptor.SuppressInternalManagementObjects)
		}
		acceptorEntry = acceptorEntry + ";" + defaultArgs

		acceptorEntry = acceptorEntry + "<\\/acceptor>"

		// Used for indexing the original acceptor port to update it if required
		i = i + 1
	}
	// TODO: Evaluate more dynamic messageMigration
	if ensureCOREOn61616Exists && !port61616InUse {
		acceptorEntry = acceptorEntry + "<acceptor name=\"" + "scaleDown" + "\">"
		acceptorEntry = acceptorEntry + "tcp:" + "\\/\\/" + "ACCEPTOR_IP:"
		acceptorEntry = acceptorEntry + fmt.Sprintf("%d", 61616)
		acceptorEntry = acceptorEntry + "?protocols=" + "CORE"
		acceptorEntry = acceptorEntry + ";" + defaultArgs
		// TODO: SSL
		acceptorEntry = acceptorEntry + "<\\/acceptor>"
	}

	return acceptorEntry, nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) generateConnectorsString(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, currentSS *appsv1.StatefulSet) (string, error) {

	connectorEntry := ""
	connectors := customResource.Spec.Connectors
	for _, connector := range connectors {
		if connector.Type == "" {
			connector.Type = "tcp"
		}
		connectorEntry = connectorEntry + "<connector name=\"" + connector.Name + "\">"
		connectorEntry = connectorEntry + strings.ToLower(connector.Type) + ":\\/\\/" + strings.ToLower(connector.Host) + ":"
		connectorEntry = connectorEntry + fmt.Sprintf("%d", connector.Port)

		if connector.SSLEnabled {
			secretName := customResource.Name + "-" + connector.Name + "-secret"
			if connector.SSLSecret != "" {
				secretName = connector.SSLSecret
			}
			secretToUse, err := reconciler.processSSLSecret(secretName, customResource, client)
			if err != nil {
				return "", err
			}

			sslArgs, sslOpts, err := reconciler.generateCommonSSLFlags(customResource, secretToUse.Name, connector.TrustSecret, connector.TrustStoreType, client, false)

			if err != nil {
				return "", err
			}
			connectorEntry = connectorEntry + "?" + sslOpts

			if len(sslArgs.PemCfgs) > 2 {
				reconciler.addPemConfigFileSecret(currentSS, sslArgs.PemCfgs)
			}

			sslOptionalArguments := reconciler.generateConnectorSSLOptionalArguments(connector)

			if sslOptionalArguments != "" {
				connectorEntry = connectorEntry + ";" + sslOptionalArguments
			}
		}
		connectorEntry = connectorEntry + "<\\/connector>"
	}

	return connectorEntry, nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureAcceptorsExposure(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client) {
	originalLabels := namer.LabelBuilder.Labels()
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	deploymentSize := common.GetDeploymentSize(customResource)
	for i := int32(0); i < deploymentSize; i++ {
		ordinalString := strconv.Itoa(int(i))
		var serviceRoutelabels = make(map[string]string)
		for k, v := range originalLabels {
			serviceRoutelabels[k] = v
		}
		serviceRoutelabels[PodNameLabelKey] = namer.SsNameBuilder.Name() + "-" + ordinalString

		for _, acceptor := range customResource.Spec.Acceptors {
			nameSuffix := acceptor.Name + "-" + ordinalString
			serviceName := types.NamespacedName{Namespace: namespacedName.Namespace, Name: namespacedName.Name + "-" + nameSuffix + "-" + ServiceTypePostfix}
			serviceDefinition := reconciler.ServiceDefinitionForCR(serviceName, client, nameSuffix, acceptor.Port, serviceRoutelabels, namer.LabelBuilder.Labels())
			reconciler.checkExistingService(customResource, serviceDefinition, client)
			reconciler.trackDesired(serviceDefinition)

			if acceptor.Expose {
				exposureDefinition := reconciler.ExposureDefinitionForCR(customResource, namespacedName, serviceRoutelabels, acceptor.SSLEnabled, acceptor.IngressHost, ordinalString, acceptor.Name, acceptor.ExposeMode)
				reconciler.trackDesired(exposureDefinition)
			}
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ServiceDefinitionForCR(serviceName types.NamespacedName, client rtclient.Client, nameSuffix string, portNumber int32, selectorLabels map[string]string, labels map[string]string) *corev1.Service {
	var serviceDefinition *corev1.Service
	obj := reconciler.cloneOfDeployed(reflect.TypeOf(corev1.Service{}), serviceName.Name)
	if obj != nil {
		serviceDefinition = obj.(*corev1.Service)
	}
	return svc.NewServiceDefinitionForCR(serviceName, client, nameSuffix, portNumber, selectorLabels, labels, serviceDefinition)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ExposureDefinitionForCR(customResource *brokerv1beta1.ActiveMQArtemis, namespacedName types.NamespacedName, labels map[string]string, passthroughTLS bool, ingressHost string, ordinalString string, itemName string, exposeMode *brokerv1beta1.ExposeMode) rtclient.Object {

	targetPortName := itemName + "-" + ordinalString
	targetServiceName := customResource.Name + "-" + targetPortName + "-" + ServiceTypePostfix

	exposeWithRoute := (exposeMode == nil && reconciler.isOnOpenShift) || (exposeMode != nil && *exposeMode == brokerv1beta1.ExposeModes.Route)

	if exposeWithRoute {
		reconciler.log.V(1).Info("creating route for "+targetPortName, "service", targetServiceName)

		var existing *routev1.Route = nil
		obj := reconciler.cloneOfDeployed(reflect.TypeOf(routev1.Route{}), targetServiceName+"-"+RouteTypePostfix)
		if obj != nil {
			existing = obj.(*routev1.Route)
		}
		brokerHost := formatTemplatedString(customResource, ingressHost, ordinalString, itemName, RouteTypePostfix)
		return routes.NewRouteDefinitionForCR(existing, namespacedName, labels, targetServiceName, targetPortName, passthroughTLS, customResource.Spec.IngressDomain, brokerHost)
	} else {
		reconciler.log.V(1).Info("creating ingress for "+targetPortName, "service", targetServiceName)

		var existing *netv1.Ingress = nil
		obj := reconciler.cloneOfDeployed(reflect.TypeOf(netv1.Ingress{}), targetServiceName+"-"+IngressTypePostfix)
		if obj != nil {
			existing = obj.(*netv1.Ingress)
		}
		brokerHost := formatTemplatedString(customResource, ingressHost, ordinalString, itemName, IngressTypePostfix)
		return ingresses.NewIngressForCRWithSSL(existing, namespacedName, labels, targetServiceName, targetPortName, passthroughTLS, customResource.Spec.IngressDomain, brokerHost, reconciler.isOnOpenShift)
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) trackDesired(desired rtclient.Object) {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) getFromDesired(kind reflect.Type, name string) rtclient.Object {
	obj, found := reconciler.requestedResources[kind][name]
	if found {
		return obj
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) applyTemplates(desired rtclient.Object) (err error) {
	for index, template := range reconciler.customResource.Spec.ResourceTemplates {
		if err = reconciler.applyTemplate(index, template, desired); err != nil {
			break
		}
	}
	return err
}

func (reconciler *ActiveMQArtemisReconcilerImpl) applyTemplate(index int, template brokerv1beta1.ResourceTemplate, target rtclient.Object) error {
	if match(template, target) {

		reconciler.matchedTemplates[index] = true

		ordinal := extractOrdinal(target)
		itemName := extractItemName(target)
		resType := extractResType(target)
		if len(template.Annotations) > 0 {
			modified := make(map[string]string)
			for key, value := range target.GetAnnotations() {
				modified[key] = value
			}
			for key, value := range template.Annotations {
				reconciler.applyFormattedKeyValue(modified, ordinal, itemName, resType, key, value)
			}
			target.SetAnnotations(modified)
		}
		if len(template.Labels) > 0 {
			modified := make(map[string]string)
			for key, value := range target.GetLabels() {
				modified[key] = value
			}
			for key, value := range template.Labels {
				reconciler.applyFormattedKeyValue(modified, ordinal, itemName, resType, key, value)
			}
			target.SetLabels(modified)
		}

		if template.Patch != nil {

			// apply any patch
			converter := runtime.DefaultUnstructuredConverter

			var err error
			var targetAsUnstructured map[string]interface{}

			if targetAsUnstructured, err = converter.ToUnstructured(target); err == nil {
				// patch, part of our CR, needs to be mutable
				patch := formatTemplatedObject(reconciler.customResource, template.Patch.Object, ordinal, itemName, resType).(map[string]interface{})
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

func formatTemplatedObject(customResource *brokerv1beta1.ActiveMQArtemis, object interface{}, ordinal string, itemName string, resType string) interface{} {
	if objectMap, isObjectMap := object.(map[string]interface{}); isObjectMap {
		targetMap := make(map[string]interface{})
		for objectMapKey, objectMapValue := range objectMap {
			targetMap[objectMapKey] = formatTemplatedObject(customResource, objectMapValue, ordinal, itemName, resType)
		}
		return targetMap
	} else if objectArray, isObjectArray := object.([]interface{}); isObjectArray {
		targetArray := make([]interface{}, len(objectArray))
		for objectArrayIndex, objectArrayValue := range objectArray {
			targetArray[objectArrayIndex] = formatTemplatedObject(customResource, objectArrayValue, ordinal, itemName, resType)
		}
		return targetArray
	} else if objectString, isObjectString := object.(string); isObjectString {
		return formatTemplatedString(customResource, objectString, ordinal, itemName, resType)
	}
	return object
}

func (reconciler *ActiveMQArtemisReconcilerImpl) applyFormattedKeyValue(collection map[string]string, ordinal string, itemName string, resType string, key string, value string) {
	formattedKey := formatTemplatedString(reconciler.customResource, key, ordinal, itemName, resType)
	if value == RemoveKeySpecialValue {
		delete(collection, formattedKey)
	} else {
		collection[formattedKey] = formatTemplatedString(reconciler.customResource, value, ordinal, itemName, resType)
	}
}

func extractItemName(desired rtclient.Object) string {
	return desired.GetName()
}

func extractResType(desired rtclient.Object) string {
	switch desired.(type) {
	case *corev1.Service:
		{
			return ServiceTypePostfix
		}
	case *netv1.Ingress:
		{
			return IngressTypePostfix
		}

	case *routev1.Route:
		{
			return RouteTypePostfix
		}
	}
	return "undefined-res-type"
}

func extractOrdinal(desired rtclient.Object) string {
	var ordinal = "undefined-ordinal"
	switch desired := desired.(type) {
	case *corev1.Service:
		{
			podName, found := desired.Spec.Selector[PodNameLabelKey]
			if found {
				ordinal = ordinalFromPodName(podName)
			}

		}
	case *netv1.Ingress, *routev1.Route:
		{
			podName, found := desired.GetLabels()[PodNameLabelKey]
			if found {
				ordinal = ordinalFromPodName(podName)
			}
		}
	}
	return ordinal
}

func ordinalFromPodName(podName string) string {
	return podName[strings.LastIndex(podName, "-")+1:]
}

func match(template brokerv1beta1.ResourceTemplate, target rtclient.Object) bool {
	if template.Selector == nil {
		return true
	}

	var groupVersion = ""
	if template.Selector.APIGroup != nil {
		groupVersion = *template.Selector.APIGroup
	}

	templateGv, _ := schema.ParseGroupVersion(groupVersion)

	var kind = ""
	if template.Selector.Kind != nil {
		kind = *template.Selector.Kind
	}

	templateGvk := templateGv.WithKind(kind)
	targetGvk := target.GetObjectKind().GroupVersionKind()

	if templateGvk.Group != "" {
		if templateGvk.Group != targetGvk.Group {
			return false
		}
	}

	if templateGvk.Version != "" {
		if templateGvk.Version != targetGvk.Version {
			return false
		}
	}

	if templateGvk.Kind != "" {
		if templateGvk.Kind != targetGvk.Kind {
			return false
		}
	}

	if template.Selector.Name != nil {
		nameMatcher, _ := regexp.Compile(*template.Selector.Name)
		if !nameMatcher.Match([]byte(target.GetName())) {
			return false
		}
	}

	return true
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureConnectorsExposure(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client) {
	originalLabels := namer.LabelBuilder.Labels()
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	deploymentSize := common.GetDeploymentSize(customResource)
	for i := int32(0); i < deploymentSize; i++ {
		ordinalString := strconv.Itoa(int(i))
		var serviceRoutelabels = make(map[string]string)
		for k, v := range originalLabels {
			serviceRoutelabels[k] = v
		}
		serviceRoutelabels[PodNameLabelKey] = namer.SsNameBuilder.Name() + "-" + ordinalString

		for _, connector := range customResource.Spec.Connectors {

			nameSuffix := connector.Name + "-" + ordinalString
			serviceName := types.NamespacedName{Namespace: namespacedName.Namespace, Name: namespacedName.Name + "-" + nameSuffix + "-" + ServiceTypePostfix}
			serviceDefinition := reconciler.ServiceDefinitionForCR(serviceName, client, nameSuffix, connector.Port, serviceRoutelabels, namer.LabelBuilder.Labels())
			reconciler.checkExistingService(customResource, serviceDefinition, client)
			reconciler.trackDesired(serviceDefinition)

			if connector.Expose {

				exposureDefinition := reconciler.ExposureDefinitionForCR(customResource, namespacedName, serviceRoutelabels, connector.SSLEnabled, connector.IngressHost, ordinalString, connector.Name, connector.ExposeMode)

				reconciler.trackDesired(exposureDefinition)
			}
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureConsoleExposure(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, client rtclient.Client) {
	console := customResource.Spec.Console
	consoleName := customResource.Spec.Console.Name

	if consoleName == "" {
		consoleName = "wconsj"
	}

	originalLabels := namer.LabelBuilder.Labels()
	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	targetPort := int32(8161)
	portNumber := int32(8162)
	deploymentSize := common.GetDeploymentSize(customResource)
	for i := int32(0); i < deploymentSize; i++ {
		ordinalString := strconv.Itoa(int(i))
		var serviceRoutelabels = make(map[string]string)
		for k, v := range originalLabels {
			serviceRoutelabels[k] = v
		}
		serviceRoutelabels[PodNameLabelKey] = namer.SsNameBuilder.Name() + "-" + ordinalString

		targetPortName := consoleName + "-" + ordinalString
		targetServiceName := customResource.Name + "-" + targetPortName + "-" + ServiceTypePostfix
		serviceName := types.NamespacedName{Namespace: namespacedName.Namespace, Name: targetServiceName}
		serviceDefinition := reconciler.ServiceDefinitionForCR(serviceName, client, consoleName, targetPort, serviceRoutelabels, namer.LabelBuilder.Labels())

		serviceDefinition.Spec.Ports = append(serviceDefinition.Spec.Ports, corev1.ServicePort{
			Name:       targetPortName,
			Port:       portNumber,
			Protocol:   "TCP",
			TargetPort: intstr.FromInt(int(targetPort)),
		})
		if console.Expose {
			reconciler.checkExistingService(customResource, serviceDefinition, client)
			reconciler.trackDesired(serviceDefinition)

			exposeWithRoute := (console.ExposeMode == nil && reconciler.isOnOpenShift) || (console.ExposeMode != nil && *console.ExposeMode == brokerv1beta1.ExposeModes.Route)

			if exposeWithRoute {
				reconciler.log.V(2).Info("routeDefinition for " + targetPortName)
				var existing *routev1.Route = nil
				obj := reconciler.cloneOfDeployed(reflect.TypeOf(routev1.Route{}), targetServiceName+"-"+RouteTypePostfix)
				if obj != nil {
					existing = obj.(*routev1.Route)
				}
				brokerHost := formatTemplatedString(customResource, customResource.Spec.Console.IngressHost, ordinalString, consoleName, RouteTypePostfix)
				routeDefinition := routes.NewRouteDefinitionForCR(existing, namespacedName, serviceRoutelabels, targetServiceName, targetPortName, console.SSLEnabled, customResource.Spec.IngressDomain, brokerHost)
				reconciler.trackDesired(routeDefinition)

			} else {
				reconciler.log.V(2).Info("ingress for " + targetPortName)
				var existing *netv1.Ingress = nil
				obj := reconciler.cloneOfDeployed(reflect.TypeOf(netv1.Ingress{}), targetServiceName+"-"+IngressTypePostfix)
				if obj != nil {
					existing = obj.(*netv1.Ingress)
				}
				brokerHost := formatTemplatedString(customResource, customResource.Spec.Console.IngressHost, ordinalString, consoleName, IngressTypePostfix)
				ingressDefinition := ingresses.NewIngressForCRWithSSL(existing, namespacedName, serviceRoutelabels, targetServiceName, targetPortName, console.SSLEnabled, customResource.Spec.IngressDomain, brokerHost, reconciler.isOnOpenShift)
				reconciler.trackDesired(ingressDefinition)
			}
		}
	}
}

func formatTemplatedString(customResource *brokerv1beta1.ActiveMQArtemis, template string, brokerOrdinal string, itemName string, resType string) string {
	if template != "" {
		template = strings.Replace(template, "$(CR_NAME)", customResource.Name, -1)
		template = strings.Replace(template, "$(CR_NAMESPACE)", customResource.Namespace, -1)
		template = strings.Replace(template, "$(BROKER_ORDINAL)", brokerOrdinal, -1)
		template = strings.Replace(template, "$(ITEM_NAME)", itemName, -1)
		template = strings.Replace(template, "$(RES_TYPE)", resType, -1)
		template = strings.Replace(template, "$(INGRESS_DOMAIN)", customResource.Spec.IngressDomain, -1)
	}
	return template
}

func (reconciler *ActiveMQArtemisReconcilerImpl) generateCommonSSLFlags(customResource *brokerv1beta1.ActiveMQArtemis, secretName string, caSecretName *string, trustStoreType string, client rtclient.Client, isConsole bool) (*certutil.SslArguments, string, error) {

	sslSecret := &corev1.Secret{}
	if strings.HasSuffix(secretName, certutil.Cert_provided_secret_suffix) {
		sslSecret.ObjectMeta = metav1.ObjectMeta{Name: secretName}
	} else {
		secretNamespacedName := types.NamespacedName{
			Name:      secretName,
			Namespace: customResource.Namespace,
		}

		if err := resources.Retrieve(secretNamespacedName, client, sslSecret); err != nil {
			return nil, "", err
		}
	}

	// for trust manager ca bundle.
	// in user secret case it can have both keystore and truststore so a separate ca bundle is optional
	var caSecret *corev1.Secret = nil
	if caSecretName != nil {
		trustSecretNamespacedName := types.NamespacedName{
			Name:      *caSecretName,
			Namespace: customResource.Namespace,
		}

		caSecret = &corev1.Secret{}

		if err := resources.Retrieve(trustSecretNamespacedName, client, caSecret); err != nil {
			return nil, "", err
		}
	}

	sslArgs, err := certutil.GetSslArgumentsFromSecret(sslSecret, trustStoreType, caSecret, isConsole)
	if err != nil {
		return nil, "", err
	}

	sslFlags := sslArgs.ToFlags()

	return sslArgs, sslFlags, nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) generateAcceptorSSLOptionalArguments(acceptor brokerv1beta1.AcceptorType) string {

	sslOptionalArguments := ""

	if acceptor.EnabledCipherSuites != "" {
		sslOptionalArguments = sslOptionalArguments + "enabledCipherSuites=" + acceptor.EnabledCipherSuites
	}
	if acceptor.EnabledProtocols != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "enabledProtocols=" + acceptor.EnabledProtocols
	}
	if acceptor.NeedClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "needClientAuth=true"
	}
	if acceptor.WantClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "wantClientAuth=true"
	}
	if acceptor.VerifyHost {
		sslOptionalArguments = sslOptionalArguments + ";" + "verifyHost=true"
	}
	if acceptor.SSLProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sslProvider=" + acceptor.SSLProvider
	}
	if acceptor.SNIHost != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sniHost=" + acceptor.SNIHost
	}

	if acceptor.KeyStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "keyStoreProvider=" + acceptor.KeyStoreProvider
	}

	if acceptor.TrustStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreProvider=" + acceptor.TrustStoreProvider
	}

	return sslOptionalArguments
}

func (reconciler *ActiveMQArtemisReconcilerImpl) addPemConfigFileSecret(ss *appsv1.StatefulSet, configs []string) {
	resourceName := types.NamespacedName{
		Namespace: ss.Namespace,
		Name:      certutil.CfgToSecretName(configs[0]),
	}

	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, "source.key=", configs[1])
	fmt.Fprintln(buf, "source.cert=", configs[2])

	contents := map[string][]byte{configs[0]: buf.Bytes()}

	var desired *corev1.Secret

	obj := reconciler.cloneOfDeployed(reflect.TypeOf(corev1.Secret{}), resourceName.Name)
	if obj != nil {
		desired = obj.(*corev1.Secret)
		desired.Data = contents
	} else {
		desired = secrets.NewSecret(resourceName, contents, nil)
	}
	reconciler.trackDesired(desired)
	volume := volumes.MakeVolumeForSecret(desired.Name)

	if !common.HasVolumeInStatefulset(ss, volume.Name) {
		ss.Spec.Template.Spec.Volumes = append(ss.Spec.Template.Spec.Volumes, volume)
	}

	secretVolumeMount := volumes.MakeVolumeMount(volume.Name)
	if !common.HasVolumeMount(&ss.Spec.Template.Spec.Containers[0], secretVolumeMount.Name) {
		ss.Spec.Template.Spec.Containers[0].VolumeMounts = append(ss.Spec.Template.Spec.Containers[0].VolumeMounts, secretVolumeMount)
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) generateConnectorSSLOptionalArguments(connector brokerv1beta1.ConnectorType) string {

	sslOptionalArguments := ""

	if connector.EnabledCipherSuites != "" {
		sslOptionalArguments = sslOptionalArguments + "enabledCipherSuites=" + connector.EnabledCipherSuites
	}
	if connector.EnabledProtocols != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "enabledProtocols=" + connector.EnabledProtocols
	}
	if connector.NeedClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "needClientAuth=true"
	}
	if connector.WantClientAuth {
		sslOptionalArguments = sslOptionalArguments + ";" + "wantClientAuth=true"
	}
	if connector.VerifyHost {
		sslOptionalArguments = sslOptionalArguments + ";" + "verifyHost=true"
	}
	if connector.SSLProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sslProvider=" + connector.SSLProvider
	}
	if connector.SNIHost != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "sniHost=" + connector.SNIHost
	}

	if connector.KeyStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "keyStoreProvider=" + connector.KeyStoreProvider
	}

	if connector.TrustStoreType != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreType=" + connector.TrustStoreType
	}

	if connector.TrustStoreProvider != "" {
		sslOptionalArguments = sslOptionalArguments + ";" + "trustStoreProvider=" + connector.TrustStoreProvider
	}

	return sslOptionalArguments
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CurrentDeployedResources(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) {
	reqLogger := reconciler.log.WithValues("ActiveMQArtemis Name", customResource.Name)

	var err error
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		reconciler.checkExistingPersistentVolumes(customResource, client)
	}

	reconciler.deployed, err = common.GetDeployedResources(customResource, client, reconciler.isOnOpenShift)
	if err != nil {
		reqLogger.Error(err, "error getting deployed resources")
		return
	}

	// track persisted cr secret
	for _, secret := range reconciler.deployed[reflect.TypeOf(corev1.Secret{})] {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessResources(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) (err error) {

	reqLogger := reconciler.log.WithValues("ActiveMQArtemis Name", customResource.Name)

	for _, requested := range common.ToResourceList(reconciler.requestedResources) {
		requested.SetNamespace(customResource.Namespace)
		if err = reconciler.applyTemplates(requested); err != nil {
			return err
		}
	}

	reqLogger.V(1).Info("Processing resources", "num requested", countOfRequested(reconciler), "num current", countOfDeployed(reconciler))

	requested := compare.NewMapBuilder().Add(common.ToResourceList(reconciler.requestedResources)...).ResourceMap()

	comparator := compare.MapComparator{
		Comparator: compare.SimpleComparator(),
	}
	comparator.Comparator.SetDefaultComparator(reconciler.CompareMetaAndSpec)
	comparator.Comparator.SetComparator(reflect.TypeOf(corev1.Secret{}), reconciler.CompareSecret)
	comparator.Comparator.SetComparator(reflect.TypeOf(corev1.ConfigMap{}), reconciler.CompareConfigMap)

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
		validationCondition := meta.FindStatusCondition(customResource.Status.Conditions, brokerv1beta1.ValidConditionType)

		// Only set to Unknown if there is no fatal validation error
		if validationCondition == nil || validationCondition.Status != metav1.ConditionFalse {
			message := fmt.Sprintf("ResourceTemplate at index %d did not match any operator-generated resources", unmatchedIndices[0])
			meta.SetStatusCondition(&customResource.Status.Conditions, metav1.Condition{
				Type:               brokerv1beta1.ValidConditionType,
				Status:             metav1.ConditionUnknown,
				Reason:             brokerv1beta1.ValidConditionUnknownReason,
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

func countOfRequested(reconciler *ActiveMQArtemisReconcilerImpl) (total int) {
	for _, v := range reconciler.requestedResources {
		total += len(v)
	}
	return total
}

func countOfDeployed(reconciler *ActiveMQArtemisReconcilerImpl) (total int) {
	for _, v := range reconciler.deployed {
		total += len(v)
	}
	return total
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CompareMetaAndSpec(deployed, requested rtclient.Object) bool {

	isEqual := equalObjectMeta(deployed, requested) &&
		equality.Semantic.DeepEqual(specOf(deployed), specOf(requested)) &&
		reconciler.ensureOwnerReferenceAPIVersion(reconciler.customResource, deployed, requested)
	if !isEqual {
		reconciler.log.V(2).Info("unequal", "deployed", &deployed, "requested", &requested)
	}
	return isEqual
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CompareSecret(deployed, requested rtclient.Object) bool {

	isEqual := equalObjectMeta(deployed, requested) &&
		reconciler.ensureOwnerReferenceAPIVersion(reconciler.customResource, deployed, requested)
	if isEqual {
		deployedSecret := deployed.(*corev1.Secret)
		requestedSecret := requested.(*corev1.Secret)
		// TODO - remove all use of SecretData, just use Data and we can do away with this merge
		deployedSecret = mergeSecretStringDataToData(deployedSecret)
		requestedSecret = mergeSecretStringDataToData(requestedSecret)
		var pairs [][2]interface{}
		pairs = append(pairs, [2]interface{}{deployedSecret.Data, requestedSecret.Data})
		isEqual = compare.EqualPairs(pairs)
	}

	if !isEqual {
		reconciler.log.V(2).Info("unequal secret", "deployed", deployed, "requested", requested)
	}
	return isEqual
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CompareConfigMap(deployed, requested rtclient.Object) bool {
	// our single configMap is immutable, the name indicates a change
	return deployed.GetName() == requested.GetName() &&
		reconciler.ensureOwnerReferenceAPIVersion(reconciler.customResource, deployed, requested)
}

func mergeSecretStringDataToData(secret *corev1.Secret) *corev1.Secret {
	s := secret.DeepCopy()
	// StringData overwrites Data
	if len(s.StringData) > 0 {
		if s.Data == nil {
			s.Data = map[string][]byte{}
		}
		for k, v := range s.StringData {
			s.Data[k] = []byte(v)
		}
	}
	return s
}

func specOf(somethingWithSpec rtclient.Object) interface{} {
	v := reflect.ValueOf(somethingWithSpec)
	specField := reflect.Indirect(v).FieldByName("Spec")
	return specField.Interface()
}

// resourceTemplate means we can modify labels and annotatins so we need to
// respect those in our comparison logic
func equalObjectMeta(deployed, requested rtclient.Object) (isEqual bool) {
	var pairs [][2]interface{}
	pairs = append(pairs, [2]interface{}{deployed.GetLabels(), requested.GetLabels()})
	pairs = append(pairs, [2]interface{}{deployed.GetAnnotations(), requested.GetAnnotations()})
	isEqual = compare.EqualPairs(pairs)
	return isEqual
}

func trackError(compositeError *[]error, err error) {
	if err != nil {
		if *compositeError == nil {
			*compositeError = make([]error, 0, 1)
		}
		*compositeError = append(*compositeError, err)
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) createResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Adding delta resources, i.e. creating ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.createRequestedResource(customResource, client, scheme, requested, kind)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) updateResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Updating delta resources, i.e. updating ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.updateRequestedResource(client, requested, kind)

}

func (reconciler *ActiveMQArtemisReconcilerImpl) deleteResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Deleting delta resources, i.e. removing ", "name ", requested.GetName(), "of kind ", kind)
	return reconciler.deleteRequestedResource(client, requested, kind)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) createRequestedResource(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme, requested rtclient.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Creating ", "kind ", kind, "named ", requested.GetName())
	return resources.Create(customResource, client, scheme, requested)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) updateRequestedResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {
	var updateError error
	if updateError = resources.Update(client, requested); updateError == nil {
		reconciler.log.V(1).Info("updated", "kind ", kind, "named ", requested.GetName())
	} else {
		reconciler.log.V(0).Info("updated Failed", "kind ", kind, "named ", requested.GetName(), "error ", updateError)
	}
	return updateError
}

func (reconciler *ActiveMQArtemisReconcilerImpl) deleteRequestedResource(client rtclient.Client, requested rtclient.Object, kind reflect.Type) error {

	var deleteError error
	if deleteError := resources.Delete(client, requested); deleteError == nil {
		reconciler.log.V(2).Info("deleted", "kind", kind, " named ", requested.GetName())
	} else {
		reconciler.log.Error(deleteError, "delete Failed", "kind", kind, " named ", requested.GetName())
	}
	return deleteError
}

// older version of the operator would drop the owner reference, we need to adopt such secrets and update them
func (reconciler *ActiveMQArtemisReconcilerImpl) adoptExistingSecretWithNoOwnerRefForUpdate(cr *brokerv1beta1.ActiveMQArtemis, candidate *corev1.Secret, client rtclient.Client) {

	key := types.NamespacedName{Name: candidate.Name, Namespace: candidate.Namespace}
	existingSecret := &corev1.Secret{}
	err := client.Get(context.TODO(), key, existingSecret)
	if err == nil {
		if len(existingSecret.OwnerReferences) == 0 {
			reconciler.updateOwnerReferencesAndMatchVersion(cr, existingSecret, candidate)
			reconciler.addToDeployed(reflect.TypeOf(corev1.Secret{}), existingSecret)
			reconciler.log.V(1).Info("found matching secret without owner reference, reclaiming", "Name", key)
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) updateOwnerReferencesAndMatchVersion(cr *brokerv1beta1.ActiveMQArtemis, existing rtclient.Object, candidate rtclient.Object) {

	resources.SetOwnerAndController(cr, existing)
	candidate.SetOwnerReferences(existing.GetOwnerReferences())
	// we need to have 'candidate' match for update
	candidate.SetResourceVersion(existing.GetResourceVersion())
	candidate.SetUID(existing.GetUID())
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ensureOwnerReferenceAPIVersion(cr *brokerv1beta1.ActiveMQArtemis, existing rtclient.Object, candidate rtclient.Object) bool {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) checkExistingService(cr *brokerv1beta1.ActiveMQArtemis, candidate *corev1.Service, client rtclient.Client) {
	serviceType := reflect.TypeOf(corev1.Service{})
	obj := reconciler.getFromDeployed(serviceType, candidate.Name)
	if obj != nil {
		// happy path we already own this
		return
	}
	// check for existing match and track
	key := types.NamespacedName{Name: candidate.Name, Namespace: candidate.Namespace}
	existingService := &corev1.Service{}
	err := client.Get(context.TODO(), key, existingService)
	if err == nil {
		if len(existingService.OwnerReferences) == 0 {
			reconciler.updateOwnerReferencesAndMatchVersion(cr, existingService, candidate)
			reconciler.addToDeployed(serviceType, existingService)
			reconciler.log.V(2).Info("found matching service without owner reference, reclaiming", "Name", key)
		} else {
			reconciler.log.V(2).Info("found matching service with unexpected owner reference, it may need manual removal", "Name", key)
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) checkExistingPersistentVolumes(instance *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) {
	var i int32
	for i = 0; i < common.GetDeploymentSize(instance); i++ {
		ordinalString := strconv.Itoa(int(i))
		pvcKey := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name + "-" + namer.CrToSS(instance.Name) + "-" + ordinalString}
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
		} else {
			if !k8serrors.IsNotFound(err) {
				reconciler.log.Error(err, "got error in getting pvc")
			}
		}
	}
}

var orderedTypes *([]reflect.Type)

func getOrderedTypeList() []reflect.Type {
	if orderedTypes == nil {
		types := make([]reflect.Type, 7)

		// we want to create/update in this order
		types[0] = reflect.TypeOf(corev1.Secret{})
		types[1] = reflect.TypeOf(corev1.ConfigMap{})
		types[2] = reflect.TypeOf(appsv1.StatefulSet{})
		types[3] = reflect.TypeOf(corev1.Service{})
		types[4] = reflect.TypeOf(netv1.Ingress{})
		types[5] = reflect.TypeOf(routev1.Route{})
		types[6] = reflect.TypeOf(policyv1.PodDisruptionBudget{})
		orderedTypes = &types
	}
	return *orderedTypes
}

func addNewVolumes(existingNames map[string]string, existing *[]corev1.Volume, newVolumeName *string) {
	if _, ok := existingNames[*newVolumeName]; !ok {
		volume := volumes.MakeVolume(*newVolumeName)
		*existing = append(*existing, volume)
		existingNames[*newVolumeName] = *newVolumeName
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) MakeVolumes(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers) ([]corev1.Volume, error) {

	volumeDefinitions := []corev1.Volume{}
	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		basicCRVolume := volumes.MakePersistentVolume(customResource.Name)
		volumeDefinitions = append(volumeDefinitions, basicCRVolume...)
	} else if common.IsRestricted(customResource) {
		emptyDirData := volumes.MakeEmptyDirVolumeFor(customResource.Name)
		volumeDefinitions = append(volumeDefinitions, emptyDirData)
	}

	volumeDefinitions = append(volumeDefinitions, customResource.Spec.DeploymentPlan.ExtraVolumes...)

	for _, epvc := range customResource.Spec.DeploymentPlan.ExtraVolumeClaimTemplates {
		epvcVolume := volumes.MakePersistentVolume(epvc.Name)
		volumeDefinitions = append(volumeDefinitions, epvcVolume...)
	}

	secretVolumes := make(map[string]string)
	// Scan acceptors for any with sslEnabled
	for _, acceptor := range customResource.Spec.Acceptors {
		if !acceptor.SSLEnabled {
			continue
		}
		secretName := customResource.Name + "-" + acceptor.Name + "-secret"

		if acceptor.SSLSecret != "" {
			secretName = acceptor.SSLSecret
		}
		addNewVolumes(secretVolumes, &volumeDefinitions, &secretName)

		if acceptor.TrustSecret != nil {
			addNewVolumes(secretVolumes, &volumeDefinitions, acceptor.TrustSecret)
		}
	}

	// Scan connectors for any with sslEnabled
	for _, connector := range customResource.Spec.Connectors {
		if !connector.SSLEnabled {
			continue
		}
		secretName := customResource.Name + "-" + connector.Name + "-secret"
		if connector.SSLSecret != "" {
			secretName = connector.SSLSecret
		}
		addNewVolumes(secretVolumes, &volumeDefinitions, &secretName)
		if connector.TrustSecret != nil {
			addNewVolumes(secretVolumes, &volumeDefinitions, connector.TrustSecret)
		}
	}

	if !common.IsRestricted(customResource) && customResource.Spec.Console.SSLEnabled {
		reconciler.log.V(1).Info("Make volumes for ssl console exposure on k8s")
		secretName := namer.SecretsConsoleNameBuilder.Name()
		addNewVolumes(secretVolumes, &volumeDefinitions, &secretName)
		if customResource.Spec.Console.TrustSecret != nil {
			addNewVolumes(secretVolumes, &volumeDefinitions, customResource.Spec.Console.TrustSecret)
		}
	}

	return volumeDefinitions, nil
}

func addNewVolumeMounts(existingNames map[string]string, existing *[]corev1.VolumeMount, newVolumeMountName *string) {
	if _, ok := existingNames[*newVolumeMountName]; !ok {
		volumeMount := volumes.MakeVolumeMount(*newVolumeMountName)
		*existing = append(*existing, volumeMount)
		existingNames[*newVolumeMountName] = *newVolumeMountName
	}
}

// MakeExtraVolumeMounts creates volume mounts for ExtraVolumes and ExtraVolumeClaimTemplates
// This is used by both the main container and init container
func MakeExtraVolumeMounts(customResource *brokerv1beta1.ActiveMQArtemis) []corev1.VolumeMount {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) MakeVolumeMounts(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers) ([]corev1.VolumeMount, error) {

	volumeMounts := []corev1.VolumeMount{}
	if customResource.Spec.DeploymentPlan.PersistenceEnabled || common.IsRestricted(customResource) {
		persistentCRVlMnt := volumes.MakePersistentVolumeMount(customResource.Name, getDataMountPath(customResource, namer))
		volumeMounts = append(volumeMounts, persistentCRVlMnt...)
	}

	// Add extra volumes and extra volume claim templates
	extraVolumeMounts := MakeExtraVolumeMounts(customResource)
	volumeMounts = append(volumeMounts, extraVolumeMounts...)

	// Scan acceptors for any with sslEnabled
	secretVolumeMounts := make(map[string]string)
	for _, acceptor := range customResource.Spec.Acceptors {
		if !acceptor.SSLEnabled {
			continue
		}
		volumeMountName := customResource.Name + "-" + acceptor.Name + "-secret-volume"
		if acceptor.SSLSecret != "" {
			volumeMountName = acceptor.SSLSecret + "-volume"
		}
		addNewVolumeMounts(secretVolumeMounts, &volumeMounts, &volumeMountName)
		if acceptor.TrustSecret != nil {
			volMountName := *acceptor.TrustSecret + "-volume"
			addNewVolumeMounts(secretVolumeMounts, &volumeMounts, &volMountName)
		}
	}

	// Scan connectors for any with sslEnabled
	for _, connector := range customResource.Spec.Connectors {
		if !connector.SSLEnabled {
			continue
		}
		volumeMountName := customResource.Name + "-" + connector.Name + "-secret-volume"
		if connector.SSLSecret != "" {
			volumeMountName = connector.SSLSecret + "-volume"
		}
		addNewVolumeMounts(secretVolumeMounts, &volumeMounts, &volumeMountName)
		if connector.TrustSecret != nil {
			volMountName := *connector.TrustSecret + "-volume"
			addNewVolumeMounts(secretVolumeMounts, &volumeMounts, &volMountName)
		}
	}

	if customResource.Spec.Console.SSLEnabled {
		reconciler.log.V(1).Info("Make volume mounts for ssl console exposure on k8s")
		volumeMountName := namer.SecretsConsoleNameBuilder.Name() + "-volume"
		addNewVolumeMounts(secretVolumeMounts, &volumeMounts, &volumeMountName)
		if customResource.Spec.Console.TrustSecret != nil {
			volMountName := *customResource.Spec.Console.TrustSecret + "-volume"
			addNewVolumeMounts(secretVolumeMounts, &volumeMounts, &volMountName)
		}
	}

	return volumeMounts, nil
}

func getDataMountPath(cr *brokerv1beta1.ActiveMQArtemis, namer common.Namers) string {
	if common.IsRestricted(cr) {
		return "/app"
	}
	return namer.GLOBAL_DATA_PATH
}
func MakeContainerPorts(cr *brokerv1beta1.ActiveMQArtemis) []corev1.ContainerPort {

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

func (reconciler *ActiveMQArtemisReconcilerImpl) PodTemplateSpecForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, currentStatefulSet *appsv1.StatefulSet, client rtclient.Client) (*corev1.PodTemplateSpec, error) {

	reqLogger := reconciler.log.WithName(customResource.Name)

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}

	current := &currentStatefulSet.Spec.Template

	terminationGracePeriodSeconds := int64(60)
	respectExistingJavaOpts := isExistingDeploymentWithJavaOpts(current) // check before we mutate

	// custom labels provided in CR applied only to the pod template spec
	// note: work with a clone of the default labels to not modify defaults
	labels := make(map[string]string)
	for key, value := range namer.LabelBuilder.Labels() {
		labels[key] = value
	}
	if customResource.Spec.DeploymentPlan.Labels != nil {
		for key, value := range customResource.Spec.DeploymentPlan.Labels {
			labels[key] = value
		}
	}

	pts := pods.MakePodTemplateSpec(current, namespacedName, labels, customResource.Spec.DeploymentPlan.Annotations)
	podSpec := &pts.Spec

	podSpec.ImagePullSecrets = customResource.Spec.DeploymentPlan.ImagePullSecrets

	container := containers.MakeContainer(podSpec, customResource.Name, common.ResolveImage(customResource, common.BrokerImageKey), MakeEnvVarArrayForCR(customResource, namer))

	container.Resources = customResource.Spec.DeploymentPlan.Resources

	reconciler.configureContianerSecurityContext(container, customResource.Spec.DeploymentPlan.ContainerSecurityContext)

	container.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError

	containerPorts := MakeContainerPorts(customResource)
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

	additionalSystemPropsForRestricted := []string{}
	if common.IsRestricted(customResource) {

		mountPathRoot := secretPathBase + getPropertiesResourceNsName(customResource).Name
		security_properties := newPropsWithHeader()
		fmt.Fprintf(security_properties, "login.config.url.1=file:%s/login.config\n", mountPathRoot)
		fmt.Fprintf(security_properties, "security.provider.13=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")
		fmt.Fprintf(security_properties, "fips.provider.8=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")

		brokerPropertiesMapData["_security.config"] = security_properties.Bytes()

		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, fmt.Sprintf("-Djava.security.properties=%s/_security.config", mountPathRoot))

		login_config := newBufferWithHeader("//")
		fmt.Fprintln(login_config, "http_server_authenticator {")
		fmt.Fprintln(login_config, "  org.apache.activemq.artemis.spi.core.security.jaas.TextFileCertificateLoginModule required")
		fmt.Fprintln(login_config, "   reload=true")
		fmt.Fprintln(login_config, "   debug=false")
		fmt.Fprintln(login_config, "   org.apache.activemq.jaas.textfiledn.user=_cert-users")
		fmt.Fprintln(login_config, "   org.apache.activemq.jaas.textfiledn.role=_cert-roles")
		fmt.Fprintf(login_config, "   baseDir=\"%v\"\n", mountPathRoot)
		fmt.Fprintln(login_config, "  ;")
		fmt.Fprintln(login_config, "};")
		brokerPropertiesMapData["login.config"] = login_config.Bytes()

		operandCertSecretName := common.GetOperandCertSecretName(customResource, client)
		operandCertSecret, err := common.GetNamespacedSecret(client, operandCertSecretName, customResource.Namespace)
		if err != nil {
			return nil, err
		}

		operandCertSubject, err := common.ExtractCertSubjectFromSecret(operandCertSecret)
		if err != nil {
			return nil, err
		}

		var caCertSecret *corev1.Secret
		if caCertSecret, err = common.GetOperatorCASecret(client); err != nil {
			return nil, err
		}

		caSecretKey, err := common.GetOperatorCASecretKey(client, caCertSecret)
		if err != nil {
			return nil, err
		}

		var operatorCert *tls.Certificate
		if operatorCert, err = common.GetOperatorClientCertificate(client, nil); err != nil {
			return nil, err
		}

		var operatorCertSubject *pkix.Name
		if operatorCertSubject, err = common.ExtractCertSubject(operatorCert); err != nil {
			return nil, err
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

		cert_user := newPropsWithHeader()
		fmt.Fprintln(cert_user, "hawtio=/CN = hawtio-online\\.hawtio\\.svc.*/")
		fmt.Fprintf(cert_user, "operator=/.*%s.*/\n", operatorCertSubject.CommonName) // regexp syntax start and with /
		// can and should use the full DN after https://issues.apache.org/jira/browse/ARTEMIS-5102
		fmt.Fprintf(cert_user, "probe=/.*%s.*/\n", operandCertSubject.CommonName)
		if prometheusCertSubject != nil {
			fmt.Fprintf(cert_user, "prometheus=/.*%s.*/\n", prometheusCertSubject.CommonName)
		}
		brokerPropertiesMapData["_cert-users"] = cert_user.Bytes()

		cert_roles := newPropsWithHeader()
		fmt.Fprintln(cert_roles, "status=operator,probe")
		fmt.Fprintln(cert_roles, "metrics=operator,prometheus")
		fmt.Fprintln(cert_roles, "hawtio=hawtio")
		brokerPropertiesMapData["_cert-roles"] = cert_roles.Bytes()

		foundationalProps := newPropsWithHeader()
		fmt.Fprintf(foundationalProps, "name=%s\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintln(foundationalProps, "criticalAnalyzer=false")
		fmt.Fprintln(foundationalProps, "messageCounterEnabled=true")
		fmt.Fprintln(foundationalProps, "journalDirectory=/app/data")
		fmt.Fprintln(foundationalProps, "bindingsDirectory=/app/data/bindings")
		fmt.Fprintln(foundationalProps, "largeMessagesDirectory=/app/data/largemessages")
		fmt.Fprintln(foundationalProps, "pagingDirectory=/app/data/paging")

		brokerPropertiesMapData["aa_restricted.properties"] = foundationalProps.Bytes()

		rbac := newPropsWithHeader()
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

		jolokia_config := newPropsWithHeader()
		fmt.Fprintln(jolokia_config, "protocol=https")
		fmt.Fprintln(jolokia_config, "authClass=org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator")
		fmt.Fprintf(jolokia_config, "caCert=%s%s/%s\n", secretPathBase, caSecret, caSecretKey)
		fmt.Fprintf(jolokia_config, "serverCert=%s%s/tls.crt\n", secretPathBase, operandCertSecretName)
		fmt.Fprintf(jolokia_config, "serverKey=%s%s/tls.key\n", secretPathBase, operandCertSecretName)
		fmt.Fprintln(jolokia_config, "port=8778")
		// https://github.com/jolokia/jolokia/issues/751 at some point host=$(env:HOSTNAME), host= is on the command line below
		fmt.Fprintln(jolokia_config, "useSslClientAuthentication=true")
		fmt.Fprintln(jolokia_config, "disabledServices=org.jolokia.service.history.HistoryMBeanRequestInterceptor")
		fmt.Fprintln(jolokia_config, "disableDetectors=true")
		fmt.Fprintln(jolokia_config, "debug=false")

		brokerPropertiesMapData["_jolokia.config"] = jolokia_config.Bytes()

		pem_cfg := newPropsWithHeader()

		fmt.Fprintf(pem_cfg, "alias=alias\n")
		fmt.Fprintf(pem_cfg, "source.cert=%s%s/tls.crt\n", secretPathBase, operandCertSecretName)
		fmt.Fprintf(pem_cfg, "source.key=%s%s/tls.key\n", secretPathBase, operandCertSecretName)
		brokerPropertiesMapData["_cert.pemcfg"] = pem_cfg.Bytes()

		prometheus_config := newPropsWithHeader() // yaml
		fmt.Fprintf(prometheus_config, "httpServer:\n")
		fmt.Fprintf(prometheus_config, "  authentication:\n")
		fmt.Fprintf(prometheus_config, "    plugin:\n")
		fmt.Fprintf(prometheus_config, "      class: org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator\n")
		fmt.Fprintf(prometheus_config, "      subjectAttributeName: org.jolokia.jaasSubject\n") // match -DhttpServerAuthenticator.requestSubjectAttribute
		fmt.Fprintf(prometheus_config, "  ssl:\n")
		fmt.Fprintf(prometheus_config, "    mutualTLS: true\n")
		fmt.Fprintf(prometheus_config, "    keyStore:\n")
		fmt.Fprintf(prometheus_config, "      filename: %s/_cert.pemcfg\n", mountPathRoot)
		fmt.Fprintf(prometheus_config, "      type: PEMCFG\n")
		fmt.Fprintf(prometheus_config, "    trustStore:\n")
		fmt.Fprintf(prometheus_config, "      filename: %s%s/%s\n", secretPathBase, caSecret, caSecretKey)
		fmt.Fprintf(prometheus_config, "      type: PEMCA\n")
		fmt.Fprintf(prometheus_config, "    certificate:\n")
		fmt.Fprintf(prometheus_config, "      alias: alias\n")
		// the collector/scraper config
		fmt.Fprintf(prometheus_config, "lowercaseOutputName: true\n")
		fmt.Fprintf(prometheus_config, "lowercaseOutputLabelNames: true\n")
		fmt.Fprintf(prometheus_config, "includeObjectNames: [org.apache.activemq.artemis:broker=\"%s\"]\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheus_config, "includeObjectNameAttributes:\n")
		fmt.Fprintf(prometheus_config, "  'org.apache.activemq.artemis:broker=\"%s\"':\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheus_config, "    - \"TotalMessageCount\"\n")
		fmt.Fprintf(prometheus_config, "    - \"TotalMessagesAdded\"\n")
		fmt.Fprintf(prometheus_config, "    - \"TotalMessagesAcknowledged\"\n")
		fmt.Fprintf(prometheus_config, "rules:\n")
		fmt.Fprintf(prometheus_config, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessageCount'\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheus_config, "    help: Number of pending messages\n")
		fmt.Fprintf(prometheus_config, "    name: artemis_total_pending_message_count\n")
		fmt.Fprintf(prometheus_config, "    type: GAUGE\n")
		fmt.Fprintf(prometheus_config, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessagesAcknowledged'\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheus_config, "    help: Number of messages consumed since start\n")
		fmt.Fprintf(prometheus_config, "    name: artemis_total_consumed_message_count\n")
		fmt.Fprintf(prometheus_config, "    type: COUNTER\n")
		fmt.Fprintf(prometheus_config, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessagesAdded'\n", environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name))
		fmt.Fprintf(prometheus_config, "    help: Number of messages produced since start\n")
		fmt.Fprintf(prometheus_config, "    name: artemis_total_produced_message_count\n")
		fmt.Fprintf(prometheus_config, "    type: COUNTER\n")

		brokerPropertiesMapData["_prometheus_exporter.yaml"] = prometheus_config.Bytes()

		// Apply control plane overrides if they exist
		if err := applyControlPlaneOverrides(customResource, client, brokerPropertiesMapData); err != nil {
			return nil, err
		}

		// adapt jolokia and prometheus authentication
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, "-DhttpServerAuthenticator.requestSubjectAttribute=org.jolokia.jaasSubject")

		// install mbean server guard
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, "-Dlog4j2.disableJmx=true -Djavax.management.builder.initial=org.apache.activemq.artemis.core.server.management.ArtemisRbacMBeanServerBuilder")

		// install jolokia agent
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, fmt.Sprintf("-javaagent:/opt/agents/jolokia.jar=host=$HOSTNAME,config=%s/_jolokia.config", mountPathRoot))

		// install prometheus agent
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, fmt.Sprintf("-javaagent:/opt/agents/prometheus.jar=$HOSTNAME:8888:%s/_prometheus_exporter.yaml", mountPathRoot))

		// non boot jar isolation classpath
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, "-classpath /opt/amq/lib/*:/opt/amq/lib/extra/*")

		// temp volume
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, "-Djava.io.tmpdir=/app/tmp")

		// jvm options
		additionalSystemPropsForRestricted = append(additionalSystemPropsForRestricted, "-XX:InitialRAMPercentage=70.0 -XX:MaxRAMPercentage=70.0 -XX:AutoBoxCacheMax=20000 -XX:+PrintClassHistogram -XX:+UseG1GC -XX:+UseStringDeduplication -Djava.net.preferIPv4Stack=true")

		if customResource.Spec.DeploymentPlan.LivenessProbe == nil {
			container.LivenessProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/bin/bash",
							"-c",
							// use curl with mtls as the broker-cert to pull the status to find start state using dns
							fmt.Sprintf(`export STATEFUL_SET_ORDINAL=${HOSTNAME##*-};curl --cacert %s%s/%s --cert %s%s/tls.crt --key %s%s/tls.key  https://%s:8778/jolokia/read/org.apache.activemq.artemis:broker=%%22%s%%22/Status | grep -w -P "(START|STOPP)(ED|ING)"`, secretPathBase, caSecret, caSecretKey, secretPathBase, operandCertSecretName, secretPathBase, operandCertSecretName, common.OrdinalStringFQDNS(customResource.Name, customResource.Namespace, "$STATEFUL_SET_ORDINAL"), environments.ResolveBrokerNameFromEnvs(customResource.Spec.Env, customResource.Name)),
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
		return nil, err
	}

	reqLogger.V(2).Info("Extra volumes", "volumes", extraVolumes)
	reqLogger.V(2).Info("Extra mounts", "mounts", extraVolumeMounts)

	container.VolumeMounts, err = reconciler.MakeVolumeMounts(customResource, namer)
	if err != nil {
		return nil, err
	}
	if len(extraVolumeMounts) > 0 {
		container.VolumeMounts = append(container.VolumeMounts, extraVolumeMounts...)
	}

	container.StartupProbe = reconciler.configureStartupProbe(container, customResource.Spec.DeploymentPlan.StartupProbe)
	if !common.IsRestricted(customResource) {
		container.LivenessProbe = reconciler.configureLivenessProbe(container, customResource.Spec.DeploymentPlan.LivenessProbe)
	}
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
		return nil, err
	}
	if len(extraVolumes) > 0 {
		brokerVolumes = append(brokerVolumes, extraVolumes...)
	}
	if len(brokerVolumes) > 0 {
		podSpec.Volumes = brokerVolumes
	}
	podSpec.TerminationGracePeriodSeconds = &terminationGracePeriodSeconds

	var cfgVolumeName string = "amq-cfg-dir"

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
	if jaasConfigPath, found := getJaasConfigExtraMountPath(customResource); found {
		debugArgs := corev1.EnvVar{
			Name:  getJaasConfigEnvVarName(customResource),
			Value: fmt.Sprintf("-Djava.security.auth.login.config=%v", jaasConfigPath),
		}
		environments.CreateOrAppend(podSpec.Containers, &debugArgs)
	}

	if loggingConfigPath, found := getLoggingConfigExtraMountPath(customResource); found {
		loggerOpts := corev1.EnvVar{
			Name:  getLoginConfigEnvVarName(customResource),
			Value: fmt.Sprintf("-Dlog4j2.configurationFile=%v", loggingConfigPath),
		}
		environments.CreateOrAppend(podSpec.Containers, &loggerOpts)
	} else if common.IsRestricted(customResource) {
		// modify log4j2 default of ERROR
		loggerOpts := corev1.EnvVar{
			Name:  getLoginConfigEnvVarName(customResource),
			Value: "-Dlog4j2.level=INFO",
		}
		environments.CreateOrAppend(podSpec.Containers, &loggerOpts)
	}

	// add TopologySpreadConstraints config
	podSpec.TopologySpreadConstraints = customResource.Spec.DeploymentPlan.TopologySpreadConstraints

	if !common.IsRestricted(customResource) {
		//add empty-dir volume and volumeMounts to main container
		volumeForCfg := volumes.MakeEmptyDirVolumeFor(cfgVolumeName)
		podSpec.Volumes = append(podSpec.Volumes, volumeForCfg)

		volumeMountForCfg := volumes.MakeRwVolumeMountForCfg(cfgVolumeName, brokerConfigRoot)
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, volumeMountForCfg)

	}
	reqLogger.V(2).Info("Creating init container for broker configuration")
	initContainer := containers.MakeInitContainer(podSpec, customResource.Name, common.ResolveImage(customResource, common.InitImageKey), MakeEnvVarArrayForCR(customResource, namer))
	initContainer.Resources = customResource.Spec.DeploymentPlan.Resources

	reconciler.configureContianerSecurityContext(initContainer, customResource.Spec.DeploymentPlan.ContainerSecurityContext)

	initContainer.TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError

	var initCmds []string
	var initCfgRootDir = "/init_cfg_root"

	compactVersionToUse, verr := common.DetermineCompactVersionToUse(customResource)
	if verr != nil {
		reqLogger.Error(verr, "failed to get compact version", "Spec.Version", customResource.Spec.Version)
		return nil, verr
	}
	yacfgProfileVersion = version.YacfgProfileVersionFromFullVersion[version.FullVersionFromCompactVersion[compactVersionToUse]]
	yacfgProfileName := version.YacfgProfileName

	//address settings
	addressSettings := customResource.Spec.AddressSettings.AddressSetting
	if len(addressSettings) > 0 {
		reqLogger.V(1).Info("processing address-settings")

		var configYaml strings.Builder
		var configSpecials map[string]string = make(map[string]string)

		brokerYaml, specials := cr2jinja2.MakeBrokerCfgOverrides(customResource, nil, nil)

		configYaml.WriteString(brokerYaml)

		for k, v := range specials {
			configSpecials[k] = v
		}

		byteArray, err := json.Marshal(configSpecials)
		if err != nil {
			reqLogger.Error(err, "failed to marshal specials")
		}
		jsonSpecials := string(byteArray)

		envVarTuneFilePath := "TUNE_PATH"
		outputDir := initCfgRootDir + "/yacfg_etc"

		initCmd := "mkdir -p " + outputDir + "; echo \"" + configYaml.String() + "\" > " + outputDir +
			"/broker.yaml; cat " + outputDir + "/broker.yaml; yacfg --profile " + yacfgProfileName + "/" +
			yacfgProfileVersion + "/default_with_user_address_settings.yaml.jinja2  --tune " +
			outputDir + "/broker.yaml --extra-properties '" + jsonSpecials + "' --output " + outputDir

		reqLogger.V(2).Info("initCmd: " + initCmd)
		initCmds = append(initCmds, initCmd)

		//populate args of init container

		podSpec.InitContainers = []corev1.Container{
			*initContainer,
		}

		//expose env for address-settings
		envVarApplyRule := "APPLY_RULE"
		envVarApplyRuleValue := customResource.Spec.AddressSettings.ApplyRule

		if envVarApplyRuleValue == nil {
			envVarApplyRuleValue = &defApplyRule
		}
		reqLogger.V(1).Info("Process addresssetting", "ApplyRule", *envVarApplyRuleValue)

		applyRule := corev1.EnvVar{
			Name:  envVarApplyRule,
			Value: *envVarApplyRuleValue,
		}
		environments.Create(podSpec.InitContainers, &applyRule)

		mergeBrokerAs := corev1.EnvVar{
			Name:  "MERGE_BROKER_AS",
			Value: "true",
		}
		environments.Create(podSpec.InitContainers, &mergeBrokerAs)

		//pass cfg file location and apply rule to init container via env vars
		tuneFile := corev1.EnvVar{
			Name:  envVarTuneFilePath,
			Value: outputDir,
		}
		environments.Create(podSpec.InitContainers, &tuneFile)

	} else {
		podSpec.InitContainers = []corev1.Container{
			*initContainer,
		}
	}
	//now make volumes mount available to init image

	//setup volumeMounts from scratch
	podSpec.InitContainers[0].VolumeMounts = []corev1.VolumeMount{}
	volumeMountForCfgRoot := volumes.MakeRwVolumeMountForCfg(cfgVolumeName, brokerConfigRoot)
	podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, volumeMountForCfgRoot)

	volumeMountForCfg := volumes.MakeRwVolumeMountForCfg("tool-dir", initCfgRootDir)
	podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, volumeMountForCfg)

	//add empty-dir volume
	volumeForTool := volumes.MakeEmptyDirVolumeFor("tool-dir")
	podSpec.Volumes = append(podSpec.Volumes, volumeForTool)

	reqLogger.V(2).Info("Total volumes ", "volumes", podSpec.Volumes)

	var mountPoint = secretPathBase
	if !isSecret {
		mountPoint = cfgMapPathBase
	}
	brokerPropsValue := reconciler.brokerPropertiesConfigSystemPropValue(mountPoint, brokerPropertiesResourceName, brokerPropertiesMapData)

	// only use init container JAVA_OPTS on existing deployments and migrate to JDK_JAVA_OPTIONS for independence
	// from init containers and broker run scripts
	if respectExistingJavaOpts {
		// this depends on init container passing --java-opts to artemis create via launch.sh *and* it
		// not getting munged on the way. We CreateOrAppend to any value from spec.Env
		javaOpts := corev1.EnvVar{
			Name:  javaOptsEnvVarName,
			Value: brokerPropsValue,
		}
		environments.CreateOrAppend(podSpec.InitContainers, &javaOpts)
	} else {
		jdkJavaOpts := corev1.EnvVar{
			Name:  jdkJavaOptionsEnvVarName,
			Value: brokerPropsValue,
		}
		environments.CreateOrAppend(podSpec.Containers, &jdkJavaOpts)
	}

	var initArgs []string = []string{"-c"}

	//provide a way to configuration after launch.sh
	var brokerHandlerCmds []string = []string{}
	brokerConfigHandler := GetBrokerConfigHandler(namespacedName)
	if brokerConfigHandler != nil {
		reqLogger.V(1).Info("there is a config handler")
		handlerCmds := brokerConfigHandler.Config(podSpec.InitContainers, initCfgRootDir+"/security", yacfgProfileVersion, yacfgProfileName)
		reqLogger.V(2).Info("Getting back some init commands", "handlerCmds", handlerCmds)
		if len(handlerCmds) > 0 {
			reqLogger.V(2).Info("appending to initCmd array...")
			brokerHandlerCmds = append(brokerHandlerCmds, handlerCmds...)

			securitySecretVolumeName := "secret-security-" + brokerConfigHandler.GetCRName()
			securitySecretVolume := volumes.MakeVolume(securitySecretVolumeName)
			podSpec.Volumes = append(podSpec.Volumes, securitySecretVolume)

			securitySecretVoluneMountName := securitySecretVolumeName + "-volume"
			securitySecretVoluneMount := volumes.MakeVolumeMount(securitySecretVoluneMountName)
			podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, securitySecretVoluneMount)
		}
	}

	var strBuilder strings.Builder

	isFirst := true
	initCmds = append(initCmds, configCmd)
	initCmds = append(initCmds, brokerHandlerCmds...)
	initCmds = append(initCmds, initHelperScript)

	for _, icmd := range initCmds {
		if isFirst {
			isFirst = false
		} else {
			strBuilder.WriteString(" && ")
		}
		strBuilder.WriteString(icmd)
	}
	initArgs = append(initArgs, strBuilder.String())

	reqLogger.V(2).Info("The final init cmds to init ", "the cmd array", initArgs)

	podSpec.InitContainers[0].Args = initArgs

	if len(extraVolumeMounts) > 0 {
		podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, extraVolumeMounts...)
		reqLogger.V(2).Info("Added some extra mounts to init", "total mounts: ", podSpec.InitContainers[0].VolumeMounts)
	}

	// Add extra volumes and extra volume claim templates to init container
	crExtraVolumeMounts := MakeExtraVolumeMounts(customResource)
	if len(crExtraVolumeMounts) > 0 {
		podSpec.InitContainers[0].VolumeMounts = append(podSpec.InitContainers[0].VolumeMounts, crExtraVolumeMounts...)
		reqLogger.V(2).Info("Added CR extra volume mounts to init", "total mounts: ", podSpec.InitContainers[0].VolumeMounts)
	}

	dontRun := corev1.EnvVar{
		Name:  "RUN_BROKER",
		Value: "false",
	}
	environments.Create(podSpec.InitContainers, &dontRun)

	envBrokerCustomInstanceDir = corev1.EnvVar{
		Name:  "CONFIG_INSTANCE_DIR",
		Value: brokerConfigRoot,
	}
	environments.Create(podSpec.InitContainers, &envBrokerCustomInstanceDir)

	// NOTE: PodSecurity contains a RunAsUser that will be overridden by that in the provided PodSecurityContext if any
	reconciler.configPodSecurity(podSpec, &customResource.Spec.DeploymentPlan.PodSecurity)
	reconciler.configurePodSecurityContext(podSpec, customResource.Spec.DeploymentPlan.PodSecurityContext)

	pts.Spec = *podSpec

	if common.IsRestricted(customResource) {
		pts.Spec.InitContainers = nil

		// restricted env
		currentEnv := environments.Retrieve(pts.Spec.Containers, jdkJavaOptionsEnvVarName)
		pts.Spec.Containers[0].Env = nil

		var commandLineString string = ""
		if currentEnv != nil {
			commandLineString += currentEnv.Value
		}
		for _, v := range additionalSystemPropsForRestricted {
			commandLineString += " " + v
		}

		// env from CR can override
		pts.Spec.Containers[0].Env = append(pts.Spec.Containers[0].Env, customResource.Spec.Env...)

		reEvalJdkOpts := generateReEvalOrdinaEnvReplacement(customResource.Spec.Env)

		pts.Spec.Containers[0].Command = []string{
			"/bin/bash", "-c",
			fmt.Sprintf("export STATEFUL_SET_ORDINAL=${HOSTNAME##*-}; %s exec java %s $JAVA_ARGS_APPEND org.apache.activemq.artemis.core.server.embedded.Main", reEvalJdkOpts, commandLineString),
		}
	}

	reqLogger.V(2).Info("Final Init spec", "Detail", podSpec.InitContainers)

	return pts, nil
}

func isExistingDeploymentWithJavaOpts(current *corev1.PodTemplateSpec) bool {
	return current != nil && environments.Retrieve(current.Spec.InitContainers, javaOptsEnvVarName) != nil
}

// support ${STATEFUL_SET_ORDINAL} replacement in JDK options from CR env if necessary
func generateReEvalOrdinaEnvReplacement(envVars []corev1.EnvVar) (cmd string) {
	cmd = ""
	for _, envVar := range envVars {
		if supportsOrdinalReplacement(envVar) {
			cmd += fmt.Sprintf("export %s=${%s//\\$\\{STATEFUL_SET_ORDINAL\\}/${HOSTNAME##*-}}; ", envVar.Name, envVar.Name)
		}
	}
	return cmd
}

func supportsOrdinalReplacement(envVar corev1.EnvVar) bool {
	return (envVar.Name == jdkJavaOptionsEnvVarName || envVar.Name == javaArgsAppendEnvVarName) && strings.Contains(envVar.Value, "${STATEFUL_SET_ORDINAL}")
}

func getJaasConfigEnvVarName(customResource *brokerv1beta1.ActiveMQArtemis) string {
	if !common.IsRestricted(customResource) {
		// legacy
		return debugArgsEnvVarName
	}

	return jdkJavaOptionsEnvVarName
}

func getLoginConfigEnvVarName(customResource *brokerv1beta1.ActiveMQArtemis) string {
	if !common.IsRestricted(customResource) {
		// legacy
		return javaArgsAppendEnvVarName
	}

	return jdkJavaOptionsEnvVarName
}

func newPropsWithHeader() *bytes.Buffer {
	return newBufferWithHeader("#")
}

func newBufferWithHeader(commentChars string) *bytes.Buffer {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "%s generated by crd\n", commentChars)
	fmt.Fprintf(buf, "%s\n", commentChars)
	return buf
}

func (reconciler *ActiveMQArtemisReconcilerImpl) brokerPropertiesConfigSystemPropValue(mountPoint, resourceName string, brokerPropertiesData map[string][]byte) string {
	var result = ""
	if len(brokerPropertiesData) == 1 && !reconciler.CrConfiguredForControllerManagedScaleDown() {
		// single entry, no ordinal subpath - broker will log if arg is not found for the watcher so make conditional
		result = fmt.Sprintf("-Dbroker.properties=%s%s/%s", mountPoint, resourceName, BrokerPropertiesName)
	} else {
		// directory works on broker image versions >= 2.27.1
		result = fmt.Sprintf("-Dbroker.properties=%s%s/,%s%s/%s${STATEFUL_SET_ORDINAL}/", mountPoint, resourceName, mountPoint, resourceName, OrdinalPrefix)
	}

	for _, extraSecretName := range reconciler.customResource.Spec.DeploymentPlan.ExtraMounts.Secrets {
		if strings.HasSuffix(extraSecretName, brokerPropsSuffix) {
			// append to ordinal path
			result = fmt.Sprintf("%s,%s%s/,%s%s/%s${STATEFUL_SET_ORDINAL}/", result, secretPathBase, extraSecretName, secretPathBase, extraSecretName, OrdinalPrefix)
		}
	}

	if reconciler.CrConfiguredForControllerManagedScaleDown() {
		// this is a better way to do ordinal config because it does not require an additional mount point, needs image > 2.45.0
		result = fmt.Sprintf("%s,%s%s/?filter=.*\\.%s${STATEFUL_SET_ORDINAL}%s", result, mountPoint, resourceName, OrdinalPropertiesSuffix, OrdinalPropertiesSuffixEnd)
	}

	return result
}

func getJaasConfigExtraMountPath(customResource *brokerv1beta1.ActiveMQArtemis) (string, bool) {
	if t, name, found := getConfigExtraMount(customResource, jaasConfigSuffix); found {
		return fmt.Sprintf("/amq/extra/%v/%v/login.config", t, name), true
	}
	return "", false
}

func getLoggingConfigExtraMountPath(customResource *brokerv1beta1.ActiveMQArtemis) (string, bool) {
	if t, name, found := getConfigExtraMount(customResource, loggingConfigSuffix); found {
		return fmt.Sprintf("/amq/extra/%v/%v/logging.properties", t, name), true
	}
	return "", false
}

func getConfigExtraMount(customResource *brokerv1beta1.ActiveMQArtemis, suffix string) (string, string, bool) {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) configureStartupProbe(container *corev1.Container, probeFromCr *corev1.Probe) *corev1.Probe {

	var startupProbe *corev1.Probe = container.StartupProbe
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

func (reconciler *ActiveMQArtemisReconcilerImpl) configureLivenessProbe(container *corev1.Container, probeFromCr *corev1.Probe) *corev1.Probe {
	var livenessProbe *corev1.Probe = container.LivenessProbe
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
	} else if !common.IsRestricted(reconciler.customResource) {

		reconciler.log.V(1).Info("Creating Default Liveness Probe")

		livenessProbe.InitialDelaySeconds = defaultLivenessProbeInitialDelay
		livenessProbe.TimeoutSeconds = 5
		livenessProbe.ProbeHandler = corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(TCPLivenessPort),
			},
		}
	}

	return livenessProbe
}

var command = []string{
	"/bin/bash",
	"-c",
	"/opt/amq/bin/readinessProbe.sh",
}

var betterCommand = []string{
	"/bin/bash",
	"-c",
	"/opt/amq/bin/readinessProbe.sh 1", // retries/count - so we get fast feedback and can configure via the Probe
	// "1", sleep seconds not applicable with 1 retry
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureReadinessProbe(container *corev1.Container, probeFromCr *corev1.Probe) *corev1.Probe {

	var readinessProbe *corev1.Probe = container.ReadinessProbe
	reconciler.log.V(1).Info("Configuring Readyness Probe", "existing", readinessProbe)

	if readinessProbe == nil {
		readinessProbe = &corev1.Probe{}
	}

	if probeFromCr != nil {
		conditionallyApplyValuesToPreserveDefaults(readinessProbe, probeFromCr)
		if probeFromCr.GRPC == nil && probeFromCr.Exec == nil && probeFromCr.HTTPGet == nil && probeFromCr.TCPSocket == nil {
			reconciler.log.V(2).Info("adding default handler to user provided readiness Probe")

			// respect existing command where already deployed
			if readinessProbe.ProbeHandler.Exec != nil && reflect.DeepEqual(readinessProbe.ProbeHandler.Exec.Command, command) {
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
	} else if common.IsRestricted(reconciler.customResource) {
		// liveness probe is sufficient
		readinessProbe = nil
	} else {
		reconciler.log.V(1).Info("creating default readiness Probe")
		readinessProbe.InitialDelaySeconds = defaultLivenessProbeInitialDelay
		readinessProbe.TimeoutSeconds = 5

		// respect existing command where already deployed
		if readinessProbe.ProbeHandler.Exec != nil && reflect.DeepEqual(readinessProbe.ProbeHandler.Exec.Command, command) {
			// leave it be so we don't force a reconcile
		} else {
			// upgrade to betterCommand!
			readinessProbe.ProbeHandler = corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: betterCommand,
				},
			}
		}
	}

	return readinessProbe
}

// when the CR has a full Spec, the intent is that the Spec is fully formed, such that there are not server side defaults in the mix.
// For probes, we historically allow a partial spec, so we need to be careful to not overide server side applied defaults with empty values
func conditionallyApplyValuesToPreserveDefaults(readinessProbe *corev1.Probe, probeFromCr *corev1.Probe) {
	if probeFromCr.InitialDelaySeconds > 0 {
		readinessProbe.InitialDelaySeconds = probeFromCr.InitialDelaySeconds
	}
	if probeFromCr.TimeoutSeconds > 0 {
		readinessProbe.TimeoutSeconds = probeFromCr.TimeoutSeconds
	}
	if probeFromCr.PeriodSeconds > 0 {
		readinessProbe.PeriodSeconds = probeFromCr.PeriodSeconds
	}
	if probeFromCr.TerminationGracePeriodSeconds != nil {
		readinessProbe.TerminationGracePeriodSeconds = probeFromCr.TerminationGracePeriodSeconds
	}
	if probeFromCr.SuccessThreshold > 0 {
		readinessProbe.SuccessThreshold = probeFromCr.SuccessThreshold
	}
	if probeFromCr.FailureThreshold > 0 {
		readinessProbe.FailureThreshold = probeFromCr.FailureThreshold
	}
}

// applyControlPlaneOverrides applies control plane configuration overrides from secrets.
// It first checks for CR-specific override secret ([cr-name]-control-plane-override),
// then falls back to shared override secret (control-plane-override).
// Each key in the override secret completely replaces the corresponding key in brokerPropertiesMapData.
func applyControlPlaneOverrides(customResource *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, brokerPropertiesMapData map[string][]byte) error {
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
	for key, value := range overrideSecret.Data {
		brokerPropertiesMapData[key] = value
	}

	return nil
}

func getPropertiesResourceNsName(artemis *brokerv1beta1.ActiveMQArtemis) types.NamespacedName {
	return types.NamespacedName{
		Namespace: artemis.Namespace,
		Name:      artemis.Name + "-props",
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) addResourceForBrokerProperties(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers) (string, bool, map[string][]byte, error) {

	// fetch and do idempotent transform based on CR

	// deal with upgrade to mutable secret, only upgrade to mutable on not found
	alder32Bytes := alder32Of(customResource.Spec.BrokerProperties)
	shaOfMap := hex.EncodeToString(alder32Bytes)
	resourceName := types.NamespacedName{
		Namespace: customResource.Namespace,
		Name:      customResource.Name + "-props-" + shaOfMap,
	}

	obj := reconciler.cloneOfDeployed(reflect.TypeOf(corev1.ConfigMap{}), resourceName.Name)
	if obj != nil {
		existing := obj.(*corev1.ConfigMap)
		// found existing (immuable) map with sha in the name
		reconciler.log.V(1).Info("Requesting configMap for broker properties", "name", resourceName.Name)
		reconciler.trackDesired(existing)

		return resourceName.Name, false, existing.BinaryData, nil
	}

	var desired *corev1.Secret
	resourceName = getPropertiesResourceNsName(customResource)

	obj = reconciler.cloneOfDeployed(reflect.TypeOf(corev1.Secret{}), resourceName.Name)
	if obj != nil {
		desired = obj.(*corev1.Secret)
	}

	data := BrokerPropertiesData(reconciler.customResource.Spec.BrokerProperties)
	reconciler.ProcessBrokerProperties(data)

	if desired == nil {
		reconciler.log.V(1).Info("desired brokerprop secret nil, create new one", "name", resourceName.Name)
		secret := secrets.MakeSecret(resourceName, data, namer.LabelBuilder.Labels())
		desired = &secret
	} else {
		desired.Data = data
	}

	reconciler.log.V(1).Info("Requesting secret for broker properties", "name", resourceName.Name)
	reconciler.trackDesired(desired)

	reconciler.log.V(1).Info("Requesting mount for broker properties secret")
	return resourceName.Name, true, data, nil
}

func (r *ActiveMQArtemisReconcilerImpl) ProcessBrokerProperties(m map[string][]byte) {
	if condition := meta.FindStatusCondition(r.customResource.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType); condition != nil {
		if ordinal, err := r.ordinalFromScaleDownCondition(condition); err == nil {
			buf := newPropsWithHeader()
			fmt.Fprintln(buf, ScaleDownConfigTriggerOn)
			propertyFileName := scaleDownOnSigTermPropsKey(ordinal)
			m[propertyFileName] = buf.Bytes()
		} else {
			r.log.V(1).Error(err, "no scaledown_on_sig_term config applied, failed to extract ordinal from pending scaledown condition", "condition", condition)
		}
	}
}

func scaleDownOnSigTermPropsKey(ordinal int) string {
	propertyFileName := fmt.Sprintf(BrokerPropertiesOrdinalFormat, "scaledown_on_sig_term", ordinal)
	return propertyFileName
}

func alder32StringValue(alder32Bytes []byte) string {
	return fmt.Sprintf("%d", binary.BigEndian.Uint32(alder32Bytes))
}

func alder32Of(props []string) []byte {

	digest := adler32.New()
	for _, k := range props {
		digest.Write([]byte(k))
	}

	return digest.Sum(nil)
}

func BrokerPropertiesData(props []string) map[string][]byte {
	contents := make(map[string][]byte)

	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, "# generated by crd")
	fmt.Fprintln(buf, "#")

	for _, propertyKeyVal := range props {
		matches := ParseBrokerPropertyWithOrdinal(propertyKeyVal)

		if len(matches) > 0 {
			mapKey := matches[1] + OrdinalPrefixSep + BrokerPropertiesName
			value := matches[2]

			existing, found := contents[mapKey]
			if found {
				contents[mapKey] = []byte(fmt.Sprintf("%s%s\n", existing, value))
			} else {
				contents[mapKey] = []byte(fmt.Sprintf("%s\n", value))
			}
		} else {
			fmt.Fprintf(buf, "%s\n", propertyKeyVal)
		}
	}

	contents[BrokerPropertiesName] = buf.Bytes()

	return contents
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configureAffinity(podSpec *corev1.PodSpec, affinity *brokerv1beta1.AffinityConfig) {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) configurePodSecurityContext(podSpec *corev1.PodSpec, podSecurityContext *corev1.PodSecurityContext) {
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

func (reconciler *ActiveMQArtemisReconcilerImpl) configureContianerSecurityContext(container *corev1.Container, containerSecurityContext *corev1.SecurityContext) {
	reconciler.log.V(1).Info("Configuring Container SecurityContext")

	if nil != containerSecurityContext {
		reconciler.log.V(2).Info("Incoming Container SecurityContext is NOT nil, assigning")
		container.SecurityContext = containerSecurityContext
	} else {
		reconciler.log.V(2).Info("Incoming Container SecurityContext is nil, creating with default values")
		readOnlyRootFilesystem := common.IsRestricted(reconciler.customResource)
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

func sortedKeys(props map[string]string) []string {
	sortedKeys := make([]string, 0, len(props))
	for k := range props {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}

// generic version?
func sortedKeysStringKeyByteValue(props map[string][]byte) []string {
	sortedKeys := make([]string, 0, len(props))
	for k := range props {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)
	return sortedKeys
}

func (reconciler *ActiveMQArtemisReconcilerImpl) configPodSecurity(podSpec *corev1.PodSpec, podSecurity *brokerv1beta1.PodSecurityType) {
	if podSecurity.ServiceAccountName != nil {
		reconciler.log.V(2).Info("Pod serviceAccountName specified", "existing", podSpec.ServiceAccountName, "new", *podSecurity.ServiceAccountName)
		podSpec.ServiceAccountName = *podSecurity.ServiceAccountName
	} else {
		autoMount := !common.IsRestricted(reconciler.customResource)
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

func (reconciler *ActiveMQArtemisReconcilerImpl) createExtraConfigmapsAndSecretsVolumeMounts(configMaps []string, secrets []string, brokePropertiesResourceName string, brokerPropsData map[string][]byte, client rtclient.Client) ([]corev1.Volume, []corev1.VolumeMount, error) {

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
			secretPath := secretPathBase + secret
			//now we have a secret. First create a volume
			secretVol := volumes.MakeVolumeForSecret(secret)

			if secret == brokePropertiesResourceName && hasOrdinalPropertieKeyInData(brokerPropsData) {
				// place ordinal data in subpath in order
				for _, key := range sortedKeysStringKeyByteValue(brokerPropsData) {
					matches := ParseBrokerPropertyWithOrdinal(key)
					if len(matches) > 0 {
						subPath := matches[1]
						secretVol.VolumeSource.Secret.Items = append(secretVol.VolumeSource.Secret.Items, corev1.KeyToPath{Key: key, Path: fmt.Sprintf("%s/%s", subPath, key)})
					} else {
						secretVol.VolumeSource.Secret.Items = append(secretVol.VolumeSource.Secret.Items, corev1.KeyToPath{Key: key, Path: key})
					}
				}
			}

			if strings.HasSuffix(secret, brokerPropsSuffix) {
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
							secretVol.VolumeSource.Secret.Items = append(secretVol.VolumeSource.Secret.Items, corev1.KeyToPath{Key: key, Path: fmt.Sprintf("%s/%s", subPath, key)})
						} else {
							secretVol.VolumeSource.Secret.Items = append(secretVol.VolumeSource.Secret.Items, corev1.KeyToPath{Key: key, Path: key})
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

func hasOrdinalPropertieKeyInData(brokerPropsData map[string][]byte) bool {
	for key := range brokerPropsData {
		matches := ParseBrokerPropertyWithOrdinal(key)
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

var brokerPropertyWithOrdinalRegex *regexp.Regexp

func ParseBrokerPropertyWithOrdinal(property string) []string {
	if brokerPropertyWithOrdinalRegex == nil {
		brokerPropertyWithOrdinalRegex = regexp.MustCompile("^(" + regexp.QuoteMeta(OrdinalPrefix) + "[0-9]+)" + regexp.QuoteMeta(OrdinalPrefixSep) + "(.*)$")
	}
	return brokerPropertyWithOrdinalRegex.FindStringSubmatch(property)
}

func (reconciler *ActiveMQArtemisReconcilerImpl) StatefulSetForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, currentStateFullSet *appsv1.StatefulSet, client rtclient.Client) (*appsv1.StatefulSet, error) {

	reqLogger := reconciler.log.WithName(customResource.Name)

	namespacedName := types.NamespacedName{
		Name:      customResource.Name,
		Namespace: customResource.Namespace,
	}
	replicas := common.GetDeploymentSize(customResource)
	currentStateFullSet = ss.MakeStatefulSet(currentStateFullSet, namer.SsNameBuilder.Name(), namer.SvcHeadlessNameBuilder.Name(), namespacedName, nil, namer.LabelBuilder.Labels(), &replicas)

	podTemplateSpec, err := reconciler.PodTemplateSpecForCR(customResource, namer, currentStateFullSet, client)
	if err != nil {
		reqLogger.Error(err, "error creating pod template")
		return nil, err
	}
	currentStateFullSet.Spec.Template = *podTemplateSpec

	currentStateFullSet.Spec.VolumeClaimTemplates = reconciler.PersistentVolumeClaimArrayForCR(customResource, namer, currentStateFullSet.Spec)

	return currentStateFullSet, nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) PersistentVolumeClaimArrayForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers, spec appsv1.StatefulSetSpec) []corev1.PersistentVolumeClaim {

	var existing, current *corev1.PersistentVolumeClaim
	pvcArray := make([]corev1.PersistentVolumeClaim, 0)

	if customResource.Spec.DeploymentPlan.PersistenceEnabled {
		capacity := "2Gi"
		if customResource.Spec.DeploymentPlan.Storage.Size != "" {
			capacity = customResource.Spec.DeploymentPlan.Storage.Size
		}

		tempateClaim := &brokerv1beta1.VolumeClaimTemplate{
			ObjectMeta: brokerv1beta1.ObjectMeta{
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
		reconciler.applyTemplates(&pvcArray[index])
	}

	if len(pvcArray) > 0 {
		return pvcArray
	}
	return nil
}

func findExistingByName(persistentVolumeClaim []corev1.PersistentVolumeClaim, tempateClaim *brokerv1beta1.VolumeClaimTemplate) *corev1.PersistentVolumeClaim {
	for index, claim := range persistentVolumeClaim {
		if claim.Name == tempateClaim.Name {
			return &persistentVolumeClaim[index]
		}
	}
	return nil
}

func MakeEnvVarArrayForCR(customResource *brokerv1beta1.ActiveMQArtemis, namer common.Namers) []corev1.EnvVar {

	requireLogin := "false"
	if customResource.Spec.DeploymentPlan.RequireLogin {
		requireLogin = "true"
	} else {
		requireLogin = "false"
	}

	journalType := "aio"
	if strings.ToLower(customResource.Spec.DeploymentPlan.JournalType) == "aio" {
		journalType = "aio"
	} else {
		journalType = "nio"
	}

	jolokiaAgentEnabled := "false"
	if customResource.Spec.DeploymentPlan.JolokiaAgentEnabled {
		jolokiaAgentEnabled = "true"
	} else {
		jolokiaAgentEnabled = "false"
	}

	managementRBACEnabled := "false"
	if customResource.Spec.DeploymentPlan.ManagementRBACEnabled {
		managementRBACEnabled = "true"
	} else {
		managementRBACEnabled = "false"
	}

	metricsPluginEnabled := "false"
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

	envVarArrayForCluster := environments.AddEnvVarForCluster(isClustered(customResource))
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

type brokerStatus struct {
	BrokerConfigStatus brokerConfigStatus `json:"configuration"`
	ServerStatus       serverStatus       `json:"server"`
}

type serverStatus struct {
	Jaas    jaasStatus `json:"jaas"`
	State   string     `json:"state"`
	Version string     `json:"version"`
	NodeId  string     `json:"nodeId"`
	Uptime  string     `json:"uptime"`
}

type jaasStatus struct {
	PropertiesStatus map[string]propertiesStatus `json:"properties"`
}

type brokerConfigStatus struct {
	PropertiesStatus map[string]propertiesStatus `json:"properties"`
}

type propertiesStatus struct {
	Alder32     string       `json:"alder32"`
	FileAlder32 string       `json:"fileAlder32,omitempty"`
	ReloadTime  string       `json:"reloadTime"`
	ApplyErrors []applyError `json:"errors"`
}

type applyError struct {
	PropKeyValue string `json:"value"`
	Reason       string `json:"reason"`
}

func (reconciler *ActiveMQArtemisReconcilerImpl) ProcessBrokerStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) (retry bool) {
	var condition metav1.Condition

	// we need to requeue till stable
	retry = meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType)

	err := AssertBrokersAvailable(cr, client)
	if err != nil {
		condition = trapErrorAsCondition(err, brokerv1beta1.ConfigAppliedConditionType)
		meta.SetStatusCondition(&cr.Status.Conditions, condition)
		retry = retry || err.Requeue()
		return retry
	}

	err = reconciler.AssertBrokerImageVersion(cr, client)
	if err == nil {
		condition = metav1.Condition{
			Type:   brokerv1beta1.BrokerVersionAlignedConditionType,
			Status: metav1.ConditionTrue,
			Reason: brokerv1beta1.BrokerVersionAlignedConditionMatchReason,
		}
	} else {
		condition = trapErrorAsCondition(err, brokerv1beta1.BrokerVersionAlignedConditionType)
		retry = retry || err.Requeue()
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	err = reconciler.AssertBrokerPropertiesStatus(cr, client, scheme)
	if err == nil {
		condition = metav1.Condition{
			Type:   brokerv1beta1.ConfigAppliedConditionType,
			Status: metav1.ConditionTrue,
			Reason: brokerv1beta1.ConfigAppliedConditionSynchedReason,
		}
	} else {
		condition = trapErrorAsCondition(err, brokerv1beta1.ConfigAppliedConditionType)
		retry = retry || err.Requeue()
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	if _, _, found := getConfigExtraMount(cr, jaasConfigSuffix); found {
		err = reconciler.AssertJaasPropertiesStatus(cr, client, scheme)
		if err == nil {
			condition = metav1.Condition{
				Type:   brokerv1beta1.JaasConfigAppliedConditionType,
				Status: metav1.ConditionTrue,
				Reason: brokerv1beta1.ConfigAppliedConditionSynchedReason,
			}
		} else {
			condition = trapErrorAsCondition(err, brokerv1beta1.JaasConfigAppliedConditionType)
			retry = retry || err.Requeue()
		}

		meta.SetStatusCondition(&cr.Status.Conditions, condition)
	}

	// transition to check for empty after config for sig term updated
	scaleDownCondtion := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ScaleDownPendingConditionType)
	if scaleDownCondtion != nil {

		switch scaleDownCondtion.Reason {

		case brokerv1beta1.ScaleDownPendingConditionPendingConfigAppliedReason:
			if meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType) {
				if ordinal, err := reconciler.ordinalFromScaleDownCondition(scaleDownCondtion); err == nil {
					if len(reconciler.jolokiaEndpoints) > ordinal {
						// reusing logic from config applied with check for _key present on ordinal that is scaling down
						reconciler.CheckStatusFromJolokia(reconciler.jolokiaEndpoints[ordinal],
							func(BrokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
								if _, exists := BrokerStatus.BrokerConfigStatus.PropertiesStatus[scaleDownOnSigTermPropsKey(ordinal)]; exists {
									// restart for sig term config to take effect
									if _, err := jk.Artemis.ScaleDown(); err == nil {
										// transition
										scaleDownCondtion.Reason = brokerv1beta1.ScaleDownPendingConditionPendingEmptyReason
										scaleDownCondtion.Message = reconciler.ScaleDownConditionPodNameMessage(int32(ordinal))
									} else {
										scaleDownCondtion.Message = reconciler.ScaleDownConditionMessage(int32(ordinal), fmt.Sprintf("scaledown to force reload of sig term scaledown properties failed, reason: %v", err))
									}
								} else {
									scaleDownCondtion.Message = reconciler.ScaleDownConditionMessage(int32(ordinal), fmt.Sprintf("pending presence of %s key in properties status", scaleDownOnSigTermPropsKey(ordinal)))
								}
								return nil
							})
					}
				} else {
					reconciler.log.V(1).Error(err, "unable to vaidate config applied condition, failed to extract ordinal from pending scaledown condition", "condition", scaleDownCondtion)
				}
			}
		}
		meta.SetStatusCondition(&cr.Status.Conditions, *scaleDownCondtion)
	}
	return retry
}

func (r *ActiveMQArtemisReconcilerImpl) ScaleDownConditionPodNameMessage(ordinal int32) string {
	return r.ScaleDownConditionMessage(ordinal, "")
}

func (r *ActiveMQArtemisReconcilerImpl) ScaleDownConditionMessage(ordinal int32, message string) string {
	podName := namer.CrToSSOrdinal(r.customResource.Name, int(ordinal))
	if message == "" {
		return podName
	} else {
		return fmt.Sprintf("%s %s", podName, message)
	}
}

func (r *ActiveMQArtemisReconcilerImpl) ordinalFromScaleDownCondition(condition *metav1.Condition) (int, error) {
	return namer.OrdinalFromPodName(r.customResource.Name, condition.Message)
}

func trapErrorAsCondition(err ArtemisError, conditionType string) metav1.Condition {
	var condition metav1.Condition
	switch err.(type) {
	case jolokiaClientNotFoundError:
		condition = metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ConfigAppliedConditionNoJolokiaClientsAvailableReason,
			Message: err.Error(),
		}
	case statusOutOfSyncError:
		condition = metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ConfigAppliedConditionOutOfSyncReason,
			Message: err.Error(),
		}
	case statusOutOfSyncMissingKeyError:
		condition = metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  brokerv1beta1.ConfigAppliedConditionOutOfSyncReason,
			Message: err.Error(),
		}
	case inSyncApplyError:
		condition = metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionFalse,
			Reason:  brokerv1beta1.ConfigAppliedConditionSynchedWithErrorReason,
			Message: err.Error(),
		}
	case versionMismatchError:
		condition = metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  brokerv1beta1.BrokerVersionAlignedConditionMismatchReason,
			Message: err.Error(),
		}
	default:
		condition = metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  brokerv1beta1.ConfigAppliedConditionUnknownReason,
			Message: err.Error(),
		}
	}
	return condition
}

type projection struct {
	Name            string
	ResourceVersion string
	Generation      int64
	Files           map[string]propertyFile
	Ordinals        []string
}

type propertyFile struct {
	Alder32     string
	FileAlder32 string
}

func AssertBrokersAvailable(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	// pre-condition, we must be deployed, avoid broker status roundtrip till ready
	DeployedCondition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.DeployedConditionType)
	if DeployedCondition == nil || DeployedCondition.Status == metav1.ConditionFalse {
		reqLogger.V(2).Info("There are no available brokers from DeployedCondition", "condition", DeployedCondition)
		return NewArtemisStatusError(errors.New("no available brokers from deployed condition"), false)
	}
	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) AssertBrokerPropertiesStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	secretProjection, err := reconciler.getSecretProjection(getPropertiesResourceNsName(cr), client)
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
			if strings.HasSuffix(extraSecretName, brokerPropsSuffix) {
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
					updateExtraConfigStatus(cr, secretProjection)
				} else {
					// report the first error
					break
				}
			}
		}
	}

	return errorStatus
}

func (reconciler *ActiveMQArtemisReconcilerImpl) AssertJaasPropertiesStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, scheme *runtime.Scheme) ArtemisError {
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
		updateExtraConfigStatus(cr, Projection)
	}

	return statusError
}

func (reconciler *ActiveMQArtemisReconcilerImpl) AssertBrokerImageVersion(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) ArtemisError {
	reqLogger := ctrl.Log.WithValues("ActiveMQArtemis Name", cr.Name)

	// The ResolveBrokerVersionFromCR should never fail because validation succeeded
	resolvedFullVersion, _ := common.ResolveBrokerVersionFromCR(cr)

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

func (reconciler *ActiveMQArtemisReconcilerImpl) CheckStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, checkBrokerStatus func(BrokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError) ArtemisError {

	reconciler.resolveJolokiaEndpoints(cr, client)

	if len(reconciler.jolokiaEndpoints) == 0 {
		reconciler.log.V(1).Info("no Jolokia Clients available. requeing")
		return NewJolokiaClientsNotFoundError(errors.New("Waiting for Jolokia Clients to become available"))
	}

	for _, jk := range reconciler.jolokiaEndpoints {

		artemisError := reconciler.CheckStatusFromJolokia(jk, checkBrokerStatus)
		if artemisError != nil {
			return artemisError
		}
	}

	return nil
}

func (reconciler *ActiveMQArtemisReconcilerImpl) CheckStatusFromJolokia(jk *jolokia_client.JkInfo, checkBrokerStatus func(BrokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError) ArtemisError {

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

func (reconciler *ActiveMQArtemisReconcilerImpl) GetAndCacheBrokerStatus(jk *jolokia_client.JkInfo) (*brokerStatus, ArtemisError) {

	if cached, exists := reconciler.cachedBrokerStatus[jk.Ordinal]; exists {
		switch v := cached.(type) {
		case ArtemisError:
			return nil, v
		case brokerStatus:
			return &v, nil
		}
	}

	currentJson, err := jk.Artemis.GetStatus()

	if err != nil {
		reconciler.log.V(1).Info("error getting broker status with Jolokia", "IP", jk.IP, "Ordinal", jk.Ordinal, "error", err)
		artemisError := NewArtemisStatusError(err, true)
		reconciler.cachedBrokerStatus[jk.Ordinal] = artemisError
		return nil, artemisError
	}

	reconciler.log.V(2).Info("raw json status", "IP", jk.IP, "ordinal", jk.Ordinal, "status json", currentJson)

	brokerStatus, err := unmarshallStatus(currentJson)
	if err != nil {
		reconciler.log.Error(err, "unable to unmarshall broker status", "json", currentJson)
		artemisError := NewArtemisStatusError(err, false)
		reconciler.cachedBrokerStatus[jk.Ordinal] = artemisError
		return nil, artemisError
	}

	reconciler.log.V(2).Info("cached broker status", "ordinal", jk.Ordinal, "status", brokerStatus)
	reconciler.cachedBrokerStatus[jk.Ordinal] = brokerStatus

	return &brokerStatus, nil

}

func (reconciler *ActiveMQArtemisReconcilerImpl) resolveJolokiaEndpoints(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) {
	if reconciler.jolokiaEndpoints == nil {
		if common.IsRestricted(cr) {
			reconciler.jolokiaEndpoints = jolokia_client.GetMinimalJolokiaAgents(cr, client)
		} else {
			resource := types.NamespacedName{
				Name:      cr.Name,
				Namespace: cr.Namespace,
			}
			reconciler.jolokiaEndpoints = jolokia_client.GetBrokers(resource, []ss.StatefulSetInfo{
				{
					NamespacedName: types.NamespacedName{Name: namer.CrToSS(cr.Name), Namespace: cr.Namespace},
					Replicas:       cr.Status.DeploymentPlanSize, // this means we wait till the pod status is good before trying the jolokia endpoint
					Labels:         nil,
				}}, client)
		}
	}
}

func (reconciler *ActiveMQArtemisReconcilerImpl) checkProjectionStatus(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client, secretProjection *projection, extractStatus func(BrokerStatus *brokerStatus, FileName string) (propertiesStatus, bool)) ArtemisError {
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
				if !(name == JaasConfigKey || strings.HasPrefix(name, UncheckedPrefix) || len(matches) > 0) {
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

func statusMessageFromProjection(Projection *projection) string {
	var statusMessage string
	statusMessageJson, err := json.Marshal(*Projection)
	if err == nil {
		statusMessage = string(statusMessageJson)
	} else {
		statusMessage = fmt.Sprintf("%+v", *Projection)
	}
	return statusMessage
}

func updateExtraConfigStatus(cr *brokerv1beta1.ActiveMQArtemis, Projection *projection) {
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
		brokerv1beta1.ExternalConfigStatus{Name: Projection.Name, ResourceVersion: Projection.ResourceVersion})
}

func (reconciler *ActiveMQArtemisReconcilerImpl) getSecretProjection(secretName types.NamespacedName, client rtclient.Client) (*projection, error) {
	resource := &corev1.Secret{}

	// check our latest desired content
	desired := reconciler.getFromDesired(reflect.TypeOf(resource), secretName.Name)
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

func (reconciler *ActiveMQArtemisReconcilerImpl) getConfigMappedJaasProperties(cr *brokerv1beta1.ActiveMQArtemis, client rtclient.Client) (*projection, error) {
	if _, name, found := getConfigExtraMount(cr, jaasConfigSuffix); found {
		return reconciler.getSecretProjection(types.NamespacedName{Namespace: cr.Namespace, Name: name}, client)
	}
	return nil, nil
}

func newProjectionFromByteValues(resourceMeta metav1.ObjectMeta, configKeyValue map[string][]byte) *projection {
	projection := projection{Name: resourceMeta.Name, ResourceVersion: resourceMeta.ResourceVersion, Generation: resourceMeta.Generation, Files: map[string]propertyFile{}}
	for prop_file_name, data := range configKeyValue {
		if strings.HasSuffix(prop_file_name, JsonSuffix) {
			dataMap := map[string]interface{}{}
			if err := json.Unmarshal(data, &dataMap); err == nil {
				keyValuePairs := []string{}
				SortedKeyValuePairsFromMap(dataMap, &keyValuePairs)
				projection.Files[prop_file_name] = propertyFile{
					Alder32:     alder32StringValue(alder32Of(keyValuePairs)),
					FileAlder32: fmt.Sprintf("%d", adler32.Checksum(data)),
				}
			}
		} else {
			projection.Files[prop_file_name] = propertyFile{
				Alder32:     alder32FromData(data),
				FileAlder32: fmt.Sprintf("%d", adler32.Checksum(data)),
			}
		}
	}
	return &projection
}

func SortedKeyValuePairsFromMap(dataMap map[string]interface{}, pairs *[]string) {
	KeyValuePairsFromMap("", dataMap, pairs)

	sort.Slice(*pairs, func(i, j int) bool {
		return (*pairs)[i] < (*pairs)[j]
	})

}

func KeyValuePairsFromMap(parentKey string, dataMap map[string]interface{}, pairs *[]string) {
	for k, v := range dataMap {
		if strings.Contains(k, ".") {
			k = "\"" + k + "\""
		}
		propertyKey := parentKey + k
		if reflect.ValueOf(v).Kind() == reflect.Map {
			KeyValuePairsFromMap(propertyKey+".", v.(map[string]interface{}), pairs)
		} else {
			*pairs = append(*pairs, propertyKey+"="+fmt.Sprint(v))
		}
	}
}

func alder32FromData(data []byte) string {
	return alder32StringValue(alder32Of(KeyValuePairs(data)))
}

func KeyValuePairs(data []byte) []string {
	// need to skip white space and comments for checksum
	keyValuePairs := []string{}

	uniCodeDataLines := strings.Split(string(data), "\n")
	for _, lineToTrim := range uniCodeDataLines {
		line := strings.TrimLeftFunc(lineToTrim, unicode.IsSpace)
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			// ignore comments
			continue
		}
		keyValuePairs = appendNonEmpty(keyValuePairs, line)
	}
	return keyValuePairs
}

func appendNonEmpty(propsKvs []string, data string) []string {
	keyAndValue := strings.TrimSpace(string(data))
	if keyAndValue != "" {
		// need to trim space arround the '=' in x = y to match properties loader check sum
		equalsSeparator := "="
		keyAndValueTokens := strings.SplitN(keyAndValue, equalsSeparator, 2)
		numTokens := len(keyAndValueTokens)
		if numTokens == 2 {
			keyAndValue =
				strings.TrimRightFunc(keyAndValueTokens[0], unicode.IsSpace) +
					equalsSeparator +
					strings.TrimLeftFunc(keyAndValueTokens[1], unicode.IsSpace)
		}
		// escaped x will converted on read, need to replace for check sum
		keyAndValue = strings.ReplaceAll(keyAndValue, `\ `, ` `)
		keyAndValue = strings.ReplaceAll(keyAndValue, `\:`, `:`)
		keyAndValue = strings.ReplaceAll(keyAndValue, `\=`, `=`)
		keyAndValue = strings.ReplaceAll(keyAndValue, `\"`, `"`)
		if keyAndValue != "" {
			propsKvs = append(propsKvs, keyAndValue)
		}
	}
	return propsKvs
}

func unmarshallStatus(jsonStatus string) (brokerStatus, error) {
	cmStatus := brokerStatus{}
	err := json.Unmarshal([]byte(jsonStatus), &cmStatus)
	return cmStatus, err
}

func marshallApplyErrors(applyErrors []applyError) string {
	val, _ := json.Marshal(applyErrors)
	return string(val)
}
