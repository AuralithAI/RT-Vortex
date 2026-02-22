package ai.aipr.server.integration.github;

import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.integration.AbstractVcsPlatformClient;
import com.fasterxml.jackson.databind.JsonNode;
import okhttp3.Interceptor;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.KeyFactory;
import java.security.PrivateKey;
import java.security.Signature;
import java.security.spec.InvalidKeySpecException;
import java.security.spec.PKCS8EncodedKeySpec;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Base64;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.TimeUnit;

/**
 * GitHub implementation of {@link ai.aipr.server.integration.VcsPlatformClient}.
 *
 * <p>Supports GitHub App JWT auth and PAT fallback.
 * Configurable API URL for GitHub Enterprise.</p>
 */
@Component
public class GitHubClient extends AbstractVcsPlatformClient implements GitHubPlatformClient {

    @Value("${aipr.auth.github.api-url:https://api.github.com}")
    private String apiBaseUrl;

    @Value("${aipr.auth.github.app-id:}")
    private String appId;

    @Value("${aipr.auth.github.private-key-path:}")
    private String privateKeyPath;

    @Value("${aipr.auth.github.token:${GITHUB_TOKEN:}}")
    private String personalAccessToken;

    /** Cache of owner → installation access token */
    private final Map<String, CachedToken> tokenCache = new ConcurrentHashMap<>();

    @PostConstruct
    public void init() {
        initBase();
        log.info("GitHub client initialized: apiUrl={}, appAuth={}", apiBaseUrl, isAppAuthConfigured());
    }

    @Override
    @NotNull
    public String platform() {
        return "github";
    }

    @Override
    @NotNull
    protected OkHttpClient createHttpClient() {
        return new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(30))
                .readTimeout(Duration.ofSeconds(60))
                .writeTimeout(Duration.ofSeconds(30))
                .addInterceptor(this::authInterceptor)
                .addInterceptor(this::retryInterceptor)
                .build();
    }

    // =========================================================================
    // VcsPlatformClient interface
    // =========================================================================

    @Override
    @NotNull
    public String getDiff(String repoId, int prNumber) {
        log.debug("Fetching diff: repo={}, pr={}", repoId, prNumber);

        Request request = new Request.Builder()
                .url(apiBaseUrl + "/repos/" + repoId + "/pulls/" + prNumber)
                .header("Accept", "application/vnd.github.v3.diff")
                .get()
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                String errorBody = safeBodyString(response);
                throw new PlatformApiException("github", response.code(), errorBody);
            }
            return safeBodyString(response);
        } catch (IOException e) {
            throw new RuntimeException("Failed to fetch diff for " + repoId + "#" + prNumber, e);
        }
    }

    @Override
    @NotNull
    public List<String> getChangedFiles(String repoId, int prNumber) {
        log.debug("Fetching PR files: repo={}, pr={}", repoId, prNumber);

        Request request = new Request.Builder()
                .url(apiBaseUrl + "/repos/" + repoId + "/pulls/" + prNumber
                        + "/files?per_page=100")
                .header("Accept", "application/vnd.github.v3+json")
                .get()
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                throw new PlatformApiException("github", response.code(), safeBodyString(response));
            }
            JsonNode filesNode = objectMapper.readTree(safeBodyString(response));
            List<String> paths = new ArrayList<>();
            if (filesNode.isArray()) {
                for (JsonNode file : filesNode) {
                    String filename = file.path("filename").asText("");
                    if (!filename.isEmpty()) {
                        paths.add(filename);
                    }
                }
            }
            return paths;
        } catch (IOException e) {
            throw new RuntimeException("Failed to fetch PR files for " + repoId + "#" + prNumber, e);
        }
    }

    @Override
    public void submitReview(String repoId, int prNumber, String commitSha,
                             @NotNull ReviewResponse review) {
        log.info("Submitting review: repo={}, pr={}, comments={}",
                repoId, prNumber, review.comments().size());

        try {
            // Map assessment to GitHub review event — null-safe
            String assessment = review.overallAssessment();
            String event;
            if (assessment == null) {
                event = "COMMENT";
            } else {
                event = switch (assessment.toLowerCase()) {
                    case "approve" -> "APPROVE";
                    case "request_changes" -> "REQUEST_CHANGES";
                    default -> "COMMENT";
                };
            }

            String body = buildSummaryBody(review);
            List<Map<String, Object>> githubComments = buildGitHubComments(review.comments());

            Map<String, Object> reviewPayload = new HashMap<>();
            reviewPayload.put("body", body);
            reviewPayload.put("event", event);
            reviewPayload.put("comments", githubComments);

            Request request = new Request.Builder()
                    .url(apiBaseUrl + "/repos/" + repoId + "/pulls/" + prNumber + "/reviews")
                    .post(RequestBody.create(
                            objectMapper.writeValueAsString(reviewPayload), JSON_MEDIA_TYPE))
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                if (!response.isSuccessful()) {
                    String errorBody = safeBodyString(response);
                    log.error("Failed to submit review: status={}, body={}",
                            response.code(), errorBody);
                    throw new PlatformApiException("github", response.code(), errorBody);
                }
                log.info("Review submitted successfully: repo={}, pr={}", repoId, prNumber);
            }
        } catch (IOException e) {
            log.error("Failed to submit review to {}/#{}", repoId, prNumber, e);
            throw new RuntimeException("Failed to submit review", e);
        }
    }

    @Override
    public void postComment(String repoId, int prNumber, String body) {
        log.debug("Posting comment: repo={}, pr={}", repoId, prNumber);

        try {
            Map<String, Object> payload = Map.of("body", body);
            Request request = new Request.Builder()
                    .url(apiBaseUrl + "/repos/" + repoId + "/issues/" + prNumber + "/comments")
                    .post(RequestBody.create(
                            objectMapper.writeValueAsString(payload), JSON_MEDIA_TYPE))
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                if (!response.isSuccessful()) {
                    log.warn("Failed to post comment: status={}", response.code());
                }
            }
        } catch (IOException e) {
            log.error("Failed to post comment on {}/#{}", repoId, prNumber, e);
        }
    }

    // =========================================================================
    // GitHub-specific public methods (used by GitHubWebhookHandler)
    // =========================================================================

    /**
     * Get pull request metadata (GitHub-specific, needed for issue_comment handler).
     */
    @Override
    public JsonNode getPullRequestInfo(String repoId, int prNumber) {
        log.debug("Fetching PR info: repo={}, pr={}", repoId, prNumber);

        Request request = new Request.Builder()
                .url(apiBaseUrl + "/repos/" + repoId + "/pulls/" + prNumber)
                .header("Accept", "application/vnd.github.v3+json")
                .get()
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                throw new PlatformApiException("github", response.code(), safeBodyString(response));
            }
            return objectMapper.readTree(safeBodyString(response));
        } catch (IOException e) {
            throw new RuntimeException("Failed to fetch PR info for " + repoId + "#" + prNumber, e);
        }
    }

    /**
     * Post an individual line comment on a PR (GitHub-specific, used as fallback).
     */
    @Override
    public void postLineComment(String repoId, int prNumber, String commitId,
                                String path, int line, String body) {
        log.debug("Posting line comment: repo={}, pr={}, path={}, line={}",
                repoId, prNumber, path, line);

        try {
            Map<String, Object> payload = new HashMap<>();
            payload.put("body", body);
            payload.put("commit_id", commitId);
            payload.put("path", path);
            payload.put("line", line);
            payload.put("side", "RIGHT");

            Request request = new Request.Builder()
                    .url(apiBaseUrl + "/repos/" + repoId + "/pulls/" + prNumber + "/comments")
                    .post(RequestBody.create(
                            objectMapper.writeValueAsString(payload), JSON_MEDIA_TYPE))
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                if (!response.isSuccessful()) {
                    log.warn("Failed to post line comment: status={}", response.code());
                }
            }
        } catch (IOException e) {
            log.error("Failed to post line comment on {}/#{}", repoId, prNumber, e);
        }
    }

    // =========================================================================
    // GitHub Comment Formatting — overrides base with emoji + suggestion blocks
    // =========================================================================

    @Override
    @NotNull
    protected String formatComment(@NotNull ReviewComment comment) {
        StringBuilder sb = new StringBuilder();

        String severityRaw = comment.severity() != null ? comment.severity() : "";
        String severityEmoji = switch (severityRaw) {
            case "critical" -> "🔴";
            case "error" -> "🟠";
            case "warning" -> "🟡";
            case "info" -> "🔵";
            default -> "💡";
        };

        String severity = severityRaw.isEmpty() ? "NOTE" : severityRaw.toUpperCase();
        String category = comment.category() != null ? comment.category() : "";

        sb.append(severityEmoji).append(" **").append(severity).append("**");
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

    @Override
    @NotNull
    protected String formatSuggestion(@NotNull String suggestion) {
        return "💡 **Suggestion:**\n```suggestion\n" + suggestion + "\n```\n";
    }

    // =========================================================================
    // GitHub Review Comment Conversion
    // =========================================================================

    @NotNull
    private List<Map<String, Object>> buildGitHubComments(@Nullable List<ReviewComment> comments) {
        if (comments == null || comments.isEmpty()) {
            return List.of();
        }

        List<Map<String, Object>> githubComments = new ArrayList<>();
        for (ReviewComment comment : comments) {
            if (comment.filePath() == null || comment.line() <= 0) {
                continue;
            }

            Map<String, Object> ghComment = new HashMap<>();
            ghComment.put("path", comment.filePath());
            ghComment.put("body", formatComment(comment));

            if (comment.endLine() != null && comment.endLine() > comment.line()) {
                ghComment.put("start_line", comment.line());
                ghComment.put("line", comment.endLine());
            } else {
                ghComment.put("line", comment.line());
            }

            githubComments.add(ghComment);
        }
        return githubComments;
    }

    // =========================================================================
    // Authentication
    // =========================================================================

    private boolean isAppAuthConfigured() {
        return appId != null && !appId.isBlank()
                && privateKeyPath != null && !privateKeyPath.isBlank();
    }

    @Nullable
    private String getAccessToken(@NotNull String owner) {
        if (isAppAuthConfigured() && !owner.isEmpty()) {
            try {
                return getInstallationToken(owner);
            } catch (Exception e) {
                log.warn("GitHub App auth failed for owner={}, falling back to PAT: {}",
                        owner, e.getMessage());
            }
        }
        if (personalAccessToken != null && !personalAccessToken.isBlank()) {
            return personalAccessToken;
        }
        log.warn("No GitHub authentication configured — API calls may be rate-limited");
        return null;
    }

    @NotNull
    private String getInstallationToken(String owner) throws IOException {
        CachedToken cached = tokenCache.get(owner);
        if (cached != null && !cached.isExpired()) {
            return cached.token;
        }

        log.debug("Fetching installation token for owner={}", owner);
        String jwt = generateAppJwt();
        long installationId = getInstallationId(jwt, owner);

        OkHttpClient plainClient = buildPlainClient();
        Request request = new Request.Builder()
                .url(apiBaseUrl + "/app/installations/" + installationId + "/access_tokens")
                .header("Authorization", "Bearer " + jwt)
                .header("Accept", "application/vnd.github.v3+json")
                .post(RequestBody.create("{}", JSON_MEDIA_TYPE))
                .build();

        try (Response response = plainClient.newCall(request).execute()) {
            if (!response.isSuccessful()) {
                throw new IOException("Failed to get installation token: " + response.code());
            }
            JsonNode body = objectMapper.readTree(safeBodyString(response));
            String token = body.get("token").asText();

            tokenCache.put(owner, new CachedToken(token,
                    System.currentTimeMillis() + TimeUnit.MINUTES.toMillis(50)));

            log.info("Obtained installation token for owner={}", owner);
            return token;
        }
    }

    @NotNull
    private String generateAppJwt() {
        try {
            PrivateKey privateKey = readPrivateKey(privateKeyPath);
            long nowSeconds = System.currentTimeMillis() / 1000;
            long expSeconds = nowSeconds + 600;

            String header = Base64.getUrlEncoder().withoutPadding()
                    .encodeToString("{\"alg\":\"RS256\",\"typ\":\"JWT\"}".getBytes());
            String payload = Base64.getUrlEncoder().withoutPadding()
                    .encodeToString(("{\"iss\":\"" + appId + "\",\"iat\":" + nowSeconds
                            + ",\"exp\":" + expSeconds + "}").getBytes());

            String signingInput = header + "." + payload;
            Signature sig = Signature.getInstance("SHA256withRSA");
            sig.initSign(privateKey);
            sig.update(signingInput.getBytes());
            byte[] signature = sig.sign();

            return signingInput + "." + Base64.getUrlEncoder().withoutPadding()
                    .encodeToString(signature);
        } catch (Exception e) {
            throw new RuntimeException("Failed to generate GitHub App JWT", e);
        }
    }

    @NotNull
    private PrivateKey readPrivateKey(String path) throws Exception {
        String pem = Files.readString(Path.of(path));
        pem = pem.replace("-----BEGIN RSA PRIVATE KEY-----", "")
                .replace("-----END RSA PRIVATE KEY-----", "")
                .replace("-----BEGIN PRIVATE KEY-----", "")
                .replace("-----END PRIVATE KEY-----", "")
                .replaceAll("\\s", "");

        byte[] keyBytes = Base64.getDecoder().decode(pem);
        try {
            PKCS8EncodedKeySpec spec = new PKCS8EncodedKeySpec(keyBytes);
            return KeyFactory.getInstance("RSA").generatePrivate(spec);
        } catch (InvalidKeySpecException e) {
            byte[] pkcs8 = wrapPkcs1InPkcs8(keyBytes);
            PKCS8EncodedKeySpec spec = new PKCS8EncodedKeySpec(pkcs8);
            return KeyFactory.getInstance("RSA").generatePrivate(spec);
        }
    }

    @NotNull
    private byte[] wrapPkcs1InPkcs8(byte[] pkcs1) {
        byte[] rsaOid = {0x06, 0x09, 0x2A, (byte) 0x86, 0x48,
                (byte) 0x86, (byte) 0xF7, 0x0D, 0x01, 0x01, 0x01};
        byte[] nullTag = {0x05, 0x00};
        byte[] algorithmSeq = derSequence(concat(rsaOid, nullTag));
        byte[] octetString = derTag(0x04, pkcs1);
        byte[] version = {0x02, 0x01, 0x00};
        return derSequence(concat(version, concat(algorithmSeq, octetString)));
    }

    @NotNull
    private byte[] derSequence(byte[] content) { return derTag(0x30, content); }

    @NotNull
    private byte[] derTag(int tag, @NotNull byte[] content) {
        byte[] length = derLength(content.length);
        byte[] result = new byte[1 + length.length + content.length];
        result[0] = (byte) tag;
        System.arraycopy(length, 0, result, 1, length.length);
        System.arraycopy(content, 0, result, 1 + length.length, content.length);
        return result;
    }

    @NotNull
    private byte[] derLength(int length) {
        if (length < 128) return new byte[]{(byte) length};
        if (length < 256) return new byte[]{(byte) 0x81, (byte) length};
        if (length < 65536) return new byte[]{(byte) 0x82, (byte) (length >> 8), (byte) length};
        return new byte[]{(byte) 0x83, (byte) (length >> 16), (byte) (length >> 8), (byte) length};
    }

    @NotNull
    private byte[] concat(@NotNull byte[] a, @NotNull byte[] b) {
        byte[] result = new byte[a.length + b.length];
        System.arraycopy(a, 0, result, 0, a.length);
        System.arraycopy(b, 0, result, a.length, b.length);
        return result;
    }

    private long getInstallationId(String jwt, String owner) throws IOException {
        OkHttpClient plainClient = buildPlainClient();

        Request orgRequest = new Request.Builder()
                .url(apiBaseUrl + "/orgs/" + owner + "/installation")
                .header("Authorization", "Bearer " + jwt)
                .header("Accept", "application/vnd.github.v3+json")
                .get().build();

        try (Response response = plainClient.newCall(orgRequest).execute()) {
            if (response.isSuccessful()) {
                JsonNode body = objectMapper.readTree(safeBodyString(response));
                return body.get("id").asLong();
            }
        }

        Request userRequest = new Request.Builder()
                .url(apiBaseUrl + "/users/" + owner + "/installation")
                .header("Authorization", "Bearer " + jwt)
                .header("Accept", "application/vnd.github.v3+json")
                .get().build();

        try (Response response = plainClient.newCall(userRequest).execute()) {
            if (response.isSuccessful()) {
                JsonNode body = objectMapper.readTree(safeBodyString(response));
                return body.get("id").asLong();
            }
            throw new IOException("No GitHub App installation found for owner: " + owner);
        }
    }

    @NotNull
    private OkHttpClient buildPlainClient() {
        return new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(10))
                .readTimeout(Duration.ofSeconds(10))
                .build();
    }

    // =========================================================================
    // OkHttp Interceptors
    // =========================================================================

    @NotNull
    private Response authInterceptor(@NotNull Interceptor.Chain chain) throws IOException {
        Request original = chain.request();
        String owner = extractOwnerFromUrl(original.url().toString());

        Request.Builder builder = original.newBuilder()
                .header("X-GitHub-Api-Version", "2022-11-28");

        if (original.header("Accept") == null) {
            builder.header("Accept", "application/vnd.github.v3+json");
        }

        String token = getAccessToken(owner);
        if (token != null) {
            builder.header("Authorization", "Bearer " + token);
        }

        return chain.proceed(builder.build());
    }

    @NotNull
    private Response retryInterceptor(@NotNull Interceptor.Chain chain) throws IOException {
        Request request = chain.request();
        IOException lastException = null;

        for (int attempt = 0; attempt <= MAX_RETRIES; attempt++) {
            try {
                Response response = chain.proceed(request);

                if (response.isSuccessful() || (response.code() >= 400 && response.code() < 500)) {
                    return response;
                }

                if (attempt < MAX_RETRIES) {
                    response.close();
                    long backoff = getBackoffMs(response, attempt);
                    log.warn("GitHub API returned {}, retrying in {}ms (attempt {}/{})",
                            response.code(), backoff, attempt + 1, MAX_RETRIES);
                    sleep(backoff);
                } else {
                    return response;
                }

            } catch (IOException e) {
                lastException = e;
                if (attempt < MAX_RETRIES) {
                    long backoff = INITIAL_BACKOFF_MS * (1L << (attempt * 2));
                    log.warn("GitHub API request failed, retrying in {}ms (attempt {}/{}): {}",
                            backoff, attempt + 1, MAX_RETRIES, e.getMessage());
                    sleep(backoff);
                }
            }
        }

        throw lastException != null ? lastException : new IOException("Retry exhausted");
    }

    private long getBackoffMs(@NotNull Response response, int attempt) {
        String retryAfter = response.header("Retry-After");
        if (retryAfter != null) {
            try {
                return Long.parseLong(retryAfter) * 1000;
            } catch (NumberFormatException ignored) { }
        }
        return INITIAL_BACKOFF_MS * (1L << (attempt * 2));
    }

    // =========================================================================
    // Utility
    // =========================================================================

    @NotNull
    String extractOwnerFromUrl(@NotNull String url) {
        int reposIdx = url.indexOf("/repos/");
        if (reposIdx >= 0) {
            String afterRepos = url.substring(reposIdx + "/repos/".length());
            int slashIdx = afterRepos.indexOf('/');
            if (slashIdx > 0) {
                return afterRepos.substring(0, slashIdx);
            }
            return afterRepos;
        }
        return "";
    }

    // =========================================================================
    // Inner Types
    // =========================================================================

    private record CachedToken(String token, long expiresAtMillis) {
        boolean isExpired() {
            return System.currentTimeMillis() >= expiresAtMillis;
        }
    }

    /** Exception for GitHub API errors — extends the platform exception. */
    public static class GitHubApiException extends PlatformApiException {
        public GitHubApiException(String message, int statusCode, String responseBody) {
            super("github", statusCode, responseBody);
        }
    }
}
