package ai.aipr.server.api;

import ai.aipr.server.config.TestSecurityConfig;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.context.annotation.Import;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;

import java.util.UUID;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.delete;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.put;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for Admin API endpoints.
 * Uses real H2 database with schema-test.sql schema.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import(TestSecurityConfig.class)
class AdminControllerIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private JdbcTemplate jdbc;

    private String testUserId;

    @BeforeEach
    void setUp() {
        // Clean up
        jdbc.update("DELETE FROM subscription_history");
        jdbc.update("DELETE FROM user_sessions");
        jdbc.update("DELETE FROM users");

        // Insert a test user
        testUserId = UUID.randomUUID().toString();
        jdbc.update(
            "INSERT INTO users (id, platform, username, email, subscription_tier) VALUES (CAST(? AS UUID), ?, ?, ?, ?)",
            testUserId, "github", "testuser", "test@example.com", "FREE"
        );
    }

    // =====================================================================
    // Subscription Tier
    // =====================================================================

    @Nested
    @DisplayName("PUT /api/v1/admin/users/{userId}/tier")
    class UpdateUserTier {

        @Test
        @DisplayName("should update tier to PRO")
        void shouldUpdateTierToPro() throws Exception {
            mockMvc.perform(put("/api/v1/admin/users/{userId}/tier", testUserId)
                    .param("tier", "PRO")
                    .param("changedBy", "admin-test"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.userId").value(testUserId))
                    .andExpect(jsonPath("$.tier").value("PRO"))
                    .andExpect(jsonPath("$.changedBy").value("admin-test"));
        }

        @Test
        @DisplayName("should update tier to ENTERPRISE")
        void shouldUpdateTierToEnterprise() throws Exception {
            mockMvc.perform(put("/api/v1/admin/users/{userId}/tier", testUserId)
                    .param("tier", "enterprise"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.tier").value("ENTERPRISE"));
        }

        @Test
        @DisplayName("should reject invalid tier")
        void shouldRejectInvalidTier() throws Exception {
            mockMvc.perform(put("/api/v1/admin/users/{userId}/tier", testUserId)
                    .param("tier", "GOLD"))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").exists());
        }

        @Test
        @DisplayName("should default changedBy to admin")
        void shouldDefaultChangedByToAdmin() throws Exception {
            mockMvc.perform(put("/api/v1/admin/users/{userId}/tier", testUserId)
                    .param("tier", "PRO"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.changedBy").value("admin"));
        }
    }

    @Nested
    @DisplayName("GET /api/v1/admin/users/{userId}/tier")
    class GetUserTier {

        @Test
        @DisplayName("should return FREE tier for new user")
        void shouldReturnFreeTierForNewUser() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/tier", testUserId))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.userId").value(testUserId))
                    .andExpect(jsonPath("$.tier").value("FREE"));
        }

        @Test
        @DisplayName("should return FREE for non-existent user")
        void shouldReturnFreeForNonExistentUser() throws Exception {
            String fakeId = UUID.randomUUID().toString();
            mockMvc.perform(get("/api/v1/admin/users/{userId}/tier", fakeId))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.tier").value("FREE"));
        }
    }

    // =====================================================================
    // Session Management
    // =====================================================================

    @Nested
    @DisplayName("DELETE /api/v1/admin/sessions/{sessionToken}")
    class RevokeSession {

        @Test
        @DisplayName("should return revoked status")
        void shouldReturnRevokedStatus() throws Exception {
            mockMvc.perform(delete("/api/v1/admin/sessions/{sessionToken}", "some-token-123"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.status").value("revoked"));
        }
    }

    @Nested
    @DisplayName("DELETE /api/v1/admin/users/{userId}/sessions")
    class RevokeAllUserSessions {

        @Test
        @DisplayName("should return count of revoked sessions")
        void shouldReturnRevokedCount() throws Exception {
            mockMvc.perform(delete("/api/v1/admin/users/{userId}/sessions", testUserId))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.userId").value(testUserId))
                    .andExpect(jsonPath("$.sessionsRevoked").isNumber());
        }
    }

    // =====================================================================
    // Rate Limiting (Redis not available in tests — covers null-guard
    // paths and validation logic that runs before Redis is touched)
    // =====================================================================

    @Nested
    @DisplayName("Rate limit endpoints without Redis")
    class RateLimitWithoutRedis {

        @Test
        @DisplayName("DELETE rate-limit should return error when Redis not configured")
        void resetRateLimitShouldReturnErrorWithoutRedis() throws Exception {
            mockMvc.perform(delete("/api/v1/admin/users/{userId}/rate-limit", testUserId))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Redis not configured"));
        }

        @Test
        @DisplayName("GET rate-limit should return error when Redis not configured")
        void getRemainingTokensShouldReturnErrorWithoutRedis() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", testUserId)
                    .param("tier", "FREE"))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Redis not configured"));
        }

        @Test
        @DisplayName("GET rate-limit with PRO tier should return error when Redis not configured")
        void getRemainingTokensProTierShouldReturnErrorWithoutRedis() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", testUserId)
                    .param("tier", "PRO"))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Redis not configured"));
        }

        @Test
        @DisplayName("GET rate-limit with ENTERPRISE tier should return error when Redis not configured")
        void getRemainingTokensEnterpriseTierShouldReturnErrorWithoutRedis() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", testUserId)
                    .param("tier", "ENTERPRISE"))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Redis not configured"));
        }

        @Test
        @DisplayName("GET rate-limit without tier param should default to FREE and return Redis error")
        void getRemainingTokensDefaultTierShouldReturnErrorWithoutRedis() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", testUserId))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Redis not configured"));
        }

        @Test
        @DisplayName("DELETE rate-limit for unknown user should still return Redis error")
        void resetRateLimitForUnknownUserShouldReturnRedisError() throws Exception {
            mockMvc.perform(delete("/api/v1/admin/users/{userId}/rate-limit", "unknown-user-id"))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Redis not configured"));
        }
    }
}

