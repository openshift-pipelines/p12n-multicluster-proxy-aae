# Makefile for proxy-aae

# Variables
BINARY_NAME=proxy-aae
NAMESPACE=proxy-aae
KO_IMAGE=ko://github.com/khrm/proxy-aae

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

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
