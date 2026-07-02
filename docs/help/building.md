---
title: "Building"
description: "Building arkmq-org.io"
lead: "Building arkmq-org.io"
date: 2020-10-06T08:49:31+00:00
lastmod: 2020-10-06T08:49:31+00:00
draft: false
images: []
menu:
  docs:
    parent: "help"
weight: 630
toc: true
---

# Building the operator

## Prerequisites

### Go

Download the Go version v1.25.9 from the [download page](https://go.dev/dl/) and install it following the [installation instructions](https://go.dev/doc/install).

### Operator SDK

Install [Operator SDK](https://sdk.operatorframework.io/) version [v1.28.0](https://github.com/operator-framework/operator-sdk/releases/tag/v1.28.0) following the [installation instructions from a GitHub release](https://sdk.operatorframework.io/docs/installation/#install-from-github-release).

### Docker

Install Docker following the [installation instructions](https://docs.docker.com/get-docker/).

## Get the code

```$xslt
git clone https://github.com/arkmq-org/arkmq-org-broker-operator
cd arkmq-org-broker-operator
git checkout main
```

## Building the code locally

```$xslt
make
```
or if you have modified the CRD types in ./api
```$xslt
make generate build && make generate-deploy && make bundle && make helm-charts
```

## Building the operator image

There are 2 variables you may need to override in order to push the images to your preferred registry.

```$xslt
OPERATOR_IMAGE_REPO (your preferred image registry name, for example quay.io/hgao/operator
```
and
```$xslt
OPERATOR_VERSION (the image's tag, for example v1.1)
```

Now build the image passing the variables

```$xslt
make OPERATOR_IMAGE_REPO=<your repo> OPERATOR_VERSION=<tag> docker-build
```

If finished sucessfully it will print the image url in the end. The image url is like

```$xslt
${OPERATOR_IMAGE_REPO}:${TAG}
```

## Push the image to registry

```$xslt
docker push ${OPERATOR_IMAGE_REPO}:${TAG}
```
or use the make target **docker-push**
```$xslt
make OPERATOR_IMAGE_REPO=<your repo> OPERATOR_VERSION=<tag> docker-push
```

Now follow the [quickstart](../getting-started/quick-start.md) to deploy the operator.
