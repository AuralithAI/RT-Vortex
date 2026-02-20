package ai.aipr.server.api;

import ai.aipr.server.integration.WebhookPayload;
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
    
    public WebhookController(
            GitHubWebhookHandler gitHubHandler,
            GitLabWebhookHandler gitLabHandler
    ) {
        this.gitHubHandler = gitHubHandler;
        this.gitLabHandler = gitLabHandler;
    }
    
    /**
     * GitHub webhook endpoint.
     */
    @PostMapping("/github")
    public ResponseEntity<Map<String, Object>> handleGitHub(
            @RequestHeader("X-GitHub-Event") String event,
            @RequestHeader("X-Hub-Signature-256") String signature,
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
            @RequestHeader("X-Gitlab-Token") String token,
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
        } catch (Exception e) {
            log.error("GitLab webhook processing failed", e);
            return ResponseEntity.internalServerError().body(Map.of("error", e.getMessage()));
        }
    }
    
    /**
     * Bitbucket webhook endpoint.
     */
    @PostMapping("/bitbucket")
    public ResponseEntity<Map<String, Object>> handleBitbucket(
            @RequestHeader("X-Event-Key") String event,
            @RequestBody String payload
    ) {
        log.info("Bitbucket webhook received: event={}", event);
        // TODO: Implement Bitbucket handler
        return ResponseEntity.ok(Map.of("status", "not_implemented"));
    }
    
    /**
     * Azure DevOps webhook endpoint.
     */
    @PostMapping("/azure-devops")
    public ResponseEntity<Map<String, Object>> handleAzureDevOps(
            @RequestBody String payload
    ) {
        log.info("Azure DevOps webhook received");
        // TODO: Implement Azure DevOps handler
        return ResponseEntity.ok(Map.of("status", "not_implemented"));
    }
}
