package chainoftrust

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CopyTLSSecret(ctx context.Context, cl client.Client, sourceKey, destKey types.NamespacedName, owner metav1.Object, ownerGVK metav1.GroupVersionKind) error {
	source := &corev1.Secret{}
	if err := cl.Get(ctx, sourceKey, source); err != nil {
		return fmt.Errorf("failed to get source secret %s: %w", sourceKey, err)
	}

	desired := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      destKey.Name,
			Namespace: destKey.Namespace,
			Labels:    managedLabels(""),
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": source.Data["tls.crt"],
			"tls.key": source.Data["tls.key"],
		},
	}

	if owner != nil {
		desired.OwnerReferences = []metav1.OwnerReference{
			ownerRef(owner, ownerGVK),
		}
	}

	return createOrUpdateSecret(ctx, cl, desired)
}

func CopyCATrust(ctx context.Context, cl client.Client, rootCertSecretKey types.NamespacedName, destKey types.NamespacedName, owner metav1.Object, ownerGVK metav1.GroupVersionKind) error {
	source := &corev1.Secret{}
	if err := cl.Get(ctx, rootCertSecretKey, source); err != nil {
		return fmt.Errorf("failed to get CA secret %s: %w", rootCertSecretKey, err)
	}

	caCrt, ok := source.Data["tls.crt"]
	if !ok {
		return fmt.Errorf("CA secret %s missing tls.crt key", rootCertSecretKey)
	}

	desired := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      destKey.Name,
			Namespace: destKey.Namespace,
			Labels:    managedLabels(""),
			OwnerReferences: []metav1.OwnerReference{
				ownerRef(owner, ownerGVK),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"tls.crt": caCrt,
		},
	}

	return createOrUpdateSecret(ctx, cl, desired)
}

func DeleteSecret(ctx context.Context, cl client.Client, key types.NamespacedName) error {
	secret := &corev1.Secret{}
	if err := cl.Get(ctx, key, secret); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return cl.Delete(ctx, secret)
}

func createOrUpdateSecret(ctx context.Context, cl client.Client, desired *corev1.Secret) error {
	key := types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}
	existing := &corev1.Secret{}
	err := cl.Get(ctx, key, existing)
	if errors.IsNotFound(err) {
		if createErr := cl.Create(ctx, desired); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				return nil
			}
			return createErr
		}
		return nil
	}
	if err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fresh := &corev1.Secret{}
		if err := cl.Get(ctx, key, fresh); err != nil {
			return err
		}
		fresh.Data = desired.Data
		fresh.Labels = desired.Labels
		fresh.Type = desired.Type
		return cl.Update(ctx, fresh)
	})
}

func ownerRef(owner metav1.Object, gvk metav1.GroupVersionKind) metav1.OwnerReference {
	isController := true
	return metav1.OwnerReference{
		APIVersion: gvk.Group + "/" + gvk.Version,
		Kind:       gvk.Kind,
		Name:       owner.GetName(),
		UID:        owner.GetUID(),
		Controller: &isController,
	}
}
