# BrokerService and BrokerApp

## Overview

BrokerService and BrokerApp are the two custom resources that together provide messaging as a service. A BrokerService provisions and manages a message broker. A BrokerApp describes the messaging requirements of an application and binds to a matching BrokerService to have those requirements fulfilled.

The relationship between them is intentionally decoupled: applications declare what they need, services declare what they offer, and Kubernetes labels connect the two. This separation allows platform teams to manage broker infrastructure independently from the application teams that consume it.

```
┌─────────────────────┐         label selector           ┌─────────────────────┐
│     BrokerApp       │ ──────────────────────────────>  │   BrokerService     │
│                     │                                  │                     │
│  spec.selector:     │    matches service labels        │  metadata.labels:   │
│    matchLabels:     │                                  │    env: production  │
│      env: production│                                  │    tier: shared     │
│                     │                                  │                     │
│  spec.capabilities: │                                  │  spec.resources:    │
│    - producerOf:    │                                  │    limits:          │
│      - address: Q1  │  <─── binding secret ──────────  │      memory: 2Gi    │
└─────────────────────┘                                  └─────────────────────┘
```

## BrokerService

A BrokerService represents a managed broker instance. The operator creates and manages the underlying Broker CR, Kubernetes Service, and Secrets on your behalf.

```yaml
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerService
metadata:
  name: shared-broker
  namespace: broker-infra
  labels:
    env: production
    tier: shared
spec:
  resources:
    limits:
      memory: 2Gi
```

### Spec Fields

| Field | Description |
|---|---|
| `resources` | Resource requirements (CPU/memory) for the broker pod |
| `env` | Environment variables passed to the broker container |
| `image` | Override the default broker container image |
| `appSelectorExpression` | CEL expression controlling which BrokerApps can bind (see [App Selector](brokerservice-appselector.md)) |

### Status

The BrokerService reports four conditions:

| Condition | Meaning |
|---|---|
| `Valid` | Spec passes validation (name format, CEL syntax) |
| `Deployed` | Underlying Broker CR and Service are created |
| `AppsProvisioned` | All bound BrokerApps have been configured on the broker |
| `Ready` | The service is fully operational |

The status also lists `provisionedApps` (apps successfully configured on the broker) and `pendingEmpty` (apps with addresses awaiting message consumption before removal — see [Data Protection](#data-protection)).

## BrokerApp

A BrokerApp describes an application's messaging requirements: which addresses it produces to, which it consumes from, and which BrokerService should host them.

```yaml
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-processor
  namespace: orders
spec:
  selector:
    matchLabels:
      env: production
      tier: shared
  addresses:
    - address: ORDERS.NEW
    - address: ORDERS.COMPLETED
  capabilities:
    - producerOf:
        - address: ORDERS.COMPLETED
      consumerOf:
        - address: ORDERS.NEW
```

### Spec Fields

| Field | Description |
|---|---|
| `selector` | Label selector to find matching BrokerServices (required, must be non-empty) |
| `addresses` | Private addresses owned by this app, not referenceable by other apps |
| `sharedAddresses` | Public addresses owned by this app, referenceable by other apps via `appNamespace`/`appName` |
| `capabilities` | Declares produce/consume permissions using `producerOf` and `consumerOf` |
| `resources` | Resource requirements for this app's share of broker capacity |

### Addresses and SharedAddresses

The use of this optional field allows Addresses to exist with a lifecycle tied to the app, independent of their reference from capabilities.

Addresses declared under `addresses` are private to this BrokerApp. Other apps cannot reference them. Sharing addresses must be intentional when apps share a broker service.

Addresses declared under `sharedAddresses` are public. Other apps can reference them in their capabilities using cross-app address references:


```yaml
# App "order-service" owns a shared work queue
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-service
  namespace: orders
spec:
  selector:
    matchLabels:
      env: production
  sharedAddresses:
    - address: ORDERS.FULFILLMENT
  capabilities:
    - producerOf:
        - address: ORDERS.FULFILLMENT
---
# App "warehouse" consumes from the shared work queue
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: warehouse
  namespace: fulfillment
spec:
  selector:
    matchLabels:
      env: production
  capabilities:
    - consumerOf:
        - address: ORDERS.FULFILLMENT
          appNamespace: orders
          appName: order-service
```

The same address cannot appear in both `addresses` and `sharedAddresses`.

### Pub/Sub Addresses

Addresses support publish/subscribe semantics with named subscriptions:

```yaml
spec:
  addresses:
    - address: APP.EVENTS
      pubSub: true
  capabilities:
    - producerOf:
        - address: APP.EVENTS
          pubSub: true
      consumerOf:
        - address: APP.EVENTS
          subscriptions:
            - client-1.events-sub
            - client-2.events-sub
```

When `pubSub` is true, consumers receive a copy of each message via their named subscription. Producers declare `pubSub: true` but do not specify subscriptions. Consumers must specify at least one subscription name.

### Status

The BrokerApp reports three conditions:

| Condition | Meaning |
|---|---|
| `Valid` | Spec passes validation |
| `Deployed` | Successfully bound to a BrokerService |
| `Ready` | Addresses and capabilities are configured on the broker |

When bound, `status.service` contains the binding details:

```yaml
status:
  service:
    name: shared-broker
    namespace: broker-infra
    secret: order-processor-binding
    assignedPort: 61616
```

A binding secret is created in the app's namespace containing `host`, `port`, and `uri` for the application to connect to the broker.

## How Matching Works

Matching between BrokerApp and BrokerService is bidirectional, requiring agreement from both sides.

### Step 1: BrokerApp Finds Candidates (Label Selector)

The BrokerApp's `spec.selector` is a standard Kubernetes [label selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors). It supports both `matchLabels` and `matchExpressions`:

```yaml
# Simple label matching
spec:
  selector:
    matchLabels:
      env: production

# Set-based matching
spec:
  selector:
    matchExpressions:
      - key: tier
        operator: In
        values: [shared, dedicated]
      - key: env
        operator: NotIn
        values: [dev]
```

The operator lists all BrokerServices across namespaces whose labels satisfy the selector. If multiple services match, the one with the most available memory capacity is selected.

### Step 2: BrokerService Authorizes the App (CEL Expression)

Once a candidate BrokerService is found, the service's `appSelectorExpression` is evaluated to determine whether the BrokerApp is authorized to bind. This is server-side access control that the BrokerApp cannot bypass.

By default (when `appSelectorExpression` is empty), only BrokerApps in the same namespace as the BrokerService are allowed. See [BrokerService App Selector](brokerservice-appselector.md) for the full CEL reference.

### Bidirectional Validation

Both directions are checked continuously, not just at initial binding:

1. The BrokerApp controller checks that the service's labels still match the app's selector
2. The BrokerService controller checks that the app still passes the CEL expression

If either side no longer matches, the app is unbound and must find a new service.

## Labels: Good Practice

### Choosing Service Labels

Use labels that describe characteristics apps will select on. Avoid labels that are unique to a single service unless you want one-to-one binding.

```yaml
# Good: describes what the service offers
metadata:
  labels:
    env: production
    tier: shared
    region: eu-west-1

# Avoid: too specific, forces apps to name the exact service
metadata:
  labels:
    instance: prod-broker-3
```

### Recommended Label Taxonomy

| Label | Purpose | Example Values |
|---|---|---|
| `env` | Environment isolation | `dev`, `staging`, `production` |
| `tier` | Service level | `shared`, `dedicated`, `premium` |
| `region` | Geographic placement | `eu-west-1`, `us-east-1` |
| `team` | Ownership | `platform`, `payments` |

### Writing Selectors

Start broad, then constrain. An app that only needs a production broker should select on `env: production` without also requiring a specific tier. This gives the platform team flexibility to reassign workloads.

```yaml
# Good: selects on what matters to the app
spec:
  selector:
    matchLabels:
      env: production

# Overly specific: locks the app to one service arrangement
spec:
  selector:
    matchLabels:
      env: production
      tier: shared
      region: eu-west-1
      team: platform
```

### Namespace Strategy

For simple deployments, colocate BrokerService and BrokerApp in the same namespace. The default `appSelectorExpression` (same-namespace only) requires no additional configuration.

For multi-tenant or platform-style deployments, place BrokerServices in a dedicated infrastructure namespace and configure `appSelectorExpression` to control access:

```
Namespace: broker-infra          Namespace: team-a           Namespace: team-b
┌──────────────────────┐         ┌─────────────────┐         ┌──────────────────┐
│ BrokerService        │         │ BrokerApp       │         │ BrokerApp        │
│   labels:            │  <────  │   selector:     │         │   selector:      │
│     env: production  │         │     env: prod   │  ────>  │     env: prod    │
│   appSelectorExpr:   │         └─────────────────┘         └──────────────────┘
│     app.metadata.    │                 ✓ allowed                  ✓ allowed
│     namespace        │
│     .startsWith(     │
│       "team-")       │
└──────────────────────┘
```

### Port Assignment

Each BrokerApp bound to a service receives a unique port, starting at 61616. The port is recorded in `status.service.assignedPort` and in the binding secret. Applications should read connection details from the binding secret rather than hardcoding ports.

## Data Protection

When a BrokerApp or BrokerService is deleted — or when an app's addresses change — messages already on the broker need to be consumed before resources are removed. The operator uses a Kubernetes finalizer (`broker.arkmq.org/addresses-pending-empty`) to coordinate this.

### How It Works

The finalizer is added automatically when an app has owned addresses. It is removed once all those addresses are empty (message count reaches zero). No user action is needed.

**When an app's addresses are removed or its service binding changes:**
The removed addresses are kept on the broker and marked as pending empty. The app stays bound to the service until consumers have received all remaining messages. Once empty, the finalizer is removed and the binding is cleared on the next reconcile.

**When a BrokerApp is deleted:**
The finalizer blocks deletion until all owned addresses are empty. Consumers can continue to read from the addresses during this time. Once all messages are consumed, the finalizer is removed and the app is garbage collected.

**When a BrokerService is deleted:**
The service receives the same finalizer when any bound app has owned content. The service remains until every bound app's addresses are empty and their finalizers are removed. While waiting, the service's `Ready` condition is set to `False` with reason `AddressesPendingEmpty`.

### Observing Progress

The BrokerService status includes a `pendingEmpty` field that lists which apps still have messages to consume and the per-queue message counts:

```yaml
status:
  pendingEmpty:
    - app: orders/order-service@3
      messages:
        ORDERS.NEW::ORDERS.NEW: 42
```

On the BrokerApp side, the `Deployed` condition message includes the addresses that are pending empty:

```
Deployed: False
Reason:   Unbinding
Message:  addresses pending empty: ORDERS.NEW::ORDERS.NEW
```

### Forced Deletion

If you need to bypass the finalizer (for example, in a non-production environment where message loss is acceptable), the `hack/clear-finalizers.sh` script removes finalizers from all BrokerApp and BrokerService resources:

```bash
./hack/clear-finalizers.sh          # dry run — lists finalizers without changing anything
./hack/clear-finalizers.sh --apply  # removes all finalizers, unblocking deletion
```

Resources will then be garbage collected immediately, and any remaining messages will be lost.

## Complete Example

A platform team provisions a shared production broker. Two application teams bind to it with different messaging requirements.

```yaml
# Platform team deploys the service
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerService
metadata:
  name: prod-shared
  namespace: broker-infra
  labels:
    env: production
    tier: shared
spec:
  resources:
    limits:
      memory: 4Gi
  appSelectorExpression: |
    app.metadata.namespace in ["orders", "payments"]
---
# Orders team
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-service
  namespace: orders
spec:
  selector:
    matchLabels:
      env: production
      tier: shared
  sharedAddresses:
    - address: ORDERS.CREATED
    - address: ORDERS.SHIPPED
  capabilities:
    - producerOf:
        - address: ORDERS.CREATED
        - address: ORDERS.SHIPPED
---
# Payments team consumes order events
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: payment-service
  namespace: payments
spec:
  selector:
    matchLabels:
      env: production
      tier: shared
  addresses:
    - address: PAYMENTS.PROCESSED
  capabilities:
    - consumerOf:
        - address: ORDERS.CREATED
          appNamespace: orders
          appName: order-service
      producerOf:
        - address: PAYMENTS.PROCESSED
```

## Kubectl Short Names

Both resources have short names for convenience:

```bash
kubectl get bsvc          # list BrokerServices
kubectl get bapp          # list BrokerApps
```

## Further Reading

- [BrokerService App Selector](brokerservice-appselector.md) — CEL expression reference for access control
