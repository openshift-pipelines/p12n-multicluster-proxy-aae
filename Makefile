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
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize

## Tool Versions
KUSTOMIZE_VERSION ?= v5.5.0

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
ko-build:
	ko build --local .

# Deploy to Kubernetes with ko
deploy:
	ko apply -R -f config/

# Undeploy from Kubernetes
undeploy:
	ko delete -R -f config/

# Port forward for local testing
port-forward:
	kubectl port-forward svc/proxy-aae 8080:80 -n $(NAMESPACE)

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
release: kustomize
	mkdir -p ${RELEASE_DIR}
	cd config && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config -o ${RELEASE_DIR}/release-${VERSION}.yaml