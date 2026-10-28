# acme-app

A Camel Quarkus AMQP client used as every workload of the
[Acme ID tutorial](../acme-identity-pipeline.md).

One image plays every stage of the pipeline: what a pod does on the broker is
configuration, and always mirrors the capabilities its BrokerApp declares.

## Roles

| `APP_ROLE` | Reads | Writes | BrokerApp capabilities |
|---|---|---|---|
| `source` | — | `OUT_DEST` at `RATE` msg/s | `producerOf` |
| `relay` | `IN_DEST` | `OUT_DEST` | `consumerOf` + `producerOf` |
| `sink` | `IN_DEST` | — | `consumerOf` |
| `loop` | `IN_DEST` | `OUT_DEST`, same private address | both, sandbox load test |

Destinations are written `queue:NAME` or `topic:NAME`. A pub/sub address is
written as a topic and read through the subscription queue the operator creates
for the consumer, by its fully qualified name `queue:ADDRESS::SUBSCRIPTION`.

## Environment

| Variable | Meaning | Default |
|---|---|---|
| `APP_ROLE` | `source`, `relay`, `sink` or `loop` | `sink` |
| `APP_NAME` | name in the logs, and AMQP username matching the app certificate | `acme-app` |
| `IN_DEST` | destination read | — |
| `OUT_DEST` | destination written | — |
| `RATE` | cases emitted per second | `2` |
| `DELAY_MS` | simulated work per case | `0` |
| `BROKER_HOST`, `BROKER_PORT` | broker endpoint | — |

`BROKER_HOST` and `BROKER_PORT` come from the binding secret the operator writes
once the BrokerApp is placed on a BrokerService. The tutorial injects them with
`secretKeyRef`, so the kubelet does not start the container before the secret
exists.

The connection is mTLS. The pod mounts the CA bundle at `/app/tls/ca`, its app
certificate at `/app/tls/client`, and the PEM keystore config at `/app/tls/pem`;
see [`application.properties`](src/main/resources/application.properties).

## Build

The `Containerfile` is a multi-stage build: Maven and the JDK run inside the
builder stage, so no local JDK or Maven installation is required. From the root
of the repository:

```bash
podman build -f docs/tutorials/acme-identity-pipeline/acme-app/Containerfile \
  docs/tutorials/acme-identity-pipeline/acme-app/ -t acme-app:latest
```

The tutorial then loads the image into minikube, and its Deployments reference
it with `imagePullPolicy: Never`.
