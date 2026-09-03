package chainoftrust

import (
	"context"
	"strings"
	"testing"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOwnerRef(t *testing.T) {
	owner := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-app",
			UID:  "test-uid-123",
		},
	}
	gvk := metav1.GroupVersionKind{
		Group:   "broker.arkmq.org",
		Version: "v1beta2",
		Kind:    "BrokerApp",
	}

	ref := ownerRef(owner, gvk)

	if ref.Name != "my-app" {
		t.Errorf("name = %q", ref.Name)
	}
	if ref.Kind != "BrokerApp" {
		t.Errorf("kind = %q", ref.Kind)
	}
	if ref.APIVersion != "broker.arkmq.org/v1beta2" {
		t.Errorf("apiVersion = %q", ref.APIVersion)
	}
	if ref.UID != "test-uid-123" {
		t.Errorf("uid = %q", ref.UID)
	}
	if ref.Controller == nil || !*ref.Controller {
		t.Error("Controller must be true")
	}
}

func TestManagedLabels(t *testing.T) {
	labels := managedLabels("svc")
	if labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("managed-by = %q", labels[LabelManagedBy])
	}
}

func TestCATrustSecretOmitsPrivateKey(t *testing.T) {
	name := CATrustSecretName("my-svc")
	if name != "my-svc-ca-trust" {
		t.Errorf("name = %q", name)
	}
}

func testScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = cmv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	return s
}

func TestDetectPKIClash_NoResources(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(testScheme()).Build()
	clashes := DetectPKIClash(context.Background(), cl, "my-svc", "test")
	if len(clashes) != 0 {
		t.Errorf("expected no clashes, got %v", clashes)
	}
}

func TestDetectPKIClash_ManagedResourcesAreIgnored(t *testing.T) {
	cert := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RootCertName("my-svc"),
			Namespace: "test",
			Labels:    map[string]string{LabelManagedBy: ManagedByValue},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(cert).Build()
	clashes := DetectPKIClash(context.Background(), cl, "my-svc", "test")
	if len(clashes) != 0 {
		t.Errorf("expected no clashes for managed resource, got %v", clashes)
	}
}

func TestDetectPKIClash_UnmanagedCertificateDetected(t *testing.T) {
	cert := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BrokerCertName("my-svc"),
			Namespace: "test",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(cert).Build()
	clashes := DetectPKIClash(context.Background(), cl, "my-svc", "test")
	if len(clashes) != 1 || clashes[0] != "Certificate/"+BrokerCertName("my-svc") {
		t.Errorf("expected clash on broker cert, got %v", clashes)
	}
}

func TestDetectPKIClash_UnmanagedIssuerDetected(t *testing.T) {
	issuer := &cmv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CAIssuerName("my-svc"),
			Namespace: "test",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(issuer).Build()
	clashes := DetectPKIClash(context.Background(), cl, "my-svc", "test")
	if len(clashes) != 1 || clashes[0] != "Issuer/"+CAIssuerName("my-svc") {
		t.Errorf("expected clash on CA issuer, got %v", clashes)
	}
}

func TestDetectPKIClash_WrongLabelValue(t *testing.T) {
	cert := &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      RootCertName("my-svc"),
			Namespace: "test",
			Labels:    map[string]string{LabelManagedBy: "something-else"},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(testScheme()).WithObjects(cert).Build()
	clashes := DetectPKIClash(context.Background(), cl, "my-svc", "test")
	if len(clashes) != 1 {
		t.Errorf("expected clash for wrong label value, got %v", clashes)
	}
}

func TestFormatPKIClashMessage(t *testing.T) {
	msg := FormatPKIClashMessage([]string{"Certificate/foo", "Issuer/bar"})
	if msg == "" {
		t.Error("expected non-empty message")
	}
	if !strings.Contains(msg, "Certificate/foo") || !strings.Contains(msg, "Issuer/bar") {
		t.Errorf("message missing clash details: %s", msg)
	}
}
