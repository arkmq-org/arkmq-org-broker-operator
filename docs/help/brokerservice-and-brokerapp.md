# BrokerService and BrokerApp

## Overview

BrokerService and BrokerApp are the two custom resources that together provide messaging as a service. A BrokerService provisions and manages a message broker. A BrokerApp describes the messaging requirements of an application and binds to a matching BrokerService to have those requirements fulfilled.

The relationship between them is intentionally decoupled: applications declare what they need, services declare what they offer, and Kubernetes labels connect the two. This separation allows platform teams to manage broker infrastructure independently from the application teams that consume it.

```
┌─────────────────────┐         label selector           ┌─────────────────────┐
│     BrokerApp       │ ──────────────────────────────>  │   BrokerService     │
│                     │                                  │                     │
│  spec.              │    matches service labels        │  metadata.labels:   │
│   serviceSelector:  │                                  │    env: production  │
│    matchLabels:     │                                  │    tier: shared     │
│      env: production│                                  │                     │
│                     │                                  │  spec.resources:    │
│  spec.capabilities: │                                  │    limits:          │
│    - producerOf:    │                                  │      memory: 2Gi    │
│      - address: Q1  │  <─── binding secret ──────────  │                     │
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

The status also lists `provisionedApps` (apps successfully configured) and `rejectedApps` (apps that failed validation, with reasons).

## BrokerApp

A BrokerApp describes an application's messaging requirements: which addresses it produces to, which it consumes from, and which BrokerService should host them.

```yaml
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-processor
  namespace: orders
spec:
  serviceSelector:
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
| `serviceSelector` | Label selector to find matching BrokerServices (required, must be non-empty) |
| `addresses` | Addresses owned by this app. Private by default; opt-in to sharing via `producerOfAppSelector`/`consumerOfAppSelector` |
| `capabilities` | Declares produce/consume permissions using `producerOf` and `consumerOf` |
| `resources` | Resource requirements for this app's share of broker capacity |

### Addresses and Sharing

Addresses declared under `addresses` have a lifecycle tied to the app, independent of their reference from capabilities. An address is **private by default** — other apps cannot reference it.

To share an address, add a `producerOfAppSelector` and/or `consumerOfAppSelector` CEL expression. The expression is evaluated against the requesting app to determine if it is allowed to reference the address in that role:

```yaml
# App "order-service" owns a shared work queue
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-service
  namespace: orders
spec:
  serviceSelector:
    matchLabels:
      env: production
  addresses:
    - address: ORDERS.FULFILLMENT
      consumerOfAppSelector: "app.metadata.name == 'warehouse' && app.metadata.namespace == 'fulfillment'"
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
  serviceSelector:
    matchLabels:
      env: production
  capabilities:
    - consumerOf:
        - address: ORDERS.FULFILLMENT
          appNamespace: orders
          appName: order-service
```

### Address Sharing in Detail

Sharing is opt-in, per-address, and per-role. Both sides must agree: the address owner declares who may reference it via a CEL selector, and the referencing app names the owner.

#### Owner Side: CEL Selectors

Each `AddressType` has two optional CEL selector fields:

| Field | Meaning |
|---|---|
| `producerOfAppSelector` | CEL expression selecting apps allowed to **produce** to this address |
| `consumerOfAppSelector` | CEL expression selecting apps allowed to **consume** from this address |

The expression is evaluated with the requesting app as the `app` variable (the same variable available in `appSelectorExpression`). Common fields: `app.metadata.name`, `app.metadata.namespace`, `app.metadata.labels`.

Selectors are **role-specific**: a `consumerOfAppSelector` does not authorize producing, and vice versa.

#### Requester Side: `appName` / `appNamespace`

To reference another app's address, set `appName` (and optionally `appNamespace`) on the address ref in `capabilities[].producerOf` or `capabilities[].consumerOf`:

```yaml
capabilities:
  - consumerOf:
      - address: ORDERS.FULFILLMENT
        appNamespace: orders       # owner's namespace (omit if same namespace)
        appName: order-service     # owner's name
```

When `appNamespace` is omitted, it defaults to the **requester's own namespace**.

#### Validation Rules

Both sides are validated at reconcile time:

1. The referenced app must exist and be provisioned on the same BrokerService
2. The address must exist in the referenced app's `addresses`
3. The address must have a matching selector (`producerOfAppSelector` or `consumerOfAppSelector`)
4. The CEL expression must evaluate to `true` for the requesting app
5. Routing type (`pubSub`/`subscriptions`) must be consistent between owner and requester

#### Sharing Patterns

**Single named consumer:**

```yaml
addresses:
  - address: work-queue
    consumerOfAppSelector: "app.metadata.name == 'worker-app'"
```

**All apps in a namespace may consume:**

```yaml
addresses:
  - address: events
    consumerOfAppSelector: "app.metadata.namespace == 'team-b'"
```

**Bidirectional sharing with a specific app:**

```yaml
addresses:
  - address: commands
    producerOfAppSelector: "app.metadata.name == 'control-plane'"
    consumerOfAppSelector: "app.metadata.name == 'control-plane'"
```

**Multiple consumers across namespaces:**

```yaml
addresses:
  - address: notifications
    pubSub: true
    consumerOfAppSelector: >-
      (app.metadata.name == 'dashboard' && app.metadata.namespace == 'team-a')
      || app.metadata.namespace == 'team-b'
```

**Label-based sharing:**

```yaml
addresses:
  - address: audit-events
    consumerOfAppSelector: "has(app.metadata.labels.audit) && app.metadata.labels.audit == 'true'"
```

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

The BrokerApp's `spec.serviceSelector` is a standard Kubernetes [label selector](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors). It supports both `matchLabels` and `matchExpressions`:

```yaml
# Simple label matching
spec:
  serviceSelector:
    matchLabels:
      env: production

# Set-based matching
spec:
  serviceSelector:
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
  serviceSelector:
    matchLabels:
      env: production

# Overly specific: locks the app to one service arrangement
spec:
  serviceSelector:
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
│   labels:            │  <────  │ serviceSelector:│         │ serviceSelector: │
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
  serviceSelector:
    matchLabels:
      env: production
      tier: shared
  addresses:
    - address: ORDERS.CREATED
      producerOfAppSelector: "app.metadata.name == 'payment-service' && app.metadata.namespace == 'payments'"
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
  serviceSelector:
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
