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

// Package pki issues the certificates the operator needs from the ClusterIssuer
// it was given at install time.
//
// Each certificate is a cert-manager Certificate the operator created. A
// Certificate under that name the operator did not create is a conflict. The
// secret it is written into is not: cert-manager writes its own keys into it,
// whoever created it, and leaves any other key alone. The secret is owned by its
// Certificate, so that it goes with it.
package pki

import (
	"context"
	"fmt"
	"sort"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

// FieldManager owns the reference from a secret to its Certificate, apart from
// whatever else writes into the secret.
const FieldManager = common.OperatorName + "-pki"

// Request describes a certificate to issue.
type Request struct {
	Name string
	// SecretName is the secret the certificate is written into, Name when empty.
	SecretName string
	Namespace  string
	// Owner of the Certificate, nil for one that outlives every CR, such as the
	// operator's own.
	Owner    client.Object
	DNSNames []string
}

func (r Request) key() types.NamespacedName {
	return types.NamespacedName{Name: r.Name, Namespace: r.Namespace}
}

func (r Request) secretKey() types.NamespacedName {
	if r.SecretName == "" {
		return r.key()
	}
	return types.NamespacedName{Name: r.SecretName, Namespace: r.Namespace}
}

// ConflictError reports a resource under the name of a certificate to issue
// that the operator did not create.
type ConflictError struct {
	Kind string
	Key  types.NamespacedName
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s %s was not issued by the operator: with %s set the operator issues every certificate, remove it or unset %s",
		e.Kind, e.Key, common.ClusterIssuerEnvVar, common.ClusterIssuerEnvVar)
}

// NotReadyError reports a certificate not issued into its secret yet, with
// cert-manager's account of why.
type NotReadyError struct {
	Key    types.NamespacedName
	Reason string
}

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("certificate %s is not issued yet: %s", e.Key, e.Reason)
}

// Ensure issues the certificate from issuer unless it already is. It returns
// nil once the secret holds it, a NotReadyError while it does not, and a
// ConflictError when the Certificate's name is taken by one the operator did not
// create.
//
// A secret holding it is good enough: renewals keep it in place, and a failing
// renewal is cert-manager's to report, not a reason to stop reconciling.
func Ensure(ctx context.Context, c client.Client, scheme *runtime.Scheme, issuer string, request Request) error {
	key := request.key()

	certificate := &cmv1.Certificate{}
	err := c.Get(ctx, key, certificate)
	if meta.IsNoMatchError(err) {
		return &NotReadyError{Key: key, Reason: "cert-manager is not installed"}
	}
	if errors.IsNotFound(err) {
		return create(ctx, c, scheme, issuer, request)
	}
	if err != nil {
		return err
	}

	if !issuedFor(certificate, request.Owner) {
		return &ConflictError{Kind: "Certificate", Key: key}
	}

	secret := &corev1.Secret{}
	err = c.Get(ctx, request.secretKey(), secret)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	if err != nil || !ownedBy(secret, certificate) {
		if err := own(ctx, c, certificate); err != nil {
			return err
		}
	}
	if err != nil || !holds(secret, certificate) {
		return &NotReadyError{Key: key, Reason: notReadyReason(ctx, c, certificate)}
	}
	return nil
}

func create(ctx context.Context, c client.Client, scheme *runtime.Scheme, issuer string, request Request) error {
	key := request.key()

	certificate := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      request.Name,
			Namespace: request.Namespace,
			Labels:    map[string]string{selectors.LabelAppKubernetesManagedBy: common.OperatorName},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: request.secretKey().Name,
			// the common name is the identity a broker authorises, unguessable so
			// that access to the issuer alone does not let one mint it
			CommonName: uuid.NewString(),
			DNSNames:   request.DNSNames,
			IssuerRef: cmmetav1.ObjectReference{
				Name:  issuer,
				Kind:  cmv1.ClusterIssuerKind,
				Group: cmv1.SchemeGroupVersion.Group,
			},
		},
	}
	if request.Owner != nil {
		// not blocking the owner's deletion, which would take update on its
		// finalizers wherever OwnerReferencesPermissionEnforcement is on
		if err := controllerutil.SetControllerReference(request.Owner, certificate, scheme, controllerutil.WithBlockOwnerDeletion(false)); err != nil {
			return err
		}
	}

	err := c.Create(ctx, certificate)
	if meta.IsNoMatchError(err) {
		return &NotReadyError{Key: key, Reason: "cert-manager is not installed"}
	}
	if errors.IsAlreadyExists(err) {
		return &NotReadyError{Key: key, Reason: "certificate requested"}
	}
	if err != nil {
		return err
	}
	// owned from the start, so that it goes with a Certificate deleted before
	// the next reconcile
	if err := own(ctx, c, certificate); err != nil {
		return err
	}
	return &NotReadyError{Key: key, Reason: "certificate requested"}
}

// own makes certificate an owner of its secret, creating the secret if need be.
// Not a controller reference: cert-manager sets its own when run with
// --enable-certificate-owner-ref, and a secret takes only one.
func own(ctx context.Context, c client.Client, certificate *cmv1.Certificate) error {
	secret := corev1ac.Secret(certificate.Spec.SecretName, certificate.Namespace).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion(cmv1.SchemeGroupVersion.String()).
			WithKind(cmv1.CertificateKind).
			WithName(certificate.Name).
			WithUID(certificate.UID))
	return c.Apply(ctx, secret, client.FieldOwner(FieldManager), client.ForceOwnership)
}

func ownedBy(secret *corev1.Secret, certificate *cmv1.Certificate) bool {
	for _, ref := range secret.OwnerReferences {
		if ref.UID == certificate.UID {
			return true
		}
	}
	return false
}

// holds tells whether secret holds what certificate issued. A secret can hold
// another certificate's under the same name, such as one left by a CR of the
// same name, until cert-manager issues into it again.
func holds(secret *corev1.Secret, certificate *cmv1.Certificate) bool {
	if secret.Annotations[cmv1.CertificateNameKey] != certificate.Name {
		return false
	}
	subject, err := common.ExtractCertSubjectFromSecret(secret)
	return err == nil && subject.CommonName == certificate.Spec.CommonName
}

// issuedFor tells whether the operator created certificate for owner. One with
// no owner is recognised by the operator's managed-by label.
func issuedFor(certificate *cmv1.Certificate, owner client.Object) bool {
	if owner == nil {
		return len(certificate.OwnerReferences) == 0 &&
			certificate.Labels[selectors.LabelAppKubernetesManagedBy] == common.OperatorName
	}
	return metav1.IsControlledBy(certificate, owner)
}

// notReadyReason is cert-manager's account of why certificate has no secret.
// The Certificate itself only says that it is issuing; the cause, such as a
// missing or unready issuer, is on its latest CertificateRequest.
func notReadyReason(ctx context.Context, c client.Client, certificate *cmv1.Certificate) string {
	requests := &cmv1.CertificateRequestList{}
	if err := c.List(ctx, requests, client.InNamespace(certificate.Namespace)); err == nil {
		owned := make([]cmv1.CertificateRequest, 0, len(requests.Items))
		for _, request := range requests.Items {
			if metav1.IsControlledBy(&request, certificate) {
				owned = append(owned, request)
			}
		}
		sort.Slice(owned, func(i, j int) bool {
			return owned[i].CreationTimestamp.After(owned[j].CreationTimestamp.Time)
		})
		for _, request := range owned {
			for _, condition := range request.Status.Conditions {
				if condition.Type == cmv1.CertificateRequestConditionReady && condition.Message != "" {
					return fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
				}
			}
		}
	}

	for _, condition := range certificate.Status.Conditions {
		if condition.Type == cmv1.CertificateConditionReady && condition.Message != "" {
			return fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
		}
	}
	return "certificate requested"
}
