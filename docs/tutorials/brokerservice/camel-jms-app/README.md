# camel-jms-app

A Camel Quarkus JMS application used as the pipeline workload in the
[BrokerService Monitoring tutorial](../brokerservice.md).

The same container image is deployed multiple times with different `APP_ROLE`
environment variables, so a single image drives the entire order-processing
pipeline without rebuilding.

## Roles

| `APP_ROLE` | Consumes | Produces |
|---|---|---|
| `generator` | — | `ORDERS.NEW` |
| `processor` | `ORDERS.NEW` | `ORDERS.PROCESSED` |
| `shipping` | `ORDERS.PROCESSED` | `ORDERS.SHIPPED` |
| `delivery` | `ORDERS.SHIPPED` | `ORDERS.DELIVERED` |
| `sink` | any queue | — (drain) |

All other behaviour (queue names, message rate, processing delay, concurrency)
is controlled by environment variables. See
[`application.properties`](src/main/resources/application.properties) for the
full list.

## Build the container image

The image is built with the local Docker daemon, which provides internet access
for Maven to download dependencies, and then loaded directly into Minikube. No
container registry is required.

The source lives alongside this tutorial in [`camel-jms-app/`](.). The
`Containerfile` is a multi-stage build — Maven and the JDK run inside the
builder container, so no local JDK or Maven installation is required.

Run from the **root of the repository**:

```bash
docker build -f docs/tutorials/brokerservice/camel-jms-app/Containerfile docs/tutorials/brokerservice/camel-jms-app/ -t camel-jms-app:latest
minikube image load camel-jms-app:latest --profile brokerservice-monitoring
```

## Using the image

The tutorial Kubernetes Deployments reference the image as:

```yaml
image: camel-jms-app:latest
imagePullPolicy: Never
```

`imagePullPolicy: Never` tells Kubernetes to use the image already present in
the node's local store rather than contacting a registry.
