package ai.aipr.server.api;

import ai.aipr.server.config.TestSecurityConfig;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for Health API endpoints.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import(TestSecurityConfig.class)
class HealthControllerIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Test
    @DisplayName("GET /api/v1/health should return 200 with status field")
    void healthEndpointShouldReturn200() throws Exception {
        mockMvc.perform(get("/api/v1/health")
                .contentType(MediaType.APPLICATION_JSON))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.status").exists())
                .andExpect(jsonPath("$.components").exists());
    }

    @Test
    @DisplayName("GET /api/v1/health should include timestamp and uptime")
    void healthEndpointShouldIncludeTimestampAndUptime() throws Exception {
        mockMvc.perform(get("/api/v1/health"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.timestamp").exists())
                .andExpect(jsonPath("$.uptime").isNumber());
    }

    @Test
    @DisplayName("GET /api/v1/health should include engine component")
    void healthEndpointShouldIncludeEngineComponent() throws Exception {
        mockMvc.perform(get("/api/v1/health"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.components.engine").exists())
                .andExpect(jsonPath("$.components.engine.status").exists());
    }

    @Test
    @DisplayName("GET /api/v1/health should include llm component")
    void healthEndpointShouldIncludeLlmComponent() throws Exception {
        mockMvc.perform(get("/api/v1/health"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.components.llm").exists())
                .andExpect(jsonPath("$.components.llm.status").exists());
    }

    @Test
    @DisplayName("GET /api/v1/health/ready should return readiness status")
    void readinessEndpointShouldReturnStatus() throws Exception {
        // In test environment without a running engine, readiness returns 503
        mockMvc.perform(get("/api/v1/health/ready")
                .contentType(MediaType.APPLICATION_JSON))
                .andExpect(jsonPath("$.status").exists())
                .andExpect(jsonPath("$.status").value("NOT_READY"));
    }

    @Test
    @DisplayName("GET /api/v1/health/ready should include reason when not ready")
    void readinessEndpointShouldIncludeReasonWhenNotReady() throws Exception {
        mockMvc.perform(get("/api/v1/health/ready"))
                .andExpect(jsonPath("$.status").value("NOT_READY"))
                .andExpect(jsonPath("$.reason").exists());
    }

    @Test
    @DisplayName("GET /api/v1/health/live should return liveness status")
    void livenessEndpointShouldReturnStatus() throws Exception {
        mockMvc.perform(get("/api/v1/health/live")
                .contentType(MediaType.APPLICATION_JSON))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.status").value("ALIVE"));
    }

    @Test
    @DisplayName("GET /api/v1/health/live should include timestamp")
    void livenessEndpointShouldIncludeTimestamp() throws Exception {
        mockMvc.perform(get("/api/v1/health/live"))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.timestamp").exists());
    }

    @Test
    @DisplayName("GET /api/v1/health/detailed should return deep health check")
    void detailedEndpointShouldReturnDeepCheck() throws Exception {
        mockMvc.perform(get("/api/v1/health/detailed"))
                .andExpect(jsonPath("$.status").exists())
                .andExpect(jsonPath("$.components").exists())
                .andExpect(jsonPath("$.components.engine").exists())
                .andExpect(jsonPath("$.components.llm").exists())
                .andExpect(jsonPath("$.uptime").isNumber());
    }

    @Test
    @DisplayName("GET /api/v1/health/detailed engine should report latency")
    void detailedEndpointShouldReportEngineLatency() throws Exception {
        mockMvc.perform(get("/api/v1/health/detailed"))
                .andExpect(jsonPath("$.components.engine.latencyMs").isNumber());
    }
}
