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

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/monitoring"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
)

func testLogger() logr.Logger {
	return logr.New(log.NullLogSink{})
}

// The scrape wiring is only useful if a scraper can act on it verbatim, so these
// pin the values a scraper reads: where to connect, what name to verify, and
// which identity to present.

func TestServiceMonitorScrapesTheMetricsPortOfTheServicesOwnService(t *testing.T) {
	reconciler := serviceReconcilerFor()

	assert.NoError(t, reconciler.processService())
	reconciler.processMonitoring()

	serviceMonitor := trackedServiceMonitor(t, reconciler.ReconcilerLoop, monitoring.WiringName(testServiceName))
	service := tracked(reconciler.ReconcilerLoop, &corev1.Service{}, testServiceName).(*corev1.Service)

	// the ServiceMonitor selects the Service by its labels and the port by its name
	for key, value := range serviceMonitor.Spec.Selector.MatchLabels {
		assert.Equal(t, value, service.Labels[key])
	}
	assert.Equal(t, monitoring.PortName, serviceMonitor.Spec.Endpoints[0].Port)
	assert.Contains(t, service.Spec.Ports, corev1.ServicePort{
		Name:       monitoring.PortName,
		Port:       monitoring.Port,
		TargetPort: intstr.FromInt32(monitoring.Port),
	})

	assert.Equal(t, monitoringv1.SchemeHTTPS, *serviceMonitor.Spec.Endpoints[0].Scheme)
	assert.Equal(t, monitoring.Path, serviceMonitor.Spec.Endpoints[0].Path)
	assert.Equal(t, "svc-ns", serviceMonitor.Namespace)
}

func TestServiceMonitorHonoursTheOwnerNamespaceOnQueueSeries(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	endpoint := trackedServiceMonitor(t, reconciler.ReconcilerLoop, monitoring.WiringName(testServiceName)).Spec.Endpoints[0]

	// the broker labels each app queue with its owner's namespace; without this
	// every series would land in the service's namespace
	assert.True(t, endpoint.HonorLabels)
}

func TestServiceMonitorVerifiesTheWildcardCoveredPodName(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	tlsConfig := trackedServiceMonitor(t, reconciler.ReconcilerLoop, monitoring.WiringName(testServiceName)).Spec.Endpoints[0].TLSConfig

	// The operand cert is issued for the service name and the hdls-svc wildcard.
	// A scrape reaches the pod by IP, so the name to verify has to be one the
	// wildcard covers, and the bare service DNS name is not.
	assert.Equal(t, common.OrdinalFQDNS(testServiceName, serviceTestNamespace, 0), *tlsConfig.ServerName)
	assert.NotEqual(t, fmt.Sprintf("my-service.svc-ns.svc.%s", common.GetClusterDomain()), *tlsConfig.ServerName)
}

func TestServiceMonitorPresentsThePrometheusIdentity(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	tlsConfig := trackedServiceMonitor(t, reconciler.ReconcilerLoop, monitoring.WiringName(testServiceName)).Spec.Endpoints[0].TLSConfig

	// the broad "metrics" role, so this scrape sees every app's queues
	assert.Equal(t, common.DefaultPrometheusCertSecretName, tlsConfig.Cert.Secret.Name)
	assert.Equal(t, "tls.crt", tlsConfig.Cert.Secret.Key)
	assert.Equal(t, common.DefaultPrometheusCertSecretName, tlsConfig.KeySecret.Name)
	assert.Equal(t, "tls.key", tlsConfig.KeySecret.Key)

	assert.Equal(t, common.GetOperatorCASecretName(), tlsConfig.CA.Secret.Name)
}

func TestServiceMonitorCarriesTheLabelAPrometheusSelectsOn(t *testing.T) {
	reconciler := serviceReconcilerFor()

	reconciler.processMonitoring()

	serviceMonitor := trackedServiceMonitor(t, reconciler.ReconcilerLoop, monitoring.WiringName(testServiceName))

	assert.Equal(t, "true", serviceMonitor.Labels[common.LabelMonitoring])
	assert.Equal(t, "my-service-metrics", serviceMonitor.Name)
}

func TestNoServiceMonitorWithoutAPrometheusCert(t *testing.T) {
	reconciler := serviceReconcilerFor()
	reconciler.Client = monitoringClient(true, withoutPrometheusCert)

	reconciler.processMonitoring()

	// there would be no identity to scrape as, and the broker would reject it
	assert.Nil(t, tracked(reconciler.ReconcilerLoop, &monitoringv1.ServiceMonitor{}, monitoring.WiringName(testServiceName)))
}

func TestNoServiceMonitorWithoutPrometheusOperator(t *testing.T) {
	reconciler := serviceReconcilerFor()
	reconciler.Client = monitoringClient(false)

	reconciler.processMonitoring()

	// the BrokerService works without it, so this is not an error
	assert.Nil(t, tracked(reconciler.ReconcilerLoop, &monitoringv1.ServiceMonitor{}, monitoring.WiringName(testServiceName)))
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

	mapper := meta.NewDefaultRESTMapper(nil)
	if served {
		mapper.Add(monitoringv1.SchemeGroupVersion.WithKind(monitoringv1.ServiceMonitorsKind), meta.RESTScopeNamespace)
	}

	// The operator namespace is package level state in common, shared with every
	// other test in this binary including the envtest suite. Use the one they all
	// use; inventing a namespace here leaves it set for whatever runs next.
	common.SetOperatorNameSpace(defaultNamespace)

	// the trust bundle trust-manager distributes; the generated wiring
	// references it, and none is generated without it
	caBundle := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.GetOperatorCASecretName(),
			Namespace: defaultNamespace,
		},
		Data: map[string][]byte{"ca.pem": []byte("not parsed, only referenced")},
	}

	// the identity the generated ServiceMonitor scrapes as
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

// TrackDesired keys by pointer type, unlike the deployed map.
func tracked(loop *ReconcilerLoop, obj client.Object, name string) client.Object {
	return loop.desired[reflect.TypeOf(obj)][name]
}

func trackedServiceMonitor(t *testing.T, loop *ReconcilerLoop, name string) *monitoringv1.ServiceMonitor {
	obj := tracked(loop, &monitoringv1.ServiceMonitor{}, name)
	assert.NotNil(t, obj, "no ServiceMonitor named %s was tracked", name)
	return obj.(*monitoringv1.ServiceMonitor)
}
