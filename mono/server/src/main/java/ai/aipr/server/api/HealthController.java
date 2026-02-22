package ai.aipr.server.api;

import ai.aipr.server.engine.EngineClient;
import ai.aipr.server.grpc.GrpcDataServiceDelegator;
import ai.aipr.server.llm.LLMClient;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

import java.time.Duration;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Health check endpoints for monitoring and orchestration.
 *
 * <p>Provides three tiers of health checks:</p>
 * <ul>
 *   <li>{@code /live} — Liveness probe: fast, JVM-only (for K8s liveness)</li>
 *   <li>{@code /ready} — Readiness probe: verifies engine gRPC connectivity (for K8s readiness)</li>
 *   <li>{@code /health} — Full health: engine + LLM component status (for dashboards)</li>
 *   <li>{@code /health/detailed} — Deep diagnostics: performs real gRPC health RPC + LLM ping</li>
 * </ul>
 */
@RestController
@RequestMapping("/api/v1/health")
public class HealthController {

    private static final Logger log = LoggerFactory.getLogger(HealthController.class);

    private static final String STATUS_KEY = "status";
    private static final String TIMESTAMP_KEY = "timestamp";

    private final Instant startTime = Instant.now();
    private final EngineClient engineClient;
    private final GrpcDataServiceDelegator delegator;
    private final LLMClient llmClient;

    public HealthController(EngineClient engineClient,
                            GrpcDataServiceDelegator delegator,
                            LLMClient llmClient) {
        this.engineClient = engineClient;
        this.delegator = delegator;
        this.llmClient = llmClient;
    }

    /**
     * Full health check endpoint.
     * Reports overall status plus component health using channel-level checks.
     *
     * @return health status with component details
     */
    @GetMapping
    public ResponseEntity<Map<String, Object>> health() {
        boolean channelUp = delegator.isHealthy();
        boolean engineRpcUp = channelUp && engineClient.isHealthy();
        String llmModel = llmClient.getModel();
        boolean llmConfigured = llmModel != null && !llmModel.isBlank();

        Map<String, Object> components = getStringObjectMap(channelUp, engineRpcUp, llmConfigured, llmModel);

        String overallStatus;
        if (engineRpcUp && llmConfigured) {
            overallStatus = "UP";
        } else if (!channelUp && !llmConfigured) {
            overallStatus = "DOWN";
        } else {
            overallStatus = "DEGRADED";
        }

        Map<String, Object> response = new LinkedHashMap<>();
        response.put(STATUS_KEY, overallStatus);
        response.put(TIMESTAMP_KEY, Instant.now().toString());
        response.put("uptime", Duration.between(startTime, Instant.now()).toSeconds());
        response.put("components", components);
        return ResponseEntity.ok(response);
    }

    @NotNull
    private Map<String, Object> getStringObjectMap(boolean channelUp, boolean engineRpcUp,
                                                    boolean llmConfigured, String llmModel) {
        Map<String, Object> engineStatus = new LinkedHashMap<>();
        engineStatus.put("status", engineRpcUp ? "UP" : channelUp ? "DEGRADED" : "DOWN");
        engineStatus.put("channelReady", channelUp);
        engineStatus.put("address", delegator.getServerAddress());

        Map<String, Object> llmStatus = new LinkedHashMap<>();
        llmStatus.put("status", llmConfigured ? "CONFIGURED" : "NOT_CONFIGURED");
        if (llmConfigured) {
            llmStatus.put("model", llmModel);
        }

        Map<String, Object> components = new LinkedHashMap<>();
        components.put("engine", engineStatus);
        components.put("llm", llmStatus);
        return components;
    }

    /**
     * Deep health check with actual gRPC and LLM connectivity verification.
     * Performs a real gRPC health check RPC to the engine and validates LLM reachability.
     * Use for diagnostics; avoid polling this frequently — it makes network calls.
     *
     * @return detailed health with latency info
     */
    @GetMapping("/detailed")
    public ResponseEntity<Map<String, Object>> detailed() {
        // Deep engine check: actual gRPC health RPC
        Map<String, Object> engineStatus = new LinkedHashMap<>();
        long engineStart = System.nanoTime();
        boolean engineServing = false;
        try {
            engineServing = delegator.healthCheck();
            long engineLatencyMs = (System.nanoTime() - engineStart) / 1_000_000;
            engineStatus.put("status", engineServing ? "SERVING" : "NOT_SERVING");
            engineStatus.put("latencyMs", engineLatencyMs);
            engineStatus.put("address", delegator.getServerAddress());
        } catch (Exception e) {
            long engineLatencyMs = (System.nanoTime() - engineStart) / 1_000_000;
            engineStatus.put("status", "ERROR");
            engineStatus.put("error", e.getMessage());
            engineStatus.put("latencyMs", engineLatencyMs);
            engineStatus.put("address", delegator.getServerAddress());
            log.warn("Engine deep health check failed: {}", e.getMessage());
        }

        // LLM config check (no actual API call — that costs money/tokens)
        Map<String, Object> llmStatus = new LinkedHashMap<>();
        String llmModel = llmClient.getModel();
        boolean llmConfigured = llmModel != null && !llmModel.isBlank();
        llmStatus.put("status", llmConfigured ? "CONFIGURED" : "NOT_CONFIGURED");
        if (llmConfigured) {
            llmStatus.put("model", llmModel);
        }

        Map<String, Object> components = new LinkedHashMap<>();
        components.put("engine", engineStatus);
        components.put("llm", llmStatus);

        String overallStatus;
        if (engineServing && llmConfigured) {
            overallStatus = "UP";
        } else if (!engineServing && !llmConfigured) {
            overallStatus = "DOWN";
        } else {
            overallStatus = "DEGRADED";
        }

        Map<String, Object> response = new LinkedHashMap<>();
        response.put(STATUS_KEY, overallStatus);
        response.put(TIMESTAMP_KEY, Instant.now().toString());
        response.put("uptime", Duration.between(startTime, Instant.now()).toSeconds());
        response.put("components", components);

        int httpStatus = engineServing ? 200 : 503;
        return ResponseEntity.status(httpStatus).body(response);
    }

    /**
     * Readiness probe — checks if the application is ready to serve traffic.
     * Verifies engine gRPC channel connectivity (fast, no RPC call).
     * If the channel is dead, attempts a reconnection.
     * Returns 503 if engine channel is not ready.
     *
     * @return readiness status
     */
    @GetMapping("/ready")
    public ResponseEntity<Map<String, Object>> ready() {
        boolean channelHealthy = delegator.isHealthy();

        // Attempt to reconnect if channel is down
        if (!channelHealthy) {
            log.warn("Engine channel not healthy, attempting reconnect...");
            try {
                delegator.reconnect();
                channelHealthy = delegator.isHealthy();
            } catch (Exception e) {
                log.error("Reconnect attempt failed: {}", e.getMessage());
            }
        }

        boolean engineHealthy = channelHealthy && engineClient.isHealthy();

        Map<String, Object> response = new LinkedHashMap<>();
        response.put(TIMESTAMP_KEY, Instant.now().toString());

        if (engineHealthy) {
            response.put(STATUS_KEY, "READY");
            return ResponseEntity.ok(response);
        } else {
            response.put(STATUS_KEY, "NOT_READY");
            response.put("channelReady", channelHealthy);
            response.put("reason", channelHealthy
                    ? "Engine gRPC health check failed"
                    : "Engine gRPC channel not in READY/IDLE state");
            response.put("address", delegator.getServerAddress());
            return ResponseEntity.status(503).body(response);
        }
    }

    /**
     * Liveness probe — checks if the application JVM is alive.
     * Intentionally cheap: no I/O, no dependency checks.
     * If this fails, the process should be restarted.
     *
     * @return liveness status
     */
    @GetMapping("/live")
    public ResponseEntity<Map<String, Object>> live() {
        Map<String, Object> response = new LinkedHashMap<>();
        response.put(STATUS_KEY, "ALIVE");
        response.put(TIMESTAMP_KEY, Instant.now().toString());
        return ResponseEntity.ok(response);
    }
}
