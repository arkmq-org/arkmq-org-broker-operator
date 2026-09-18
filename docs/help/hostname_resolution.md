---
title: "Resolve you cluster domain"
description: "Various possible configuration to make your local cluster domain
resolvable"
draft: false
images: []
menu:
  docs:
    parent: "help"
weight: 110
toc: true
---

If you are running a local `k8s` instance, you might want to configure your
local setup so that it is able to resolve the domain of the cluster. So that
out-of-cluster running programs can access your services.

There are a couple of options available to you, all with their pros & cons. We
will list some of them in this document. Note that this is not a exhaustive
coverage of all the possibilities and feel free to contribute to the
documentation if you have other ways of doing this.

The hostnames used by the operator are built from the same template regardless
of `exposeMode`. The variables `$(CR_NAME)`, `$(CR_NAMESPACE)`,
`$(BROKER_ORDINAL)`, `$(ITEM_NAME)`, `$(RES_TYPE)` and `$(INGRESS_DOMAIN)` are
substituted in `ingressHost` for the Ingress (`$(RES_TYPE) = ing`),
Route (`rte`), HTTPRoute (`httprte`) and TLSRoute (`tlsrte`) cases, so any
DNS setup that resolves the wildcard `*.$(INGRESS_DOMAIN)` works for all four
resource kinds.

## Prerequisite

Before you start, you need to have access to a running Kubernetes cluster
environment. A [Minikube](https://minikube.sigs.k8s.io/docs/start/) instance
running on your laptop will do fine.

### Start minikube with a parametrized `dns-domain` name

```console
$ minikube start --dns-domain='demo.arkmq-org.io'

😄  minikube v1.32.0 on Fedora 39
🎉  minikube 1.33.1 is available! Download it: https://github.com/kubernetes/minikube/releases/tag/v1.33.1
💡  To disable this notice, run: 'minikube config set WantUpdateNotification false'

✨  Automatically selected the kvm2 driver. Other choices: qemu2, ssh
👍  Starting control plane node minikube in cluster minikube
🔥  Creating kvm2 VM (CPUs=2, Memory=6000MB, Disk=20000MB) ...
🐳  Preparing Kubernetes v1.28.3 on Docker 24.0.7 ...
    ▪ Generating certificates and keys ...
    ▪ Booting up control plane ...
    ▪ Configuring RBAC rules ...
🔗  Configuring bridge CNI (Container Networking Interface) ...
    ▪ Using image gcr.io/k8s-minikube/storage-provisioner:v5
🔎  Verifying Kubernetes components...
🌟  Enabled addons: storage-provisioner, default-storageclass
🏄  Done! kubectl is now configured to use "minikube" cluster and "default" namespace by default
```

Get minikube's ip

```console
minikube ip
192.168.39.54
```

Note that every time you restart minikube you'll have to follow update the ip in
the configuration files.

## /etc/hosts

The generic way to make the url resolve to an IP address is to update the
`/etc/hosts/`. There's no wildcard in this file, this means you'll need to
specify all the urls you are interested in.

Here's an example for an ingress:
```console
$ cat /etc/hosts
192.168.39.54      ing.sslacceptor.send-receive-0.send-receive-project.demo.arkmq-org.io
```
### pros

* Works on every setup and is simple

### cons

* No wildcard, you need to list every domains and subdomains you'll need to access

## NetworkManager's DNSMasq plugin

We will use networkmanager's dnsmasq plugin
([source](https://fedoramagazine.org/using-the-networkmanagers-dnsmasq-plugin/))to
configure the ip associated with the domain `demo.arkmq-org.io`. The dnsmasq plugin
has wildcards which is better than setting manually every hosts in the
`/etc/hosts` file.

### Configure DNSMasq

The goal here is to set up enable dnsmasq and to make it resolve your cluster
domain `demo.arkmq-org.io` to the cluster's ip address. Because DNSMasq has a
wildcard, all subdomains will also resolve to the same ip address.

1. Create the following files:

```console
$ cat << EOF > /etc/NetworkManager/conf.d/00-use-dnsmasq.conf
[main]
dns=dnsmasq
EOF
```

```console
$ cat << EOF > /etc/NetworkManager/dnsmasq.d/00-demo.arkmq-org.io.conf
local=/demo.arkmq-org.io/
address=/.demo.arkmq-org.io/192.168.39.54
EOF
```

```console
$ cat << EOF > /etc/NetworkManager/dnsmasq.d/02-add-hosts.conf
addn-hosts=/etc/hosts
EOF
```

2. restart NetworkManager:

```console
$ sudo systemctl restart NetworkManager
```

### pros

* Has wildcard, you only need to setup the ip once

### Cons

* Works only with NetworkManager

## Minikube's `ingress-dns` plugin:

[Follow the official documentation.](https://minikube.sigs.k8s.io/docs/handbook/addons/ingress-dns/)

### pros

* Has wildcard, you only need to setup the ip once
* Supported for every setup (linux, mac, windows)

### cons

* Can only resolve the ingresses URLs
