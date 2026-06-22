---
title: "Setting up SSL connections with ArkMQ Operator"  
description: "An example for setting up ssl connections for broker in kubernetes with operator"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 110
toc: true
---

Security is always a concern in a production environment. With arkmq-org Operator
You can easily configure and set up a broker with ssl-enabled acceptors. The blog explains how to do it.

The [Apache Artemis](https://artemis.apache.org/components/artemis/) broker supports a variety of network protocols(tcp, http, etc) including [SSL(TLS)](https://en.wikipedia.org/wiki/Transport_Layer_Security) secure connections. Underneath it uses [Netty](https://netty.io/) as the base transport layer.

This article guides you through the steps to set up a broker to run in kubernetes (Minikube). The broker will listen on a secure port 61617 (ssl over tcp). It also demonstrates sending and receiving messages over secure connections using one-way authentication.

### Prerequisite
Before you start you need have access to a running Kubernetes cluster environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) running on your laptop will just do fine. The ArkMQ operator also runs in a Openshift cluster environment like [CodeReady Container](https://developers.redhat.com/products/openshift-local/overview).


#### Start minikube with a parametrized dns domain name

```{"stage":"init", "id":"minikube_start"}
minikube start --profile tutorialtester
minikube profile tutorialtester
```
```shell markdown_runner
* [tutorialtester] minikube v1.37.0 on Fedora 43
* Using the kvm2 driver based on user configuration
* Starting "tutorialtester" primary control-plane node in "tutorialtester" cluster
* Configuring bridge CNI (Container Networking Interface) ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: default-storageclass, storage-provisioner
* Done! kubectl is now configured to use "tutorialtester" cluster and "default" namespace by default
* minikube profile was successfully set to tutorialtester
```


### Deploy the operator
First you need to deploy the arkmq-org operator.
For further details on how to deploy the operator take a look at [this blog](using_operator.md).

#### Create the namespace
In this tutorial the operator is deployed to a namespace called **ssl-broker-project**.

```{"stage":"init"}
kubectl create namespace ssl-broker-project
kubectl config set-context --current --namespace=ssl-broker-project
```
```shell markdown_runner
namespace/ssl-broker-project created
Context "tutorialtester" modified.
```

Go to the root of the operator repo and install it:

```{"stage":"init", "rootdir":"$initial_dir"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
Client Version: 4.20.8
Kustomize Version: v5.6.0
Kubernetes Version: v1.34.0
customresourcedefinition.apiextensions.k8s.io/activemqartemises.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
serviceaccount/arkmq-org-broker-controller-manager created
role.rbac.authorization.k8s.io/arkmq-org-broker-operator-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-operator-rolebinding created
role.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-rolebinding created
deployment.apps/arkmq-org-broker-controller-manager created
Warning: unrecognized format "int32"
Warning: unrecognized format "int64"
```

Wait for the Operator to start (status: `running`).

```{"stage":"init", "label":"wait for the operator to be running"}
kubectl rollout status deployment/arkmq-org-broker-controller-manager --timeout=600s
```
```shell markdown_runner
Waiting for deployment spec update to be observed...
Waiting for deployment spec update to be observed...
Waiting for deployment "arkmq-org-broker-controller-manager" rollout to finish: 0 out of 1 new replicas have been updated...
Waiting for deployment "arkmq-org-broker-controller-manager" rollout to finish: 0 of 1 updated replicas are available...
deployment "arkmq-org-broker-controller-manager" successfully rolled out
```

### Prepare keystore and truststore
To establish a SSL connection you need certificates. Here for demonstration purpose we prepare a self-signed certificate.

We'll use the "keytool" utility that comes with JDK:

```{"stage":"cert-init", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"create broker keystore"}
keytool -genkeypair -alias arkmq-broker -keyalg RSA -keysize 2048 -storetype PKCS12 -keystore broker.ks -storepass securepass -validity 3000 -dname "CN=ex-aao-ss-0, OU=Broker, O=ArkMQ"
```
```shell markdown_runner
Generating 2048-bit RSA key pair and self-signed certificate (SHA384withRSA) with a validity of 3,000 days
	for: CN=ex-aao-ss-0, OU=Broker, O=ArkMQ
```
It creates a keystore file named **broker.ks** under the current directory.

Next make a truststore using the same cert in the keystore.

```{"stage":"cert-init", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"export broker certificate"}
keytool -export -alias arkmq-broker -file broker.cert -keystore broker.ks -storepass securepass
```
```shell markdown_runner
Certificate stored in file <broker.cert>
```
```{"stage":"cert-init", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"create client truststore"}
keytool -import -v -trustcacerts -alias arkmq-broker -file broker.cert -keystore client.ts -storepass securepass -noprompt
```
```shell markdown_runner
Certificate was added to keystore
[Storing client.ts]
```

By default the operator fetches the truststore and keystore from a secret in kubernetes in order to configure SSL acceptors for a broker. The secret name is deducted from broker CR's name combined with the acceptor's name.

Here we'll use "ex-aao" for CR's name and "sslacceptor" for the acceptor's name. So the truststore and keystore should be stored in a secret named **ex-aao-sslacceptor-secret**.

Run the following command to create the secret we need:
```{"stage":"cert-init", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"create acceptor ssl secret"}
kubectl create secret generic ex-aao-sslacceptor-secret --from-file=broker.ks --from-file=client.ts --from-literal=keyStorePassword='securepass' --from-literal=trustStorePassword='securepass'
```
```shell markdown_runner
secret/ex-aao-sslacceptor-secret created
```

### Prepare the broker CR with SSL enabled
Now create a broker CR with an acceptor named "sslacceptor" that listens on tcp port 61617.
The **sslEnabled: true** tells the operator to make this acceptor to use SSL transport.

```{"stage":"deploy", "runtime":"bash", "label":"deploy the broker"}
kubectl apply -f - << EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerCluster
metadata:
  name: ex-aao
spec:
  acceptors:
  - name: sslacceptor
    protocols: all
    port: 61617
    sslEnabled: true
EOF
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao created
```

Wait for the Broker to be ready:

```{"stage":"deploy"}
kubectl wait BrokerCluster ex-aao --for=condition=Ready --namespace=ssl-broker-project --timeout=240s
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao condition met
```

### Test messaging over a SSL connection
With the broker pod in running status we can proceed to make some connections against it and do some simple messaging. We'll use Artemis broker's built in CLI commands to do this.

```{"stage":"test", "label":"send 100 messages"}
kubectl exec ex-aao-ss-0 -- /bin/bash -c 'cd amq-broker/bin && ./artemis producer --user admin --password admin --url tcp://ex-aao-ss-0:61617?sslEnabled=true\&trustStorePath=/etc/ex-aao-sslacceptor-secret-volume/client.ts\&trustStorePassword=securepass --message-count 100'
```
```shell markdown_runner
Connection brokerURL = tcp://ex-aao-ss-0:61617?sslEnabled=true&trustStorePath=/etc/ex-aao-sslacceptor-secret-volume/client.ts&trustStorePassword=securepass
Producer ActiveMQQueue[TEST], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[TEST], thread=0 Produced: 100 messages
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in second : 0 s
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in milli second : 809 milli seconds
Defaulted container "ex-aao-container" out of: ex-aao-container, ex-aao-container-init (init)
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/ex-aao-props/broker.properties
```

Pay attention to the **--url** option that is required to make an SSL connection to the broker.

You may also wonder how it gets the **trustStorePath** for the connection.

This is because the truststore and keystore are mounted automatically by the operator when it processes the broker CR. The mount path follows the pattern derived from CR's name (ex-aao) and the acceptor's name (sslacceptor, thus **/etc/ex-aao-sslacceptor-secret-volume**).

Now receive the messages we just sent -- also using SSL over the same port (61617):

```{"stage":"test", "label":"receive 100 messages"}
kubectl exec ex-aao-ss-0 -- /bin/bash -c 'cd amq-broker/bin && ./artemis consumer --user admin --password admin --url tcp://ex-aao-ss-0:61617?sslEnabled=true\&trustStorePath=/etc/ex-aao-sslacceptor-secret-volume/client.ts\&trustStorePassword=securepass --message-count 100'
```
```shell markdown_runner
Connection brokerURL = tcp://ex-aao-ss-0:61617?sslEnabled=true&trustStorePath=/etc/ex-aao-sslacceptor-secret-volume/client.ts&trustStorePassword=securepass
Consumer:: filter = null
Consumer ActiveMQQueue[TEST], thread=0 wait 3000ms until 100 messages are consumed
Consumer ActiveMQQueue[TEST], thread=0 Consumed: 100 messages
Consumer ActiveMQQueue[TEST], thread=0 Elapsed time in second : 0 s
Consumer ActiveMQQueue[TEST], thread=0 Elapsed time in milli second : 66 milli seconds
Consumer ActiveMQQueue[TEST], thread=0 Consumed: 100 messages
Consumer ActiveMQQueue[TEST], thread=0 Consumer thread finished
Defaulted container "ex-aao-container" out of: ex-aao-container, ex-aao-container-init (init)
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/ex-aao-props/broker.properties
```

Now you get an idea how an SSL acceptor is configured and processed by the operator and see it in action!

### cleanup

To leave a pristine environment after executing this tutorial you can simply,
delete the minikube cluster and clean the `/etc/hosts` file.

```{"stage":"teardown", "requires":"init/minikube_start"}
minikube delete --profile tutorialtester
```
```shell markdown_runner
* Deleting "tutorialtester" in kvm2 ...
* Removed all traces of the "tutorialtester" cluster.
```
