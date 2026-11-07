ARG GO_BUILDER=registry.redhat.io/ubi9/go-toolset:latest@sha256:c2bd0309bf78ae317024fdfc5602a31704e0dba9572f6d4b0bd4b681033dc965
ARG UBI_RUNTIME=registry.redhat.io/ubi9/ubi-minimal-pqc:latest@sha256:c7fdcf2617a608d07f95fa01d8936fc8bf898096a60e5ebc432aae8ead50ea6c

FROM $GO_BUILDER AS builder

WORKDIR /go/src/github.com/openshift-pipelines/multicluster-proxy-aae
COPY upstream .
COPY .konflux/patches ./patches
ENV GODEBUG="http2server=0"
ENV GOEXPERIMENT=strictfipsruntime

RUN set -e; for f in patches/*.patch; do echo ${f}; [[ -f ${f} ]] || continue; git apply ${f}; done
RUN CGO_ENABLED=1 go build -mod=vendor -tags disable_gcp,strictfipsruntime  -v -o /tmp/proxy-aae \
    ./cmd/proxy-server

FROM $UBI_RUNTIME
WORKDIR /


COPY --from=builder /tmp/proxy-aae /ko-app/proxy-aae

LABEL \
    com.redhat.component="openshift-pipelines-multicluster-proxy-aae-rhel9-container" \
    cpe="cpe:/a:redhat:openshift_pipelines:1.24::el9" \
    description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.k8s.description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.k8s.display-name="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.openshift.tags="tekton,openshift,multicluster-proxy-aae,multicluster-proxy-aae" \
    maintainer="pipelines-extcomm@redhat.com" \
    name="openshift-pipelines/pipelines-multicluster-proxy-aae-rhel9" \
    summary="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    version="v1.24.4"

RUN microdnf install -y shadow-utils && \
    groupadd -r -g 65532 nonroot && useradd --no-log-init -r -u 65532 -g nonroot nonroot
USER 65532

EXPOSE 8080
ENTRYPOINT ["/ko-app/proxy-aae"]
