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

package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegatypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	namespace = "ns"
	issuer    = "the-issuer"
)

var _ = Describe("certificates issued from a cluster issuer", func() {
	var (
		ctx     context.Context
		scheme  *runtime.Scheme
		c       client.Client
		owner   *corev1.ConfigMap
		request Request
		key     types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(cmv1.AddToScheme(scheme)).To(Succeed())
		c = fake.NewClientBuilder().WithScheme(scheme).Build()

		owner = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: namespace, UID: "owner-uid"}}
		request = Request{Name: "owner-broker-cert", Namespace: namespace, Owner: owner, DNSNames: []string{"owner.ns"}}
		key = types.NamespacedName{Name: request.Name, Namespace: namespace}
	})

	requested := func() *cmv1.Certificate {
		Expect(Ensure(ctx, c, scheme, issuer, request)).To(BeAssignableToTypeOf(&NotReadyError{}))
		certificate := &cmv1.Certificate{}
		Expect(c.Get(ctx, key, certificate)).To(Succeed())
		return certificate
	}

	// issue does what cert-manager does: writes its keys into the secret, under
	// its own field manager
	issue := func(certificate *cmv1.Certificate, commonName string) {
		certPEM, keyPEM := keyPair(commonName)
		secret := corev1ac.Secret(certificate.Spec.SecretName, namespace).
			WithAnnotations(map[string]string{cmv1.CertificateNameKey: certificate.Name}).
			WithData(map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM})
		Expect(c.Apply(ctx, secret, client.FieldOwner("cert-manager-certificates-issuing"), client.ForceOwnership)).To(Succeed())
	}

	secretOf := func(certificate *cmv1.Certificate) *corev1.Secret {
		secret := &corev1.Secret{}
		Expect(c.Get(ctx, types.NamespacedName{Name: certificate.Spec.SecretName, Namespace: namespace}, secret)).To(Succeed())
		return secret
	}

	ownedByCertificate := func(certificate *cmv1.Certificate) gomegatypes.GomegaMatcher {
		return ContainElement(SatisfyAll(
			HaveField("Kind", cmv1.CertificateKind),
			HaveField("Name", certificate.Name),
			HaveField("UID", certificate.UID),
			HaveField("Controller", Or(BeNil(), Equal(ptr.To(false)))),
		))
	}

	Context("Ensure", func() {
		It("requests a certificate the owner controls, with an unguessable common name", func() {
			certificate := requested()
			Expect(certificate.Spec.SecretName).To(Equal(request.Name))
			Expect(certificate.Spec.DNSNames).To(Equal(request.DNSNames))
			Expect(certificate.Spec.IssuerRef).To(Equal(cmmetav1.ObjectReference{Name: issuer, Kind: "ClusterIssuer", Group: "cert-manager.io"}))
			Expect(metav1.IsControlledBy(certificate, owner)).To(BeTrue())
			Expect(certificate.OwnerReferences).To(ConsistOf(HaveField("BlockOwnerDeletion", Equal(ptr.To(false)))))
			_, err := uuid.Parse(certificate.Spec.CommonName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("creates the secret owned by its certificate, so that it goes with it", func() {
			certificate := requested()
			Expect(secretOf(certificate).OwnerReferences).To(ownedByCertificate(certificate))
		})

		It("gives each certificate its own common name", func() {
			other := Request{Name: "other-cert", Namespace: namespace, Owner: owner}
			Expect(Ensure(ctx, c, scheme, issuer, request)).NotTo(Succeed())
			Expect(Ensure(ctx, c, scheme, issuer, other)).NotTo(Succeed())

			first, second := &cmv1.Certificate{}, &cmv1.Certificate{}
			Expect(c.Get(ctx, key, first)).To(Succeed())
			Expect(c.Get(ctx, types.NamespacedName{Name: other.Name, Namespace: namespace}, second)).To(Succeed())
			Expect(first.Spec.CommonName).NotTo(Equal(second.Spec.CommonName))
		})

		It("keeps the common name it first requested", func() {
			before := requested()
			after := requested()
			Expect(after.Spec.CommonName).To(Equal(before.Spec.CommonName))
		})

		It("succeeds once the secret holds the certificate", func() {
			certificate := requested()
			issue(certificate, certificate.Spec.CommonName)

			Expect(Ensure(ctx, c, scheme, issuer, request)).To(Succeed())
		})

		It("waits while the secret holds a certificate of another common name", func() {
			certificate := requested()
			issue(certificate, "left-by-a-cr-of-the-same-name")

			Expect(Ensure(ctx, c, scheme, issuer, request)).To(BeAssignableToTypeOf(&NotReadyError{}))
		})

		It("issues into a secret already there, leaving its other keys alone", func() {
			user := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: namespace},
				Data:       map[string][]byte{"tls.crt": []byte("user"), "host": []byte("h")},
			}
			Expect(c.Create(ctx, user)).To(Succeed())

			certificate := requested()
			issue(certificate, certificate.Spec.CommonName)
			Expect(Ensure(ctx, c, scheme, issuer, request)).To(Succeed())

			secret := secretOf(certificate)
			Expect(secret.OwnerReferences).To(ownedByCertificate(certificate))
			Expect(secret.Data).To(HaveKeyWithValue("host", []byte("h")))
		})

		It("owns a secret cert-manager created before it did", func() {
			certificate := requested()
			Expect(c.Delete(ctx, secretOf(certificate))).To(Succeed())
			issue(certificate, certificate.Spec.CommonName)
			Expect(secretOf(certificate).OwnerReferences).To(BeEmpty())

			Expect(Ensure(ctx, c, scheme, issuer, request)).To(Succeed())
			Expect(secretOf(certificate).OwnerReferences).To(ownedByCertificate(certificate))
		})

		It("refuses a certificate the user created under that name", func() {
			user := &cmv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: namespace}}
			Expect(c.Create(ctx, user)).To(Succeed())

			var conflict *ConflictError
			Expect(Ensure(ctx, c, scheme, issuer, request)).To(BeAssignableToTypeOf(conflict))
			Expect(errors.IsNotFound(c.Get(ctx, key, &corev1.Secret{}))).To(BeTrue())
		})

		It("recognises its own unowned certificate by label, and only that", func() {
			operator := Request{Name: "operator-cert", Namespace: namespace}
			Expect(Ensure(ctx, c, scheme, issuer, operator)).NotTo(Succeed())
			Expect(Ensure(ctx, c, scheme, issuer, operator)).To(BeAssignableToTypeOf(&NotReadyError{}))

			user := Request{Name: "user-operator-cert", Namespace: namespace}
			Expect(c.Create(ctx, &cmv1.Certificate{ObjectMeta: metav1.ObjectMeta{Name: user.Name, Namespace: namespace}})).To(Succeed())
			Expect(Ensure(ctx, c, scheme, issuer, user)).To(BeAssignableToTypeOf(&ConflictError{}))
		})

		It("reports why the latest request of the certificate is not issued", func() {
			certificate := requested()

			request := func(name string, age time.Duration, message string) *cmv1.CertificateRequest {
				cr := &cmv1.CertificateRequest{ObjectMeta: metav1.ObjectMeta{
					Name:              name,
					Namespace:         namespace,
					CreationTimestamp: metav1.NewTime(time.Now().Add(-age)),
				}}
				Expect(controllerutil.SetControllerReference(certificate, cr, scheme)).To(Succeed())
				cr.Status.Conditions = []cmv1.CertificateRequestCondition{{
					Type: cmv1.CertificateRequestConditionReady, Status: cmmetav1.ConditionFalse, Reason: "Pending", Message: message,
				}}
				return cr
			}
			Expect(c.Create(ctx, request("older", time.Hour, "stale"))).To(Succeed())
			Expect(c.Create(ctx, request("latest", time.Minute, `Referenced "ClusterIssuer" not found: clusterissuer.cert-manager.io "the-issuer" not found`))).To(Succeed())

			err := Ensure(ctx, c, scheme, issuer, Request{Name: key.Name, Namespace: namespace, Owner: owner})
			Expect(err).To(BeAssignableToTypeOf(&NotReadyError{}))
			Expect(err.Error()).To(ContainSubstring(`"the-issuer" not found`))
			Expect(err.Error()).NotTo(ContainSubstring("stale"))
		})
	})
})

func keyPair(commonName string) (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}
