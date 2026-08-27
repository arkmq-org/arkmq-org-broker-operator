---
title: "BrokerService Monitoring with Prometheus and Grafana"
description: "Build and observe a realistic order-processing pipeline using BrokerService, BrokerApp, Camel, Prometheus and Grafana."
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 123
toc: true
---

## 1. What We're Building

This tutorial deploys a realistic event-driven order-processing pipeline and shows how to observe it with Prometheus and Grafana.

### Architecture

```
 Traffic Generator           5 msg/s
 (order-generator)               │
                                 ▼
                          ORDERS.NEW
                                 │
                       order-processor-app
                          Order Processor
                                 │
                                 ▼
                       ORDERS.PROCESSED
                                 │
                       shipping-service-app
                          Shipping Service
                                 │
                                 ▼
                        ORDERS.SHIPPED
                                 │
                       delivery-service-app
                          Delivery Service
                                 │
                                 ▼
                       ORDERS.DELIVERED
                                 │
                          master-sink
                          (optional drain)
                                 │
                                 ▼

          BrokerService → Prometheus → Grafana
```

**One reusable Camel image, four application roles.**
The same container image (`camel-jms-app`) is deployed four times for the core pipeline. An optional fifth deployment, `master-sink`, can drain the terminal queue when needed.
Role and queue configuration come from environment variables.

| Kubernetes Deployment | BrokerApp identity | `APP_ROLE` | Consumes | Produces |
|---|---|---|---|---|
| order-generator | `order-generator` | `generator` | — | `ORDERS.NEW` |
| order-processor-app | `order-processor-app` | `processor` | `ORDERS.NEW` | `ORDERS.PROCESSED` |
| shipping-service-app | `shipping-service-app` | `shipping` | `ORDERS.PROCESSED` | `ORDERS.SHIPPED` |
| delivery-service-app | `delivery-service-app` | `delivery` | `ORDERS.SHIPPED` | `ORDERS.DELIVERED` |

The Kubernetes Deployment name and BrokerApp identity are the same — the business service name — so every `kubectl` command and every Artemis access-control identity uses the same name throughout.

### Prerequisites

- A running Kubernetes cluster (this tutorial uses `minikube`)
- `kubectl` configured to interact with your cluster
- `helm` installed for deploying monitoring components
- The Camel pipeline image `quay.io/rh-ee-vnachiap/camel-jms-app:pipeline-1.0` must be pullable from your cluster.

> **Naming note:** Throughout this tutorial the optional terminal consumer is called `master-sink` as a conceptual role. The corresponding Kubernetes resources use more specific names: the BrokerApp is `master-sink-app`, and the Camel Deployment is `camel-jms-master-sink`.

---

## 2. Setup Infrastructure

### Start Minikube

```bash {"stage":"init", "id":"minikube_start", "runtime":"bash"}
minikube start \
  --profile brokerservice-monitoring \
  --cpus 2 \
  --memory 8192 \
  --disk-size 20000
minikube addons enable ingress --profile brokerservice-monitoring
```

### Create Namespace

```bash {"stage":"init", "runtime":"bash"}
kubectl create namespace service-app-project
kubectl config set-context --current --namespace=service-app-project
```

### Install Cert-Manager

```bash {"stage":"init", "label":"install cert-manager", "runtime":"bash"}
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.5/cert-manager.yaml
```

Wait for the cert-manager deployments to be available, then wait for all pods — including the webhook — to be ready before proceeding:

```bash {"stage":"init", "label":"wait for cert-manager", "runtime":"bash"}
kubectl wait deployment --for=condition=Available -n cert-manager --timeout=600s cert-manager cert-manager-cainjector cert-manager-webhook
kubectl wait pods --all --for=condition=Ready -n cert-manager --timeout=300s
```

### Install Trust Manager

```bash {"stage":"init", "label":"add jetstack helm repo", "runtime":"bash"}
helm repo add jetstack https://charts.jetstack.io --force-update
```

```bash {"stage":"init", "label":"install trust-manager", "runtime":"bash"}
helm upgrade trust-manager jetstack/trust-manager --install --namespace cert-manager --set secretTargets.enabled=true --set secretTargets.authorizedSecretsAll=true --wait
```

### Install kube-prometheus-stack

```bash {"stage":"init", "label":"add prometheus helm repo", "runtime":"bash"}
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

```bash {"stage":"init", "label":"install kube-prometheus-stack", "runtime":"bash"}
helm upgrade -i prometheus prometheus-community/kube-prometheus-stack \
  -n service-app-project \
  --set grafana.sidecar.dashboards.enabled=true \
  --set grafana.sidecar.dashboards.label=grafana_dashboard \
  --set grafana.sidecar.dashboards.searchNamespace=ALL \
  --set grafana.sidecar.datasources.enabled=true \
  --set kubeEtcd.enabled=false \
  --set kubeControllerManager.enabled=false \
  --set kubeScheduler.enabled=false \
  --wait
```

Wait for all monitoring components:

```bash {"stage":"init", "label":"wait for prometheus stack", "runtime":"bash"}
kubectl wait deployment --for=condition=Available -n service-app-project prometheus-grafana prometheus-kube-prometheus-operator --timeout=300s
kubectl wait statefulset --for=jsonpath='{.status.readyReplicas}'=1 -n service-app-project prometheus-prometheus-kube-prometheus-prometheus --timeout=300s
```

### Install the Operator

```bash {"stage":"init", "rootdir":"$initial_dir", "runtime":"bash"}
./deploy/install_opr.sh
```

```bash {"stage":"init", "label":"wait for the operator to be running", "runtime":"bash"}
kubectl wait deployment arkmq-org-broker-controller-manager --for=create --timeout=240s
kubectl wait pod --all --for=condition=Ready --namespace=service-app-project --timeout=600s
```

---

## 3. Configure Certificates

### Create Issuers and Root Certificate

```bash {"stage":"deploy_certs", "label":"create root issuer", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: root-issuer
spec:
  selfSigned: {}
EOF
```

```bash {"stage":"deploy_certs", "label":"wait for root issuer", "runtime":"bash"}
kubectl wait clusterissuer root-issuer --for=condition=Ready --timeout=300s
```

```bash {"stage":"deploy_certs", "label":"create root cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: root-cert
  namespace: cert-manager
spec:
  isCA: true
  commonName: artemis.root.ca
  secretName: artemis-root-cert-secret
  issuerRef:
    name: root-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_certs", "label":"wait for root cert", "runtime":"bash"}
kubectl wait certificate root-cert --for=condition=Ready -n cert-manager --timeout=300s
```

```bash {"stage":"deploy_certs", "label":"create signing issuer", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: broker-ca-issuer
spec:
  ca:
    secretName: artemis-root-cert-secret
EOF
```

```bash {"stage":"deploy_certs", "label":"wait for signing issuer", "runtime":"bash"}
kubectl wait clusterissuer broker-ca-issuer --for=condition=Ready --timeout=300s
```

### Create Operator Certificate

```bash {"stage":"deploy_certs", "label":"create ca bundle", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: trust.cert-manager.io/v1alpha1
kind: Bundle
metadata:
  name: arkmq-org-broker-manager-ca
  namespace: cert-manager
spec:
  sources:
  - secret:
      name: artemis-root-cert-secret
      key: "tls.crt"
  target:
    secret:
      key: "ca.pem"
EOF
```

```bash {"stage":"deploy_certs", "label":"wait for ca bundle", "runtime":"bash"}
kubectl wait bundle arkmq-org-broker-manager-ca -n cert-manager --for=condition=Synced --timeout=300s
```

```bash {"stage":"deploy_certs", "label":"create operator cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: arkmq-org-broker-manager-cert
  namespace: service-app-project
spec:
  secretName: arkmq-org-broker-manager-cert
  commonName: arkmq-org-broker-operator
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_certs", "label":"wait for operator cert", "runtime":"bash"}
kubectl wait certificate arkmq-org-broker-manager-cert -n service-app-project --for=condition=Ready --timeout=300s
```

---

## 4. Deploy BrokerService and BrokerApps

### BrokerService Certificate

```bash {"stage":"deploy_service", "label":"create broker cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: messaging-service-broker-cert
  namespace: service-app-project
spec:
  secretName: messaging-service-broker-cert
  commonName: messaging-service
  dnsNames:
  - messaging-service
  - messaging-service.service-app-project.svc.cluster.local
  - '*.messaging-service-hdls-svc.service-app-project.svc.cluster.local'
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_service", "label":"wait for broker cert", "runtime":"bash"}
kubectl wait certificate messaging-service-broker-cert -n service-app-project --for=condition=Ready --timeout=300s
```

### Deploy BrokerService

```bash {"stage":"deploy_service", "label":"deploy brokerservice", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerService
metadata:
  name: messaging-service
  namespace: service-app-project
  labels:
    forWorkQueue: "true"
spec:
  resources:
    requests:
      memory: "1Gi"
    limits:
      memory: "1Gi"
  env:
    - name: JAVA_ARGS_APPEND
      value: "-Dlog4j2.level=INFO"
EOF
```

> The broker is configured with 1 GiB of memory for this tutorial workload. The memory request and limit are intentionally set to the same value.

```bash {"stage":"deploy_service", "label":"wait for brokerservice", "runtime":"bash"}
kubectl wait BrokerService messaging-service -n service-app-project --for=condition=Ready --timeout=300s
```

### Deploy BrokerApps

Each `BrokerApp` declares exactly the permissions its pipeline stage needs. The ownership chain is:

```
order-generator  →  ORDERS.NEW  →  order-processor-app  →  ORDERS.PROCESSED  →  shipping-service-app  →  ORDERS.SHIPPED  →  delivery-service-app  →  ORDERS.DELIVERED
   (produce)              (consume/produce)                       (consume/produce)                              (consume/produce)
```

Each app only owns the addresses it **produces**. Downstream consumers reference upstream producers using `appName` + `appNamespace`.

#### order-generator (Traffic Generator)

The generator has a single capability: produce into `ORDERS.NEW`.

```bash {"stage":"deploy_app", "label":"create order-generator cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: order-generator-app-cert
  namespace: service-app-project
spec:
  secretName: order-generator-app-cert
  commonName: order-generator
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_app", "label":"wait for order-generator cert", "runtime":"bash"}
kubectl wait certificate order-generator-app-cert -n service-app-project --for=condition=Ready --timeout=300s
```

```bash {"stage":"deploy_app", "label":"deploy order-generator brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-generator
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      forWorkQueue: "true"
  sharedAddresses:
    - address: "ORDERS.NEW"
  capabilities:
    - producerOf:
        - address: "ORDERS.NEW"
EOF
```

```bash {"stage":"deploy_app", "label":"wait for order-generator brokerapp", "runtime":"bash"}
kubectl wait BrokerApp order-generator -n service-app-project --for=condition=Ready --timeout=300s
```

#### order-processor-app (Order Processor)

```bash {"stage":"deploy_app", "label":"create order-processor-app cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: order-processor-app-cert
  namespace: service-app-project
spec:
  secretName: order-processor-app-cert
  commonName: order-processor-app
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_app", "label":"wait for order-processor-app cert", "runtime":"bash"}
kubectl wait certificate order-processor-app-cert -n service-app-project --for=condition=Ready --timeout=300s
```

```bash {"stage":"deploy_app", "label":"deploy order-processor-app brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-processor-app
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      forWorkQueue: "true"
  sharedAddresses:
    - address: "ORDERS.PROCESSED"
  capabilities:
    - consumerOf:
        - address: "ORDERS.NEW"
          appName: "order-generator"
          appNamespace: "service-app-project"
      producerOf:
        - address: "ORDERS.PROCESSED"
EOF
```

```bash {"stage":"deploy_app", "label":"wait for order-processor-app brokerapp", "runtime":"bash"}
kubectl wait BrokerApp order-processor-app -n service-app-project --for=condition=Ready --timeout=300s
```

#### shipping-service-app (Shipping Service)

```bash {"stage":"deploy_app", "label":"create shipping-service-app cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: shipping-service-app-cert
  namespace: service-app-project
spec:
  secretName: shipping-service-app-cert
  commonName: shipping-service-app
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_app", "label":"wait for shipping-service-app cert", "runtime":"bash"}
kubectl wait certificate shipping-service-app-cert -n service-app-project --for=condition=Ready --timeout=300s
```

```bash {"stage":"deploy_app", "label":"deploy shipping-service-app brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: shipping-service-app
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      forWorkQueue: "true"
  sharedAddresses:
    - address: "ORDERS.SHIPPED"
  capabilities:
    - consumerOf:
        - address: "ORDERS.PROCESSED"
          appName: "order-processor-app"
          appNamespace: "service-app-project"
      producerOf:
        - address: "ORDERS.SHIPPED"
EOF
```

```bash {"stage":"deploy_app", "label":"wait for shipping-service-app brokerapp", "runtime":"bash"}
kubectl wait BrokerApp shipping-service-app -n service-app-project --for=condition=Ready --timeout=300s
```

#### delivery-service-app (Delivery Service)

```bash {"stage":"deploy_app", "label":"create delivery-service-app cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: delivery-service-app-cert
  namespace: service-app-project
spec:
  secretName: delivery-service-app-cert
  commonName: delivery-service-app
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_app", "label":"wait for delivery-service-app cert", "runtime":"bash"}
kubectl wait certificate delivery-service-app-cert -n service-app-project --for=condition=Ready --timeout=300s
```

```bash {"stage":"deploy_app", "label":"deploy delivery-service-app brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: delivery-service-app
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      forWorkQueue: "true"
  sharedAddresses:
    - address: "ORDERS.DELIVERED"
  capabilities:
    - consumerOf:
        - address: "ORDERS.SHIPPED"
          appName: "shipping-service-app"
          appNamespace: "service-app-project"
      producerOf:
        - address: "ORDERS.DELIVERED"
EOF
```

```bash {"stage":"deploy_app", "label":"wait for delivery-service-app brokerapp", "runtime":"bash"}
kubectl wait BrokerApp delivery-service-app -n service-app-project --for=condition=Ready --timeout=300s
```

#### master-sink (Optional operational drain)

`master-sink` is not part of the business processing pipeline. It is an optional operational drain that you enable when you want to consume messages accumulating on the terminal `ORDERS.DELIVERED` queue — for example, to prevent unbounded growth during a long-running demo, or as an explicit "pipeline complete" acknowledgement.

During the normal pipeline demonstration and the bottleneck/scale scenarios, keep this deployment at **0 replicas** so that `ORDERS.DELIVERED` depth remains visible in Grafana.

```bash {"stage":"deploy_app", "label":"create master-sink cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: master-sink-app-cert
  namespace: service-app-project
spec:
  secretName: master-sink-app-cert
  commonName: master-sink-app
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"deploy_app", "label":"wait for master-sink cert", "runtime":"bash"}
kubectl wait certificate master-sink-app-cert -n service-app-project --for=condition=Ready --timeout=300s
```

```bash {"stage":"deploy_app", "label":"deploy master-sink brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: master-sink-app
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      forWorkQueue: "true"
  capabilities:
    - consumerOf:
        - address: "ORDERS.DELIVERED"
          appName: "delivery-service-app"
          appNamespace: "service-app-project"
EOF
```

```bash {"stage":"deploy_app", "label":"wait for master-sink brokerapp", "runtime":"bash"}
kubectl wait BrokerApp master-sink-app -n service-app-project --for=condition=Ready --timeout=300s
```

The Camel Deployment for master-sink is deployed at **0 replicas**. Scale it up only when you want to actively drain `ORDERS.DELIVERED`.

```bash {"stage":"deploy_app", "label":"create master-sink pemcfg", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: cert-pemcfg-sink
  namespace: service-app-project
type: Opaque
stringData:
  tls.pemcfg: |
    source.key=/app/tls/client/tls.key
    source.cert=/app/tls/client/tls.crt
  java.security: security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider
EOF
```

```bash {"stage":"deploy_app", "label":"wait for master-sink binding secret", "runtime":"bash"}
kubectl wait secret master-sink-app-binding-secret -n service-app-project --for=create --timeout=300s
```

```bash {"stage":"deploy_app", "label":"deploy master-sink camel app", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: camel-jms-master-sink
  namespace: service-app-project
spec:
  replicas: 0
  selector:
    matchLabels:
      app: camel-jms-master-sink
  template:
    metadata:
      labels:
        app: camel-jms-master-sink
    spec:
      containers:
      - name: camel-jms-app
        image: quay.io/rh-ee-vnachiap/camel-jms-app:pipeline-1.0
        imagePullPolicy: Always
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "100m"
        env:
        - name: BROKER_HOST
          valueFrom:
            secretKeyRef:
              name: master-sink-app-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: master-sink-app-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "master-sink-app"
        - name: APP_ROLE
          value: "sink"
        - name: CONSUMER_QUEUE
          value: "ORDERS.DELIVERED"
        - name: PRODUCER_QUEUE
          value: "NONE"
        - name: CONSUMER_CONCURRENCY
          value: "5"
        - name: JDK_JAVA_OPTIONS
          value: "-Xbootclasspath/a:/deployments/lib/main/de.dentrassi.crypto.pem-keystore-3.0.0.jar:/deployments/lib/main/com.hierynomus.asn-one-0.6.0.jar:/deployments/lib/main/org.slf4j.slf4j-api-2.0.18.jar -Djava.security.properties=/app/tls/pem/java.security"
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
          readOnly: true
        - name: cert
          mountPath: /app/tls/client
          readOnly: true
        - name: pem
          mountPath: /app/tls/pem
          readOnly: true
      volumes:
      - name: trust
        secret:
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: master-sink-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg-sink
EOF
```

To drain `ORDERS.DELIVERED` at any point during the tutorial, scale it up:

```bash
kubectl scale deployment camel-jms-master-sink --replicas=1 -n service-app-project
```

To stop draining and let the queue accumulate again:

```bash
kubectl scale deployment camel-jms-master-sink --replicas=0 -n service-app-project
```

### Wait for All Apps Provisioned

```bash {"stage":"deploy_app", "label":"wait for all apps provisioned", "runtime":"bash"}
kubectl wait BrokerService messaging-service -n service-app-project --for=condition=AppsProvisioned --timeout=300s
kubectl wait pod --selector=ActiveMQArtemis=messaging-service -n service-app-project --for=condition=Ready --timeout=300s
```

### Verify BrokerApp Bindings

Each `BrokerApp` causes the Operator to create a binding secret containing the broker host and port for that application's dedicated acceptor. The Camel Deployments in the next section read these secrets directly — no manual connection string management required.

```bash {"stage":"deploy_app", "label":"verify brokerapp bindings", "runtime":"bash"}
kubectl get brokerapp -n service-app-project \
  -o custom-columns='NAME:.metadata.name,READY:.status.conditions[?(@.type=="Ready")].status,PORT:.status.service.assignedPort,SECRET:.status.service.secret'
```

Then confirm the secrets exist:

```bash {"stage":"deploy_app", "label":"verify binding secrets", "runtime":"bash"}
kubectl get secret -n service-app-project | grep binding-secret
```

You should see `order-generator-binding-secret`, `order-processor-app-binding-secret`, `shipping-service-app-binding-secret`, and `delivery-service-app-binding-secret` before proceeding to deploy the Camel applications.

---

## 5. Deploy Camel Applications

The same Docker image is deployed four times. Role and queue configuration are injected via environment variables — no rebuilding required.

### Shared PEM Secret

```bash {"stage":"deploy_camel", "label":"create pemcfg secret", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: cert-pemcfg
  namespace: service-app-project
type: Opaque
stringData:
  tls.pemcfg: |
    source.key=/app/tls/client/tls.key
    source.cert=/app/tls/client/tls.crt
  java.security: security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider
EOF
```

Each deployment mounts its own app certificate at `/app/tls/client`. All four deployments share the same `cert-pemcfg` content (the PEM keystore type configuration), but mount different cert secrets for their individual mTLS identity.

### order-generator

Produces 5 order messages per second into `ORDERS.NEW`. The rate is controlled by the `MESSAGE_RATE` environment variable in the Camel Deployment — the BrokerApp only declares the messaging capability (`producerOf: ORDERS.NEW`). To change the rate without rebuilding the image:

Wait for the `order-generator` binding secret before deploying:

```bash {"stage":"deploy_camel", "label":"wait for order-generator binding secret", "runtime":"bash"}
kubectl wait secret order-generator-binding-secret -n service-app-project --for=create --timeout=300s
```

Create the PEM keystore secret for the generator's own certificate:

```bash {"stage":"deploy_camel", "label":"create order-generator pemcfg", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: cert-pemcfg-generator
  namespace: service-app-project
type: Opaque
stringData:
  tls.pemcfg: |
    source.key=/app/tls/client/tls.key
    source.cert=/app/tls/client/tls.crt
  java.security: security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider
EOF
```

```bash {"stage":"deploy_camel", "label":"deploy order-generator", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-generator
  namespace: service-app-project
spec:
  replicas: 1
  selector:
    matchLabels:
      app: order-generator
  template:
    metadata:
      labels:
        app: order-generator
    spec:
      containers:
      - name: camel-jms-app
        image: quay.io/rh-ee-vnachiap/camel-jms-app:pipeline-1.0
        imagePullPolicy: Always
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "100m"
        env:
        - name: BROKER_HOST
          valueFrom:
            secretKeyRef:
              name: order-generator-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: order-generator-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "order-generator"
        - name: APP_ROLE
          value: "generator"
        - name: PRODUCER_QUEUE
          value: "ORDERS.NEW"
        - name: MESSAGE_RATE
          value: "5"
        - name: JDK_JAVA_OPTIONS
          value: "-Xbootclasspath/a:/deployments/lib/main/de.dentrassi.crypto.pem-keystore-3.0.0.jar:/deployments/lib/main/com.hierynomus.asn-one-0.6.0.jar:/deployments/lib/main/org.slf4j.slf4j-api-2.0.18.jar -Djava.security.properties=/app/tls/pem/java.security"
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
          readOnly: true
        - name: cert
          mountPath: /app/tls/client
          readOnly: true
        - name: pem
          mountPath: /app/tls/pem
          readOnly: true
      volumes:
      - name: trust
        secret:
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: order-generator-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg-generator
EOF
```

```bash {"stage":"deploy_camel", "label":"wait for order-generator", "runtime":"bash"}
kubectl wait deployment order-generator -n service-app-project --for=condition=Available --timeout=300s
```

### order-processor-app (Order Processor)

```bash {"stage":"deploy_camel", "label":"wait for order-processor-app binding secret", "runtime":"bash"}
kubectl wait secret order-processor-app-binding-secret -n service-app-project --for=create --timeout=300s
```

```bash {"stage":"deploy_camel", "label":"create order-processor-app pemcfg", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: cert-pemcfg-order-processor
  namespace: service-app-project
type: Opaque
stringData:
  tls.pemcfg: |
    source.key=/app/tls/client/tls.key
    source.cert=/app/tls/client/tls.crt
  java.security: security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider
EOF
```

```bash {"stage":"deploy_camel", "label":"deploy order-processor-app", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-processor-app
  namespace: service-app-project
spec:
  replicas: 1
  selector:
    matchLabels:
      app: order-processor-app
  template:
    metadata:
      labels:
        app: order-processor-app
    spec:
      containers:
      - name: camel-jms-app
        image: quay.io/rh-ee-vnachiap/camel-jms-app:pipeline-1.0
        imagePullPolicy: Always
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "100m"
        env:
        - name: BROKER_HOST
          valueFrom:
            secretKeyRef:
              name: order-processor-app-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: order-processor-app-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "order-processor-app"
        - name: APP_ROLE
          value: "processor"
        - name: CONSUMER_QUEUE
          value: "ORDERS.NEW"
        - name: PRODUCER_QUEUE
          value: "ORDERS.PROCESSED"
        - name: PROCESSING_DELAY_MS
          value: "100"
        - name: CONSUMER_CONCURRENCY
          value: "1"
        # Throughput: 1 consumer / 0.1 s = ~10 msg/s — 2x headroom above the 5 msg/s generator rate.
        - name: JDK_JAVA_OPTIONS
          value: "-Xbootclasspath/a:/deployments/lib/main/de.dentrassi.crypto.pem-keystore-3.0.0.jar:/deployments/lib/main/com.hierynomus.asn-one-0.6.0.jar:/deployments/lib/main/org.slf4j.slf4j-api-2.0.18.jar -Djava.security.properties=/app/tls/pem/java.security"
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
          readOnly: true
        - name: cert
          mountPath: /app/tls/client
          readOnly: true
        - name: pem
          mountPath: /app/tls/pem
          readOnly: true
      volumes:
      - name: trust
        secret:
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: order-processor-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg-order-processor
EOF
```

```bash {"stage":"deploy_camel", "label":"wait for order-processor-app", "runtime":"bash"}
kubectl wait deployment order-processor-app -n service-app-project --for=condition=Available --timeout=300s
```

### shipping-service-app (Shipping Service)

```bash {"stage":"deploy_camel", "label":"create shipping-service-app pemcfg", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: cert-pemcfg-shipping-service
  namespace: service-app-project
type: Opaque
stringData:
  tls.pemcfg: |
    source.key=/app/tls/client/tls.key
    source.cert=/app/tls/client/tls.crt
  java.security: security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider
EOF
```

```bash {"stage":"deploy_camel", "label":"wait for shipping-service-app binding secret", "runtime":"bash"}
kubectl wait secret shipping-service-app-binding-secret -n service-app-project --for=create --timeout=300s
```

```bash {"stage":"deploy_camel", "label":"deploy shipping-service-app", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: shipping-service-app
  namespace: service-app-project
spec:
  replicas: 1
  selector:
    matchLabels:
      app: shipping-service-app
  template:
    metadata:
      labels:
        app: shipping-service-app
    spec:
      containers:
      - name: camel-jms-app
        image: quay.io/rh-ee-vnachiap/camel-jms-app:pipeline-1.0
        imagePullPolicy: Always
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "100m"
        env:
        - name: BROKER_HOST
          valueFrom:
            secretKeyRef:
              name: shipping-service-app-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: shipping-service-app-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "shipping-service-app"
        - name: APP_ROLE
          value: "shipping"
        - name: CONSUMER_QUEUE
          value: "ORDERS.PROCESSED"
        - name: PRODUCER_QUEUE
          value: "ORDERS.SHIPPED"
        - name: PROCESSING_DELAY_MS
          value: "25"
        - name: CONSUMER_CONCURRENCY
          value: "1"
        # Throughput: 1 consumer / 0.025 s = ~40 msg/s — 8x headroom above the 5 msg/s generator rate.
        - name: JDK_JAVA_OPTIONS
          value: "-Xbootclasspath/a:/deployments/lib/main/de.dentrassi.crypto.pem-keystore-3.0.0.jar:/deployments/lib/main/com.hierynomus.asn-one-0.6.0.jar:/deployments/lib/main/org.slf4j.slf4j-api-2.0.18.jar -Djava.security.properties=/app/tls/pem/java.security"
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
          readOnly: true
        - name: cert
          mountPath: /app/tls/client
          readOnly: true
        - name: pem
          mountPath: /app/tls/pem
          readOnly: true
      volumes:
      - name: trust
        secret:
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: shipping-service-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg-shipping-service
EOF
```

```bash {"stage":"deploy_camel", "label":"wait for shipping-service-app", "runtime":"bash"}
kubectl wait deployment shipping-service-app -n service-app-project --for=condition=Available --timeout=300s
```

### delivery-service-app (Delivery Service)

```bash {"stage":"deploy_camel", "label":"create delivery-service-app pemcfg", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: cert-pemcfg-delivery-service
  namespace: service-app-project
type: Opaque
stringData:
  tls.pemcfg: |
    source.key=/app/tls/client/tls.key
    source.cert=/app/tls/client/tls.crt
  java.security: security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider
EOF
```

```bash {"stage":"deploy_camel", "label":"wait for delivery-service-app binding secret", "runtime":"bash"}
kubectl wait secret delivery-service-app-binding-secret -n service-app-project --for=create --timeout=300s
```

```bash {"stage":"deploy_camel", "label":"deploy delivery-service-app", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: delivery-service-app
  namespace: service-app-project
spec:
  replicas: 1
  selector:
    matchLabels:
      app: delivery-service-app
  template:
    metadata:
      labels:
        app: delivery-service-app
    spec:
      containers:
      - name: camel-jms-app
        image: quay.io/rh-ee-vnachiap/camel-jms-app:pipeline-1.0
        imagePullPolicy: Always
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "100m"
        env:
        - name: BROKER_HOST
          valueFrom:
            secretKeyRef:
              name: delivery-service-app-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: delivery-service-app-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "delivery-service-app"
        - name: APP_ROLE
          value: "delivery"
        - name: CONSUMER_QUEUE
          value: "ORDERS.SHIPPED"
        - name: PRODUCER_QUEUE
          value: "ORDERS.DELIVERED"
        - name: PROCESSING_DELAY_MS
          value: "25"
        - name: CONSUMER_CONCURRENCY
          value: "1"
        # Throughput: 1 consumer / 0.025 s = ~40 msg/s — 8x headroom above the 5 msg/s generator rate.
        - name: JDK_JAVA_OPTIONS
          value: "-Xbootclasspath/a:/deployments/lib/main/de.dentrassi.crypto.pem-keystore-3.0.0.jar:/deployments/lib/main/com.hierynomus.asn-one-0.6.0.jar:/deployments/lib/main/org.slf4j.slf4j-api-2.0.18.jar -Djava.security.properties=/app/tls/pem/java.security"
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
          readOnly: true
        - name: cert
          mountPath: /app/tls/client
          readOnly: true
        - name: pem
          mountPath: /app/tls/pem
          readOnly: true
      volumes:
      - name: trust
        secret:
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: delivery-service-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg-delivery-service
EOF
```

```bash {"stage":"deploy_camel", "label":"wait for delivery-service-app", "runtime":"bash"}
kubectl wait deployment delivery-service-app -n service-app-project --for=condition=Available --timeout=300s
```

### Verify the Pipeline

Check that orders are flowing through all stages:

```bash {"stage":"verify", "label":"check generator logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/order-generator --tail=20
```

```bash {"stage":"verify", "label":"check processor logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/order-processor-app --tail=20
```

```bash {"stage":"verify", "label":"check shipping logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/shipping-service-app --tail=20
```

```bash {"stage":"verify", "label":"check delivery logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/delivery-service-app --tail=20
```

You should see log lines like:

```
[generator]  → ORDERS.NEW      | orderId=ORD-8f31a2...
[processor]  ← ORDERS.NEW      | processing...
[processor]  → ORDERS.PROCESSED | status=PROCESSED
[shipping]   ← ORDERS.PROCESSED | shipping...
[shipping]   → ORDERS.SHIPPED   | status=SHIPPED
[delivery]   ← ORDERS.SHIPPED   | delivering...
[delivery]   → ORDERS.DELIVERED | status=DELIVERED
```

---

## 6. Configure Prometheus Monitoring

> **How broker metrics work:** The ArkMQ Operator automatically configures the Prometheus Java agent for every `BrokerService`, exposing broker metrics on port 8888. This is a `BrokerService`-level concern — individual `BrokerApp` resources do not configure metrics. This section creates a Kubernetes `Service` for the metrics port and a `ServiceMonitor` so Prometheus can discover and scrape it.

### Create Prometheus Client Certificate

Create the Prometheus client certificate. The Operator uses `prometheus-cert` as the default Prometheus client certificate secret name; it uses the certificate's Common Name to grant Prometheus access to the broker metrics endpoint.

```bash {"stage":"monitoring", "label":"create prometheus cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: prometheus-cert
  namespace: service-app-project
spec:
  secretName: prometheus-cert
  commonName: prometheus
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```

```bash {"stage":"monitoring", "label":"wait for prometheus cert", "runtime":"bash"}
kubectl wait certificate prometheus-cert -n service-app-project --for=condition=Ready --timeout=300s
```

### Create Metrics Service

```bash {"stage":"monitoring", "label":"create metrics service", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: messaging-service-metrics
  namespace: service-app-project
  labels:
    app: messaging-service
spec:
  selector:
    ActiveMQArtemis: messaging-service
  ports:
    - name: metrics
      port: 8888
      targetPort: 8888
      protocol: TCP
EOF
```

### Create ServiceMonitor

```bash {"stage":"monitoring", "label":"set broker fqdn", "runtime":"bash"}
export BROKER_POD=$(kubectl get pods \
  -n service-app-project \
  -l ActiveMQArtemis=messaging-service \
  -o jsonpath='{.items[0].metadata.name}')
export BROKER_FQDN="${BROKER_POD}.messaging-service-hdls-svc.service-app-project.svc.cluster.local"
echo "Broker FQDN: ${BROKER_FQDN}"
```

```bash {"stage":"monitoring", "label":"create servicemonitor", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: messaging-service-monitor
  namespace: service-app-project
  labels:
    app: messaging-service
    release: prometheus
spec:
  selector:
    matchLabels:
      app: messaging-service
  endpoints:
  - port: metrics
    scheme: https
    interval: 15s
    tlsConfig:
      serverName: '${BROKER_FQDN}'
      ca:
        secret:
          name: arkmq-org-broker-manager-ca
          key: ca.pem
      cert:
        secret:
          name: prometheus-cert
          key: tls.crt
      keySecret:
        name: prometheus-cert
        key: tls.key
      insecureSkipVerify: false
EOF
```

### Create Prometheus Recording Rules

Create a recording rule for the total number of Artemis queue consumers. This keeps the dashboard query simple and provides a stable metric for the "Total Consumer Count" panel:

```bash {"stage":"monitoring", "label":"create recording rules", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: artemis-aggregation-rules
  namespace: service-app-project
  labels:
    release: prometheus
spec:
  groups:
  - name: artemis_aggregations
    interval: 15s
    rules:
    # Total consumer count across all queues — used by the dashboard "Total Consumer Count" panel
    - record: artemis:total_consumer_count
      expr: sum(broker_queue_consumer_count{job="messaging-service-metrics"})
EOF
```

---

## 7. Create Grafana Dashboard

The dashboard is provisioned as a Kubernetes `ConfigMap` with the `grafana_dashboard: "1"` label. The Grafana sidecar — already configured in Section 2 — watches for ConfigMaps with that label and automatically loads them. No Helm upgrade or Grafana restart is required.

```bash {"stage":"grafana", "label":"create dashboard configmap", "runtime":"bash"}
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: artemis-broker-health
  namespace: service-app-project
  labels:
    grafana_dashboard: "1"
data:
  artemis-broker-health.json: |
    {
      "title": "Artemis Broker Operational Health & Performance",
      "uid": "artemis-broker-health",
      "style": "dark",
      "tags": ["artemis", "messaging", "observability"],
      "timezone": "",
      "editable": true,
      "graphTooltip": 1,
      "time": { "from": "now-15m", "to": "now" },
      "timepicker": {},
      "refresh": "5s",
      "schemaVersion": 38,
      "version": 2,
      "panels": [
        {
          "id": 100, "title": "Row 1: Broker Overview", "type": "row",
          "collapsed": false, "gridPos": { "h": 1, "w": 24, "x": 0, "y": 0 }
        },
        {
          "id": 1, "title": "Broker Pod Ready", "type": "stat",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 6, "w": 6, "x": 0, "y": 1 },
          "targets": [{ "expr": "sum(kube_pod_status_ready{namespace=\"service-app-project\",pod=~\"messaging-service-ss-.*\",condition=\"true\"})", "refId": "A" }],
          "fieldConfig": {
            "defaults": {
              "unit": "short", "min": 0, "max": 1,
              "color": { "mode": "thresholds" },
              "thresholds": { "mode": "absolute", "steps": [{ "color": "red", "value": null }, { "color": "green", "value": 1 }] },
              "mappings": [{ "type": "value", "options": { "0": { "text": "NOT READY", "color": "red" }, "1": { "text": "HEALTHY", "color": "green" } } }]
            }
          },
          "options": { "reduceOptions": { "values": false, "calcs": ["lastNotNull"], "fields": "" }, "orientation": "auto", "textMode": "auto", "colorMode": "background" }
        },
        {
          "id": 2, "title": "Broker Ready Replicas", "type": "stat",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 6, "w": 6, "x": 6, "y": 1 },
          "targets": [{ "expr": "kube_statefulset_status_replicas_ready{namespace=\"service-app-project\",statefulset=\"messaging-service-ss\"}", "refId": "A" }],
          "fieldConfig": {
            "defaults": {
              "unit": "short",
              "color": { "mode": "thresholds" },
              "thresholds": { "mode": "absolute", "steps": [{ "color": "red", "value": null }, { "color": "green", "value": 1 }] }
            }
          },
          "options": { "reduceOptions": { "values": false, "calcs": ["lastNotNull"], "fields": "" }, "orientation": "auto", "textMode": "auto", "colorMode": "value" }
        },
        {
          "id": 3, "title": "Total Queue Messages", "type": "stat",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 6, "w": 6, "x": 12, "y": 1 },
          "targets": [{ "expr": "sum(broker_queue_message_count{job=\"messaging-service-metrics\"})", "refId": "A" }],
          "fieldConfig": {
            "defaults": {
              "unit": "short",
              "color": { "mode": "thresholds" },
              "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": null }, { "color": "yellow", "value": 1000 }, { "color": "red", "value": 5000 }] }
            }
          },
          "options": { "reduceOptions": { "values": false, "calcs": ["lastNotNull"], "fields": "" }, "orientation": "auto", "textMode": "auto", "colorMode": "value" }
        },
        {
          "id": 4, "title": "Backlog With No Consumers", "type": "stat",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 6, "w": 6, "x": 18, "y": 1 },
          "targets": [{ "expr": "sum(broker_queue_message_count{job=\"messaging-service-metrics\"} and on(queue, instance) (broker_queue_consumer_count{job=\"messaging-service-metrics\"} == 0))", "refId": "A" }],
          "fieldConfig": {
            "defaults": {
              "unit": "short",
              "color": { "mode": "thresholds" },
              "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": null }, { "color": "yellow", "value": 1 }, { "color": "red", "value": 1000 }] }
            }
          },
          "options": { "reduceOptions": { "values": false, "calcs": ["lastNotNull"], "fields": "" }, "orientation": "auto", "textMode": "auto", "colorMode": "background" }
        },
        {
          "id": 200, "title": "Row 2: Queue Overview", "type": "row",
          "collapsed": false, "gridPos": { "h": 1, "w": 24, "x": 0, "y": 7 }
        },
        {
          "id": 5, "title": "Queue Message Count", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 12, "x": 0, "y": 8 },
          "targets": [{ "expr": "sum by (queue) (broker_queue_message_count{job=\"messaging-service-metrics\"})", "legendFormat": "{{queue}}", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "short", "min": 0 } },
          "options": { "legend": { "displayMode": "table", "placement": "bottom" } }
        },
        {
          "id": 6, "title": "Queue Backlog Growth", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 12, "x": 12, "y": 8 },
          "targets": [{ "expr": "sum by (queue) (deriv(broker_queue_message_count{job=\"messaging-service-metrics\"}[10m]))", "legendFormat": "{{queue}}", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "short" } },
          "options": { "legend": { "displayMode": "table", "placement": "bottom" } }
        },
        {
          "id": 300, "title": "Row 3: Queue Processing", "type": "row",
          "collapsed": false, "gridPos": { "h": 1, "w": 24, "x": 0, "y": 15 }
        },
        {
          "id": 7, "title": "Queue Consumer Count", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 8, "x": 0, "y": 16 },
          "targets": [{ "expr": "sum by (queue) (broker_queue_consumer_count{job=\"messaging-service-metrics\"})", "legendFormat": "{{queue}}", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "short", "min": 0 } }
        },
        {
          "id": 8, "title": "Messages Being Delivered", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 8, "x": 8, "y": 16 },
          "targets": [{ "expr": "sum by (queue) (broker_queue_delivering_count{job=\"messaging-service-metrics\"})", "legendFormat": "{{queue}}", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "short", "min": 0 } }
        },
        {
          "id": 9, "title": "Queue Persistent Size", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 8, "x": 16, "y": 16 },
          "targets": [{ "expr": "sum by (queue) (broker_queue_persistent_size{job=\"messaging-service-metrics\"})", "legendFormat": "{{queue}}", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "bytes", "min": 0 } }
        },
        {
          "id": 400, "title": "Row 4: Broker Resource Health", "type": "row",
          "collapsed": false, "gridPos": { "h": 1, "w": 24, "x": 0, "y": 23 }
        },
        {
          "id": 10, "title": "Total Consumer Count", "type": "stat",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 6, "w": 6, "x": 0, "y": 24 },
          "targets": [{ "expr": "artemis:total_consumer_count", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "short", "min": 0 } },
          "options": { "reduceOptions": { "values": false, "calcs": ["lastNotNull"], "fields": "" }, "orientation": "auto", "textMode": "auto", "colorMode": "value" }
        },
        {
          "id": 11, "title": "Container CPU Usage", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 6, "x": 6, "y": 24 },
          "targets": [{ "expr": "sum(rate(container_cpu_usage_seconds_total{pod=~\"messaging-service-ss-.*\",container!=\"POD\",container!=\"\"}[5m]))", "legendFormat": "CPU Cores", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "cores", "min": 0 } }
        },
        {
          "id": 12, "title": "Container Memory Working Set %", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 6, "x": 12, "y": 24 },
          "targets": [{ "expr": "100 * sum(container_memory_working_set_bytes{pod=~\"messaging-service-ss-.*\",container!=\"POD\",container!=\"\"}) / sum(kube_pod_container_resource_limits{pod=~\"messaging-service-ss-.*\",resource=\"memory\",unit=\"byte\"})", "legendFormat": "Memory % of Limit", "refId": "A" }],
          "fieldConfig": {
            "defaults": {
              "unit": "percent", "min": 0, "max": 100,
              "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": null }, { "color": "yellow", "value": 70 }, { "color": "red", "value": 85 }] }
            }
          }
        },
        {
          "id": 13, "title": "JVM Heap Utilization %", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 6, "x": 18, "y": 24 },
          "targets": [{ "expr": "100 * sum(jvm_memory_used_bytes{job=\"messaging-service-metrics\",area=\"heap\"}) / sum(jvm_memory_max_bytes{job=\"messaging-service-metrics\",area=\"heap\"})", "legendFormat": "Heap % Used", "refId": "A" }],
          "fieldConfig": {
            "defaults": {
              "unit": "percent", "min": 0, "max": 100,
              "thresholds": { "mode": "absolute", "steps": [{ "color": "green", "value": null }, { "color": "yellow", "value": 70 }, { "color": "red", "value": 85 }] }
            }
          }
        },
        {
          "id": 500, "title": "Row 5: JVM Details", "type": "row",
          "collapsed": false, "gridPos": { "h": 1, "w": 24, "x": 0, "y": 31 }
        },
        {
          "id": 14, "title": "JVM Heap Used", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 12, "x": 0, "y": 32 },
          "targets": [{ "expr": "sum(jvm_memory_used_bytes{job=\"messaging-service-metrics\",area=\"heap\"})", "legendFormat": "Heap Used", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "bytes", "min": 0 } }
        },
        {
          "id": 15, "title": "JVM Heap Max", "type": "timeseries",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "gridPos": { "h": 7, "w": 12, "x": 12, "y": 32 },
          "targets": [{ "expr": "sum(jvm_memory_max_bytes{job=\"messaging-service-metrics\",area=\"heap\"})", "legendFormat": "Heap Max", "refId": "A" }],
          "fieldConfig": { "defaults": { "unit": "bytes", "min": 0 } }
        }
      ]
    }
EOF
```

### Access Grafana

Create an Ingress to expose Grafana through the Minikube ingress controller (this tutorial uses the NGINX addon enabled at cluster start):

```bash {"stage":"grafana", "label":"create grafana ingress", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: grafana
  namespace: service-app-project
spec:
  ingressClassName: nginx
  rules:
    - host: grafana.brokerservice-monitoring.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: prometheus-grafana
                port:
                  number: 80
EOF
```

Add the Minikube IP to your `/etc/hosts` so the hostname resolves locally:

```bash {"stage":"grafana", "label":"configure hosts", "runtime":"bash"}
export CLUSTER_IP=$(minikube ip --profile brokerservice-monitoring)
echo "${CLUSTER_IP} grafana.brokerservice-monitoring.local" | sudo tee -a /etc/hosts
echo "Grafana available at http://grafana.brokerservice-monitoring.local"
```

```bash {"stage":"grafana", "label":"get grafana password", "runtime":"bash"}
kubectl get secret prometheus-grafana -n service-app-project -o jsonpath='{.data.admin-password}' | base64 -d && echo
```

Login at **http://grafana.brokerservice-monitoring.local** with username `admin` and the password printed above, then open the **"Artemis Broker Operational Health & Performance"** dashboard.

Under normal conditions (5 msg/s, all consumers healthy) you should see:

| Panel | Expected value |
|---|---|
| Broker Pod Ready | `1` / HEALTHY |
| Broker Ready Replicas | `1` |
| Total Queue Messages | Low — dominated by `ORDERS.DELIVERED` which grows intentionally |
| Backlog With No Consumers | Reflects `ORDERS.DELIVERED` depth — growing while `master-sink` is disabled |
| Queue Message Count | `ORDERS.NEW`, `ORDERS.PROCESSED`, `ORDERS.SHIPPED` near zero; `ORDERS.DELIVERED` growing |
| Queue Consumer Count | ~1 consumer on each active processing queue |
| Messages Being Delivered | Activity visible while messages are in flight |
| Total Consumer Count | ~3 |
| Container CPU Usage | Low and stable |
| Container Memory Working Set % | Stable and below 70% |
| JVM Heap Utilization % | Stable and below 70% |

A growing `ORDERS.DELIVERED` queue is expected and does not indicate a pipeline failure — `master-sink` is intentionally disabled. Because `master-sink` starts at 0 replicas, `ORDERS.DELIVERED` accumulates at approximately 5 messages/sec. After about 20 seconds it should contain roughly 100 messages — a useful sanity check that the full pipeline is flowing end-to-end.

The expected baseline is three JMS consumers: one each for processor, shipping, and delivery. The generator is producer-only and `master-sink` starts with zero replicas.

---

## 8. Operations Scenarios

These three scenarios are the purpose of the tutorial. Each one creates or resolves a real operational event that you observe in Grafana.

### Scenario 1 — Normal Traffic

Everything is already running. Open the dashboard and confirm the pipeline is flowing at 5 msg/s.

Each stage has comfortable headroom at the baseline rate:

| Stage | Delay | Consumers | Approx. capacity |
|---|---|---|---|
| processor | 100 ms | 1 | ~10 msg/s |
| shipping | 25 ms | 1 | ~40 msg/s |
| delivery | 25 ms | 1 | ~40 msg/s |

**What to observe:** `ORDERS.NEW`, `ORDERS.PROCESSED`, and `ORDERS.SHIPPED` remain near zero. `ORDERS.DELIVERED` grows steadily because `master-sink` is intentionally disabled. To drain it at any point:

```bash
kubectl scale deployment camel-jms-master-sink --replicas=1 -n service-app-project
```

### Scenario 2 — Create a Bottleneck

Increase the shipping processing delay to make it the bottleneck:

```bash {"stage":"scenario_bottleneck", "label":"slow down shipping", "runtime":"bash"}
kubectl set env deployment/shipping-service-app \
  PROCESSING_DELAY_MS=2000 \
  -n service-app-project
kubectl rollout status deployment/shipping-service-app -n service-app-project --timeout=120s
```

With `PROCESSING_DELAY_MS=2000` and `CONSUMER_CONCURRENCY=1`, the shipping service can process at most **0.5 msg/s** (theoretical). The generator is still producing 5 msg/s, so under idealized conditions `ORDERS.PROCESSED` should accumulate at roughly **4.5 messages per second** (5 − 0.5). The actual rate depends on JMS overhead and scheduling, but the growth will be clearly visible in Grafana within seconds.

**What to observe in Grafana:**

```
ORDERS.PROCESSED queue depth
        │
        │              ▲ growing
        │            ██
        │          ████
        │        ██████
        │      ████████
        └──────────────────► time
```

The `Queue Message Count` panel shows `ORDERS.PROCESSED` climbing while the other queues stay flat. This is the bottleneck made visible.

### Scenario 3 — Scale to Recover

Scale the shipping deployment to five replicas:

```bash {"stage":"scenario_scale", "label":"scale up shipping", "runtime":"bash"}
kubectl scale deployment shipping-service-app \
  --replicas=5 \
  -n service-app-project
kubectl wait deployment shipping-service-app \
  -n service-app-project \
  --for=condition=Available \
  --timeout=300s
```

Scaling gives you five consumers, but they still have the 2-second processing delay — so aggregate throughput is still only ~2.5 msg/s at this point. The backlog may therefore continue growing during the rollout. The next step restores the 25 ms delay; after that rollout completes, aggregate theoretical capacity rises to ~200 msg/s — though actual throughput will be lower due to JMS and scheduling overhead. With the generator still producing 5 msg/s, the backlog can drain at up to roughly 195 msg/s under idealized conditions, so even a large accumulated backlog clears quickly:

```bash {"stage":"scenario_scale", "label":"reset shipping delay", "runtime":"bash"}
kubectl set env deployment/shipping-service-app \
  PROCESSING_DELAY_MS=25 \
  -n service-app-project
kubectl rollout status deployment/shipping-service-app -n service-app-project --timeout=120s
```

After the scale-up and delay reset, the updated consumer counts are:

| Queue | Consumers |
|---|---|
| `ORDERS.NEW` | 1 (unchanged) |
| `ORDERS.PROCESSED` | 5 (scaled up from 1) |
| `ORDERS.SHIPPED` | 1 (unchanged) |
| **Total** | **7** |

**What to observe in Grafana:**

```
ORDERS.PROCESSED queue depth
        |
        |      ^ grew during bottleneck
        |    ########
        |  ##########
        |  ######
        |  ####     <- draining after scale-up
        |  ##
        |  #
        |  0        <- recovered
        +-------------------> time
```

Because `CONSUMER_CONCURRENCY=1`, the five shipping replicas create five JMS consumers on `ORDERS.PROCESSED`. The `Queue Consumer Count` panel shows that jump from 1 to 5, and `Total Queue Messages` drops back toward zero.

**The operational story:**

> The shipping service became the bottleneck. We detected queue growth in Grafana and scaled the consumer deployment to restore throughput.

That is the complete demonstration of why BrokerService and real-time monitoring matter.

---

## Cleanup

```bash
# Remove the /etc/hosts entry added during setup
sudo sed -i '/grafana.brokerservice-monitoring.local/d' /etc/hosts

# Delete the tutorial namespace and everything deployed into it
kubectl delete namespace service-app-project

# Delete the minikube cluster (also removes the Ingress controller)
minikube delete --profile brokerservice-monitoring
```

---

## Troubleshooting

### Metrics not appearing in Grafana

> **Troubleshooting only:** The steps below use `kubectl port-forward` to inspect Prometheus directly. This is a debugging tool, not the normal way to access anything in this tutorial.

Temporarily port-forward Prometheus and check that the broker target is `UP`:

```bash
kubectl port-forward svc/prometheus-kube-prometheus-prometheus \
  -n service-app-project 9090:9090 > /tmp/prometheus-pf.log 2>&1 &
```

Open http://localhost:9090/targets and find `messaging-service-monitor`. The error shown on a failing target will identify whether the problem is TLS, DNS, or authentication.

Also verify the `prometheus-cert` secret exists — the Operator requires it to authorise Prometheus:

```bash
kubectl get secret prometheus-cert -n service-app-project
kubectl get servicemonitor messaging-service-monitor -n service-app-project -o yaml | grep "release: prometheus"
```

### Panel shows "No data" but the target is UP

Search for `broker_queue` in the Prometheus UI to see what metric names your Operator version exposes. If they differ from what the recording rules expect, update the `expr` fields in the PrometheusRule and the dashboard JSON to match.

### Pipeline not flowing

Check deployments and binding secrets:

```bash
kubectl get deployment -n service-app-project
kubectl get secret -n service-app-project | grep binding-secret
```

Check logs for connection errors:

```bash
kubectl logs -n service-app-project deployment/order-processor-app --tail=30 | grep -i error
```
