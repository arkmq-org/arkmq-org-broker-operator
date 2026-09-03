#!/bin/bash
# Signal the operator to reconcile on broker lifecycle events.
#
# Watched log codes (from org.apache.activemq.artemis.core.server):
#   AMQ221007  - Server is now active            (startup)
#   AMQ221087  - Configuration reload completed  (Artemis 2.54+, ARTEMIS-6099)
#
# Runs as a native sidecar (init container with restartPolicy: Always).
# Kubelet restarts the container on exit.

PATCH_URL="https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}/api/v1/namespaces/${POD_NAMESPACE}/pods/${POD_NAME}"
TOKEN_PATH=/var/run/secrets/kubernetes.io/serviceaccount/token
CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

signal_reconcile() {
  curl -sf --cacert "$CA_CERT" -X PATCH \
    -H "Authorization: Bearer $(cat "$TOKEN_PATH")" \
    -H "Content-Type: application/merge-patch+json" \
    -d '{"metadata":{"annotations":{"broker.arkmq.org/request-reconcile":"'"$(cat /proc/sys/kernel/random/uuid)"'"}}}' \
    "$PATCH_URL" > /dev/null
}

tail -F "$RELOAD_LOG_PATH" 2>/dev/null | grep -E --line-buffered 'AMQ221007|AMQ221087' | while read -r; do
  signal_reconcile
done
