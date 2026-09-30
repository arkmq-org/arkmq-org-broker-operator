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
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ScrapeTarget is everything a scrape of a BrokerService's mTLS metrics
// endpoint needs.
type ScrapeTarget struct {
	// Owner names the generated object and places it in the owner's namespace.
	// The namespace is taken from the owner rather than passed separately
	// because an owner reference cannot cross namespaces: an object placed
	// elsewhere would be garbage collected as soon as it was created. The
	// reference itself is set by the reconciler loop when it creates the object.
	Owner rtclient.Object

	// ServiceSelector selects the Service whose PortName endpoints are scraped.
	ServiceSelector map[string]string

	// ServerName is the name the broker's certificate is verified against. Every
	// pod presents the same certificate, whose wildcard covers the pods' fully
	// qualified names, so any one of those names verifies every pod.
	ServerName string

	// ClientCertSecret holds the tls.crt and tls.key the scraper presents. The
	// broker maps that certificate's common name to a role, and the role decides
	// which queues the scrape can see.
	ClientCertSecret string

	// CA is the trust bundle the broker is verified with, resolved in the
	// generated object's own namespace.
	CA *corev1.SecretKeySelector

	// Labels go on the object, for a Prometheus to select it by.
	Labels map[string]string
}

// BuildServiceMonitor renders a ScrapeTarget as a ServiceMonitor.
//
// The broker labels each app queue's series with the owning app's namespace,
// and honorLabels keeps that label instead of replacing it with the
// ServiceMonitor's own. That is what files an app's queues with the app while
// broker-wide series stay with the service. A Prometheus that enforces its own
// namespace label, as OpenShift's user workload monitoring does, overrides it
// regardless.
//
// Pass the currently deployed object as existing to update it in place; only the
// fields the operator owns are touched, leaving anything the API server defaulted
// alone, which is what stops the object being rewritten on every reconcile.
func BuildServiceMonitor(target ScrapeTarget, existing *monitoringv1.ServiceMonitor) *monitoringv1.ServiceMonitor {
	name := WiringName(target.Owner.GetName())

	desired := existing
	if desired == nil {
		desired = &monitoringv1.ServiceMonitor{
			TypeMeta: metav1.TypeMeta{
				APIVersion: monitoringv1.SchemeGroupVersion.Identifier(),
				Kind:       monitoringv1.ServiceMonitorsKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: target.Owner.GetNamespace(),
			},
		}
	}

	certRef, keyRef := clientCertRefs(target.ClientCertSecret)

	endpoint := monitoringv1.Endpoint{
		Port:        PortName,
		Scheme:      ptr.To(monitoringv1.SchemeHTTPS),
		Path:        Path,
		HonorLabels: true,
		// Prometheus would otherwise name the job after the Service, which is
		// the BrokerService's own name; keep the name dashboards query. The
		// action is the default, spelt out because the API server stores it and
		// a desired object without it would differ from the deployed one on
		// every reconcile.
		RelabelConfigs: []monitoringv1.RelabelConfig{{
			TargetLabel: "job",
			Replacement: ptr.To(name),
			Action:      "replace",
		}},
		HTTPConfigWithProxyAndTLSFiles: monitoringv1.HTTPConfigWithProxyAndTLSFiles{
			HTTPConfigWithTLSFiles: monitoringv1.HTTPConfigWithTLSFiles{
				TLSConfig: &monitoringv1.TLSConfig{
					SafeTLSConfig: monitoringv1.SafeTLSConfig{
						ServerName: ptr.To(target.ServerName),
						CA:         monitoringv1.SecretOrConfigMap{Secret: target.CA},
						Cert:       monitoringv1.SecretOrConfigMap{Secret: certRef},
						KeySecret:  keyRef,
					},
				},
			},
		},
	}

	desired.Labels = target.Labels
	desired.Spec.Selector = metav1.LabelSelector{MatchLabels: target.ServiceSelector}
	desired.Spec.Endpoints = []monitoringv1.Endpoint{endpoint}

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
