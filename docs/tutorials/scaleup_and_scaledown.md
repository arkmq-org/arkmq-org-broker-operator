---
title: "Scaling Up and Down Brokers with arkmq-org Operator"  
description: "How to use operator to scale up and down broker pods"
draft: false
images: []
menu:
  docs:
    parent: "tutorials"
weight: 110
toc: true
---

With arkmq-org operator one can easily manage the broker clusters.
Either scaling up number of nodes(pods) when workload is high, or scaling down when some is not needed -- without messages being lost or stuck.

### Prerequisite
Before you start you need have access to a running Kubernetes cluster environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) running on your laptop will just do fine. The arkmq-org operator also runs in a Openshift cluster environment like [CodeReady Container](https://developers.redhat.com/products/openshift-local/overview). In this blog we assume you have Kubernetes cluster environment. (If you use CodeReady the client tool is **oc** in place of **kubectl**)

### Step 1 - Deploy arkmq-org Operator
In this tutorial we are using the [arkmq-org operator repo](https://github.com/arkmq-org/arkmq-org-broker-operator). In case you haven't done so, clone it to your local disk:

```shell
git clone https://github.com/arkmq-org/arkmq-org-broker-operator.git
cd arkmq-org-broker-operator
```
### Start Minikube and Deploy the Operator

Start a local Minikube cluster:

```{"stage":"init","id":"start_minikube"}
minikube start --profile tutorialtester
minikube profile tutorialtester
kubectl config use-context tutorialtester
minikube addons enable metrics-server --profile tutorialtester
```
```shell markdown_runner
* [tutorialtester] minikube v1.37.0 on Fedora 43
* Automatically selected the docker driver. Other choices: kvm2, qemu2, ssh
* Using Docker driver with root privileges
* Starting "tutorialtester" primary control-plane node in "tutorialtester" cluster
* Pulling base image v0.0.48 ...
* Configuring bridge CNI (Container Networking Interface) ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: storage-provisioner, default-storageclass
* Done! kubectl is now configured to use "tutorialtester" cluster and "default" namespace by default
* minikube profile was successfully set to tutorialtester
Switched to context "tutorialtester".
* metrics-server is an addon maintained by Kubernetes. For any concerns contact minikube on GitHub.
You can view the list of minikube maintainers at: https://github.com/kubernetes/minikube/blob/master/OWNERS
  - Using image registry.k8s.io/metrics-server/metrics-server:v0.8.0
* The 'metrics-server' addon is enabled
```

Create the namespace for the tutorial:

```{"stage":"init","id":"create_namespace","runtime":"bash"}
kubectl get ns myproject >/dev/null 2>&1 || kubectl create namespace myproject
kubectl config set-context --current --namespace=myproject
```
```shell markdown_runner
namespace/myproject created
Context "tutorialtester" modified.
```

Deploy the operator to the `myproject` namespace:

```{"stage":"init","id":"deploy_operator","rootdir":"$initial_dir"}
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

Wait for the operator to be ready:

```{"stage":"init","id":"wait_operator"}
kubectl wait deployment arkmq-org-broker-controller-manager --namespace=myproject --for=condition=Available --timeout=300s
```
```shell markdown_runner
deployment.apps/arkmq-org-broker-controller-manager condition met
```

Verify the operator is running:

```{"stage":"init","id":"check_operator"}
kubectl get pod -n myproject
```
```shell markdown_runner
No resources found in myproject namespace.
```

### Step 2 - Deploy Apache Artemis broker
In this step we'll setup a one-node broker in kubernetes. First we need create a broker custom resource file.

Create it using kubectl:

```{"stage":"deploy","id":"create_broker","runtime":"bash"}
kubectl apply -f - -n myproject <<EOF
apiVersion: broker.arkmq.org/v1beta2
kind: BrokerCluster
metadata:
  name: ex-aao
spec:
  deploymentPlan:
    size: 1
    clustered: true
    persistenceEnabled: true
    messageMigration: true
  brokerProperties:
    - "HAPolicyConfiguration=PRIMARY_ONLY"
    - "HAPolicyConfiguration.scaleDownConfiguration.discoveryGroup=my-discovery-group"
    - "HAPolicyConfiguration.scaleDownConfiguration.enabled=false"
EOF
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao created
```

The custom resource tells the operator to deploy one broker pod with **persistenceEnabled: true** and **messageMigration: true**.

**persistenceEnabled: true** means the broker persists messages to persistent storage.

**messageMigration: true** means if a broker pod is shut down due to scaledown of its StatefulSet, its messages will be migrated to another live broker pod so that those messages will be processed.

Wait for the broker to be ready:

```{"stage":"deploy","id":"wait_broker"}
kubectl wait BrokerCluster ex-aao --for=condition=Ready --namespace=myproject --timeout=300s
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao condition met
```

Check the pods:

```{"stage":"deploy","id":"check_pods"}
kubectl get pod -n myproject
```
```shell markdown_runner
NAME                                                  READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5d969499b-rpxg4   1/1     Running   0          74s
ex-aao-ss-0                                           1/1     Running   0          62s
```

### Step 3 - Scaling up

In this step we will scale the broker pods from one to two 

```{"stage":"scale_up","id":"scale_to_2"}
kubectl patch brokercluster ex-aao -n myproject --type='json' -p='[{"op": "replace", "path": "/spec/deploymentPlan/size", "value": 2}]'
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao patched
```

Wait for the scale up to complete:

```{"stage":"scale_up","id":"wait_scale_up"}
kubectl wait BrokerCluster ex-aao --for=condition=Ready --namespace=myproject --timeout=300s
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao condition met
```

Check the pods:

```{"stage":"scale_up","id":"check_pods_scaled"}
kubectl get pod -n myproject
```
```shell markdown_runner
NAME                                                  READY   STATUS     RESTARTS   AGE
arkmq-org-broker-controller-manager-5d969499b-rpxg4   1/1     Running    0          75s
ex-aao-ss-0                                           1/1     Running    0          63s
ex-aao-ss-1                                           0/1     Init:0/1   0          1s
```

### Step 4 - Send messages

Send 100 messages to broker0 (pod `ex-aao-ss-0`):

```{"stage":"send","id":"produce_100_broker0"}
kubectl exec ex-aao-ss-0 -n myproject -c ex-aao-container -- /bin/bash /home/jboss/amq-broker/bin/artemis producer --user admin --password admin --url tcp://ex-aao-ss-0:61616 --message-count 100
```
```shell markdown_runner
Connection brokerURL = tcp://ex-aao-ss-0:61616
Producer ActiveMQQueue[TEST], thread=0 Started to calculate elapsed time ...

Producer ActiveMQQueue[TEST], thread=0 Produced: 100 messages
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in second : 0 s
Producer ActiveMQQueue[TEST], thread=0 Elapsed time in milli second : 527 milli seconds
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/ex-aao-props/,/amq/extra/secrets/ex-aao-props/broker-${STATEFUL_SET_ORDINAL}/,/amq/extra/secrets/ex-aao-props/?filter=.*\.for_ordinal_${STATEFUL_SET_ORDINAL}_only
```

Check the queue's message count on broker0:

```{"stage":"send","id":"queue_stat_broker0"}
kubectl exec ex-aao-ss-0 -n myproject -c ex-aao-container -- /bin/bash /home/jboss/amq-broker/bin/artemis queue stat --user admin --password admin --url tcp://ex-aao-ss-0:61616
```
```shell markdown_runner
Connection brokerURL = tcp://ex-aao-ss-0:61616
|NAME              |ADDRESS           |CONSUMER|MESSAGE|MESSAGES|DELIVERING|MESSAGES|SCHEDULED|ROUTING|INTERNAL|
|                  |                  | COUNT  | COUNT | ADDED  |  COUNT   | ACKED  |  COUNT  | TYPE  |        |
|$sys.mqtt.sessions|$sys.mqtt.sessions|   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST|  true  |
|DLQ               |DLQ               |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
|ExpiryQueue       |ExpiryQueue       |   0    |   0   |   0    |    0     |   0    |    0    |ANYCAST| false  |
|TEST              |TEST              |   0    |  100  |  100   |    0     |   0    |    0    |ANYCAST| false  |
NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/ex-aao-props/,/amq/extra/secrets/ex-aao-props/broker-${STATEFUL_SET_ORDINAL}/,/amq/extra/secrets/ex-aao-props/?filter=.*\.for_ordinal_${STATEFUL_SET_ORDINAL}_only
```

### Step 5 - Scale down with message draining

The operator not only can scale up brokers in a cluster but also can scale them down. As we set **messageMigration: true** in the [broker cr](#broker_clustered_yaml), the operator will migrate messages when the StatefulSet is scaled down.

When a broker pod is scaled down, the operator waits for it to forward all messages to live brokers before removing it from the cluster.


Now scale down the cluster from two pods to one

```{"stage":"scale_down","id":"scale_to_1"}
kubectl patch brokercluster ex-aao -n myproject --type='json' -p='[{"op": "replace", "path": "/spec/deploymentPlan/size", "value": 1}]'
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao patched
```

Wait for the scale down and message migration to complete:

```{"stage":"scale_down","id":"wait_scale_down"}
kubectl wait BrokerCluster ex-aao --for=condition=Ready --namespace=myproject --timeout=300s
```
```shell markdown_runner
broker.broker.arkmq.org/ex-aao condition met
```

Check the pods:

```{"stage":"scale_down","id":"check_pods_final"}
kubectl get pod -n myproject
```
```shell markdown_runner
NAME                                                  READY   STATUS    RESTARTS   AGE
arkmq-org-broker-controller-manager-5d969499b-rpxg4   1/1     Running   0          80s
ex-aao-ss-0                                           1/1     Running   0          68s
ex-aao-ss-1                                           0/1     Running   0          6s
```

Now check the messages in queue TEST at the pod:

```{"stage":"scale_down","id":"final_queue_stat"}
kubectl exec ex-aao-ss-0 -n myproject -c ex-aao-container -- /bin/bash /home/jboss/amq-broker/bin/artemis queue stat --user admin --password admin --url tcp://ex-aao-ss-0:61616
```
```shell markdown_runner
Connection brokerURL = tcp://ex-aao-ss-0:61616
|NAME                     |ADDRESS                  |CONSUMER|MESSAGE|MESSAGES|DELIVERING|MESSAGES|SCHEDULED| ROUTING |INTERNAL|
|                         |                         | COUNT  | COUNT | ADDED  |  COUNT   | ACKED  |  COUNT  |  TYPE   |        |
|$.artemis.internal.sf.my-|$.artemis.internal.sf.my-|   1    |   0   |   0    |    0     |   0    |    0    |MULTICAST|  true  |
|  cluster.7823d7ce-fd19-1|  cluster.7823d7ce-fd19-1|        |       |        |          |        |         |         |        |
|  1f0-bcf2-527b56920ba3  |  1f0-bcf2-527b56920ba3  |        |       |        |          |        |         |         |        |
|$sys.mqtt.sessions       |$sys.mqtt.sessions       |   0    |   0   |   0    |    0     |   0    |    0    | ANYCAST |  true  |
|DLQ                      |DLQ                      |   0    |   0   |   0    |    0     |   0    |    0    | ANYCAST | false  |
|ExpiryQueue              |ExpiryQueue              |   0    |   0   |   0    |    0     |   0    |    0    | ANYCAST | false  |
|TEST                     |TEST                     |   0    |  100  |  100   |    0     |   0    |    0    | ANYCAST | false  |
|notif.79406b69-fd19-11f0-|activemq.notifications   |   1    |   0   |   10   |    0     |   10   |    0    |MULTICAST|  true  |
|  bcf2-527b56920ba3.Activ|                         |        |       |        |          |        |         |         |        |
|  eMQServerImpl_name=amq-|                         |        |       |        |          |        |         |         |        |
|  broker                 |                         |        |       |        |          |        |         |         |        |

Note: Use [31m--clustered[0m to expand the report to other nodes in the topology.

NOTE: Picked up JDK_JAVA_OPTIONS: -Dbroker.properties=/amq/extra/secrets/ex-aao-props/,/amq/extra/secrets/ex-aao-props/broker-${STATEFUL_SET_ORDINAL}/,/amq/extra/secrets/ex-aao-props/?filter=.*\.for_ordinal_${STATEFUL_SET_ORDINAL}_only
```

## Cleanup

To leave a pristine environment after executing this tutorial, delete the minikube cluster.

```{"stage":"teardown", "requires":"init/start_minikube"}
minikube delete --profile tutorialtester
```
```shell markdown_runner
* Deleting "tutorialtester" in docker ...
* Deleting container "tutorialtester" ...
* Removing /home/makella19/.minikube/machines/tutorialtester ...
* Removed all traces of the "tutorialtester" cluster.
```

## More information

* Check out [arkmq-org project repo](https://github.com/arkmq-org)
