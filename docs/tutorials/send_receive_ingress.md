---
title: "Exchanging messages over an ssl ingress"  
description: "Steps to get a producer and a consummer exchanging messages over a deployed broker on kubernetes using an ingress"
draft: false
images: []
menu:
  docs:
    parent: "send-receive"
weight: 110
toc: true
---

### Prerequisite

Before you start, you need to have access to a running Kubernetes cluster
environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) instance
running on your laptop will do fine.

#### Start minikube with a parametrized dns domain name

```{"stage":"init", "id":"minikube_start"}
minikube start --profile tutorialtester
minikube profile tutorialtester
```
```shell markdown_runner
* [tutorialtester] minikube v1.36.0 on Fedora 41
  - MINIKUBE_ROOTLESS=true
* Automatically selected the kvm2 driver. Other choices: qemu2, podman, ssh
* Starting "tutorialtester" primary control-plane node in "tutorialtester" cluster
* Creating kvm2 VM (CPUs=2, Memory=6000MB, Disk=20000MB) ...
* Preparing Kubernetes v1.33.1 on Docker 28.0.4 ...
  - Generating certificates and keys ...
  - Booting up control plane ...
  - Configuring RBAC rules ...
* Configuring bridge CNI (Container Networking Interface) ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: storage-provisioner, default-storageclass
* Done! kubectl is now configured to use "tutorialtester" cluster and "default" namespace by default
! Image was not built for the current minikube version. To resolve this you can delete and recreate your minikube cluster using the latest images. Expected minikube version: v1.35.0 -> Actual minikube version: v1.36.0
* minikube profile was successfully set to tutorialtester
```

#### Enable nginx and ssl passthrough for minikube

```{"stage":"init"}
minikube addons enable ingress
minikube kubectl -- patch deployment -n ingress-nginx ingress-nginx-controller --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value":"--enable-ssl-passthrough"}]'
```
```shell markdown_runner
* ingress is an addon maintained by Kubernetes. For any concerns contact minikube on GitHub.
You can view the list of minikube maintainers at: https://github.com/kubernetes/minikube/blob/master/OWNERS
  - Using image registry.k8s.io/ingress-nginx/controller:v1.12.2
  - Using image registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.5.3
  - Using image registry.k8s.io/ingress-nginx/kube-webhook-certgen:v1.5.3
* Verifying ingress addon...
* The 'ingress' addon is enabled
deployment.apps/ingress-nginx-controller patched
```

#### Get minikube's ip

```{"stage":"init", "runtime":"bash", "label":"get the cluster ip"}
export CLUSTER_IP=$(minikube ip --profile tutorialtester)
```

#### Make sure the domain of your cluster is resolvable

If you are running your OpenShift cluster locally, you might not be able to
resolve the urls to IPs out of the blue. Follow [this guide](../help/hostname_resolution.md) to configure your setup.

This tutorial will follow the simple /etc/hosts approach, but feel free to use
the most appropriate one for you.

### Deploy the operator

#### create the namespace

```{"stage":"init"}
kubectl create namespace send-receive-project
kubectl config set-context --current --namespace=send-receive-project
```
```shell markdown_runner
namespace/send-receive-project created
Context "tutorialtester" modified.
```

Go to the root of the operator repo and install it:

```{"stage":"init", "rootdir":"$initial_dir"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
Client Version: 4.18.5
Kustomize Version: v5.4.2
Kubernetes Version: v1.33.1
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
```

Wait for the Operator to start (status: `running`).

```{"stage":"init", "runtime":"bash", "label":"wait for the operator to be running"}
kubectl wait pod --all --for=condition=Ready --namespace=send-receive-project --timeout=600s
```
```shell markdown_runner
pod/arkmq-org-broker-controller-manager-fdd64476f-km8ml condition met
```

### Deploy the Apache Artemis Broker

For this tutorial we need to:

* have a broker that is able to listen to any network interface. For that we
  setup an `acceptor` that will be listening on every interfaces on port
  `62626`.
* have the ssl protocol configured for the `acceptor`
* have queues to exchange messages on. These are configured by the broker
  properties. Two queues are setup, one called `APP.JOBS` that is of type
  `ANYCAST` and one called `APP.COMMANDS` that is of type `MULTICAST`.

#### Create the certs

We'll take some inspiration from the [ssl broker
setup](https://github.com/arkmq-org/send-receive-project/blob/main/docs/tutorials/ssl_broker_setup.md)
to configure the certificates.

> [!NOTE]
> In this tutorial:
> * The password used for the certificates is `000000`.
> * The secret name is `send-receive-sslacceptor-secret` composed from the broker
>   name `send-receive` and the acceptor name `sselacceptor`

```{"stage":"etc", "runtime":"bash", "label":"get the cluster ip"}
export CLUSTER_IP=$(minikube ip --profile tutorialtester)
```

```{"stage":"cert-creation", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"generate cert"}
printf "000000\n000000\n${CLUSTER_IP}.nip.io\narkmq-org\nRed Hat\nGrenoble\nAuvergne Rhône Alpes\nFR\nyes\n" | keytool -genkeypair -alias artemis -keyalg RSA -keysize 2048 -storetype PKCS12 -keystore broker.ks -validity 3000
printf '000000\n' | keytool -export -alias artemis -file broker.cert -keystore broker.ks
printf '000000\n000000\nyes\n' | keytool -import -v -trustcacerts -alias artemis -file broker.cert -keystore client.ts
```
```shell markdown_runner
Owner: CN=192.168.50.5.nip.io, OU=arkmq-org, O=Red Hat, L=Grenoble, ST=Auvergne Rhône Alpes, C=FR
Issuer: CN=192.168.50.5.nip.io, OU=arkmq-org, O=Red Hat, L=Grenoble, ST=Auvergne Rhône Alpes, C=FR
Serial number: 46deb0fb8d74485d
Valid from: Thu Nov 13 10:40:29 CET 2025 until: Mon Jan 30 10:40:29 CET 2034
Certificate fingerprints:
	 SHA1: 75:9B:29:01:C4:2E:D9:86:9D:7B:4B:C7:D2:65:E4:98:75:BF:8D:58
	 SHA256: 76:10:2C:99:5E:A2:C3:AE:1C:2B:13:92:40:BE:25:77:1E:F7:0A:23:A4:D6:DF:D0:42:E4:E2:FA:E5:5A:C9:DB
Signature algorithm name: SHA384withRSA
Subject Public Key Algorithm: 2048-bit RSA key
Version: 3

Extensions: 

#1: ObjectId: 2.5.29.14 Criticality=false
SubjectKeyIdentifier [
KeyIdentifier [
0000: 9A D1 BB 09 AA 84 1C D4   13 6D 5B B4 98 93 0B 5D  .........m[....]
0010: DB 8E CA 36                                        ...6
]
]

Enter keystore password:  Re-enter new password: Enter the distinguished name. Provide a single dot (.) to leave a sub-component empty or press ENTER to use the default value in braces.
What is your first and last name?
  [Unknown]:  What is the name of your organizational unit?
  [Unknown]:  What is the name of your organization?
  [Unknown]:  What is the name of your City or Locality?
  [Unknown]:  What is the name of your State or Province?
  [Unknown]:  What is the two-letter country code for this unit?
  [Unknown]:  Is CN=192.168.50.5.nip.io, OU=arkmq-org, O=Red Hat, L=Grenoble, ST=Auvergne Rhône Alpes, C=FR correct?
  [no]:  
Generating 2,048 bit RSA key pair and self-signed certificate (SHA384withRSA) with a validity of 3,000 days
	for: CN=192.168.50.5.nip.io, OU=arkmq-org, O=Red Hat, L=Grenoble, ST=Auvergne Rhône Alpes, C=FR
Enter keystore password:  Certificate stored in file <broker.cert>
Enter keystore password:  Re-enter new password: Trust this certificate? [no]:  Certificate was added to keystore
[Storing client.ts]
```

Create the secret in kubernetes

```{"stage":"cert-creation", "rootdir":"$tmpdir.1"}
kubectl create secret generic send-receive-sslacceptor-secret --from-file=broker.ks --from-file=client.ts --from-literal=keyStorePassword='000000' --from-literal=trustStorePassword='000000' -n send-receive-project
```
```shell markdown_runner
secret/send-receive-sslacceptor-secret created
```

Get the path of the cert folder for later

```{"stage":"cert-creation", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"get cert folder"}
export CERT_FOLDER=$(pwd)
```

#### Start the broker


```{"stage":"deploy", "runtime":"bash", "label":"deploy the broker"}
kubectl apply -f - << EOF
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: send-receive
  namespace: send-receive-project
spec:
  ingressDomain: ${CLUSTER_IP}.nip.io
  acceptors:
    - name: sslacceptor
      port: 62626
      expose: true
      sslEnabled: true
      sslSecret: send-receive-sslacceptor-secret
  brokerProperties:
    - addressConfigurations."APP.JOBS".routingTypes=ANYCAST
    - addressConfigurations."APP.JOBS".queueConfigs."APP.JOBS".routingType=ANYCAST
    - addressConfigurations."APP.COMMANDS".routingTypes=MULTICAST
EOF
```
```shell markdown_runner
broker.broker.arkmq.org/send-receive created
```

Wait for the Broker to be ready:

```{"stage":"deploy"}
kubectl wait Broker send-receive --for=condition=Ready --namespace=send-receive-project --timeout=240s
```
```shell markdown_runner
broker.broker.arkmq.org/send-receive condition met
```

#### Create a route to access the ingress:

Check for the ingress availability:

```{"stage":"deploy"}
kubectl get ingress --show-labels
```
```shell markdown_runner
NAME                                 CLASS   HOSTS                                                                         ADDRESS        PORTS     AGE   LABELS
send-receive-sslacceptor-0-svc-ing   nginx   send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io   192.168.50.5   80, 443   40s   ActiveMQArtemis=send-receive,application=send-receive-app,statefulset.kubernetes.io/pod-name=send-receive-ss-0
```

### Exchanging messages between a producer and a consumer

#### Get the actual broker version

```{"stage":"test_setup", "runtime":"bash", "label":"get latest broker version"}
export BROKER_VERSION=$(kubectl get Broker send-receive --namespace=send-receive-project -o json | jq .status.version.brokerVersion -r)
echo broker version: $BROKER_VERSION
```
```shell markdown_runner
broker version: 2.42.0
```

#### Download the broker to access the producer/consumer client command line tool

```{"stage":"test_setup", "rootdir":"$tmpdir.1", "runtime":"bash", "label":"download artemis"}
wget --quiet https://repo1.maven.org/maven2/org/apache/artemis/apache-artemis/${BROKER_VERSION}/apache-artemis-${BROKER_VERSION}-bin.tar.gz
tar -zxf apache-artemis-${BROKER_VERSION}-bin.tar.gz apache-artemis-${BROKER_VERSION}/
# make the rest of commands version agnostic
mv apache-artemis-${BROKER_VERSION}/ apache-artemis/
```

#### Figure out the broker endpoint

The `artemis` client will need to point to the https endpoint generated in earlier with
a couple of parameters set:
* `sslEnabled` = `true`
* `verifyHost` = `false`
* `trustStorePath` = `/some/path/broker.ks`
* `trustStorePassword` = `000000`

To use the consumer and the producer you'll need to give the path to the
`broker.ks` file you've created earlier. In the following commands the file is
located to `${CERT_FOLDER}/broker.ks`.


```{"stage":"test_setup", "runtime":"bash", "label":"get the ingress host"}
export INGRESS_URL=$(kubectl get ingress send-receive-sslacceptor-0-svc-ing -o json | jq -r '.spec.rules[] | .host')
```

Craft the broker url for artemis

```{"stage":"test_setup", "runtime":"bash", "label":"compute the broker url"}
export BROKER_URL="tcp://${INGRESS_URL}:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=${CERT_FOLDER}/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false"
```

##### Test the connection

```{"stage":"test0", "rootdir":"$tmpdir.1/apache-artemis/bin", "runtime":"bash", "label":"test connection"}
./artemis check queue --name TEST --produce 10 --browse 10 --consume 10 --url ${BROKER_URL} --verbose --timeout 120000
```
```shell markdown_runner
Executing org.apache.activemq.artemis.cli.commands.check.QueueCheck check queue --name TEST --produce 10 --browse 10 --consume 10 --url tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2821826002/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false --verbose 
Home::/tmp/2821826002/apache-artemis, Instance::null
Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2821826002/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false
Running QueueCheck
Checking that a producer can send 10 messages to the queue TEST ... success
Checking that a consumer can browse 10 messages from the queue TEST ... success
Checking that a consumer can consume 10 messages from the queue TEST ... success
Checks run: 3, Failures: 0, Errors: 0, Skipped: 0, Time elapsed: 0.262 sec - QueueCheck
NOTE: Picked up JDK_JAVA_OPTIONS: -Djavax.net.ssl.trustStore=/etc/pki/java/cacerts
```

#### ANYCAST

For this use case, run first the producer, then the consumer.

```{"stage":"test1", "rootdir":"$tmpdir.1/apache-artemis/bin/", "runtime":"bash", "label":"anycast: produce 1000 messages"}
./artemis producer --destination queue://APP.JOBS --url ${BROKER_URL}
```
```shell markdown_runner
Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2821826002/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false
Producer ActiveMQQueue[APP.JOBS], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[APP.JOBS], thread=0 Produced: 1000 messages
Producer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in second : 3 s
Producer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in milli second : 3561 milli seconds
NOTE: Picked up JDK_JAVA_OPTIONS: -Djavax.net.ssl.trustStore=/etc/pki/java/cacerts
```

```{"stage":"test1", "rootdir":"$tmpdir.1/apache-artemis/bin/", "runtime":"bash", "label":"anycast: consume 1000 messages"}
./artemis consumer --destination queue://APP.JOBS --url ${BROKER_URL}
```
```shell markdown_runner
Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2821826002/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false
Consumer:: filter = null
Consumer ActiveMQQueue[APP.JOBS], thread=0 wait 3000ms until 1000 messages are consumed
Received 1000
Consumer ActiveMQQueue[APP.JOBS], thread=0 Consumed: 1000 messages
Consumer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in second : 0 s
Consumer ActiveMQQueue[APP.JOBS], thread=0 Elapsed time in milli second : 94 milli seconds
Consumer ActiveMQQueue[APP.JOBS], thread=0 Consumed: 1000 messages
Consumer ActiveMQQueue[APP.JOBS], thread=0 Consumer thread finished
NOTE: Picked up JDK_JAVA_OPTIONS: -Djavax.net.ssl.trustStore=/etc/pki/java/cacerts
```

#### MULTICAST

For this use case, run first the consumer(s), then the producer.
[More details there](https://artemis.apache.org/components/artemis/documentation/latest/address-model.html).

1. in `n` other terminal(s) connect `n` consumer(s):

```{"stage":"test2", "rootdir":"$tmpdir.1/apache-artemis/bin/", "parallel": true, "runtime":"bash", "label":"multicast: consume 1000 messages"}
./artemis consumer --destination topic://APP.COMMANDS --url ${BROKER_URL}
```
```shell markdown_runner
Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2821826002/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false
Consumer:: filter = null
Consumer ActiveMQTopic[APP.COMMANDS], thread=0 wait 3000ms until 1000 messages are consumed
Received 1000
Consumer ActiveMQTopic[APP.COMMANDS], thread=0 Consumed: 1000 messages
Consumer ActiveMQTopic[APP.COMMANDS], thread=0 Elapsed time in second : 5 s
Consumer ActiveMQTopic[APP.COMMANDS], thread=0 Elapsed time in milli second : 5597 milli seconds
Consumer ActiveMQTopic[APP.COMMANDS], thread=0 Consumed: 1000 messages
Consumer ActiveMQTopic[APP.COMMANDS], thread=0 Consumer thread finished
NOTE: Picked up JDK_JAVA_OPTIONS: -Djavax.net.ssl.trustStore=/etc/pki/java/cacerts
```

2. connect the producer to start broadcasting messages.

```{"stage":"test2", "rootdir":"$tmpdir.1/apache-artemis/bin/", "parallel": true, "runtime":"bash", "label":"multicast: produce 1000 messages"}
sleep 5s
./artemis producer --destination topic://APP.COMMANDS --url ${BROKER_URL}
```
```shell markdown_runner
Connection brokerURL = tcp://send-receive-sslacceptor-0-svc-ing-send-receive-project.192.168.50.5.nip.io:443?forceSSLParameters=true&sslEnabled=true&verifyHost=false&trustStorePath=/tmp/2821826002/broker.ks&trustStorePassword=000000&useTopologyForLoadBalancing=false
Producer ActiveMQTopic[APP.COMMANDS], thread=0 Started to calculate elapsed time ...

Producer ActiveMQTopic[APP.COMMANDS], thread=0 Produced: 1000 messages
Producer ActiveMQTopic[APP.COMMANDS], thread=0 Elapsed time in second : 0 s
Producer ActiveMQTopic[APP.COMMANDS], thread=0 Elapsed time in milli second : 625 milli seconds
NOTE: Picked up JDK_JAVA_OPTIONS: -Djavax.net.ssl.trustStore=/etc/pki/java/cacerts
```

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
