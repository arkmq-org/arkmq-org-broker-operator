package com.acme;

import jakarta.enterprise.context.ApplicationScoped;
import org.apache.camel.Exchange;
import org.apache.camel.builder.RouteBuilder;
import org.eclipse.microprofile.config.inject.ConfigProperty;

import java.time.Instant;
import java.util.Optional;
import java.util.Random;
import java.util.concurrent.atomic.AtomicLong;

/**
 * One image for every Acme ID workload: the stage it plays is configuration.
 *
 * Acme ID runs an identity-verification pipeline. Each team deploys this image
 * with the role and destinations its BrokerApp declares, so what the pod does on
 * the broker always mirrors the BrokerApp's capabilities:
 *
 *   source  — emits cases on OUT_DEST at RATE msg/s        (producerOf)
 *   relay   — reads IN_DEST, works DELAY_MS, writes OUT_DEST (consumerOf + producerOf)
 *   sink    — reads IN_DEST, works DELAY_MS                (consumerOf)
 *   loop    — source and sink on the same private address  (sandbox load test)
 *
 * Destinations are written "queue:NAME" or "topic:NAME". A pub/sub address is
 * produced to as a topic, so the broker routes it MULTICAST, and read through the
 * subscription queue the operator creates for the consumer, by its fully
 * qualified name "queue:ADDRESS::SUBSCRIPTION".
 *
 * Environment:
 *   APP_ROLE   source | relay | sink | loop
 *   APP_NAME   name printed in logs and stamped on every case it touches
 *   IN_DEST    destination read  (relay, sink, loop)
 *   OUT_DEST   destination written (source, relay, loop)
 *   RATE       cases per second emitted (source, loop; default 2)
 *   DELAY_MS   simulated work per case (relay, sink; default 0)
 */
@ApplicationScoped
public class AcmeRoute extends RouteBuilder {

    private static final Random RNG = new Random();
    private static final AtomicLong CASES = new AtomicLong();

    @ConfigProperty(name = "acme.role", defaultValue = "sink")
    String role;

    @ConfigProperty(name = "acme.name", defaultValue = "acme-app")
    String name;

    // Empty values count as unset for Quarkus config, hence Optional.
    @ConfigProperty(name = "acme.in")
    Optional<String> in;

    @ConfigProperty(name = "acme.out")
    Optional<String> out;

    @ConfigProperty(name = "acme.rate", defaultValue = "2")
    int rate;

    @ConfigProperty(name = "acme.delay.ms", defaultValue = "0")
    int delayMs;

    @Override
    public void configure() {
        errorHandler(defaultErrorHandler().maximumRedeliveries(0).logExhaustedMessageHistory(false));

        switch (role.trim().toLowerCase()) {
            case "source" -> source(require("OUT_DEST", out));
            case "relay" -> consume(require("IN_DEST", in), require("OUT_DEST", out));
            case "sink" -> consume(require("IN_DEST", in), null);
            case "loop" -> {
                source(require("OUT_DEST", out));
                consume(require("IN_DEST", in), null);
            }
            default -> throw new IllegalArgumentException(
                    "Unknown APP_ROLE '" + role + "', expected source, relay, sink or loop");
        }
    }

    /** Emits a new case on the destination at the configured rate. */
    private void source(String dest) {
        long periodMs = rate > 0 ? Math.max(1L, 1000L / rate) : 1000L;
        from("timer:" + name + "-source?period=" + periodMs + "&delay=2000")
                .routeId(name + "-source")
                .process(this::newCase)
                .to(jmsUri(dest))
                .log("[" + name + "] emitted ${header.AcmeCase} -> " + dest);
    }

    /** Reads cases, stamps this stage on them, and forwards them when a destination is given. */
    private void consume(String from, String forward) {
        var route = from(jmsUri(from))
                .routeId(name + "-consume")
                .process(this::stamp)
                .process(exchange -> work());
        if (forward != null) {
            route.to(jmsUri(forward))
                    .log("[" + name + "] ${header.AcmeCase} " + from + " -> " + forward);
        } else {
            route.log("[" + name + "] ${header.AcmeCase} received from " + from);
        }
    }

    /** Simulates the time a stage spends on one case. */
    private void work() throws InterruptedException {
        if (delayMs > 0) {
            Thread.sleep(delayMs);
        }
    }

    private void newCase(Exchange exchange) {
        String id = String.format("ACME-%06d", CASES.incrementAndGet());
        exchange.getIn().setHeader("AcmeCase", id);
        exchange.getIn().setBody(String.format(
                "{\"case\":\"%s\",\"channel\":\"%s\",\"createdAt\":\"%s\",\"stages\":[\"%s\"]}",
                id, RNG.nextBoolean() ? "mobile" : "kiosk", Instant.now(), name));
    }

    private void stamp(Exchange exchange) {
        String body = exchange.getIn().getBody(String.class);
        if (body != null && body.endsWith("]}")) {
            exchange.getIn().setBody(body.substring(0, body.length() - 2) + ",\"" + name + "\"]}");
        }
    }

    /**
     * Maps "queue:NAME" or "topic:NAME" to a JMS endpoint. A bare name is a queue.
     * A fully qualified queue name keeps its "::", which the broker reads as
     * ADDRESS::QUEUE.
     */
    private String jmsUri(String dest) {
        String kind = "queue";
        String destination = dest;
        if (dest.startsWith("topic:")) {
            kind = "topic";
            destination = dest.substring("topic:".length());
        } else if (dest.startsWith("queue:")) {
            destination = dest.substring("queue:".length());
        }
        return "jms:" + kind + ":" + destination;
    }

    private static String require(String env, Optional<String> value) {
        return value.map(String::trim).filter(v -> !v.isEmpty()).orElseThrow(
                () -> new IllegalArgumentException(env + " is required for this APP_ROLE"));
    }
}
