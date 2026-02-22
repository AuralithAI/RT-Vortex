package ai.aipr.server.integration.bitbucket;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.integration.WebhookPayload;
import ai.aipr.server.integration.WebhookResult;
import ai.aipr.server.service.ReviewService;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;

import java.util.List;
import java.util.Set;

/**
 * Handler for Bitbucket Cloud webhook events.
 */
@Component
public class BitbucketWebhookHandler {

    private static final Logger log = LoggerFactory.getLogger(BitbucketWebhookHandler.class);
    private static final Set<String> SUPPORTED_EVENTS = Set.of(
            "pullrequest:created",
            "pullrequest:updated",
            "pullrequest:comment_created"
    );

    private final ReviewService reviewService;
    private final BitbucketPlatformClient bitbucketClient;
    private final ObjectMapper objectMapper;

    public BitbucketWebhookHandler(ReviewService reviewService,
                                   BitbucketPlatformClient bitbucketClient,
                                   ObjectMapper objectMapper) {
        this.reviewService = reviewService;
        this.bitbucketClient = bitbucketClient;
        this.objectMapper = objectMapper;
    }

    /**
     * Handle a Bitbucket webhook event.
     */
    public WebhookResult handle(@NotNull WebhookPayload payload) {
        String event = payload.event();
        if (!SUPPORTED_EVENTS.contains(event)) {
            return WebhookResult.skipped("Event not supported: " + event);
        }

        try {
            JsonNode body = objectMapper.readTree(payload.body());

            return switch (event) {
                case "pullrequest:created", "pullrequest:updated" -> handlePullRequest(body, event);
                case "pullrequest:comment_created" -> handleComment(body);
                default -> WebhookResult.skipped("Unhandled event: " + event);
            };

        } catch (Exception e) {
            log.error("Failed to handle Bitbucket webhook: event={}", event, e);
            return WebhookResult.error(e.getMessage());
        }
    }

    // =========================================================================
    // Event Handlers
    // =========================================================================

    private WebhookResult handlePullRequest(@NotNull JsonNode body, String event) {
        JsonNode pr = body.path("pullrequest");
        JsonNode repo = body.path("repository");

        if (pr.isMissingNode() || repo.isMissingNode()) {
            return WebhookResult.error("Missing pullrequest or repository in payload");
        }

        return triggerReview(pr, repo, event);
    }

    private WebhookResult handleComment(@NotNull JsonNode body) {
        JsonNode comment = body.path("comment");
        JsonNode pr = body.path("pullrequest");
        JsonNode repo = body.path("repository");

        if (comment.isMissingNode() || pr.isMissingNode() || repo.isMissingNode()) {
            return WebhookResult.error("Missing comment, pullrequest, or repository");
        }

        String commentBody = safeText(comment.path("content"), "raw").toLowerCase();
        if (!commentBody.contains("@aipr") && !commentBody.contains("/review")) {
            return WebhookResult.skipped("No command detected in comment");
        }

        log.info("Review command detected in Bitbucket comment: repo={}, pr={}",
                safeText(repo, "full_name"), pr.path("id").asInt(0));

        return triggerReview(pr, repo, "comment_command");
    }

    // =========================================================================
    // Shared Review Trigger
    // =========================================================================

    private WebhookResult triggerReview(@NotNull JsonNode pr, @NotNull JsonNode repo,
                                        String trigger)
    {
        // Bitbucket full_name = "workspace/repo_slug" — used directly as repoId
        String fullName = safeText(repo, "full_name");
        int prId = pr.path("id").asInt(0);
        String prTitle = safeText(pr, "title");
        String prDescription = pr.path("description").asText("");

        // Source/destination branches: source.branch.name / destination.branch.name
        String sourceBranch = safeText(pr.path("source").path("branch"), "name");
        String destBranch = safeText(pr.path("destination").path("branch"), "name");

        // Head commit: source.commit.hash
        String headCommit = safeText(pr.path("source").path("commit"), "hash");

        if (fullName.isEmpty() || prId <= 0 || !fullName.contains("/")) {
            return WebhookResult.error("Invalid repository or PR ID");
        }

        log.info("Triggering Bitbucket review: repo={}, pr={}, trigger={}, source={}, dest={}",
                fullName, prId, trigger, sourceBranch, destBranch);

        // Fetch diff via interface method
        String diff;
        try {
            diff = bitbucketClient.getDiff(fullName, prId);
        } catch (Exception e) {
            log.error("Failed to fetch PR diff: repo={}, pr={}", fullName, prId, e);
            return WebhookResult.error("Failed to fetch diff: " + e.getMessage());
        }

        if (diff == null || diff.isBlank()) {
            return WebhookResult.skipped("Empty diff — nothing to review");
        }

        // Fetch changed files (non-critical)
        List<String> changedFiles = fetchChangedFiles(fullName, prId);

        // Build review request
        var request = ReviewRequest.builder()
                .repoId(fullName)
                .prNumber(prId)
                .diff(diff)
                .prTitle(prTitle)
                .prDescription(prDescription)
                .baseBranch(destBranch)
                .headBranch(sourceBranch)
                .headCommit(headCommit)
                .changedFiles(changedFiles)
                .build();

        final String commitSha = headCommit;
        reviewService.reviewPullRequest(request)
                .thenAccept(response -> {
                    log.info("Bitbucket review completed: repo={}, pr={}, comments={}",
                            fullName, prId, response.comments().size());
                    try {
                        bitbucketClient.submitReview(fullName, prId, commitSha, response);
                    } catch (Exception e) {
                        log.error("Failed to submit Bitbucket review: repo={}, pr={}",
                                fullName, prId, e);
                        // Fallback: post summary as a general comment
                        bitbucketClient.postComment(fullName, prId,
                                ":warning: Review completed but inline comments failed.\n\n"
                                        + (response.summary() != null ? response.summary() : ""));
                    }
                })
                .exceptionally(e -> {
                    log.error("Failed to complete review for Bitbucket {} PR #{}",
                            fullName, prId, e);
                    return null;
                });

        return WebhookResult.success("review_started");
    }

    private List<String> fetchChangedFiles(String repoId, int prId) {
        try {
            return bitbucketClient.getChangedFiles(repoId, prId);
        } catch (Exception e) {
            log.warn("Failed to fetch changed files for {} #{}: {}", repoId, prId, e.getMessage());
            return List.of();
        }
    }

    // =========================================================================
    // JSON Utility
    // =========================================================================

    private String safeText(JsonNode node, String field)
    {
        if (node == null || node.isMissingNode()) return "";
        JsonNode child = node.path(field);
        return child.isMissingNode() || child.isNull() ? "" : child.asText("");
    }
}



