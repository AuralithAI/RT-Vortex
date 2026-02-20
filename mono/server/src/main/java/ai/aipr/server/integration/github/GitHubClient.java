package ai.aipr.server.integration.github;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewResponse;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import okhttp3.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * Client for GitHub API operations.
 */
@Component
public class GitHubClient {

    private static final Logger log = LoggerFactory.getLogger(GitHubClient.class);
    private static final MediaType JSON = MediaType.parse("application/json");
    private static final String API_BASE = "https://api.github.com";

    @Value("${aipr.integrations.github.app-id:}")
    private String appId;

    @Value("${aipr.integrations.github.private-key-path:}")
    private String privateKeyPath;

    private OkHttpClient httpClient;
    private ObjectMapper objectMapper;

    @PostConstruct
    public void init() {
        httpClient = new OkHttpClient.Builder()
                .addInterceptor(chain -> {
                    Request original = chain.request();
                    Request.Builder builder = original.newBuilder()
                            .header("Accept", "application/vnd.github.v3+json")
                            .header("X-GitHub-Api-Version", "2022-11-28");
                    
                    // Add authentication
                    String token = getAccessToken(extractOwnerFromUrl(original.url().toString()));
                    if (token != null) {
                        builder.header("Authorization", "Bearer " + token);
                    }
                    
                    return chain.proceed(builder.build());
                })
                .build();
        
        objectMapper = new ObjectMapper();
        log.info("GitHub client initialized");
    }

    /**
     * Get the diff for a pull request.
     */
    public String getPullRequestDiff(String repoId, int prNumber) {
        log.debug("Fetching diff: repo={}, pr={}", repoId, prNumber);

        Request request = new Request.Builder()
                .url(API_BASE + "/repos/" + repoId + "/pulls/" + prNumber)
                .header("Accept", "application/vnd.github.v3.diff")
                .get()
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                throw new RuntimeException("Failed to fetch diff: " + response.code());
            }
            return response.body().string();
        } catch (IOException e) {
            throw new RuntimeException("Failed to fetch diff", e);
        }
    }

    /**
     * Submit a review to a pull request.
     */
    public void submitReview(String repoId, int prNumber, ReviewResponse review) {
        log.info("Submitting review: repo={}, pr={}, comments={}", 
                repoId, prNumber, review.comments().size());

        try {
            // Map assessment to GitHub review event
            String event = switch (review.overallAssessment()) {
                case "approve" -> "APPROVE";
                case "request_changes" -> "REQUEST_CHANGES";
                default -> "COMMENT";
            };

            // Build review body
            StringBuilder body = new StringBuilder();
            body.append("## AI PR Review\n\n");
            body.append(review.summary()).append("\n\n");
            
            if (review.metrics() != null) {
                body.append("### Metrics\n");
                body.append("| Category | Score |\n");
                body.append("|----------|-------|\n");
                if (review.metrics().securityScore() != null) {
                    body.append(String.format("| Security | %.0f%% |\n", review.metrics().securityScore() * 100));
                }
                if (review.metrics().reliabilityScore() != null) {
                    body.append(String.format("| Reliability | %.0f%% |\n", review.metrics().reliabilityScore() * 100));
                }
                body.append("\n");
            }

            // Convert comments to GitHub format
            List<Map<String, Object>> githubComments = new ArrayList<>();
            for (ReviewComment comment : review.comments()) {
                Map<String, Object> ghComment = Map.of(
                        "path", comment.filePath(),
                        "line", comment.line(),
                        "body", formatComment(comment)
                );
                githubComments.add(ghComment);
            }

            // Submit review
            var reviewBody = Map.of(
                    "body", body.toString(),
                    "event", event,
                    "comments", githubComments
            );

            Request request = new Request.Builder()
                    .url(API_BASE + "/repos/" + repoId + "/pulls/" + prNumber + "/reviews")
                    .post(RequestBody.create(objectMapper.writeValueAsString(reviewBody), JSON))
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                if (!response.isSuccessful()) {
                    String errorBody = response.body() != null ? response.body().string() : "No body";
                    log.error("Failed to submit review: status={}, body={}", response.code(), errorBody);
                    throw new RuntimeException("Failed to submit review: " + response.code());
                }
                
                log.info("Review submitted successfully: repo={}, pr={}", repoId, prNumber);
            }
        } catch (IOException e) {
            log.error("Failed to submit review", e);
            throw new RuntimeException("Failed to submit review", e);
        }
    }

    private String formatComment(ReviewComment comment) {
        StringBuilder sb = new StringBuilder();
        
        // Severity badge
        String severityEmoji = switch (comment.severity()) {
            case "critical" -> "🔴";
            case "error" -> "🟠";
            case "warning" -> "🟡";
            case "info" -> "🔵";
            default -> "💡";
        };
        
        sb.append(severityEmoji).append(" **").append(comment.severity().toUpperCase())
          .append("** - ").append(comment.category()).append("\n\n");
        sb.append(comment.message()).append("\n");
        
        if (comment.suggestion() != null && !comment.suggestion().isBlank()) {
            sb.append("\n**Suggestion:**\n");
            sb.append(comment.suggestion()).append("\n");
        }
        
        return sb.toString();
    }

    private String getAccessToken(String owner) {
        // TODO: Implement GitHub App authentication
        // 1. Generate JWT from private key
        // 2. Exchange for installation access token
        // For now, return null (use GITHUB_TOKEN from environment)
        return System.getenv("GITHUB_TOKEN");
    }

    private String extractOwnerFromUrl(String url) {
        // Extract owner/repo from URL for installation lookup
        return "";
    }
}
