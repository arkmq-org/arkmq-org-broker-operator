---
title: "Service and App CRD Round Trip"
description: "A tutorial on using BrokerService and BrokerApp CRDs, based on the 'round trip simple' e2e test."
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 121
toc: true
---

This tutorial walks through a complete round trip of sending and receiving messages using the `BrokerService` and `BrokerApp` CRDs.

### Prerequisites

- A running Kubernetes cluster (this tutorial uses `minikube`).
- `kubectl` configured to interact with your cluster.

### 1. Setup

#### Start Minikube

```bash {"stage":"init", "id":"minikube_start", "runtime":"bash"}
minikube start --profile service-app-tutorial --extra-config=kubelet.sync-frequency=10s
minikube profile service-app-tutorial
```

#### Create Namespace

```bash {"stage":"init", "runtime":"bash" }
kubectl create namespace service-app-project
kubectl config set-context --current --namespace=service-app-project
```

#### Install Cert-Manager

```bash {"stage":"init", "label":"install cert-manager", "runtime":"bash"}
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.1/cert-manager.yaml
```

Wait for `cert-manager` to be ready.

```bash {"stage":"init", "label":"wait for cert-manager", "runtime":"bash"}
kubectl wait deployment --for=condition=Available -n cert-manager --timeout=600s cert-manager cert-manager-cainjector cert-manager-webhook
```

#### Install the Operator

```{"stage":"init", "rootdir":"$initial_dir"}
./deploy/install_opr.sh
```

Wait for the operator pod to become ready.

```bash {"stage":"init", "label":"wait for the operator to be running", "runtime":"bash"}
kubectl wait deployment arkmq-org-broker-controller-manager --for=create --timeout=240s
kubectl wait pod --all --for=condition=Ready --namespace=service-app-project --timeout=600s
```

### 2. Deploy the Messaging Service and Application

> **Note**: The operator provisions a three-plane PKI automatically:
> - **Operator plane** — internal management certs (broker, operator) under the service's CA.
> - **Metrics plane** — prometheus and per-app metrics certs under a dedicated metrics CA.
> - **App plane** — per-app CA, client cert, and server cert created when a `BrokerApp` is deployed.
>
> There is no need to create any certificates manually.

#### Deploy `BrokerService`

```bash {"stage":"deploy_service", "label":"deploy service crd", "runtime":"bash"}
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
    limits:
      memory: "1Gi"
  env:
    - name: JAVA_ARGS_APPEND
      value: "-Dlog4j2.level=INFO"
EOF
```

Wait for the resource to be ready. The operator will automatically create the
full PKI chain: operator-plane issuers and certs (broker, operator) plus
metrics-plane issuers and certs (metrics CA, prometheus cert).

```bash {"stage":"deploy_service", "label":"wait for service"}
kubectl wait BrokerService messaging-service -n service-app-project --for=condition=Ready --timeout=300s
```

You can verify the auto-provisioned PKI resources:

```bash {"stage":"deploy_service", "label":"verify PKI resources", "runtime":"bash"}
echo "--- Issuers ---"
kubectl get issuers -n service-app-project
echo "--- Certificates ---"
kubectl get certificates -n service-app-project
echo "--- Secrets ---"
kubectl get secrets -n service-app-project -l chain-of-trust/managed-by=arkmq-org-broker-operator
```

#### Deploy `BrokerApp`

The `BrokerApp` connects to a `BrokerService` using label selectors and declares
its messaging capabilities. The operator automatically assigns a port from the
service's port pool for the application's acceptor.

The operator auto-provisions the app's own PKI plane (CA, client cert, CA trust
secret) so the app can authenticate to the broker using mTLS. The app's PKI
exists independently of provisioning — configuration is decoupled from binding.

```bash {"stage":"deploy_app", "label":"deploy app crd", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerApp
metadata:
  name: first-app
  namespace: service-app-project
spec:
  selector:
    matchLabels:
      forWorkQueue: "true"
  capabilities:
    - producerOf:
        - address: "APP.JOBS"
      consumerOf:
        - address: "APP.JOBS"
EOF
```

Wait for the resource to be ready.

```bash {"stage":"deploy_app", "label":"wait for app", "runtime":"bash"}
kubectl wait BrokerApp first-app -n service-app-project --for=condition=Ready --timeout=300s
```

#### Verify Port Assignment

You can check the automatically assigned port in the app's status:

```bash {"stage":"deploy_app", "label":"check assigned port", "runtime":"bash"}
kubectl get BrokerApp first-app -n service-app-project -o jsonpath='{.status.service.assignedPort}'
```

### 3. Test Messaging

#### Create Client Configuration

```bash {"stage":"test_messaging", "label":"create pemcfg secret", "runtime":"bash"}
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

```bash {"stage":"test_messaging", "label":"wait for pemcfg secret", "runtime":"bash"}
until kubectl get secret cert-pemcfg -n service-app-project &> /dev/null; do echo "Waiting for secret..." && sleep 2; done
```

#### Run Producer Job

The producer job uses environment variables from the binding secret to connect to the
correct host and port assigned by the operator. The binding secret name follows the
pattern `{app-name}-binding-secret`.

The CA trust and client certificate secrets are auto-provisioned by the operator:
- `first-app-ca-trust` — contains the app's CA certificate (`tls.crt`)
- `first-app-app-cert` — contains the application's mTLS client certificate

```bash {"stage":"test_messaging", "label":"run producer", "runtime":"bash"}
cat <<'EOT' | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: producer
  namespace: service-app-project
spec:
  template:
    spec:
      containers:
      - name: producer
        image: quay.io/arkmq-org/arkmq-org-broker-kubernetes:artemis.2.40.0
        command:
        - "/bin/sh"
        - "-c"
        - exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url amqps://${BROKER_SERVICE_HOST}:${BROKER_SERVICE_PORT}\?transport.trustStoreType=PEMCA\&transport.trustStoreLocation=/app/tls/ca/ca.pem\&transport.keyStoreType=PEMCFG\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg --message-count 1 --destination queue://APP.JOBS;
        env:
        - name: JDK_JAVA_OPTIONS
          value: "-Djava.security.properties=/app/tls/pem/java.security"
        - name: BROKER_SERVICE_HOST
          valueFrom:
            secretKeyRef:
              name: first-app-binding-secret
              key: host
        - name: BROKER_SERVICE_PORT
          valueFrom:
            secretKeyRef:
              name: first-app-binding-secret
              key: port
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
        - name: cert
          mountPath: /app/tls/client
        - name: pem
          mountPath: /app/tls/pem
      volumes:
      - name: trust
        secret:
          secretName: first-app-ca-trust
          items:
          - key: tls.crt
            path: ca.pem
      - name: cert
        secret:
          secretName: first-app-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg
      restartPolicy: OnFailure
EOT
```

#### Run Consumer Job

The consumer job also uses the binding secret to access the service endpoint.

```bash {"stage":"test_messaging", "label":"run consumer", "runtime":"bash"}
cat <<'EOT' | kubectl apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: consumer
  namespace: service-app-project
spec:
  template:
    spec:
      containers:
      - name: consumer
        image: quay.io/arkmq-org/arkmq-org-broker-kubernetes:artemis.2.40.0
        command:
        - "/bin/sh"
        - "-c"
        - exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url amqps://${BROKER_SERVICE_HOST}:${BROKER_SERVICE_PORT}\?transport.trustStoreType=PEMCA\&transport.trustStoreLocation=/app/tls/ca/ca.pem\&transport.keyStoreType=PEMCFG\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg --message-count 1 --destination queue://APP.JOBS --receive-timeout 10000;
        env:
        - name: JDK_JAVA_OPTIONS
          value: "-Djava.security.properties=/app/tls/pem/java.security"
        - name: BROKER_SERVICE_HOST
          valueFrom:
            secretKeyRef:
              name: first-app-binding-secret
              key: host
        - name: BROKER_SERVICE_PORT
          valueFrom:
            secretKeyRef:
              name: first-app-binding-secret
              key: port
        volumeMounts:
        - name: trust
          mountPath: /app/tls/ca
        - name: cert
          mountPath: /app/tls/client
        - name: pem
          mountPath: /app/tls/pem
      volumes:
      - name: trust
        secret:
          secretName: first-app-ca-trust
          items:
          - key: tls.crt
            path: ca.pem
      - name: cert
        secret:
          secretName: first-app-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg
      restartPolicy: OnFailure
EOT
```

Wait for jobs to complete.

```bash {"stage":"test_messaging", "label":"wait for jobs", "runtime":"bash"}
kubectl wait job producer -n service-app-project --for=condition=Complete --timeout=300s
kubectl wait job consumer -n service-app-project --for=condition=Complete --timeout=300s
```

### 4. Cleanup

Delete our BrokerApp

```bash {"stage":"teardown", "label":"delete app", "runtime":"bash"}
kubectl delete BrokerApp first-app -n service-app-project
```

Finally, delete the minikube cluster.

```bash {"stage":"teardown", "requires":"init/minikube_start", "runtime":"bash"}
minikube delete --profile service-app-tutorial
```
