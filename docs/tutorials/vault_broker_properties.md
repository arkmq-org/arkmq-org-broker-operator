---
title: "Injecting secrets into broker properties from Vault"
description: "Using HashiCorp Vault Agent Injector and Banzai Webhook to inject secrets into broker properties from HashiCorp Vault"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 115
toc: true
---

Managing broker configuration in a production environment requires secure secret management. This tutorial demonstrates two approaches for injecting secrets into broker properties from HashiCorp Vault for Apache Artemis brokers managed by the ArkMQ Broker Operator:

1. **HashiCorp Vault Agent Injector** - Official HashiCorp solution using sidecar containers
2. **Banzai Secret Injection Webhook** - Webhook that replaces Vault references in environment variables

Both approaches:
- Use the same Kubernetes auth method with a service account (`vault-broker-sa`)
- Fetch secrets directly from Vault (eliminating the need to store sensitive data in Kubernetes)
- Use the same Vault policy and role for authentication

In this tutorial, we'll:
- Store only the address name `VAULT-TEST` in Vault
- Configure Kubernetes auth for both approaches using the same service account (`vault-broker-sa`)
- **HashiCorp approach**: Use Go templates to construct broker properties via sidecar
- **Banzai approach**: Inject values as environment variables, use `${VAR}` placeholders in broker properties
- Deploy two brokers and compare both injection methods

### Prerequisite

Before you start you need:
- Access to a running Kubernetes cluster environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) running on your laptop will work fine.
- Helm 3 installed for deploying the HashiCorp Vault and Banzai Secret Injection Webhook
- Basic familiarity with Vault concepts

### Start minikube

```{"stage":"init", "id":"minikube_start"}
minikube start --profile tutorialtester --memory=4096 --cpus=2
minikube profile tutorialtester
```
```shell markdown_runner
* [tutorialtester] minikube v1.38.1 on Fedora 43
* Using the kvm2 driver based on user configuration
* Starting "tutorialtester" primary control-plane node in "tutorialtester" cluster
* Configuring bridge CNI (Container Networking Interface) ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: default-storageclass, storage-provisioner

  - Want kubectl v1.35.1? Try 'minikube kubectl -- get pods -A'
* Done! kubectl is now configured to use "tutorialtester" cluster and "default" namespace by default
! Starting v1.39.0, minikube will default to "containerd" container runtime. See #21973 for more info.
! /usr/local/bin/kubectl is version 1.33.3, which may have incompatibilities with Kubernetes 1.35.1.
* minikube profile was successfully set to tutorialtester
```

### Create the namespace

```{"stage":"init"}
kubectl create namespace vault-broker-project
kubectl config set-context --current --namespace=vault-broker-project
```
```shell markdown_runner
namespace/vault-broker-project created
Context "tutorialtester" modified.
```

### Deploy the ArkMQ operator

Go to the root of the operator repo and install it:

```{"stage":"operator-setup", "rootdir":"$initial_dir"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
Client Version: 4.20.8
Kustomize Version: v5.6.0
Kubernetes Version: v1.35.1
customresourcedefinition.apiextensions.k8s.io/activemqartemises.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/brokers.broker.arkmq.org created
serviceaccount/arkmq-org-broker-controller-manager created
role.rbac.authorization.k8s.io/arkmq-org-broker-operator-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-operator-rolebinding created
role.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-rolebinding created
deployment.apps/arkmq-org-broker-controller-manager created
```

Wait for the Operator to start (status: `running`):

```{"stage":"operator-setup", "label":"wait for operator"}
kubectl rollout status deployment/arkmq-org-broker-controller-manager --timeout=600s
```
```shell markdown_runner
Waiting for deployment spec update to be observed...
Waiting for deployment spec update to be observed...
Waiting for deployment "arkmq-org-broker-controller-manager" rollout to finish: 0 out of 1 new replicas have been updated...
Waiting for deployment "arkmq-org-broker-controller-manager" rollout to finish: 0 of 1 updated replicas are available...
deployment "arkmq-org-broker-controller-manager" successfully rolled out
```

### Install HashiCorp Vault

Install HashiCorp Vault in dev mode with the Agent Injector enabled. In production, use a properly configured HashiCorp Vault instance.

```{"stage":"vault-setup", "label":"install hashicorp vault"}
helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update
helm install vault hashicorp/vault --namespace=vault-broker-project --set "server.dev.enabled=true" --set "injector.enabled=true"
```
```shell markdown_runner
"hashicorp" already exists with the same configuration, skipping
Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "jetstack" chart repository
...Successfully got an update from the "hashicorp" chart repository
Update Complete. ⎈Happy Helming!⎈
NAME: vault
LAST DEPLOYED: Thu Mar 26 09:10:58 2026
NAMESPACE: vault-broker-project
STATUS: deployed
REVISION: 1
NOTES:
Thank you for installing HashiCorp Vault!

Now that you have deployed Vault, you should look over the docs on using
Vault with Kubernetes available here:

https://developer.hashicorp.com/vault/docs


Your release is named vault. To learn more about the release, try:

  $ helm status vault
  $ helm get manifest vault
```

Wait for Vault to be ready:

```{"stage":"vault-setup", "label":"wait for vault"}
kubectl wait --for=jsonpath='{.status.readyReplicas}'=1 statefulset/vault --timeout=120s
```
```shell markdown_runner
statefulset.apps/vault condition met
```

### Configure Vault with address name

Store only the address name in Vault. This value will be used by both injection approaches to construct the broker properties:

```{"stage":"vault-setup", "runtime":"bash", "label":"store secrets in vault"}
kubectl exec vault-0 -- vault kv put secret/broker \
  addressName=VAULT-TEST
```
```shell markdown_runner
=== Secret Path ===
secret/data/broker

======= Metadata =======
Key                Value
---                -----
created_time       2026-03-26T08:11:38.053354861Z
custom_metadata    <nil>
deletion_time      n/a
destroyed          false
version            1
```

Verify the secret was stored:

```{"stage":"vault-setup", "runtime":"bash", "label":"verify vault secret"}
kubectl exec vault-0 -- vault kv get secret/broker
```
```shell markdown_runner
=== Secret Path ===
secret/data/broker

======= Metadata =======
Key                Value
---                -----
created_time       2026-03-26T08:11:38.053354861Z
custom_metadata    <nil>
deletion_time      n/a
destroyed          false
version            1

======= Data =======
Key            Value
---            -----
addressName    VAULT-TEST
```

### Create service account for Vault access

Create a service account for authenticating to Vault:

```{"stage":"vault-setup", "runtime":"bash", "label":"create service account"}
kubectl create serviceaccount vault-broker-sa
```
```shell markdown_runner
serviceaccount/vault-broker-sa created
```

### Configure Vault authentication

Enable Kubernetes auth and create a policy for the broker service account:

```{"stage":"vault-setup", "runtime":"bash", "label":"configure vault auth"}
# Enable Kubernetes auth
kubectl exec vault-0 -- vault auth enable kubernetes

# Configure Kubernetes auth to use the service account token
kubectl exec vault-0 -- sh -c 'vault write auth/kubernetes/config \
    kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443"'

# Create a simple policy allowing read access to broker secrets
kubectl exec vault-0 -- sh -c 'vault policy write broker - <<EOF
path "secret/data/broker" {
  capabilities = ["read"]
}
EOF'

# Create a role for the vault-broker-sa service account
kubectl exec vault-0 -- vault write auth/kubernetes/role/broker \
    bound_service_account_names=vault-broker-sa \
    bound_service_account_namespaces=vault-broker-project \
    policies=broker \
    ttl=24h
```
```shell markdown_runner
Success! Enabled kubernetes auth method at: kubernetes/
Success! Data written to: auth/kubernetes/config
Success! Uploaded policy: broker
WARNING! The following warnings were returned from Vault:

  * Role broker does not have an audience configured. While audiences are
  not required, consider specifying one if your use case would benefit from
  additional JWT claim verification.

```

## Approach 1: Using HashiCorp Vault Agent Injector

The Vault Agent Injector is the official HashiCorp solution for injecting secrets. Let's demonstrate this approach by creating a broker using the Vault configuration (`secret/data/broker`) to create an address with the name `VAULT-TEST`.

### Deploy the broker (hashicorp-broker)

Create the broker CR with Vault Agent Injector annotations. The template constructs the broker properties using the address name from Vault with the path `secret/data/broker`:

```{"stage":"agent-injector", "runtime":"bash", "label":"deploy hashicorp broker"}
kubectl apply -f - << EOF
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: hashicorp-broker
spec:
  deploymentPlan:
    annotations:
      vault.hashicorp.com/agent-inject: "true"
      vault.hashicorp.com/role: "broker"
      vault.hashicorp.com/agent-inject-template-vault-broker.properties: |
        {{- with secret "secret/data/broker" -}}
        addressConfigurations.{{ .Data.data.addressName }}.routingTypes=ANYCAST
        addressConfigurations.{{ .Data.data.addressName }}.queueConfigs.{{ .Data.data.addressName }}.address={{ .Data.data.addressName }}
        addressConfigurations.{{ .Data.data.addressName}}.queueConfigs.{{ .Data.data.addressName }}.routingType=ANYCAST
        {{- end -}}
      vault.hashicorp.com/agent-inject-file-broker.properties: "broker.properties"
    podSecurity:
      serviceAccountName: vault-broker-sa
  env:
  - name: JAVA_ARGS_APPEND
    value: "-Dbroker.properties=/amq/extra/secrets/hashicorp-broker-props/broker.properties,/vault/secrets/vault-broker.properties"
EOF
```
```shell markdown_runner
broker.broker.arkmq.org/hashicorp-broker created
```

Wait for the broker to be ready:

```{"stage":"agent-injector", "label":"wait for hashicorp broker"}
kubectl wait Broker hashicorp-broker --for=condition=Ready --namespace=vault-broker-project --timeout=300s
```
```shell markdown_runner
broker.broker.arkmq.org/hashicorp-broker condition met
```

### Verify Vault Agent Injector worked

Check that the Vault agent sidecar was injected:

```{"stage":"agent-injector", "runtime":"bash", "label":"check hashicorp vault sidecar"}
kubectl get pod hashicorp-broker-ss-0 -o jsonpath='{.spec.containers[*].name}' | tee output && grep -q vault-agent output
```
```shell markdown_runner
hashicorp-broker-container vault-agent

```

Verify the broker properties were constructed by the Vault agent template using the address name from Vault:

```{"stage":"agent-injector", "runtime":"bash", "label":"verify hashicorp injected config"}
kubectl exec hashicorp-broker-ss-0 -c hashicorp-broker-container -- cat /vault/secrets/vault-broker.properties
```
```shell markdown_runner
addressConfigurations.VAULT-TEST.routingTypes=ANYCAST
addressConfigurations.VAULT-TEST.queueConfigs.VAULT-TEST.address=VAULT-TEST
addressConfigurations.VAULT-TEST.queueConfigs.VAULT-TEST.routingType=ANYCAST
```

Perfect! The Vault agent template retrieved `addressName=VAULT-TEST` from Vault and constructed the complete broker properties. Now verify that the VAULT-TEST address and queue were created:

```{"stage":"agent-injector", "runtime":"bash", "label":"check hashicorp vault-test queue"}
kubectl exec hashicorp-broker-ss-0 -c hashicorp-broker-container -- /bin/bash -c 'cd amq-broker/bin && ./artemis queue stat --user admin --password admin --url tcp://hashicorp-broker-ss-0:61616' | tee output && grep -q VAULT-TEST output
```
```shell markdown_runner
Connection brokerURL = tcp://hashicorp-broker-ss-0:61616
|NAME              |ADDRESS           |CONSUMER|MESSAGE|MESSAGES|DELIVERING|MESSAGES|SCHEDULED|ROUTING|INTERNAL|
|                  |                  | COUNT  | COUNT | ADDED  |  COUNT   | ACKED  |  COUNT  | TYPE  |        |
|$sys.mqtt.sessions|$sys.mqtt.sessions|   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST|  true  |
|DLQ               |DLQ               |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
|ExpiryQueue       |ExpiryQueue       |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
|VAULT-TEST        |VAULT-TEST        |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/hashicorp-broker-props/broker.properties
```

Excellent! The `VAULT-TEST` queue and address were successfully created using the Vault Agent Injector.

## Approach 2: Using Banzai Secret Injection Webhook

### Install Bank-Vaults Secret Injection Webhook

Install the Bank-Vaults secrets webhook using Helm OCI registry. The webhook uses the same `vault-broker-sa` service account to authenticate to Vault via Kubernetes auth:

```{"stage":"banzai", "runtime":"bash", "label":"install secrets webhook"}
helm install vault-secrets-webhook oci://ghcr.io/bank-vaults/helm-charts/vault-secrets-webhook \
  --namespace=vault-broker-project --set ignoreReleaseNamespace=false --wait
```
```shell markdown_runner
NAME: vault-secrets-webhook
LAST DEPLOYED: Thu Mar 26 09:12:52 2026
NAMESPACE: vault-broker-project
STATUS: deployed
REVISION: 1
TEST SUITE: None
Pulled: ghcr.io/bank-vaults/helm-charts/vault-secrets-webhook:1.22.2
Digest: sha256:4e67105d138a3025c8c75a61934a221186eefdb6db77860d3bf42118ce2f2ec1
```

Wait for the webhook to be ready:

```{"stage":"banzai", "label":"wait for webhook"}
kubectl rollout status deployment/vault-secrets-webhook --timeout=120s
```
```shell markdown_runner
deployment "vault-secrets-webhook" successfully rolled out
```

Ensure the webhook is fully registered and ready to intercept pod creations:

```{"stage":"banzai", "runtime":"bash", "label":"verify webhook registration"}
for i in {1..3}; do sleep 3 && kubectl logs -l app.kubernetes.io/name=vault-secrets-webhook | tee output && grep -q watching output && break || test $i -lt 3 || exit 1; done
```
```shell markdown_runner
time=2026-03-26T08:13:06.250Z level=INFO msg="Listening on https://:8443" app=vault-secrets-webhook
time=2026-03-26T08:13:06.250Z level=INFO msg="watching directory for changes: /var/serving-cert/" app=vault-secrets-webhook
time=2026-03-26T08:13:06.872Z level=INFO msg="Listening on https://:8443" app=vault-secrets-webhook
time=2026-03-26T08:13:06.872Z level=INFO msg="watching directory for changes: /var/serving-cert/" app=vault-secrets-webhook
```

### Create broker properties secret

Create a Kubernetes secret that uses an environment variable for the address name. This variable will be injected from Vault via an environment variable in the broker pod:

```{"stage":"banzai", "runtime":"bash", "label":"create broker properties secret"}
kubectl apply -f - << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: banzai-broker-config-bp
type: Opaque
stringData:
  vault-broker.properties: |
    addressConfigurations.${VAULT_ADDRESS_NAME}.routingTypes=ANYCAST
    addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.address=${VAULT_ADDRESS_NAME}
    addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.routingType=ANYCAST
EOF
```
```shell markdown_runner
secret/banzai-broker-config-bp created
```

Verify the secret was created with environment variable placeholders:

```{"stage":"banzai", "runtime":"bash", "label":"verify secret with env var"}
kubectl get secret banzai-broker-config-bp -o jsonpath='{.data.vault-broker\.properties}' | base64 -d | tee output && grep -q '${VAULT_ADDRESS_NAME}' output
```
```shell markdown_runner
addressConfigurations.${VAULT_ADDRESS_NAME}.routingTypes=ANYCAST
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.address=${VAULT_ADDRESS_NAME}
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.routingType=ANYCAST
```

### Deploy the broker (banzai-broker)

Create the broker CR with an environment variable that gets injected from Vault. The Banzai webhook will replace the `vault:` reference in the environment variable with the actual value from Vault with the path `secret/data/broker`:

```{"stage":"banzai", "runtime":"bash", "label":"deploy banzai broker"}
kubectl apply -f - << EOF
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: banzai-broker
spec:
  deploymentPlan:
    annotations:
      vault.security.banzaicloud.io/vault-addr: "http://vault:8200"
      vault.security.banzaicloud.io/vault-role: "broker"
    extraMounts:
      secrets:
      - banzai-broker-config-bp
    podSecurity:
      serviceAccountName: vault-broker-sa
  env:
  - name: VAULT_ADDRESS_NAME
    value: "vault:secret/data/broker#addressName"
EOF
```
```shell markdown_runner
broker.broker.arkmq.org/banzai-broker created
```

Wait for the broker to be ready:

```{"stage":"banzai", "label":"wait for banzai broker"}
kubectl wait Broker banzai-broker --for=condition=Ready --namespace=vault-broker-project --timeout=300s
```
```shell markdown_runner
broker.broker.arkmq.org/banzai-broker condition met
```

### Verify Secret Injection Webhook worked

Check that the mutating webhook injected the copy-vault-env init container:

```{"stage":"agent-injector", "runtime":"bash", "label":"check hashicorp vault sidecar"}
kubectl get pod banzai-broker-ss-0 -o jsonpath='{.spec.initContainers[*].name}' | tee output && grep -q copy-vault-env output
```
```shell markdown_runner
copy-vault-env banzai-broker-container-init
```

Check the broker properties file which uses the environment variable:

```{"stage":"banzai", "runtime":"bash", "label":"verify banzai broker config"}
kubectl exec banzai-broker-ss-0 -c banzai-broker-container -- /bin/bash -c 'cat /amq/extra/secrets/banzai-broker-config-bp/vault-broker.properties' | tee output && grep -q '${VAULT_ADDRESS_NAME}' output
```
```shell markdown_runner
addressConfigurations.${VAULT_ADDRESS_NAME}.routingTypes=ANYCAST
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.address=${VAULT_ADDRESS_NAME}
addressConfigurations.${VAULT_ADDRESS_NAME}.queueConfigs.${VAULT_ADDRESS_NAME}.routingType=ANYCAST
```

The broker properties still contain the `${VAULT_ADDRESS_NAME}` placeholder, but Artemis will resolve it using the injected environment variable. Now verify that the VAULT-TEST address and queue were created:

```{"stage":"banzai", "runtime":"bash", "label":"check banzai vault-test queue"}
kubectl exec banzai-broker-ss-0 -- /bin/bash -c 'cd amq-broker/bin && ./artemis queue stat --user admin --password admin --url tcp://banzai-broker-ss-0:61616' | tee output && grep -q VAULT-TEST output
```
```shell markdown_runner
Connection brokerURL = tcp://banzai-broker-ss-0:61616
|NAME              |ADDRESS           |CONSUMER|MESSAGE|MESSAGES|DELIVERING|MESSAGES|SCHEDULED|ROUTING|INTERNAL|
|                  |                  | COUNT  | COUNT | ADDED  |  COUNT   | ACKED  |  COUNT  | TYPE  |        |
|$sys.mqtt.sessions|$sys.mqtt.sessions|   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST|  true  |
|DLQ               |DLQ               |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
|ExpiryQueue       |ExpiryQueue       |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
|VAULT-TEST        |VAULT-TEST        |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
Defaulted container "banzai-broker-container" out of: banzai-broker-container, copy-vault-env (init), banzai-broker-container-init (init)
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/banzai-broker-props/broker.properties,/amq/extra/secrets/banzai-broker-config-bp/,/amq/extra/secrets/banzai-broker-config-bp/broker-${STATEFUL_SET_ORDINAL}/
```

Excellent! The `VAULT-TEST` queue bound to the `VAULT-TEST` address was successfully created from the Vault configuration.

### Comparing Both Approaches

Both brokers successfully created the same `VAULT-TEST` queue and address from the same Vault configuration, demonstrating two different injection methods:

**HashiCorp Vault Agent Injector (`hashicorp-broker`):**
- Official HashiCorp solution
- Injects secrets via sidecar container (`vault-agent`)
- Uses `vault-broker-sa` service account for Kubernetes auth
- Supports secret rotation
- Better for production environments
- Properties injected at `/vault/secrets/vault-broker.properties`

**Banzai Secret Injection Webhook (`banzai-broker`):**
- Webhook intercepts pod creation
- Replaces `vault:` references in environment variables
- Uses same `vault-broker-sa` service account for Kubernetes auth
- Good for static secret injection
- Properties mounted at `/amq/extra/secrets/banzai-broker-config-bp/vault-broker.properties`

**Common elements:**
- Both use the same `vault-broker-sa` service account
- Both authenticate via Kubernetes auth method
- Both use the same Vault policy and role
- Both read from the same Vault secret (`secret/data/broker`)

### How it works

This tutorial demonstrates two approaches for injecting the **same Vault secret** (`secret/data/broker` containing `addressName=VAULT-TEST`) into two different broker instances:

#### HashiCorp Vault Agent Injector (`hashicorp-broker`)

1. **Vault Storage**: Address name stored at `secret/data/broker` (`addressName=VAULT-TEST`)
2. **Vault Auth**: Kubernetes auth method with simple policy allowing read access to broker secrets
3. **Vault Agent**: Sidecar container injected into broker pod via annotations
4. **Template Processing**: Agent uses Go template to fetch `addressName` and construct complete broker properties
5. **Artemis Broker**: Loads constructed properties from `/vault/secrets/vault-broker.properties` via `JAVA_ARGS_APPEND`

#### Banzai Secret Injection Webhook (`banzai-broker`)

1. **Vault Storage**: Same address name at `secret/data/broker` (`addressName=VAULT-TEST`)
2. **Vault Auth**: Webhook uses `vault-broker-sa` service account with Kubernetes auth (same as HashiCorp approach)
3. **Environment Variable**: Broker CR defines `VAULT_ADDRESS_NAME` env var with `vault:secret/data/broker#addressName` reference
4. **Banzai Webhook**: Mutating webhook intercepts pod creation, authenticates to Vault using Kubernetes auth, replaces `vault:` reference in env var with actual value `VAULT-TEST`
5. **Kubernetes Secret**: Broker properties use `${VAULT_ADDRESS_NAME}` placeholder
6. **ArkMQ Operator**: Mounts the secret via `extraMounts.secrets`
7. **Artemis Broker**: Loads properties and resolves `${VAULT_ADDRESS_NAME}` to `VAULT-TEST` from environment variable

Both brokers create the same infrastructure from the Vault-injected address name:
- Address `VAULT-TEST` with ANYCAST routing
- Queue `VAULT-TEST` bound to the address
- Queue routing type set to ANYCAST

**Key Differences**:
- **Authentication timing**:
  - HashiCorp: Vault agent authenticates continuously during pod runtime (sidecar container)
  - Banzai: Webhook authenticates once during pod creation (mutating webhook)
- **Secret injection method**:
  - HashiCorp: Template-based construction using Go templates to build complete properties file
  - Banzai: Injects Vault value into environment variable, Artemis resolves `${VAR}` placeholders at runtime
- **Resource usage**:
  - HashiCorp: Additional sidecar container per pod
  - Banzai: Single webhook deployment for all pods

This approach provides:
- **Secure secret management**: Sensitive data never stored in Kubernetes
- **Dynamic configuration**: Change Vault values and restart pods to pick up new configuration
- **Audit trail**: Vault logs all secret access
- **Centralized management**: One Vault instance for all broker configurations
- **Declarative infrastructure**: Queue and address creation through properties instead of manual configuration

### Cleanup

To leave a pristine environment after executing this tutorial, delete the minikube cluster:

```{"stage":"teardown", "requires":"init/minikube_start"}
minikube delete --profile tutorialtester
```
```shell markdown_runner
* Deleting "tutorialtester" in kvm2 ...
* Removed all traces of the "tutorialtester" cluster.
```

### Additional considerations

For production environments, consider:

- **Choose the Right Approach**:
  - Vault Agent Injector: Official HashiCorp solution, better for production with secret rotation support (Approach 1)
  - Banzai Webhook: Simpler setup, good for static secret injection (Approach 2)
- **Vault Authentication**: Both approaches use Kubernetes auth method with the same service account (simplified example shown in tutorial)
- **Vault Policies**: Create specific policies with least privilege access - separate policies per application/team in production
- **TLS**: Enable TLS for Vault communication
- **HA Vault**: Deploy Vault in high-availability mode with proper storage backend
- **Secret Rotation**: Vault Agent Injector supports automatic secret rotation
- **Monitoring**: Monitor webhook/agent injection and Vault access
- **Broker Properties**: Store complete broker configuration in Vault:
  - Security credentials (user passwords, keystore passwords)
  - Connection settings (cluster passwords, connector passwords)
  - Queue and address creation
  - Performance tuning parameters
  - Environment-specific settings

### What can be configured via broker.properties from Vault

The Artemis broker supports extensive configuration through broker.properties:

- **Address Configurations**:
  - `addressConfigurations.ADDRESS_NAME.routingTypes=ANYCAST|MULTICAST`
  - `addressConfigurations.ADDRESS_NAME.queueConfigs.QUEUE_NAME.address=ADDRESS_NAME`
  - `addressConfigurations.ADDRESS_NAME.queueConfigs.QUEUE_NAME.routingType=ANYCAST`
- **Address Settings**: `addressSettings."AddressPattern".expiryDelay=value`
- **Security**: User credentials, LDAP settings
- **Clustering**: Cluster user, cluster password
- **Connectors**: Connector configurations
- **Global Settings**: Memory limits, journal settings
- **Custom Properties**: Any valid Artemis configuration property

This makes it easy to manage environment-specific configurations (dev, staging, production) entirely through Vault.

For more information:
- [Banzai Secret Injection Webhook](https://github.com/bank-vaults/secrets-webhook)
- [HashiCorp Vault Agent Injector](https://developer.hashicorp.com/vault/docs/platform/k8s/injector)
- [HashiCorp Vault](https://www.vaultproject.io/)
- [ArkMQ Operator Documentation](https://arkmq.org/)
- [Apache Artemis Configuration](https://activemq.apache.org/components/artemis/documentation/)
