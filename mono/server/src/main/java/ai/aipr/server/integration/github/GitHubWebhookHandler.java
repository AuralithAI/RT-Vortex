package ai.aipr.server.integration.github;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.integration.WebhookPayload;
import ai.aipr.server.integration.WebhookResult;
import ai.aipr.server.service.ReviewService;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.util.HexFormat;
import java.util.Set;

/**
 * Handler for GitHub webhooks.
 */
@Component
public class GitHubWebhookHandler {

    private static final Logger log = LoggerFactory.getLogger(GitHubWebhookHandler.class);
    private static final Set<String> SUPPORTED_EVENTS = Set.of(
            "pull_request",
            "pull_request_review",
            "issue_comment"
    );

    @Value("${aipr.integrations.github.webhook-secret:}")
    private String webhookSecret;

    private final ReviewService reviewService;
    private final GitHubClient gitHubClient;
    private final ObjectMapper objectMapper;

    public GitHubWebhookHandler(
            ReviewService reviewService,
            GitHubClient gitHubClient,
            ObjectMapper objectMapper
    ) {
        this.reviewService = reviewService;
        this.gitHubClient = gitHubClient;
        this.objectMapper = objectMapper;
    }

    /**
     * Handle a GitHub webhook.
     */
    public WebhookResult handle(WebhookPayload payload) {
        // Verify signature
        if (!webhookSecret.isBlank()) {
            if (!verifySignature(payload.body(), payload.signature())) {
                throw new SecurityException("Invalid webhook signature");
            }
        }

        // Parse event
        String event = payload.event();
        if (!SUPPORTED_EVENTS.contains(event)) {
            return WebhookResult.skipped("Event not supported: " + event);
        }

        try {
            JsonNode body = objectMapper.readTree(payload.body());
            
            return switch (event) {
                case "pull_request" -> handlePullRequest(body);
                case "pull_request_review" -> handlePullRequestReview(body);
                case "issue_comment" -> handleIssueComment(body);
                default -> WebhookResult.skipped("Unhandled event: " + event);
            };
            
        } catch (Exception e) {
            log.error("Failed to handle GitHub webhook", e);
            return WebhookResult.error(e.getMessage());
        }
    }

    private WebhookResult handlePullRequest(JsonNode body) {
        String action = body.get("action").asText();
        
        // Only review on open, reopen, or synchronize
        if (!Set.of("opened", "reopened", "synchronize").contains(action)) {
            return WebhookResult.skipped("PR action not reviewable: " + action);
        }

        JsonNode pr = body.get("pull_request");
        JsonNode repo = body.get("repository");

        String repoId = repo.get("full_name").asText();
        int prNumber = pr.get("number").asInt();
        String prTitle = pr.get("title").asText();
        String prBody = pr.get("body").asText();
        String baseBranch = pr.get("base").get("ref").asText();
        String headBranch = pr.get("head").get("ref").asText();
        String headCommit = pr.get("head").get("sha").asText();

        log.info("Processing PR: repo={}, pr={}, action={}", repoId, prNumber, action);

        // Get diff
        String diff = gitHubClient.getPullRequestDiff(repoId, prNumber);

        // Submit for review
        var request = ReviewRequest.builder()
                .repoId(repoId)
                .prNumber(prNumber)
                .diff(diff)
                .prTitle(prTitle)
                .prDescription(prBody)
                .baseBranch(baseBranch)
                .headBranch(headBranch)
                .headCommit(headCommit)
                .build();

        reviewService.reviewPullRequest(request)
                .thenAccept(response -> {
                    // Post review comments back to GitHub
                    gitHubClient.submitReview(repoId, prNumber, response);
                })
                .exceptionally(e -> {
                    log.error("Failed to complete review for PR #{}", prNumber, e);
                    return null;
                });

        return WebhookResult.success("review_started");
    }

    private WebhookResult handlePullRequestReview(JsonNode body) {
        // Handle review events (e.g., request re-review)
        return WebhookResult.skipped("Review events not yet implemented");
    }

    private WebhookResult handleIssueComment(JsonNode body) {
        // Handle comment commands (e.g., "@aipr review")
        String action = body.get("action").asText();
        if (!"created".equals(action)) {
            return WebhookResult.skipped("Comment action not handled: " + action);
        }

        JsonNode comment = body.get("comment");
        String commentBody = comment.get("body").asText();

        // Check for command trigger
        if (!commentBody.toLowerCase().contains("@aipr") &&
            !commentBody.toLowerCase().contains("/review")) {
            return WebhookResult.skipped("No command detected");
        }

        // TODO: Trigger re-review based on command
        return WebhookResult.skipped("Comment commands not yet implemented");
    }

    private boolean verifySignature(String payload, String signature) {
        if (signature == null || !signature.startsWith("sha256=")) {
            return false;
        }

        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            SecretKeySpec spec = new SecretKeySpec(
                    webhookSecret.getBytes(StandardCharsets.UTF_8),
                    "HmacSHA256"
            );
            mac.init(spec);
            byte[] hash = mac.doFinal(payload.getBytes(StandardCharsets.UTF_8));
            String expected = "sha256=" + HexFormat.of().formatHex(hash);
            return expected.equalsIgnoreCase(signature);
        } catch (Exception e) {
            log.error("Failed to verify signature", e);
            return false;
        }
    }
}
