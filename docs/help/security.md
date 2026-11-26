# Security

## Overview

This document describes the security model of the ArkMQ Operator and the
controls it applies at each layer: code, container, and cluster. It also
explains how to harden broker deployments and how to report vulnerabilities.

---

## Vulnerability Reporting

> [!IMPORTANT]
> Please do not report security vulnerabilities through public GitHub issues.

To make a report, send an email containing the details of the vulnerability
to security@arkmq.org (an alias to a private mailing list in
Google Groups containing just the maintainers of the project).
Private disclosure of a potential vulnerability is important.
The maintainers will reply acknowledging the report,
and decide whether to keep it private or publicly disclose it.

---

## Code Layer

The project enforces the following automated quality gates on every pull
request and CI run:

| Check | Tool | Purpose |
|-------|------|---------|
| Dependency hygiene | `go mod tidy` + `git diff` | Ensures `go.mod` and `go.sum` are committed and up to date |
| Static analysis | `go vet` | Detects suspicious or incorrect Go constructs |
| Generated file consistency | `make generate-deploy` + `git diff` | Ensures generated CRD manifests and code are not out of sync |
| Operator scorecard | OLM scorecard | Validates the bundle against Operator best practices |

A failure in any of these steps blocks the pull request from merging.

---

## Container Layer

The operator image and the broker operand images are built with minimal attack
surfaces:

- Based on `ubi9-minimal` (Red Hat Universal Base Image 9 minimal), which
  follows CIS and DISA hardening guidelines.
- Rebuilt regularly to pick up base-image patch-level updates.
- No package managers or shells required at runtime.

Container images are scanned for known CVEs before release. The
[images documentation](images.md) describes how to verify and use the published
images.

---

## Cluster Layer

### Trust Model and Security Boundaries

Any Kubernetes principal that can `create` or `update` an `ActiveMQArtemis` or
`Broker` custom resource (CR) effectively controls the Broker Pods spec that
the operator manages. This includes security-sensitive fields such as
spec.securityContext, spec.podSecurityContext, and spec.serviceAccountName.
The operator applies hardened defaults whenever CR fields are not explicitly
set. Admission control at the namespace level (via
[Pod Security Admission](https://kubernetes.io/docs/concepts/security/pod-security-admission/))
is the actual enforcement boundary for Pod policies, not the operator itself.

This is consistent with how all Kubernetes operators work: the operator acts as
a privileged agent that translates high-level intent into low-level Kubernetes
resources. Granting someone write access to a an `ActiveMQArtemis` or `Broker`
resource is equivalent to granting them control over the resulting StatefulSet,
Services, and other managed resources.

### Role-Based Access Control (RBAC)

#### Operator Service Account

The operator runs as the `controller-manager` ServiceAccount in the operator
namespace. The scope of its permissions depends on the installation mode:

| Installation mode | RBAC kind | Scope |
|-------------------|-----------|-------|
| Single namespace | `Role` + `RoleBinding` | Operator namespace only |
| Cluster-wide | `ClusterRole` + `ClusterRoleBinding` | All watched namespaces |

Prefer the single-namespace mode when all broker workloads live in one
namespace — it limits the blast radius if the operator ServiceAccount is
ever compromised. Use the cluster-wide mode only when the operator must
manage brokers across multiple namespaces, and constrain which namespaces it
watches via the `WATCH_NAMESPACE` environment variable.

### Pod and Container Security Contexts

#### Operator Pod

The operator Pod itself is hardened by default:

```yaml
# Pod-level
securityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

# Container-level
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
```

#### Broker Pods

The operator applies the following defaults to broker Pods:

```yaml
# Pod-level default
securityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

# Container-level default
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
  seccompProfile:
    type: RuntimeDefault
  runAsNonRoot: true
```

You can customise these defaults via the `spec.deploymentPlan.podSecurityContext`
and `spec.deploymentPlan.containerSecurityContext` fields in the `Broker` CR.
Container-level values fully replace (not merge with) the defaults, so always
include all required fields when overriding.

##### Running as a Specific User

To pin the broker Pod to a specific UID:

```yaml
spec:
  deploymentPlan:
    runAsUser: 1000
```

##### Custom Service Account

To associate the broker Pod with a pre-existing ServiceAccount (for workload
identity federation such as AWS IRSA or GCP Workload Identity):

```yaml
spec:
  deploymentPlan:
    serviceAccountName: my-broker-sa
```

The operator validates that the ServiceAccount exists but does not create or
modify it.

When `spec.deploymentPlan.restricted: true` is set the operator enforces a
fully locked-down security context — see [Restricted Mode](#restricted-mode)
below.

### Network Policies

The operator ships a `NetworkPolicy` for its own controller Pod that restricts
inbound traffic to only the ports the operator legitimately exposes:

| Port | Protocol | Purpose |
|------|----------|---------|
| 8081 | TCP | Health (`/healthz`) and readiness (`/readyz`) probes |
| 8383 | TCP | Prometheus metrics endpoint |

All other ingress to the operator Pod is denied by default.

For broker Pods, the operator does not create `NetworkPolicy` objects
automatically. You should deploy your own `NetworkPolicy` resources to restrict
access to the broker ports that you expose.

## Additional Resources

- [SSL Broker Setup Tutorial](../tutorials/ssl_broker_setup.md)
- [Cert-Manager and Trust-Manager Tutorial](../tutorials/cert-manager-and-trust-manager.md)
- [Locked-Down Broker with Prometheus Tutorial](../tutorials/prometheus_locked_down.md)
- [Custom Resources Reference](custom-resources.md)
- [Operator Overview](operator.md)
