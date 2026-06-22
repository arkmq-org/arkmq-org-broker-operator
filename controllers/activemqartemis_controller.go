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
	"reflect"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
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

func (r *ActiveMQArtemisReconciler) toBrokerClusterParent() *BrokerClusterReconciler {
	return &BrokerClusterReconciler{
		Client:        r.Client,
		Scheme:        r.Scheme,
		log:           r.log,
		isOnOpenShift: r.isOnOpenShift,
	}
}

//run 'make manifests' after changing the following rbac markers

//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemises,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemises/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=activemqartemises/finalizers,verbs=update
//+kubebuilder:rbac:groups=broker.amq.io,namespace=arkmq-org-broker-operator,resources=pods,verbs=get;list
//+kubebuilder:rbac:groups="",namespace=arkmq-org-broker-operator,resources=pods;services;endpoints;persistentvolumeclaims;events;configmaps;secrets;routes;serviceaccounts,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups="",namespace=arkmq-org-broker-operator,resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,namespace=arkmq-org-broker-operator,resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=networking.k8s.io,namespace=arkmq-org-broker-operator,resources=ingresses,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=route.openshift.io,namespace=arkmq-org-broker-operator,resources=routes;routes/custom-host;routes/status,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=monitoring.coreos.com,namespace=arkmq-org-broker-operator,resources=servicemonitors,verbs=get;create
//+kubebuilder:rbac:groups=apps,namespace=arkmq-org-broker-operator,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace=arkmq-org-broker-operator,resources=roles;rolebindings,verbs=create;get;delete
//+kubebuilder:rbac:groups=policy,namespace=arkmq-org-broker-operator,resources=poddisruptionbudgets,verbs=create;get;delete;list;update;watch

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

	artemisResource := &brokerv1beta1.ActiveMQArtemis{}

	result := ctrl.Result{}

	err := r.Get(context.TODO(), request.NamespacedName, artemisResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			reqLogger.V(1).Info("ActiveMQArtemis Controller Reconcile encountered a IsNotFound, for request NamespacedName " + request.NamespacedName.String())
			return result, nil
		}
		reqLogger.Error(err, "unable to retrieve the ActiveMQArtemis")
		return result, err
	}

	customResource, err := ConvertArtemisToBrokerCluster(artemisResource)
	if err != nil {
		reqLogger.Error(err, "failed to convert ActiveMQArtemis to internal Broker representation")
		return result, err
	}

	var reconcileBlocked bool = false
	if val, present := customResource.Annotations[common.BlockReconcileAnnotation]; present {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			reconcileBlocked = boolVal
		}
	}

	namer := MakeNamers(customResource)
	reconciler := NewBrokerClusterReconcilerImpl(customResource, r.toBrokerClusterParent())

	var requeueRequest bool = false
	var valid bool = false
	if valid, requeueRequest = reconciler.validate(customResource, r.Client, *namer); valid {

		if !reconcileBlocked {
			err = reconciler.Process(customResource, *namer, r.Client, r.Scheme)
		}
		if reconciler.ProcessBrokerClusterStatus(customResource, r.Client, r.Scheme) {
			requeueRequest = true
		}
	}

	common.UpdateBlockedStatus(customResource, reconcileBlocked)
	common.ProcessStatus(customResource, r.Client, request.NamespacedName, *namer, err)

	if !requeueRequest {
		deployedCondition := meta.FindStatusCondition(customResource.Status.Conditions, brokerv1beta1.DeployedConditionType)
		if deployedCondition != nil && deployedCondition.Status == metav1.ConditionFalse && deployedCondition.Reason == brokerv1beta1.DeployedConditionNotReadyReason {
			requeueRequest = true
		}
	}

	if convertErr := ConvertBrokerClusterStatusToArtemis(customResource, artemisResource); convertErr != nil {
		reqLogger.Error(convertErr, "failed to convert status back to ActiveMQArtemis")
		return result, convertErr
	}

	crStatusUpdateErr := r.UpdateCRStatus(artemisResource, r.Client, request.NamespacedName)
	if crStatusUpdateErr != nil {
		requeueRequest = true
	}

	if !requeueRequest && !reconcileBlocked && hasExtraMounts(customResource) {
		reqLogger.V(1).Info("resource has extraMounts, requeuing")
		requeueRequest = true
	}

	if requeueRequest && err == nil {
		reqLogger.V(1).Info("requeue reconcile")
		result = ctrl.Result{RequeueAfter: common.GetReconcileResyncPeriod()}
	}

	if valid && err == nil && crStatusUpdateErr == nil {
		reqLogger.V(1).Info("resource successfully reconciled")
	}

	if err != nil {
		reqLogger.V(1).Error(err, "reconcile failed")
	}
	return result, err
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
