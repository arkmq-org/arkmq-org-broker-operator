---
title: "Quick Start"
description: "One page summary of how to start a new arkmq-org project."
lead: "One page summary of how to start a new arkmq-org project."
date: 2020-11-16T13:59:39+01:00
lastmod: 2020-11-16T13:59:39+01:00
draft: false
images: []
menu:
  docs:
    parent: "getting-started"
weight: 110
toc: true
---

## Overview

At the moment these instructions have been tested against Kubernetes 1.25 and above,
other kubernetes or OpenShift environments may require minor adjustment.

One important note about operators in general is that to get the operator
installed requires cluster-admin level privileges. Once installed, a regular
user should be able to deploy the Apache Artemis Broker via the provided custom
resource.

## General environment requirements

Currently the operator is tested against kubernetes v1.25 and above.
You can install a [Minikube](https://minikube.sigs.k8s.io/docs/) or a [CodeReady Containers(CRC)](https://developers.redhat.com/products/openshift-local/overview) to deploy the operator.

## Install the operator
There are multiple methods of installing the operator: [from releases](#install-the-operator-from-releases), [from helm charts](#install-the-operator-from-helm-charts), [from OperatorHub.io](#install-the-operator-from-operatorhubio) and [from sources](#install-the-operator-from-sources).

### Install the operator from releases
In order to install the operator from the latest release is by running the following command:
```shell
kubectl apply -f https://github.com/arkmq-org/arkmq-org-broker-operator/releases/latest/download/arkmq-org-broker-operator.yaml
```
You can also install a specific operator version, i.e. to install the version `2.0.5`
```shell
kubectl apply -f https://github.com/arkmq-org/arkmq-org-broker-operator/releases/download/v2.0.5/arkmq-org-broker-operator.yaml
```

### Install the operator from helm charts
In order to install the operator from helm charts, first be sure that you have [Helm](https://helm.sh/) v3.8.0 or a more recent version installed then run the following command:
```shell
helm install my-arkmq-org-broker-operator oci://quay.io/arkmq-org/helm-charts/arkmq-org-broker-operator
```
You can also install a specific operator version, i.e. to install the version `2.1.0`
```shell
helm install my-arkmq-org-broker-operator oci://quay.io/arkmq-org/helm-charts/arkmq-org-broker-operator --version 2.1.0
```
Use the `--set` option to customize the default [values](https://github.com/arkmq-org/arkmq-org-broker-operator/blob/main/helm-charts/arkmq-org-broker-operator/values.yaml),
i.e. to install the operator without the CRDs:
```shell
helm install my-arkmq-org-broker-operator oci://quay.io/arkmq-org/helm-charts/arkmq-org-broker-operator --set crds.apply=false
```

### Install the operator from OperatorHub.io
In order to install the operator from OperatorHub.io, first be sure that you have a Kubernetes cluster with [OLM](https://olm.operatorframework.io/) v0.32.0 or later (on OpenShift, version 4.20 or later). The operator bundle includes a NetworkPolicy manifest that requires OLM NetworkPolicy support. Then run the following command:
```shell
kubectl create -f https://operatorhub.io/install/arkmq-org-broker-operator.yaml
```
For further details see https://operatorhub.io/operator/arkmq-org-broker-operator

### Install the operator from sources
In order to install the operator from sources, first clone the git repository with the following command:
```shell
git clone https://github.com/arkmq-org/arkmq-org-broker-operator.git
```

Create the namespace arkmq-org-broker-operator and save it for all subsequent kubectl commands
```shell
$ kubectl create namespace arkmq-org-broker-operator
$ kubectl config set-context --current --namespace arkmq-org-broker-operator
```

To deploy the operator in the current namespace arkmq-org-broker-operator simply run:

```shell
$ ./deploy/install_opr.sh
```
or if you have built your own image, change the image defined in deploy/operator.yaml before deploy the operator.

The operator will be deployed into the current namespace and watches the same namespace.

To watch specific namespaces or all namespaces, run the command below and it will ask which namespaces you want to watch.

```shell
$ ./deploy/cluster_wide_install_opr.sh
```

At this point you should see the arkmq-org-broker-operator starting up and if you check the
pods you should see something like

```$shell
$ kubectl get pod -n arkmq-org-broker-operator
NAME                                                   READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5ff459cd95-kn22m   1/1     Running   0          70m
```

## Deploying the broker

Now that the operator is running and listening for changes related to our crd we can deploy our [broker single example](../../examples/broker/broker_single.yaml)
which looks like

```$yaml
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: my-broker
```

Note in particular the **spec.image** which identifies the container image to use to launch the AMQ Broker. If it's empty, 'placeholder' or not defined it will get the latest default image url from deploy/operator.yaml where a list of supported broker image are defined as environment variables.

To deploy the broker simply execute

```$shell
$ kubectl create -f examples/broker/broker_single.yaml -n arkmq-org-broker-operator
broker.broker.arkmq.org/my-broker created
```
In a moment you should see one broker pod is created alongside the operator pod:

```$shell
$ kubectl get pod -n arkmq-org-broker-operator
NAME                                                   READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5ff459cd95-kn22m   1/1     Running   0          128m
my-broker-ss-0                                         1/1     Running   0          23m
```

## Scaling

The spec.deploymentPlan.size controls how many broker pods you want to deploy to the cluster. You can change this value and apply it to a running deployment to scale up and scale down the broker pods.

For example if you want to scale up the above deployment to 2 pods, modify the size to 2:

examples/broker/broker_single.yaml
```$yaml
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: my-broker
spec:
  deploymentPlan:
    size: 2
```
and apply it:

```$shell
$ kubectl apply -f examples/broker/broker_single.yaml -n arkmq-org-broker-operator
broker.broker.arkmq.org/my-broker configured
```

and you will get 2 broker pods in the cluster

```$shell
$ kubectl get pod -n arkmq-org-broker-operator
NAME                                                   READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5ff459cd95-kn22m   1/1     Running   0          140m
my-broker-ss-0                                         1/1     Running   0          35m
my-broker-ss-1                                         1/1     Running   0          69s
```

You can scale down the deployment in similar manner by reducing the size and apply it again.

### Clustering

By default if broker pods are scaled to more than one then the broker pods form a broker
[cluster](https://artemis.apache.org/components/artemis/documentation/latest/clusters.html), meaning connect to each other and redistribute messages using default 'ON_DEMAND' policy. 

## Undeploying the broker

To undeploy the broker we simply execute

```$shell
$ kubectl delete -f examples/broker/broker_single.yaml -n arkmq-org-broker-operator
broker.broker.arkmq.org "my-broker" deleted
```

## Managing Queues

### Overview

Users can use the activemqartemisaddress CRD to create and remove queues/address on a running broker pod.

Having [a deployed broker pod](#deploying-the-broker) is necessary to apply the commands in this section.

Assuming you have one already running, you can then deploy an activemqartemisaddress resource from the [examples dir](../../examples/address/address_queue.yaml):

address-queue.yaml:
```$yaml
apiVersion: broker.amq.io/v1beta1
kind: ActiveMQArtemisAddress
metadata:
  name: artemis-address-queue
spec:
  addressName: myAddress0
  queueName: myQueue0
  routingType: anycast
  removeFromBrokerOnDelete: true
```

and the deploy command:

```$shell
$ kubectl create -f examples/address/address_queue.yaml -n arkmq-org-broker-operator
activemqartemisaddress.broker.amq.io/artemis-address-queue created
```

When it is deployed it will create a queue named **myQueue0** on an address **myAddress0** with **anycast** routing type, like:

```$shell
$ kubectl exec artemis-broker-ss-0 --container artemis-broker-container -- amq-broker/bin/artemis queue stat
Connection brokerURL = tcp://artemis-broker-ss-0.artemis-broker-hdls-svc.arkmq-org-broker-operator.svc.cluster.local:61616
|NAME                     |ADDRESS                  |CONSUMER_COUNT|MESSAGE_COUNT|MESSAGES_ADDED|DELIVERING_COUNT|MESSAGES_ACKED|SCHEDULED_COUNT|ROUTING_TYPE|
|DLQ                      |DLQ                      |0             |0            |0             |0               |0             |0              |ANYCAST     |
|ExpiryQueue              |ExpiryQueue              |0             |0            |0             |0               |0             |0              |ANYCAST     |
|activemq.management.27...|activemq.management.27...|1             |0            |0             |0               |0             |0              |MULTICAST   |
|myQueue0                 |myAddress0               |0             |0            |0             |0               |0             |0              |ANYCAST     |
```

The **spec.removeFromBrokerOnDelete** controls how to deal with the created queue/address resources when you delete the above custom resource:

```$shell
$ kubectl delete -f examples/address/address_queue.yaml -n arkmq-org-broker-operator
activemqartemisaddress.broker.amq.io "artemis-address-queue" deleted
```

If **spec.removeFromBrokerOnDelete** is true, the queue/address resources will be deleted from broker.
If it is false, the queue/address created by this custome resource will be kept in broker even after the custom resource has been deleted.

You can check if the queue was removed using the command below:
```$shell
$ kubectl exec artemis-broker-ss-0 --container artemis-broker-container -- amq-broker/bin/artemis queue stat
Connection brokerURL = tcp://artemis-broker-ss-0.artemis-broker-hdls-svc.arkmq-org-broker-operator.svc.cluster.local:61616
|NAME                     |ADDRESS                  |CONSUMER_COUNT|MESSAGE_COUNT|MESSAGES_ADDED|DELIVERING_COUNT|MESSAGES_ACKED|SCHEDULED_COUNT|ROUTING_TYPE|
|DLQ                      |DLQ                      |0             |0            |0             |0               |0             |0              |ANYCAST     |
|ExpiryQueue              |ExpiryQueue              |0             |0            |0             |0               |0             |0              |ANYCAST     |
|activemq.management.d9...|activemq.management.d9...|1             |0            |0             |0               |0             |0              |MULTICAST   |
```

## Draining messages on scale down

### Using the broker controller
This has the benefit of keeping a scaling down broker alive and the pod visible till it is empty and of automatically choosing an available broker in the cluster to migrate messages to.

Additional CR spec.brokerProperties are used to indicate that the broker operator should manage scaledown. These properties configure the broker scaledown policy and the use of the cluster discovery group to locate a scaledown target. The enabled flag is false, such that normal rollout of changes or termination does not trigger scaledown. The operator flips HAPolicyConfiguration.scaleDownConfiguration.enabled to true, via a brokerProperties update for each broker, to trigger actual scaledown.

NOTE: the key=value: `HAPolicyConfiguration.scaleDownConfiguration.enabled=false` is mandatory, it is the trigger for the broker operator to manage scaledown.


An operand with version >= 2.43.0 is necessary when jgroups discovery (the default) is used.


```$yaml
apiVersion: broker.arkmq.org/v1beta1
kind: Broker
metadata:
  name: artemis-broker
spec:
  deploymentPlan:
    size: 2
    persistenceEnabled: true
    messageMigration: true
  brokerProperties:
   - "HAPolicyConfiguration=PRIMARY_ONLY"
   - "HAPolicyConfiguration.scaleDownConfiguration.discoveryGroup=my-discovery-group"
   - "HAPolicyConfiguration.scaleDownConfiguration.enabled=false"
```

When the CR spec.brokerProperties contain the key=value: `HAPolicyConfiguration.scaleDownConfiguration.enabled=false`, the broker controller manages scaling down of the cluster. The spec.deploymentPlan.size is decremented by one as each of the requred brokers are removed from the stateful set, till the desired size is reached.

NOTE: the scaling down broker remains in the statefull set until it has no pending messages. The operator uses the cr.Status Conditions to indicate that scaledown is pending, which allows a user to track the progress of scaledown. In the event of a failure to fully migrate messages before shutdown, the pod is restarted and the procedure is repeated until the broker is empty. Only when the broker is empty will it be removed from the stataful set and it's PV will be available for reclaim.

### Using the scaledown controller (deprecated)

When a broker pod is being scaled down, a scaledown controller can be deployed aumatically to handle message migration from the scaled down broker pod to an active broker.

When the scale down controller detects the event it starts a drainer pod.
The drainer pod will contact one of the live pods in the cluster and drain the messages over to it.
After the draining is complete it shuts down itself.

The message draining only works when you enabled persistence and messageMigration on broker custome resource.
For example, you can deploy a cluster from our [broker cluster persistence example](../../examples/broker/broker_cluster_persistence.yaml)

```$yaml
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: my-broker
spec:
  deploymentPlan:
    size: 2
    persistenceEnabled: true
    messageMigration: true
```

To demonstrate the message draining first deploy the above custom resource (assuming the operator is running):

```$shell
$ kubectl create -f ./examples/broker/broker_cluster_persistence.yaml -n arkmq-org-broker-operator
broker.broker.arkmq.org/my-broker created
```

You shall see 2 broker pods are created.

```$shell
$ kubectl get pod -n arkmq-org-broker-operator
NAME                                                   READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5ff459cd95-kn22m   1/1     Running   0          3h19m
my-broker-ss-0                                         1/1     Running   0          89s
my-broker-ss-1                                         1/1     Running   0          53s
```

Now we'll use broker's cli tool to send some messages to each broker pod. 

First send 100 messages to broker **my-broker-ss-0**:

```$shell
$ kubectl exec my-broker-ss-0 -- amq-broker/bin/artemis producer --url tcp://my-broker-ss-0:61616 --message-count=100
Defaulted container "my-broker-container" out of: my-broker-container, my-broker-container-init (init)
Connection brokerURL = tcp://my-broker-ss-0:61616
Producer ActiveMQQueue[TEST], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[TEST], thread=0 Produced: 100 messages
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in second : 0 s
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in milli second : 409 milli seconds
```

then send another 100 messages to broker **my-broker-ss-1**

```$shell
$ kubectl exec my-broker-ss-1 -- amq-broker/bin/artemis producer --user x --password y --url tcp://my-broker-ss-1:61616 --message-count=100
Defaulted container "my-broker-container" out of: my-broker-container, my-broker-container-init (init)
Connection brokerURL = tcp://my-broker-ss-1:61616
Producer ActiveMQQueue[TEST], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[TEST], thread=0 Produced: 100 messages
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in second : 0 s
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in milli second : 466 milli seconds
```

Now each of the 2 brokers has 100 messages. 

```$shell
$ kubectl exec my-broker-ss-0 --container my-broker-container -- amq-broker/bin/artemis queue stat
Connection brokerURL = tcp://my-broker-ss-0.my-broker-hdls-svc.arkmq-org-broker-operator.svc.cluster.local:61616
|NAME                     |ADDRESS                  |CONSUMER_COUNT|MESSAGE_COUNT|MESSAGES_ADDED|DELIVERING_COUNT|MESSAGES_ACKED|SCHEDULED_COUNT|ROUTING_TYPE|
|DLQ                      |DLQ                      |0             |0            |0             |0               |0             |0              |ANYCAST     |
|ExpiryQueue              |ExpiryQueue              |0             |0            |0             |0               |0             |0              |ANYCAST     |
|TEST                     |TEST                     |0             |100          |100           |0               |0             |0              |ANYCAST     |
|activemq.management.14...|activemq.management.14...|1             |0            |0             |0               |0             |0              |MULTICAST   |
```

```$shell
$ kubectl exec my-broker-ss-1 --container my-broker-container -- amq-broker/bin/artemis queue stat
Connection brokerURL = tcp://my-broker-ss-1.my-broker-hdls-svc.arkmq-org-broker-operator.svc.cluster.local:61616
|NAME                     |ADDRESS                  |CONSUMER_COUNT|MESSAGE_COUNT|MESSAGES_ADDED|DELIVERING_COUNT|MESSAGES_ACKED|SCHEDULED_COUNT|ROUTING_TYPE|
|DLQ                      |DLQ                      |0             |0            |0             |0               |0             |0              |ANYCAST     |
|ExpiryQueue              |ExpiryQueue              |0             |0            |0             |0               |0             |0              |ANYCAST     |
|TEST                     |TEST                     |0             |100          |100           |0               |0             |0              |ANYCAST     |
|activemq.management.2a...|activemq.management.2a...|1             |0            |0             |0               |0             |0              |MULTICAST   |
```

Modify the ./examples/broker/broker_cluster_persistence.yaml to scale down to one broker

```$yaml
apiVersion: broker.arkmq.org/v1beta2
kind: Broker
metadata:
  name: my-broker
spec:
  deploymentPlan:
    size: 1
    persistenceEnabled: true
    messageMigration: true
```
and re-apply it:
```$shell
$ kubectl apply -f ./examples/broker/broker_cluster_persistence.yaml -n arkmq-org-broker-operator
broker.broker.arkmq.org/my-broker configured
```

The broker pods will be reduced to only one

```$shell
$ kubectl get pod -n arkmq-org-broker-operator
NAME                                                   READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5ff459cd95-kn22m   1/1     Running   0          3h57m
my-broker-ss-0                                         1/1     Running   0          39m
```

Now the messages on the broker pod **my-broker-ss-1** should have been all migrated to pod **my-broker-ss-0**.
Use the broker cli tool again to check:

```$shell
$ kubectl exec my-broker-ss-0 -- amq-broker/bin/artemis queue stat --url tcp://my-broker-ss-0:61616
Defaulted container "my-broker-container" out of: my-broker-container, my-broker-container-init (init)
Connection brokerURL = tcp://my-broker-ss-0:61616
|NAME                     |ADDRESS                  |CONSUMER_COUNT |MESSAGE_COUNT |MESSAGES_ADDED |DELIVERING_COUNT |MESSAGES_ACKED |SCHEDULED_COUNT |ROUTING_TYPE |
|$.artemis.internal.sf.my-cluster.941368e6-79c9-11ec-b4c8-0242ac11000b|$.artemis.internal.sf.my-cluster.941368e6-79c9-11ec-b4c8-0242ac11000b|0              |0             |0              |0                |0              |0               |MULTICAST    |
|DLQ                      |DLQ                      |0              |0             |0              |0                |0              |0               |ANYCAST      |
|ExpiryQueue              |ExpiryQueue              |0              |0             |0              |0                |0              |0               |ANYCAST      |
|TEST                     |TEST                     |0              |200           |200            |0                |0              |0               |ANYCAST      |
|activemq.management.91b87b03-0c70-4630-beb8-6a7919a4923a|activemq.management.91b87b03-0c70-4630-beb8-6a7919a4923a|1              |0             |0              |0                |0              |0               |MULTICAST    |
```
You can see the queue TEST has 200 messages now.

## Using a operator extraMounts

The Broker custom resource allows you to define extraMounts which will mount secrets and/or configmaps with
configuration information as files to be used in the artemis configuration. One usage of extraMounts is to 
redefine the log4j file used by artemis to log information. [Here](https://artemis.apache.org/components/artemis/documentation/latest/logging.html#logging) you can find details about artemis logging configuration.

To use a custom logging you will need a log4j configuration file. The default log4j configuration file can be used as
an initial example and can be downloaded from [here](https://raw.githubusercontent.com/arkmq-org/arkmq-org-broker-kubernetes-image/main/modules/arkmq-org-broker-launch/added/log4j2.properties)

Assuming you already have the operator deployed, in our example we are going to modify the default logging file and enable the audit logging. You will need to modify the log4j2.properties file and change the lines as below:

```shell
$ sed -i 's/logger.audit_base.level = OFF/logger.audit_base.level = INFO/' log4j2.properties
$ sed -i 's/logger.audit_resource.level = OFF/logger.audit_resource.level = INFO/' log4j2.properties
$ sed -i 's/logger.audit_message.level = OFF/logger.audit_message.level = INFO/' log4j2.properties
```

Then you will need to create configMap or a secret in your namespace. The configMap or secret name must have a suffix of `-logging-config` and the key must be `logging.properties`

```$shell
$ kubectl create secret generic newlog4j-logging-config --from-file=logging.properties=log4j2.properties -n arkmq-org-broker-operator
secret/newlog4j-logging-config created
```

After the secret creation it should be like this:
```$shell
$ kubectl describe secret newlog4j-logging-config -n arkmq-org-broker-operator
Name:         newlog4j-logging-config
Namespace:    arkmq-org-broker-operator
Labels:       <none>
Annotations:  <none>

Type:  Opaque

Data
====
logging.properties:  2687 bytes
```

The next step is to define the extraMount in the Broker custom resource like in our [example](../../examples/broker/broker_custom_logging_secret.yaml) and deploy it.

```$shell
$ kubectl create -f ./examples/broker/broker_custom_logging_secret.yaml -n arkmq-org-broker-operator
broker.broker.arkmq.org/my-broker-logging created
```

Then you should be able to see some audit log entries after the artemis start:

```$shell
$ kubectl logs my-broker-logging-ss-0 -n arkmq-org-broker-operator |grep audit
Defaulted container "my-broker-logging-container" out of: my-broker-logging-container, my-broker-logging-container-init (init)
2023-11-08 20:02:46,914 INFO  [org.apache.activemq.audit.base] AMQ601019: User anonymous@internal is getting mbean info on target resource: org.apache.activemq.artemis.core.server.management.impl.HawtioSecurityControlImpl@7a26928a
2023-11-08 20:02:47,115 INFO  [org.apache.activemq.audit.base] AMQ601019: User anonymous@internal is getting mbean info on target resource: org.apache.activemq.artemis.core.management.impl.JGroupsFileBroadcastGroupControlImpl@72725ee1
2023-11-08 20:02:49,164 INFO  [org.apache.activemq.audit.base] AMQ601019: User anonymous@internal is getting mbean info on target resource: org.apache.activemq.artemis.core.management.impl.ClusterConnectionControlImpl@3cb8c8ce
2023-11-08 20:02:49,188 INFO  [org.apache.activemq.audit.base] AMQ601138: User anonymous@internal is getting notification info on target resource: null
2023-11-08 20:02:49,188 INFO  [org.apache.activemq.audit.base] AMQ601019: User anonymous@internal is getting mbean info on target resource: org.apache.activemq.artemis.core.management.impl.ActiveMQServerControlImpl@124ac145
```

You can also do the same using configMaps instead of secrets. Just create a configMap like:

```$shell
$ kubectl create configmap newlog4j-logging-config --from-file=logging.properties=log4j2.properties -n arkmq-org-broker-operator
secret/newlog4j-logging-config created
```

and use the [example](../../examples/broker/broker_custom_logging_configmap.yaml)

## Undeploying the operator

Run this command to undeploy the operator

```$shell
$ ./deploy/undeploy_all.sh
```

