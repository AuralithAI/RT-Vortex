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

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.delete;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for Index API endpoints.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import(TestSecurityConfig.class)
class IndexControllerIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Nested
    @DisplayName("POST /api/v1/index/full")
    class FullIndex {

        @Test
        @DisplayName("should reject empty body")
        void shouldRejectEmptyBody() throws Exception {
            mockMvc.perform(post("/api/v1/index/full")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{}"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should reject missing repoId")
        void shouldRejectMissingRepoId() throws Exception {
            mockMvc.perform(post("/api/v1/index/full")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""
                        { "branch": "main" }
                        """))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should reject request without content type")
        void shouldRejectWithoutContentType() throws Exception {
            mockMvc.perform(post("/api/v1/index/full")
                    .content("{\"repoId\":\"test/repo\"}"))
                    .andExpect(status().isUnsupportedMediaType());
        }
    }

    @Nested
    @DisplayName("POST /api/v1/index/incremental")
    class IncrementalIndex {

        @Test
        @DisplayName("should reject empty body")
        void shouldRejectEmptyBody() throws Exception {
            mockMvc.perform(post("/api/v1/index/incremental")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{}"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should reject missing repoId")
        void shouldRejectMissingRepoId() throws Exception {
            mockMvc.perform(post("/api/v1/index/incremental")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""
                        { "sinceCommit": "abc123" }
                        """))
                    .andExpect(status().isBadRequest());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/index/status/{jobId}")
    class GetStatus {

        @Test
        @DisplayName("should return 404 for non-existent job")
        void shouldReturn404ForNonExistentJob() throws Exception {
            mockMvc.perform(get("/api/v1/index/status/{jobId}", "non-existent-job"))
                    .andExpect(status().isNotFound());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/index/info/{repoId}")
    class GetIndexInfo {

        @Test
        @DisplayName("should return 404 for non-indexed repo")
        void shouldReturn404ForNonIndexedRepo() throws Exception {
            mockMvc.perform(get("/api/v1/index/info/{repoId}", "unknown-repo"))
                    .andExpect(status().isNotFound());
        }
    }

    @Nested
    @DisplayName("DELETE /api/v1/index/{repoId}")
    class DeleteIndex {

        @Test
        @DisplayName("should return 204 on delete")
        void shouldReturn204OnDelete() throws Exception {
            mockMvc.perform(delete("/api/v1/index/{repoId}", "some-repo"))
                    .andExpect(status().isNoContent());
        }
    }
}

