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
```shell markdown_runner
* [service-app-tutorial] minikube v1.37.0 on Fedora 44
  - MINIKUBE_ROOTLESS=true
* Automatically selected the kvm2 driver. Other choices: podman, qemu2, ssh
* Starting "service-app-tutorial" primary control-plane node in "service-app-tutorial" cluster
  - kubelet.sync-frequency=10s
* Configuring bridge CNI (Container Networking Interface) ...
* Verifying Kubernetes components...
  - Using image gcr.io/k8s-minikube/storage-provisioner:v5
* Enabled addons: default-storageclass, storage-provisioner
* Done! kubectl is now configured to use "service-app-tutorial" cluster and "default" namespace by default
* minikube profile was successfully set to service-app-tutorial
```

#### Create Namespace

```bash {"stage":"init", "runtime":"bash" }
kubectl create namespace service-app-project
kubectl config set-context --current --namespace=service-app-project
```
```shell markdown_runner
namespace/service-app-project created
Context "service-app-tutorial" modified.
```

#### Install Cert-Manager

```bash {"stage":"init", "label":"install cert-manager", "runtime":"bash"}
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.1/cert-manager.yaml
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
role.rbac.authorization.k8s.io/cert-manager-webhook:dynamic-serving created
rolebinding.rbac.authorization.k8s.io/cert-manager-cainjector:leaderelection created
rolebinding.rbac.authorization.k8s.io/cert-manager:leaderelection created
rolebinding.rbac.authorization.k8s.io/cert-manager-webhook:dynamic-serving created
service/cert-manager created
service/cert-manager-webhook created
deployment.apps/cert-manager-cainjector created
deployment.apps/cert-manager created
deployment.apps/cert-manager-webhook created
mutatingwebhookconfiguration.admissionregistration.k8s.io/cert-manager-webhook created
validatingwebhookconfiguration.admissionregistration.k8s.io/cert-manager-webhook created
Warning: unrecognized format "int32"
Warning: unrecognized format "int64"
```

Wait for `cert-manager` to be ready.

```bash {"stage":"init", "label":"wait for cert-manager", "runtime":"bash"}
kubectl wait deployment --for=condition=Available -n cert-manager --timeout=600s cert-manager cert-manager-cainjector cert-manager-webhook
```
```shell markdown_runner
deployment.apps/cert-manager condition met
deployment.apps/cert-manager-cainjector condition met
deployment.apps/cert-manager-webhook condition met
```

#### Install Trust Manager

First, add the Jetstack Helm repository.

```bash {"stage":"init", "label":"add jetstack helm repo", "runtime":"bash"}
helm repo add jetstack https://charts.jetstack.io --force-update
```
```shell markdown_runner
"jetstack" has been added to your repositories
```

Now, install `trust-manager`.

```bash {"stage":"init", "label":"install trust-manager", "runtime":"bash"}
helm upgrade trust-manager jetstack/trust-manager --install --namespace cert-manager --set secretTargets.enabled=true --set secretTargets.authorizedSecretsAll=true --wait
```
```shell markdown_runner
Release "trust-manager" does not exist. Installing it now.
NAME: trust-manager
LAST DEPLOYED: Wed Jun 10 17:43:00 2026
NAMESPACE: cert-manager
STATUS: deployed
REVISION: 1
DESCRIPTION: Install complete
TEST SUITE: None
NOTES:
⚠️  WARNING: Consider increasing the Helm value `replicaCount` to 2 if you require high availability.
⚠️  WARNING: Consider setting the Helm value `podDisruptionBudget.enabled` to true if you require high availability.

trust-manager v0.22.1 has been deployed successfully!
Your installation includes a default CA package, using the following
default CA package image:

:

It's imperative that you keep the default CA package image up to date.
To find out more about securely running trust-manager and to get started
with creating your first bundle, check out the documentation on the
cert-manager website:

https://cert-manager.io/docs/projects/trust-manager/
I0610 17:43:00.287935 3452312 warnings.go:107] "Warning: unrecognized format \"int64\""
```

#### Install the Operator

```{"stage":"init", "rootdir":"$initial_dir"}
./deploy/install_opr.sh
```
```shell markdown_runner
Deploying operator to watch single namespace
Client Version: 4.18.5
Kustomize Version: v5.4.2
Kubernetes Version: v1.34.0
customresourcedefinition.apiextensions.k8s.io/activemqartemises.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisaddresses.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemisscaledowns.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/activemqartemissecurities.broker.amq.io created
customresourcedefinition.apiextensions.k8s.io/brokers.broker.arkmq.org created
customresourcedefinition.apiextensions.k8s.io/brokerapps.broker.arkmq.org created
customresourcedefinition.apiextensions.k8s.io/brokerservices.broker.arkmq.org created
serviceaccount/arkmq-org-broker-controller-manager created
role.rbac.authorization.k8s.io/arkmq-org-broker-operator-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-operator-rolebinding created
role.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-role created
rolebinding.rbac.authorization.k8s.io/arkmq-org-broker-leader-election-rolebinding created
networkpolicy.networking.k8s.io/arkmq-org-broker-controller-manager-netpol created
deployment.apps/arkmq-org-broker-controller-manager created
Warning: unrecognized format "int32"
Warning: unrecognized format "int64"
```

Wait for the operator pod to become ready.

```bash {"stage":"init", "label":"wait for the operator to be running", "runtime":"bash"}
kubectl wait deployment arkmq-org-broker-controller-manager --for=create --timeout=240s
kubectl wait pod --all --for=condition=Ready --namespace=service-app-project --timeout=600s
```
```shell markdown_runner
deployment.apps/arkmq-org-broker-controller-manager condition met
pod/arkmq-org-broker-controller-manager-589b668cb8-jkkvt condition met
```

### 2. Configure Certificates
We'll set up a CA and issue certificates for the service and the application.

#### Create Issuers and Root Certificate

First the root issuer.

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

Then the root certificate.

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

Then a signing issuer that uses the root certificate.

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

#### Install the CA Bundle in the `cert-manager` namespace

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

### 3. Deploy the Messaging Service and Application

#### Create Service Certificate

The service needs a certificate customized with a matching common name to enable
mTLS communication.

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

```bash {"stage":"deploy_certs", "label":"wait for broker cert", "runtime":"bash"}
kubectl wait certificate messaging-service-broker-cert -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
certificate.cert-manager.io/messaging-service-broker-cert condition met
```

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
```shell markdown_runner
brokerservice.broker.arkmq.org/messaging-service created
```

Wait for the resource to be ready.

```bash {"stage":"deploy_service", "label":"wait for service"}
kubectl wait BrokerService messaging-service -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerservice.broker.arkmq.org/messaging-service condition met
```

#### Create Application Certificate

```bash {"stage":"deploy_app", "label":"create app cert", "runtime":"bash"}
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: first-app-app-cert
  namespace: service-app-project
spec:
  secretName: first-app-app-cert
  commonName: first-app
  issuerRef:
    name: broker-ca-issuer
    kind: ClusterIssuer
EOF
```
```shell markdown_runner
certificate.cert-manager.io/first-app-app-cert created
```

#### Deploy `BrokerApp`

The `BrokerApp` connects to a `BrokerService` using label selectors and declares
its messaging capabilities. The operator automatically assigns a port from the
service's port pool for the application's acceptor.

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
```shell markdown_runner
brokerapp.broker.arkmq.org/first-app created
```

Wait for the resource to be ready.

```bash {"stage":"deploy_app", "label":"wait for app", "runtime":"bash"}
kubectl wait BrokerApp first-app -n service-app-project --for=condition=Ready --timeout=300s
```
```shell markdown_runner
brokerapp.broker.arkmq.org/first-app condition met
```

#### Verify Port Assignment

You can check the automatically assigned port in the app's status:

```bash {"stage":"deploy_app", "label":"check assigned port", "runtime":"bash"}
kubectl get BrokerApp first-app -n service-app-project -o jsonpath='{.status.service.assignedPort}'
```
```shell markdown_runner
61616### ENV ###
SHELL=/bin/bash
COLORTERM=truecolor
HISTCONTROL=ignoreboth
XDG_MENU_PREFIX=gnome-
QT_IM_MODULES=wayland;ibus
HISTSIZE=-1
HOSTNAME=li-75457f4c-288e-11b2-a85c-cf0a42e43e10.ibm.com
TERMINATOR_DBUS_PATH=/net/tenshu/Terminator2
LESS_TERMCAP_se=[0m
LESS_TERMCAP_so=[01;33m
GUESTFISH_OUTPUT=\e[0m
SSH_AUTH_SOCK=/home/tlavocat/.ssh/agent/s.Fd47eG1g0z.agent.S5XlJgklBL
AWS_PROFILE=saml
XDG_CONFIG_HOME=/home/tlavocat/.config
MEMORY_PRESSURE_WRITE=c29tZSAyMDAwMDAgMjAwMDAwMAA=
TERMINATOR_UUID=urn:uuid:2972088a-aa97-42a3-b0c3-d3fbb4459c33
XMODIFIERS=@im=ibus
DESKTOP_SESSION=gnome
SSH_AGENT_PID=1812643
GPG_TTY=/dev/pts/0
EDITOR=nvim
GOBIN=/home/tlavocat/dev/go-version
PWD=/tmp/421962197
XDG_SESSION_DESKTOP=gnome
LOGNAME=tlavocat
XDG_SESSION_TYPE=wayland
SYSTEMD_EXEC_PID=1804279
XAUTHORITY=/run/user/1000/.mutter-Xwaylandauth.Q8DNQ3
GUESTFISH_RESTORE=\e[0m
GDM_LANG=en_US.UTF-8
HOME=/home/tlavocat
USERNAME=tlavocat
AUTOJUMP_ERROR_PATH=/home/tlavocat/.local/share/autojump/errors.log
LANG=en_US.UTF-8
LS_COLORS=rs=0:di=01;34:ln=01;36:mh=00:pi=40;33:so=01;35:do=01;35:bd=40;33;01:cd=40;33;01:or=40;31;01:mi=00:su=37;41:sg=30;43:ca=00:tw=30;42:ow=34;42:st=37;44:ex=01;32:*.7z=01;31:*.ace=01;31:*.alz=01;31:*.apk=01;31:*.arc=01;31:*.arj=01;31:*.bz=01;31:*.bz2=01;31:*.cab=01;31:*.cpio=01;31:*.crate=01;31:*.deb=01;31:*.drpm=01;31:*.dwm=01;31:*.dz=01;31:*.ear=01;31:*.egg=01;31:*.esd=01;31:*.gz=01;31:*.jar=01;31:*.lha=01;31:*.lrz=01;31:*.lz=01;31:*.lz4=01;31:*.lzh=01;31:*.lzma=01;31:*.lzo=01;31:*.pyz=01;31:*.rar=01;31:*.rpm=01;31:*.rz=01;31:*.sar=01;31:*.swm=01;31:*.t7z=01;31:*.tar=01;31:*.taz=01;31:*.tbz=01;31:*.tbz2=01;31:*.tgz=01;31:*.tlz=01;31:*.txz=01;31:*.tz=01;31:*.tzo=01;31:*.tzst=01;31:*.udeb=01;31:*.war=01;31:*.whl=01;31:*.wim=01;31:*.xz=01;31:*.z=01;31:*.zip=01;31:*.zoo=01;31:*.zst=01;31:*.avif=01;35:*.jpg=01;35:*.jpeg=01;35:*.jxl=01;35:*.mjpg=01;35:*.mjpeg=01;35:*.gif=01;35:*.bmp=01;35:*.pbm=01;35:*.pgm=01;35:*.ppm=01;35:*.tga=01;35:*.xbm=01;35:*.xpm=01;35:*.tif=01;35:*.tiff=01;35:*.png=01;35:*.svg=01;35:*.svgz=01;35:*.mng=01;35:*.pcx=01;35:*.mov=01;35:*.mpg=01;35:*.mpeg=01;35:*.m2v=01;35:*.mkv=01;35:*.webm=01;35:*.webp=01;35:*.ogm=01;35:*.mp4=01;35:*.m4v=01;35:*.mp4v=01;35:*.vob=01;35:*.qt=01;35:*.nuv=01;35:*.wmv=01;35:*.asf=01;35:*.rm=01;35:*.rmvb=01;35:*.flc=01;35:*.avi=01;35:*.fli=01;35:*.flv=01;35:*.gl=01;35:*.dl=01;35:*.xcf=01;35:*.xwd=01;35:*.yuv=01;35:*.cgm=01;35:*.emf=01;35:*.ogv=01;35:*.ogx=01;35:*.aac=00;36:*.au=00;36:*.flac=00;36:*.m4a=00;36:*.mid=00;36:*.midi=00;36:*.mka=00;36:*.mp3=00;36:*.mpc=00;36:*.ogg=00;36:*.ra=00;36:*.wav=00;36:*.oga=00;36:*.opus=00;36:*.spx=00;36:*.xspf=00;36:*~=00;90:*#=00;90:*.bak=00;90:*.crdownload=00;90:*.dpkg-dist=00;90:*.dpkg-new=00;90:*.dpkg-old=00;90:*.dpkg-tmp=00;90:*.old=00;90:*.orig=00;90:*.part=00;90:*.rej=00;90:*.rpmnew=00;90:*.rpmorig=00;90:*.rpmsave=00;90:*.swp=00;90:*.tmp=00;90:*.ucf-dist=00;90:*.ucf-new=00;90:*.ucf-old=00;90:
CLAUDE_CODE_USE_VERTEX=1
XDG_CURRENT_DESKTOP=GNOME
MEMORY_PRESSURE_WATCH=/sys/fs/cgroup/user.slice/user-1000.slice/user@1000.service/session.slice/org.gnome.SettingsDaemon.MediaKeys.service/memory.pressure
VTE_VERSION=8400
WAYLAND_DISPLAY=wayland-0
GUESTFISH_PS1=\[\e[1;32m\]><fs>\[\e[0;31m\] 
TPM2_PKCS11_LOG_LEVEL=0
INVOCATION_ID=7a06dbd2ac3f41c493539e54d390d40f
TERMINATOR_DBUS_NAME=net.tenshu.Terminator25ef4b219e3b005583550f2b0f9f990c3
MANAGERPID=1803621
ANTHROPIC_VERTEX_PROJECT_ID=itpc-gcp-cp-pe-eng-claude
WORKING_DIR=/home/tlavocat/dev/activemq-artemis-operator
MOZ_GMP_PATH=/usr/lib64/mozilla/plugins/gmp-gmpopenh264/system-installed
GNOME_SETUP_DISPLAY=unix:/tmp/.X11-unix/X1
GROFF_NO_SGR=1
XDG_SESSION_CLASS=user
TERM=xterm-256color
LESS_TERMCAP_mb=[01;31m
LESS_TERMCAP_me=[0m
LESS_TERMCAP_md=[01;35m
LESSOPEN=||/usr/bin/lesspipe.sh %s
USER=tlavocat
VISUAL=nvim
AUTOJUMP_SOURCED=1
DISPLAY=:0
LESS_TERMCAP_ue=[0m
SHLVL=23
LESS_TERMCAP_us=[04;36m
PAGER=/usr/bin/less -R
TSS2_LOG=fapi+NONE
GUESTFISH_INIT=\e[1;34m
JDK_JAVA_OPTIONS=-Djavax.net.ssl.trustStore=/etc/pki/java/cacerts
QT_IM_MODULE=ibus
MANAGERPIDFDID=1789835
CLASSPATH=:~/bin/spark/spark-2.2.0-bin-hadoop2.7/jars
XDG_RUNTIME_DIR=/run/user/1000
DEBUGINFOD_URLS=ima:enforcing https://debuginfod.fedoraproject.org/ ima:ignore 
DOCKER_HOST=unix:///run/user/1000/podman/podman.sock
DEBUGINFOD_IMA_CERT_PATH=/etc/keys/ima:
TPM2_PKCS11_STORE=/etc/tpm2_pkcs11
CLOUD_ML_REGION=us-east5
JOURNAL_STREAM=10:12557962
XDG_DATA_DIRS=/home/tlavocat/.local/share/flatpak/exports/share:/var/lib/flatpak/exports/share:/usr/local/share/:/usr/share/
BROWSER=firefox-free
PATH=/home/tlavocat/.yarn/bin:/home/tlavocat/.config/yarn/global/node_modules/.bin:/home/tlavocat/dev/go-version:/home/tlavocat/dev/golang/bin:/home/tlavocat/.local/bin/:/home/tlavocat/.local/bin:/usr/local/bin:/usr/bin:/home/tlavocat/bin~/bin/CMU-Cam_Toolkit_v2/src
GDMSESSION=gnome
XDG_SESSION_EXTRA_DEVICE_ACCESS=render:accel
DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus
MAIL=/var/spool/mail/tlavocat
GIO_LAUNCHED_DESKTOP_FILE_PID=1812326
OLDPWD=/home/tlavocat
GOPATH=/home/tlavocat/dev/golang
_=/usr/bin/printenv

```

### 4. Test Messaging

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
```shell markdown_runner
secret/cert-pemcfg created
```

```bash {"stage":"test_messaging", "label":"wait for pemcfg secret", "runtime":"bash"}
until kubectl get secret cert-pemcfg -n service-app-project &> /dev/null; do echo "Waiting for secret..." && sleep 2; done
```

#### Run Producer Job

The producer job uses environment variables from the binding secret to connect to the
correct host and port assigned by the operator. The binding secret name follows the
pattern `{app-name}-binding-secret`.

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
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: first-app-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg
      restartPolicy: OnFailure
EOT
```
```shell markdown_runner
job.batch/producer created
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
          secretName: arkmq-org-broker-manager-ca
      - name: cert
        secret:
          secretName: first-app-app-cert
      - name: pem
        secret:
          secretName: cert-pemcfg
      restartPolicy: OnFailure
EOT
```
```shell markdown_runner
job.batch/consumer created
```

Wait for jobs to complete.

```bash {"stage":"test_messaging", "label":"wait for jobs", "runtime":"bash"}
kubectl wait job producer -n service-app-project --for=condition=Complete --timeout=300s
kubectl wait job consumer -n service-app-project --for=condition=Complete --timeout=300s
```
```shell markdown_runner
job.batch/producer condition met
job.batch/consumer condition met
```

### 5. Cleanup

Delete our BrokerApp

```bash {"stage":"teardown", "label":"delete app", "runtime":"bash"}
kubectl delete BrokerApp first-app -n service-app-project
```
```shell markdown_runner
brokerapp.broker.arkmq.org "first-app" deleted from service-app-project namespace
```

Finally, delete the minikube cluster.

```bash {"stage":"teardown", "requires":"init/minikube_start", "runtime":"bash"}
minikube delete --profile service-app-tutorial
```
```shell markdown_runner
* Deleting "service-app-tutorial" in kvm2 ...
* Removed all traces of the "service-app-tutorial" cluster.
```

