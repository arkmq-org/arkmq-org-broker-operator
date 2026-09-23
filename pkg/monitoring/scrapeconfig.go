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
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ScrapeTarget is everything a scrape of a broker's mTLS metrics endpoint needs.
// A BrokerService and a BrokerApp differ only in what they put here: which hosts
// to reach and which certificate to present.
type ScrapeTarget struct {
	// Owner names the generated object and places it in the owner's namespace.
	// The namespace is taken from the owner rather than passed separately
	// because an owner reference cannot cross namespaces: an object placed
	// elsewhere would be garbage collected as soon as it was created. The
	// reference itself is set by the reconciler loop when it creates the object.
	Owner rtclient.Object

	// Host is the endpoint to scrape, without a port. A BrokerService owns a
	// single Broker, so one target is enough; serving an active/passive pair
	// would mean carrying a list here.
	Host string
	Port int32

	// ServerName is the name the broker's certificate is verified against. It is
	// not necessarily Host: the operand certificate carries a wildcard covering
	// the pods' fully qualified names, which a service DNS name is not.
	ServerName string

	// ClientCertSecret holds the tls.crt and tls.key the scraper presents. The
	// broker maps that certificate's common name to a role, and the role decides
	// which queues the scrape can see, so this is what scopes the result.
	ClientCertSecret string

	// CA is the trust bundle the broker is verified with, resolved in the
	// generated object's own namespace.
	CA *corev1.SecretKeySelector

	// Labels go on the object, for a Prometheus to select it by. SeriesLabels go
	// on every series it collects, so copies gathered under different identities
	// stay distinguishable.
	Labels       map[string]string
	SeriesLabels map[string]string
}

// BuildScrapeConfig renders a ScrapeTarget as a ScrapeConfig.
//
// Pass the currently deployed object as existing to update it in place; only the
// fields the operator owns are touched, leaving anything the API server defaulted
// alone, which is what stops the object being rewritten on every reconcile.
func BuildScrapeConfig(target ScrapeTarget, existing *monitoringv1alpha1.ScrapeConfig) *monitoringv1alpha1.ScrapeConfig {
	name := WiringName(target.Owner.GetName())

	desired := existing
	if desired == nil {
		desired = &monitoringv1alpha1.ScrapeConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: monitoringv1alpha1.SchemeGroupVersion.Identifier(),
				Kind:       monitoringv1alpha1.ScrapeConfigsKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: target.Owner.GetNamespace(),
			},
		}
	}

	// Prometheus would otherwise name the job after the object's own coordinates,
	// scrapeConfig/<namespace>/<name>, which leaks into every user query. Naming
	// it after the wiring keeps queries stable and matches what a ServiceMonitor
	// over a Service of the same name produced.
	seriesLabels := map[monitoringv1.LabelName]string{"job": name}
	for key, value := range target.SeriesLabels {
		seriesLabels[monitoringv1.LabelName(key)] = value
	}

	certRef, keyRef := clientCertRefs(target.ClientCertSecret)

	desired.Labels = target.Labels
	desired.Spec.StaticConfigs = []monitoringv1alpha1.StaticConfig{{
		Targets: []monitoringv1alpha1.Target{
			monitoringv1alpha1.Target(fmt.Sprintf("%s:%d", target.Host, target.Port)),
		},
		Labels: seriesLabels,
	}}
	desired.Spec.MetricsPath = ptr.To(Path)
	desired.Spec.Scheme = ptr.To("HTTPS")
	desired.Spec.TLSConfig = &monitoringv1.SafeTLSConfig{
		ServerName: target.ServerName,
		CA:         monitoringv1.SecretOrConfigMap{Secret: target.CA},
		Cert:       monitoringv1.SecretOrConfigMap{Secret: certRef},
		KeySecret:  keyRef,
	}

	return desired
}

// clientCertRefs names the keypair a scraper presents, by the convention
// cert-manager writes into a certificate's secret.
func clientCertRefs(secretName string) (*corev1.SecretKeySelector, *corev1.SecretKeySelector) {
	cert := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  "tls.crt",
	}
	key := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  "tls.key",
	}
	return cert, key
}
