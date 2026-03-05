package dev.rtvortex.sdk;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.DeserializationFeature;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import dev.rtvortex.sdk.model.*;
import okhttp3.*;

import java.io.IOException;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;

/**
 * Synchronous Java client for the RTVortex API.
 *
 * <pre>{@code
 * RTVortexClient client = new RTVortexClient.Builder()
 *     .token("your-token")
 *     .build();
 *
 * User user = client.me();
 * System.out.println(user.getEmail());
 * }</pre>
 */
public class RTVortexClient implements AutoCloseable {

    private static final String DEFAULT_BASE_URL = "https://api.rtvortex.dev";
    private static final String USER_AGENT = "rtvortex-sdk-java/0.1.0";
    private static final MediaType JSON_MEDIA = MediaType.get("application/json; charset=utf-8");

    private final String baseUrl;
    private final String token;
    private final OkHttpClient httpClient;
    private final ObjectMapper mapper;

    private RTVortexClient(Builder builder) {
        this.baseUrl = builder.baseUrl.replaceAll("/+$", "");
        this.token = builder.token;
        this.httpClient = builder.httpClient != null
                ? builder.httpClient
                : new OkHttpClient.Builder()
                    .connectTimeout(builder.timeout, TimeUnit.SECONDS)
                    .readTimeout(builder.timeout, TimeUnit.SECONDS)
                    .writeTimeout(builder.timeout, TimeUnit.SECONDS)
                    .build();
        this.mapper = new ObjectMapper()
                .configure(DeserializationFeature.FAIL_ON_UNKNOWN_PROPERTIES, false);
    }

    @Override
    public void close() {
        httpClient.dispatcher().executorService().shutdown();
        httpClient.connectionPool().evictAll();
    }

    // ── Builder ─────────────────────────────────────────────────────────────

    public static class Builder {
        private String token;
        private String baseUrl = DEFAULT_BASE_URL;
        private long timeout = 30;
        private OkHttpClient httpClient;

        public Builder token(String token) {
            this.token = token;
            return this;
        }

        public Builder baseUrl(String baseUrl) {
            this.baseUrl = baseUrl;
            return this;
        }

        public Builder timeout(long timeoutSeconds) {
            this.timeout = timeoutSeconds;
            return this;
        }

        public Builder httpClient(OkHttpClient httpClient) {
            this.httpClient = httpClient;
            return this;
        }

        public RTVortexClient build() {
            if (token == null || token.isEmpty()) {
                throw new IllegalArgumentException("token is required");
            }
            return new RTVortexClient(this);
        }
    }

    // ── Internal helpers ────────────────────────────────────────────────────

    private Request.Builder newRequest(String path) {
        return new Request.Builder()
                .url(baseUrl + path)
                .header("Authorization", "Bearer " + token)
                .header("User-Agent", USER_AGENT)
                .header("Accept", "application/json");
    }

    private String execute(Request request) throws IOException {
        try (Response response = httpClient.newCall(request).execute()) {
            String body = response.body() != null ? response.body().string() : "";
            if (!response.isSuccessful()) {
                throwForStatus(response.code(), body);
            }
            return body;
        }
    }

    private <T> T executeAndParse(Request request, Class<T> clazz) throws IOException {
        String body = execute(request);
        return mapper.readValue(body, clazz);
    }

    private <T> T executeAndParse(Request request, TypeReference<T> typeRef) throws IOException {
        String body = execute(request);
        return mapper.readValue(body, typeRef);
    }

    private void throwForStatus(int code, String body) {
        String msg;
        try {
            JsonNode node = mapper.readTree(body);
            msg = node.has("error") ? node.get("error").asText() : body;
        } catch (Exception e) {
            msg = body;
        }

        switch (code) {
            case 401 -> throw new AuthenticationException(msg, body);
            case 404 -> throw new NotFoundException(msg, body);
            case 422 -> throw new ValidationException(msg, body);
            case 403, 429 -> throw new QuotaExceededException(msg, code, body);
            default -> {
                if (code >= 500) throw new ServerException(msg, code, body);
                throw new RTVortexException(msg, code, body);
            }
        }
    }

    private String paginationQuery(int limit, int offset) {
        return "?limit=" + limit + "&offset=" + offset;
    }

    // ── User ────────────────────────────────────────────────────────────────

    public User me() throws IOException {
        Request req = newRequest("/user/me").get().build();
        return executeAndParse(req, User.class);
    }

    public User updateMe(Map<String, String> fields) throws IOException {
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(fields), JSON_MEDIA);
        Request req = newRequest("/user/me").put(rb).build();
        return executeAndParse(req, User.class);
    }

    // ── Organizations ───────────────────────────────────────────────────────

    public JsonNode listOrgs(int limit, int offset) throws IOException {
        Request req = newRequest("/orgs" + paginationQuery(limit, offset)).get().build();
        return mapper.readTree(execute(req));
    }

    public Org createOrg(String name, String slug, String plan) throws IOException {
        Map<String, String> payload = Map.of("name", name, "slug", slug, "plan", plan);
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(payload), JSON_MEDIA);
        Request req = newRequest("/orgs").post(rb).build();
        return executeAndParse(req, Org.class);
    }

    public Org getOrg(String orgId) throws IOException {
        Request req = newRequest("/orgs/" + orgId).get().build();
        return executeAndParse(req, Org.class);
    }

    public Org updateOrg(String orgId, Map<String, String> fields) throws IOException {
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(fields), JSON_MEDIA);
        Request req = newRequest("/orgs/" + orgId).put(rb).build();
        return executeAndParse(req, Org.class);
    }

    // ── Members ─────────────────────────────────────────────────────────────

    public JsonNode listMembers(String orgId, int limit, int offset) throws IOException {
        Request req = newRequest("/orgs/" + orgId + "/members" + paginationQuery(limit, offset))
                .get().build();
        return mapper.readTree(execute(req));
    }

    public OrgMember inviteMember(String orgId, String email, String role) throws IOException {
        Map<String, String> payload = Map.of("email", email, "role", role);
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(payload), JSON_MEDIA);
        Request req = newRequest("/orgs/" + orgId + "/members").post(rb).build();
        return executeAndParse(req, OrgMember.class);
    }

    public void removeMember(String orgId, String userId) throws IOException {
        Request req = newRequest("/orgs/" + orgId + "/members/" + userId)
                .delete().build();
        execute(req);
    }

    // ── Repositories ────────────────────────────────────────────────────────

    public JsonNode listRepos(int limit, int offset) throws IOException {
        Request req = newRequest("/repos" + paginationQuery(limit, offset)).get().build();
        return mapper.readTree(execute(req));
    }

    public Repo registerRepo(Map<String, String> data) throws IOException {
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(data), JSON_MEDIA);
        Request req = newRequest("/repos").post(rb).build();
        return executeAndParse(req, Repo.class);
    }

    public Repo getRepo(String repoId) throws IOException {
        Request req = newRequest("/repos/" + repoId).get().build();
        return executeAndParse(req, Repo.class);
    }

    public Repo updateRepo(String repoId, Map<String, Object> fields) throws IOException {
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(fields), JSON_MEDIA);
        Request req = newRequest("/repos/" + repoId).put(rb).build();
        return executeAndParse(req, Repo.class);
    }

    public void deleteRepo(String repoId) throws IOException {
        Request req = newRequest("/repos/" + repoId).delete().build();
        execute(req);
    }

    // ── Reviews ─────────────────────────────────────────────────────────────

    public JsonNode listReviews(int limit, int offset) throws IOException {
        Request req = newRequest("/reviews" + paginationQuery(limit, offset)).get().build();
        return mapper.readTree(execute(req));
    }

    public Review triggerReview(String repoId, int prNumber) throws IOException {
        Map<String, Object> payload = Map.of("repo_id", repoId, "pr_number", prNumber);
        RequestBody rb = RequestBody.create(mapper.writeValueAsString(payload), JSON_MEDIA);
        Request req = newRequest("/reviews").post(rb).build();
        return executeAndParse(req, Review.class);
    }

    public Review getReview(String reviewId) throws IOException {
        Request req = newRequest("/reviews/" + reviewId).get().build();
        return executeAndParse(req, Review.class);
    }

    public List<ReviewComment> getReviewComments(String reviewId) throws IOException {
        Request req = newRequest("/reviews/" + reviewId + "/comments").get().build();
        String body = execute(req);
        JsonNode node = mapper.readTree(body);
        if (node.isArray()) {
            return mapper.readValue(body, new TypeReference<List<ReviewComment>>() {});
        }
        if (node.has("comments")) {
            return mapper.readValue(
                    node.get("comments").toString(),
                    new TypeReference<List<ReviewComment>>() {}
            );
        }
        return List.of();
    }

    // ── Indexing ─────────────────────────────────────────────────────────────

    public IndexStatus triggerIndex(String repoId) throws IOException {
        RequestBody rb = RequestBody.create("", JSON_MEDIA);
        Request req = newRequest("/repos/" + repoId + "/index").post(rb).build();
        return executeAndParse(req, IndexStatus.class);
    }

    public IndexStatus getIndexStatus(String repoId) throws IOException {
        Request req = newRequest("/repos/" + repoId + "/index/status").get().build();
        return executeAndParse(req, IndexStatus.class);
    }

    // ── Admin ───────────────────────────────────────────────────────────────

    public AdminStats getStats() throws IOException {
        Request req = newRequest("/admin/stats").get().build();
        return executeAndParse(req, AdminStats.class);
    }

    public HealthStatus health() throws IOException {
        Request req = newRequest("/health").get().build();
        return executeAndParse(req, HealthStatus.class);
    }

    public HealthStatus healthDetailed() throws IOException {
        Request req = newRequest("/admin/health/detailed").get().build();
        return executeAndParse(req, HealthStatus.class);
    }
}
