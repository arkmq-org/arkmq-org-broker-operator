# Cross-namespace Prometheus scraping

## Problem

A BrokerApp can bind to a BrokerService in another namespace, and that is the
case multi-tenancy rests on. Prometheus grants access to metrics per namespace,
so an app's metrics are private to it only when its series are labelled with a
namespace of its own. Two apps sharing a namespace can each be scraped under
their own certificate, but anyone allowed to read metrics there reads both.

So each app's scrape has to be declared in the app's namespace, where its
series land and where its client certificate lives, while the broker it scrapes
lives in the service's namespace. The scrape wiring has to cross that gap on
every cluster the operator supports, OpenShift included, whose user workload
monitoring:

- serves `ServiceMonitor`, `PodMonitor` and `Probe`, but not `ScrapeConfig`;
- enforces `ignoreNamespaceSelectors: true`, so a `ServiceMonitor` or
  `PodMonitor` only discovers objects in its own namespace. From the OpenShift
  monitoring team: *"it's always been the case that with user-defined
  monitoring, the service monitor has to live in the same namespace as the
  service."*

## Decision: a Probe whose prober is the broker

Every service and every app gets one `Probe`, in its own namespace, with:

- `prober.url` set to the broker pod's fully qualified name on the mTLS metrics
  port, `prober.path` to `/metrics` and `prober.scheme` to `https`;
- a single static target naming the same host;
- a `tlsConfig` presenting the owner's certificate and verifying the broker by
  that name, which the operand certificate's wildcard covers.

prometheus-operator renders a static-target Probe with `__address__` taken from
the prober URL, `instance` from the target, and `namespace` from the Probe's
own namespace, whether or not a namespace label is enforced. The scrape
therefore reaches the broker directly, `instance` is the broker's stable name,
and the series belong to the Probe's namespace.

This was verified on OpenShift's user workload monitoring, including from a
namespace holding nothing but a certificate and a Probe: the target is up, its
series are labelled with that namespace, and the broker returns only the queues
the presented certificate is entitled to.

**Caveats.** A Probe is designed to drive a blackbox exporter, and this uses it
off-label:

- every scrape carries a `target` query parameter, which the broker's exporter
  ignores; an exporter that interpreted it would break the scrape;
- the design relies on how prometheus-operator renders a Probe rather than on a
  documented contract for this use, although that rendering has been stable
  across the `v1` API;
- a Probe addresses a single host, so a service running several brokers would
  need one Probe per broker;
- Prometheus connects straight from its own namespace to the broker's, so the
  broker's namespace has to admit it, as it does for the service's own scrape.

**The Probe types and their pin.** The operator writes Probes with
prometheus-operator's own API module. Each release of that module is built
against one controller-runtime and Kubernetes minor, so the pin moves in step
with this repository's: v0.93.1 matches controller-runtime v0.24. Releases up
to at least v0.87 hold `bearerTokenSecret` as a struct rather than a pointer,
so every Probe they serialize names an empty secret, and current
prometheus-operator releases reject such a Probe as unresolvable. A unit test
pins the serialized spec to the fields the operator sets, and the end-to-end
suite asks a real Prometheus whether each generated Probe became a target that
is up, so a pin that regresses either fails there.

## Options considered

### A. ScrapeConfig

A `ScrapeConfig` names the broker as a static target, the same shape as the
chosen design with a kind meant for it.

**Rejected:** OpenShift does not serve it. Supporting it where it is served
means a second path besides whatever OpenShift needs, a runtime switch between
them, and a CI cluster exercising the path OpenShift never takes. It is also
`v1alpha1`.

### B. Cross-namespace namespaceSelector on a ServiceMonitor

**Rejected:** OpenShift's Cluster Monitoring Operator enforces
`ignoreNamespaceSelectors: true` and rewrites it within minutes if changed. It
is not a supportable configuration.

### C. Selectorless Service with operator-managed Endpoints

A headless Service in the app's namespace with no selector, whose Endpoints the
operator fills with the broker pod IPs, discovered by a ServiceMonitor beside
it. Kubernetes documents this pattern for pointing a Service at another
namespace. The address has to be an IP, because prometheus-operator discovers
through the `endpoints` role, which reads core v1 Endpoints, and those carry
IPs only.

**Rejected:** OpenShift's `RestrictedEndpointsAdmission` refuses Endpoints
pointing into the pod network unless their author holds `create` and `update`
on `endpoints/restricted`. The operator would need that grant cluster-wide,
lifting a tenant isolation guard in every namespace it watches, to deliver a
tenancy feature. Beyond that, it carries pod IPs that go stale on a restart,
needs a watch on broker pods to correct them, and pins the integration to core
v1 Endpoints, deprecated since Kubernetes v1.33.

### D. ExternalName Service

**Rejected:** an ExternalName Service has no Endpoints, and prometheus-operator
reads Endpoints.

### E. EndpointSlice with addressType FQDN

**Rejected:** the `endpoints` discovery role reads core v1 Endpoints, not
EndpointSlices, so an FQDN slice is never discovered.

### F. PodMonitor

**Rejected:** subject to the same `ignoreNamespaceSelectors` enforcement, and
there are no broker pods in the app's namespace to select.

### G. A proxy pod in the app's namespace

A TCP passthrough in the app's namespace, fronted by a Service a ServiceMonitor
discovers. TLS stays end to end, no special permission is needed, and the
scrape follows the network path the app's own clients already use.

**Rejected for now:** the operator would run a workload in every tenant
namespace hosting a cross-namespace app, with its image, resources, security
context and health to maintain, and one more component on the scrape path. It
remains the fallback should the Probe caveats above ever bite.

## References

- [prometheus-operator: Probe][prom-probe]
- [prometheus-operator: ScrapeConfig][prom-sc]
- [Kubernetes: Services without selectors][k8s-svc-no-selector]
- [OpenShift: enabling monitoring for user-defined projects][ocp-uwm]
- [OpenShift: admission plug-ins][ocp-admission]

[prom-probe]: https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1.Probe
[prom-sc]: https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1alpha1.ScrapeConfig
[k8s-svc-no-selector]: https://kubernetes.io/docs/concepts/services-networking/service/#services-without-selectors
[ocp-uwm]: https://docs.openshift.com/container-platform/latest/observability/monitoring/enabling-monitoring-for-user-defined-projects.html
[ocp-admission]: https://docs.openshift.com/container-platform/latest/architecture/admission-plug-ins.html
