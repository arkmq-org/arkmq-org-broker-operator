# Status Conditions: BrokerApp and BrokerService

## The Core Design: BoundSpec as a Firewall

The BrokerApp and BrokerService controllers are independent reconciliation
loops that coordinate entirely through status fields. The central mechanism
that makes this safe is **BoundSpec**.

When the app controller successfully validates a BrokerApp's spec, it snapshots
it into `status.boundSpec` along with the generation number. The service
controller reads `boundSpec` — never `spec` — when generating broker
configuration. This creates a firewall: a user can push a broken spec to the
API server and nothing happens to the running broker. The old validated spec
continues to drive configuration until the user fixes the problem.

Everything else in the condition system follows from this:

- **Valid** tells the user whether the current spec passed validation.
- **Deployed** tells the user whether a spec (possibly an older one) is
  running on the broker. When validation fails, Deployed is deliberately
  not updated — it preserves the generation of the spec that is actually
  running.
- **Ready** reflects whether the app is running, like a StatefulSet. If
  the broker is serving this app's config, the app is ready — even if a
  newer spec failed validation. The Valid condition covers spec correctness
  independently.

The generation gap between Valid and Deployed is the signal. If
`Valid.observedGeneration = 5` and `Deployed.observedGeneration = 3`, the
user knows: generation 5 failed validation, generation 3 is still running.

## Lifecycle Walkthrough

Follow a single app from creation through each state transition.

### 1. Creation — no matching service

```
Valid:    True   (gen 1)   ValidationSucceeded
Deployed: False  (gen 1)   NoMatchingService
Ready:    False  (gen 1)
```

The app's spec is structurally valid, but no BrokerService matches its
`serviceSelector`. Nothing is deployed. The app controller will retry
when a matching service appears.

### 2. Service appears — binding

```
Valid:    True   (gen 1)   ValidationSucceeded
Deployed: False  (gen 1)   Binding
Ready:    False  (gen 1)
```

The app controller found a matching service, set `status.service`, and
wrote `status.boundSpec`. The service controller picks up the new app on
its next reconcile, generates the config secret, and pushes it to the
broker. While waiting for the broker to apply it, Deployed shows `Binding`.

### 3. Broker applies config — deployed

```
Valid:    True   (gen 1)   ValidationSucceeded
Deployed: True   (gen 1)   Bound
Ready:    True   (gen 1)
```

The broker reports that it applied the secret version the controller wrote.
The service controller sets `AppsProvisioned=True` and records the app in
`status.provisionedApps` as `namespace/name@1`. The app controller sees
its identity+generation in that list and sets `Deployed=True`.

### 4. Spec update — new generation deployed

The user changes the app's addresses. Kubernetes increments the generation
to 2.

```
Valid:    True   (gen 2)   ValidationSucceeded
Deployed: True   (gen 1)   Bound           ← briefly, while catching up
Ready:    True   (gen 2)                    ← Ready because Deployed=True at any gen
```

The app controller validates generation 2, updates `boundSpec`. The service
controller generates new config. Once the broker applies it and
`provisionedApps` advances to `@2`:

```
Valid:    True   (gen 2)   ValidationSucceeded
Deployed: True   (gen 2)   Bound
Ready:    True   (gen 2)
```

### 5. Bad spec update — validation failure

The user pushes generation 3 with an address that clashes with another app
on the same service.

```
Valid:    False  (gen 3)   AddressTypeError   "address 'orders' already declared by ns/other-app
                                               (reconcile blocked, app continues running at generation 2)"
Deployed: True   (gen 2)   Bound              ← PRESERVED, not updated
Ready:    True   (gen 3)                      ← app is running (like a StatefulSet)
```

The app controller detected the clash in its post-binding validation. It
set `Valid=False` at generation 3 but did not touch the Deployed condition
or `boundSpec`. The service controller continues using the generation 2
spec. The broker is unaffected.

Ready stays True because the app is running — following the Kubernetes
convention where Ready reflects operational state, not whether the latest
desired state has been reached. The Valid condition is the signal that the
new spec has problems.

The generation gap (Valid at 3, Deployed at 2) is immediately visible in
`kubectl get bapp -o wide` or a status query. The Valid message tells the
user exactly what went wrong and which generation is still running.

### 6. Spec fixed — recovery

The user fixes the clash (generation 4).

```
Valid:    True   (gen 4)   ValidationSucceeded
Deployed: True   (gen 2)   Bound              ← briefly, until broker catches up
Ready:    True   (gen 4)
```

Validation passes, `boundSpec` is updated to generation 4. The service
controller picks it up, and once the broker applies:

```
Valid:    True   (gen 4)   ValidationSucceeded
Deployed: True   (gen 4)   Bound
Ready:    True   (gen 4)
```

### 7. Service deleted — unbinding with drain

The BrokerService is deleted. The app has addresses with messages, so the
`addresses-pending-empty` finalizer is present.

```
Valid:    True   (gen 4)   ValidationSucceeded
Deployed: True   (gen 4)   Unbinding          ← still provisioned, but tearing down
Ready:    False  (gen 4)   Unbinding           "Bound service is being deleted"
```

The app controller detected the deletion in `checkServiceAcceptance` and set
`unbindingReason`. Because the finalizer is present, it holds the
`status.service` binding open so the service controller can monitor drain.

The service controller sees `Deployed.reason = Unbinding` via
`isAppUnbinding()`, returns an empty address tracker (all addresses treated
as removed), and monitors message counts through Jolokia. The service's
`status.pendingEmpty` shows per-address counts.

### 8. Drain complete — binding cleared

All messages consumed. The service controller removes the finalizer.

```
Valid:    True   (gen 4)   ValidationSucceeded
Deployed: False  (gen 4)   NoMatchingService
Ready:    False  (gen 4)
```

The app controller's next reconcile sees no finalizer, clears
`status.service` and `status.boundSpec`, and falls back to looking for a
new matching service. If none exists, `Deployed=NoMatchingService`.

## Condition Reference

### BrokerApp

#### Valid

Set on every reconcile at the current generation.

| Status | Reason | Meaning |
|--------|--------|---------|
| True | ValidationSucceeded | Spec passes all checks |
| False | ServiceSelectorError | `spec.serviceSelector` is nil or empty |
| False | AddressTypeError | Address clash with another app, or structural error in addresses/capabilities |

#### Deployed

Tracks whether broker configuration has been applied. **Never overwritten by
a validation failure** — the previous condition is preserved.

| Status | Reason | ObservedGeneration | Meaning |
|--------|--------|--------------------|---------|
| True | Bound | current | Provisioned on the broker |
| True | Unbinding | current | Provisioned but unbinding (drain in progress) |
| False | Binding | current | Bound to service, waiting for broker to apply config |
| False | Unbinding | current | Not provisioned and unbinding |
| False | NoMatchingService | current | No service matches `spec.serviceSelector` |
| False | NoServiceCapacity | current | Matching services have no port capacity |
| False | MatchedServiceNotFound | current | Bound service not in filtered list |
| False | MissingAppCert | current | TLS certificate not found |
| False | ValidationFailed | 0 | Never deployed, current spec invalid |
| False | ResourceError | current | API error |

#### Ready

Follows the Kubernetes convention: reflects whether the app is running,
not whether the latest spec is applied.

| Status | Meaning |
|--------|---------|
| True | Deployed=True at any generation — broker is serving this app's config |
| False (Unbinding) | App is detaching — message gives the reason |
| False | Deployed is False — app is not running on the broker |

### BrokerService

#### Valid

Set on every reconcile at the current generation. Covers service-level
validation (port pool, spec structure).

#### Deployed

Tracks whether the underlying Broker StatefulSet is running.

| Status | Reason | Meaning |
|--------|--------|---------|
| True | Ready | Broker is deployed |
| False | NotReady | Broker not yet deployed |
| False | ValidationFailed | Spec validation failed (preserves previous if exists) |
| False | PortPoolExhausted | No ports available |
| Unknown | ResourceError | API error |

#### AppsProvisioned

The handshake between the service controller and the broker. Confirms the
broker has applied the app configuration secret.

| Status | Reason | Meaning |
|--------|--------|---------|
| True | Synced | Broker's `externalConfig` resource version matches the secret |
| False | WaitingForBroker | Secret written, waiting for broker |
| False | BrokerNotReady | Broker not ready |

When True, `status.provisionedApps` is updated from the secret's annotation.
This is the authoritative record of which apps and generations the broker is
running. The app controller reads this to determine `Deployed=True`.

#### Ready

| Status | Meaning |
|--------|---------|
| True | All other conditions are True |
| False (AddressesPendingEmpty) | Service deleting, waiting for message drain |
| False | Any condition is False |

## Cross-Controller Coordination

The controllers never call each other. They communicate through these status
fields:

| Field | Writer | Reader | Purpose |
|-------|--------|--------|---------|
| app `status.service` | App controller | Service controller (field index) | Binding: which service this app is bound to |
| app `status.boundSpec` | App controller | Service controller | The validated spec to generate config from |
| app `Deployed.reason` | App controller | Service controller (`isAppUnbinding`) | Signals unbinding so drain can begin |
| service `status.provisionedApps` | Service controller | App controller | Which apps+generations the broker confirmed |
| service `status.pendingEmpty` | Service controller | App controller (detail messages) | Per-address drain progress |
| `addresses-pending-empty` finalizer | Service controller (add/remove) | App controller (check presence) | Gates binding teardown until drain completes |

## Debugging

### App stuck at Ready=False

Check conditions in order:

1. **Valid=False**: read the message. If it says "reconcile blocked, app
   continues running at generation N", the app is deployed but the new spec
   is invalid — fix the spec.

2. **Deployed=False**:
   - `NoMatchingService` — check `spec.serviceSelector` labels against
     BrokerService labels in the namespace.
   - `NoServiceCapacity` — services exist but port pools are full.
   - `Binding` — service controller hasn't confirmed config. Check the
     service's `AppsProvisioned` condition.
   - `Unbinding` — app is detaching. Check `Ready.message` for why. Check
     `status.pendingEmpty` on the service for drain progress.
   - `MissingAppCert` — check cert-manager for the app's certificate.

3. **Ready=True but Valid=False** — the app is running an older spec because
   the latest generation failed validation. Read the Valid message, fix the spec.

### App unbinding but not completing

- Check for the `broker.arkmq.org/addresses-pending-empty` finalizer on
  the app. If present, the service controller is waiting for messages to
  drain.
- Check `status.pendingEmpty` on the service for per-address message
  counts. If counts aren't decreasing, consumers may be offline.
- If the finalizer is present but the service was force-deleted, the
  finalizer may need manual removal.

### AppsProvisioned=False on the service

Follow the reason and message:
- `WaitingForBroker` — secret written but broker hasn't applied it.
  Check Broker CR status and pod readiness.
- `externalConfig version mismatch` — broker applied a different secret
  version. Usually resolves on the next config reload.
- `pending externalConfig` — broker hasn't reported any applied config.
  Check that the broker pod is running.
- `BrokerNotReady` — broker pod not ready. Check pod events and logs.

### ProvisionedApps shows old generation

During drain, the service controller holds the generation at the
previously-applied value. It advances only after all pending addresses
are fully drained. Check `status.pendingEmpty`.

### Two apps claiming the same address

The second app to reconcile gets `Valid=False` with `AddressTypeError`
and a message naming the clash. Its Deployed condition is preserved (old
spec still runs), and the service controller continues using the old
`boundSpec` without the clash.
