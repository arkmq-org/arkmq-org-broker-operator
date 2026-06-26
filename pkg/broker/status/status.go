// Package status manages Broker CR status updates — conditions, pod state, version tracking, and deployment readiness.
package status

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/RHsyseng/operator-utils/pkg/resource/read"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	brokerversion "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/broker/version"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/channels"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	policyv1 "k8s.io/api/policy/v1"
)

var lastStatusMap map[types.NamespacedName]olm.DeploymentStatus = make(map[types.NamespacedName]olm.DeploymentStatus)

func ProcessStatus(cr *v1beta2.Broker, client rtclient.Client, namespacedName types.NamespacedName, namer common.Namers, reconcileError error) {
	reqLogger := ctrl.Log.WithName("util_process_status").WithValues("ActiveMQArtemis Name", cr.Name)

	updateVersionStatus(cr)

	updateScaleStatus(cr, namer)

	cr.Status.PodStatus = updatePodStatus(cr, client, namespacedName)

	reqLogger.V(1).Info("PodStatus current", "info:", cr.Status.PodStatus)

	ValidCondition := getValidCondition(cr)
	SetStatusConditionWithGeneration(cr, ValidCondition)
	meta.SetStatusCondition(&cr.Status.Conditions, getDeploymentCondition(cr, client, ValidCondition.Status != metav1.ConditionFalse, reconcileError))
}

func SetStatusConditionWithGeneration(cr *v1beta2.Broker, condition metav1.Condition) {
	condition.ObservedGeneration = cr.Generation
	meta.SetStatusCondition(&cr.Status.Conditions, condition)
}

func UpdateBlockedStatus(cr *v1beta2.Broker, blocked bool) {
	if blocked {
		SetStatusConditionWithGeneration(cr, metav1.Condition{
			Type:    v1beta2.ReconcileBlockedType,
			Status:  metav1.ConditionTrue,
			Reason:  v1beta2.ReconcileBlockedReason,
			Message: "Reconcile blocked by presence of annotation " + common.BlockReconcileAnnotation,
		})
	} else {
		meta.RemoveStatusCondition(&cr.Status.Conditions, v1beta2.ReconcileBlockedType)
	}
}

func updateVersionStatus(cr *v1beta2.Broker) {
	cr.Status.Version.Image = brokerversion.ResolveImage(cr, common.BrokerImageKey)
	cr.Status.Version.InitImage = brokerversion.ResolveImage(cr, common.InitImageKey)
	cr.Status.Version.BrokerVersion, _ = brokerversion.ResolveBrokerVersionFromCR(cr)

	if brokerversion.IsLockedDown(cr.Spec.DeploymentPlan.Image) || brokerversion.IsLockedDown(cr.Spec.DeploymentPlan.InitImage) {
		cr.Status.Upgrade.SecurityUpdates = false
		cr.Status.Upgrade.MajorUpdates = false
		cr.Status.Upgrade.MinorUpdates = false
		cr.Status.Upgrade.PatchUpdates = false

	} else {
		cr.Status.Upgrade.SecurityUpdates = true

		if cr.Spec.Version == "" {
			cr.Status.Upgrade.MajorUpdates = true
			cr.Status.Upgrade.MinorUpdates = true
			cr.Status.Upgrade.PatchUpdates = true
		} else {

			cr.Status.Upgrade.MajorUpdates = false
			cr.Status.Upgrade.MinorUpdates = false
			cr.Status.Upgrade.PatchUpdates = false

			switch len(strings.Split(cr.Spec.Version, ".")) {
			case 1:
				cr.Status.Upgrade.MinorUpdates = true
				fallthrough
			case 2:
				cr.Status.Upgrade.PatchUpdates = true
			}
		}
	}
}

func updateScaleStatus(cr *v1beta2.Broker, n common.Namers) {
	labels := make([]string, 0, len(n.LabelBuilder.Labels())+len(cr.Spec.DeploymentPlan.Labels))
	for k, v := range n.LabelBuilder.Labels() {
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range cr.Spec.DeploymentPlan.Labels {
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(labels)
	cr.Status.ScaleLabelSelector = strings.Join(labels[:], ",")
}

func updatePodStatus(cr *v1beta2.Broker, client rtclient.Client, namespacedName types.NamespacedName) olm.DeploymentStatus {
	reqLogger := ctrl.Log.WithName("util_update_pod_status").WithValues("ActiveMQArtemis Name", namespacedName.Name)
	reqLogger.V(1).Info("Getting status for pods")

	var status olm.DeploymentStatus
	var lastStatus olm.DeploymentStatus

	lastStatusExist := false
	if lastStatus, lastStatusExist = lastStatusMap[namespacedName]; !lastStatusExist {
		reqLogger.V(2).Info("Creating lastStatus for new CR", "name", namespacedName)
		lastStatus = olm.DeploymentStatus{}
		lastStatusMap[namespacedName] = lastStatus
	}

	ssNamespacedName := types.NamespacedName{Name: namer.CrToSS(namespacedName.Name), Namespace: namespacedName.Namespace}
	sfsFound := &appsv1.StatefulSet{}
	err := client.Get(context.TODO(), ssNamespacedName, sfsFound)
	if err == nil {
		cr.Status.DeploymentPlanSize = 1
		podName := fmt.Sprintf("%s-0", sfsFound.Name)
		if sfsFound.Status.ReadyReplicas == 0 {
			status = olm.DeploymentStatus{Starting: []string{podName}}
		} else {
			status = olm.DeploymentStatus{Ready: []string{podName}}
		}
	}

	if len(status.Ready) == 1 && len(lastStatus.Ready) == 0 {
		reqLogger.V(1).Info("Notifying address controller", "new ready", status.Ready[0])
		channels.AddressListeningCh <- types.NamespacedName{Namespace: namespacedName.Namespace, Name: status.Ready[0]}
	}
	lastStatusMap[namespacedName] = status

	return status
}

func getValidCondition(cr *v1beta2.Broker) metav1.Condition {
	for _, c := range cr.Status.Conditions {
		if c.Type == v1beta2.ValidConditionType {
			return c
		}
	}
	return metav1.Condition{
		Type:   v1beta2.ValidConditionType,
		Reason: v1beta2.ValidConditionSuccessReason,
		Status: metav1.ConditionTrue,
	}
}

func getDeploymentCondition(cr *v1beta2.Broker, client rtclient.Client, valid bool, reconcileError error) metav1.Condition {
	if !valid {
		return metav1.Condition{
			Type:   v1beta2.DeployedConditionType,
			Status: metav1.ConditionFalse,
			Reason: v1beta2.DeployedConditionValidationFailedReason,
		}
	}

	if reconcileError != nil {
		return metav1.Condition{
			Type:    v1beta2.DeployedConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.DeployedConditionCrudKindErrorReason,
			Message: reconcileError.Error(),
		}
	}

	deploymentSize := GetDeploymentSize(cr)
	if deploymentSize == 0 {
		return metav1.Condition{
			Type:    v1beta2.DeployedConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.DeployedConditionZeroSizeReason,
			Message: common.DeployedConditionZeroSizeMessage,
		}
	}
	if len(cr.Status.PodStatus.Ready) == 0 {
		crDeployedCondition := metav1.Condition{
			Type:    v1beta2.DeployedConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  v1beta2.DeployedConditionNotReadyReason,
			Message: "0/1 pods ready",
		}
		if len(cr.Status.PodStatus.Starting) == 1 {
			startingPodName := cr.Status.PodStatus.Starting[0]
			podNamespacedName := types.NamespacedName{Namespace: cr.Namespace, Name: startingPodName}
			pod := &corev1.Pod{}
			if err := client.Get(context.TODO(), podNamespacedName, pod); err == nil {
				ctrl.Log.V(1).Info("Pod "+startingPodName, "starting status", pod.Status)
				crDeployedCondition.Message = fmt.Sprintf("%s %s", crDeployedCondition.Message, podStartingStatusDigestMessage(startingPodName, pod.Status))
			}
		}
		return crDeployedCondition
	}
	return metav1.Condition{
		Type:   v1beta2.DeployedConditionType,
		Reason: v1beta2.DeployedConditionReadyReason,
		Status: metav1.ConditionTrue,
	}
}

func podStartingStatusDigestMessage(podName string, status corev1.PodStatus) string {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "{%s", podName)

	if status.Phase != "" {
		fmt.Fprintf(buf, ": %s", status.Phase)
	}

	if len(status.Conditions) > 0 {
		fmt.Fprintf(buf, " [")
	}
	for _, condition := range status.Conditions {
		fmt.Fprintf(buf, "{%s", condition.DeepCopy().Type)
		if condition.Status != "" {
			fmt.Fprintf(buf, "=%s", condition.Status)
		}
		if condition.Reason != "" {
			fmt.Fprintf(buf, " %s", condition.Reason)
		}
		if condition.Message != "" {
			fmt.Fprintf(buf, " %s", condition.Message)
		}
		fmt.Fprintf(buf, "}")
	}
	if len(status.Conditions) > 0 {
		fmt.Fprintf(buf, "]")
	}
	fmt.Fprintf(buf, "}")
	return buf.String()
}

func GetDeploymentSize(cr *v1beta2.Broker) int32 {
	return common.DefaultDeploymentSize
}

func GetDeployedResources(instance *v1beta2.Broker, client rtclient.Client, onOpenShift bool) (map[reflect.Type][]rtclient.Object, error) {
	log := ctrl.Log.WithName("util_common")
	reader := read.New(client).WithNamespace(instance.Namespace).WithOwnerObject(instance)
	var resourceMap map[reflect.Type][]rtclient.Object
	var err error
	if onOpenShift {
		resourceMap, err = reader.ListAll(
			&corev1.ServiceList{},
			&appsv1.StatefulSetList{},
			&routev1.RouteList{},
			&corev1.SecretList{},
			&corev1.ConfigMapList{},
			&policyv1.PodDisruptionBudgetList{},
			&netv1.IngressList{},
		)
	} else {
		resourceMap, err = reader.ListAll(
			&corev1.ServiceList{},
			&appsv1.StatefulSetList{},
			&netv1.IngressList{},
			&corev1.SecretList{},
			&corev1.ConfigMapList{},
			&policyv1.PodDisruptionBudgetList{},
		)
	}
	if err != nil {
		log.Error(err, "Failed to list deployed objects.")
		return nil, err
	}

	return resourceMap, nil
}
