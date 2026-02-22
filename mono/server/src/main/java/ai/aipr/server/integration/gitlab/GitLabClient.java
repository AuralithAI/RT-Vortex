package ai.aipr.server.integration.gitlab;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.integration.AbstractVcsPlatformClient;
import com.fasterxml.jackson.databind.JsonNode;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import org.jetbrains.annotations.NotNull;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * GitLab implementation of {@link ai.aipr.server.integration.VcsPlatformClient}.
 *
 * <p>Auth via PRIVATE-TOKEN header. Configurable base URL for self-hosted GitLab.</p>
 */
@Component
public class GitLabClient extends AbstractVcsPlatformClient implements GitLabPlatformClient {

    @Value("${aipr.auth.gitlab.base-url:https://gitlab.com}")
    private String baseUrl;

    @Value("${aipr.auth.gitlab.token:${GITLAB_TOKEN:}}")
    private String accessToken;

    @PostConstruct
    public void init() {
        initBase();
        log.info("GitLab client initialized: baseUrl={}", baseUrl);
    }

    @Override
    @NotNull
    public String platform() {
        return "gitlab";
    }

    @Override
    @NotNull
    protected OkHttpClient createHttpClient() {
        return new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(30))
                .readTimeout(Duration.ofSeconds(60))
                .writeTimeout(Duration.ofSeconds(30))
                .addInterceptor(chain -> {
                    Request.Builder builder = chain.request().newBuilder();
                    if (accessToken != null && !accessToken.isBlank()) {
                        builder.header("PRIVATE-TOKEN", accessToken);
                    }
                    return chain.proceed(builder.build());
                })
                .build();
    }

    // =========================================================================
    // VcsPlatformClient interface
    // =========================================================================

    @Override
    @NotNull
    public String getDiff(String repoId, int prNumber) {
        log.debug("Fetching MR diff: project={}, mr={}", repoId, prNumber);

        JsonNode changes = executeForJson(new Request.Builder()
                .url(apiUrl("/projects/" + urlEncode(repoId)
                        + "/merge_requests/" + prNumber + "/changes"))
                .get().build());

        StringBuilder diff = new StringBuilder();
        JsonNode changesArray = changes.path("changes");
        if (changesArray.isArray()) {
            for (JsonNode change : changesArray) {
                String oldPath = change.path("old_path").asText("");
                String newPath = change.path("new_path").asText("");
                String diffContent = change.path("diff").asText("");
                diff.append("--- a/").append(oldPath).append("\n");
                diff.append("+++ b/").append(newPath).append("\n");
                diff.append(diffContent).append("\n");
            }
        }
        return diff.toString();
    }

    @Override
    @NotNull
    public List<String> getChangedFiles(String repoId, int prNumber) {
        log.debug("Fetching MR files: project={}, mr={}", repoId, prNumber);

        JsonNode changes = executeForJson(new Request.Builder()
                .url(apiUrl("/projects/" + urlEncode(repoId)
                        + "/merge_requests/" + prNumber + "/changes"))
                .get().build());

        List<String> files = new ArrayList<>();
        JsonNode changesArray = changes.path("changes");
        if (changesArray.isArray()) {
            for (JsonNode change : changesArray) {
                String newPath = change.path("new_path").asText("");
                if (!newPath.isEmpty()) {
                    files.add(newPath);
                }
            }
        }
        return files;
    }

    @Override
    public void submitReview(String repoId, int prNumber, String commitSha,
                             @NotNull ReviewResponse review) {
        log.info("Submitting review to GitLab: project={}, mr={}, comments={}",
                repoId, prNumber, review.comments().size());

        // Post summary as a general MR note
        postComment(repoId, prNumber, buildSummaryBody(review));

        // Post inline comments as discussion threads
        for (ReviewComment comment : review.comments()) {
            if (comment.filePath() == null || comment.line() <= 0) {
                continue;
            }
            try {
                postInlineDiscussion(repoId, prNumber, commitSha, comment);
            } catch (Exception e) {
                log.warn("Failed to post inline comment on {}:{}: {}",
                        comment.filePath(), comment.line(), e.getMessage());
            }
        }
    }

    @Override
    public void postComment(String repoId, int prNumber, String body) {
        Map<String, Object> payload = Map.of("body", body);
        executeForJson(buildJsonPost(
                apiUrl("/projects/" + urlEncode(repoId) + "/merge_requests/" + prNumber + "/notes"),
                payload));
    }

    // =========================================================================
    // GitLab-specific: inline discussions
    // =========================================================================

    private void postInlineDiscussion(String repoId, int mrIid, String headSha,
                                      @NotNull ReviewComment comment) {
        Map<String, Object> position = new HashMap<>();
        position.put("base_sha", headSha);
        position.put("head_sha", headSha);
        position.put("start_sha", headSha);
        position.put("position_type", "text");
        position.put("new_path", comment.filePath());
        position.put("old_path", comment.filePath());
        position.put("new_line", comment.line());

        Map<String, Object> payload = new HashMap<>();
        payload.put("body", formatComment(comment));
        payload.put("position", position);

        executeForJson(buildJsonPost(
                apiUrl("/projects/" + urlEncode(repoId)
                        + "/merge_requests/" + mrIid + "/discussions"),
                payload));
    }

    // =========================================================================
    // GitLab suggestion syntax override
    // =========================================================================

    @Override
    @NotNull
    protected String buildSummaryBody(@NotNull ReviewResponse review) {
        // GitLab uses :robot: emoji syntax instead of unicode
        String body = super.buildSummaryBody(review);
        return body.replaceFirst("## AI PR Review", "## :robot: AI PR Review");
    }

    @Override
    @NotNull
    protected String formatSuggestion(@NotNull String suggestion) {
        return "```suggestion:-0+0\n" + suggestion + "\n```\n";
    }

    // =========================================================================
    // URL Helpers
    // =========================================================================

    @NotNull
    private String apiUrl(String path) {
        String base = baseUrl.endsWith("/") ? baseUrl.substring(0, baseUrl.length() - 1) : baseUrl;
        if (!base.contains("/api/v4")) {
            base = base + "/api/v4";
        }
        return base + path;
    }

    @NotNull
    private String urlEncode(@NotNull String value) {
        return value.replace("/", "%2F");
    }
}
