package ai.aipr.server.api;

import ai.aipr.server.integration.AbstractVcsPlatformClient.PlatformApiException;
import ai.aipr.server.integration.WebhookPayload;
import ai.aipr.server.integration.bitbucket.BitbucketWebhookHandler;
import ai.aipr.server.integration.github.GitHubWebhookHandler;
import ai.aipr.server.integration.gitlab.GitLabWebhookHandler;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.Map;

/**
 * Webhook endpoints for platform integrations.
 */
@RestController
@RequestMapping("/api/v1/webhooks")
public class WebhookController {

    private static final Logger log = LoggerFactory.getLogger(WebhookController.class);

    private final GitHubWebhookHandler gitHubHandler;
    private final GitLabWebhookHandler gitLabHandler;
    private final BitbucketWebhookHandler bitbucketHandler;

    public WebhookController(
            GitHubWebhookHandler gitHubHandler,
            GitLabWebhookHandler gitLabHandler,
            BitbucketWebhookHandler bitbucketHandler
    ) {
        this.gitHubHandler = gitHubHandler;
        this.gitLabHandler = gitLabHandler;
        this.bitbucketHandler = bitbucketHandler;
    }

    /**
     * GitHub webhook endpoint.
     */
    @PostMapping("/github")
    public ResponseEntity<Map<String, Object>> handleGitHub(
            @RequestHeader("X-GitHub-Event") String event,
            @RequestHeader(value = "X-Hub-Signature-256", required = false) String signature,
            @RequestHeader(value = "X-GitHub-Delivery", required = false) String deliveryId,
            @RequestBody String payload
    ) {
        log.info("GitHub webhook received: event={}, delivery={}", event, deliveryId);

        WebhookPayload webhookPayload = new WebhookPayload(
                "github", event, payload, signature, deliveryId
        );

        try {
            var result = gitHubHandler.handle(webhookPayload);
            return ResponseEntity.ok(Map.of(
                    "status", "accepted",
                    "event", event,
                    "action", result.action()
            ));
        } catch (SecurityException e) {
            log.warn("GitHub webhook signature verification failed: {}", e.getMessage());
            return ResponseEntity.status(401).body(Map.of("error", "Invalid signature"));
        } catch (PlatformApiException e) {
            log.error("{} API error during webhook processing: status={}, body={}",
                    e.getPlatform(), e.getStatusCode(), e.getResponseBody());
            return ResponseEntity.status(e.getStatusCode() >= 400 ? 502 : 500)
                    .body(Map.of("error", e.getMessage(), "api_status", e.getStatusCode()));
        } catch (Exception e) {
            log.error("GitHub webhook processing failed", e);
            return ResponseEntity.internalServerError().body(Map.of("error", e.getMessage()));
        }
    }

    /**
     * GitLab webhook endpoint.
     */
    @PostMapping("/gitlab")
    public ResponseEntity<Map<String, Object>> handleGitLab(
            @RequestHeader("X-Gitlab-Event") String event,
            @RequestHeader(value = "X-Gitlab-Token", required = false) String token,
            @RequestBody String payload
    ) {
        log.info("GitLab webhook received: event={}", event);

        WebhookPayload webhookPayload = new WebhookPayload(
                "gitlab", event, payload, token, null
        );

        try {
            var result = gitLabHandler.handle(webhookPayload);
            return ResponseEntity.ok(Map.of(
                    "status", "accepted",
                    "event", event,
                    "action", result.action()
            ));
        } catch (SecurityException e) {
            log.warn("GitLab webhook token verification failed: {}", e.getMessage());
            return ResponseEntity.status(401).body(Map.of("error", "Invalid token"));
        } catch (PlatformApiException e) {
            log.error("{} API error during webhook processing: status={}, body={}",
                    e.getPlatform(), e.getStatusCode(), e.getResponseBody());
            return ResponseEntity.status(e.getStatusCode() >= 400 ? 502 : 500)
                    .body(Map.of("error", e.getMessage(), "api_status", e.getStatusCode()));
        } catch (Exception e) {
            log.error("GitLab webhook processing failed", e);
            return ResponseEntity.internalServerError().body(Map.of("error", e.getMessage()));
        }
    }

    /**
     * Bitbucket Cloud webhook endpoint.
     *
     * <p>Bitbucket sends the event type in the {@code X-Event-Key} header.
     * Webhook secrets for Bitbucket Cloud are typically passed as a URL query parameter
     * rather than a header, so no signature verification is done here.</p>
     */
    @PostMapping("/bitbucket")
    public ResponseEntity<Map<String, Object>> handleBitbucket(
            @RequestHeader("X-Event-Key") String event,
            @RequestHeader(value = "X-Request-UUID", required = false) String requestUuid,
            @RequestBody String payload
    ) {
        log.info("Bitbucket webhook received: event={}, uuid={}", event, requestUuid);

        WebhookPayload webhookPayload = new WebhookPayload(
                "bitbucket", event, payload, null, requestUuid
        );

        try {
            var result = bitbucketHandler.handle(webhookPayload);
            return ResponseEntity.ok(Map.of(
                    "status", "accepted",
                    "event", event,
                    "action", result.action()
            ));
        } catch (PlatformApiException e) {
            log.error("{} API error during webhook processing: status={}, body={}",
                    e.getPlatform(), e.getStatusCode(), e.getResponseBody());
            return ResponseEntity.status(e.getStatusCode() >= 400 ? 502 : 500)
                    .body(Map.of("error", e.getMessage(), "api_status", e.getStatusCode()));
        } catch (Exception e) {
            log.error("Bitbucket webhook processing failed", e);
            return ResponseEntity.internalServerError().body(Map.of("error", e.getMessage()));
        }
    }

    /**
     * Azure DevOps webhook endpoint — placeholder for future implementation.
     */
    @PostMapping("/azure-devops")
    public ResponseEntity<Map<String, Object>> handleAzureDevOps(
            @RequestBody String payload
    ) {
        log.info("Azure DevOps webhook received");
        return ResponseEntity.ok(Map.of("status", "not_implemented"));
    }
}
