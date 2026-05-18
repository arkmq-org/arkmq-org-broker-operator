/*
Copyright 2022.

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

package statefulsets

import (
	"context"
	"reflect"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"

	"github.com/RHsyseng/operator-utils/pkg/resource/read"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/pods"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulSetInfo struct {
	NamespacedName types.NamespacedName
	Labels         map[string]string
	Replicas       int32
}

func MakeStatefulSet(currentStateFulSet *appsv1.StatefulSet, ssName string, svcHeadlessName string, namespacedName types.NamespacedName, annotations map[string]string, labels map[string]string, replicas *int32) *appsv1.StatefulSet {

	if currentStateFulSet == nil {
		currentStateFulSet = &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{
				Kind:       "StatefulSet",
				APIVersion: "apps/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      ssName,
				Namespace: namespacedName.Namespace,
			},
			Spec: appsv1.StatefulSetSpec{
				// these fields are immutable
				ServiceName: svcHeadlessName,
				Selector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		}
	}
	// these fields we reconcile
	currentStateFulSet.ObjectMeta.Labels = labels
	common.ApplyAnnotations(&currentStateFulSet.ObjectMeta, annotations)

	currentStateFulSet.Spec.Template = *pods.MakePodTemplateSpec(&currentStateFulSet.Spec.Template, namespacedName, labels, nil)

	return currentStateFulSet
}

var GLOBAL_CRNAME string = ""

func RetrieveStatefulSet(statefulsetName string, namespacedName types.NamespacedName, labels map[string]string, client client.Client) (*appsv1.StatefulSet, error) {

	// Log where we are and what we're doing
	reqLogger := ctrl.Log.WithName("util_statefulset")

	reqLogger.V(1).Info("Retrieving " + "StatefulSet " + statefulsetName)

	var err error = nil

	ss := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        statefulsetName,
			Namespace:   namespacedName.Namespace,
			Labels:      labels,
			Annotations: nil,
		},
	}

	if err = client.Get(context.TODO(), namespacedName, ss); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.V(1).Info("StatefulSet claim IsNotFound", "Namespace", namespacedName.Namespace, "Name", namespacedName.Name)
		} else {
			reqLogger.V(1).Info("StatefulSet claim found", "Namespace", namespacedName.Namespace, "Name", namespacedName.Name)
		}
	}

	return ss, err
}

func GetDeployedStatefulSetNames(client rtclient.Client, ns string, filter []types.NamespacedName) []StatefulSetInfo {

	var result []StatefulSetInfo = nil

	var resourceMap map[reflect.Type][]rtclient.Object

	// may need to lock down list result with labels
	resourceMap, _ = read.New(client).WithNamespace(ns).ListAll(
		&appsv1.StatefulSetList{},
	)

	for _, ssObject := range resourceMap[reflect.TypeOf(appsv1.StatefulSet{})] {
		// track if a match
		if len(filter) == 0 {
			result = append(result, buildStatefulSetInfo(ssObject.(*appsv1.StatefulSet)))
		} else {
			for _, ref := range filter {
				if ref.Namespace == ssObject.GetNamespace() && ref.Name == namer.SSToCr(ssObject.GetName()) {
					result = append(result, buildStatefulSetInfo(ssObject.(*appsv1.StatefulSet)))
				}
			}
		}
	}
	return result
}

func buildStatefulSetInfo(ssObject *appsv1.StatefulSet) StatefulSetInfo {
	var replicas int32 = 0
	if ssObject.Spec.Replicas != nil {
		replicas = *ssObject.Spec.Replicas
	}
	return StatefulSetInfo{
		NamespacedName: types.NamespacedName{Namespace: ssObject.GetNamespace(), Name: ssObject.GetName()},
		Labels:         ssObject.GetLabels(),
		Replicas:       replicas,
	}
}
