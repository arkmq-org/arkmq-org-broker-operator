// Package brokervolumes provides shared volume and volume-mount definitions for Broker and BrokerCluster StatefulSets.
package brokervolumes

import (
	"context"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	resvolumes "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/volumes"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const DataMountPath = "/app"

// No emptyDir fallback when not persistent — callers that need one must add it explicitly.
func MakeVolumes(name string, persistent bool, extras []corev1.Volume, epvcs []v1beta2.VolumeClaimTemplate) []corev1.Volume {
	volumeDefinitions := []corev1.Volume{}
	if persistent {
		basicCRVolume := resvolumes.MakePersistentVolume(name)
		volumeDefinitions = append(volumeDefinitions, basicCRVolume...)
	}

	volumeDefinitions = append(volumeDefinitions, extras...)

	for _, epvc := range epvcs {
		epvcVolume := resvolumes.MakePersistentVolume(epvc.Name)
		volumeDefinitions = append(volumeDefinitions, epvcVolume...)
	}

	return volumeDefinitions
}

func MakeExtraVolumeMounts(extras []corev1.Volume, mounts []corev1.VolumeMount, epvcs []v1beta2.VolumeClaimTemplate) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{}

	for _, volume := range extras {
		var volumeMount corev1.VolumeMount
		found := false
		for _, vm := range mounts {
			if vm.Name == volume.Name {
				volumeMount = vm
				if volumeMount.MountPath == "" {
					volumeMount.MountPath = resvolumes.GetDefaultMountPath(&volume)
				}
				found = true
				break
			}
		}
		if !found {
			volumeMount = *resvolumes.MakeVolumeMountForVolume(&volume)
		}
		volumeMounts = append(volumeMounts, volumeMount)
	}

	for _, epvc := range epvcs {
		var vMount corev1.VolumeMount
		found := false
		for _, mount := range mounts {
			if epvc.Name == mount.Name {
				vMount = mount
				found = true
				break
			}
		}
		if !found {
			vMount = *resvolumes.NewVolumeMountForPVC(epvc.Name)
		}
		volumeMounts = append(volumeMounts, vMount)
	}

	return volumeMounts
}

func MakeVolumeMounts(name string, extras []corev1.Volume, mounts []corev1.VolumeMount, epvcs []v1beta2.VolumeClaimTemplate) []corev1.VolumeMount {
	volumeMounts := resvolumes.MakePersistentVolumeMount(name, DataMountPath)
	volumeMounts = append(volumeMounts, MakeExtraVolumeMounts(extras, mounts, epvcs)...)
	return volumeMounts
}

func RemovePVCOwnerRef(pvcKey types.NamespacedName, uid types.UID, client rtclient.Client, log logr.Logger) {
	pvc := &corev1.PersistentVolumeClaim{}
	err := client.Get(context.TODO(), pvcKey, pvc)

	if err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "got error in getting pvc")
		}
		return
	}

	if len(pvc.OwnerReferences) == 0 {
		return
	}

	found := false
	newOwnerReferences := make([]metav1.OwnerReference, 0)
	for _, oref := range pvc.OwnerReferences {
		if oref.UID == uid {
			found = true
		} else {
			newOwnerReferences = append(newOwnerReferences, oref)
		}
	}
	if found {
		log.V(1).Info("removing owner ref from pvc to avoid potential data loss")
		pvc.OwnerReferences = newOwnerReferences
		if er := client.Update(context.TODO(), pvc); er != nil {
			log.Error(er, "failed to remove ownerReference from pvc", "pvc", *pvc)
		}
	}
}
