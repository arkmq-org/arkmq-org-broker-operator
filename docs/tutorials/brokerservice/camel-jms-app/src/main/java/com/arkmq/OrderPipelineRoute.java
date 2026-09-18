package com.arkmq;

import jakarta.enterprise.context.ApplicationScoped;
import org.apache.camel.Exchange;
import org.apache.camel.builder.RouteBuilder;
import org.eclipse.microprofile.config.inject.ConfigProperty;

import java.time.Instant;
import java.util.Random;
import java.util.UUID;

/**
 * Single reusable Camel route for all pipeline stages.
 *
 * Role is selected by the APP_ROLE environment variable:
 *
 *   generator  — produces realistic order JSON into ORDERS.NEW
 *   processor  — consumes ORDERS.NEW, enriches, forwards to ORDERS.PROCESSED
 *   shipping   — consumes ORDERS.PROCESSED, enriches, forwards to ORDERS.SHIPPED
 *   delivery   — consumes ORDERS.SHIPPED, enriches, forwards to ORDERS.DELIVERED
 *   sink       — consumes any queue, logs and discards (used for drain/testing)
 *
 * Configurable per deployment:
 *
 *   APP_ROLE               generator | processor | shipping | delivery | sink
 *   CONSUMER_QUEUE         input queue  (not used when APP_ROLE=generator)
 *   PRODUCER_QUEUE         output queue (not used when APP_ROLE=sink)
 *   MESSAGE_RATE           messages/sec for the generator (default 25)
 *   PROCESSING_DELAY_MS    artificial latency in ms      (default 0)
 *   ERROR_RATE             fraction 0.0–1.0 that fails   (default 0.0)
 *   CONSUMER_CONCURRENCY   parallel consumers per route  (default 1)
 */
@ApplicationScoped
public class OrderPipelineRoute extends RouteBuilder {

    private static final Random RNG = new Random();

    // ------------------------------------------------------------------
    // Configuration injected from environment variables / system props
    // ------------------------------------------------------------------

    @ConfigProperty(name = "app.role", defaultValue = "sink")
    String role;

    @ConfigProperty(name = "consumer.queue", defaultValue = "ORDERS.NEW")
    String consumerQueue;

    @ConfigProperty(name = "producer.queue", defaultValue = "NONE")
    String producerQueue;

    @ConfigProperty(name = "message.rate", defaultValue = "25")
    int messageRate;

    @ConfigProperty(name = "processing.delay.ms", defaultValue = "0")
    int processingDelayMs;

    @ConfigProperty(name = "error.rate", defaultValue = "0.0")
    double errorRate;

    @ConfigProperty(name = "consumer.concurrency", defaultValue = "1")
    int consumerConcurrency;

    // ------------------------------------------------------------------
    // Route configuration
    // ------------------------------------------------------------------

    @Override
    public void configure() {
        validateConfiguration();

        String normalizedRole = role.trim().toLowerCase();

        switch (normalizedRole) {
            case "generator" -> configureGenerator();
            case "processor" -> configureProcessor();
            case "shipping"  -> configureShipping();
            case "delivery"  -> configureDelivery();
            case "sink"      -> configureSink();
            default -> throw new IllegalArgumentException(
                    "Unknown APP_ROLE '" + role + "'. " +
                    "Valid values: generator, processor, shipping, delivery, sink");
        }
    }

    // ------------------------------------------------------------------
    // generator — injects orders into ORDERS.NEW at MESSAGE_RATE msg/s
    // ------------------------------------------------------------------

    private void configureGenerator() {
        String target = resolveProducerQueue("ORDERS.NEW");
        // Calculate the interval delay (in milliseconds) between generated messages.
// - Minimum rate is 1 msg/sec (a rate <= 0 falls back to 1000ms).
// - Maximum effective rate is 1000 msgs/sec (rates > 1000 are capped to a 1ms delay by Math.max).
        long periodMs = messageRate > 0 ? Math.max(1L, 1000L / messageRate) : 1000L;

        from("timer:order-generator?period=" + periodMs + "&delay=2000")
            .routeId("order-generator")
            .process(exchange -> {
                String orderId = "ORD-" + shortId();
                exchange.getIn().setHeader("orderId", orderId);
                exchange.getIn().setBody(buildOrderJson(orderId));
            })
            .log("[generator] → " + target + " | orderId=${header.orderId}")
            .toF("jms:queue:%s", target);
    }

    // ------------------------------------------------------------------
    // processor — ORDERS.NEW → enrich → ORDERS.PROCESSED
    // ------------------------------------------------------------------

    private void configureProcessor() {
        String from    = consumerQueue;
        String to      = resolveProducerQueue("ORDERS.PROCESSED");

        fromF("jms:queue:%s?concurrentConsumers=%d", from, consumerConcurrency)
            .routeId("order-processor")
            .log("[processor] ← " + from + " | processing...")
            .process(this::applyDelay)
            .process(this::simulateError)
            .process(exchange -> enrichOrder(exchange, "PROCESSED",
                    "\"warehouse\":\"AMS-01\""))
            .log("[processor] → " + to + " | status=PROCESSED")
            .toF("jms:queue:%s", to);
    }

    // ------------------------------------------------------------------
    // shipping — ORDERS.PROCESSED → enrich → ORDERS.SHIPPED
    // ------------------------------------------------------------------

    private void configureShipping() {
        String from = consumerQueue;
        String to   = resolveProducerQueue("ORDERS.SHIPPED");

        fromF("jms:queue:%s?concurrentConsumers=%d", from, consumerConcurrency)
            .routeId("order-shipping")
            .log("[shipping] ← " + from + " | shipping...")
            .process(this::applyDelay)
            .process(this::simulateError)
            .process(exchange -> enrichOrder(exchange, "SHIPPED",
                    "\"trackingNumber\":\"NL" + shortId() + "\""))
            .log("[shipping] → " + to + " | status=SHIPPED")
            .toF("jms:queue:%s", to);
    }

    // ------------------------------------------------------------------
    // delivery — ORDERS.SHIPPED → enrich → ORDERS.DELIVERED
    // ------------------------------------------------------------------

    private void configureDelivery() {
        String from = consumerQueue;
        String to   = resolveProducerQueue("ORDERS.DELIVERED");

        fromF("jms:queue:%s?concurrentConsumers=%d", from, consumerConcurrency)
            .routeId("order-delivery")
            .log("[delivery] ← " + from + " | delivering...")
            .process(this::applyDelay)
            .process(this::simulateError)
            .process(exchange -> enrichOrder(exchange, "DELIVERED",
                    "\"deliveredAt\":\"" + Instant.now() + "\""))
            .log("[delivery] → " + to + " | status=DELIVERED")
            .toF("jms:queue:%s", to);
    }

    // ------------------------------------------------------------------
    // sink — consumes and discards (drain / testing)
    // ------------------------------------------------------------------

    private void configureSink() {
        String[] queues = consumerQueue.split(",");
        for (String q : queues) {
            String trimmed = q.trim();
            if (trimmed.isEmpty()) continue;

            fromF("jms:queue:%s?concurrentConsumers=%d", trimmed, consumerConcurrency)
                .routeId("sink-" + trimmed.toLowerCase().replace('.', '-'))
                .log("[sink] drained 1 message from " + trimmed);
        }
    }

    // ------------------------------------------------------------------
    // Helpers
    // ------------------------------------------------------------------

    /**
     * Returns the configured producer queue, or the supplied default when the
     * property is absent, blank, or set to the sentinel value "NONE".
     * Use PRODUCER_QUEUE=NONE for roles that have no output queue (e.g. sink).
     */
    private String resolveProducerQueue(String defaultQueue) {
        if (producerQueue == null
                || producerQueue.trim().isEmpty()
                || producerQueue.trim().equalsIgnoreCase("NONE")) {
            return defaultQueue;
        }
        return producerQueue.trim();
    }

    /**
     * Validates all numeric configuration properties at startup.
     * Fails fast with a clear message rather than misbehaving at runtime.
     */
    private void validateConfiguration() {
        if (messageRate < 0) {
            throw new IllegalArgumentException("MESSAGE_RATE must be >= 0, got: " + messageRate);
        }
        if (processingDelayMs < 0) {
            throw new IllegalArgumentException("PROCESSING_DELAY_MS must be >= 0, got: " + processingDelayMs);
        }
        if (errorRate < 0.0 || errorRate > 1.0) {
            throw new IllegalArgumentException("ERROR_RATE must be between 0.0 and 1.0, got: " + errorRate);
        }
        if (consumerConcurrency < 1) {
            throw new IllegalArgumentException("CONSUMER_CONCURRENCY must be >= 1, got: " + consumerConcurrency);
        }
    }

    /**
     * Sleeps for PROCESSING_DELAY_MS milliseconds to simulate work.
     */
    private void applyDelay(Exchange exchange) throws InterruptedException {
        if (processingDelayMs > 0) {
            Thread.sleep(processingDelayMs);
        }
    }

    /**
     * Randomly throws an exception for ERROR_RATE fraction of messages.
     * JMS/Camel error handling determines how the failed message is handled.
     */
    private void simulateError(Exchange exchange) {
        if (errorRate > 0.0 && RNG.nextDouble() < errorRate) {
            throw new RuntimeException(
                    "[pipeline] Simulated processing failure (error.rate=" + errorRate + ")");
        }
    }

    /**
     * Appends status, processedAt, and an optional extra field to the order JSON.
     * Works on the raw JSON string without requiring a full JSON library.
     */
    private void enrichOrder(Exchange exchange, String status, String extraField) {
        String body = exchange.getIn().getBody(String.class);
        if (body == null) body = "{}";

        // Remove the trailing "}" so we can append fields.
        String trimmed = body.trim();
        if (trimmed.endsWith("}")) {
            trimmed = trimmed.substring(0, trimmed.length() - 1);
        }

        String enriched = trimmed
                + ",\"status\":\"" + status + "\""
                + ",\"processedAt\":\"" + Instant.now() + "\""
                + "," + extraField
                + "}";

        exchange.getIn().setBody(enriched);
    }

    /**
     * Builds a realistic order JSON string.
     */
    private String buildOrderJson(String orderId) {
        String customerId = "CUST-" + (10000 + RNG.nextInt(90000));
        String sku1       = "SKU-" + (10000 + RNG.nextInt(90000));
        String sku2       = "SKU-" + (10000 + RNG.nextInt(90000));
        int    qty1       = 1 + RNG.nextInt(5);
        int    qty2       = 1 + RNG.nextInt(3);
        double price1     = Math.round((5 + RNG.nextDouble() * 95) * 100.0) / 100.0;
        double price2     = Math.round((5 + RNG.nextDouble() * 45) * 100.0) / 100.0;
        double total      = Math.round((qty1 * price1 + qty2 * price2) * 100.0) / 100.0;

        String[] countries = {"NL", "DE", "FR", "GB", "ES", "IT", "SE", "PL"};
        String   country   = countries[RNG.nextInt(countries.length)];

        return "{"
                + "\"orderId\":\"" + orderId + "\""
                + ",\"customerId\":\"" + customerId + "\""
                + ",\"timestamp\":\"" + Instant.now() + "\""
                + ",\"items\":["
                +   "{\"sku\":\"" + sku1 + "\",\"quantity\":" + qty1 + ",\"price\":" + price1 + "}"
                + ",{\"sku\":\"" + sku2 + "\",\"quantity\":" + qty2 + ",\"price\":" + price2 + "}"
                + "]"
                + ",\"total\":" + total
                + ",\"shippingCountry\":\"" + country + "\""
                + ",\"status\":\"NEW\""
                + "}";
    }

    /** Six-character hex string derived from a random UUID. */
    private static String shortId() {
        return UUID.randomUUID().toString().replace("-", "").substring(0, 6);
    }
}
