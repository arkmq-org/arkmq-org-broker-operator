# BrokerService App Selector

## Overview

BrokerService app selector provides flexible access control for BrokerApps using CEL (Common Expression Language). This feature allows you to define rules for which applications can deploy to a specific broker service, enabling multi-tenant isolation, security boundaries, and policy enforcement.

## Use Cases

- **Multi-tenant clusters**: Prevent non-qualifying teams from consuming shared broker resources
- **Environment isolation**: Separate development, staging, and production workloads
- **Resource governance**: Control which teams can access premium or dedicated broker services
- **Security boundaries**: Enforce organizational policies through namespace boundaries and labels

## Configuration

### AppSelectorExpression Field

The `appSelectorExpression` field in `BrokerServiceSpec` contains a CEL expression that determines whether a BrokerApp can use the service.

```yaml
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerService
metadata:
  name: my-broker
  namespace: broker-services
spec:
  appSelectorExpression: |
    app.metadata.namespace.startsWith("team-") &&
    app.metadata.namespace.endsWith("-prod")
```

### Available Variables

CEL expressions have access to:

- **`app`**: The complete BrokerApp object being evaluated
  - All metadata: `app.metadata.name`, `app.metadata.namespace`, `app.metadata.labels`, `app.metadata.annotations`, etc.
  - All spec fields: `app.spec.serviceSelector`, `app.spec.capabilities`, etc.
  - All status fields: `app.status.conditions`, `app.status.service.assignedPort`, etc.

- **`service`**: The complete BrokerService object
  - All metadata: `service.metadata.name`, `service.metadata.namespace`, `service.metadata.labels`, etc.
  - All spec and status fields

#### `NOTE`: Namespaces are cluster resources. Only when the operator service account has a ClusterRole permission to get namespaces will the following namespace variables be accessible.
- **`appNamespace`**: The Namespace object where the app resides
  - Namespace metadata: `appNamespace.metadata.labels`, `appNamespace.metadata.annotations`

- **`serviceNamespace`**: The Namespace object where the service resides
  - Namespace metadata: `serviceNamespace.metadata.labels`, `serviceNamespace.metadata.annotations`

### Default Behavior (Secure by Default)

**Empty or nil (default)**:
- Uses expression: `app.metadata.namespace == service.metadata.namespace`
- Only BrokerApps from the service's own namespace are allowed
- Provides namespace isolation without configuration

```yaml
spec:
  # Omit the field entirely for same-namespace-only access
```

## Common Patterns

### App-Based Selection

#### Same Namespace Only (Default)
```yaml
appSelectorExpression: "app.metadata.namespace == service.metadata.namespace"
```

#### Allow All Namespaces
```yaml
appSelectorExpression: "true"
```

#### Specific Namespaces List
```yaml
appSelectorExpression: |
  app.metadata.namespace in ["team-a-prod", "team-b-prod"]
```

#### Namespace Prefix/Suffix Matching
```yaml
appSelectorExpression: |
  app.metadata.namespace.startsWith("team-") &&
  app.metadata.namespace.endsWith("-prod")
```

#### Label-Based Selection
```yaml
appSelectorExpression: |
  has(app.metadata.labels) &&
  app.metadata.labels["tier"] == "premium"
```

### Namespace-Based Selection

#### Environment-Based Selection
```yaml
appSelectorExpression: |
  has(appNamespace.metadata.labels) && 
  appNamespace.metadata.labels["environment"] == "production"
```

#### Team-Based Multi-Tenancy
```yaml
appSelectorExpression: |
  has(appNamespace.metadata.labels) && 
  has(serviceNamespace.metadata.labels) &&
  appNamespace.metadata.labels["team"] == serviceNamespace.metadata.labels["team"]
```

#### Approval-Based Access
```yaml
appSelectorExpression: |
  has(appNamespace.metadata.annotations) && 
  appNamespace.metadata.annotations["broker-access.arkmq.org/approved"] == "true"
```


## Status Conditions

BrokerApps show app selector matching status through the `Deployed` condition:

### Matches Selector

```yaml
status:
  conditions:
    - type: Deployed
      status: "True"
      reason: Provisioned
```

### Does Not Match Selector

```yaml
status:
  conditions:
    - type: Deployed
      status: "False"
      reason: AppSelectorNoMatch
      message: "app in namespace team-c does not match selector for any service"
```

## Troubleshooting

### App Not Binding to Service

**Symptom**: BrokerApp shows `Deployed: False, Reason: AppSelectorNoMatch`

**Debugging Steps**:

1. **Check the service's expression**:
   ```bash
   kubectl get brokerservice <service-name> -o jsonpath='{.spec.appSelectorExpression}'
   ```

2. **Inspect the app details**:
   ```bash
   kubectl get brokerapp <app-name> -n <namespace> -o yaml
   ```

3. **Check namespace labels** (if using namespace-based selection):
   ```bash
   kubectl get namespace <namespace> -o yaml | grep -A5 labels
   ```

**Common Issues**:

1. **Different namespace (default behavior)**: Service uses default expression (same-namespace only) but app is in a different namespace
2. **Expression doesn't match app**: Expression has specific criteria app doesn't meet
3. **Missing field access**: Use `has()` to check field existence before accessing labels/annotations


## CEL Language Reference

For more information on CEL syntax and capabilities:
- [CEL Language Definition](https://github.com/google/cel-spec)
- [Kubernetes CEL Documentation](https://kubernetes.io/docs/reference/using-api/cel/)
