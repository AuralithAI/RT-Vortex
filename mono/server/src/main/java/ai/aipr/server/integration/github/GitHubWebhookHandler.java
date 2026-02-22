package ai.aipr.server.integration.github;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.integration.WebhookPayload;
import ai.aipr.server.integration.WebhookResult;
import ai.aipr.server.service.ReviewService;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.util.HexFormat;
import java.util.List;
import java.util.Set;

/**
 * Handler for GitHub webhook events.
 *
 * <p>Supported events:</p>
 * <ul>
 *   <li>{@code pull_request} — triggers AI review on PR open/reopen/synchronize</li>
 *   <li>{@code pull_request_review} — triggers re-review when a review is dismissed</li>
 *   <li>{@code issue_comment} — triggers review when {@code @aipr review} or {@code /review} is commented</li>
 * </ul>
 */
@Component
public class GitHubWebhookHandler {

    private static final Logger log = LoggerFactory.getLogger(GitHubWebhookHandler.class);
    private static final Set<String> SUPPORTED_EVENTS = Set.of(
            "pull_request",
            "pull_request_review",
            "issue_comment"
    );
    private static final Set<String> REVIEWABLE_PR_ACTIONS = Set.of(
            "opened", "reopened", "synchronize"
    );

    @Value("${aipr.auth.github.webhook-secret:}")
    private String webhookSecret;

    private final ReviewService reviewService;
    private final GitHubPlatformClient gitHubClient;
    private final ObjectMapper objectMapper;

    public GitHubWebhookHandler(
            ReviewService reviewService,
            GitHubPlatformClient gitHubClient,
            ObjectMapper objectMapper
    ) {
        this.reviewService = reviewService;
        this.gitHubClient = gitHubClient;
        this.objectMapper = objectMapper;
    }

    /**
     * Handle a GitHub webhook event.
     *
     * @param payload the webhook payload including event type, body, and signature
     * @return result describing what action was taken
     */
    public WebhookResult handle(WebhookPayload payload) {
        // Verify signature if webhook secret is configured
        if (webhookSecret != null && !webhookSecret.isBlank()) {
            if (!verifySignature(payload.body(), payload.authToken())) {
                throw new SecurityException("Invalid webhook signature");
            }
        }

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
            log.error("Failed to handle GitHub webhook: event={}", event, e);
            return WebhookResult.error(e.getMessage());
        }
    }

    // =========================================================================
    // Event Handlers
    // =========================================================================

    /**
     * Handle pull_request events.
     * Triggers AI review on opened, reopened, or synchronize actions.
     */
    private WebhookResult handlePullRequest(JsonNode body) {
        String action = safeText(body, "action");

        if (!REVIEWABLE_PR_ACTIONS.contains(action)) {
            return WebhookResult.skipped("PR action not reviewable: " + action);
        }

        JsonNode pr = body.path("pull_request");
        JsonNode repo = body.path("repository");

        if (pr.isMissingNode() || repo.isMissingNode()) {
            return WebhookResult.error("Missing pull_request or repository in payload");
        }

        return triggerReview(pr, repo, action);
    }

    /**
     * Handle pull_request_review events.
     * Triggers re-review when a review is dismissed (reviewer requests fresh analysis).
     */
    private WebhookResult handlePullRequestReview(JsonNode body) {
        String action = safeText(body, "action");

        // Only re-review when a review is dismissed
        if (!"dismissed".equals(action)) {
            return WebhookResult.skipped("Review action not actionable: " + action);
        }

        JsonNode pr = body.path("pull_request");
        JsonNode repo = body.path("repository");

        if (pr.isMissingNode() || repo.isMissingNode()) {
            return WebhookResult.error("Missing pull_request or repository in payload");
        }

        log.info("Review dismissed, triggering re-review");
        return triggerReview(pr, repo, "dismissed_re_review");
    }

    /**
     * Handle issue_comment events.
     * Triggers review when a comment on a PR contains {@code @aipr review} or {@code /review}.
     */
    private WebhookResult handleIssueComment(JsonNode body) {
        String action = safeText(body, "action");
        if (!"created".equals(action)) {
            return WebhookResult.skipped("Comment action not handled: " + action);
        }

        // Verify this comment is on a pull request (not a regular issue)
        JsonNode issue = body.path("issue");
        if (issue.isMissingNode() || !issue.has("pull_request")) {
            return WebhookResult.skipped("Comment is not on a pull request");
        }

        JsonNode comment = body.path("comment");
        String commentBody = safeText(comment, "body").toLowerCase();

        // Check for command triggers
        if (!commentBody.contains("@aipr") && !commentBody.contains("/review")) {
            return WebhookResult.skipped("No command detected in comment");
        }

        // Extract repo and PR info
        JsonNode repo = body.path("repository");
        if (repo.isMissingNode()) {
            return WebhookResult.error("Missing repository in payload");
        }

        String repoId = safeText(repo, "full_name");
        int prNumber = issue.path("number").asInt(0);

        if (repoId.isEmpty() || prNumber <= 0) {
            return WebhookResult.error("Could not determine repo or PR number from comment");
        }

        log.info("Review command detected: repo={}, pr={}, user={}",
                repoId, prNumber, safeText(comment.path("user"), "login"));

        // Fetch PR info from GitHub API to get branch/commit details
        try {
            JsonNode prInfo = gitHubClient.getPullRequestInfo(repoId, prNumber);
            return triggerReview(prInfo, repo, "comment_command");
        } catch (Exception e) {
            log.error("Failed to fetch PR info for comment command: repo={}, pr={}",
                    repoId, prNumber, e);
            return WebhookResult.error("Failed to fetch PR info: " + e.getMessage());
        }
    }

    // =========================================================================
    // Shared Review Trigger
    // =========================================================================

    /**
     * Common method to trigger an AI review for a pull request.
     * Extracts all necessary data from the PR JSON and submits for async review.
     */
    private WebhookResult triggerReview(@NotNull JsonNode pr, JsonNode repo, String trigger) {
        String repoId = safeText(repo, "full_name");
        int prNumber = pr.path("number").asInt(0);
        String prTitle = safeText(pr, "title");
        String prBody = pr.path("body").asText("");  // PR body can be null
        String baseBranch = safeText(pr.path("base"), "ref");
        String headBranch = safeText(pr.path("head"), "ref");
        String headCommit = safeText(pr.path("head"), "sha");

        if (repoId.isEmpty() || prNumber <= 0) {
            return WebhookResult.error("Invalid repo or PR number");
        }

        log.info("Triggering review: repo={}, pr={}, trigger={}, base={}, head={}",
                repoId, prNumber, trigger, baseBranch, headBranch);

        // Fetch the unified diff from GitHub
        String diff;
        try {
            diff = gitHubClient.getDiff(repoId, prNumber);
        } catch (Exception e) {
            log.error("Failed to fetch diff: repo={}, pr={}", repoId, prNumber, e);
            return WebhookResult.error("Failed to fetch diff: " + e.getMessage());
        }

        if (diff == null || diff.isBlank()) {
            return WebhookResult.skipped("Empty diff — nothing to review");
        }

        // Fetch changed file paths to enrich the review request
        List<String> changedFiles = fetchChangedFilePaths(repoId, prNumber);

        // Submit for async review
        var request = ReviewRequest.builder()
                .repoId(repoId)
                .prNumber(prNumber)
                .diff(diff)
                .prTitle(prTitle)
                .prDescription(prBody)
                .baseBranch(baseBranch)
                .headBranch(headBranch)
                .headCommit(headCommit)
                .changedFiles(changedFiles)
                .build();

        final String commitSha = headCommit;
        reviewService.reviewPullRequest(request)
                .thenAccept(response -> {
                    log.info("Review completed: repo={}, pr={}, comments={}",
                            repoId, prNumber, response.comments().size());
                    try {
                        gitHubClient.submitReview(repoId, prNumber, commitSha, response);
                    } catch (Exception e) {
                        log.warn("Full review submission failed, posting comments individually", e);
                        postCommentsIndividually(repoId, prNumber, commitSha, response);
                    }
                })
                .exceptionally(e -> {
                    log.error("Failed to complete review for {} PR #{}",
                            repoId, prNumber, e);
                    return null;
                });

        return WebhookResult.success("review_started");
    }

    /**
     * Fetch the list of changed file paths from the GitHub API.
     * Returns an empty list if the fetch fails (non-critical enrichment).
     */
    @NotNull
    private List<String> fetchChangedFilePaths(String repoId, int prNumber) {
        try {
            List<String> paths = gitHubClient.getChangedFiles(repoId, prNumber);
            log.debug("Fetched {} changed files for {}/#{}", paths.size(), repoId, prNumber);
            return paths;
        } catch (Exception e) {
            log.warn("Failed to fetch changed files for {}/#{}: {}", repoId, prNumber, e.getMessage());
            return List.of();
        }
    }

    /**
     * Fallback: post review comments individually when full review submission fails.
     * Uses {@link GitHubClient#postLineComment} for each comment that has a valid file/line.
     */
    private void postCommentsIndividually(String repoId, int prNumber, String commitSha,
                                          @NotNull ai.aipr.server.dto.ReviewResponse response) {
        if (commitSha == null || commitSha.isEmpty()) {
            log.warn("Cannot post individual comments without a commit SHA");
            return;
        }
        for (var comment : response.comments()) {
            if (comment.filePath() != null && comment.line() > 0) {
                gitHubClient.postLineComment(repoId, prNumber, commitSha,
                        comment.filePath(), comment.line(),
                        formatFallbackComment(comment));
            }
        }
    }

    /**
     * Format a comment for individual posting (simpler than full review format).
     */
    @NotNull
    private String formatFallbackComment(@NotNull ai.aipr.server.dto.ReviewComment comment) {
        StringBuilder sb = new StringBuilder();
        String severity = comment.severity() != null ? comment.severity().toUpperCase() : "NOTE";
        sb.append("**").append(severity).append("**");
        if (comment.category() != null) {
            sb.append(" — ").append(comment.category());
        }
        sb.append("\n\n");
        if (comment.message() != null) {
            sb.append(comment.message());
        }
        if (comment.suggestion() != null && !comment.suggestion().isBlank()) {
            sb.append("\n\n💡 **Suggestion:** ").append(comment.suggestion());
        }
        return sb.toString();
    }

    // =========================================================================
    // Signature Verification
    // =========================================================================

    /**
     * Verify the GitHub webhook HMAC-SHA256 signature.
     * Uses constant-time comparison to prevent timing attacks.
     */
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

            // Constant-time comparison
            return java.security.MessageDigest.isEqual(
                    expected.getBytes(StandardCharsets.UTF_8),
                    signature.getBytes(StandardCharsets.UTF_8)
            );
        } catch (Exception e) {
            log.error("Failed to verify webhook signature", e);
            return false;
        }
    }

    // =========================================================================
    // JSON Utility — Null-safe accessors
    // =========================================================================

    /**
     * Safely get a text field from a JSON node, returning empty string if missing/null.
     */
    private String safeText(JsonNode node, String field) {
        if (node == null || node.isMissingNode()) {
            return "";
        }
        JsonNode child = node.path(field);
        return child.isMissingNode() || child.isNull() ? "" : child.asText("");
    }
}
