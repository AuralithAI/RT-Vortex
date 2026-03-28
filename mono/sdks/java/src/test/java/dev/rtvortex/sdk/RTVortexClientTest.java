package dev.rtvortex.sdk;

import dev.rtvortex.sdk.model.*;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import org.junit.jupiter.api.*;

import java.io.IOException;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.*;

class RTVortexClientTest {

    private MockWebServer server;
    private RTVortexClient client;

    @BeforeEach
    void setUp() throws IOException {
        server = new MockWebServer();
        server.start();
        String baseUrl = server.url("/").toString();
        client = new RTVortexClient.Builder()
                .token("test-token")
                .baseUrl(baseUrl)
                .build();
    }

    @AfterEach
    void tearDown() throws IOException {
        client.close();
        server.shutdown();
    }

    // ── User ────────────────────────────────────────────────────────────────

    @Test
    void testMe() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"id\":\"u1\",\"email\":\"a@b.com\",\"display_name\":\"Alice\"}")
                .addHeader("Content-Type", "application/json"));

        User user = client.me();
        assertEquals("u1", user.getId());
        assertEquals("a@b.com", user.getEmail());
        assertEquals("Alice", user.getDisplayName());

        RecordedRequest req = server.takeRequest();
        assertEquals("GET", req.getMethod());
        assertTrue(req.getHeader("Authorization").contains("test-token"));
    }

    @Test
    void testUpdateMe() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"id\":\"u1\",\"email\":\"a@b.com\",\"display_name\":\"Bob\"}")
                .addHeader("Content-Type", "application/json"));

        User user = client.updateMe(Map.of("display_name", "Bob"));
        assertEquals("Bob", user.getDisplayName());

        RecordedRequest req = server.takeRequest();
        assertEquals("PUT", req.getMethod());
    }

    // ── Organizations ───────────────────────────────────────────────────────

    @Test
    void testCreateOrg() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"id\":\"o1\",\"name\":\"Acme\",\"slug\":\"acme\",\"plan\":\"pro\"}")
                .addHeader("Content-Type", "application/json"));

        Org org = client.createOrg("Acme", "acme", "pro");
        assertEquals("o1", org.getId());
        assertEquals("pro", org.getPlan());
    }

    @Test
    void testGetOrg() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"id\":\"o1\",\"name\":\"Acme\",\"slug\":\"acme\"}")
                .addHeader("Content-Type", "application/json"));

        Org org = client.getOrg("o1");
        assertEquals("Acme", org.getName());
    }

    // ── Repos ───────────────────────────────────────────────────────────────

    @Test
    void testGetRepo() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"id\":\"r1\",\"platform\":\"github\",\"owner\":\"acme\",\"name\":\"api\"}")
                .addHeader("Content-Type", "application/json"));

        Repo repo = client.getRepo("r1");
        assertEquals("github", repo.getPlatform());
    }

    @Test
    void testDeleteRepo() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(204));
        assertDoesNotThrow(() -> client.deleteRepo("r1"));
    }

    // ── Reviews ─────────────────────────────────────────────────────────────

    @Test
    void testTriggerReview() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"id\":\"rv1\",\"repo_id\":\"r1\",\"pr_number\":42,\"status\":\"pending\"}")
                .addHeader("Content-Type", "application/json"));

        Review review = client.triggerReview("r1", 42);
        assertEquals("pending", review.getStatus());
        assertEquals(42, review.getPrNumber());
    }

    @Test
    void testGetReviewCommentsList() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("[{\"id\":\"c1\",\"severity\":\"warning\",\"message\":\"Fix\"},{\"id\":\"c2\",\"severity\":\"error\",\"message\":\"Bug\"}]")
                .addHeader("Content-Type", "application/json"));

        List<ReviewComment> comments = client.getReviewComments("rv1");
        assertEquals(2, comments.size());
        assertEquals("error", comments.get(1).getSeverity());
    }

    @Test
    void testGetReviewCommentsObject() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"comments\":[{\"id\":\"c1\",\"severity\":\"info\",\"message\":\"OK\"}]}")
                .addHeader("Content-Type", "application/json"));

        List<ReviewComment> comments = client.getReviewComments("rv1");
        assertEquals(1, comments.size());
    }

    // ── Index ───────────────────────────────────────────────────────────────

    @Test
    void testTriggerIndex() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"repo_id\":\"r1\",\"status\":\"running\",\"job_id\":\"j1\"}")
                .addHeader("Content-Type", "application/json"));

        IndexStatus status = client.triggerIndex("r1");
        assertEquals("running", status.getStatus());
    }

    // ── Admin ───────────────────────────────────────────────────────────────

    @Test
    void testGetStats() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"total_users\":42,\"total_repos\":7,\"reviews_today\":3}")
                .addHeader("Content-Type", "application/json"));

        AdminStats stats = client.getStats();
        assertEquals(42, stats.getTotalUsers());
        assertEquals(3, stats.getReviewsToday());
    }

    @Test
    void testHealth() throws Exception {
        server.enqueue(new MockResponse()
                .setBody("{\"status\":\"ok\"}")
                .addHeader("Content-Type", "application/json"));

        HealthStatus health = client.health();
        assertEquals("ok", health.getStatus());
    }

    // ── Error Mapping ───────────────────────────────────────────────────────

    @Test
    void test401ThrowsAuth() {
        server.enqueue(new MockResponse()
                .setResponseCode(401)
                .setBody("{\"error\":\"unauthorized\"}"));

        assertThrows(AuthenticationException.class, () -> client.me());
    }

    @Test
    void test404ThrowsNotFound() {
        server.enqueue(new MockResponse()
                .setResponseCode(404)
                .setBody("{\"error\":\"not found\"}"));

        assertThrows(NotFoundException.class, () -> client.getRepo("missing"));
    }

    @Test
    void test422ThrowsValidation() {
        server.enqueue(new MockResponse()
                .setResponseCode(422)
                .setBody("{\"error\":\"invalid\"}"));

        assertThrows(ValidationException.class, () -> client.createOrg("", "", ""));
    }

    @Test
    void test429ThrowsQuota() {
        server.enqueue(new MockResponse()
                .setResponseCode(429)
                .setBody("{\"error\":\"rate limited\"}"));

        assertThrows(QuotaExceededException.class, () -> client.me());
    }

    @Test
    void test500ThrowsServer() {
        server.enqueue(new MockResponse()
                .setResponseCode(500)
                .setBody("{\"error\":\"boom\"}"));

        assertThrows(ServerException.class, () -> client.me());
    }

    // ── Builder ─────────────────────────────────────────────────────────────

    @Test
    void testBuilderRequiresToken() {
        assertThrows(IllegalArgumentException.class,
                () -> new RTVortexClient.Builder().build());
    }
}
