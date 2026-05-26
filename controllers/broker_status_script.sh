#!/bin/bash
# Signal the operator to reconcile when broker config reload completes.
# Requires AMQ221087 (Artemis 2.54+, ARTEMIS-6099).

PATCH_URL="https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}/api/v1/namespaces/${POD_NAMESPACE}/pods/${POD_NAME}"
AUTH="Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

signal_reconcile() {
  curl -sf --cacert "$CA_CERT" -X PATCH \
    -H "$AUTH" -H "Content-Type: application/merge-patch+json" \
    -d '{"metadata":{"annotations":{"broker.arkmq.org/reconcile-me":"true"}}}' \
    "$PATCH_URL" > /dev/null
}

tail -F "$RELOAD_LOG_PATH" 2>/dev/null | grep --line-buffered AMQ221087 | while read -r; do
  signal_reconcile
done
