ARG GO_BUILDER=registry.access.redhat.com/ubi9/go-toolset:1.25
ARG RUNTIME=registry.access.redhat.com/ubi9/ubi-minimal:latest

FROM $GO_BUILDER AS builder

WORKDIR /go/src/github.com/openshift-pipelines/multicluster-proxy-aae
COPY upstream .
COPY .konflux/patches ./patches
ENV GODEBUG="http2server=0"
ENV GOEXPERIMENT=strictfipsruntime

RUN set -e; for f in patches/*.patch; do echo ${f}; [[ -f ${f} ]] || continue; git apply ${f}; done
RUN CGO_ENABLED=1 go build -mod=vendor -tags disable_gcp,strictfipsruntime  -v -o /tmp/proxy-aae \
    ./cmd/proxy-server

FROM $RUNTIME
WORKDIR /


COPY --from=builder /tmp/proxy-aae /ko-app/proxy-aae

LABEL \
    com.redhat.component="openshift-pipelines-multicluster-proxy-aae-rhel9-container" \
    cpe="cpe:/a:redhat:openshift_pipelines:1.22::el9" \
    description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.k8s.description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.k8s.display-name="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.openshift.tags="tekton,openshift,multicluster-proxy-aae,multicluster-proxy-aae" \
    maintainer="pipelines-extcomm@redhat.com" \
    name="openshift-pipelines/pipelines-multicluster-proxy-aae-rhel9" \
    summary="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    version="v1.22.0"

RUN microdnf install -y shadow-utils && \
    groupadd -r -g 65532 nonroot && useradd --no-log-init -r -u 65532 -g nonroot nonroot
USER 65532

EXPOSE 8080
ENTRYPOINT ["/ko-app/proxy-aae"]
