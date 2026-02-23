package ai.aipr.server.integration.azuredevops;

import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.integration.AbstractVcsPlatformClient;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.io.IOException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.nio.charset.StandardCharsets;

/**
 * Azure DevOps platform client for PR operations.
 *
 * <p>Supports two authentication methods:</p>
 * <ul>
 *   <li><b>PAT (Personal Access Token)</b> — Recommended for most scenarios</li>
 *   <li><b>Azure AD (Entra ID)</b> — For enterprise SSO with service principals</li>
 * </ul>
 *
 * <p>API Reference: <a href="https://docs.microsoft.com/en-us/rest/api/azure/devops/">
 * Azure DevOps REST API</a></p>
 *
 * <p>The {@code repoId} format for Azure DevOps is: {@code organization/project/repository}</p>
 *
 * @see <a href="https://docs.microsoft.com/en-us/azure/devops/integrate/concepts/rate-limits">Rate Limits</a>
 */
@Component
@ConditionalOnProperty(name = "aipr.auth.azure-devops.enabled", havingValue = "true")
public class AzureDevOpsPlatformClient extends AbstractVcsPlatformClient {

    private static final Logger log = LoggerFactory.getLogger(AzureDevOpsPlatformClient.class);
    private static final MediaType JSON = MediaType.parse("application/json");
    private static final String API_VERSION = "7.1";

    @Value("${aipr.auth.azure-devops.organization:}")
    private String organization;

    @Value("${aipr.auth.azure-devops.pat:}")
    private String personalAccessToken;

    @Value("${aipr.auth.azure-devops.use-azure-ad:false}")
    private boolean useAzureAd;

    @Value("${aipr.auth.azure-devops.tenant-id:}")
    private String tenantId;

    @Value("${aipr.auth.azure-devops.client-id:}")
    private String clientId;

    @Value("${aipr.auth.azure-devops.client-secret:}")
    private String clientSecret;

    @Value("${aipr.auth.azure-devops.base-url:https://dev.azure.com}")
    private String baseUrl;

    @Value("${aipr.auth.azure-devops.vssps-url:https://app.vssps.visualstudio.com}")
    private String vsspsUrl;

    @Value("${aipr.auth.azure-devops.login-url:https://login.microsoftonline.com}")
    private String azureLoginUrl;

    private OkHttpClient httpClient;
    private ObjectMapper objectMapper;
    private String cachedAzureAdToken;
    private long tokenExpiry;

    @PostConstruct
    public void init() {
        httpClient = new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(30))
                .readTimeout(Duration.ofSeconds(60))
                .writeTimeout(Duration.ofSeconds(30))
                .build();

        objectMapper = new ObjectMapper();

        log.info("Azure DevOps client initialized: org={}, baseUrl={}, useAzureAD={}",
                organization, baseUrl, useAzureAd);
    }

    @Override
    public @NotNull String platform() {
        return "azure-devops";
    }

    @Override
    public @NotNull String getDiff(String repoId, int prNumber) {
        RepoPath path = parseRepoId(repoId);

        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/pullRequests/" + prNumber + "/iterations");

        try {
            // Get iterations to find the diff
            JsonNode iterations = get(url);

            if (!iterations.path("value").isArray() || iterations.path("value").isEmpty()) {
                throw new RuntimeException("No iterations found for PR #" + prNumber);
            }

            // Get the latest iteration
            int latestIteration = 0;
            for (JsonNode iteration : iterations.path("value")) {
                int id = iteration.path("id").asInt();
                if (id > latestIteration) {
                    latestIteration = id;
                }
            }

            // Get diff for the iteration
            String changesUrl = buildApiUrl(path, "/git/repositories/" + path.repository +
                    "/pullRequests/" + prNumber + "/iterations/" + latestIteration + "/changes");

            JsonNode changes = get(changesUrl);

            // Build unified diff from changes
            StringBuilder diffBuilder = new StringBuilder();

            for (JsonNode change : changes.path("changeEntries")) {
                String changeType = change.path("changeType").asText();
                String itemPath = change.path("item").path("path").asText();

                diffBuilder.append("diff --git a").append(itemPath)
                        .append(" b").append(itemPath).append("\n");

                if ("edit".equals(changeType) || "add".equals(changeType)) {
                    // Fetch file content diff
                    String contentUrl = buildApiUrl(path, "/git/repositories/" + path.repository +
                            "/diffs/commits?baseVersion=" + change.path("item").path("originalObjectId").asText() +
                            "&targetVersion=" + change.path("item").path("objectId").asText());

                    try {
                        JsonNode contentDiff = get(contentUrl);
                        diffBuilder.append(contentDiff.path("diff").asText("")).append("\n");
                    } catch (Exception e) {
                        // If we can't get the detailed diff, add a placeholder
                        diffBuilder.append("+++ Modified: ").append(itemPath).append("\n");
                    }
                } else if ("delete".equals(changeType)) {
                    diffBuilder.append("deleted file\n");
                }
            }

            return diffBuilder.toString();

        } catch (IOException e) {
            throw new RuntimeException("Failed to get diff for Azure DevOps PR #" + prNumber, e);
        }
    }

    @Override
    public @NotNull List<String> getChangedFiles(String repoId, int prNumber) {
        RepoPath path = parseRepoId(repoId);

        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/pullRequests/" + prNumber + "/iterations");

        try {
            JsonNode iterations = get(url);
            List<String> changedFiles = new ArrayList<>();

            if (!iterations.path("value").isArray() || iterations.path("value").isEmpty()) {
                return changedFiles;
            }

            // Get the latest iteration
            int latestIteration = 0;
            for (JsonNode iteration : iterations.path("value")) {
                int id = iteration.path("id").asInt();
                if (id > latestIteration) {
                    latestIteration = id;
                }
            }

            // Get changes for the iteration
            String changesUrl = buildApiUrl(path, "/git/repositories/" + path.repository +
                    "/pullRequests/" + prNumber + "/iterations/" + latestIteration + "/changes");

            JsonNode changes = get(changesUrl);

            for (JsonNode change : changes.path("changeEntries")) {
                String filePath = change.path("item").path("path").asText();
                if (!filePath.isEmpty()) {
                    // Remove leading slash
                    changedFiles.add(filePath.startsWith("/") ? filePath.substring(1) : filePath);
                }
            }

            return changedFiles;

        } catch (IOException e) {
            throw new RuntimeException("Failed to get changed files for Azure DevOps PR #" + prNumber, e);
        }
    }

    @Override
    public void submitReview(String repoId, int prNumber, String commitSha,
                             @NotNull ReviewResponse review) {
        RepoPath path = parseRepoId(repoId);

        try {
            // Post overall review comment
            String reviewSummary = buildReviewSummary(review);
            postComment(repoId, prNumber, reviewSummary);

            // Post inline comments as code review threads
            for (ReviewComment comment : review.comments()) {
                if (comment.filePath() != null && comment.line() > 0) {
                    postThreadComment(path, prNumber, commitSha, comment);
                }
            }

            // Set vote if we have a recommendation
            if (review.overallAssessment() != null) {
                setVote(path, prNumber, review.overallAssessment());
            }

            log.info("Submitted review to Azure DevOps PR {}/#{}", repoId, prNumber);

        } catch (IOException e) {
            throw new RuntimeException("Failed to submit review to Azure DevOps PR #" + prNumber, e);
        }
    }

    @Override
    public void postComment(String repoId, int prNumber, String body) {
        RepoPath path = parseRepoId(repoId);

        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/pullRequests/" + prNumber + "/threads");

        try {
            ObjectNode threadNode = objectMapper.createObjectNode();
            ArrayNode commentsArray = threadNode.putArray("comments");
            ObjectNode commentNode = commentsArray.addObject();
            commentNode.put("content", body);
            commentNode.put("commentType", 1); // Text comment

            threadNode.put("status", 1); // Active

            post(url, threadNode.toString());

        } catch (IOException e) {
            throw new RuntimeException("Failed to post comment to Azure DevOps PR #" + prNumber, e);
        }
    }

    // =========================================================================
    // Azure DevOps Specific Methods
    // =========================================================================

    /**
     * Post a code review thread (inline comment) on specific file and line.
     */
    private void postThreadComment(RepoPath path, int prNumber, String commitSha,
                                   @NotNull ReviewComment comment) throws IOException {
        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/pullRequests/" + prNumber + "/threads");

        ObjectNode threadNode = objectMapper.createObjectNode();

        // Thread context for line positioning
        ObjectNode threadContext = threadNode.putObject("threadContext");
        threadContext.put("filePath", "/" + comment.filePath());

        // Anchor to source commit for iteration tracking
        if (commitSha != null && !commitSha.isEmpty()) {
            ObjectNode changeTrackingContext = threadContext.putObject("changeTrackingContext");
            changeTrackingContext.put("changeTrackingId",
                    commitSha.substring(0, Math.min(8, commitSha.length())));
        }

        ObjectNode rightFileStart = threadContext.putObject("rightFileStart");
        rightFileStart.put("line", comment.line());
        rightFileStart.put("offset", 1);

        ObjectNode rightFileEnd = threadContext.putObject("rightFileEnd");
        rightFileEnd.put("line", comment.line());
        rightFileEnd.put("offset", 200); // End of line

        // Comment content
        ArrayNode commentsArray = threadNode.putArray("comments");
        ObjectNode commentNode = commentsArray.addObject();

        // Format: [SEVERITY] message
        String formattedBody = formatCommentBody(comment);
        commentNode.put("content", formattedBody);
        commentNode.put("commentType", 1); // Text comment

        // Set thread status based on severity
        // 1 = active, 2 = fixed, 3 = won't fix, 4 = closed, 5 = by design, 6 = pending
        threadNode.put("status", 1);

        post(url, threadNode.toString());
    }

    /**
     * Set the reviewer's vote on a PR.
     *
     * <p>Vote values:</p>
     * <ul>
     *   <li>-10 = Reject</li>
     *   <li>-5 = Waiting for author</li>
     *   <li>0 = No vote</li>
     *   <li>5 = Approved with suggestions</li>
     *   <li>10 = Approved</li>
     * </ul>
     */
    private void setVote(RepoPath path, int prNumber, @NotNull String approvalStatus) throws IOException {
        int vote = switch (approvalStatus.toLowerCase()) {
            case "approved", "approve" -> 10;
            case "approved_with_suggestions", "suggest" -> 5;
            case "request_changes", "reject" -> -5;
            case "blocked" -> -10;
            default -> 0;
        };

        // Get current user ID
        String meUrl = vsspsUrl + "/_apis/profile/profiles/me?api-version=6.0";
        JsonNode profile = get(meUrl);
        String reviewerId = profile.path("id").asText();

        if (reviewerId.isEmpty()) {
            log.warn("Could not determine reviewer ID, skipping vote");
            return;
        }

        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/pullRequests/" + prNumber + "/reviewers/" + reviewerId);

        ObjectNode voteNode = objectMapper.createObjectNode();
        voteNode.put("vote", vote);

        put(url, voteNode.toString());
    }

    /**
     * Get pull request details.
     * Used by {@link AzureDevOpsWebhookHandler} when PR metadata is needed
     * beyond what the webhook payload provides.
     */
    public JsonNode getPullRequest(String repoId, int prNumber) throws IOException {
        RepoPath path = parseRepoId(repoId);
        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/pullRequests/" + prNumber);
        return get(url);
    }

    /**
     * Get file content from a specific commit.
     */
    public String getFileContent(String repoId, String filePath, String commitSha) throws IOException {
        RepoPath path = parseRepoId(repoId);

        String url = buildApiUrl(path, "/git/repositories/" + path.repository +
                "/items?path=" + filePath +
                "&versionDescriptor.version=" + commitSha +
                "&versionDescriptor.versionType=commit" +
                "&$format=text");

        Request request = new Request.Builder()
                .url(url)
                .header("Authorization", getAuthHeader())
                .get()
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                throw new IOException("Failed to get file content: " + response.code());
            }
            assert response.body() != null;
            return response.body().string();
        }
    }

    // =========================================================================
    // HTTP Helpers
    // =========================================================================

    private JsonNode get(String url) throws IOException {
        Request request = new Request.Builder()
                .url(url)
                .header("Authorization", getAuthHeader())
                .get()
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                String body = response.body() != null ? response.body().string() : "";
                throw new IOException("Azure DevOps API error: " + response.code() + " - " + body);
            }
            assert response.body() != null;
            return objectMapper.readTree(response.body().string());
        }
    }

    private JsonNode post(String url, String body) throws IOException {
        Request request = new Request.Builder()
                .url(url)
                .header("Authorization", getAuthHeader())
                .header("Content-Type", "application/json")
                .post(RequestBody.create(body, JSON))
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                String responseBody = response.body() != null ? response.body().string() : "";
                throw new IOException("Azure DevOps API error: " + response.code() + " - " + responseBody);
            }
            if (response.body() != null) {
                return objectMapper.readTree(response.body().string());
            }
            return objectMapper.createObjectNode();
        }
    }

    private JsonNode put(String url, String body) throws IOException {
        Request request = new Request.Builder()
                .url(url)
                .header("Authorization", getAuthHeader())
                .header("Content-Type", "application/json")
                .put(RequestBody.create(body, JSON))
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                String responseBody = response.body() != null ? response.body().string() : "";
                throw new IOException("Azure DevOps API error: " + response.code() + " - " + responseBody);
            }
            if (response.body() != null) {
                return objectMapper.readTree(response.body().string());
            }
            return objectMapper.createObjectNode();
        }
    }

    // =========================================================================
    // Authentication
    // =========================================================================

    @NotNull
    private String getAuthHeader() {
        if (useAzureAd) {
            return "Bearer " + getAzureAdToken();
        } else {
            // PAT auth: Basic base64(":PAT")
            String credentials = ":" + personalAccessToken;
            return "Basic " + Base64.getEncoder().encodeToString(
                    credentials.getBytes(StandardCharsets.UTF_8));
        }
    }

    private synchronized String getAzureAdToken() {
        if (cachedAzureAdToken != null && System.currentTimeMillis() < tokenExpiry - 60000) {
            return cachedAzureAdToken;
        }

        try {
            String tokenUrl = azureLoginUrl + "/" + tenantId + "/oauth2/v2.0/token";

            String body = "client_id=" + clientId +
                    "&client_secret=" + clientSecret +
                    "&scope=499b84ac-1321-427f-aa17-267ca6975798/.default" + // Azure DevOps scope
                    "&grant_type=client_credentials";

            Request request = new Request.Builder()
                    .url(tokenUrl)
                    .header("Content-Type", "application/x-www-form-urlencoded")
                    .post(RequestBody.create(body, MediaType.parse("application/x-www-form-urlencoded")))
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                if (!response.isSuccessful()) {
                    throw new RuntimeException("Failed to get Azure AD token: " + response.code());
                }

                assert response.body() != null;
                JsonNode tokenResponse = objectMapper.readTree(response.body().string());

                cachedAzureAdToken = tokenResponse.path("access_token").asText();
                int expiresIn = tokenResponse.path("expires_in").asInt(3600);
                tokenExpiry = System.currentTimeMillis() + (expiresIn * 1000L);

                return cachedAzureAdToken;
            }

        } catch (IOException e) {
            throw new RuntimeException("Failed to get Azure AD token", e);
        }
    }

    // =========================================================================
    // Utility Methods
    // =========================================================================

    private record RepoPath(String organization, String project, String repository) {}

    @NotNull
    private RepoPath parseRepoId(@NotNull String repoId) {
        String[] parts = repoId.split("/");

        if (parts.length == 3) {
            return new RepoPath(parts[0], parts[1], parts[2]);
        } else if (parts.length == 2) {
            // Assume organization from config
            return new RepoPath(organization, parts[0], parts[1]);
        } else if (parts.length == 1) {
            // Assume organization and project from config
            return new RepoPath(organization, organization, parts[0]);
        }

        throw new IllegalArgumentException("Invalid Azure DevOps repo ID: " + repoId +
                ". Expected format: organization/project/repository");
    }

    @NotNull
    private String buildApiUrl(@NotNull RepoPath path, String endpoint) {
        return baseUrl + "/" + path.organization + "/" + path.project +
                "/_apis" + endpoint + "?api-version=" + API_VERSION;
    }

    @NotNull
    private String buildReviewSummary(@NotNull ReviewResponse review) {
        StringBuilder summary = new StringBuilder();

        summary.append("## 🤖 AI PR Review\n\n");

        if (review.summary() != null) {
            summary.append(review.summary()).append("\n\n");
        }

        // Statistics
        long criticalCount = review.comments().stream()
                .filter(c -> "critical".equalsIgnoreCase(c.severity())).count();
        long warningCount = review.comments().stream()
                .filter(c -> "warning".equalsIgnoreCase(c.severity())).count();
        long infoCount = review.comments().stream()
                .filter(c -> "info".equalsIgnoreCase(c.severity()) ||
                            "suggestion".equalsIgnoreCase(c.severity())).count();

        if (criticalCount > 0 || warningCount > 0 || infoCount > 0) {
            summary.append("### Findings\n");
            summary.append("| Severity | Count |\n");
            summary.append("|----------|-------|\n");
            if (criticalCount > 0) {
                summary.append("| 🔴 Critical | ").append(criticalCount).append(" |\n");
            }
            if (warningCount > 0) {
                summary.append("| 🟡 Warning | ").append(warningCount).append(" |\n");
            }
            if (infoCount > 0) {
                summary.append("| 🔵 Info | ").append(infoCount).append(" |\n");
            }
            summary.append("\n");
        }

        summary.append("---\n");
        summary.append("*Generated by [AI PR Reviewer](https://github.com/AuralithAI/RT-AI-PR-Reviewer)*");

        return summary.toString();
    }

    @NotNull
    private String formatCommentBody(@NotNull ReviewComment comment) {
        String sev = comment.severity() != null ? comment.severity() : "note";
        String severityEmoji = switch (sev.toLowerCase()) {
            case "critical", "error" -> "🔴";
            case "warning" -> "🟡";
            case "info", "suggestion" -> "🔵";
            default -> "💬";
        };

        StringBuilder body = new StringBuilder();
        body.append(severityEmoji).append(" **").append(sev.toUpperCase()).append("**");

        if (comment.category() != null) {
            body.append(" (").append(comment.category()).append(")");
        }

        body.append("\n\n").append(comment.message() != null ? comment.message() : "");

        if (comment.suggestion() != null) {
            body.append("\n\n**Suggested fix:**\n```\n")
                    .append(comment.suggestion()).append("\n```");
        }

        return body.toString();
    }
}
