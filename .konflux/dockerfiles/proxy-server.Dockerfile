ARG GO_BUILDER=brew.registry.redhat.io/rh-osbs/openshift-golang-builder:v1.25
ARG RUNTIME=registry.redhat.io/ubi9/ubi-minimal@sha256:c7d44146f826037f6873d99da479299b889473492d3c1ab8af86f08af04ec8a0

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
      com.redhat.component="openshift-pipelines-multicluster-proxy-aae-proxy-server-rhel9-container" \
      cpe="cpe:/a:redhat:openshift_pipelines:1.21::el9" \
      description="Red Hat OpenShift Pipelines multicluster-proxy-aae proxy-server" \
      io.k8s.description="Red Hat OpenShift Pipelines multicluster-proxy-aae proxy-server" \
      io.k8s.display-name="Red Hat OpenShift Pipelines multicluster-proxy-aae proxy-server" \
      io.openshift.tags="tekton,openshift,multicluster-proxy-aae,proxy-server" \
      maintainer="pipelines-extcomm@redhat.com" \
      name="openshift-pipelines/pipelines-multicluster-proxy-aae-proxy-server-rhel9" \
      summary="Red Hat OpenShift Pipelines multicluster-proxy-aae proxy-server" \
      version="v1.21.1"

RUN microdnf install -y shadow-utils && \
    groupadd -r -g 65532 nonroot && useradd --no-log-init -r -u 65532 -g nonroot nonroot
USER 65532

EXPOSE 8080
ENTRYPOINT ["/ko-app/proxy-aae"]
