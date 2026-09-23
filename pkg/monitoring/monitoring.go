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

// Package monitoring generates the Prometheus scrape wiring for the brokers the
// operator manages, and probes whether the cluster can accept it.
package monitoring

import (
	"sync"
	"time"

	monitoringv1alpha1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

const (
	// Port is where the broker's prometheus javaagent listens, over mTLS.
	Port int32 = 8888

	// Path is the path the prometheus javaagent serves.
	Path = "/metrics"

	// WiringSuffix is appended to a BrokerService or BrokerApp name to name the
	// scrape wiring generated for it.
	WiringSuffix = "-metrics"
)

// WiringName names the scrape wiring generated for a CR.
func WiringName(crName string) string {
	return crName + WiringSuffix
}

var (
	availableMutex sync.Mutex
	available      *bool
	availableAt    time.Time

	// Re-probed rather than cached forever, so installing prometheus-operator
	// after the operator started is picked up without a restart. The watches set
	// up in SetupWithManager still need one, see the controllers.
	availabilityTTL = time.Minute
)

// IsPrometheusAvailable reports whether this cluster can accept the scrape
// wiring the operator generates, by asking the API server whether it serves the
// ScrapeConfig kind. It says nothing about a Prometheus actually running: that
// is the cluster admin's to provide, and nothing the operator can verify.
//
// The answer is reused for a minute, so a cluster that gains prometheus-operator
// later starts getting wiring without an operator restart.
func IsPrometheusAvailable(mapper meta.RESTMapper) bool {
	availableMutex.Lock()
	defer availableMutex.Unlock()

	if available != nil && time.Since(availableAt) < availabilityTTL {
		return *available
	}

	detected := scrapeConfigIsServed(mapper)
	available = &detected
	availableAt = time.Now()

	return detected
}

// ResetAvailability drops the cached probe.
func ResetAvailability() {
	availableMutex.Lock()
	defer availableMutex.Unlock()

	available = nil
}

// scrapeConfigIsServed treats every failure as "not served": a missing CRD is the
// expected case, and a transient discovery error is retried when the cache
// expires. Generating nothing is always safer than failing a reconcile over an
// optional integration.
func scrapeConfigIsServed(mapper meta.RESTMapper) bool {
	if mapper == nil {
		return false
	}

	groupKind := schema.GroupKind{
		Group: monitoringv1alpha1.SchemeGroupVersion.Group,
		Kind:  monitoringv1alpha1.ScrapeConfigsKind,
	}

	if _, err := mapper.RESTMapping(groupKind, monitoringv1alpha1.SchemeGroupVersion.Version); err != nil {
		if !meta.IsNoMatchError(err) {
			ctrl.Log.V(1).Info("could not determine whether ScrapeConfig is served", "err", err)
		}
		return false
	}

	return true
}

// Labels are stamped on every generated object. common.LabelMonitoring is the
// only one a Prometheus instance needs to select on, so nothing has to be
// parametrized per service or per app; the rest identify what the wiring belongs
// to.
func Labels(component string, instance string, serviceName string) map[string]string {
	return map[string]string{
		common.LabelMonitoring:             "true",
		common.LabelAppKubernetesManagedBy: common.OperatorName,
		common.LabelAppKubernetesComponent: component,
		common.LabelAppKubernetesInstance:  instance,
		common.LabelBrokerService:          serviceName,
	}
}

// CARef names the trust bundle a scraper verifies the broker with. trust-manager
// distributes it to every namespace under the same name, so the reference is
// valid from the service's namespace and from any app's. Returns nil when the
// bundle cannot be resolved, which means no wiring should be generated.
func CARef(client rtclient.Client) *corev1.SecretKeySelector {
	name := common.GetOperatorCASecretName()

	key, err := common.GetOperatorCASecretKey(client, nil)
	if err != nil {
		return nil
	}

	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
}
