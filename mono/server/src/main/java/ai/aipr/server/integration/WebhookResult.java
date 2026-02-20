package ai.aipr.server.integration;

/**
 * Result from processing a webhook.
 */
public record WebhookResult(
        String action,
        String reviewId,
        boolean success,
        String message
) {
    public static WebhookResult success(String action) {
        return new WebhookResult(action, null, true, null);
    }

    public static WebhookResult success(String action, String reviewId) {
        return new WebhookResult(action, reviewId, true, null);
    }

    public static WebhookResult skipped(String reason) {
        return new WebhookResult("skipped", null, true, reason);
    }

    public static WebhookResult error(String message) {
        return new WebhookResult("error", null, false, message);
    }
}
