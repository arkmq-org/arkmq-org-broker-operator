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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

// BrokerClusterReconciler reconciles a BrokerCluster object (broker.arkmq.org/v1beta2)
type BrokerClusterReconciler struct {
	rtclient.Client
	Scheme        *runtime.Scheme
	log           logr.Logger
	isOnOpenShift bool
}

func NewBrokerClusterReconciler(cluster cluster.Cluster, logger logr.Logger, isOpenShift bool) *BrokerClusterReconciler {
	return &BrokerClusterReconciler{
		isOnOpenShift: isOpenShift,
		Client:        cluster.GetClient(),
		Scheme:        cluster.GetScheme(),
		log:           logger,
	}
}

//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=broker.arkmq.org,namespace=arkmq-org-broker-operator,resources=brokerclusters/finalizers,verbs=update

func (r *BrokerClusterReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name, "Reconciling", "BrokerCluster")

	customResource := &v1beta2.BrokerCluster{}

	result := ctrl.Result{}

	err := r.Get(ctx, request.NamespacedName, customResource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			reqLogger.V(1).Info("BrokerCluster Controller Reconcile encountered a IsNotFound, for request NamespacedName " + request.NamespacedName.String())
			return result, nil
		}
		reqLogger.Error(err, "unable to retrieve the BrokerCluster")
		return result, err
	}

	var reconcileBlocked bool = false
	if val, present := customResource.Annotations[common.BlockReconcileAnnotation]; present {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			reconcileBlocked = boolVal
		}
	}

	namer := MakeNamers(customResource)
	reconciler := NewBrokerClusterReconcilerImpl(customResource, r)

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

	crStatusUpdateErr := r.UpdateBrokerClusterCRStatus(customResource, r.Client, request.NamespacedName)
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

func (r *BrokerClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta2.BrokerCluster{}).
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

func (r *BrokerClusterReconciler) UpdateBrokerClusterCRStatus(desired *v1beta2.BrokerCluster, client rtclient.Client, namespacedName types.NamespacedName) error {

	common.SetReadyCondition(&desired.Status.Conditions)

	current := &v1beta2.BrokerCluster{}

	err := client.Get(context.TODO(), namespacedName, current)
	if err != nil {
		r.log.Error(err, "unable to retrieve current resource", "BrokerCluster", namespacedName)
		return err
	}

	if !EqualBrokerClusterCRStatus(&desired.Status, &current.Status) {
		r.log.V(1).Info("cr.status update", "Namespace", desired.Namespace, "Name", desired.Name, "Observed status", desired.Status)
		return resources.UpdateStatus(client, desired)
	}

	return nil
}

func EqualBrokerClusterCRStatus(s1, s2 *v1beta2.BrokerClusterStatus) bool {
	if s1.DeploymentPlanSize != s2.DeploymentPlanSize ||
		s1.ScaleLabelSelector != s2.ScaleLabelSelector ||
		!reflect.DeepEqual(s1.Version, s2.Version) ||
		len(s2.ExternalConfigs) != len(s1.ExternalConfigs) ||
		brokerClusterExternalConfigsModified(s2.ExternalConfigs, s1.ExternalConfigs) ||
		!reflect.DeepEqual(s1.PodStatus, s2.PodStatus) ||
		len(s1.Conditions) != len(s2.Conditions) ||
		conditionsModified(s2.Conditions, s1.Conditions) {

		return false
	}

	return true
}

func brokerClusterExternalConfigsModified(desiredExternalConfigs []v1beta2.ExternalConfigStatus, currentExternalConfigs []v1beta2.ExternalConfigStatus) bool {
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
