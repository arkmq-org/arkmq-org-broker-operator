# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 2.1.5

KUBE_CLI=kubectl
OPERATOR_VERSION := $(VERSION)
OPERATOR_ACCOUNT_NAME := arkmq-org-broker-operator
OPERATOR_CLUSTER_ROLE_NAME := operator-role
OPERATOR_IMAGE_REPO := quay.io/arkmq-org/arkmq-org-broker-operator
OPERATOR_NAMESPACE := arkmq-org-broker-operator
BUNDLE_VERSION := $(VERSION)
BUNDLE_PACKAGE := arkmq-org-broker-operator
BUNDLE_ANNOTATION_PACKAGE := $(BUNDLE_PACKAGE)
MANAGER_VERSION := $(VERSION)
GO_MODULE := github.com/arkmq-org/arkmq-org-broker-operator
OS := $(shell go env GOOS)
ARCH := $(shell go env GOARCH)
OPM_VERSION := v1.55.0
OPERATOR_SDK_VERSION := v1.42.2
YQ_VERSION := v4.52.5

HELM_CHART_NAME := $(BUNDLE_PACKAGE)
HELMIFY ?= $(LOCALBIN)/helmify
HELMIFY_VERSION ?= v0.4.18

# Check that the system's go version is compatible with the one stored in the
# go.mod file.
RUNTIME_GO_VERSION := $(shell go version)
REQUIRED_GO_VERSION := $(subst go ,,$(shell grep -i -e '^go .*' go.mod))

ifeq (,$(findstring $(REQUIRED_GO_VERSION),$(RUNTIME_GO_VERSION)))
$(error The go version $(RUNTIME_GO_VERSION) the system is currently running is incompatible with the required one $(REQUIRED_GO_VERSION))
endif

# directory to hold static resources for deploying operator
DEPLOY := ./deploy

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
CHANNELS = "stable"
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
DEFAULT_CHANNEL = "stable"
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# quay.io/arkmq-org/arkmq-org-broker-operator-bundle:$VERSION and quay.io/arkmq-org/arkmq-org-broker-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/arkmq-org/arkmq-org-broker-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(BUNDLE_VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
IMG ?= $(OPERATOR_IMAGE_REPO):$(OPERATOR_VERSION)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.26.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

BUILD_TIMESTAMP := $(shell date '+%Y-%m-%dT%H:%M:%S')
LDFLAGS = "-X '$(GO_MODULE)/version.BuildTimestamp=$(BUILD_TIMESTAMP)'"


.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen
## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
## v2alpha3, v2alpha4 and v2alpha3 requires allowDangerousTypes=true because they use float32 type
	$(CONTROLLER_GEN) rbac:roleName=$(OPERATOR_CLUSTER_ROLE_NAME) crd:allowDangerousTypes=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	sed -i 's~^LABEL version=.*~LABEL version="$(VERSION)"~g' Dockerfile
	sed -i 's~\tVersion = ".*"~\tVersion = "$(MANAGER_VERSION)"~' version/version.go

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet -composites=false ./...

## Run tests.
test test-v: TEST_VARS = KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" RECONCILE_RESYNC_PERIOD=5s

## Run tests against minikube with local operator.
test-mk test-mk-v: TEST_ARGS += -test.timeout=120m -ginkgo.label-filter='!do'
test-mk test-mk-v: TEST_VARS = USE_EXISTING_CLUSTER=true RECONCILE_RESYNC_PERIOD=5s

## Run tests against minikube with deployed operator(do)
test-mk-do test-mk-do-v: TEST_ARGS += -test.timeout=60m -ginkgo.label-filter='do'
test-mk-do test-mk-do-v: TEST_VARS = DEPLOY_OPERATOR=true USE_EXISTING_CLUSTER=true

## Run tests against minikube with deployed operator(do) and exclude slow, useful for CI smoke
test-mk-do-fast test-mk-do-fast-v: TEST_ARGS += -test.timeout=30m -ginkgo.label-filter='do && !slow'
test-mk-do-fast test-mk-do-fast-v: TEST_VARS = DEPLOY_OPERATOR=true USE_EXISTING_CLUSTER=true

test-v test-mk-v test-mk-do-v test-mk-do-fast-v: TEST_ARGS += -v
test-v test-mk test-mk-v test-mk-do test-mk-do-v test-mk-do-fast test-mk-do-fast-v: TEST_ARGS += -ginkgo.poll-progress-after=150s -ginkgo.fail-fast -coverprofile cover-mk.out

test test-v: manifests generate fmt vet envtest
	$(TEST_VARS) go test ./... -p 1 $(TEST_ARGS) $(TEST_EXTRA_ARGS)

test-mk test-mk-v test-mk-do test-mk-do-v test-mk-do-fast test-mk-do-fast-v: TEST_VARS += HELM=$(HELM)
test-mk test-mk-v test-mk-do test-mk-do-v test-mk-do-fast test-mk-do-fast-v: manifests generate fmt vet envtest helm
	$(TEST_VARS) go test ./... -p 1 $(TEST_ARGS) $(TEST_EXTRA_ARGS)

##@ Debugging

.PHONY: debug
debug: manifests generate ## Run the operator with Delve in interactive mode
	DEFAULT_OPERATOR_NAMESPACE="$(OPERATOR_NAMESPACE)" \
	WATCH_NAMESPACE="$(WATCH_NAMESPACE)" \
	POD_NAME="local-debug" \
	OPERATOR_NAME="arkmq-org-broker-operator" \
	dlv debug ./main.go -- --leader-elect=false

.PHONY: debug-remote
debug-remote: manifests generate ## Run the operator with Delve exposing a socket for IDE (localhost:2345)
	@echo ""
	@echo "\033[32mStarting Delve debugger on localhost:2345\033[0m"
	@echo "Connect your IDE: Run > Start Debugging > 'Attach to Delve'"
	@echo ""
	DEFAULT_OPERATOR_NAMESPACE="$(OPERATOR_NAMESPACE)" \
	WATCH_NAMESPACE="$(WATCH_NAMESPACE)" \
	POD_NAME="local-debug" \
	OPERATOR_NAME="arkmq-org-broker-operator" \
	dlv debug ./main.go --headless --listen=:2345 --api-version=2 --accept-multiclient -- --leader-elect=false

##@ Build

.PHONY: build
build: generate fmt vet manifests ## Build manager binary.
	go build -ldflags=$(LDFLAGS) -o bin/manager main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run -ldflags=$(LDFLAGS) ./main.go

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: test generate-deploy ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

# PLATFORMS defines the target platforms for  the manager image be build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: test ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- docker buildx create --name project-v3-builder
	docker buildx use project-v3-builder
	- docker buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- docker buildx rm project-v3-builder
	rm Dockerfile.cross

.PHONY: podman-remote-build
podman-remote-build: build generate-deploy
	podman-remote build -t ${IMG} .

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBE_CLI) apply --server-side -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBE_CLI) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize generate-deploy ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBE_CLI) apply --server-side -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBE_CLI) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy-dry-run
deploy-dry-run: manifests kustomize ## Create deploy yaml file in tmp/deploy.yaml
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	mkdir -p tmp && $(KUSTOMIZE) build config/default > tmp/deploy.yaml

generate-deploy: manifests kustomize ## Generate deployment artifacts in separate files in $(DEPLOY) dir
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | hack/static_manifest_gen.sh $(DEPLOY) $(OPERATOR_NAMESPACE)

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v3.8.7
CONTROLLER_TOOLS_VERSION ?= v0.16.5

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || { curl -Ss $(KUSTOMIZE_INSTALL_SCRIPT) | bash -s -- $(subst v,,$(KUSTOMIZE_VERSION)) $(LOCALBIN); }

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.20

.PHONY: bundle
bundle: manifests operator-sdk kustomize ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q --package $(BUNDLE_PACKAGE)
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle --package $(BUNDLE_PACKAGE) $(BUNDLE_GEN_FLAGS)
	sed -i.bak '/creationTimestamp:/d' ./bundle/manifests/*.yaml && rm ./bundle/manifests/*.bak
	sed -i.bak "s/createdAt:.*/`grep -oP 'createdAt:.*' config/manifests/bases/$(BUNDLE_PACKAGE).clusterserviceversion.yaml`/" ./bundle/manifests/*.yaml && rm ./bundle/manifests/*.bak
	sed -i.bak 's|containerImage: .*|containerImage: '${IMG}'|' ./bundle/manifests/*.yaml && rm ./bundle/manifests/*.bak
	sed -i "s~ version:.*~ version: \"${VERSION}\"~" config/metadata/$(BUNDLE_PACKAGE).annotations.yaml
	sed 's/annotations://' config/metadata/$(BUNDLE_PACKAGE).annotations.yaml >> bundle/metadata/annotations.yaml
	sed -e 's/annotations://' -e 's/  /LABEL /g' -e 's/: /=/g'  config/metadata/$(BUNDLE_PACKAGE).annotations.yaml >> bundle.Dockerfile
	sed -i.bak 's/operators.operatorframework.io.bundle.package.v1:.*/operators.operatorframework.io.bundle.package.v1: $(BUNDLE_ANNOTATION_PACKAGE)/' bundle/metadata/annotations.yaml && rm bundle/metadata/annotations.yaml.bak
	sed -i.bak 's/operators.operatorframework.io.bundle.package.v1=.*/operators.operatorframework.io.bundle.package.v1=$(BUNDLE_ANNOTATION_PACKAGE)/' bundle.Dockerfile && rm bundle.Dockerfile.bak
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-clean
bundle-clean: ## Clean the bundle directory
	rm -rf ./bundle/*

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: helm
HELM = $(LOCALBIN)/helm
helm: ## Download helm locally if necessary. Ref: https://helm.sh/docs/intro/install/
ifeq (,$(wildcard $(HELM)))
ifeq (,$(shell which helm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(HELM)) ;\
	curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 && chmod 700 get_helm.sh && HELM_INSTALL_DIR=$(LOCALBIN) ./get_helm.sh --no-sudo && rm ./get_helm.sh;\
	chmod +x $(HELM) ;\
	}
else
HELM = $(shell which helm)
endif
endif
	
.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/${OPM_VERSION}/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

.PHONY: yq
YQ = ./bin/yq
yq: ##Download yq locally if necessary
ifeq (,$(wildcard $(YQ)))
ifeq (,$(shell which yq 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(YQ)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(YQ) https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_$${OS}_$${ARCH} ;\
	chmod +x $(YQ) ;\
	}
else
YQ = $(shell which yq)
endif
endif

.PHONY: operator-sdk
OPERATOR_SDK = $(shell pwd)/bin/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (,$(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/operator-sdk_${OS}_${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif	
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

.PHONY: catalog-clean
catalog-clean: ## Clean up catalog files and Dockerfile
	rm -rf catalog

CATALOG_GEN_FLAGS ?= -q --overwrite --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)

.PHONY: catalog-generate 
catalog-generate: catalog-clean opm yq ## Generate the catalog by adding bundles to stable channel. It requires BUNDLE_IMG exists before running the target"
	mkdir -p catalog
	cp config/catalog/basic-catalog-template.yaml catalog/basic-catalog-template.yaml
	chmod +x ./hack/update-catalog-template.sh
	./hack/update-catalog-template.sh catalog/basic-catalog-template.yaml $(BUNDLE_IMGS) $(YQ)
	$(OPM) alpha render-template basic catalog/basic-catalog-template.yaml \
		--migrate-level=bundle-object-to-csv-metadata -o yaml \
		$(OPM_FLAGS) > catalog/operator.yaml
	$(OPM) validate catalog $(OPM_FLAGS)
	rm -f catalog/basic-catalog-template.yaml

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe uses 'opm alpha render-template basic' to generate a catalog from a template.
# The template defines bundle images and channel relationships in a declarative way.
.PHONY: catalog-build
catalog-build: catalog-generate
	docker build -f catalog.Dockerfile -t $(CATALOG_IMG) .

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

# To run scorecard test
.PHONY: scorecard
scorecard: bundle ## Run scorecard
	$(OPERATOR_SDK) scorecard ./bundle

.PHONY: helmify
helmify: $(HELMIFY) ## Download helmify locally if necessary.
$(HELMIFY): $(LOCALBIN)
	test -s $(LOCALBIN)/helmify || GOBIN=$(LOCALBIN) go install github.com/arttor/helmify/cmd/helmify@$(HELMIFY_VERSION)

helm-charts: manifests kustomize helmify yq helm
	$(KUSTOMIZE) build config/default | $(HELMIFY) -crd-dir -original-name -image-pull-secrets helm-charts/$(HELM_CHART_NAME)
	./hack/fix_helm_chart.sh $(VERSION) helm-charts/$(HELM_CHART_NAME) $(YQ)
	$(HELM) template test helm-charts/$(HELM_CHART_NAME) --debug > /dev/null
