package ai.aipr.server.integration;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewResponse;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import okhttp3.ResponseBody;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.time.Duration;

/**
 * Abstract base for VCS platform clients providing shared OkHttp infrastructure,
 * retry logic, and markdown formatting.
 *
 * <p>Subclasses must implement:</p>
 * <ul>
 *   <li>{@link #createHttpClient()} — configure platform-specific auth interceptors</li>
 *   <li>All {@link VcsPlatformClient} methods for platform-specific API calls</li>
 * </ul>
 *
 * <p>Subclasses may override:</p>
 * <ul>
 *   <li>{@link #buildSummaryBody(ReviewResponse)} — customize review summary markdown</li>
 *   <li>{@link #formatComment(ReviewComment)} — customize inline comment markdown</li>
 * </ul>
 */
public abstract class AbstractVcsPlatformClient implements VcsPlatformClient {

    protected final Logger log = LoggerFactory.getLogger(getClass());
    protected static final MediaType JSON_MEDIA_TYPE = MediaType.parse("application/json");

    protected static final int MAX_RETRIES = 3;
    protected static final long INITIAL_BACKOFF_MS = 100;

    protected OkHttpClient httpClient;
    protected ObjectMapper objectMapper;

    // =========================================================================
    // Initialization — subclasses call from @PostConstruct
    // =========================================================================

    /**
     * Initialize shared state. Subclasses should call this from their
     * {@code @PostConstruct} method.
     */
    protected void initBase() {
        this.objectMapper = new ObjectMapper();
        this.httpClient = createHttpClient();
        log.info("{} client initialized", platform());
    }

    /**
     * Create the OkHttpClient with platform-specific auth interceptors.
     * Subclasses override this to add auth headers, custom timeouts, etc.
     */
    @NotNull
    protected OkHttpClient createHttpClient() {
        return new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(30))
                .readTimeout(Duration.ofSeconds(60))
                .writeTimeout(Duration.ofSeconds(30))
                .build();
    }

    // =========================================================================
    // HTTP Helpers with Retry
    // =========================================================================

    /**
     * Execute a request and return the response body as a string, with retry on 5xx.
     */
    @NotNull
    protected String executeForString(@NotNull Request request) {
        IOException lastException = null;

        for (int attempt = 0; attempt <= MAX_RETRIES; attempt++) {
            try (Response response = httpClient.newCall(request).execute()) {
                String body = safeBodyString(response);
                if (!response.isSuccessful()) {
                    if (response.code() >= 500 && attempt < MAX_RETRIES) {
                        long backoff = INITIAL_BACKOFF_MS * (1L << (attempt * 2));
                        log.warn("{} API returned {}, retrying in {}ms (attempt {}/{})",
                                platform(), response.code(), backoff, attempt + 1, MAX_RETRIES);
                        sleep(backoff);
                        continue;
                    }
                    throw new PlatformApiException(platform(), response.code(), body);
                }
                return body;
            } catch (IOException e) {
                lastException = e;
                if (attempt < MAX_RETRIES) {
                    long backoff = INITIAL_BACKOFF_MS * (1L << (attempt * 2));
                    log.warn("{} API request failed, retrying in {}ms: {}",
                            platform(), backoff, e.getMessage());
                    sleep(backoff);
                }
            }
        }
        throw new RuntimeException(platform() + " API request failed after retries", lastException);
    }

    /**
     * Execute a request and parse the JSON response, with retry on 5xx.
     */
    @NotNull
    protected JsonNode executeForJson(@NotNull Request request) {
        String body = executeForString(request);
        try {
            return body.isEmpty()
                    ? objectMapper.createObjectNode()
                    : objectMapper.readTree(body);
        } catch (IOException e) {
            throw new RuntimeException("Failed to parse " + platform() + " API response", e);
        }
    }

    /**
     * Build a JSON POST request.
     */
    @NotNull
    protected Request buildJsonPost(@NotNull String url, @NotNull Object payload) {
        try {
            String json = objectMapper.writeValueAsString(payload);
            return new Request.Builder()
                    .url(url)
                    .post(RequestBody.create(json, JSON_MEDIA_TYPE))
                    .build();
        } catch (IOException e) {
            throw new RuntimeException("Failed to serialize payload", e);
        }
    }

    /**
     * Safely extract body string from a response, handling null bodies.
     */
    @NotNull
    protected String safeBodyString(@NotNull Response response) throws IOException {
        ResponseBody body = response.body();
        return body != null ? body.string() : "";
    }

    // =========================================================================
    // Markdown Formatting — shared across all platforms
    // =========================================================================

    /**
     * Build the review summary body in markdown.
     * Override in subclasses for platform-specific tweaks (e.g., GitLab emoji syntax).
     */
    @NotNull
    protected String buildSummaryBody(@NotNull ReviewResponse review) {
        StringBuilder sb = new StringBuilder();
        sb.append("## AI PR Review\n\n");

        if (review.summary() != null) {
            sb.append(review.summary()).append("\n\n");
        }

        if (review.metrics() != null) {
            appendMetricsTable(sb, review);
        }

        if (review.suggestions() != null && !review.suggestions().isEmpty()) {
            sb.append("### Suggestions\n\n");
            for (String suggestion : review.suggestions()) {
                sb.append("- ").append(suggestion).append("\n");
            }
            sb.append("\n");
        }

        sb.append("---\n*Generated by AI PR Reviewer*\n");
        return sb.toString();
    }

    private void appendMetricsTable(@NotNull StringBuilder sb, @NotNull ReviewResponse review) {
        var m = review.metrics();
        sb.append("### Metrics\n");
        sb.append("| Category | Value |\n");
        sb.append("|----------|-------|\n");
        if (m.filesAnalyzed() != null) {
            sb.append("| Files Analyzed | ").append(m.filesAnalyzed()).append(" |\n");
        }
        if (m.totalFindings() != null) {
            sb.append("| Total Findings | ").append(m.totalFindings()).append(" |\n");
        }
        if (m.securityScore() != null) {
            sb.append(String.format("| Security | %.0f%% |\n", m.securityScore() * 100));
        }
        if (m.reliabilityScore() != null) {
            sb.append(String.format("| Reliability | %.0f%% |\n", m.reliabilityScore() * 100));
        }
        if (m.performanceScore() != null) {
            sb.append(String.format("| Performance | %.0f%% |\n", m.performanceScore() * 100));
        }
        if (m.overallScore() != null) {
            sb.append(String.format("| **Overall** | **%.0f%%** |\n", m.overallScore() * 100));
        }
        sb.append("\n");
    }

    /**
     * Format a single review comment in markdown.
     * Override in subclasses for platform-specific suggestion syntax.
     */
    @NotNull
    protected String formatComment(@NotNull ReviewComment comment) {
        StringBuilder sb = new StringBuilder();

        String severity = comment.severity() != null ? comment.severity().toUpperCase() : "NOTE";
        String category = comment.category() != null ? comment.category() : "";

        sb.append("**").append(severity).append("**");
        if (!category.isEmpty()) {
            sb.append(" — ").append(category);
        }
        sb.append("\n\n");

        if (comment.message() != null) {
            sb.append(comment.message()).append("\n");
        }

        if (comment.suggestion() != null && !comment.suggestion().isBlank()) {
            sb.append("\n").append(formatSuggestion(comment.suggestion()));
        }

        if (comment.confidence() != null) {
            sb.append(String.format("\n*Confidence: %.0f%%*", comment.confidence() * 100));
        }

        return sb.toString();
    }

    /**
     * Format a code suggestion block. Override for platform-specific syntax.
     * GitHub uses {@code ```suggestion}, GitLab uses {@code ```suggestion:-0+0}.
     */
    @NotNull
    protected String formatSuggestion(@NotNull String suggestion) {
        return "**Suggestion:** " + suggestion + "\n";
    }

    // =========================================================================
    // Utility
    // =========================================================================

    protected void sleep(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }

    // =========================================================================
    // Exception
    // =========================================================================

    /**
     * Exception for platform API errors with status code and response body.
     */
    public static class PlatformApiException extends RuntimeException {
        private final String platform;
        private final int statusCode;
        private final String responseBody;

        public PlatformApiException(String platform, int statusCode, String responseBody) {
            super(platform + " API error " + statusCode + ": " + responseBody);
            this.platform = platform;
            this.statusCode = statusCode;
            this.responseBody = responseBody;
        }

        public String getPlatform() { return platform; }
        public int getStatusCode() { return statusCode; }
        public String getResponseBody() { return responseBody; }
    }
}

