#!/bin/sh
# The Deployment exists before its BrokerApp: the operator writes the binding
# secret (host, port, uri) once the app is placed on a BrokerService, and the
# kubelet then projects it into /app/binding. Wait for it rather than crash-loop.
BINDING=/app/binding
echo "[${APP_NAME}] waiting for the BrokerApp binding in ${BINDING}..."
until [ -s "${BINDING}/host" ] && [ -s "${BINDING}/port" ]; do
  sleep 5
done
export BROKER_HOST="$(cat ${BINDING}/host)"
export BROKER_PORT="$(cat ${BINDING}/port)"
echo "[${APP_NAME}] bound to ${BROKER_HOST}:${BROKER_PORT}, role=${APP_ROLE} in=${IN_DEST:-none} out=${OUT_DEST:-none}"
exec /opt/jboss/container/java/run/run-java.sh "$@"

# env var from secret?
