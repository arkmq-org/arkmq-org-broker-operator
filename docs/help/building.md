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

Download the Go version v1.26.4 from the [download page](https://go.dev/dl/) and install it following the [installation instructions](https://go.dev/doc/install).

### Operator SDK

Install [Operator SDK](https://sdk.operatorframework.io/) version [v1.28.0](https://github.com/operator-framework/operator-sdk/releases/tag/v1.28.0) following the [installation instructions from a GitHub release](https://sdk.operatorframework.io/docs/installation/#install-from-github-release).

### Docker

Install Docker following the [installation instructions](https://docs.docker.com/get-docker/).

## Get the code

```bash
git clone https://github.com/arkmq-org/arkmq-org-broker-operator
cd arkmq-org-broker-operator
git checkout main
```

## Building the code locally

```bash
make
```
or if you have modified the CRD types in ./api
```bash
make generate build && make generate-deploy && make bundle && make helm-charts
```

## Building the operator image

There are 2 variables you may need to override in order to push the images to your preferred registry.

```bash
OPERATOR_IMAGE_REPO (your preferred image registry name, for example quay.io/hgao/operator
```
and
```bash
OPERATOR_VERSION (the image's tag, for example v1.1)
```

Now build the image passing the variables

```bash
make OPERATOR_IMAGE_REPO=<your repo> OPERATOR_VERSION=<tag> docker-build
```

If finished sucessfully it will print the image url in the end. The image url is like

```bash
${OPERATOR_IMAGE_REPO}:${TAG}
```

## Push the image to registry

```bash
docker push ${OPERATOR_IMAGE_REPO}:${TAG}
```
or use the make target **docker-push**
```bash
make OPERATOR_IMAGE_REPO=<your repo> OPERATOR_VERSION=<tag> docker-push
```

Now follow the [quickstart](../getting-started/quick-start.md) to deploy the operator.

## Update operator with new image

Once your custom image has been pushed to a registry, you must update the value of **spec.template.spec.containers.image** in **./deploy/operator.yaml**

```bash
        image: ${OPERATOR_IMAGE_REPO}:${TAG}
```

It is important to remember to build and push a new image whenever you pull new changes from the remote repository, or are testing local changes. 

## Add .vscode/settings.json

```bash
{
  "go.lintTool": "golangci-lint",
  "go.lintFlags": ["--fast"]
}
```

This ensures Cursor and VS Code users see the same findings as CI. Required for golangci-lint with ratchet mechanism to enforce lint quality over time.