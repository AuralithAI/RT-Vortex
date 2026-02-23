package ai.aipr.server.api;

import ai.aipr.server.config.TestEmbeddedRedisConfig;
import ai.aipr.server.config.TestSecurityConfig;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.context.annotation.Import;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.TestPropertySource;
import org.springframework.test.web.servlet.MockMvc;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.delete;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for Admin rate-limit endpoints with a real embedded Redis.
 *
 * <p>These tests exercise the happy paths of {@code resetRateLimit} and
 * {@code getRemainingTokens} — the code paths that require a live
 * {@code RateLimiterService} bean (backed by Redis).</p>
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import({TestSecurityConfig.class, TestEmbeddedRedisConfig.class})
@TestPropertySource(properties = {
    "spring.data.redis.enabled=true",
    "spring.data.redis.host=localhost",
    "spring.data.redis.port=16379",
    "spring.main.allow-bean-definition-overriding=true"
})
class AdminRateLimitIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    // =====================================================================
    // Reset Rate Limit
    // =====================================================================

    @Nested
    @DisplayName("DELETE /api/v1/admin/users/{userId}/rate-limit (with Redis)")
    class ResetRateLimit {

        @Test
        @DisplayName("should reset rate limit and return success")
        void shouldResetRateLimitSuccessfully() throws Exception {
            mockMvc.perform(delete("/api/v1/admin/users/{userId}/rate-limit", "user-123"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.userId").value("user-123"))
                    .andExpect(jsonPath("$.status").value("rate_limit_reset"));
        }

        @Test
        @DisplayName("should reset rate limit for any userId string")
        void shouldResetRateLimitForAnyUserId() throws Exception {
            mockMvc.perform(delete("/api/v1/admin/users/{userId}/rate-limit", "some-other-user"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.userId").value("some-other-user"))
                    .andExpect(jsonPath("$.status").value("rate_limit_reset"));
        }
    }

    // =====================================================================
    // Get Remaining Tokens
    // =====================================================================

    @Nested
    @DisplayName("GET /api/v1/admin/users/{userId}/rate-limit (with Redis)")
    class GetRemainingTokens {

        @Test
        @DisplayName("should return remaining tokens for FREE tier")
        void shouldReturnRemainingTokensForFreeTier() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-456")
                    .param("tier", "FREE"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.userId").value("user-456"))
                    .andExpect(jsonPath("$.tier").value("FREE"))
                    .andExpect(jsonPath("$.remainingTokens").isNumber())
                    .andExpect(jsonPath("$.capacity").value(60));
        }

        @Test
        @DisplayName("should return remaining tokens for PRO tier")
        void shouldReturnRemainingTokensForProTier() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-456")
                    .param("tier", "PRO"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.tier").value("PRO"))
                    .andExpect(jsonPath("$.remainingTokens").isNumber())
                    .andExpect(jsonPath("$.capacity").value(300));
        }

        @Test
        @DisplayName("should return remaining tokens for ENTERPRISE tier")
        void shouldReturnRemainingTokensForEnterpriseTier() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-456")
                    .param("tier", "ENTERPRISE"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.tier").value("ENTERPRISE"))
                    .andExpect(jsonPath("$.remainingTokens").isNumber())
                    .andExpect(jsonPath("$.capacity").value(1000));
        }

        @Test
        @DisplayName("should default to FREE tier when param omitted")
        void shouldDefaultToFreeTierWhenParamOmitted() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-789"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.tier").value("FREE"))
                    .andExpect(jsonPath("$.capacity").value(60));
        }

        @Test
        @DisplayName("should reject invalid tier with 400")
        void shouldRejectInvalidTier() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-456")
                    .param("tier", "GOLD"))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Invalid tier"));
        }

        @Test
        @DisplayName("should reject empty string tier with 400")
        void shouldRejectEmptyTier() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-456")
                    .param("tier", ""))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.error").value("Invalid tier"));
        }

        @Test
        @DisplayName("should handle case-insensitive tier names")
        void shouldHandleCaseInsensitiveTierNames() throws Exception {
            mockMvc.perform(get("/api/v1/admin/users/{userId}/rate-limit", "user-456")
                    .param("tier", "pro"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.tier").value("PRO"));
        }
    }
}

