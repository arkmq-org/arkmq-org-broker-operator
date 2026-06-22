---
title: "Connecting to the broker from external mqtt clients"  
description: "Expose an mqtt acceptor with ssl enabled to accept connections from external mqtt clients"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 110
toc: true
---

When you expose an acceptor to external clients (that is, by setting the value of the expose parameter to true), the Operator automatically creates an ingress on Kubernetes or a route on OpenShift for each broker pod of the deployment. An external client can connect to the broker by specifying the full host name of the ingress/route created for the broker pod.

# Prerequisite
Before you start you need to have access to a running Kubernetes cluster environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) with [Ingress](https://kubernetes.io/docs/tasks/access-application-cluster/ingress-minikube/) running on your laptop will just do fine. The arkmq-org operator also runs in Openshift cluster environment like [CodeReady Container](https://developers.redhat.com/products/openshift-local/overview). In this blog we assume you have Kubernetes cluster environment. Execute the following command to enable Ingress in minikube:

```shell script
$ minikube addons enable ingress
```

# Enable SSL Passthrough
[SSL Passthrough](https://kubernetes.github.io/ingress-nginx/user-guide/tls/) leverages [SNI](https://en.wikipedia.org/wiki/Server_Name_Indication) and reads the virtual domain from the TLS negotiation, which requires compatible clients. After a connection has been accepted by the TLS listener, it is handled by the controller itself and piped back and forth between the backend and the client. Execute the following command to enable SSL Passthrough in minikube:

```shell script
$ minikube kubectl -- patch deployment -n ingress-nginx ingress-nginx-controller --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value":"--enable-ssl-passthrough"}]'
```

# Deploy Arkmq-org operator
First you need to deploy the Arkmq-org operator.
If you are not sure how to deploy the operator take a look at [this blog](using_operator.md).

# Download the test certficates from Apache Artemis
```shell script
$ wget -O server-keystore.jks https://github.com/apache/artemis/raw/main/tests/security-resources/server-keystore.jks
$ wget -O client-ca-truststore.jks https://github.com/apache/artemis/raw/main/tests/security-resources/client-ca-truststore.jks
$ wget -O server-ca-keystore.p12 https://github.com/apache/artemis/raw/main/tests/security-resources/server-ca-keystore.p12
$ keytool -storetype pkcs12 -keystore server-ca-keystore.p12 -storepass securepass -alias server-ca -exportcert -rfc > server-ca.crt
```

# Create a secret with the test certificates
Use the following command to create a secret with the test certificates:
```shell script
$ kubectl create secret generic my-tls-secret \
--from-file=broker.ks=server-keystore.jks \
--from-file=client.ts=client-ca-truststore.jks \
--from-literal=keyStorePassword=securepass \
--from-literal=trustStorePassword=securepass
```

### Deploy Broker with an mqtt acceptor
Use the following command to deploy Broker with an mqtt acceptor:
```shell script
$ kubectl apply -f - <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerCluster
metadata:
  name: artemis-mqtt-ssl
spec:
  acceptors:
    - name: my-acceptor
      expose: true
      port: 5672
      protocols: mqtt
      sslEnabled: true
      sslSecret: my-tls-secret
  env:
    - name: JAVA_ARGS_APPEND
      value: -Djavax.net.debug=all
EOF
```

### Publish a message with mosquitto
[Eclipse Mosquitto](https://mosquitto.org/) is an open source project and it provides a message broker that implements the MQTT protocol, a C library for implementing MQTT clients, and the very popular mosquitto_pub and mosquitto_sub command line MQTT clients.

Use the following command to publish a message with mosquitto_pub from your host:
```shell script
$ mosquitto_pub -d --insecure -t "test" -m "test" -u admin -P admin  -h artemis-mqtt-ssl-my-acceptor-0-svc-ing.apps.arkmq-org.io -p 443 --cafile server-ca.crt
```

Alternatively you can execute mosquitto_pub from the eclipse-mosquitto container running on your host with [podman](https://podman.io/). Use the following command to publish a message with mosquitto_pub from the eclipse-mosquitto container running on your host:
```shell script
$ podman run --name mosquitto_pub -it --rm --add-host artemis-mqtt-ssl-my-acceptor-0-svc-ing.apps.arkmq-org.io:$(minikube ip) --network host --entrypoint /usr/bin/mosquitto_pub -v ${PWD}/server-ca.crt:/mosquitto/config/server-ca.crt:Z eclipse-mosquitto -d --insecure -t "test" -m "test" -u admin -P admin  -h artemis-mqtt-ssl-my-acceptor-0-svc-ing.apps.arkmq-org.io -p 443 --cafile /mosquitto/config/server-ca.crt
```
