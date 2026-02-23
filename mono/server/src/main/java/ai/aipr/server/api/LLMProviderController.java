package ai.aipr.server.api;

import ai.aipr.server.llm.LLMProviderManager;
import org.jetbrains.annotations.NotNull;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;
import java.util.Map;

/**
 * REST API for LLM provider management and status.
 *
 * <p>Provides endpoints to:</p>
 * <ul>
 *   <li>List all configured and discovered LLM providers</li>
 *   <li>Check provider health status</li>
 *   <li>Switch active provider</li>
 *   <li>List available models</li>
 * </ul>
 */
@RestController
@RequestMapping("/api/v1/llm")
public class LLMProviderController {

    private final LLMProviderManager providerManager;

    public LLMProviderController(LLMProviderManager providerManager) {
        this.providerManager = providerManager;
    }

    /**
     * List all LLM providers with their status.
     *
     * <p>Example response:</p>
     * <pre>
     * {
     *   "providers": [
     *     {
     *       "name": "openai",
     *       "type": "OPENAI",
     *       "healthy": true,
     *       "isActive": true,
     *       "models": ["gpt-4", "gpt-3.5-turbo"],
     *       "lastCheck": 1708704000000
     *     },
     *     {
     *       "name": "ollama-local",
     *       "type": "OLLAMA",
     *       "healthy": true,
     *       "isActive": false,
     *       "models": ["llama2:7b", "codellama:7b"],
     *       "lastCheck": 1708704000000
     *     }
     *   ],
     *   "activeProvider": "openai"
     * }
     * </pre>
     */
    @GetMapping("/providers")
    public ResponseEntity<ProvidersResponse> listProviders() {
        List<LLMProviderManager.ProviderStatus> providers = providerManager.getAllProviders();
        String active = providerManager.getActiveProvider();

        return ResponseEntity.ok(new ProvidersResponse(providers, active));
    }

    /**
     * Get status of a specific provider.
     */
    @GetMapping("/providers/{name}")
    public ResponseEntity<LLMProviderManager.ProviderStatus> getProvider(@PathVariable String name) {
        return providerManager.getAllProviders().stream()
                .filter(p -> p.name().equals(name))
                .findFirst()
                .map(ResponseEntity::ok)
                .orElse(ResponseEntity.notFound().build());
    }

    /**
     * Get available models for a provider.
     */
    @GetMapping("/providers/{name}/models")
    public ResponseEntity<ModelsResponse> getModels(@PathVariable String name) {
        List<String> models = providerManager.getModels(name);

        if (models.isEmpty() && !providerManager.isProviderHealthy(name)) {
            return ResponseEntity.notFound().build();
        }

        return ResponseEntity.ok(new ModelsResponse(name, models));
    }

    /**
     * Switch to a different LLM provider.
     *
     * <p>Request body:</p>
     * <pre>
     * {
     *   "provider": "ollama-local"
     * }
     * </pre>
     */
    @PostMapping("/providers/switch")
    public ResponseEntity<SwitchResponse> switchProvider(@NotNull @RequestBody SwitchRequest request) {
        try {
            String previousProvider = providerManager.getActiveProvider();
            providerManager.switchProvider(request.provider());

            return ResponseEntity.ok(new SwitchResponse(
                    true,
                    "Switched to " + request.provider(),
                    previousProvider,
                    request.provider()
            ));
        } catch (IllegalArgumentException e) {
            return ResponseEntity.badRequest().body(new SwitchResponse(
                    false,
                    e.getMessage(),
                    providerManager.getActiveProvider(),
                    null
            ));
        }
    }

    /**
     * Force refresh provider health checks.
     */
    @PostMapping("/providers/refresh")
    public ResponseEntity<Map<String, String>> refreshHealth() {
        providerManager.refreshHealth();
        return ResponseEntity.ok(Map.of(
                "status", "refreshed",
                "activeProvider", providerManager.getActiveProvider()
        ));
    }

    /**
     * Get the currently active provider.
     */
    @GetMapping("/active")
    public ResponseEntity<ActiveProviderResponse> getActiveProvider() {
        String active = providerManager.getActiveProvider();

        return providerManager.getProviderConfig(active)
                .map(config -> ResponseEntity.ok(new ActiveProviderResponse(
                        active,
                        config.type().name(),
                        config.baseUrl(),
                        config.defaultModel(),
                        providerManager.isProviderHealthy(active)
                )))
                .orElse(ResponseEntity.notFound().build());
    }

    /**
     * Health check endpoint for LLM subsystem.
     *
     * <p>Returns 200 if at least one provider is healthy, 503 otherwise.</p>
     */
    @GetMapping("/health")
    public ResponseEntity<HealthResponse> healthCheck() {
        List<LLMProviderManager.ProviderStatus> providers = providerManager.getAllProviders();

        long healthyCount = providers.stream().filter(LLMProviderManager.ProviderStatus::healthy).count();
        boolean isHealthy = healthyCount > 0;

        HealthResponse response = new HealthResponse(
                isHealthy ? "UP" : "DOWN",
                healthyCount,
                providers.size(),
                providerManager.getActiveProvider(),
                providerManager.isProviderHealthy(providerManager.getActiveProvider())
        );

        return isHealthy
                ? ResponseEntity.ok(response)
                : ResponseEntity.status(503).body(response);
    }

    // =========================================================================
    // Response DTOs
    // =========================================================================

    public record ProvidersResponse(
            List<LLMProviderManager.ProviderStatus> providers,
            String activeProvider
    ) {}

    public record ModelsResponse(
            String provider,
            List<String> models
    ) {}

    public record SwitchRequest(String provider) {}

    public record SwitchResponse(
            boolean success,
            String message,
            String previousProvider,
            String newProvider
    ) {}

    public record ActiveProviderResponse(
            String name,
            String type,
            String baseUrl,
            String defaultModel,
            boolean healthy
    ) {}

    public record HealthResponse(
            String status,
            long healthyProviders,
            long totalProviders,
            String activeProvider,
            boolean activeProviderHealthy
    ) {}
}
