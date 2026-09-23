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
	"testing"

	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
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

func TestScrapeConfigIsNamedAndPlacedAfterItsOwner(t *testing.T) {
	scrapeConfig := BuildScrapeConfig(target(), nil)

	assert.Equal(t, "my-service-metrics", scrapeConfig.Name)
	// an owner reference cannot cross namespaces, so this must follow the owner
	assert.Equal(t, "svc-ns", scrapeConfig.Namespace)
}

func TestScrapeConfigAddressesTheHostOnTheMetricsPort(t *testing.T) {
	spec := target()
	spec.Host = "broker-0.example"

	targets := BuildScrapeConfig(spec, nil).Spec.StaticConfigs[0].Targets

	assert.Len(t, targets, 1)
	assert.Equal(t, "broker-0.example:8888", string(targets[0]))
}

func TestScrapeConfigPresentsTheGivenIdentityOverMutualTLS(t *testing.T) {
	scrapeConfig := BuildScrapeConfig(target(), nil)

	assert.Equal(t, "HTTPS", *scrapeConfig.Spec.Scheme)
	assert.Equal(t, Path, *scrapeConfig.Spec.MetricsPath)

	tlsConfig := scrapeConfig.Spec.TLSConfig
	// the broker maps this certificate's common name to a role, and the role
	// decides which queues the scrape can see
	assert.Equal(t, "prometheus-cert", tlsConfig.Cert.Secret.Name)
	assert.Equal(t, "tls.crt", tlsConfig.Cert.Secret.Key)
	assert.Equal(t, "prometheus-cert", tlsConfig.KeySecret.Name)
	assert.Equal(t, "tls.key", tlsConfig.KeySecret.Key)

	assert.Equal(t, caRef(), tlsConfig.CA.Secret)
	assert.Equal(t, target().ServerName, tlsConfig.ServerName)
}

func TestScrapeConfigServerNameNeedNotBeATarget(t *testing.T) {
	spec := target()
	spec.Host = "my-service.svc-ns.svc.cluster.local"

	scrapeConfig := BuildScrapeConfig(spec, nil)

	// the operand certificate carries a wildcard over the pods' fully qualified
	// names, which the service DNS name is not covered by, so the two differ
	assert.NotEqual(t,
		string(scrapeConfig.Spec.StaticConfigs[0].Targets[0]),
		scrapeConfig.Spec.TLSConfig.ServerName+":8888")
}

func TestBuildingOntoAnExistingObjectLeavesWhatWeDoNotOwn(t *testing.T) {
	existing := &monitoringv1alpha1.ScrapeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "my-service-metrics",
			Namespace:       "svc-ns",
			ResourceVersion: "42",
		},
	}
	// something the API server defaulted, which we must not clobber
	existing.Spec.HonorTimestamps = new(bool)

	updated := BuildScrapeConfig(target(), existing)

	assert.Same(t, existing, updated)
	assert.Equal(t, "42", updated.ResourceVersion)
	assert.NotNil(t, updated.Spec.HonorTimestamps, "defaulted fields must survive, or the object is rewritten every reconcile")
	assert.Len(t, updated.Spec.StaticConfigs, 1)
}

func TestScrapeConfigNamesTheJobAfterTheWiring(t *testing.T) {
	scrapeConfig := BuildScrapeConfig(target(), nil)

	// not prometheus-operator's default of scrapeConfig/<namespace>/<name>, which
	// would put the object's coordinates into every query a user writes
	assert.Equal(t, "my-service-metrics", string(scrapeConfig.Spec.StaticConfigs[0].Labels["job"]))
}

func TestScrapeConfigLetsTheCallerOverrideTheJob(t *testing.T) {
	spec := target()
	spec.SeriesLabels = map[string]string{"job": "something-else"}

	scrapeConfig := BuildScrapeConfig(spec, nil)

	assert.Equal(t, "something-else", string(scrapeConfig.Spec.StaticConfigs[0].Labels["job"]))
}
