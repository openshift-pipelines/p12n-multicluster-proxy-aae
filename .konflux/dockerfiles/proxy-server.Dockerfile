ARG GO_BUILDER=brew.registry.redhat.io/rh-osbs/openshift-golang-builder:v1.24 
ARG RUNTIME=registry.redhat.io/ubi9/ubi-minimal@sha256:bb08f2300cb8d12a7eb91dddf28ea63692b3ec99e7f0fa71a1b300f2756ea829

FROM $GO_BUILDER AS builder

WORKDIR /go/src/github.com/openshift-pipelines/multicluster-proxy-aae
COPY upstream .
COPY .konflux/patches ./patches
RUN set -e; for f in patches/*.patch; do echo ${f}; [[ -f ${f} ]] || continue; git apply ${f}; done
ENV GODEBUG="http2server=0"
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -v -o /tmp/proxy-aae \
    ./cmd/proxy-server

FROM $RUNTIME
ARG VERSION=multicluster-proxy-aae-proxy-server-main

WORKDIR /


COPY --from=builder /tmp/proxy-aae /ko-app/proxy-aae

LABEL \
    com.redhat.component="openshift-pipelines-multicluster-proxy-aae-rhel9-container" \
    name="openshift-pipelines/multicluster-proxy-aae-rhel9" \
    version=$VERSION \
    summary="Red Hat OpenShift Pipelines Multicluster Proxy Service" \
    maintainer="pipelines-extcomm@redhat.com" \
    description="Red Hat OpenShift Pipelines Proxy Service" \
    io.k8s.display-name="Red Hat OpenShift Pipelines Proxy Service" \
    io.k8s.description="Red Hat OpenShift Pipelines Proxy Service" \
    io.openshift.tags="pipelines,tekton,openshift"

RUN microdnf install -y shadow-utils && \
    groupadd -r -g 65532 nonroot && useradd --no-log-init -r -u 65532 -g nonroot nonroot
USER 65532

EXPOSE 8080
ENTRYPOINT ["/ko-app/proxy-aae"]
