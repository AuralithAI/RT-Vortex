package ai.aipr.server.integration.gitlab;

import ai.aipr.server.integration.WebhookPayload;
import ai.aipr.server.integration.WebhookResult;
import ai.aipr.server.service.ReviewService;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

/**
 * Handler for GitLab webhooks.
 */
@Component
public class GitLabWebhookHandler {

    private static final Logger log = LoggerFactory.getLogger(GitLabWebhookHandler.class);

    @Value("${aipr.integrations.gitlab.webhook-secret:}")
    private String webhookSecret;

    @Value("${aipr.integrations.gitlab.token:}")
    private String gitlabToken;

    private final ReviewService reviewService;
    private final ObjectMapper objectMapper;

    public GitLabWebhookHandler(ReviewService reviewService, ObjectMapper objectMapper) {
        this.reviewService = reviewService;
        this.objectMapper = objectMapper;
    }

    /**
     * Handle a GitLab webhook.
     */
    public WebhookResult handle(WebhookPayload payload) {
        // Verify token
        if (!webhookSecret.isBlank() && !webhookSecret.equals(payload.signature())) {
            throw new SecurityException("Invalid webhook token");
        }

        try {
            JsonNode body = objectMapper.readTree(payload.body());
            String eventType = payload.event();

            return switch (eventType) {
                case "Merge Request Hook" -> handleMergeRequest(body);
                case "Note Hook" -> handleNote(body);
                default -> WebhookResult.skipped("Event not supported: " + eventType);
            };

        } catch (Exception e) {
            log.error("Failed to handle GitLab webhook", e);
            return WebhookResult.error(e.getMessage());
        }
    }

    private WebhookResult handleMergeRequest(JsonNode body) {
        JsonNode mr = body.get("object_attributes");
        String action = mr.get("action").asText();

        if (!"open".equals(action) && !"reopen".equals(action) && !"update".equals(action)) {
            return WebhookResult.skipped("MR action not reviewable: " + action);
        }

        JsonNode project = body.get("project");
        String projectId = project.get("id").asText();
        int mrIid = mr.get("iid").asInt();
        
        log.info("Processing MR: project={}, mr={}, action={}", projectId, mrIid, action);

        // TODO: Implement GitLab MR review
        // 1. Fetch diff from GitLab API
        // 2. Submit for review
        // 3. Post comments back

        return WebhookResult.skipped("GitLab MR review not yet implemented");
    }

    private WebhookResult handleNote(JsonNode body) {
        // Handle comment commands
        JsonNode note = body.get("object_attributes");
        String noteBody = note.get("note").asText();

        if (!noteBody.toLowerCase().contains("@aipr") && 
            !noteBody.toLowerCase().contains("/review")) {
            return WebhookResult.skipped("No command detected");
        }

        return WebhookResult.skipped("GitLab comment commands not yet implemented");
    }
}
