# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.27.1

GOLANG_VERSION?="1.23"
GO ?= $(shell source ./scripts/common.sh && get_go_path $(GOLANG_VERSION))/go

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

GIT_VERSION?=0.0.0
MANIFEST_HOST?=hybrid-assets.eks.amazonaws.com
HYBRID_MANIFEST_URL=https://$(MANIFEST_HOST)/manifest.yaml

.PHONY: all
all: crds generate fmt vet build

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

.PHONY: clean
clean:
	rm -rf $(LOCALBIN)

.PHONY: crds
crds: controller-gen ## Generate CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=crds/

.PHONY: generate
generate: generate-code generate-doc ## Generate code and documentation.

.PHONY: generate-code
generate-code: controller-gen conversion-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object paths="./..."
	$(CONVERSION_GEN) --go-header-file=/dev/null --output-file zz_generated.conversion.go -v0 github.com/aws/eks-hybrid/internal/api/bridge

.PHONY: generate-doc
generate-doc: crd-ref-docs
	$(CRD_REF_DOCS) --config=doc/config.yaml --source-path=api/ --renderer=markdown --templates-dir=doc/templates --output-path=doc/api.md
	cat -s doc/api.md > doc/api.md.tmp
	sed '$$ d' doc/api.md.tmp > doc/api.md
	rm doc/api.md.tmp

.PHONY: fmt
fmt: ## Run go fmt against code.
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	$(GO) vet ./...

.PHONY: test
test: ## Run validate tests.
	$(GO) test ./... 

COVERAGEFILE = $(LOCALBIN)/coverage.out
.PHONY: coverage
coverage: test
	$(GO) test -coverprofile=$(COVERAGEFILE) ./...
	$(GO) tool cover -html=$(COVERAGEFILE)

.PHONY: test-integration
test-integration: ## Run integration tests.
	test/integration/run.sh

.PHONY: lint
lint: golangci-lint ## Run golangci-lint.
	$(GOLANGCI_LINT) run -v

##@ Build

.PHONY: build
build: LINKER_FLAGS :=-X github.com/aws/eks-hybrid/cmd/nodeadm/version.GitVersion=$(GIT_VERSION) -X github.com/aws/eks-hybrid/internal/aws.manifestUrl=$(HYBRID_MANIFEST_URL) -s -w -buildid='' -extldflags -static
build: ## Build nodeadm binary.
	$(GO) build -ldflags "$(LINKER_FLAGS)" -trimpath -o $(LOCALBIN)/nodeadm cmd/nodeadm/main.go

.PHONY: build-cross-platform
build-cross-platform: LINKER_FLAGS :=-X github.com/aws/eks-hybrid/cmd/nodeadm/version.GitVersion=$(GIT_VERSION) -X github.com/aws/eks-hybrid/internal/aws.manifestUrl=$(HYBRID_MANIFEST_URL) -s -w -buildid='' -extldflags -static
build-cross-platform: ## Build binary for Linux amd64 and arm64.
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LINKER_FLAGS)" -trimpath -o $(LOCALBIN)/amd64/nodeadm cmd/nodeadm/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LINKER_FLAGS)" -trimpath -o $(LOCALBIN)/arm64/nodeadm cmd/nodeadm/main.go

.PHONY: run
run: build ## Run nodeadm binary.
	$(GO) run cmd/nodeadm/main.go $(args)

.PHONY: e2e-tests-binary
e2e-tests-binary: ## Build binary with e2e tests.
	CGO_ENABLED=0 $(GO) test -ldflags "-s -w -buildid='' -extldflags -static" -c ./test/e2e/suite -o ./_bin/e2e.test -tags "e2e"

.PHONY: build-cross-e2e-tests-binary
build-cross-e2e-tests-binary:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) test -ldflags "-s -w -buildid='' -extldflags -static" -c ./test/e2e/suite -o ./_bin/amd64/e2e.test -tags "e2e"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) test -ldflags "-s -w -buildid='' -extldflags -static" -c ./test/e2e/suite -o ./_bin/arm64/e2e.test -tags "e2e"

.PHONY: e2e-test
e2e-test: ## Build e2e test setup binary.
	CGO_ENABLED=0 $(GO) build -ldflags "-s -w -buildid='' -extldflags -static" -o _bin/e2e-test ./cmd/e2e-test/main.go

.PHONY: build-cross-e2e-test
build-cross-e2e-test:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags "-s -w -buildid='' -extldflags -static" -o _bin/amd64/e2e-test ./cmd/e2e-test/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -ldflags "-s -w -buildid='' -extldflags -static" -o _bin/arm64/e2e-test ./cmd/e2e-test/main.go

.PHONY: generate-attribution
generate-attribution:
	scripts/make_attribution.sh $(GOLANG_VERSION)

.PHONY: generate-attribution-in-docker
generate-attribution-in-docker:
	mkdir -p _output/.go/mod/cache
	docker run --rm --pull=always -e GOPROXY=$(GOPROXY) -e GOMODCACHE=/mod-cache -v  $$(pwd)/_output/.go/mod/cache:/mod-cache -v $$(pwd):/eks-hybrid public.ecr.aws/eks-distro-build-tooling/builder-base:standard-latest.al23 make -C /eks-hybrid generate-attribution

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/_bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
CONVERSION_GEN ?= $(LOCALBIN)/conversion-gen
CRD_REF_DOCS ?= $(LOCALBIN)/crd-ref-docs
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT_CONFIG ?= .github/workflows/golangci-lint.yml
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
GINKGO ?= $(LOCALBIN)/ginkgo

## Tool Versions
KUSTOMIZE_VERSION ?= v5.0.1
CONTROLLER_TOOLS_VERSION ?= v0.16.3
CODE_GENERATOR_VERSION ?= v0.30.6
CRD_REF_DOCS_VERSION ?= v0.1.0
GINKGO_VERSION ?= v2.19.0

tools: kustomize controller-gen conversion-gen crd-ref-docs ginkgo ## Install the toolchain.

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || \
	GOBIN=$(LOCALBIN) GO111MODULE=on $(GO) install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) $(GO) install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: conversion-gen
conversion-gen: $(CONVERSION_GEN) ## Download conversion-gen locally if necessary.
$(CONVERSION_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/conversion-gen || \
	GOBIN=$(LOCALBIN) $(GO) install k8s.io/code-generator/cmd/conversion-gen@$(CODE_GENERATOR_VERSION)

.PHONY: crd-ref-docs
crd-ref-docs: $(CRD_REF_DOCS) ## Download crd-ref-docs locally if necessary.
$(CRD_REF_DOCS): $(LOCALBIN)
	test -s $(LOCALBIN)/crd-ref-docs || \
	GOBIN=$(LOCALBIN) $(GO) install github.com/elastic/crd-ref-docs@$(CRD_REF_DOCS_VERSION)

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo.
$(GINKGO): $(LOCALBIN)
	GOBIN=$(LOCALBIN) $(GO) install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

.PHONY: install-cross-ginkgo
install-cross-ginkgo: $(LOCALBIN)/amd64/ginkgo $(LOCALBIN)/arm64/ginkgo

# When cross-compiling with go install, you can not override the output directory
# with GOBIN.  In the case that it is a different arch than the host
# go install will put the bins in a `goos_goarch` folder under GOPATH/bin
$(LOCALBIN)/%/ginkgo: GOARCH=$*
$(LOCALBIN)/%/ginkgo: GOPATH=$(shell $(GO) env GOPATH | cut -d ':' -f 1)
$(LOCALBIN)/%/ginkgo: CROSS_ARCH_INSTALL_FILE=$(GOPATH)/bin/linux_$(GOARCH)/$(@F)
$(LOCALBIN)/%/ginkgo: NATIVE_INSTALL_FILE=$(GOPATH)/bin/$(@F)
$(LOCALBIN)/%/ginkgo:
	mkdir -p $(@D)
	CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) $(GO) install -ldflags "-extldflags -static" github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)
	if [ -f $(CROSS_ARCH_INSTALL_FILE) ]; then cp $(CROSS_ARCH_INSTALL_FILE) $@; else cp $(NATIVE_INSTALL_FILE) $@; fi

.PHONY: update-deps
update-deps:
	$(GO) get $(shell $(GO) list -f '{{if not (or .Main .Indirect)}}{{.Path}}{{end}}' -mod=mod -m all) && $(GO) mod tidy

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint.
$(GOLANGCI_LINT): $(LOCALBIN) $(GOLANGCI_LINT_CONFIG)
	$(eval GOLANGCI_LINT_VERSION?=$(shell cat .github/workflows/golangci-lint.yml | yq e '.jobs.golangci.steps[] | select(.name == "golangci-lint") .with.version' -))
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(LOCALBIN) $(GOLANGCI_LINT_VERSION)
