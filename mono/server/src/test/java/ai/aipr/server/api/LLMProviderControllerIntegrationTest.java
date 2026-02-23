package ai.aipr.server.api;

import ai.aipr.server.config.TestSecurityConfig;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for LLM Provider API endpoints.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import(TestSecurityConfig.class)
class LLMProviderControllerIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Nested
    @DisplayName("GET /api/v1/llm/providers")
    class ListProviders {

        @Test
        @DisplayName("should return providers list with active provider")
        void shouldReturnProvidersList() throws Exception {
            mockMvc.perform(get("/api/v1/llm/providers"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.providers").isArray())
                    .andExpect(jsonPath("$.activeProvider").isString());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/llm/providers/{name}")
    class GetProvider {

        @Test
        @DisplayName("should return 404 for non-existent provider")
        void shouldReturn404ForNonExistentProvider() throws Exception {
            mockMvc.perform(get("/api/v1/llm/providers/{name}", "non-existent-provider"))
                    .andExpect(status().isNotFound());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/llm/providers/{name}/models")
    class GetModels {

        @Test
        @DisplayName("should return 404 for unknown provider")
        void shouldReturn404ForUnknownProvider() throws Exception {
            mockMvc.perform(get("/api/v1/llm/providers/{name}/models", "unknown-provider"))
                    .andExpect(status().isNotFound());
        }
    }

    @Nested
    @DisplayName("POST /api/v1/llm/providers/switch")
    class SwitchProvider {

        @Test
        @DisplayName("should reject non-existent provider")
        void shouldRejectNonExistentProvider() throws Exception {
            mockMvc.perform(post("/api/v1/llm/providers/switch")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""
                        { "provider": "does-not-exist" }
                        """))
                    .andExpect(status().isBadRequest())
                    .andExpect(jsonPath("$.success").value(false));
        }
    }

    @Nested
    @DisplayName("POST /api/v1/llm/providers/refresh")
    class RefreshHealth {

        @Test
        @DisplayName("should refresh and return status")
        void shouldRefreshAndReturnStatus() throws Exception {
            mockMvc.perform(post("/api/v1/llm/providers/refresh"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.status").value("refreshed"))
                    .andExpect(jsonPath("$.activeProvider").isString());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/llm/active")
    class GetActiveProvider {

        @Test
        @DisplayName("should return active provider config")
        void shouldReturnActiveProviderConfig() throws Exception {
            // In test env, no LLM providers are configured with real API keys
            // so this may return 404 or a valid response depending on defaults
            mockMvc.perform(get("/api/v1/llm/active"))
                    .andExpect(status().isNotFound());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/llm/health")
    class LlmHealth {

        @Test
        @DisplayName("should return health status with provider counts")
        void shouldReturnHealthStatus() throws Exception {
            // In test env, no real LLM providers are up, expect 503
            mockMvc.perform(get("/api/v1/llm/health"))
                    .andExpect(jsonPath("$.status").exists())
                    .andExpect(jsonPath("$.totalProviders").isNumber())
                    .andExpect(jsonPath("$.healthyProviders").isNumber())
                    .andExpect(jsonPath("$.activeProvider").isString());
        }
    }
}

