package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"sync"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestEnvironment holds common test setup
type TestEnvironment struct {
	Scheme     *runtime.Scheme
	Client     client.Client
	Reconciler *BrokerAppReconciler
	Namespace  string
	Logger     logr.Logger
	Objects    []client.Object
}

// SetupBrokerAppIndexer adds the status.service field indexer to avoid duplication in tests
func SetupBrokerAppIndexer(builder *fake.ClientBuilder) *fake.ClientBuilder {
	return builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
		app := obj.(*v1beta2.BrokerApp)
		if app.Status.Service != nil {
			return []string{app.Status.Service.Key()}
		}
		return nil
	})
}

// NewTestEnvironment creates a complete test environment with scheme, client, and reconciler
func NewTestEnvironment(namespace string, objects ...client.Object) *TestEnvironment {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Add namespace object if not already included
	hasNamespace := false
	for _, obj := range objects {
		if ns, ok := obj.(*corev1.Namespace); ok && ns.Name == namespace {
			hasNamespace = true
			break
		}
	}
	if !hasNamespace && namespace != "" {
		objects = append([]client.Object{&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}}, objects...)
	}

	objects = WithCerts(objects...)

	// Separate status-enabled resources
	var statusObjs []client.Object
	for _, obj := range objects {
		switch obj.(type) {
		case *v1beta2.BrokerApp, *v1beta2.BrokerService:
			statusObjs = append(statusObjs, obj)
		}
	}

	cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(statusObjs...)).
		Build()

	logger := logr.New(log.NullLogSink{})
	reconciler := NewBrokerAppReconciler(cl, scheme, nil, logger)

	return &TestEnvironment{
		Scheme:     scheme,
		Client:     cl,
		Reconciler: reconciler,
		Namespace:  namespace,
		Logger:     logger,
		Objects:    objects,
	}
}

// BrokerServiceBuilder builds BrokerService test fixtures
type BrokerServiceBuilder struct {
	service *v1beta2.BrokerService
}

// NewBrokerService creates a new BrokerService builder with sensible defaults
func NewBrokerService(name, namespace string) *BrokerServiceBuilder {
	return &BrokerServiceBuilder{
		service: &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{"type": "broker"},
			},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ReadyConditionReason,
					},
				},
			},
		},
	}
}

func (b *BrokerServiceBuilder) WithLabels(labels map[string]string) *BrokerServiceBuilder {
	b.service.Labels = labels
	return b
}

func (b *BrokerServiceBuilder) WithMemoryLimit(memory string) *BrokerServiceBuilder {
	if b.service.Spec.Resources.Limits == nil {
		b.service.Spec.Resources.Limits = corev1.ResourceList{}
	}
	b.service.Spec.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(memory)
	return b
}

func (b *BrokerServiceBuilder) WithProvisionedApp(appIdentity string) *BrokerServiceBuilder {
	b.service.Status.ProvisionedApps = append(b.service.Status.ProvisionedApps, appIdentity)
	return b
}

func (b *BrokerServiceBuilder) WithDeployedCondition(status metav1.ConditionStatus, reason string) *BrokerServiceBuilder {
	for i := range b.service.Status.Conditions {
		if b.service.Status.Conditions[i].Type == v1beta2.DeployedConditionType {
			b.service.Status.Conditions[i].Status = status
			b.service.Status.Conditions[i].Reason = reason
			return b
		}
	}
	b.service.Status.Conditions = append(b.service.Status.Conditions, metav1.Condition{
		Type:   v1beta2.DeployedConditionType,
		Status: status,
		Reason: reason,
	})
	return b
}

func (b *BrokerServiceBuilder) Build() *v1beta2.BrokerService {
	return b.service
}

// BrokerAppBuilder builds BrokerApp test fixtures
type BrokerAppBuilder struct {
	app *v1beta2.BrokerApp
}

// NewBrokerApp creates a new BrokerApp builder with sensible defaults
func NewBrokerApp(name, namespace string) *BrokerAppBuilder {
	return &BrokerAppBuilder{
		app: &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		},
	}
}

func (b *BrokerAppBuilder) WithServiceSelector(selector *metav1.LabelSelector) *BrokerAppBuilder {
	b.app.Spec.ServiceSelector = selector
	return b
}

func (b *BrokerAppBuilder) WithConsumerOf(addresses ...v1beta2.AddressRef) *BrokerAppBuilder {
	if len(b.app.Spec.Capabilities) == 0 {
		b.app.Spec.Capabilities = []v1beta2.AppCapabilityType{{}}
	}
	b.app.Spec.Capabilities[0].ConsumerOf = append(b.app.Spec.Capabilities[0].ConsumerOf, addresses...)
	return b
}

func (b *BrokerAppBuilder) WithProducerOf(addresses ...v1beta2.AddressRef) *BrokerAppBuilder {
	if len(b.app.Spec.Capabilities) == 0 {
		b.app.Spec.Capabilities = []v1beta2.AppCapabilityType{{}}
	}
	b.app.Spec.Capabilities[0].ProducerOf = append(b.app.Spec.Capabilities[0].ProducerOf, addresses...)
	return b
}

func (b *BrokerAppBuilder) WithSharedAddresses(addresses ...v1beta2.AddressType) *BrokerAppBuilder {
	b.app.Spec.SharedAddresses = append(b.app.Spec.SharedAddresses, addresses...)
	return b
}

func (b *BrokerAppBuilder) WithAddresses(addresses ...v1beta2.AddressType) *BrokerAppBuilder {
	b.app.Spec.Addresses = append(b.app.Spec.Addresses, addresses...)
	return b
}

func (b *BrokerAppBuilder) WithMemoryRequest(memory string) *BrokerAppBuilder {
	if b.app.Spec.Resources.Requests == nil {
		b.app.Spec.Resources.Requests = corev1.ResourceList{}
	}
	b.app.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(memory)
	return b
}

func (b *BrokerAppBuilder) WithServiceBinding(name, namespace, secret string, port int32) *BrokerAppBuilder {
	b.app.Status.Service = &v1beta2.BrokerServiceBindingStatus{
		Name:         name,
		Namespace:    namespace,
		Secret:       secret,
		AssignedPort: port,
	}
	return b
}

func (b *BrokerAppBuilder) WithCapabilities(capabilities ...v1beta2.AppCapabilityType) *BrokerAppBuilder {
	b.app.Spec.Capabilities = capabilities
	return b
}

func (b *BrokerAppBuilder) Build() *v1beta2.BrokerApp {
	return b.app
}

// AddressRefBuilder builds AddressRef test fixtures
type AddressRefBuilder struct {
	ref v1beta2.AddressRef
}

// NewAddressRef creates a new AddressRef builder
func NewAddressRef(address string) *AddressRefBuilder {
	return &AddressRefBuilder{
		ref: v1beta2.AddressRef{
			Address: address,
		},
	}
}

func (a *AddressRefBuilder) WithSubscriptions(subs ...string) *AddressRefBuilder {
	a.ref.Subscriptions = subs
	return a
}

func (a *AddressRefBuilder) WithAppRef(namespace, name string) *AddressRefBuilder {
	a.ref.AppNamespace = namespace
	a.ref.AppName = name
	return a
}

func (a *AddressRefBuilder) Build() v1beta2.AddressRef {
	return a.ref
}

// AddressTypeBuilder builds AddressType test fixtures
type AddressTypeBuilder struct {
	addrType v1beta2.AddressType
}

// NewAddressType creates a new AddressType builder
func NewAddressType(address string) *AddressTypeBuilder {
	return &AddressTypeBuilder{
		addrType: v1beta2.AddressType{
			Address: address,
		},
	}
}

func (a *AddressTypeBuilder) WithSubscriptions(subs ...string) *AddressTypeBuilder {
	a.addrType.Subscriptions = subs
	return a
}

func (a *AddressTypeBuilder) WithPubSub(pubSub bool) *AddressTypeBuilder {
	a.addrType.PubSub = &pubSub
	return a
}

func (a *AddressTypeBuilder) Build() v1beta2.AddressType {
	return a.addrType
}

// CreateNamespace creates a test namespace object
func CreateNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

// CreateSecret creates a test secret
func CreateSecret(name, namespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: make(map[string][]byte),
	}
}

// BrokerServiceInstanceReconcilerForTest creates a reconciler for testing processCapabilities
func BrokerServiceInstanceReconcilerForTest() *BrokerServiceInstanceReconciler {
	return &BrokerServiceInstanceReconciler{}
}

// testSigningKey is generated once; RSA keygen dominates the cost of building
// these fixtures, while issuing a cert from an existing key is cheap.
var testSigningKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

// NewAppCertSecret builds the client certificate an app is expected to have,
// following the <app>-app-cert convention, with the app name as its CN.
func NewAppCertSecret(appName, namespace string) *corev1.Secret {
	return NewCertSecret(appName+common.AppCertSecretSuffix, namespace, appName)
}

// NewCertSecret builds a self-signed key pair in a secret, for fixtures that
// need a certificate the operator can read a subject from.
func NewCertSecret(secretName, namespace, commonName string) *corev1.Secret {
	key := testSigningKey()

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"tls.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			"tls.key": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
		},
	}
}

// WithCerts gives the fixture the certificates the operator expects to exist:
// an <app>-app-cert for every BrokerApp, and for every BrokerService the
// operator and operand certs the control-plane identities are resolved from.
// Neither an app nor a service reconciles without them, so fixtures that build
// a client directly pass their objects through here. Anything the test supplied
// itself is left alone.
func WithCerts(objects ...client.Object) []client.Object {
	existing := make(map[string]bool)
	for _, obj := range objects {
		if secret, ok := obj.(*corev1.Secret); ok {
			existing[secret.Namespace+"/"+secret.Name] = true
		}
	}

	add := func(name, namespace, commonName string) {
		if key := namespace + "/" + name; !existing[key] {
			secret := NewCertSecret(name, namespace, commonName)
			objects = append(objects, secret)
			existing[key] = true
		}
	}

	for _, obj := range objects {
		switch res := obj.(type) {
		case *v1beta2.BrokerApp:
			add(res.Name+common.AppCertSecretSuffix, res.Namespace, res.Name)
		case *v1beta2.BrokerService:
			// stand in for a cluster where the operator runs alongside the
			// service; a test that sets the namespace itself keeps its value,
			// and the cert is added to whichever namespace resolves.
			if _, err := common.GetOperatorNamespaceFromEnv(); err != nil {
				common.SetOperatorNameSpace(res.Namespace)
			}
			if operatorNs, err := common.GetOperatorNamespaceFromEnv(); err == nil {
				add(common.GetOperatorCertSecretName(), operatorNs, "operator")
			}
			add(common.GetOperatorCertSecretName(), res.Namespace, "operator")
			add(res.Name+"-"+common.DefaultOperandCertSecretName, res.Namespace, res.Name)
		}
	}
	return objects
}
