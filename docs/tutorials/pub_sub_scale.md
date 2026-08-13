---
title: "MQTT5 Pub/Sub with AMQP Federation and mTLS"
description: "Deploy a 2-node federated broker pair with an MQTT5 TLS acceptor and a Camel-based producer/consumer secured by mutual TLS"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 120
toc: true
---

This tutorial walks through deploying a publish/subscribe topology where:

- A **Camel producer** (`camel-jms-app:mqtt`, `APP_ROLE=producer`) publishes messages to the `COMMANDS` MQTT5 topic on any broker over **mutual TLS**.
- A **Camel consumer** (`camel-jms-app:mqtt`, `APP_ROLE=consumer`) subscribes to `COMMANDS`, appends `[PROCESSED BY APP]` to each message, and re-publishes it to the same topic.
- **AMQP federation** links the two broker pods so that a message published to either broker is forwarded to consumers on both.
- All MQTT5 traffic is secured with **mTLS**: the broker presents a server certificate and requires a client certificate signed by the same CA.

> **_NOTE:_** This tutorial requires a running Kubernetes cluster with **cert-manager** installed. All client workloads run as Deployments inside the cluster using the `quay.io/rh-ee-vnachiap/camel-jms-app:mqtt` image.

---

### How it works

The diagram below shows the key components and how they interact:

```
  [producer — APP_ROLE=producer]         [consumer — APP_ROLE=consumer]
        │                                         │
        │  ssl://pub-sub-broker:8883              │  ssl://pub-sub-broker:8883
        ▼                                         ▼
 ┌──────────────────────────────────────────────────────────────┐
 │           Load-balanced Service (port 8883, MQTT5/TLS)       │
 └──────────────┬───────────────────────────────┬──────────────┘
                │                               │
                ▼                               ▼
     ┌─────────────────────┐         ┌─────────────────────┐
     │  pub-sub-broker-ss-0│◄───────►│  pub-sub-broker-ss-1│
     │   (broker-0)        │  AMQP   │   (broker-1)        │
     │                     │ federat.│                     │
     └─────────────────────┘         └─────────────────────┘
```

**MQTT5 with mTLS:** each broker pod exposes an MQTT5 acceptor on port 8883 secured with TLS. Both the broker and the Camel clients present certificates signed by a shared cert-manager CA. The broker verifies the client certificate (`needClientAuth=true`), and the Camel client verifies the broker certificate via the same CA.

**AMQP federation:** each broker opens an outbound AMQP connection to the *other* pod's headless DNS name. A federation policy mirrors the `COMMANDS` address, so every message produced on either broker is forwarded to the other broker's consumers.

---

### Prerequisites

- A running Kubernetes cluster (for example [Minikube](https://minikube.sigs.k8s.io/docs/start/) or [CRC](https://www.redhat.com/fr/blog/codeready-containers))

- `kubectl` configured to point at the cluster

- **cert-manager** — installed as part of this tutorial (see [Install cert-manager](#install-cert-manager) below). If your cluster already has cert-manager in the `cert-manager` namespace you can skip that step.

### Start minikube

```{"stage":"init", "id":"minikube_start"}
minikube start --profile pub-sub-tutorial --memory=4096 --cpus=2
minikube profile pub-sub-tutorial
kubectl config use-context pub-sub-tutorial
minikube addons enable metrics-server --profile pub-sub-tutorial
```

### Install cert-manager

cert-manager is required for TLS certificate issuance in Step 3. Install it using the official static manifest — no Helm required:

```bash {"stage":"install_cert",  "label":"Install cert-manager"}
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.21.1/cert-manager.yaml
kubectl wait deployment.apps/cert-manager -n cert-manager --for=condition=Available --timeout=300s
kubectl wait deployment.apps/cert-manager-webhook -n cert-manager --for=condition=Available --timeout=300s
```

### Run operator

- Create the namespace `pub-sub-tutorial` and set it as the default for all subsequent `kubectl` commands:

```bash {"stage":"init", "id":"create_namespace", "runtime":"bash", "label":"Create namespace", "id":"create_namespace"}
kubectl create namespace pub-sub-tutorial --dry-run=client -o yaml | kubectl apply -f -
kubectl config set-context --current --namespace=pub-sub-tutorial
```

- Deploy the arkmq-org operator into the `pub-sub-tutorial` namespace:

```{"stage":"init", "id":"deploy_operator", "rootdir":"$initial_dir", "label":"Deploy operator"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
customresourcedefinition.apiextensions.k8s.io/brokerclusters.broker.arkmq.org created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
serviceaccount/arkmq-org-broker-controller-manager created
role.rbac.authorization.k8s.io/arkmq-org-broker-operator-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-operator-rolebinding created
role.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-rolebinding created
deployment.apps/arkmq-org-broker-controller-manager created
./deploy/install_opr.sh: line 7: oc: command not found
Warning: unrecognized format "int32"
Warning: unrecognized format "int64"
```

- Wait for the operator to be ready:

```{"stage":"init", "id":"wait_operator", "label":"Wait for operator"}
kubectl rollout status deployment/arkmq-org-broker-controller-manager --timeout=600s
```

- Verify the ingress domain. The broker CR exposes the MQTT acceptor and management console via Ingress. On minikube the operator **requires** `spec.ingressDomain` to be set or it will reject the CR with `InvalidIngressSettings`. The broker CR in Step 4 sets it automatically using `minikube ip`:

```bash {"stage":"init", "id":"set_ingress_domain", "label":"Set ingress domain", "runtime":"bash"}
echo "INGRESS_DOMAIN=$(minikube ip --profile pub-sub-tutorial).nip.io"
```

---

### Step 1 — Deploy the JAAS authentication Secret

The broker uses JAAS `PropertiesLoginModule` for authentication. This Secret
provides three files that are mounted into every broker pod:

- **`login.config`** — chains two login modules: the operator's built-in one (so the operator can connect to the management console) and an app-specific one that reads the files below.
- **`users.properties`** — defines the users and their passwords. There are three users: `producer`, `consumer`, and `control-plane`.
- **`roles.properties`** — maps users to roles that the broker's `securityRoles` RBAC uses to grant send/consume permissions on the `COMMANDS` address.

```bash {"stage": "Deploy_JAAS", "label": "Deploy the JAAS authentication Secret", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: pub-sub-jaas-config
  namespace: pub-sub-tutorial
stringData:
  login.config: |
    activemq {
      // ensure the operator can connect to the mgmt console by referencing the existing properties config
      org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
        org.apache.activemq.jaas.properties.user="artemis-users.properties"
        org.apache.activemq.jaas.properties.role="artemis-roles.properties"
        baseDir="/home/jboss/amq-broker/etc";

      // app specific users and roles
      org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule sufficient
        reload=true
        debug=true
        org.apache.activemq.jaas.properties.user="users.properties"
        org.apache.activemq.jaas.properties.role="roles.properties";
    };
  users.properties: |
    control-plane=passwd
    producer=passwd
    consumer=passwd
  roles.properties: |
    # used by the AMQP federation links between broker pods
    control-plane=control-plane

    # RBAC for the COMMANDS address
    producers=producer
    consumers=consumer
EOF
```

**Role design explained:**

| Role | Members | Purpose |
|------|---------|---------|
| `producers` | `producer` | Allowed to send to `COMMANDS`. |
| `consumers` | `consumer` | Allowed to send to and consume from `COMMANDS`, and create/delete durable and non-durable queues. MQTT5 subscriptions create durable queues; the consumer must also be able to publish (the bridge route re-publishes processed messages). |
| `control-plane` | `control-plane` | Used by the AMQP federation links. Has permission to create durable queues and consume from the internal federation addresses. |

---

### Step 2 — Deploy the logging ConfigMap

This ConfigMap sets `TRACE` level logging on the JAAS and configuration packages, making it easy to see authentication decisions and broker property loading in the pod logs.

````bash {"stage": "Deploy_logging_configmap", "label": "Deploy the logging ConfigMap", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-logging-config
  namespace: pub-sub-tutorial
data:
  logging.properties: |
    appender.stdout.name = STDOUT
    appender.stdout.type = Console
    rootLogger = info, STDOUT
    logger.activemq.name=org.apache.activemq.artemis.core.config.impl.ConfigurationImpl
    logger.activemq.level=TRACE
    logger.jaas.name=org.apache.activemq.artemis.spi.core.security.jaas
    logger.jaas.level=TRACE
    logger.rest.name=org.apache.activemq.artemis.core
    logger.rest.level=INFO
EOF
````

---

### Step 3 — Issue TLS certificates with cert-manager

This step creates the CA and the two leaf certificates needed for mTLS:

- **A self-signed CA** — a `ClusterIssuer` that signs its own root, then a CA-backed `ClusterIssuer` that signs leaf certificates.
- **A broker server certificate** — presented by every broker pod on the MQTT5 acceptor. Its DNS SANs cover the headless service names for both pods (so federation links can verify it) and the load-balanced Service name (so Camel clients can verify it). cert-manager also generates a JKS keystore and truststore inside the broker cert Secret.
- **A client certificate** — presented by the Camel producer and consumer when connecting to the broker. The broker requires a valid client certificate (`needClientAuth=true`). cert-manager also generates a JKS keystore (key + cert) and truststore (CA) inside the client cert Secret so the Camel app can read them directly.
- **A broker SSL Secret** — four literal keys (`keyStorePath`, `keyStorePassword`, `trustStorePath`, `trustStorePassword`) that the operator reads to configure the MQTT acceptor's JKS keystores.

#### 3a — Create the self-signed root and CA issuer

```bash {"stage": "Deploy_TLS_CA", "label": "Create the self-signed CA and issuer", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: pub-sub-selfsigned-issuer
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: pub-sub-root-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: pub-sub-root-ca
  secretName: pub-sub-root-ca-secret
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: pub-sub-selfsigned-issuer
    kind: ClusterIssuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: pub-sub-ca-issuer
spec:
  ca:
    secretName: pub-sub-root-ca-secret
EOF
```

Wait for the CA certificate to be issued:

```bash {"stage": "Wait_TLS_CA", "label": "Wait for CA certificate", "runtime":"bash"}
kubectl wait certificate/pub-sub-root-ca \
  --for=condition=Ready \
  --namespace=cert-manager \
  --timeout=60s
```

#### 3b — Create the JKS keystore password Secret

cert-manager will embed a JKS keystore into the broker certificate Secret. It needs a password to encrypt the keystore. This Secret provides that password:

```bash {"stage": "Deploy_JKS_Password", "label": "Create the JKS keystore password Secret", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: pub-sub-jks-password
  namespace: pub-sub-tutorial
stringData:
  password: changeit
EOF
```

#### 3c — Issue the broker server certificate

The broker certificate's DNS SANs cover:
- `pub-sub-broker` — the load-balanced Service (used by Camel clients)
- `pub-sub-broker-ss-0.pub-sub-broker-hdls-svc` and `pub-sub-broker-ss-1.pub-sub-broker-hdls-svc` — the headless DNS names (used by the AMQP federation links)

The `keystores.jks` block instructs cert-manager to also generate a `keystore.jks` file (the broker's key + cert) and a `truststore.jks` file (the CA cert) inside `pub-sub-broker-cert-secret`, encrypted with the password above. The broker image reads these standard JKS files natively — no extra libraries needed.

```bash {"stage": "Deploy_Broker_Cert", "label": "Issue the broker server certificate", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: pub-sub-broker-cert
  namespace: pub-sub-tutorial
spec:
  secretName: pub-sub-broker-cert-secret
  commonName: pub-sub-broker
  dnsNames:
    - pub-sub-broker
    - pub-sub-broker.pub-sub-tutorial.svc
    - pub-sub-broker.pub-sub-tutorial.svc.cluster.local
    - pub-sub-broker-ss-0.pub-sub-broker-hdls-svc
    - pub-sub-broker-ss-0.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local
    - pub-sub-broker-ss-1.pub-sub-broker-hdls-svc
    - pub-sub-broker-ss-1.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local
  issuerRef:
    name: pub-sub-ca-issuer
    kind: ClusterIssuer
    group: cert-manager.io
  keystores:
    jks:
      create: true
      passwordSecretRef:
        key: password
        name: pub-sub-jks-password
EOF
```

Wait for the broker certificate to be issued:

```bash {"stage": "Wait_Broker_Cert", "label": "Wait for broker certificate", "runtime":"bash"}
kubectl wait certificate/pub-sub-broker-cert \
  --for=condition=Ready \
  --namespace=pub-sub-tutorial \
  --timeout=60s
```

#### 3d — Issue the client certificate

The `keystores.jks` block instructs cert-manager to embed a `keystore.jks` (client key + cert chain) and a `truststore.jks` (the signing CA cert) directly into `pub-sub-client-cert-secret`. The Camel app reads these JKS files natively — no conversion or extra libraries needed.

```bash {"stage": "Deploy_Client_Cert", "label": "Issue the client certificate", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: pub-sub-client-cert
  namespace: pub-sub-tutorial
spec:
  secretName: pub-sub-client-cert-secret
  commonName: camel-mqtt-client
  issuerRef:
    name: pub-sub-ca-issuer
    kind: ClusterIssuer
    group: cert-manager.io
  keystores:
    jks:
      create: true
      passwordSecretRef:
        key: password
        name: pub-sub-jks-password
EOF
```

Wait for the client certificate to be issued:

```bash {"stage": "Wait_Client_Cert", "label": "Wait for client certificate", "runtime":"bash"}
kubectl wait certificate/pub-sub-client-cert \
  --for=condition=Ready \
  --namespace=pub-sub-tutorial \
  --timeout=60s
```

#### 3e — Create the broker SSL Secret

The operator's `sslSecret` mechanism expects a Secret with four literal keys pointing at the keystore and truststore paths inside the pod, plus their passwords. cert-manager generates `keystore.jks` (the broker's key + cert) and `truststore.jks` (the CA cert) directly into `pub-sub-broker-cert-secret`, which is mounted at `/amq/extra/secrets/pub-sub-broker-cert-secret/` inside the pod.

```bash {"stage": "Deploy_SSL_Secret", "label": "Create the broker SSL Secret", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: pub-sub-ssl-secret
  namespace: pub-sub-tutorial
stringData:
  keyStorePath: /amq/extra/secrets/pub-sub-broker-cert-secret/keystore.jks
  keyStorePassword: changeit
  trustStorePath: /amq/extra/secrets/pub-sub-broker-cert-secret/truststore.jks
  trustStorePassword: changeit
EOF
```

---

### Step 4 — Deploy the broker

This deploys a 2-pod `ActiveMQArtemis` broker with:

- **No persistence** and **no native Artemis cluster** — the two pods are independent brokers linked only by AMQP federation.
- **MQTT5 acceptor with mTLS** — listens on port 8883. The broker presents the cert-manager-issued server certificate and requires a valid client certificate signed by the same CA (`needClientAuth=true`).
- **AMQP federation** — each broker connects outbound to the other pod's headless service DNS name. Messages published to either broker are forwarded to the other, so all consumers receive every message regardless of which broker they are on.
- **Metrics plugin** — exposes Prometheus metrics at `http://<pod-hostname>:8161/metrics` (bound to the headless service DNS name), used for verification.

> **How the federation properties work:** the `broker-0.` and `broker-1.` property prefixes route each property to the matching pod's `broker.properties` file. **All `AMQPConnections.target.*` properties — including credentials, retry settings, and the federation policy — are placed under the same pod prefix as the URI.** This is critical: if credentials live in the shared file and the URI lives in the per-pod file, Artemis creates two separate `AMQPConnections.target` objects and the per-pod one (URI only, no credentials) wins. The federation URIs point at port `5672`, which is the dedicated `amqp` acceptor (`protocols=AMQP`) — the default CORE acceptor on `61616` does not advertise SASL `PLAIN`, so federation connections would authenticate as `anonymous` and be silently rejected after 60 seconds. `${CR_NAME}` and `${STATEFUL_SET_ORDINAL}` are **not** expanded inside `.properties` file values — the operator expands `${STATEFUL_SET_ORDINAL}` only in the `-Dbroker.properties=` JVM path, not in property values themselves.

> **Why FQDNs and `reconnectAttempts=-1`:** StatefulSet pods start sequentially, so when `ss-0` first tries to connect to `ss-1` the target DNS entry may not exist yet. FQDNs (`<name>.svc.cluster.local`) are used because the JVM's Netty resolver does not apply Kubernetes search-domain expansion, so short names can fail to resolve. `retryInterval=1000` reconnects every 1 second; `reconnectAttempts=-1` retries indefinitely. Together they guarantee the federation mesh forms as soon as both pods are ready, without manual intervention.

```bash {"stage": "Deploy_Broker", "label": "Deploy the broker", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerCluster
metadata:
  name: pub-sub-broker
  namespace: pub-sub-tutorial
spec:
  ingressDomain: $(minikube ip --profile pub-sub-tutorial).nip.io
  deploymentPlan:
    size: 2
    persistenceEnabled: false
    clustered: false
    enableMetricsPlugin: true
    extraMounts:
      secrets:
        - pub-sub-jaas-config
        - pub-sub-broker-cert-secret
      configMaps:
        - my-logging-config
  console:
    expose: true
  acceptors:
    - name: mqtt
      port: 8883
      protocols: MQTT
      sslEnabled: true
      sslSecret: pub-sub-ssl-secret
      needClientAuth: true
      expose: true
    - name: amqp
      port: 5672
      protocols: AMQP
  brokerProperties:
    # address config: MULTICAST = pub/sub topic behaviour
    - addressConfigurations.COMMANDS.routingTypes=MULTICAST

    # rbac
    - securityRoles.COMMANDS.producers.send=true
    - securityRoles.COMMANDS.consumers.send=true
    - securityRoles.COMMANDS.consumers.consume=true
    - securityRoles.COMMANDS.consumers.createNonDurableQueue=true
    - securityRoles.COMMANDS.consumers.deleteNonDurableQueue=true
    # MQTT5 clients create durable subscription queues by default
    - securityRoles.COMMANDS.consumers.createDurableQueue=true
    - securityRoles.COMMANDS.consumers.deleteDurableQueue=true

    # control-plane rbac (used by federation links)
    - securityRoles.COMMANDS.control-plane.createDurableQueue=true
    - securityRoles.COMMANDS.control-plane.deleteDurableQueue=true
    - securityRoles.COMMANDS.control-plane.consume=true
    - securityRoles.COMMANDS.control-plane.send=true

    # federation internal address permissions
    - 'securityRoles."\$ACTIVEMQ_ARTEMIS_FEDERATION.#".control-plane.createNonDurableQueue=true'
    - 'securityRoles."\$ACTIVEMQ_ARTEMIS_FEDERATION.#".control-plane.createAddress=true'
    - 'securityRoles."\$ACTIVEMQ_ARTEMIS_FEDERATION.#".control-plane.consume=true'
    - 'securityRoles."\$ACTIVEMQ_ARTEMIS_FEDERATION.#".control-plane.send=true'

    # AMQP federation: each broker's complete connection config is self-contained
    # under its own pod prefix.  All AMQPConnections.target.* properties MUST be
    # under the same prefix as the URI — if credentials live in the shared file and
    # the URI lives in the per-pod file, Artemis creates two separate connection
    # objects and the per-pod one (with the URI but no credentials) wins, causing
    # every federation connection to authenticate as anonymous and be rejected.
    - broker-0.AMQPConnections.target.uri=tcp://pub-sub-broker-ss-1.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local:5672
    - broker-0.AMQPConnections.target.user=control-plane
    - broker-0.AMQPConnections.target.password=passwd
    - broker-0.AMQPConnections.target.retryInterval=1000
    - broker-0.AMQPConnections.target.reconnectAttempts=-1
    - broker-0.AMQPConnections.target.autostart=true
    - broker-0.AMQPConnections.target.federations.peerN.localAddressPolicies.forCommands.includes.justCommands.addressMatch=COMMANDS

    - broker-1.AMQPConnections.target.uri=tcp://pub-sub-broker-ss-0.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local:5672
    - broker-1.AMQPConnections.target.user=control-plane
    - broker-1.AMQPConnections.target.password=passwd
    - broker-1.AMQPConnections.target.retryInterval=1000
    - broker-1.AMQPConnections.target.reconnectAttempts=-1
    - broker-1.AMQPConnections.target.autostart=true
    - broker-1.AMQPConnections.target.federations.peerN.localAddressPolicies.forCommands.includes.justCommands.addressMatch=COMMANDS
EOF
```

Wait for both broker pods to be ready:

```bash {"stage": "Wait_For_Broker", "label": "Wait for broker to be ready", "runtime":"bash"}
kubectl wait BrokerCluster pub-sub-broker \
  --for=condition=Ready \
  --namespace=pub-sub-tutorial \
  --timeout=240s
```

Verify both pods are running:

```bash
kubectl get pods -n pub-sub-tutorial -l ActiveMQArtemis=pub-sub-broker
```

Expected output:
```
NAME                  READY   STATUS    RESTARTS   AGE
pub-sub-broker-ss-0   1/1     Running   0          ...
pub-sub-broker-ss-1   1/1     Running   0          ...
```

---

### Step 5 — Deploy the load-balanced Service

The operator creates per-pod and headless services automatically, but the Camel producer and consumer need a single **load-balanced** entry point so that their connection is distributed across the two broker pods. Either pod can accept any client — the AMQP federation layer ensures every message reaches consumers on both brokers regardless of which pod the producer connected to.

````bash {"stage": "Deploy_load_balancer", "label": "Deploy the load-balanced Service", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: pub-sub-broker
  namespace: pub-sub-tutorial
spec:
  selector:
    ActiveMQArtemis: pub-sub-broker
  ports:
    - port: 8883
      targetPort: 8883
EOF
````

> Port `8883` is the standard MQTT5-over-TLS port and maps directly to the MQTT acceptor inside the pod. The Camel app connects to `ssl://pub-sub-broker:8883`, matching the `BROKER_HOST=pub-sub-broker` and `BROKER_PORT=8883` environment variables set in the Deployments.

---

### Step 6 — Deploy the consumer

The consumer Deployment runs `camel-jms-app:mqtt` with `APP_ROLE=consumer`. When started in this role, the Camel route subscribes to `CONSUMER_QUEUE` (`COMMANDS`), appends `[PROCESSED BY APP]` to each message body, and re-publishes the result to `PRODUCER_QUEUE` (also `COMMANDS`). This creates a visible processing trail in the broker metrics.

**How mTLS and authentication are wired:**

The `camel-jms-app:mqtt` image reads its SSL configuration from `application.properties` baked into the JAR. The SSL property keys use a dotted format (`com.ibm.ssl.keyStore`, etc.) that cannot be overridden via environment variables. Instead, an **init container** writes a `config/application.properties` file into an `emptyDir` volume that is mounted at `/deployments/config/` inside the app container. Quarkus reads this override file at startup with higher priority than the baked-in one.

The init container uses the same `camel-jms-app:mqtt` image (which includes the JDK) — no extra images are needed.

cert-manager generates `keystore.jks` (client key + cert) and `truststore.jks` (CA cert) directly into `pub-sub-client-cert-secret` via the `keystores.jks` block in the `Certificate` resource. The app reads these JKS files natively — no conversion or extra libraries needed.

One Secret and one `emptyDir` are used:
- `pub-sub-client-cert-secret` at `/app/tls/client` — provides `keystore.jks` (client key+cert) and `truststore.jks` (CA). The truststore is also projected separately under `/app/tls/ca` so the original `trustStore` path in `application.properties` can be overridden alongside the keystore.
- `config-dir` (`emptyDir`) at `/deployments/config` — the init container writes `application.properties` here; Quarkus reads it at startup.

````bash {"stage": "Deploy_Consumers", "label": "Deploy the consumer", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: consumer
  namespace: pub-sub-tutorial
  labels:
    app: consumer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: consumer
  template:
    metadata:
      labels:
        app: consumer
    spec:
      initContainers:
        - name: write-config
          image: quay.io/rh-ee-vnachiap/camel-jms-app:mqtt
          command: ["sh", "-c"]
          args:
            - |
              mkdir -p /config-dir/config
              printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.keyStore]=/app/tls/client/keystore.jks' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.keyStoreType]=JKS' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.keyStorePassword]=changeit' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.trustStore]=/app/tls/client/truststore.jks' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.trustStoreType]=JKS' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.trustStorePassword]=changeit' \
                'camel.component.paho-mqtt5.user-name=consumer' \
                'camel.component.paho-mqtt5.password=passwd' \
                > /config-dir/config/application.properties
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            runAsNonRoot: true
          volumeMounts:
            - name: config-dir
              mountPath: /config-dir
      containers:
        - name: consumer
          image: quay.io/rh-ee-vnachiap/camel-jms-app:mqtt
          imagePullPolicy: Always
          env:
            - name: APP_ROLE
              value: consumer
            - name: BROKER_HOST
              value: pub-sub-broker
            - name: BROKER_PORT
              value: "8883"
            - name: CONSUMER_QUEUE
              value: COMMANDS
            - name: PRODUCER_QUEUE
              value: COMMANDS
          volumeMounts:
            - name: client-cert
              mountPath: /app/tls/client
              readOnly: true
            - name: config-dir
              mountPath: /deployments/config
              subPath: config
      volumes:
        - name: client-cert
          secret:
            secretName: pub-sub-client-cert-secret
        - name: config-dir
          emptyDir: {}
EOF
````

---

### Step 7 — Deploy the producer

The producer Deployment runs `camel-jms-app:mqtt` with `APP_ROLE=producer`. When started in this role, the Camel route fires a timer every 100 ms and publishes each message to `PRODUCER_QUEUE` (`COMMANDS`). The consumer route is disabled. The producer will send up to 15,000 messages before the timer stops, at which point the pod exits and Kubernetes restarts it — so publishing continues indefinitely.

The init container and volume configuration are identical to the consumer Deployment — only `APP_ROLE`, `user-name`, and `password` differ.

````bash {"stage": "Deploy_Producer", "label": "Deploy the producer", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: producer
  namespace: pub-sub-tutorial
  labels:
    app: producer
spec:
  replicas: 1
  selector:
    matchLabels:
      app: producer
  template:
    metadata:
      labels:
        app: producer
    spec:
      initContainers:
        - name: write-config
          image: quay.io/rh-ee-vnachiap/camel-jms-app:mqtt
          command: ["sh", "-c"]
          args:
            - |
              mkdir -p /config-dir/config
              printf '%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.keyStore]=/app/tls/client/keystore.jks' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.keyStoreType]=JKS' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.keyStorePassword]=changeit' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.trustStore]=/app/tls/client/truststore.jks' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.trustStoreType]=JKS' \
                'camel.component.paho-mqtt5.ssl-client-props[com.ibm.ssl.trustStorePassword]=changeit' \
                'camel.component.paho-mqtt5.user-name=producer' \
                'camel.component.paho-mqtt5.password=passwd' \
                > /config-dir/config/application.properties
          securityContext:
            allowPrivilegeEscalation: false
            capabilities:
              drop: ["ALL"]
            runAsNonRoot: true
          volumeMounts:
            - name: config-dir
              mountPath: /config-dir
      containers:
        - name: producer
          image: quay.io/rh-ee-vnachiap/camel-jms-app:mqtt
          imagePullPolicy: Always
          env:
            - name: APP_ROLE
              value: producer
            - name: BROKER_HOST
              value: pub-sub-broker
            - name: BROKER_PORT
              value: "8883"
            - name: PRODUCER_QUEUE
              value: COMMANDS
            - name: CONSUMER_QUEUE
              value: COMMANDS
          volumeMounts:
            - name: client-cert
              mountPath: /app/tls/client
              readOnly: true
            - name: config-dir
              mountPath: /deployments/config
              subPath: config
      volumes:
        - name: client-cert
          secret:
            secretName: pub-sub-client-cert-secret
        - name: config-dir
          emptyDir: {}
EOF
````

---

### Step 8 — Verify messages are flowing

There are two ways to confirm the topology is working end-to-end.

#### Check the Camel app logs

The consumer logs a progress line every 100 messages processed from the `com.arkmq.PipelineRoute` logger:

```bash
kubectl logs -n pub-sub-tutorial -l app=consumer --follow
```

Expected output (once the consumer has connected and is receiving):

```
INFO  [com.arkmq.PipelineRoute] Successfully bridged 100 orders...
INFO  [com.arkmq.PipelineRoute] Successfully bridged 200 orders...
```

> **Producer logging:** the producer route has no per-message log statements — it fires the timer, sets the body, and publishes silently. The only output visible in the producer pod logs is Quarkus startup and health messages. Use the Prometheus metrics below to confirm it is publishing.

#### Check the Prometheus metrics

The Prometheus metrics plugin exposes an `artemis_routed_message_count` gauge per address. The broker's web server binds to the pod's headless service DNS name rather than `localhost` or the pod IP, so `curl` must be run from inside each pod using `kubectl exec`:

```bash
# broker-0
kubectl exec -n pub-sub-tutorial pub-sub-broker-ss-0 -c pub-sub-broker-container -- \
  curl -s http://pub-sub-broker-ss-0.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local:8161/metrics/ \
  | grep 'artemis_routed_message_count.*COMMANDS'

# broker-1
kubectl exec -n pub-sub-tutorial pub-sub-broker-ss-1 -c pub-sub-broker-container -- \
  curl -s http://pub-sub-broker-ss-1.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local:8161/metrics/ \
  | grep 'artemis_routed_message_count.*COMMANDS'
```

**What to expect:** AMQP address federation is demand-driven — a broker only pulls messages from its peer when it has a local consumer for that address. With a single consumer Deployment, the consumer pod connects to one broker (whichever the ClusterIP Service routes it to). Only that broker shows a non-zero `routed_message_count`; the other shows `0.0` because no client is locally subscribed on it, and federation correctly does not pull messages to a broker with no local consumers.

The important thing to verify is that **the broker the consumer landed on shows a steadily increasing count**:

```
artemis_routed_message_count{address="COMMANDS",broker="amq-broker",} 436.0
```

#### Verify federation end-to-end

To prove that federation is actually forwarding messages between the two brokers, check which per-pod service each broker exposes and look at the `artemis_messages_added` metric on the broker the consumer is *not* connected to. If that counter is greater than `0`, messages are being published there and federation is forwarding them to the consumer on the other pod.

First, find out which broker the consumer landed on by checking which one has a non-zero `routed_message_count` (from the commands above). Then check the `messages_added` counter on the *other* broker's COMMANDS queue:

```bash
# Example: check ss-0 for messages_added (if the consumer is on ss-1)
kubectl exec -n pub-sub-tutorial pub-sub-broker-ss-0 -c pub-sub-broker-container -- \
  curl -s http://pub-sub-broker-ss-0.pub-sub-broker-hdls-svc.pub-sub-tutorial.svc.cluster.local:8161/metrics/ \
  | grep 'artemis_messages_added.*COMMANDS'
```

If the producer is also publishing to the same broker as the consumer (both connected via the same ClusterIP), `messages_added` on the other broker will be `0.0` — that is normal. In that case you can confirm federation is wired correctly by checking that the outbound AMQP connection from `ss-0` has established:

```bash
kubectl logs -n pub-sub-tutorial pub-sub-broker-ss-0 -c pub-sub-broker-container \
  | grep 'Connected on Server AMQP Connection target'
```

Expected output:
```
Connected on Server AMQP Connection target on pub-sub-broker-ss-1...svc.cluster.local:61616 after 0 retries
```

The `after 0 retries` (or a small number on a slow cluster) confirms that the FQDN-based URI resolved correctly at startup and the federation mesh is active.

> **Federation startup timing:** the StatefulSet brings `ss-0` up before `ss-1`. The FQDN-based URIs and `reconnectAttempts=-1` ensure the outbound federation link from `ss-0` keeps retrying until `ss-1` is ready, so the mesh self-heals without manual intervention. If you check metrics immediately after deployment, allow 10–20 seconds for both links to establish.

---

### Cleanup

Delete all resources created by this tutorial:

```bash {"stage":"delete", "label": "Delete all resources created by this tutorial", "runtime":"bash"}
kubectl delete deployment producer consumer -n pub-sub-tutorial
kubectl delete BrokerCluster pub-sub-broker -n pub-sub-tutorial
kubectl delete secret pub-sub-jaas-config pub-sub-broker-cert-secret pub-sub-client-cert-secret pub-sub-ssl-secret pub-sub-jks-password -n pub-sub-tutorial
kubectl delete certificate pub-sub-broker-cert pub-sub-client-cert -n pub-sub-tutorial
kubectl delete configmap my-logging-config -n pub-sub-tutorial
kubectl delete service pub-sub-broker -n pub-sub-tutorial
kubectl delete namespace pub-sub-tutorial
kubectl delete clusterissuer pub-sub-selfsigned-issuer pub-sub-ca-issuer
kubectl delete certificate pub-sub-root-ca -n cert-manager
kubectl delete secret pub-sub-root-ca-secret -n cert-manager
```

Or, to remove the entire Minikube cluster:

```{"stage":"teardown", "requires":"init/minikube_start"}
minikube delete --profile pub-sub-tutorial
```

---

### Further reading

- [BrokerProperties reference](../help/operator.md#configuring-brokerproperties) — how to configure the broker without an init container.
- [Extra mounts](../getting-started/quick-start.md#using-a-operator-extramounts) — how to mount Secrets and ConfigMaps into broker pods.
- [Scale up and scale down](scaleup_and_scaledown.md) — adding and removing broker pods from a deployment.
- [Setting up SSL with cert-manager and trust-manager](cert-manager-and-trust-manager.md) — how to use cert-manager to issue certificates for broker acceptors.
- [AMQP Federation](https://activemq.apache.org/components/artemis/documentation/latest/amqp-broker-connections.html) — upstream Artemis documentation for `AMQPConnections` and federation policies.
