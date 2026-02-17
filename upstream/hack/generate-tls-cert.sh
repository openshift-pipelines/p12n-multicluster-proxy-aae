#!/usr/bin/env bash

set -e

NAMESPACE=${NAMESPACE:-proxy-aae}
SECRET_NAME=${SECRET_NAME:-proxy-server-tls}
SERVICE_NAME=${SERVICE_NAME:-proxy-aae}
FULL_DNS_NAME="${SERVICE_NAME}.${NAMESPACE}.svc.cluster.local"

echo "Generating self-signed certificate for ${FULL_DNS_NAME}..."

openssl req -x509 \
-newkey rsa:4096 \
-keyout key.pem \
-out cert.pem \
-days 365 \
-nodes \
-subj "/CN=${FULL_DNS_NAME}" \
-addext "subjectAltName = DNS:${FULL_DNS_NAME}"

echo "Creating TLS secret ${SECRET_NAME} in namespace ${NAMESPACE}..."

# Create namespace if it doesn't exist
kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | kubectl apply -f -

# Create secret
kubectl create secret tls -n ${NAMESPACE} ${SECRET_NAME} \
--cert=cert.pem \
--key=key.pem

rm key.pem cert.pem

echo "Done!"