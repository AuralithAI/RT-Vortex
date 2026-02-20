package ai.aipr.server.api;

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
 */
@RestController
@RequestMapping("/api/v1/health")
public class HealthController {

    private static final String STATUS_KEY = "status";
    private static final String TIMESTAMP_KEY = "timestamp";

    private final Instant startTime = Instant.now();

    /**
     * Basic health check endpoint.
     *
     * @return health status
     */
    @GetMapping
    public ResponseEntity<Map<String, Object>> health() {
        Map<String, Object> response = new LinkedHashMap<>();
        response.put(STATUS_KEY, "UP");
        response.put(TIMESTAMP_KEY, Instant.now().toString());
        response.put("uptime", Duration.between(startTime, Instant.now()).toSeconds());
        return ResponseEntity.ok(response);
    }

    /**
     * Readiness probe - checks if the application is ready to serve traffic.
     *
     * @return readiness status
     */
    @GetMapping("/ready")
    public ResponseEntity<Map<String, Object>> ready() {
        Map<String, Object> response = new LinkedHashMap<>();
        response.put(STATUS_KEY, "READY");
        response.put(TIMESTAMP_KEY, Instant.now().toString());
        return ResponseEntity.ok(response);
    }

    /**
     * Liveness probe - checks if the application is alive.
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
