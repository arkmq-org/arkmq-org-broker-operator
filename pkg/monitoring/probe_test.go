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
		Host:             "my-service-ss-0.my-service-hdls-svc.svc-ns.svc.cluster.local",
		Port:             Port,
		ServerName:       "my-service-ss-0.my-service-hdls-svc.svc-ns.svc.cluster.local",
		ClientCertSecret: "prometheus-cert",
		CA:               caRef(),
		Labels:           map[string]string{"broker.arkmq.org/monitoring": "true"},
		SeriesLabels:     map[string]string{"brokerservice": "my-service"},
	}
}

func TestProbeIsNamedAndPlacedAfterItsOwner(t *testing.T) {
	probe := BuildProbe(target(), nil)

	assert.Equal(t, "my-service-metrics", probe.Name)
	// an owner reference cannot cross namespaces, so this must follow the owner
	assert.Equal(t, "svc-ns", probe.Namespace)
}

func TestProbeScrapesTheHostDirectly(t *testing.T) {
	spec := target()
	spec.Host = "broker-0.example"

	probe := BuildProbe(spec, nil)

	// the broker is its own prober: the scrape goes to the prober URL
	assert.Equal(t, "broker-0.example:8888", probe.Spec.ProberSpec.URL)
	assert.Equal(t, monitoringv1.SchemeHTTPS, *probe.Spec.ProberSpec.Scheme)
	assert.Equal(t, Path, probe.Spec.ProberSpec.Path)

	// and instance is taken from the static target, the same host by name
	assert.Equal(t, []string{"broker-0.example"}, probe.Spec.Targets.StaticConfig.Targets)
}

func TestProbePresentsTheGivenIdentityOverMutualTLS(t *testing.T) {
	tlsConfig := BuildProbe(target(), nil).Spec.TLSConfig

	// the broker maps this certificate's common name to a role, and the role
	// decides which queues the scrape can see
	assert.Equal(t, "prometheus-cert", tlsConfig.Cert.Secret.Name)
	assert.Equal(t, "tls.crt", tlsConfig.Cert.Secret.Key)
	assert.Equal(t, "prometheus-cert", tlsConfig.KeySecret.Name)
	assert.Equal(t, "tls.key", tlsConfig.KeySecret.Key)

	assert.Equal(t, caRef(), tlsConfig.CA.Secret)
	assert.Equal(t, target().ServerName, *tlsConfig.ServerName)
}

func TestProbeServerNameNeedNotBeTheHost(t *testing.T) {
	spec := target()
	spec.Host = "my-service.svc-ns.svc.cluster.local"

	probe := BuildProbe(spec, nil)

	// the operand certificate carries a wildcard over the pods' fully qualified
	// names, which the service DNS name is not covered by, so the two differ
	assert.NotEqual(t, probe.Spec.ProberSpec.URL, *probe.Spec.TLSConfig.ServerName+":8888")
}

func TestProbeNamesTheJobAfterTheWiring(t *testing.T) {
	probe := BuildProbe(target(), nil)

	// not prometheus-operator's default of probe/<namespace>/<name>, which would
	// put the object's coordinates into every query a user writes
	assert.Equal(t, "my-service-metrics", probe.Spec.JobName)
}

func TestProbeStampsTheSeriesLabels(t *testing.T) {
	probe := BuildProbe(target(), nil)

	assert.Equal(t, map[string]string{"brokerservice": "my-service"}, probe.Spec.Targets.StaticConfig.Labels)
}

func TestProbeSerializesOnlyWhatItSets(t *testing.T) {
	raw, err := json.Marshal(BuildProbe(target(), nil))
	assert.NoError(t, err)

	var wire struct {
		Spec map[string]json.RawMessage `json:"spec"`
	}
	assert.NoError(t, json.Unmarshal(raw, &wire))

	// prometheus-operator resolves any credential reference present, so an
	// empty one, such as a bearerTokenSecret with no name, gets the Probe
	// rejected
	keys := make([]string, 0, len(wire.Spec))
	for key := range wire.Spec {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, []string{"jobName", "prober", "targets", "tlsConfig"}, keys)
}

func TestBuildingOntoAnExistingObjectLeavesWhatWeDoNotOwn(t *testing.T) {
	existing := &monitoringv1.Probe{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-service-metrics",
			Namespace:       "svc-ns",
			ResourceVersion: "42",
		},
	}
	// a field the operator does not own, which it must not clobber
	existing.Labels = nil
	existing.Annotations = map[string]string{"owner": "someone-else"}

	updated := BuildProbe(target(), existing)

	assert.Same(t, existing, updated)
	assert.Equal(t, "42", updated.ResourceVersion)
	assert.Equal(t, "someone-else", updated.Annotations["owner"], "fields set elsewhere must survive, or the object is rewritten every reconcile")
	assert.NotNil(t, updated.Spec.Targets.StaticConfig)
}
