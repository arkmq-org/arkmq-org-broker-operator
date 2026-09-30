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

package monitoring

import (
	"encoding/json"
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func caRef() *corev1.SecretKeySelector {
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "arkmq-org-broker-manager-ca"},
		Key:                  "ca.pem",
	}
}

func owner(name, namespace string) *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
}

func target() ScrapeTarget {
	return ScrapeTarget{
		Owner:            owner("my-service", "svc-ns"),
		ServiceSelector:  map[string]string{"broker.arkmq.org/service": "my-service"},
		ServerName:       "my-service-ss-0.my-service-hdls-svc.svc-ns.svc.cluster.local",
		ClientCertSecret: "prometheus-cert",
		CA:               caRef(),
		Labels:           map[string]string{"broker.arkmq.org/monitoring": "true"},
	}
}

func TestServiceMonitorIsNamedAndPlacedAfterItsOwner(t *testing.T) {
	serviceMonitor := BuildServiceMonitor(target(), nil)

	assert.Equal(t, "my-service-metrics", serviceMonitor.Name)
	// an owner reference cannot cross namespaces, so this must follow the owner
	assert.Equal(t, "svc-ns", serviceMonitor.Namespace)
	assert.Equal(t, target().Labels, serviceMonitor.Labels)
}

func TestServiceMonitorScrapesTheMetricsPortOfTheSelectedService(t *testing.T) {
	serviceMonitor := BuildServiceMonitor(target(), nil)

	assert.Equal(t, target().ServiceSelector, serviceMonitor.Spec.Selector.MatchLabels)
	assert.Len(t, serviceMonitor.Spec.Endpoints, 1)

	endpoint := serviceMonitor.Spec.Endpoints[0]
	assert.Equal(t, PortName, endpoint.Port)
	assert.Equal(t, monitoringv1.SchemeHTTPS, *endpoint.Scheme)
	assert.Equal(t, Path, endpoint.Path)
}

func TestServiceMonitorHonoursTheNamespaceTheBrokerLabelsQueuesWith(t *testing.T) {
	endpoint := BuildServiceMonitor(target(), nil).Spec.Endpoints[0]

	// without it, an app queue's namespace label is renamed exported_namespace
	// and the series land in the service's namespace, the app's tenant blind
	assert.True(t, endpoint.HonorLabels)
}

func TestServiceMonitorPresentsTheGivenIdentityOverMutualTLS(t *testing.T) {
	tlsConfig := BuildServiceMonitor(target(), nil).Spec.Endpoints[0].TLSConfig

	// the broker maps this certificate's common name to a role, and the role
	// decides which queues the scrape can see
	assert.Equal(t, "prometheus-cert", tlsConfig.Cert.Secret.Name)
	assert.Equal(t, "tls.crt", tlsConfig.Cert.Secret.Key)
	assert.Equal(t, "prometheus-cert", tlsConfig.KeySecret.Name)
	assert.Equal(t, "tls.key", tlsConfig.KeySecret.Key)

	assert.Equal(t, caRef(), tlsConfig.CA.Secret)
	assert.Equal(t, target().ServerName, *tlsConfig.ServerName)
}

func TestServiceMonitorSerializesOnlyWhatItSets(t *testing.T) {
	raw, err := json.Marshal(BuildServiceMonitor(target(), nil))
	assert.NoError(t, err)

	var wire struct {
		Spec struct {
			Endpoints []map[string]json.RawMessage `json:"endpoints"`
		} `json:"spec"`
	}
	assert.NoError(t, json.Unmarshal(raw, &wire))

	// prometheus-operator resolves any credential reference present, so an
	// empty one, such as a bearerTokenSecret with no name, gets the object
	// rejected
	keys := make([]string, 0)
	for key := range wire.Spec.Endpoints[0] {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, []string{"port", "scheme", "path", "honorLabels", "relabelings", "tlsConfig"}, keys)
}

func TestServiceMonitorNamesTheJobAfterTheWiring(t *testing.T) {
	relabelings := BuildServiceMonitor(target(), nil).Spec.Endpoints[0].RelabelConfigs

	// not the Service's name, which is the BrokerService's own: dashboards and
	// alerts written against <service>-metrics keep working
	assert.Equal(t, []monitoringv1.RelabelConfig{
		{TargetLabel: "job", Replacement: ptr.To("my-service-metrics"), Action: "replace"},
	}, relabelings)
}

func TestServiceMonitorLeavesNoRelabellingActionToBeDefaulted(t *testing.T) {
	// the API server fills in an empty action, and the stored object would then
	// differ from the desired one on every reconcile
	for _, endpoint := range BuildServiceMonitor(target(), nil).Spec.Endpoints {
		for _, relabeling := range append(endpoint.RelabelConfigs, endpoint.MetricRelabelConfigs...) {
			assert.NotEmpty(t, relabeling.Action, "relabeling of %s", relabeling.TargetLabel)
		}
	}
}

func TestBuildingOntoAnExistingObjectLeavesWhatWeDoNotOwn(t *testing.T) {
	existing := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-service-metrics",
			Namespace:       "svc-ns",
			ResourceVersion: "42",
		},
	}
	// a field the operator does not own, which it must not clobber
	existing.Annotations = map[string]string{"owner": "someone-else"}

	updated := BuildServiceMonitor(target(), existing)

	assert.Same(t, existing, updated)
	assert.Equal(t, "42", updated.ResourceVersion)
	assert.Equal(t, "someone-else", updated.Annotations["owner"], "fields set elsewhere must survive, or the object is rewritten every reconcile")
	assert.Len(t, updated.Spec.Endpoints, 1)
}
