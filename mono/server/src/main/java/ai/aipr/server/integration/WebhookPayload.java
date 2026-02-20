package ai.aipr.server.integration;

/**
 * Webhook payload received from integrations.
 */
public record WebhookPayload(
        String platform,
        String event,
        String body,
        String signature,
        String deliveryId
) {}
