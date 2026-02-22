package ai.aipr.server.integration.bitbucket;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.integration.AbstractVcsPlatformClient;
import com.fasterxml.jackson.databind.JsonNode;
import okhttp3.Credentials;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Bitbucket Cloud implementation of {@link ai.aipr.server.integration.VcsPlatformClient}.
 *
 * <p>Auth via Bearer token or Basic auth (username + app password).
 * Configurable API URL for Bitbucket Server/Data Center.</p>
 */
@Component
public class BitbucketClient extends AbstractVcsPlatformClient implements BitbucketPlatformClient {

    @Value("${aipr.auth.bitbucket.api-url:https://api.bitbucket.org/2.0}")
    private String apiBaseUrl;

    @Value("${aipr.auth.bitbucket.username:${BITBUCKET_USERNAME:}}")
    private String username;

    @Value("${aipr.auth.bitbucket.app-password:${BITBUCKET_APP_PASSWORD:}}")
    private String appPassword;

    @Value("${aipr.auth.bitbucket.token:${BITBUCKET_TOKEN:}}")
    private String bearerToken;

    @PostConstruct
    public void init() {
        initBase();
        log.info("Bitbucket client initialized: apiUrl={}", apiBaseUrl);
    }

    @Override
    @NotNull
    public String platform() {
        return "bitbucket";
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
                    String auth = getAuthHeader();
                    if (auth != null) {
                        builder.header("Authorization", auth);
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
        // repoId = "workspace/repo_slug"
        log.debug("Fetching PR diff: {} #{}", repoId, prNumber);

        Request request = new Request.Builder()
                .url(apiBaseUrl + "/repositories/" + repoId
                        + "/pullrequests/" + prNumber + "/diff")
                .get()
                .build();

        return executeForString(request);
    }

    @Override
    @NotNull
    public List<String> getChangedFiles(String repoId, int prNumber) {
        log.debug("Fetching PR files: {} #{}", repoId, prNumber);

        JsonNode diffstat = executeForJson(new Request.Builder()
                .url(apiBaseUrl + "/repositories/" + repoId
                        + "/pullrequests/" + prNumber + "/diffstat")
                .get().build());

        List<String> files = new ArrayList<>();
        JsonNode values = diffstat.path("values");
        if (values.isArray()) {
            for (JsonNode entry : values) {
                String path = entry.path("new").path("path").asText("");
                if (!path.isEmpty()) {
                    files.add(path);
                }
            }
        }
        return files;
    }

    @Override
    public void submitReview(String repoId, int prNumber, String commitSha,
                             @NotNull ReviewResponse review) {
        log.info("Submitting review to Bitbucket: {} #{}, comments={}",
                repoId, prNumber, review.comments().size());

        // Post summary as a general PR comment
        postComment(repoId, prNumber, buildSummaryBody(review));

        // Post inline comments
        for (ReviewComment comment : review.comments()) {
            if (comment.filePath() == null || comment.line() <= 0) {
                continue;
            }
            try {
                postInlineComment(repoId, prNumber, comment);
            } catch (Exception e) {
                log.warn("Failed to post inline comment on {}:{}: {}",
                        comment.filePath(), comment.line(), e.getMessage());
            }
        }
    }

    @Override
    public void postComment(String repoId, int prNumber, String body) {
        Map<String, Object> payload = Map.of("content", Map.of("raw", body));
        executeForJson(buildJsonPost(
                apiBaseUrl + "/repositories/" + repoId
                        + "/pullrequests/" + prNumber + "/comments",
                payload));
    }

    // =========================================================================
    // Bitbucket-specific: inline comments
    // =========================================================================

    private void postInlineComment(String repoId, int prNumber,
                                   @NotNull ReviewComment comment) {
        Map<String, Object> inline = new HashMap<>();
        inline.put("path", comment.filePath());
        inline.put("to", comment.line());

        Map<String, Object> payload = new HashMap<>();
        payload.put("content", Map.of("raw", formatComment(comment)));
        payload.put("inline", inline);

        executeForJson(buildJsonPost(
                apiBaseUrl + "/repositories/" + repoId
                        + "/pullrequests/" + prNumber + "/comments",
                payload));
    }

    // =========================================================================
    // Authentication
    // =========================================================================

    @Nullable
    private String getAuthHeader() {
        if (bearerToken != null && !bearerToken.isBlank()) {
            return "Bearer " + bearerToken;
        }
        if (username != null && !username.isBlank()
                && appPassword != null && !appPassword.isBlank()) {
            return Credentials.basic(username, appPassword);
        }
        return null;
    }
}
