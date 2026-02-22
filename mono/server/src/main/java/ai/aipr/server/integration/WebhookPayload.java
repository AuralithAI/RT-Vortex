package ai.aipr.server.integration;

/**
 * Webhook payload received from a VCS platform integration.
 *
 * <p>The {@code authToken} field carries platform-specific auth material:</p>
 * <ul>
 *   <li><b>GitHub</b> — HMAC-SHA256 signature from {@code X-Hub-Signature-256} header
 *       (format: {@code sha256=<hex>})</li>
 *   <li><b>GitLab</b> — plain secret token from {@code X-Gitlab-Token} header</li>
 *   <li><b>Bitbucket</b> — not used (Bitbucket Cloud sends no auth header by default);
 *       {@code null} is expected</li>
 * </ul>
 *
 * <p>The {@code deliveryId} is a platform-assigned request identifier for deduplication
 * and tracing ({@code X-GitHub-Delivery}, {@code X-Request-UUID}, etc.).</p>
 */
public record WebhookPayload(
        String platform,
        String event,
        String body,
        String authToken,
        String deliveryId
) {}
