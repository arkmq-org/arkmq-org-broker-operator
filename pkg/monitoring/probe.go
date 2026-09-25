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
	// would mean one Probe per broker, since a Probe addresses a single host.
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

// BuildProbe renders a ScrapeTarget as a Probe.
//
// A Probe is meant to drive a blackbox prober, which Prometheus calls with the
// probed host in a target parameter. Here the broker is its own prober: the
// prober URL is the broker's metrics endpoint and the static target names the
// same host, so the scrape reaches the broker directly and instance is its
// stable name. The target parameter it carries is ignored by the broker. This
// is the one static scrape target prometheus-operator serves everywhere,
// OpenShift's user workload monitoring included, and it reaches a broker in any
// namespace without a Service or Endpoints of its own.
//
// Pass the currently deployed object as existing to update it in place; only the
// fields the operator owns are touched, leaving anything the API server defaulted
// alone, which is what stops the object being rewritten on every reconcile.
func BuildProbe(target ScrapeTarget, existing *monitoringv1.Probe) *monitoringv1.Probe {
	name := WiringName(target.Owner.GetName())

	desired := existing
	if desired == nil {
		desired = &monitoringv1.Probe{
			TypeMeta: metav1.TypeMeta{
				APIVersion: monitoringv1.SchemeGroupVersion.Identifier(),
				Kind:       monitoringv1.ProbesKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: target.Owner.GetNamespace(),
			},
		}
	}

	certRef, keyRef := clientCertRefs(target.ClientCertSecret)

	desired.Labels = target.Labels
	// Prometheus would otherwise name the job after the object's own coordinates,
	// probe/<namespace>/<name>, which leaks into every user query.
	desired.Spec.JobName = name
	desired.Spec.ProberSpec = monitoringv1.ProberSpec{
		URL:    fmt.Sprintf("%s:%d", target.Host, target.Port),
		Scheme: ptr.To(monitoringv1.SchemeHTTPS),
		Path:   Path,
	}
	desired.Spec.Targets = monitoringv1.ProbeTargets{
		StaticConfig: &monitoringv1.ProbeTargetStaticConfig{
			Targets: []string{target.Host},
			Labels:  target.SeriesLabels,
		},
	}
	desired.Spec.TLSConfig = &monitoringv1.SafeTLSConfig{
		ServerName: ptr.To(target.ServerName),
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
