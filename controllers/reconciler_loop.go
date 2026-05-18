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
	"fmt"
	"reflect"

	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	"github.com/RHsyseng/operator-utils/pkg/resource/read"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type KubeBits struct {
	client.Client
	Scheme *runtime.Scheme
	Config *rest.Config
	log    logr.Logger
}

type ReconcilerLoopType interface {
	getOwned() []client.ObjectList
	getOrderedTypeList() []reflect.Type
}

type ReconcilerLoop struct {
	*KubeBits
	ReconcilerLoopType

	deployed map[reflect.Type][]client.Object
	desired  map[reflect.Type]map[string]client.Object
}

func (reconciler *ReconcilerLoop) InitDeployed(owner client.Object, owned ...client.ObjectList) (err error) {

	reader := read.New(reconciler.Client).WithNamespace(owner.GetNamespace()).WithOwnerObject(owner)
	if reconciler.deployed, err = reader.ListAll(owned...); err != nil {
		reconciler.log.Error(err, "failed to list deployed objects")
		return err
	}

	for t, objs := range reconciler.deployed {
		for _, obj := range objs {
			reconciler.log.V(2).Info("Deployed ", "Type", t, "Name", obj.GetName())
		}
	}
	return nil
}

func (reconciler *ReconcilerLoop) CloneOfDeployed(kind reflect.Type, name string) client.Object {
	obj := reconciler.GetFromDeployed(kind, name)
	if obj != nil {
		return obj.DeepCopyObject().(client.Object)
	}
	return nil
}
func (reconciler *ReconcilerLoop) GetFromDeployed(kind reflect.Type, name string) client.Object {
	for _, obj := range reconciler.deployed[kind] {
		if obj.GetName() == name {
			return obj
		}
	}
	return nil
}
func (reconciler *ReconcilerLoop) TrackDesired(desired client.Object) {
	desiredType := reflect.TypeOf(desired)
	if reconciler.desired == nil {
		reconciler.desired = make(map[reflect.Type]map[string]client.Object)
	}
	resMap, ok := reconciler.desired[desiredType]
	if !ok {
		resMap = make(map[string]client.Object)
		reconciler.desired[desiredType] = resMap
	}
	resName := desired.GetName()
	resMap[resName] = desired
}

func (reconciler *ReconcilerLoop) SyncDesiredWithDeployed(owner client.Object) error {

	requested := compare.NewMapBuilder().Add(common.ToResourceList(reconciler.desired)...).ResourceMap()

	reconciler.log.V(1).Info("Processing resources", "num requested", reconciler.CountOfRequested(), "num current", reconciler.CountOfDeployed())

	comparator := compare.MapComparator{
		Comparator: compare.SimpleComparator(),
	}
	comparator.Comparator.SetDefaultComparator(reconciler.CompareMetaAndSpec)
	comparator.Comparator.SetComparator(reflect.TypeOf(corev1.Secret{}), reconciler.CompareSecret)
	comparator.Comparator.SetComparator(reflect.TypeOf(corev1.ConfigMap{}), reconciler.CompareConfigMap)

	var compositeError []error
	deltas := comparator.Compare(reconciler.deployed, requested)
	for _, resourceType := range reconciler.getOrderedTypeList() {
		delta, ok := deltas[resourceType]
		if !ok {
			// not all types will have deltas
			continue
		}
		reconciler.log.V(1).Info("", "instances of ", resourceType, "Will create ", len(delta.Added), "update ", len(delta.Updated), "and delete", len(delta.Removed))

		for index := range delta.Added {
			resourceToAdd := delta.Added[index]
			trackError(&compositeError, reconciler.CreateRequestedResource(owner, reconciler.Scheme, resourceToAdd, resourceType))
		}
		for index := range delta.Updated {
			resourceToUpdate := delta.Updated[index]
			trackError(&compositeError, reconciler.UpdateRequestedResource(resourceToUpdate, resourceType))
		}
		for index := range delta.Removed {
			resourceToRemove := delta.Removed[index]
			trackError(&compositeError, reconciler.DeleteRequestedResource(resourceToRemove, resourceType))
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

func (reconciler *ReconcilerLoop) CreateRequestedResource(owner client.Object, scheme *runtime.Scheme, requested client.Object, kind reflect.Type) error {
	reconciler.log.V(1).Info("Creating ", "kind ", kind, "named ", requested.GetName())
	return resources.Create(owner, reconciler.Client, scheme, requested)
}

func (reconciler *ReconcilerLoop) UpdateRequestedResource(requested client.Object, kind reflect.Type) error {
	var updateError error
	if updateError = resources.Update(reconciler.Client, requested); updateError == nil {
		reconciler.log.V(2).Info("updated", "kind ", kind, "named ", requested.GetName())
	} else {
		reconciler.log.V(0).Info("updated Failed", "kind ", kind, "named ", requested.GetName(), "error ", updateError)
	}
	return updateError
}

func (reconciler *ReconcilerLoop) DeleteRequestedResource(requested client.Object, kind reflect.Type) error {

	var deleteError error
	if deleteError := resources.Delete(reconciler.Client, requested); deleteError == nil {
		reconciler.log.V(2).Info("deleted", "kind", kind, " named ", requested.GetName())
	} else {
		reconciler.log.V(0).Error(deleteError, "delete Failed", "kind", kind, " named ", requested.GetName())
	}
	return deleteError
}

func (reconciler *ReconcilerLoop) CountOfRequested() (total int) {
	for _, v := range reconciler.desired {
		total += len(v)
	}
	return total
}

func (reconciler *ReconcilerLoop) CountOfDeployed() (total int) {
	for _, v := range reconciler.deployed {
		total += len(v)
	}
	return total
}

func (reconciler *ReconcilerLoop) CompareMetaAndSpec(deployed, requested client.Object) bool {

	isEqual := equalObjectMeta(deployed, requested) &&
		equality.Semantic.DeepEqual(specOf(deployed), specOf(requested))
	if !isEqual {
		reconciler.log.V(2).Info("unequal", "deployed", &deployed, "requested", &requested)
	}
	return isEqual
}

func (reconciler *ReconcilerLoop) CompareSecret(deployed, requested client.Object) bool {

	isEqual := equalObjectMeta(deployed, requested)
	if isEqual {
		deployedSecret := deployed.(*corev1.Secret)
		requestedSecret := requested.(*corev1.Secret)
		var pairs [][2]interface{}
		deployedData := deployedSecret.Data
		if len(deployedData) == 0 {
			deployedData = nil
		}
		requestedData := requestedSecret.Data
		if len(requestedData) == 0 {
			requestedData = nil
		}
		pairs = append(pairs, [2]interface{}{deployedData, requestedData})
		isEqual = compare.EqualPairs(pairs)
	}

	if !isEqual {
		reconciler.log.V(2).Info("unequal secret", "deployed", deployed, "requested", requested)
	}
	return isEqual
}

func (reconciler *ReconcilerLoop) CompareConfigMap(deployed, requested client.Object) bool {
	// our single configMap is immutable, the name indicates a change
	return deployed.GetName() == requested.GetName()
}
