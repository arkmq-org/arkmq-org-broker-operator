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

This tutorial builds a realistic event-driven order-processing pipeline on Kubernetes and shows how to observe it with Prometheus and Grafana.

The pipeline processes orders through four stages — generation, processing, shipping, and delivery — each running as a separate Camel JMS application connected to a shared Apache Artemis broker. Every stage communicates exclusively through the broker using mTLS, with access enforced by `BrokerApp` RBAC so each application can only read from and write to the queues it owns.

The broker is deployed as a `BrokerService`, which the ArkMQ Operator manages automatically: it provisions the StatefulSet, configures per-application acceptors, and exposes Prometheus metrics on port 8888. A `ServiceMonitor` tells Prometheus where to scrape, and a Grafana dashboard visualizes memory usage and queue depth in real time.

The three scenario sections at the end let you interactively create a processing bottleneck, diagnose the resulting backlog in Grafana, and recover by scaling the bottleneck deployment.

### Architecture

```mermaid
flowchart TD
    GEN["Traffic Generator<br/><code>order-generator</code><br/><i>5 msg/s</i>"]
    Q1[("Queue<br/><b>ORDERS.NEW</b>")]
    PROC["Order Processor<br/><code>order-processor</code>"]
    Q2[("Queue<br/><b>ORDERS.PROCESSED</b>")]
    SHIP["Shipping Service<br/><code>shipping-service</code>"]
    Q3[("Queue<br/><b>ORDERS.SHIPPED</b>")]
    DELIV["Delivery Service<br/><code>delivery-service</code>"]
    Q4[("Queue<br/><b>ORDERS.DELIVERED</b>")]
    SINK["Master Sink<br/><code>master-sink</code><br/><i>optional drain</i>"]

    GEN --> Q1
    Q1 --> PROC
    PROC --> Q2
    Q2 --> SHIP
    SHIP --> Q3
    Q3 --> DELIV
    DELIV --> Q4
    Q4 -.-> SINK

    SINK ~~~ OBS

    subgraph OBS ["Observability Stack"]
        direction LR
        BS["BrokerService<br/><i>:8888</i>"] -->|metrics| PROM["Prometheus<br/><i>ServiceMonitor</i>"]
        PROM -->|queries| GRAF["Grafana<br/><i>Dashboard</i>"]
    end

    classDef generator fill:#e0f7fa,stroke:#00838f,stroke-width:2px,color:#000;
    classDef app fill:#e3f2fd,stroke:#1976d2,stroke-width:2px,color:#000;
    classDef queue fill:#fff3e0,stroke:#ef6c00,stroke-width:2px,color:#000;
    classDef sink fill:#f3e5f5,stroke:#7b1fa2,stroke-width:1px,stroke-dasharray:5 5,color:#000;
    classDef observability fill:#e8f5e9,stroke:#388e3c,stroke-width:2px,color:#000;

    class GEN generator;
    class PROC,SHIP,DELIV app;
    class Q1,Q2,Q3,Q4 queue;
    class SINK sink;
    class BS,PROM,GRAF observability;
```

**One reusable Camel image, four application roles.**
The same container image (`camel-jms-app`) is deployed four times for the core pipeline. An optional fifth deployment, `master-sink`, can drain the terminal queue when needed.
Role and queue configuration come from environment variables.

| Kubernetes Deployment | BrokerApp identity | `APP_ROLE` | Consumes | Produces |
|---|---|---|---|---|
| order-generator | `order-generator` | `generator` | — | `ORDERS.NEW` |
| order-processor-app | `order-processor` | `processor` | `ORDERS.NEW` | `ORDERS.PROCESSED` |
| shipping-service-app | `shipping-service` | `shipping` | `ORDERS.PROCESSED` | `ORDERS.SHIPPED` |
| delivery-service-app | `delivery-service` | `delivery` | `ORDERS.SHIPPED` | `ORDERS.DELIVERED` |
| camel-jms-master-sink *(optional)* | `master-sink` | `sink` | `ORDERS.DELIVERED` | — |

The Kubernetes Deployment name is the full service name (e.g. `order-processor-app`). The BrokerApp identity drops the `-app` suffix (e.g. `order-processor`) so that the operator-derived certificate secret name (`{brokerAppName}-app-cert`) matches the certificate created for the deployment.

### Prerequisites

- A running Kubernetes cluster (this tutorial uses `minikube`)
- `kubectl` configured to interact with your cluster
- `helm` installed for deploying monitoring components
- A container build tool (`docker` or `podman`) available locally

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
```shell markdown_runner
* [brokerservice-monitoring] minikube v1.38.1 on Fedora 44
* Using the docker driver based on existing profile
* Starting "brokerservice-monitoring" primary control-plane node in "brokerservice-monitoring" cluster
* Pulling base image v0.0.50 ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: default-storageclass, storage-provisioner
* Done! kubectl is now configured to use "brokerservice-monitoring" cluster and "default" namespace by default
* ingress is an addon maintained by Kubernetes. For any concerns contact minikube on GitHub.
You can view the list of minikube maintainers at: https://github.com/kubernetes/minikube/blob/master/OWNERS
  - Using image registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.6.7
  - Using image registry.k8s.io/ingress-nginx/controller:v1.14.3
  - Using image registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.6.7
* Verifying ingress addon...
* The 'ingress' addon is enabled
! You cannot change the memory size for an existing minikube cluster. Please first delete the cluster.
```

### Build the Camel Pipeline Image

The Camel pipeline image is built using your local Docker daemon (which has
internet access for Maven dependencies) and then loaded directly into Minikube.
The source lives alongside this tutorial in [`camel-jms-app/`](camel-jms-app/).
The `Containerfile` is a multi-stage build — Maven and the JDK run inside the
builder container, so no local JDK or Maven installation is required.

```bash {"stage":"init", "label":"build camel jms image", "rootdir":"$initial_dir", "runtime":"bash"}
docker build -f docs/tutorials/brokerservice/camel-jms-app/Containerfile docs/tutorials/brokerservice/camel-jms-app/ -t camel-jms-app:latest
minikube image load camel-jms-app:latest --profile brokerservice-monitoring
```
```shell markdown_runner
#0 building with "default" instance using docker driver

#1 [internal] load build definition from Containerfile
#1 transferring dockerfile: 1.43kB done
#1 DONE 0.0s

#2 [internal] load metadata for registry.access.redhat.com/ubi9/openjdk-21:latest
#2 ...

#3 [internal] load metadata for registry.access.redhat.com/ubi9/openjdk-21-runtime:latest
#3 DONE 0.4s

#2 [internal] load metadata for registry.access.redhat.com/ubi9/openjdk-21:latest
#2 DONE 0.4s

#4 [internal] load .dockerignore
#4 transferring context: 217B done
#4 DONE 0.0s

#5 [internal] load build context
#5 DONE 0.0s

#6 [builder 1/7] FROM registry.access.redhat.com/ubi9/openjdk-21:latest@sha256:40929d99200a97ae859994d3a41080befb871c26ae116f7d24bc9aeaa7d31d46
#6 resolve registry.access.redhat.com/ubi9/openjdk-21:latest@sha256:40929d99200a97ae859994d3a41080befb871c26ae116f7d24bc9aeaa7d31d46 0.1s done
#6 DONE 0.1s

#7 [stage-1 1/5] FROM registry.access.redhat.com/ubi9/openjdk-21-runtime:latest@sha256:f10cc334bd39f39ad35bcefc5ac0ce1ad25bda499f6abf73ccb8afdd1ea6d3c1
#7 resolve registry.access.redhat.com/ubi9/openjdk-21-runtime:latest@sha256:f10cc334bd39f39ad35bcefc5ac0ce1ad25bda499f6abf73ccb8afdd1ea6d3c1 0.1s done
#7 DONE 0.1s

#5 [internal] load build context
#5 transferring context: 21.73kB done
#5 DONE 0.0s

#8 [stage-1 2/5] COPY --chown=185 --from=builder /build/target/quarkus-app/lib/       /deployments/lib/
#8 CACHED

#9 [builder 6/7] COPY src/ src/
#9 CACHED

#10 [builder 2/7] WORKDIR /build
#10 CACHED

#11 [stage-1 3/5] COPY --chown=185 --from=builder /build/target/quarkus-app/*.jar       /deployments/
#11 CACHED

#12 [builder 5/7] RUN mvn dependency:resolve-plugins dependency:resolve -q
#12 CACHED

#13 [builder 7/7] RUN mvn package -DskipTests -q
#13 CACHED

#14 [stage-1 4/5] COPY --chown=185 --from=builder /build/target/quarkus-app/app/        /deployments/app/
#14 CACHED

#15 [builder 3/7] RUN microdnf install -y maven --setopt=install_weak_deps=0 && microdnf clean all
#15 CACHED

#16 [builder 4/7] COPY pom.xml pom.xml
#16 CACHED

#17 [stage-1 5/5] COPY --chown=185 --from=builder /build/target/quarkus-app/quarkus/    /deployments/quarkus/
#17 CACHED

#18 exporting to image
#18 exporting layers done
#18 exporting manifest sha256:7ec3c5074e20334c6fcaab1e44a352c72ff106940d1572b1ff3a1b93a3995d73 done
#18 exporting config sha256:026c59c2230ae766a5a6f6dc34fd59e2c8c4778a971e1576fa46fc79bfd4455a done
#18 exporting attestation manifest sha256:649c3c636d40884ab73092455503e4ecdd87c139aa7eefb342d60bec78eb8035
#18 exporting attestation manifest sha256:649c3c636d40884ab73092455503e4ecdd87c139aa7eefb342d60bec78eb8035 0.0s done
#18 exporting manifest list sha256:f4452bccb929c1a82c29d440d25ac10b3cd0e0818a211eb030d73ad9907f120d 0.0s done
#18 naming to docker.io/library/camel-jms-app:latest done
#18 unpacking to docker.io/library/camel-jms-app:latest 0.0s done
#18 DONE 0.1s
```

The first build takes a few minutes while Maven downloads dependencies and
compiles the Quarkus application. Subsequent builds reuse the cached dependency
layer and are much faster.

### Create Namespace

```bash {"stage":"init", "runtime":"bash"}
kubectl create namespace service-app-project
kubectl config set-context --current --namespace=service-app-project
```
```shell markdown_runner
namespace/service-app-project created
Context "brokerservice-monitoring" modified.
```

### Install Cert-Manager

```bash {"stage":"init", "label":"install cert-manager", "runtime":"bash"}
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.5/cert-manager.yaml
```
```shell markdown_runner
namespace/cert-manager created
customresourcedefinition.apiextensions.k8s.io/certificaterequests.cert-manager.io created
customresourcedefinition.apiextensions.k8s.io/certificates.cert-manager.io created
customresourcedefinition.apiextensions.k8s.io/challenges.acme.cert-manager.io created
customresourcedefinition.apiextensions.k8s.io/clusterissuers.cert-manager.io created
customresourcedefinition.apiextensions.k8s.io/issuers.cert-manager.io created
customresourcedefinition.apiextensions.k8s.io/orders.acme.cert-manager.io created
serviceaccount/cert-manager-cainjector created
serviceaccount/cert-manager created
serviceaccount/cert-manager-webhook created
clusterrole.rbac.authorization.k8s.io/cert-manager-cainjector created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-issuers created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-clusterissuers created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-certificates created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-orders created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-challenges created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-ingress-shim created
clusterrole.rbac.authorization.k8s.io/cert-manager-cluster-view created
clusterrole.rbac.authorization.k8s.io/cert-manager-view created
clusterrole.rbac.authorization.k8s.io/cert-manager-edit created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-approve:cert-manager-io created
clusterrole.rbac.authorization.k8s.io/cert-manager-controller-certificatesigningrequests created
clusterrole.rbac.authorization.k8s.io/cert-manager-webhook:subjectaccessreviews created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-cainjector created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-issuers created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-clusterissuers created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-certificates created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-orders created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-challenges created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-ingress-shim created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-approve:cert-manager-io created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-controller-certificatesigningrequests created
clusterrolebinding.rbac.authorization.k8s.io/cert-manager-webhook:subjectaccessreviews created
role.rbac.authorization.k8s.io/cert-manager-cainjector:leaderelection created
role.rbac.authorization.k8s.io/cert-manager:leaderelection created
role.rbac.authorization.k8s.io/cert-manager-tokenrequest created
role.rbac.authorization.k8s.io/cert-manager-webhook:dynamic-serving created
rolebinding.rbac.authorization.k8s.io/cert-manager-cainjector:leaderelection created
rolebinding.rbac.authorization.k8s.io/cert-manager:leaderelection created
rolebinding.rbac.authorization.k8s.io/cert-manager-cert-manager-tokenrequest created
rolebinding.rbac.authorization.k8s.io/cert-manager-webhook:dynamic-serving created
service/cert-manager-cainjector created
service/cert-manager created
service/cert-manager-webhook created
deployment.apps/cert-manager-cainjector created
deployment.apps/cert-manager created
deployment.apps/cert-manager-webhook created
mutatingwebhookconfiguration.admissionregistration.k8s.io/cert-manager-webhook created
validatingwebhookconfiguration.admissionregistration.k8s.io/cert-manager-webhook created
```

Wait for `cert-manager` to be ready:

```bash {"stage":"init", "label":"wait for cert-manager", "runtime":"bash"}
kubectl wait deployment --for=condition=Available -n cert-manager --timeout=600s cert-manager cert-manager-cainjector cert-manager-webhook
```
```shell markdown_runner
deployment.apps/cert-manager condition met
deployment.apps/cert-manager-cainjector condition met
deployment.apps/cert-manager-webhook condition met
```

### Install Trust Manager

```bash {"stage":"init", "label":"add jetstack helm repo", "runtime":"bash"}
helm repo add jetstack https://charts.jetstack.io --force-update
```
```shell markdown_runner
"jetstack" has been added to your repositories
```

```bash {"stage":"init", "label":"install trust-manager", "runtime":"bash"}
helm upgrade trust-manager jetstack/trust-manager --install --namespace cert-manager --set secretTargets.enabled=true --set secretTargets.authorizedSecretsAll=true --wait
```
```shell markdown_runner
Release "trust-manager" does not exist. Installing it now.
NAME: trust-manager
LAST DEPLOYED: Fri Sep 18 11:13:58 2026
NAMESPACE: cert-manager
STATUS: deployed
REVISION: 1
TEST SUITE: None
NOTES:
⚠️  WARNING: Consider increasing the Helm value `replicaCount` to 2 if you require high availability.
⚠️  WARNING: Consider setting the Helm value `podDisruptionBudget.enabled` to true if you require high availability.

trust-manager v0.25.0 has been deployed successfully!
Your installation includes a default CA package, using the following
default CA package image:

:

It's imperative that you keep the default CA package image up to date.
To find out more about securely running trust-manager and to get started
with creating your first bundle, check out the documentation on the
cert-manager website:

https://cert-manager.io/docs/projects/trust-manager/
```

### Install kube-prometheus-stack

```bash {"stage":"init", "label":"add prometheus helm repo", "runtime":"bash"}
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```
```shell markdown_runner
"prometheus-community" already exists with the same configuration, skipping
Hang tight while we grab the latest from your chart repositories...
...Successfully got an update from the "jetstack" chart repository
...Successfully got an update from the "prometheus-community" chart repository
Update Complete. ⎈Happy Helming!⎈
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
```shell markdown_runner
Release "prometheus" does not exist. Installing it now.
NAME: prometheus
LAST DEPLOYED: Fri Sep 18 11:14:21 2026
NAMESPACE: service-app-project
STATUS: deployed
REVISION: 1
TEST SUITE: None
NOTES:
kube-prometheus-stack has been installed. Check its status by running:
  kubectl --namespace service-app-project get pods -l "release=prometheus"

Get Grafana 'admin' user password by running:

  kubectl --namespace service-app-project get secrets prometheus-grafana -o jsonpath="{.data.admin-password}" | base64 -d ; echo

Access Grafana local instance:

  export POD_NAME=$(kubectl --namespace service-app-project get pod -l "app.kubernetes.io/name=grafana,app.kubernetes.io/instance=prometheus" -oname)
  kubectl --namespace service-app-project port-forward $POD_NAME 3000

Get your grafana admin user password by running:

  kubectl get secret --namespace service-app-project -l app.kubernetes.io/component=admin-secret -o jsonpath="{.items[0].data.admin-password}" | base64 --decode ; echo


Visit https://github.com/prometheus-operator/kube-prometheus for instructions on how to create & configure Alertmanager and Prometheus instances using the Operator.
```

Wait for all monitoring components:

```bash {"stage":"init", "label":"wait for prometheus stack", "runtime":"bash"}
kubectl wait deployment --for=condition=Available -n service-app-project prometheus-grafana prometheus-kube-prometheus-operator --timeout=300s
kubectl wait statefulset --for=jsonpath='{.status.readyReplicas}'=1 -n service-app-project prometheus-prometheus-kube-prometheus-prometheus --timeout=300s
```
```shell markdown_runner
deployment.apps/prometheus-grafana condition met
deployment.apps/prometheus-kube-prometheus-operator condition met
statefulset.apps/prometheus-prometheus-kube-prometheus-prometheus condition met
```

### Install the Operator

```bash {"stage":"init", "rootdir":"$initial_dir", "runtime":"bash"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
Client Version: 4.8.11
Kubernetes Version: v1.35.1
customresourcedefinition.apiextensions.k8s.io/activemqartemises.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/brokers.broker.arkmq.org created
customresourcedefinition.apiextensions.k8s.io/brokerapps.broker.arkmq.org created
customresourcedefinition.apiextensions.k8s.io/brokerclusters.broker.arkmq.org created
customresourcedefinition.apiextensions.k8s.io/brokerservices.broker.arkmq.org created
serviceaccount/arkmq-org-broker-controller-manager created
role.rbac.authorization.k8s.io/arkmq-org-broker-operator-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-operator-rolebinding created
role.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-rolebinding created
networkpolicy.networking.k8s.io/arkmq-org-broker-controller-manager-netpol created
deployment.apps/arkmq-org-broker-controller-manager created
```

```bash {"stage":"init", "label":"wait for the operator to be running", "runtime":"bash"}
kubectl wait deployment arkmq-org-broker-controller-manager --for=condition=Available --timeout=240s
```
```shell markdown_runner
deployment.apps/arkmq-org-broker-controller-manager condition met
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
```shell markdown_runner
clusterissuer.cert-manager.io/root-issuer created
```

```bash {"stage":"deploy_certs", "label":"wait for root issuer", "runtime":"bash"}
kubectl wait clusterissuer root-issuer --for=condition=Ready --timeout=300s
```
```shell markdown_runner
clusterissuer.cert-manager.io/root-issuer condition met
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
```shell markdown_runner
certificate.cert-manager.io/root-cert created
```

```bash {"stage":"deploy_certs", "label":"wait for root cert", "runtime":"bash"}
kubectl wait certificate root-cert --for=condition=Ready -n cert-manager --timeout=300s
```
```shell markdown_runner
certificate.cert-manager.io/root-cert condition met
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
```shell markdown_runner
clusterissuer.cert-manager.io/broker-ca-issuer created
```

```bash {"stage":"deploy_certs", "label":"wait for signing issuer", "runtime":"bash"}
kubectl wait clusterissuer broker-ca-issuer --for=condition=Ready --timeout=300s
```
```shell markdown_runner
clusterissuer.cert-manager.io/broker-ca-issuer condition met
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
```shell markdown_runner
bundle.trust.cert-manager.io/arkmq-org-broker-manager-ca created
```

```bash {"stage":"deploy_certs", "label":"wait for ca bundle", "runtime":"bash"}
kubectl wait bundle arkmq-org-broker-manager-ca -n cert-manager --for=condition=Synced --timeout=300s
```
```shell markdown_runner
bundle.trust.cert-manager.io/arkmq-org-broker-manager-ca condition met
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
```shell markdown_runner
certificate.cert-manager.io/arkmq-org-broker-manager-cert created
```

```bash {"stage":"deploy_certs", "label":"wait for operator cert", "runtime":"bash"}
kubectl wait certificate arkmq-org-broker-manager-cert -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
certificate.cert-manager.io/arkmq-org-broker-manager-cert condition met
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
```shell markdown_runner
certificate.cert-manager.io/messaging-service-broker-cert created
```

```bash {"stage":"deploy_service", "label":"wait for broker cert", "runtime":"bash"}
kubectl wait certificate messaging-service-broker-cert -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
certificate.cert-manager.io/messaging-service-broker-cert condition met
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
    app: "order-processing-pipeline"
spec:
  resources:
    limits:
      memory: "1Gi"
  env:
    - name: JAVA_ARGS_APPEND
      value: "-Dlog4j2.level=INFO"
EOF
```
```shell markdown_runner
brokerservice.broker.arkmq.org/messaging-service created
```
> The broker is configured with 1 GiB memory limit for this tutorial workload.


```bash {"stage":"deploy_service", "label":"wait for brokerservice", "runtime":"bash"}
kubectl wait BrokerService messaging-service -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerservice.broker.arkmq.org/messaging-service condition met
```

### Deploy BrokerApps

Each `BrokerApp` declares exactly the permissions its pipeline stage needs. The ownership chain is:

```
order-generator  →  ORDERS.NEW  →  order-processor  →  ORDERS.PROCESSED  →  shipping-service  →  ORDERS.SHIPPED  →  delivery-service  →  ORDERS.DELIVERED
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
```shell markdown_runner
certificate.cert-manager.io/order-generator-app-cert created
```

```bash {"stage":"deploy_app", "label":"wait for order-generator cert", "runtime":"bash"}
kubectl wait certificate order-generator-app-cert -n service-app-project --for=condition=Ready --timeout=300s
kubectl wait secret order-generator-app-cert -n service-app-project --for=create --timeout=60s
```
```shell markdown_runner
certificate.cert-manager.io/order-generator-app-cert condition met
secret/order-generator-app-cert condition met
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
      app: "order-processing-pipeline"
  sharedAddresses:
    - address: "ORDERS.NEW"
  capabilities:
    - producerOf:
        - address: "ORDERS.NEW"
EOF
```
```shell markdown_runner
brokerapp.broker.arkmq.org/order-generator created
```

```bash {"stage":"deploy_app", "label":"wait for order-generator brokerapp", "runtime":"bash"}
kubectl wait brokerapp/order-generator -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerapp.broker.arkmq.org/order-generator condition met
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
```shell markdown_runner
certificate.cert-manager.io/order-processor-app-cert created
```

```bash {"stage":"deploy_app", "label":"wait for order-processor-app cert", "runtime":"bash"}
kubectl wait certificate order-processor-app-cert -n service-app-project --for=condition=Ready --timeout=300s
kubectl wait secret order-processor-app-cert -n service-app-project --for=create --timeout=60s
```
```shell markdown_runner
certificate.cert-manager.io/order-processor-app-cert condition met
secret/order-processor-app-cert condition met
```

```bash {"stage":"deploy_app", "label":"deploy order-processor-app brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: order-processor
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      app: "order-processing-pipeline"
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
```shell markdown_runner
brokerapp.broker.arkmq.org/order-processor created
```

```bash {"stage":"deploy_app", "label":"wait for order-processor-app brokerapp", "runtime":"bash"}
kubectl wait brokerapp/order-processor -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerapp.broker.arkmq.org/order-processor condition met
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
```shell markdown_runner
certificate.cert-manager.io/shipping-service-app-cert created
```

```bash {"stage":"deploy_app", "label":"wait for shipping-service-app cert", "runtime":"bash"}
kubectl wait certificate shipping-service-app-cert -n service-app-project --for=condition=Ready --timeout=300s
kubectl wait secret shipping-service-app-cert -n service-app-project --for=create --timeout=60s
```
```shell markdown_runner
certificate.cert-manager.io/shipping-service-app-cert condition met
secret/shipping-service-app-cert condition met
```

```bash {"stage":"deploy_app", "label":"deploy shipping-service-app brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: shipping-service
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      app: "order-processing-pipeline"
  sharedAddresses:
    - address: "ORDERS.SHIPPED"
  capabilities:
    - consumerOf:
        - address: "ORDERS.PROCESSED"
          appName: "order-processor"
          appNamespace: "service-app-project"
      producerOf:
        - address: "ORDERS.SHIPPED"
EOF
```
```shell markdown_runner
brokerapp.broker.arkmq.org/shipping-service created
```

```bash {"stage":"deploy_app", "label":"wait for shipping-service-app brokerapp", "runtime":"bash"}
kubectl wait brokerapp/shipping-service -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerapp.broker.arkmq.org/shipping-service condition met
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
```shell markdown_runner
certificate.cert-manager.io/delivery-service-app-cert created
```

```bash {"stage":"deploy_app", "label":"wait for delivery-service-app cert", "runtime":"bash"}
kubectl wait certificate delivery-service-app-cert -n service-app-project --for=condition=Ready --timeout=300s
kubectl wait secret delivery-service-app-cert -n service-app-project --for=create --timeout=60s
```
```shell markdown_runner
certificate.cert-manager.io/delivery-service-app-cert condition met
secret/delivery-service-app-cert condition met
```

```bash {"stage":"deploy_app", "label":"deploy delivery-service-app brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: delivery-service
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      app: "order-processing-pipeline"
  sharedAddresses:
    - address: "ORDERS.DELIVERED"
  capabilities:
    - consumerOf:
        - address: "ORDERS.SHIPPED"
          appName: "shipping-service"
          appNamespace: "service-app-project"
      producerOf:
        - address: "ORDERS.DELIVERED"
EOF
```
```shell markdown_runner
brokerapp.broker.arkmq.org/delivery-service created
```

```bash {"stage":"deploy_app", "label":"wait for delivery-service-app brokerapp", "runtime":"bash"}
kubectl wait brokerapp/delivery-service -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerapp.broker.arkmq.org/delivery-service condition met
```

### Wait for All Apps Provisioned

```bash {"stage":"deploy_app", "label":"wait for all apps provisioned", "runtime":"bash"}
kubectl wait BrokerService messaging-service -n service-app-project --for=condition=AppsProvisioned --timeout=300s
kubectl wait statefulset messaging-service-ss -n service-app-project --for=jsonpath='{.status.readyReplicas}'=1 --timeout=300s
```
```shell markdown_runner
brokerservice.broker.arkmq.org/messaging-service condition met
statefulset.apps/messaging-service-ss condition met
```

### Verify BrokerApp Bindings

Each `BrokerApp` causes the Operator to create a binding secret containing the broker host and port for that application's dedicated acceptor. The Camel Deployments in the next section read these secrets directly — no manual connection string management required.

```bash {"stage":"deploy_app", "label":"verify brokerapp bindings", "runtime":"bash"}
kubectl get brokerapp -n service-app-project \
  -o custom-columns='NAME:.metadata.name,READY:.status.conditions[?(@.type=="Ready")].status,PORT:.status.service.assignedPort,SECRET:.status.service.secret'
```
```shell markdown_runner
NAME               READY   PORT    SECRET
delivery-service   True    61619   delivery-service-binding-secret
order-generator    True    61616   order-generator-binding-secret
order-processor    True    61617   order-processor-binding-secret
shipping-service   True    61618   shipping-service-binding-secret
```

Then confirm the secrets exist:

```bash {"stage":"deploy_app", "label":"verify binding secrets", "runtime":"bash"}
kubectl get secret -n service-app-project | grep binding-secret
```
```shell markdown_runner
delivery-service-binding-secret                                                       Opaque                                3      90s
order-generator-binding-secret                                                        Opaque                                3      22m
order-processor-binding-secret                                                        Opaque                                3      21m
shipping-service-binding-secret                                                       Opaque                                3      2m30s
```

You should see `order-generator-binding-secret`, `order-processor-binding-secret`, `shipping-service-binding-secret`, and `delivery-service-binding-secret` before proceeding to deploy the Camel applications.

---

## 5. Deploy Camel Applications

The pipeline workload is a purpose-built Camel Quarkus JMS application ([`camel-jms-app`](camel-jms-app/README.md)) that uses [`camel-quarkus-jms`](https://camel.apache.org/camel-quarkus/latest/reference/extensions/jms.html) and [`quarkus-qpid-jms`](https://github.com/amqphub/quarkus-qpid-jms) to connect to the broker over AMQP with mTLS. It was written specifically for this tutorial to demonstrate how a `BrokerApp` resource binds an application to an operator-managed `BrokerService`. See the [`camel-jms-app` README](camel-jms-app/README.md) for full details on available roles and environment configuration.

The same Docker image (`camel-jms-app`) is deployed four times — once for each pipeline stage. The role and queue configuration are provided through environment variables, so the image does not need to be rebuilt for each application.

The main configuration variables are:

| Variable | Purpose |
|---|---|
| `APP_ROLE` | Selects which Camel route the application runs |
| `CONSUMER_QUEUE` | Queue the application consumes from |
| `PRODUCER_QUEUE` | Queue the application produces to |
| `MESSAGE_RATE` | Messages per second for the generator |
| `PROCESSING_DELAY_MS` | Simulated processing delay per message |
| `CONSUMER_CONCURRENCY` | Number of concurrent JMS consumers per pod |

Each Camel deployment needs two things for mTLS:

- Its own **app certificate** (for example, `order-generator-app-cert`). cert-manager creates this certificate, and the Deployment mounts it at `/app/tls/client/`.
- A **PEM keystore configuration** that tells the [dentrassi PEM keystore](https://github.com/ctron/pem-keystore) library where to find the certificate and private key.

The PEM configuration is identical for all four applications because they all use the same filesystem paths:

- `/app/tls/client/tls.key`
- `/app/tls/client/tls.crt`

However, each Deployment mounts a different certificate Secret at that path. This means the applications share the same PEM configuration while retaining separate mTLS identities.

### PEM keystore configuration

Create the shared PEM configuration Secret once. All Deployments reference this single Secret. The config contains only filesystem paths — the actual mTLS identity comes from each application's own cert-manager-issued certificate Secret:

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
```shell markdown_runner
secret/cert-pemcfg created
```

### order-generator

`order-generator` produces 5 order messages per second into `ORDERS.NEW`. The rate is controlled by the `MESSAGE_RATE` environment variable in the Camel Deployment. The BrokerApp only declares the messaging capability (`producerOf: ORDERS.NEW`).

To change the message rate, update the Deployment's `MESSAGE_RATE` value — no image rebuild is required.

Before deploying the application, wait for the binding Secret created for its BrokerApp:

```bash {"stage":"deploy_camel", "label":"wait for order-generator binding secret", "runtime":"bash"}
kubectl wait secret order-generator-binding-secret -n service-app-project --for=create --timeout=300s
```
```shell markdown_runner
secret/order-generator-binding-secret condition met
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
        image: camel-jms-app:latest
        imagePullPolicy: Never
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
          secretName: cert-pemcfg
EOF
```
```shell markdown_runner
deployment.apps/order-generator created
```

```bash {"stage":"deploy_camel", "label":"wait for order-generator", "runtime":"bash"}
kubectl wait deployment order-generator -n service-app-project --for=condition=Available --timeout=300s
```
```shell markdown_runner
deployment.apps/order-generator condition met
```

### order-processor-app (Order Processor)

```bash {"stage":"deploy_camel", "label":"wait for order-processor-app binding secret", "runtime":"bash"}
kubectl wait secret order-processor-binding-secret -n service-app-project --for=create --timeout=300s
```
```shell markdown_runner
secret/order-processor-binding-secret condition met
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
        image: camel-jms-app:latest
        imagePullPolicy: Never
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
              name: order-processor-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: order-processor-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "order-processor"
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
          secretName: cert-pemcfg
EOF
```
```shell markdown_runner
deployment.apps/order-processor-app created
```

```bash {"stage":"deploy_camel", "label":"wait for order-processor-app", "runtime":"bash"}
kubectl wait deployment order-processor-app -n service-app-project --for=condition=Available --timeout=300s
```
```shell markdown_runner
deployment.apps/order-processor-app condition met
```

### shipping-service-app (Shipping Service)

```bash {"stage":"deploy_camel", "label":"wait for shipping-service-app binding secret", "runtime":"bash"}
kubectl wait secret shipping-service-binding-secret -n service-app-project --for=create --timeout=300s
```
```shell markdown_runner
secret/shipping-service-binding-secret condition met
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
        image: camel-jms-app:latest
        imagePullPolicy: Never
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
              name: shipping-service-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: shipping-service-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "shipping-service"
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
          secretName: cert-pemcfg
EOF
```
```shell markdown_runner
deployment.apps/shipping-service-app created
```

```bash {"stage":"deploy_camel", "label":"wait for shipping-service-app", "runtime":"bash"}
kubectl wait deployment shipping-service-app -n service-app-project --for=condition=Available --timeout=300s
```
```shell markdown_runner
deployment.apps/shipping-service-app condition met
```

### delivery-service-app (Delivery Service)

```bash {"stage":"deploy_camel", "label":"wait for delivery-service-app binding secret", "runtime":"bash"}
kubectl wait secret delivery-service-binding-secret -n service-app-project --for=create --timeout=300s
```
```shell markdown_runner
secret/delivery-service-binding-secret condition met
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
        image: camel-jms-app:latest
        imagePullPolicy: Never
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
              name: delivery-service-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: delivery-service-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "delivery-service"
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
          secretName: cert-pemcfg
EOF
```
```shell markdown_runner
deployment.apps/delivery-service-app created
```

```bash {"stage":"deploy_camel", "label":"wait for delivery-service-app", "runtime":"bash"}
kubectl wait deployment delivery-service-app -n service-app-project --for=condition=Available --timeout=300s
```
```shell markdown_runner
deployment.apps/delivery-service-app condition met
```

### master-sink (Optional operational drain)

`master-sink` is not part of the business processing pipeline. It is an optional operational drain that you enable when you want to consume messages accumulating on the terminal `ORDERS.DELIVERED` queue — for example, to prevent unbounded growth during a long-running demo, or as an explicit "pipeline complete" acknowledgement.

During the normal pipeline demonstration and the bottleneck/scale scenarios, keep this deployment at **0 replicas** so that `ORDERS.DELIVERED` depth remains visible in Grafana.

```bash {"stage":"deploy_camel", "label":"create master-sink cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: master-sink-app-cert
  namespace: service-app-project
spec:
  secretName: master-sink-app-cert
  commonName: master-sink
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```
```shell markdown_runner
certificate.cert-manager.io/master-sink-app-cert created
```

```bash {"stage":"deploy_camel", "label":"wait for master-sink cert", "runtime":"bash"}
kubectl wait certificate master-sink-app-cert -n service-app-project --for=condition=Ready --timeout=300s
kubectl wait secret master-sink-app-cert -n service-app-project --for=create --timeout=60s
```
```shell markdown_runner
certificate.cert-manager.io/master-sink-app-cert condition met
secret/master-sink-app-cert condition met
```

```bash {"stage":"deploy_camel", "label":"deploy master-sink brokerapp", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: master-sink
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      app: "order-processing-pipeline"
  capabilities:
    - consumerOf:
        - address: "ORDERS.DELIVERED"
          appName: "delivery-service"
          appNamespace: "service-app-project"
EOF
```
```shell markdown_runner
brokerapp.broker.arkmq.org/master-sink created
```

```bash {"stage":"deploy_camel", "label":"wait for master-sink brokerapp", "runtime":"bash"}
kubectl wait brokerapp/master-sink -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerapp.broker.arkmq.org/master-sink condition met
```

The Camel JMS app uses the [dentrassi PEM keystore](https://github.com/ctron/pem-keystore) library to handle mTLS. The `master-sink-app-cert` Secret contains the TLS certificate and private key, which are mounted in the container at `/app/tls/client/`.

```bash {"stage":"deploy_camel", "label":"wait for master-sink binding secret", "runtime":"bash"}
kubectl wait secret master-sink-binding-secret -n service-app-project --for=create --timeout=300s
```
```shell markdown_runner
secret/master-sink-binding-secret condition met
```

```bash {"stage":"deploy_camel", "label":"deploy master-sink camel app", "runtime":"bash"}
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
        image: camel-jms-app:latest
        imagePullPolicy: Never
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
              name: master-sink-binding-secret
              key: host
        - name: BROKER_PORT
          valueFrom:
            secretKeyRef:
              name: master-sink-binding-secret
              key: port
        - name: CLIENT_USERNAME
          value: "master-sink"
        - name: APP_ROLE
          value: "sink"
        - name: CONSUMER_QUEUE
          value: "ORDERS.DELIVERED"
        - name: PRODUCER_QUEUE
          value: "NONE"
        - name: CONSUMER_CONCURRENCY
          value: "5"
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
          secretName: cert-pemcfg
EOF
```
```shell markdown_runner
deployment.apps/camel-jms-master-sink created
```

The Camel Deployment for master-sink is deployed at **0 replicas**. Scale it up only when you want to actively drain `ORDERS.DELIVERED`.

To drain `ORDERS.DELIVERED` at any point during the tutorial, scale it up:

```bash
kubectl scale deployment camel-jms-master-sink --replicas=1 -n service-app-project
```

To stop draining and let the queue accumulate again:

```bash
kubectl scale deployment camel-jms-master-sink --replicas=0 -n service-app-project
```

### Verify the Pipeline

Check that orders are flowing through all stages:

```bash {"stage":"verify", "label":"check generator logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/order-generator --tail=5
```
```shell markdown_runner
2026-09-18 10:41:05,916 INFO  [org.apache.qpid.jms.JmsConnection] (AmqpProvider :(398):[amqps://messaging-service.service-app-project.svc.cluster.local:61616]) Connection ID:79009c7a-5105-482f-b8dd-7a332bd90f1c:398 connected to server: amqps://messaging-service.service-app-project.svc.cluster.local:61616
2026-09-18 10:41:06,138 INFO  [order-generator] (Camel (camel-1) thread #1 - timer://order-generator) [generator] → ORDERS.NEW | orderId=ORD-bd1b67
2026-09-18 10:41:06,168 INFO  [org.apache.qpid.jms.JmsConnection] (AmqpProvider :(399):[amqps://messaging-service.service-app-project.svc.cluster.local:61616]) Connection ID:6ab23265-b50f-4d8f-8325-e7b01d0a3acf:399 connected to server: amqps://messaging-service.service-app-project.svc.cluster.local:61616
2026-09-18 10:41:06,338 INFO  [order-generator] (Camel (camel-1) thread #1 - timer://order-generator) [generator] → ORDERS.NEW | orderId=ORD-2bd4b0
2026-09-18 10:41:06,360 INFO  [org.apache.qpid.jms.JmsConnection] (AmqpProvider :(400):[amqps://messaging-service.service-app-project.svc.cluster.local:61616]) Connection ID:ad1e732e-f016-4b4f-9062-b49ced526553:400 connected to server: amqps://messaging-service.service-app-project.svc.cluster.local:61616
```

```bash {"stage":"verify", "label":"check processor logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/order-processor-app --tail=5
```
```shell markdown_runner
2026-09-18 10:41:06,138 INFO  [order-processor] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.NEW]) [processor] ← ORDERS.NEW | processing...
2026-09-18 10:41:06,239 INFO  [order-processor] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.NEW]) [processor] → ORDERS.PROCESSED | status=PROCESSED
2026-09-18 10:41:06,364 INFO  [order-processor] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.NEW]) [processor] ← ORDERS.NEW | processing...
2026-09-18 10:41:06,465 INFO  [order-processor] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.NEW]) [processor] → ORDERS.PROCESSED | status=PROCESSED
2026-09-18 10:41:06,558 INFO  [order-processor] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.NEW]) [processor] ← ORDERS.NEW | processing...
```

```bash {"stage":"verify", "label":"check shipping logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/shipping-service-app --tail=5
```
```shell markdown_runner
2026-09-18 10:41:06,364 INFO  [order-shipping] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.PROCESSED]) [shipping] ← ORDERS.PROCESSED | shipping...
2026-09-18 10:41:06,390 INFO  [order-shipping] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.PROCESSED]) [shipping] → ORDERS.SHIPPED | status=SHIPPED
2026-09-18 10:41:06,558 INFO  [order-shipping] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.PROCESSED]) [shipping] ← ORDERS.PROCESSED | shipping...
2026-09-18 10:41:06,584 INFO  [order-shipping] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.PROCESSED]) [shipping] → ORDERS.SHIPPED | status=SHIPPED
2026-09-18 10:41:06,696 INFO  [order-shipping] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.PROCESSED]) [shipping] ← ORDERS.PROCESSED | shipping...
```

```bash {"stage":"verify", "label":"check delivery logs", "runtime":"bash"}
kubectl logs -n service-app-project deployment/delivery-service-app --tail=5
```
```shell markdown_runner
2026-09-18 10:41:06,398 INFO  [order-delivery] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.SHIPPED]) [delivery] ← ORDERS.SHIPPED | delivering...
2026-09-18 10:41:06,424 INFO  [order-delivery] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.SHIPPED]) [delivery] → ORDERS.DELIVERED | status=DELIVERED
2026-09-18 10:41:06,695 INFO  [order-delivery] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.SHIPPED]) [delivery] ← ORDERS.SHIPPED | delivering...
2026-09-18 10:41:06,720 INFO  [order-delivery] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.SHIPPED]) [delivery] → ORDERS.DELIVERED | status=DELIVERED
2026-09-18 10:41:06,839 INFO  [order-delivery] (Camel (camel-1) thread #1 - JmsConsumer[ORDERS.SHIPPED]) [delivery] ← ORDERS.SHIPPED | delivering...
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
```shell markdown_runner
certificate.cert-manager.io/prometheus-cert created
```

```bash {"stage":"monitoring", "label":"wait for prometheus cert", "runtime":"bash"}
kubectl wait certificate prometheus-cert -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
certificate.cert-manager.io/prometheus-cert condition met
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
```shell markdown_runner
service/messaging-service-metrics created
```

### Create ServiceMonitor

```bash {"stage":"monitoring", "label":"create servicemonitor", "runtime":"bash"}
kubectl wait pod/messaging-service-ss-0 -n service-app-project --for=condition=Ready --timeout=300s
export BROKER_POD=$(kubectl get pods \
  -n service-app-project \
  -l ActiveMQArtemis=messaging-service \
  -o jsonpath='{.items[0].metadata.name}')
export BROKER_FQDN="${BROKER_POD}.messaging-service-hdls-svc.service-app-project.svc.cluster.local"
echo "Broker FQDN: ${BROKER_FQDN}"
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
```shell markdown_runner
pod/messaging-service-ss-0 condition met
Broker FQDN: messaging-service-ss-0.messaging-service-hdls-svc.service-app-project.svc.cluster.local
servicemonitor.monitoring.coreos.com/messaging-service-monitor created
```

### Create Prometheus Recording Rules

Create a recording rule that pre-aggregates total consumer count across all queues. This makes it easy to query `artemis:total_consumer_count` directly in Prometheus without writing the full aggregation each time:

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
    # Total consumer count — query as: artemis:total_consumer_count
    - record: artemis:total_consumer_count
      expr: sum(broker_queue_consumer_count{job="messaging-service-metrics"})
EOF
```
```shell markdown_runner
prometheusrule.monitoring.coreos.com/artemis-aggregation-rules created
```

### Apply Grafana Dashboard

```bash {"stage":"grafana", "label":"create dashboard configmap", "rootdir":"$initial_dir", "runtime":"bash"}
kubectl apply -f docs/tutorials/brokerservice/assets/artemis-dashboard-configmap.yaml
```
```shell markdown_runner
configmap/artemis-broker-health created
```

### Access Grafana

Create an Ingress to expose Grafana through the Minikube ingress controller:

```bash {"stage":"grafana", "label":"create grafana ingress", "runtime":"bash"}
CLUSTER_IP=$(minikube ip --profile brokerservice-monitoring)
export GRAFANA_HOST=grafana.service-app-project.${CLUSTER_IP}.nip.io
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: grafana
  namespace: service-app-project
spec:
  ingressClassName: nginx
  rules:
    - host: ${GRAFANA_HOST}
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
echo "Grafana available at http://${GRAFANA_HOST}"
```
```shell markdown_runner
ingress.networking.k8s.io/grafana created
Grafana available at http://grafana.service-app-project.192.168.49.2.nip.io
```

```bash {"stage":"grafana", "label":"get grafana password", "runtime":"bash"}
kubectl get secret prometheus-grafana -n service-app-project -o jsonpath='{.data.admin-password}' | base64 -d && echo
```
```shell markdown_runner
T2bZa0a5DleMl3kxcj9XGOT09O26TF223b9FL7cH
```

Login at the URL printed above with username `admin` and the password printed above, then open the **"Artemis Broker - Memory & Queue Analysis"** dashboard. Panels will start showing data within one scrape interval (15 s).

### Verify Prometheus is Scraping

Wait for Prometheus to mark the broker target as `UP`:

```bash {"stage":"monitoring", "label":"verify prometheus target", "runtime":"bash"}
kubectl get servicemonitor messaging-service-monitor -n service-app-project
kubectl get endpointslice -l app=messaging-service -n service-app-project
```
```shell markdown_runner
NAME                        AGE
messaging-service-monitor   1s
NAME                              ADDRESSTYPE   PORTS   ENDPOINTS     AGE
messaging-service-metrics-whblz   IPv4          8888    10.244.0.19   1s
```

Open the Grafana dashboard — panels should now show live data within one scrape interval (15 s).

---

## 7. Grafana Dashboard Reference

The Grafana dashboard (**"Artemis Broker - Memory & Queue Analysis"**) was applied in Section 6 and is accessible at the URL from the Access Grafana step. The dashboard contains two panels:

| Panel | What to look for |
|---|---|
| **Container vs JVM vs Queue Memory** | Five lines in bytes: Container Working Set (purple), JVM Heap Used (orange), Queue Persistent Data (green), Container Memory Limit (blue dashed), JVM Heap Max (red dashed). Under normal load all lines are stable except Queue Persistent Data, which grows slowly as `ORDERS.DELIVERED` accumulates. |
| **Current Queue Breakdown** | Table showing persistent bytes and message count per queue. `ORDERS.DELIVERED` should be the top row with a growing count; all other queues should be near zero while the pipeline is healthy. |

## 8. Operations Scenarios

These scenarios demonstrate how to create a processing bottleneck, identify the resulting queue buildup in Grafana, and recover by scaling the bottleneck deployment.

The scenarios are intentionally interactive. Run the commands in each scenario and use the Grafana dashboard to observe the resulting state. Scenario 3 continues directly from Scenario 2, so do not restore the shipping configuration between them.

### Scenario 1 — Normal Traffic

Everything is already running. Open the **"Artemis Broker - Memory & Queue Analysis"** dashboard and confirm that the pipeline is flowing at approximately 5 msg/s.

Under normal conditions you should see:

| Panel | Expected observation |
|---|---|
| Container vs JVM vs Queue Memory | Container Working Set and JVM Heap remain relatively stable. Queue Persistent Data grows slowly. |
| Current Queue Breakdown | `ORDERS.DELIVERED` grows while the other queues remain near zero. |

A growing `ORDERS.DELIVERED` queue is expected because `master-sink` is intentionally disabled. The terminal queue therefore provides a visible indication that messages are making it through the complete pipeline.

Each processing stage has capacity above the generator rate:

| Stage | Delay | Consumers | Approx. theoretical capacity |
|---|---|---|---|
| processor | 100 ms | 1 | ~10 msg/s |
| shipping | 25 ms | 1 | ~40 msg/s |
| delivery | 25 ms | 1 | ~40 msg/s |

**What to observe:** Because every stage has more processing capacity than the generator rate, messages pass through each queue almost immediately — no backlog accumulates in `ORDERS.NEW`, `ORDERS.PROCESSED`, or `ORDERS.SHIPPED`. The only queue that grows is `ORDERS.DELIVERED`, since its consumer (`master-sink`) is deliberately disabled.

![Scenario 1 — Normal traffic: only ORDERS.DELIVERED accumulates while all intermediate queues remain near zero](assets/images/scenario-1-normal.png)

To drain `ORDERS.DELIVERED`, scale the optional sink to one replica:

```bash
kubectl scale deployment camel-jms-master-sink \
  --replicas=1 \
  -n service-app-project
```

To stop the sink again:

```bash
kubectl scale deployment camel-jms-master-sink \
  --replicas=0 \
  -n service-app-project
```

### Scenario 2 — Create a Bottleneck

This scenario intentionally slows the shipping service so that it processes messages more slowly than they are produced.

Run:

```bash
kubectl set env deployment/shipping-service-app \
  PROCESSING_DELAY_MS=2000 \
  -n service-app-project

kubectl rollout status deployment/shipping-service-app \
  -n service-app-project \
  --timeout=120s
```

The shipping service still has one consumer, but each message now has a processing delay of 2 seconds. Its theoretical processing capacity is therefore approximately 0.5 msg/s.

The generator continues producing approximately 5 msg/s. Under idealized conditions, the difference is approximately:

> 5 msg/s produced − 0.5 msg/s processed = 4.5 msg/s backlog growth

Actual throughput can be lower because of JMS overhead, scheduling, and other runtime effects.

#### Observe the bottleneck in Grafana

Open the **"Artemis Broker - Memory & Queue Analysis"** dashboard.

In the **Current Queue Breakdown** panel:

- Locate `ORDERS.PROCESSED`.
- Look at its message count.
- Compare it with the other queues.
- Observe the accumulated messages while the shipping consumer is constrained.

**Expected observation:** `ORDERS.PROCESSED` should accumulate messages because the shipping service is processing them more slowly than the order processor is producing them.

![Scenario 2 — ORDERS.PROCESSED backlog building up while the shipping service is constrained to 0.5 msg/s](assets/images/scenario-2-bottleneck.png)

Continue directly to Scenario 3. Do not restore the shipping configuration yet.

### Scenario 3 — Scale to Recover

This scenario continues from Scenario 2. The shipping service should still have:

- `PROCESSING_DELAY_MS=2000`
- `CONSUMER_CONCURRENCY=1`

First, scale the shipping deployment to five replicas:

```bash
kubectl scale deployment shipping-service-app \
  --replicas=5 \
  -n service-app-project

kubectl wait deployment shipping-service-app \
  -n service-app-project \
  --for=condition=Available \
  --timeout=300s
```

At this point, there are five shipping pods, but each still has the 2-second processing delay. The theoretical aggregate capacity is therefore approximately 2.5 msg/s, which is still below the generator rate of 5 msg/s.

Restore the normal processing delay:

```bash
kubectl set env deployment/shipping-service-app \
  PROCESSING_DELAY_MS=25 \
  -n service-app-project

kubectl rollout status deployment/shipping-service-app \
  -n service-app-project \
  --timeout=120s
```

With five replicas and a 25 ms processing delay, the theoretical processing capacity is approximately 200 msg/s. The actual throughput depends on JMS and runtime overhead, but it is now substantially above the 5 msg/s input rate.

#### Observe recovery in Grafana

Return to the **"Artemis Broker - Memory & Queue Analysis"** dashboard.

In the **Current Queue Breakdown** panel:

- Locate `ORDERS.PROCESSED`.
- Look at its message count.
- Observe the queue after the shipping deployment has been scaled and the processing delay restored.

The accumulated messages should begin to drain as processing capacity exceeds the incoming rate.

**Expected observation:** `ORDERS.PROCESSED` should decrease as the five shipping consumers process the accumulated backlog faster than new messages arrive.

**3a — Immediately after scaling (5 replicas, 25 ms delay applied)**

`ORDERS.PROCESSED` still contains messages accumulated during the bottleneck period, while the scaled shipping deployment is now processing them. Depending on timing, `ORDERS.SHIPPED` may also contain transient messages as the downstream delivery stage processes them. `ORDERS.NEW` has reached zero, indicating that the order processor is keeping up with the generator.

![Scenario 3a — Queue state immediately after scaling shipping to 5 replicas and restoring the 25 ms delay](assets/images/scenario-3a-scaling.png)

**3b — After the backlog has drained**

`ORDERS.PROCESSED` has dropped to 0 messages, while `ORDERS.SHIPPED` remains near zero or is clearing. `ORDERS.DELIVERED` continues to grow because `master-sink` is disabled. This confirms that five shipping consumers at 25 ms delay provide more than enough capacity to absorb the 5 msg/s input rate and clear the accumulated backlog.

![Scenario 3b — ORDERS.PROCESSED fully drained after scaling; only ORDERS.DELIVERED continues to accumulate](assets/images/scenario-3b-drained.png)

#### What the scenario demonstrates

The operational sequence is:

```
Normal pipeline
      ↓
Slow shipping consumer
      ↓
ORDERS.PROCESSED accumulates
      ↓
Grafana reveals the queue buildup
      ↓
Scale shipping deployment
      ↓
Restore normal processing delay
      ↓
ORDERS.PROCESSED drains
```

This demonstrates how queue-depth metrics can be used to identify a processing bottleneck and verify recovery after increasing consumer capacity.

### Restore the Baseline Configuration

After completing the scenarios, restore the shipping deployment to its original configuration:

```bash
kubectl scale deployment shipping-service-app \
  --replicas=1 \
  -n service-app-project

kubectl set env deployment/shipping-service-app \
  PROCESSING_DELAY_MS=25 \
  -n service-app-project

kubectl rollout status deployment/shipping-service-app \
  -n service-app-project \
  --timeout=120s
```

If you enabled `master-sink` during the tutorial, disable it again:

```bash
kubectl scale deployment camel-jms-master-sink \
  --replicas=0 \
  -n service-app-project
```

The baseline shipping configuration is:

| Setting | Baseline |
|---|---|
| Replicas | 1 |
| `PROCESSING_DELAY_MS` | 25 ms |
| `CONSUMER_CONCURRENCY` | 1 |

---

## Cleanup

To fully tear down the tutorial environment, delete the Minikube cluster. This removes the entire cluster including the operator, all deployed resources, and all cluster-scoped CRDs:

```bash
minikube delete --profile brokerservice-monitoring
```

If you want to clean up only the tutorial resources while keeping the cluster running, delete the namespace and the ClusterIssuers explicitly. ClusterIssuers are not namespace-scoped and survive namespace deletion, so they must be removed separately:

```bash
# Remove all namespace-scoped resources
kubectl delete namespace service-app-project

# Remove cluster-scoped resources created by this tutorial
kubectl delete clusterissuer root-issuer broker-ca-issuer
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
