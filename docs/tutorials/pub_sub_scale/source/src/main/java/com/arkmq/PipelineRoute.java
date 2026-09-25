package com.arkmq;

import org.apache.camel.builder.RouteBuilder;
import jakarta.enterprise.context.ApplicationScoped;
import java.util.concurrent.atomic.AtomicInteger;

@ApplicationScoped
public class PipelineRoute extends RouteBuilder {

    private final AtomicInteger messageCount = new AtomicInteger(0);

    @Override
    public void configure() throws Exception {

        // Evaluate the environment variable in Java
        String appRole = System.getenv("APP_ROLE");
        // Update the below string 'producer/consumer' as per YAML
        boolean isProducer = "producer".equalsIgnoreCase(appRole);
        boolean isConsumer = "consumer".equalsIgnoreCase(appRole);

        // PRODUCER ROLE: Only runs if APP_ROLE=producer
        from("timer:generator?period=100&repeatCount=15000")
                .routeId("test-data-generator")
                .autoStartup(isProducer)
                .setBody(constant("Auto-generated Test Order"))
                .to("paho-mqtt5:{{consumer.queue}}");

        // CONSUMER ROLE: Only runs if APP_ROLE=consumer
        from("paho-mqtt5:{{consumer.queue}}")
                .routeId("updated-app")
                .autoStartup(isConsumer)
                .process(exchange -> {
                    int currentCount = messageCount.incrementAndGet();
                    String originalBody = exchange.getIn().getBody(String.class);

                    String processedBody = originalBody + " -> [PROCESSED BY APP]";
                    exchange.getIn().setBody(processedBody);

                    if (currentCount % 100 == 0) {
                        log.info("Successfully bridged {} orders...", currentCount);
                    }
                })
                .log("About to send to {{producer.queue}}: ${body}")
                .to("paho-mqtt5:{{producer.queue}}")
                .log("Successfully sent to {{producer.queue}}");
    }
}
