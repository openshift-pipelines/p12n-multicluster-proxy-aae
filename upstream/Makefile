# Makefile for proxy-aae

# Variables
BINARY_NAME=proxy-aae
NAMESPACE=proxy-aae
IMG ?= ko://github.com/openshift-pipelines/multicluster-proxy
RELEASE_DIR ?= release
VERSION ?= nightly

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KO ?= $(LOCALBIN)/ko
KUSTOMIZE ?= $(LOCALBIN)/kustomize

## Tool Versions
KO_VERSION ?= v0.17.1
KUSTOMIZE_VERSION ?= v5.5.0

.PHONY: ko
ko: $(KO) ## Download ko locally if necessary.
$(KO): $(LOCALBIN)
	$(call go-install-tool,$(KO),github.com/google/ko,$(KO_VERSION))

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

# Build the binary
build:
	$(GOBUILD) -o $(BINARY_NAME) -v ./cmd/proxy-server/main.go

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	$(GOTEST) -v -cover ./...

# Download dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Build and push with ko
ko-build: $(KO)
	$(KO) build --local .

# Deploy to Kubernetes with ko
deploy: $(KO)
	kubectl kustomize config/base/ | $(KO) apply -f -

# Undeploy from Kubernetes
undeploy: $(KO)
	kubectl kustomize config/base/ | $(KO) delete -f -

# Port forward for local testing
port-forward:
	kubectl port-forward svc/proxy-aae 8080:443 -n $(NAMESPACE)

# Run locally
run:
	$(GOCMD) run ./cmd/proxy-server/main.go --port=8080

# Format code
fmt:
	$(GOCMD) fmt ./...

# Lint code
lint:
	golangci-lint run

# All targets
all: deps test build

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  clean         - Clean build artifacts"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  deps          - Download dependencies"
	@echo "  ko-build      - Build with ko (local)"
	@echo "  deploy        - Deploy to Kubernetes with ko"
	@echo "  undeploy      - Remove from Kubernetes"
	@echo "  port-forward  - Port forward for testing"
	@echo "  run           - Run locally"
	@echo "  fmt           - Format code"
	@echo "  lint          - Lint code"
	@echo "  all           - Run deps, test, and build"
	@echo "  help          - Show this help"

.PHONY: release
release: $(KUSTOMIZE)
	mkdir -p ${RELEASE_DIR}
	cd config/release && $(KUSTOMIZE) edit set image ko://github.com/openshift-pipelines/multicluster-proxy-aae/cmd/proxy-server=${IMG}:${VERSION}
	$(KUSTOMIZE) build config/release -o ${RELEASE_DIR}/release-${VERSION}.yaml