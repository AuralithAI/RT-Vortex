package ai.aipr.server.integration.azuredevops;

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
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Component;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.List;
import java.util.Set;

/**
 * Handler for Azure DevOps webhook events.
 *
 * <p>Azure DevOps uses Service Hooks to send webhook notifications.
 * Supported events:</p>
 * <ul>
 *   <li>{@code git.pullrequest.created} — triggers AI review on PR creation</li>
 *   <li>{@code git.pullrequest.updated} — triggers AI review on PR update (new commits)</li>
 *   <li>{@code git.pullrequest.merged} — optional: cleanup/learning</li>
 * </ul>
 *
 * <p>Authentication: Azure DevOps supports HTTP Basic Auth or HMAC-SHA256 for webhooks.
 * We use HMAC-SHA256 for security.</p>
 *
 * @see <a href="https://docs.microsoft.com/en-us/azure/devops/service-hooks/services/webhooks">Azure DevOps Webhooks</a>
 */
@Component
@ConditionalOnProperty(name = "aipr.auth.azure-devops.enabled", havingValue = "true")
public class AzureDevOpsWebhookHandler {

    private static final Logger log = LoggerFactory.getLogger(AzureDevOpsWebhookHandler.class);

    private static final Set<String> SUPPORTED_EVENTS = Set.of(
            "git.pullrequest.created",
            "git.pullrequest.updated",
            "git.pullrequest.merged"
    );

    private static final Set<String> REVIEWABLE_STATUSES = Set.of(
            "active"
    );

    @Value("${aipr.auth.azure-devops.webhook-secret:}")
    private String webhookSecret;

    @Value("${aipr.auth.azure-devops.organization:}")
    private String defaultOrganization;

    private final ReviewService reviewService;
    private final AzureDevOpsPlatformClient azureClient;
    private final ObjectMapper objectMapper;

    public AzureDevOpsWebhookHandler(
            ReviewService reviewService,
            AzureDevOpsPlatformClient azureClient,
            ObjectMapper objectMapper
    ) {
        this.reviewService = reviewService;
        this.azureClient = azureClient;
        this.objectMapper = objectMapper;
    }

    /**
     * Handle an Azure DevOps webhook event.
     *
     * @param payload the webhook payload including event type, body, and signature
     * @return result describing what action was taken
     */
    public WebhookResult handle(WebhookPayload payload) {
        // Verify HMAC-SHA256 signature if webhook secret is configured
        if (webhookSecret != null && !webhookSecret.isBlank()) {
            if (!verifySignature(payload.body(), payload.authToken())) {
                log.warn("Invalid Azure DevOps webhook signature");
                throw new SecurityException("Invalid webhook signature");
            }
        }

        try {
            JsonNode body = objectMapper.readTree(payload.body());

            // Azure DevOps event type is in eventType field
            String eventType = body.path("eventType").asText("");

            if (!SUPPORTED_EVENTS.contains(eventType)) {
                return WebhookResult.skipped("Event not supported: " + eventType);
            }

            return switch (eventType) {
                case "git.pullrequest.created" -> handlePullRequestCreated(body);
                case "git.pullrequest.updated" -> handlePullRequestUpdated(body);
                case "git.pullrequest.merged" -> handlePullRequestMerged(body);
                default -> WebhookResult.skipped("Unhandled event: " + eventType);
            };

        } catch (Exception e) {
            log.error("Failed to handle Azure DevOps webhook", e);
            return WebhookResult.error(e.getMessage());
        }
    }

    // =========================================================================
    // Event Handlers
    // =========================================================================

    private WebhookResult handlePullRequestCreated(@NotNull JsonNode body) {
        JsonNode resource = body.path("resource");

        String status = resource.path("status").asText("");
        if (!REVIEWABLE_STATUSES.contains(status)) {
            return WebhookResult.skipped("PR status not reviewable: " + status);
        }

        return triggerReview(resource, "created");
    }

    private WebhookResult handlePullRequestUpdated(@NotNull JsonNode body) {
        JsonNode resource = body.path("resource");

        // Check if this is a commit update (not just metadata change)
        JsonNode revisedResource = body.path("resource").path("revisedResource");
        if (revisedResource.isMissingNode()) {
            revisedResource = resource;
        }

        String status = revisedResource.path("status").asText("");
        if (!REVIEWABLE_STATUSES.contains(status)) {
            return WebhookResult.skipped("PR status not reviewable: " + status);
        }

        // Check if source commit changed
        String currentCommit = resource.path("lastMergeSourceCommit").path("commitId").asText("");
        String previousCommit = body.path("resourceBefore")
                .path("lastMergeSourceCommit").path("commitId").asText("");

        if (!currentCommit.isEmpty() && currentCommit.equals(previousCommit)) {
            return WebhookResult.skipped("No new commits, skipping re-review");
        }

        return triggerReview(revisedResource, "updated");
    }

    @NotNull
    private WebhookResult handlePullRequestMerged(@NotNull JsonNode body) {
        JsonNode resource = body.path("resource");

        int prId = resource.path("pullRequestId").asInt();
        String repoId = extractRepoId(resource);

        log.info("PR merged: {} #{}", repoId, prId);

        // Could trigger learning here
        // reviewService.learnFromMerge(repoId, prId);

        return WebhookResult.skipped("PR merged, no review needed");
    }

    // =========================================================================
    // Helper Methods
    // =========================================================================

    private WebhookResult triggerReview(@NotNull JsonNode resource, String action) {
        int prId = resource.path("pullRequestId").asInt();
        String repoId = extractRepoId(resource);
        String commitSha = resource.path("lastMergeSourceCommit").path("commitId").asText("");
        String title = resource.path("title").asText("");
        String description = resource.path("description").asText("");

        // Extract source and target branches
        String sourceBranch = resource.path("sourceRefName").asText("")
                .replace("refs/heads/", "");
        String targetBranch = resource.path("targetRefName").asText("")
                .replace("refs/heads/", "");

        log.info("Triggering review for Azure DevOps PR: {} #{} [{}] - {}",
                repoId, prId, action, title);

        // Fetch the diff via platform client
        String diff;
        try {
            diff = azureClient.getDiff(repoId, prId);
        } catch (Exception e) {
            log.error("Failed to fetch diff for Azure DevOps PR #{}", prId, e);
            return WebhookResult.error("Failed to fetch diff: " + e.getMessage());
        }

        if (diff.isBlank()) {
            return WebhookResult.skipped("Empty diff — nothing to review");
        }

        // Fetch changed files
        List<String> changedFiles;
        try {
            changedFiles = azureClient.getChangedFiles(repoId, prId);
        } catch (Exception e) {
            log.warn("Failed to fetch changed files for PR #{}: {}", prId, e.getMessage());
            changedFiles = List.of();
        }

        ReviewRequest request = ReviewRequest.builder()
                .repoId(repoId)
                .prNumber(prId)
                .diff(diff)
                .prTitle(title)
                .prDescription(description)
                .baseBranch(targetBranch)
                .headBranch(sourceBranch)
                .headCommit(commitSha)
                .changedFiles(changedFiles)
                .build();

        reviewService.reviewPullRequest(request)
                .thenAccept(response -> {
                    log.info("Review completed for Azure DevOps PR #{}, comments={}",
                            prId, response.comments().size());
                    try {
                        azureClient.submitReview(repoId, prId, commitSha, response);
                    } catch (Exception e) {
                        log.error("Failed to submit review to Azure DevOps PR #{}", prId, e);
                    }
                })
                .exceptionally(e -> {
                    log.error("Review failed for Azure DevOps PR #{}", prId, e);
                    return null;
                });

        return WebhookResult.success("review_started");
    }

    @NotNull
    private String extractRepoId(@NotNull JsonNode resource) {
        JsonNode repository = resource.path("repository");

        // Azure DevOps repo ID format: organization/project/repo
        String organization = repository.path("project").path("name").asText("");
        String project = organization;  // In Azure DevOps, project is often the same level
        String repoName = repository.path("name").asText("");

        // Try to get organization from different locations
        if (organization.isEmpty()) {
            // Check resourceContainers
            organization = defaultOrganization;
        }

        // Get the URL to extract organization if needed
        String remoteUrl = repository.path("remoteUrl").asText("");
        if (!remoteUrl.isEmpty() && organization.isEmpty()) {
            // Parse: https://dev.azure.com/{org}/{project}/_git/{repo}
            // or: https://{org}.visualstudio.com/{project}/_git/{repo}
            if (remoteUrl.contains("dev.azure.com")) {
                String[] parts = remoteUrl.split("/");
                for (int i = 0; i < parts.length - 1; i++) {
                    if ("dev.azure.com".equals(parts[i])) {
                        organization = parts[i + 1];
                        break;
                    }
                }
            } else if (remoteUrl.contains(".visualstudio.com")) {
                String host = remoteUrl.split("//")[1].split("/")[0];
                organization = host.replace(".visualstudio.com", "");
            }
        }

        // Extract project from URL
        if (project.isEmpty() && !remoteUrl.isEmpty()) {
            String[] parts = remoteUrl.split("/");
            for (int i = 0; i < parts.length; i++) {
                if ("_git".equals(parts[i]) && i > 0) {
                    project = parts[i - 1];
                    break;
                }
            }
        }

        return organization + "/" + project + "/" + repoName;
    }

    /**
     * Verify HMAC-SHA256 signature from Azure DevOps.
     *
     * <p>Azure DevOps sends the signature in the Authorization header as:
     * {@code Basic base64(user:signature)}</p>
     *
     * <p>Or in custom header X-Azure-Signature (depending on configuration).</p>
     */
    private boolean verifySignature(@NotNull String payload, String providedSignature) {
        if (providedSignature == null || providedSignature.isBlank()) {
            return false;
        }

        try {
            String signature = providedSignature;

            // Handle Basic auth format
            if (providedSignature.startsWith("Basic ")) {
                String decoded = new String(Base64.getDecoder().decode(
                        providedSignature.substring(6)), StandardCharsets.UTF_8);
                // Format: username:signature
                int colonIndex = decoded.indexOf(':');
                if (colonIndex > 0) {
                    signature = decoded.substring(colonIndex + 1);
                }
            }

            // Calculate expected signature
            Mac hmac = Mac.getInstance("HmacSHA256");
            SecretKeySpec keySpec = new SecretKeySpec(
                    webhookSecret.getBytes(StandardCharsets.UTF_8), "HmacSHA256");
            hmac.init(keySpec);
            byte[] hash = hmac.doFinal(payload.getBytes(StandardCharsets.UTF_8));

            String expected = Base64.getEncoder().encodeToString(hash);

            // Constant-time comparison
            return constantTimeEquals(expected.getBytes(), signature.getBytes());

        } catch (Exception e) {
            log.error("Failed to verify Azure DevOps webhook signature", e);
            return false;
        }
    }

    private boolean constantTimeEquals(@NotNull byte[] a, @NotNull byte[] b) {
        if (a.length != b.length) {
            return false;
        }
        int result = 0;
        for (int i = 0; i < a.length; i++) {
            result |= a[i] ^ b[i];
        }
        return result == 0;
    }
}
