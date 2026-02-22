package ai.aipr.server.integration.gitlab;

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

import java.util.List;
import java.util.Set;

/**
 * Handler for GitLab webhook events.
 *
 * <p>Supported events:</p>
 * <ul>
 *   <li>{@code Merge Request Hook} — triggers AI review on MR open/reopen/update</li>
 *   <li>{@code Note Hook} — triggers review when {@code @aipr review} or {@code /review}
 *       is commented on an MR</li>
 * </ul>
 *
 * <p>GitLab authenticates webhooks via a static secret token sent in the
 * {@code X-Gitlab-Token} header — no HMAC signature needed.</p>
 */
@Component
public class GitLabWebhookHandler {

    private static final Logger log = LoggerFactory.getLogger(GitLabWebhookHandler.class);
    private static final Set<String> REVIEWABLE_MR_ACTIONS = Set.of(
            "open", "reopen", "update"
    );

    @Value("${aipr.auth.gitlab.webhook-secret:}")
    private String webhookSecret;

    private final ReviewService reviewService;
    private final GitLabPlatformClient gitLabClient;
    private final ObjectMapper objectMapper;

    public GitLabWebhookHandler(ReviewService reviewService,
                                GitLabPlatformClient gitLabClient,
                                ObjectMapper objectMapper) {
        this.reviewService = reviewService;
        this.gitLabClient = gitLabClient;
        this.objectMapper = objectMapper;
    }

    /**
     * Handle a GitLab webhook event.
     */
    public WebhookResult handle(WebhookPayload payload) {
        // GitLab sends the secret as a plain token — compare directly
        if (webhookSecret != null && !webhookSecret.isBlank()) {
            if (!webhookSecret.equals(payload.authToken())) {
                throw new SecurityException("Invalid GitLab webhook token");
            }
        }

        try {
            JsonNode body = objectMapper.readTree(payload.body());
            String eventType = payload.event();

            return switch (eventType) {
                case "Merge Request Hook" -> handleMergeRequest(body);
                case "Note Hook" -> handleNote(body);
                default -> WebhookResult.skipped("Event not supported: " + eventType);
            };

        } catch (SecurityException e) {
            throw e; // re-throw so the controller returns 401
        } catch (Exception e) {
            log.error("Failed to handle GitLab webhook: event={}", payload.event(), e);
            return WebhookResult.error(e.getMessage());
        }
    }

    // =========================================================================
    // Event Handlers
    // =========================================================================

    /**
     * Handle Merge Request Hook events.
     */
    private WebhookResult handleMergeRequest(@NotNull JsonNode body) {
        JsonNode mr = body.path("object_attributes");
        if (mr.isMissingNode()) {
            return WebhookResult.error("Missing object_attributes in MR webhook");
        }

        String action = safeText(mr, "action");
        if (!REVIEWABLE_MR_ACTIONS.contains(action)) {
            return WebhookResult.skipped("MR action not reviewable: " + action);
        }

        JsonNode project = body.path("project");
        if (project.isMissingNode()) {
            return WebhookResult.error("Missing project in MR webhook");
        }

        return triggerReview(mr, project, action);
    }

    /**
     * Handle Note Hook events (comments on MRs).
     * Only triggers when the comment is on a merge request and contains a command.
     */
    private WebhookResult handleNote(@NotNull JsonNode body) {
        // Only handle MR notes
        String noteableType = safeText(body.path("object_attributes"), "noteable_type");
        if (!"MergeRequest".equals(noteableType)) {
            return WebhookResult.skipped("Note is not on a merge request");
        }

        JsonNode note = body.path("object_attributes");
        String noteBody = safeText(note, "note").toLowerCase();

        if (!noteBody.contains("@aipr") && !noteBody.contains("/review")) {
            return WebhookResult.skipped("No command detected in note");
        }

        JsonNode mr = body.path("merge_request");
        JsonNode project = body.path("project");

        if (mr.isMissingNode() || project.isMissingNode()) {
            return WebhookResult.error("Missing merge_request or project in note webhook");
        }

        log.info("Review command detected in GitLab note: project={}, mr={}",
                safeText(project, "path_with_namespace"), mr.path("iid").asInt(0));

        return triggerReview(mr, project, "comment_command");
    }

    // =========================================================================
    // Shared Review Trigger
    // =========================================================================

    private WebhookResult triggerReview(@NotNull JsonNode mr, @NotNull JsonNode project,
                                        String trigger) {
        String projectPath = safeText(project, "path_with_namespace");
        int mrIid = mr.path("iid").asInt(0);
        String mrTitle = safeText(mr, "title");
        String mrDescription = mr.path("description").asText("");
        String sourceBranch = safeText(mr, "source_branch");
        String targetBranch = safeText(mr, "target_branch");
        String headSha = safeText(mr, "last_commit", "id"); // nested: last_commit.id

        // Fallback for head SHA — try direct field
        if (headSha.isEmpty()) {
            headSha = safeText(mr, "sha");
        }

        if (projectPath.isEmpty() || mrIid <= 0) {
            return WebhookResult.error("Invalid project or MR number");
        }

        log.info("Triggering GitLab review: project={}, mr={}, trigger={}, source={}, target={}",
                projectPath, mrIid, trigger, sourceBranch, targetBranch);

        // Fetch the diff
        String diff;
        try {
            diff = gitLabClient.getDiff(projectPath, mrIid);
        } catch (Exception e) {
            log.error("Failed to fetch MR diff: project={}, mr={}", projectPath, mrIid, e);
            return WebhookResult.error("Failed to fetch diff: " + e.getMessage());
        }

        if (diff == null || diff.isBlank()) {
            return WebhookResult.skipped("Empty diff — nothing to review");
        }

        // Fetch changed file paths
        List<String> changedFiles = fetchChangedFiles(projectPath, mrIid);

        // Build review request — use projectPath as repoId for GitLab
        var request = ReviewRequest.builder()
                .repoId(projectPath)
                .prNumber(mrIid)
                .diff(diff)
                .prTitle(mrTitle)
                .prDescription(mrDescription)
                .baseBranch(targetBranch)
                .headBranch(sourceBranch)
                .headCommit(headSha)
                .changedFiles(changedFiles)
                .build();

        final String commitSha = headSha;
        reviewService.reviewPullRequest(request)
                .thenAccept(response -> {
                    log.info("GitLab review completed: project={}, mr={}, comments={}",
                            projectPath, mrIid, response.comments().size());
                    try {
                        gitLabClient.submitReview(projectPath, mrIid, commitSha, response);
                    } catch (Exception e) {
                        log.error("Failed to submit GitLab review: project={}, mr={}",
                                projectPath, mrIid, e);
                        // Fallback: post summary as a single note
                        gitLabClient.postComment(projectPath, mrIid,
                                ":warning: Review completed but failed to post inline comments.\n\n"
                                        + (response.summary() != null ? response.summary() : ""));
                    }
                })
                .exceptionally(e -> {
                    log.error("Failed to complete review for GitLab {} MR !{}",
                            projectPath, mrIid, e);
                    return null;
                });

        return WebhookResult.success("review_started");
    }

    private List<String> fetchChangedFiles(String projectPath, int mrIid) {
        try {
            return gitLabClient.getChangedFiles(projectPath, mrIid);
        } catch (Exception e) {
            log.warn("Failed to fetch changed files for {}/!{}: {}",
                    projectPath, mrIid, e.getMessage());
            return List.of();
        }
    }

    // =========================================================================
    // JSON Utility
    // =========================================================================

    private String safeText(JsonNode node, String field) {
        if (node == null || node.isMissingNode()) return "";
        JsonNode child = node.path(field);
        return child.isMissingNode() || child.isNull() ? "" : child.asText("");
    }

    /** Safely read a nested text field: node.field1.field2 */
    private String safeText(JsonNode node, String field1, String field2) {
        if (node == null || node.isMissingNode()) return "";
        JsonNode child = node.path(field1);
        if (child.isMissingNode() || child.isNull()) return "";
        JsonNode grandChild = child.path(field2);
        return grandChild.isMissingNode() || grandChild.isNull() ? "" : grandChild.asText("");
    }
}
