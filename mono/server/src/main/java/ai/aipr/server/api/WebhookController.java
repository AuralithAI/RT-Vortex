package ai.aipr.server.api;

import ai.aipr.server.integration.AbstractVcsPlatformClient.PlatformApiException;
import ai.aipr.server.integration.WebhookPayload;
import ai.aipr.server.integration.azuredevops.AzureDevOpsWebhookHandler;
import ai.aipr.server.integration.bitbucket.BitbucketWebhookHandler;
import ai.aipr.server.integration.github.GitHubWebhookHandler;
import ai.aipr.server.integration.gitlab.GitLabWebhookHandler;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.Map;

/**
 * Webhook endpoints for platform integrations.
 *
 * <p>Each platform handler is optional — only enabled when its corresponding
 * {@code aipr.auth.<platform>.enabled=true} property is set.</p>
 */
@RestController
@RequestMapping("/api/v1/webhooks")
public class WebhookController {

    private static final Logger log = LoggerFactory.getLogger(WebhookController.class);

    private final GitHubWebhookHandler gitHubHandler;
    private final GitLabWebhookHandler gitLabHandler;
    private final BitbucketWebhookHandler bitbucketHandler;
    @Nullable
    private final AzureDevOpsWebhookHandler azureDevOpsHandler;

    public WebhookController(
        GitHubWebhookHandler gitHubHandler,
        GitLabWebhookHandler gitLabHandler,
        BitbucketWebhookHandler bitbucketHandler,
        @NotNull ObjectProvider<AzureDevOpsWebhookHandler> azureDevOpsHandlerProvider
    ) {
        this.gitHubHandler = gitHubHandler;
        this.gitLabHandler = gitLabHandler;
        this.bitbucketHandler = bitbucketHandler;
        this.azureDevOpsHandler = azureDevOpsHandlerProvider.getIfAvailable();
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
     * Azure DevOps webhook endpoint.
     *
     * <p>Azure DevOps Service Hooks send the event type inside the JSON body
     * ({@code eventType} field), not as a header. An optional Basic Auth or
     * custom header carries the HMAC signature.</p>
     */
    @PostMapping("/azure-devops")
    public ResponseEntity<Map<String, Object>> handleAzureDevOps(
            @RequestHeader(value = "Authorization", required = false) String authHeader,
            @RequestBody String payload
    ) {
        log.info("Azure DevOps webhook received");

        if (azureDevOpsHandler == null) {
            return ResponseEntity.ok(Map.of(
                    "status", "skipped",
                    "reason", "Azure DevOps integration not enabled"
            ));
        }

        WebhookPayload webhookPayload = new WebhookPayload(
                "azure-devops", "service_hook", payload, authHeader, null
        );

        try {
            var result = azureDevOpsHandler.handle(webhookPayload);
            return ResponseEntity.ok(Map.of(
                    "status", "accepted",
                    "action", result.action()
            ));
        } catch (SecurityException e) {
            log.warn("Azure DevOps webhook signature verification failed: {}", e.getMessage());
            return ResponseEntity.status(401).body(Map.of("error", "Invalid signature"));
        } catch (PlatformApiException e) {
            log.error("{} API error during webhook processing: status={}, body={}",
                    e.getPlatform(), e.getStatusCode(), e.getResponseBody());
            return ResponseEntity.status(502).body(Map.of("error", e.getMessage()));
        } catch (Exception e) {
            log.error("Azure DevOps webhook processing failed", e);
            return ResponseEntity.internalServerError().body(Map.of("error", e.getMessage()));
        }
    }
}
