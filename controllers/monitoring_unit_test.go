/*
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
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/monitoring"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

func testLogger() logr.Logger {
	return logr.New(log.NullLogSink{})
}

// The scrape wiring is only useful if a scraper can act on it verbatim, so these
// pin the values a scraper reads: where to connect, what name to verify, and
// which identity to present.

func TestServiceScrapeConfigTargetsTheBrokerPod(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	scrapeConfig := trackedServiceScrapeConfig(t, reconciler, monitoring.WiringName(testServiceName))

	// the pod is addressed directly, not through a service
	assert.Len(t, scrapeConfig.Spec.StaticConfigs, 1)
	targets := scrapeConfig.Spec.StaticConfigs[0].Targets
	assert.Len(t, targets, 1)
	assert.Equal(t,
		fmt.Sprintf("%s:%d", common.OrdinalFQDNS(testServiceName, serviceTestNamespace, 0), monitoring.Port),
		string(targets[0]))

	assert.Equal(t, "HTTPS", *scrapeConfig.Spec.Scheme)
	assert.Equal(t, monitoring.Path, *scrapeConfig.Spec.MetricsPath)
	assert.Equal(t, "svc-ns", scrapeConfig.Namespace)
}

func TestServiceScrapeConfigVerifiesTheWildcardCoveredPodName(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	tlsConfig := trackedServiceScrapeConfig(t, reconciler, monitoring.WiringName(testServiceName)).Spec.TLSConfig

	// The operand cert is issued for the service name and the hdls-svc wildcard.
	// The bare service DNS name is NOT covered, so using it here would fail the
	// handshake wherever the scrape actually lands.
	assert.Equal(t, common.OrdinalFQDNS(testServiceName, serviceTestNamespace, 0), tlsConfig.ServerName)
	assert.NotEqual(t, fmt.Sprintf("my-service.svc-ns.svc.%s", common.GetClusterDomain()), tlsConfig.ServerName)
}

func TestServiceScrapeConfigPresentsThePrometheusIdentity(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	tlsConfig := trackedServiceScrapeConfig(t, reconciler, monitoring.WiringName(testServiceName)).Spec.TLSConfig

	// the broad "metrics" role, so this scrape sees every app's queues, unlike an
	// app's own ScrapeConfig
	assert.Equal(t, common.DefaultPrometheusCertSecretName, tlsConfig.Cert.Secret.Name)
	assert.Equal(t, "tls.crt", tlsConfig.Cert.Secret.Key)
	assert.Equal(t, common.DefaultPrometheusCertSecretName, tlsConfig.KeySecret.Name)
	assert.Equal(t, "tls.key", tlsConfig.KeySecret.Key)

	assert.Equal(t, common.GetOperatorCASecretName(), tlsConfig.CA.Secret.Name)
}

func TestServiceScrapeConfigLabelsKeepEachServicesCopyDistinguishable(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	scrapeConfig := trackedServiceScrapeConfig(t, reconciler, monitoring.WiringName(testServiceName))

	labels := scrapeConfig.Spec.StaticConfigs[0].Labels
	assert.Equal(t, "my-service", labels["brokerservice"])
	assert.Equal(t, "svc-ns", labels["brokerservice_namespace"])
	// stable across the ServiceMonitor to ScrapeConfig move, so existing
	// dashboards and recording rules keep matching
	assert.Equal(t, "my-service-metrics", labels["job"])

	assert.Equal(t, "true", scrapeConfig.Labels[common.LabelMonitoring])
}

func TestNoServiceScrapeConfigWithoutAPrometheusCert(t *testing.T) {
	reconciler := serviceReconcilerFor()
	reconciler.Client = monitoringClient(true, withoutPrometheusCert)

	reconciler.processMonitoring()

	// there would be no identity to scrape as, and the broker would reject it
	assert.Nil(t, tracked(reconciler.ReconcilerLoop, &monitoringv1alpha1.ScrapeConfig{}, monitoring.WiringName(testServiceName)))
}

func TestAppScrapeConfigTargetsTheBrokerPodOfItsService(t *testing.T) {
	reconciler := appReconcilerFor()

	reconciler.processScrapeConfig()

	scrapeConfig := trackedScrapeConfig(t, reconciler, monitoring.WiringName(testAppName))

	// the broker pod of the service it is bound to, in that service's namespace,
	// not the messaging host from its binding secret
	expected := fmt.Sprintf("%s:%d", common.OrdinalFQDNS(testServiceName, serviceTestNamespace, 0), monitoring.Port)
	assert.Len(t, scrapeConfig.Spec.StaticConfigs, 1)
	assert.Len(t, scrapeConfig.Spec.StaticConfigs[0].Targets, 1)
	assert.Equal(t, expected, string(scrapeConfig.Spec.StaticConfigs[0].Targets[0]))

	// an app in another namespace reaches it by the same fully qualified name
	assert.Equal(t, "app-ns", scrapeConfig.Namespace)
	assert.Contains(t, expected, "svc-ns")

	assert.Equal(t, "HTTPS", *scrapeConfig.Spec.Scheme)
	assert.Equal(t, monitoring.Path, *scrapeConfig.Spec.MetricsPath)
}

func TestScrapeConfigPresentsTheAppsOwnIdentity(t *testing.T) {
	reconciler := appReconcilerFor()

	reconciler.processScrapeConfig()

	scrapeConfig := trackedScrapeConfig(t, reconciler, monitoring.WiringName(testAppName))

	// This is what scopes the scrape: the broker maps this cert's CN to the
	// app's metrics role. Referencing anything else would leak other apps' queues.
	assert.Equal(t, testAppName+common.AppCertSecretSuffix, scrapeConfig.Spec.TLSConfig.Cert.Secret.Name)
	assert.Equal(t, testAppName+common.AppCertSecretSuffix, scrapeConfig.Spec.TLSConfig.KeySecret.Name)

	// prometheus-operator resolves secret refs in the declaring object's
	// namespace, so the ScrapeConfig has to live with the app, not the service
	assert.Equal(t, "app-ns", scrapeConfig.Namespace)

	assert.Equal(t, common.OrdinalFQDNS(testServiceName, serviceTestNamespace, 0), scrapeConfig.Spec.TLSConfig.ServerName)
}

func TestScrapeConfigLabelsKeepEachAppsCopyDistinguishable(t *testing.T) {
	reconciler := appReconcilerFor()

	reconciler.processScrapeConfig()

	scrapeConfig := trackedScrapeConfig(t, reconciler, monitoring.WiringName(testAppName))

	// every app scrapes the same endpoint, so the series need telling apart
	labels := scrapeConfig.Spec.StaticConfigs[0].Labels
	assert.Equal(t, "my-app-metrics", labels["job"])
	assert.Equal(t, "my-app", labels["brokerapp"])
	assert.Equal(t, "app-ns", labels["brokerapp_namespace"])
	assert.Equal(t, "my-service", labels["brokerservice"])
	assert.Equal(t, "svc-ns", labels["brokerservice_namespace"])

	assert.Equal(t, "true", scrapeConfig.Labels[common.LabelMonitoring])
	assert.Equal(t, "my-app", scrapeConfig.Labels[common.LabelBrokerApp])
}

func TestNoScrapeConfigBeforeTheAppIsBound(t *testing.T) {
	reconciler := appReconcilerFor()
	reconciler.status.Service = nil

	reconciler.processScrapeConfig()

	// nothing to point a scraper at yet
	assert.Nil(t, tracked(reconciler.ReconcilerLoop, &monitoringv1alpha1.ScrapeConfig{}, monitoring.WiringName(testAppName)))
}

// withoutPrometheusCert drops the prometheus certificate from the fixture, for
// the cluster where nobody has issued one yet.
func withoutPrometheusCert(objects []client.Object) []client.Object {
	kept := make([]client.Object, 0, len(objects))
	for _, obj := range objects {
		if obj.GetName() == common.DefaultPrometheusCertSecretName {
			continue
		}
		kept = append(kept, obj)
	}
	return kept
}

// monitoringClient is a client whose RESTMapper reports the monitoring kinds as
// served, which is what the capability probe keys off. Passing served=false gives
// the cluster-without-prometheus-operator case.
func monitoringClient(served bool, adjust ...func([]client.Object) []client.Object) client.Client {
	monitoring.ResetAvailability()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1beta2.AddToScheme(scheme)
	_ = monitoringv1.AddToScheme(scheme)
	_ = monitoringv1alpha1.AddToScheme(scheme)

	mapper := meta.NewDefaultRESTMapper(nil)
	if served {
		mapper.Add(monitoringv1alpha1.SchemeGroupVersion.WithKind(monitoringv1alpha1.ScrapeConfigsKind), meta.RESTScopeNamespace)
	}

	// The operator namespace is package level state in common, shared with every
	// other test in this binary including the envtest suite. Use the one they all
	// use; inventing a namespace here leaves it set for whatever runs next.
	common.SetOperatorNameSpace(defaultNamespace)

	// the trust bundle trust-manager distributes; both halves reference it, and
	// generate nothing without it
	caBundle := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetOperatorCASecretName(),
			Namespace: defaultNamespace,
		},
		Data: map[string][]byte{"ca.pem": []byte("not parsed, only referenced")},
	}

	// the identity the generated ScrapeConfig scrapes as
	promCert := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.DefaultPrometheusCertSecretName,
			Namespace: serviceTestNamespace,
		},
		Data: map[string][]byte{"tls.crt": []byte("cert"), "tls.key": []byte("key")},
	}

	objects := []client.Object{caBundle, promCert}
	for _, fn := range adjust {
		objects = fn(objects)
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRESTMapper(mapper).WithObjects(objects...).Build()
}

const (
	testServiceName = "my-service"
	// the namespace every BrokerService fixture below is created in
	serviceTestNamespace = "svc-ns"

	testAppName = "my-app"
	// deliberately not the service's, so the fixtures exercise an app bound
	// across namespaces
	appTestNamespace = "app-ns"
)

func serviceReconcilerFor() *BrokerServiceInstanceReconciler {
	loop := &ReconcilerLoop{KubeBits: &KubeBits{Client: monitoringClient(true), log: testLogger()}}
	loop.ReconcilerLoopType = &BrokerServiceReconciler{ReconcilerLoop: loop}

	return &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: &BrokerServiceReconciler{ReconcilerLoop: loop},
		instance: &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: testServiceName, Namespace: serviceTestNamespace},
		},
		status: &v1beta2.BrokerServiceStatus{},
	}
}

func appReconcilerFor() *BrokerAppInstanceReconciler {
	loop := &ReconcilerLoop{KubeBits: &KubeBits{Client: monitoringClient(true), log: testLogger()}}
	loop.ReconcilerLoopType = &BrokerAppReconciler{ReconcilerLoop: loop}

	return &BrokerAppInstanceReconciler{
		BrokerAppReconciler: &BrokerAppReconciler{ReconcilerLoop: loop},
		instance: &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: appTestNamespace},
		},
		status: &v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         testServiceName,
				Namespace:    serviceTestNamespace,
				AssignedPort: 61616,
			},
		},
	}
}

// TrackDesired keys by pointer type, unlike the deployed map.
func tracked(loop *ReconcilerLoop, obj client.Object, name string) client.Object {
	return loop.desired[reflect.TypeOf(obj)][name]
}

func trackedServiceScrapeConfig(t *testing.T, reconciler *BrokerServiceInstanceReconciler, name string) *monitoringv1alpha1.ScrapeConfig {
	obj := tracked(reconciler.ReconcilerLoop, &monitoringv1alpha1.ScrapeConfig{}, name)
	assert.NotNil(t, obj, "no ScrapeConfig named %s was tracked", name)
	return obj.(*monitoringv1alpha1.ScrapeConfig)
}

func trackedScrapeConfig(t *testing.T, reconciler *BrokerAppInstanceReconciler, name string) *monitoringv1alpha1.ScrapeConfig {
	obj := tracked(reconciler.ReconcilerLoop, &monitoringv1alpha1.ScrapeConfig{}, name)
	assert.NotNil(t, obj, "no ScrapeConfig named %s was tracked", name)
	return obj.(*monitoringv1alpha1.ScrapeConfig)
}
