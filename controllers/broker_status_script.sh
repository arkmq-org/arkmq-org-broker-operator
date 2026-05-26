#!/bin/bash
# broker-status.sh -- reads the broker's raw Jolokia Status attribute and
# writes it verbatim as a pod annotation. All interpretation stays in the
# operator. On startup it fetches once, then tails reload.log for AMQ221087
# ("Configuration reload completed", Artemis 2.54+) to re-fetch. On older
# brokers where AMQ221087 is absent, the initial status is all the operator
# gets from the script -- it can still reconcile via its own requeue cycle.

PATCH_URL="https://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}/api/v1/namespaces/${POD_NAMESPACE}/pods/${POD_NAME}"
AUTH="Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

# Retro-compatibility: wait for Jolokia to be ready before the initial fetch.
# On Artemis < 2.54 AMQ221087 is not emitted, so this initial fetch is the
# only status the operator will ever get from the script. Without it, brokers
# that predate ARTEMIS-6099 would never have an annotation written.
JOLOKIA_OPTS="-u ${JOLOKIA_USER}:${JOLOKIA_PASSWORD} -H Origin:${JOLOKIA_URL}"

while ! curl -sf -o /dev/null ${JOLOKIA_OPTS} "${JOLOKIA_URL}/"; do
  sleep 5
done

fetch_and_patch() {
  RAW=$(curl -sf ${JOLOKIA_OPTS} \
    "${JOLOKIA_URL}/read/org.apache.activemq.artemis:broker=%22${BROKER_NAME}%22/Status")
  if [ $? -ne 0 ] || [ -z "$RAW" ]; then
    return 1
  fi
  VALUE=$(echo "$RAW" | jq -r '.value')
  if [ -z "$VALUE" ] || [ "$VALUE" = "null" ]; then
    return 1
  fi
  curl -sf --cacert "$CA_CERT" -X PATCH \
    -H "$AUTH" -H "Content-Type: application/merge-patch+json" \
    -d "{\"metadata\":{\"annotations\":{\"broker.arkmq.org/broker-status\":$(echo "$VALUE" | jq -c -R .)}}}" \
    "$PATCH_URL" > /dev/null
}

# Retro-compatibility: initial fetch for brokers without AMQ221087.
# Can be removed once Artemis < 2.54 is no longer supported.
fetch_and_patch

# AMQ221087 (ARTEMIS-6099, Artemis 2.54+): "Configuration reload completed".
# On older brokers this tail matches nothing and the script just idles.
tail -F "$RELOAD_LOG_PATH" 2>/dev/null | grep --line-buffered AMQ221087 | while read -r; do
  fetch_and_patch
done
