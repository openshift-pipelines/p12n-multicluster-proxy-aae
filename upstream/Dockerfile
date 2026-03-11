# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Build the application
RUN go build -o proxy-aae ./cmd/proxy-server/main.go

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /app/proxy-aae .

# Use nonroot user
USER 65532:65532

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/proxy-aae"]

LABEL \
      com.redhat.component="openshift-multicluster-proxy-aae-rhel9-container" \
      cpe="cpe:/a:redhat:openshift_pipelines:0.1::el9" \
      description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
      io.k8s.description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
      io.k8s.display-name="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
      io.openshift.tags="tekton,openshift,multicluster-proxy-aae,multicluster-proxy-aae" \
      maintainer="pipelines-extcomm@redhat.com" \
      name="openshift-pipelines/multicluster-proxy-aae-rhel9" \
      summary="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
      version="v0.1.1"