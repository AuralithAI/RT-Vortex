package ai.aipr.server.llm;

import ai.aipr.server.config.Environment;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.Response;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Component;

import jakarta.annotation.PostConstruct;
import java.io.IOException;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Manages multiple LLM providers with automatic discovery and failover.
 *
 * <p>All configuration is read from {@code rtserverprops.xml} via
 * {@link Environment#server()}, under the {@code <llm>} section.</p>
 *
 * <p>Supported providers:</p>
 * <ul>
 *   <li><b>openai</b> — OpenAI API (GPT-4, etc.)</li>
 *   <li><b>anthropic</b> — Anthropic API (Claude)</li>
 *   <li><b>ollama</b> — Local Ollama instance</li>
 *   <li><b>azure-openai</b> — Azure OpenAI Service</li>
 *   <li><b>openai-compatible</b> — Any OpenAI-compatible API (vLLM, LMStudio, etc.)</li>
 * </ul>
 */
@Component
public class LLMProviderManager {

    private static final Logger log = LoggerFactory.getLogger(LLMProviderManager.class);

    private final OkHttpClient httpClient;
    private final ObjectMapper objectMapper;

    private final Map<String, ProviderInfo> providers = new ConcurrentHashMap<>();
    private volatile String activeProvider;

    private String primaryProvider;
    private String fallbackProvider;

    public LLMProviderManager() {
        this.httpClient = new OkHttpClient.Builder()
                .connectTimeout(Duration.ofSeconds(5))
                .readTimeout(Duration.ofSeconds(10))
                .build();
        this.objectMapper = new ObjectMapper();
    }

    @PostConstruct
    public void init() {
        log.info("Initializing LLM Provider Manager...");

        Environment.ConfigReader cfg = Environment.server();

        // Top-level LLM settings from <llm primary="..." fallback="..." ...>
        this.primaryProvider = cfg.get("llm.primary", "openai");
        this.fallbackProvider = cfg.get("llm.fallback", "");
        boolean autoDiscoverLocal = cfg.getBoolean("llm.auto-discover-local", true);

        // Register configured providers from XML sub-elements
        registerConfiguredProviders(cfg);

        // Auto-discover local Ollama
        if (autoDiscoverLocal) {
            String host = cfg.get("llm.ollama.discovery-host", "localhost");
            String portsStr = cfg.get("llm.ollama.discovery-ports", "11434,11435,8080");
            List<Integer> ports = Arrays.stream(portsStr.split(","))
                    .map(String::trim)
                    .filter(s -> !s.isEmpty())
                    .map(Integer::parseInt)
                    .toList();
            discoverOllama(host, ports);
        }

        // Set active provider
        activeProvider = primaryProvider;

        // Initial health check
        checkAllProviderHealth();

        log.info("LLM Provider Manager initialized. Active: {}, Available: {}",
                activeProvider, providers.keySet());
    }

    /**
     * Get the currently active provider.
     */
    public String getActiveProvider() {
        return activeProvider;
    }

    /**
     * Get provider configuration for the active provider.
     */
    public ProviderConfig getActiveConfig() {
        ProviderInfo info = providers.get(activeProvider);
        if (info == null) {
            throw new IllegalStateException("No active provider configured");
        }
        return info.config;
    }

    /**
     * Get all available providers with their status.
     */
    public List<ProviderStatus> getAllProviders() {
        List<ProviderStatus> result = new ArrayList<>();

        for (Map.Entry<String, ProviderInfo> entry : providers.entrySet()) {
            ProviderInfo info = entry.getValue();
            result.add(new ProviderStatus(
                    entry.getKey(),
                    info.healthy,
                    info.config.type(),
                    info.lastCheck,
                    info.lastError,
                    info.models,
                    entry.getKey().equals(activeProvider)
            ));
        }

        return result;
    }

    /**
     * Get available models for a provider.
     */
    public List<String> getModels(String provider) {
        ProviderInfo info = providers.get(provider);
        return info != null ? new ArrayList<>(info.models) : List.of();
    }

    /**
     * Manually switch to a different provider.
     */
    public void switchProvider(String provider) {
        if (!providers.containsKey(provider)) {
            throw new IllegalArgumentException("Unknown provider: " + provider);
        }

        ProviderInfo info = providers.get(provider);
        if (!info.healthy) {
            log.warn("Switching to unhealthy provider: {}", provider);
        }

        activeProvider = provider;
        log.info("Switched to LLM provider: {}", provider);
    }

    /**
     * Get provider config by name.
     */
    public Optional<ProviderConfig> getProviderConfig(String provider) {
        ProviderInfo info = providers.get(provider);
        return info != null ? Optional.of(info.config) : Optional.empty();
    }

    /**
     * Check if a specific provider is healthy.
     */
    public boolean isProviderHealthy(String provider) {
        ProviderInfo info = providers.get(provider);
        return info != null && info.healthy;
    }

    /**
     * Force refresh provider health status.
     */
    public void refreshHealth() {
        checkAllProviderHealth();
    }

    // =========================================================================
    // Health Checks (called by LLMHealthCheckBackgroundTask)
    // =========================================================================

    /**
     * Run health check on all providers. Called by the background task scheduler.
     */
    public void performHealthCheck() {
        checkAllProviderHealth();
    }

    // =========================================================================
    // Provider Registration
    // =========================================================================

    private void registerConfiguredProviders(@NotNull Environment.ConfigReader cfg) {
        // OpenAI — from <llm><openai api-key="..." base-url="..." .../>
        String openaiApiKey = cfg.get("llm.openai.api-key", "");
        if (!openaiApiKey.isBlank()) {
            registerProvider("openai", new ProviderConfig(
                    ProviderType.OPENAI,
                    cfg.get("llm.openai.base-url", "https://api.openai.com/v1"),
                    openaiApiKey,
                    null,
                    cfg.get("llm.openai.model")
            ));
        }

        // Anthropic — from <llm><anthropic api-key="..." base-url="..." models="..."/>
        String anthropicApiKey = cfg.get("llm.anthropic.api-key", "");
        if (!anthropicApiKey.isBlank()) {
            registerProvider("anthropic", new ProviderConfig(
                    ProviderType.ANTHROPIC,
                    cfg.get("llm.anthropic.base-url", "https://api.anthropic.com/v1"),
                    anthropicApiKey,
                    null,
                    null
            ));
        }

        // Azure OpenAI — from <llm><azure-openai endpoint="..." api-key="..." deployment="..."/>
        String azureEndpoint = cfg.get("llm.azure-openai.endpoint", "");
        String azureApiKey = cfg.get("llm.azure-openai.api-key", "");
        if (!azureEndpoint.isBlank() && !azureApiKey.isBlank()) {
            registerProvider("azure-openai", new ProviderConfig(
                    ProviderType.AZURE_OPENAI,
                    azureEndpoint,
                    azureApiKey,
                    cfg.get("llm.azure-openai.deployment"),
                    null
            ));
        }

        // Custom OpenAI-compatible — from <llm><custom base-url="..." api-key="..."/>
        String customBaseUrl = cfg.get("llm.custom.base-url", "");
        if (!customBaseUrl.isBlank()) {
            registerProvider("custom", new ProviderConfig(
                    ProviderType.OPENAI_COMPATIBLE,
                    customBaseUrl,
                    cfg.get("llm.custom.api-key"),
                    null,
                    null
            ));
        }

        // Ollama (explicit config) — from <llm><ollama base-url="..."/>
        String ollamaBaseUrl = cfg.get("llm.ollama.base-url", "");
        if (!ollamaBaseUrl.isBlank()) {
            registerProvider("ollama", new ProviderConfig(
                    ProviderType.OLLAMA,
                    ollamaBaseUrl,
                    null,
                    null,
                    null
            ));
        }
    }

    private void registerProvider(String name, ProviderConfig config) {
        providers.put(name, new ProviderInfo(config));
        log.info("Registered LLM provider: {} ({})", name, config.type());
    }

    // =========================================================================
    // Ollama Auto-Discovery
    // =========================================================================

    private void discoverOllama(String host, @NotNull List<Integer> ports) {
        log.info("Auto-discovering local Ollama instances...");

        for (int port : ports) {
            String baseUrl = "http://" + host + ":" + port;

            if (checkOllamaHealth(baseUrl)) {
                List<String> models = fetchOllamaModels(baseUrl);

                if (!models.isEmpty()) {
                    String providerName = "ollama-local" + (port == 11434 ? "" : "-" + port);

                    registerProvider(providerName, new ProviderConfig(
                            ProviderType.OLLAMA,
                            baseUrl,
                            null,
                            null,
                            models.getFirst()  // Default model
                    ));

                    providers.get(providerName).healthy = true;
                    providers.get(providerName).models = models;

                    log.info("Discovered Ollama at {}: {} models available", baseUrl, models.size());

                    // If no primary provider, use Ollama
                    if (primaryProvider == null || primaryProvider.isBlank() ||
                        !providers.containsKey(primaryProvider)) {
                        primaryProvider = providerName;
                        activeProvider = providerName;
                        log.info("Set Ollama as primary provider (no cloud provider configured)");
                    }
                }
            }
        }
    }

    private boolean checkOllamaHealth(String baseUrl) {
        try {
            Request request = new Request.Builder()
                    .url(baseUrl + "/api/tags")
                    .get()
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                return response.isSuccessful();
            }
        } catch (Exception e) {
            return false;
        }
    }

    @NotNull
    private List<String> fetchOllamaModels(String baseUrl) {
        List<String> models = new ArrayList<>();

        try {
            Request request = new Request.Builder()
                    .url(baseUrl + "/api/tags")
                    .get()
                    .build();

            try (Response response = httpClient.newCall(request).execute()) {
                if (response.isSuccessful() && response.body() != null) {
                    JsonNode json = objectMapper.readTree(response.body().string());
                    JsonNode modelsNode = json.path("models");

                    if (modelsNode.isArray()) {
                        for (JsonNode model : modelsNode) {
                            String name = model.path("name").asText();
                            if (!name.isBlank()) {
                                models.add(name);
                            }
                        }
                    }
                }
            }
        } catch (Exception e) {
            log.debug("Failed to fetch Ollama models from {}: {}", baseUrl, e.getMessage());
        }

        return models;
    }

    // =========================================================================
    // Health Checking
    // =========================================================================

    private void checkAllProviderHealth() {
        boolean primaryHealthy = false;

        for (Map.Entry<String, ProviderInfo> entry : providers.entrySet()) {
            String name = entry.getKey();
            ProviderInfo info = entry.getValue();

            try {
                boolean healthy = checkProviderHealth(info.config);
                info.healthy = healthy;
                info.lastCheck = System.currentTimeMillis();

                if (healthy) {
                    info.lastError = null;

                    // Fetch models if healthy
                    List<String> models = fetchModels(info.config);
                    if (!models.isEmpty()) {
                        info.models = models;
                    }

                    if (name.equals(primaryProvider)) {
                        primaryHealthy = true;
                    }
                }

            } catch (Exception e) {
                info.healthy = false;
                info.lastError = e.getMessage();
                log.warn("Provider {} health check failed: {}", name, e.getMessage());
            }
        }

        // Failover if primary is unhealthy
        if (!primaryHealthy && activeProvider.equals(primaryProvider)) {
            attemptFailover();
        } else if (primaryHealthy && !activeProvider.equals(primaryProvider)) {
            // Restore to primary if it's healthy again
            log.info("Primary provider {} is healthy again, restoring", primaryProvider);
            activeProvider = primaryProvider;
        }
    }

    private boolean checkProviderHealth(@NotNull ProviderConfig config) throws IOException {
        String healthUrl = switch (config.type()) {
            case OPENAI, OPENAI_COMPATIBLE -> config.baseUrl() + "/models";
            case ANTHROPIC -> config.baseUrl() + "/messages";  // Will fail but indicates service is up
            case AZURE_OPENAI -> config.baseUrl() + "/openai/deployments/" +
                    config.deployment() + "?api-version=2024-02-15-preview";
            case OLLAMA -> config.baseUrl() + "/api/tags";
        };

        Request.Builder requestBuilder = new Request.Builder().url(healthUrl).get();

        // Add auth headers
        switch (config.type()) {
            case OPENAI, OPENAI_COMPATIBLE:
                if (config.apiKey() != null) {
                    requestBuilder.header("Authorization", "Bearer " + config.apiKey());
                }
                break;
            case ANTHROPIC:
                requestBuilder.header("x-api-key", config.apiKey());
                requestBuilder.header("anthropic-version", "2023-06-01");
                break;
            case AZURE_OPENAI:
                requestBuilder.header("api-key", config.apiKey());
                break;
            case OLLAMA:
                // No auth needed
                break;
        }

        try (Response response = httpClient.newCall(requestBuilder.build()).execute()) {
            // For Anthropic, 401/405 means service is up, but we need proper auth
            return response.isSuccessful() ||
                   (config.type() == ProviderType.ANTHROPIC && response.code() < 500);
        }
    }

    @NotNull
    private List<String> fetchModels(ProviderConfig config) {
        List<String> models = new ArrayList<>();

        try {
            switch (config.type()) {
                case OPENAI, OPENAI_COMPATIBLE -> {
                    Request.Builder requestBuilder = new Request.Builder()
                            .url(config.baseUrl() + "/models")
                            .get();

                    if (config.apiKey() != null) {
                        requestBuilder.header("Authorization", "Bearer " + config.apiKey());
                    }

                    try (Response response = httpClient.newCall(requestBuilder.build()).execute()) {
                        if (response.isSuccessful() && response.body() != null) {
                            JsonNode json = objectMapper.readTree(response.body().string());
                            for (JsonNode model : json.path("data")) {
                                String id = model.path("id").asText();
                                if (id.contains("gpt") || id.contains("claude") ||
                                    id.contains("llama") || id.contains("mistral")) {
                                    models.add(id);
                                }
                            }
                        }
                    }
                }

                case OLLAMA -> models.addAll(fetchOllamaModels(config.baseUrl()));

                case ANTHROPIC -> {
                    String modelsStr = Environment.server().get("llm.anthropic.models",
                            "claude-3-opus-20240229,claude-3-sonnet-20240229,claude-3-haiku-20240307,claude-3-5-sonnet-20241022");
                    models.addAll(Arrays.asList(modelsStr.split(",")));
                }

                case AZURE_OPENAI -> {
                    if (config.deployment() != null) {
                        models.add(config.deployment());
                    }
                }
            }
        } catch (Exception e) {
            log.debug("Failed to fetch models for {}: {}", config.type(), e.getMessage());
        }

        return models;
    }

    private void attemptFailover() {
        // Try fallback provider first
        if (fallbackProvider != null && !fallbackProvider.isBlank()) {
            ProviderInfo fallback = providers.get(fallbackProvider);
            if (fallback != null && fallback.healthy) {
                log.warn("Primary provider {} unhealthy, failing over to {}",
                        primaryProvider, fallbackProvider);
                activeProvider = fallbackProvider;
                return;
            }
        }

        // Try any healthy provider
        for (Map.Entry<String, ProviderInfo> entry : providers.entrySet()) {
            if (entry.getValue().healthy && !entry.getKey().equals(primaryProvider)) {
                log.warn("Primary provider {} unhealthy, failing over to {}",
                        primaryProvider, entry.getKey());
                activeProvider = entry.getKey();
                return;
            }
        }

        log.error("No healthy LLM providers available!");
    }

    // =========================================================================
    // Data Classes
    // =========================================================================

    public enum ProviderType {
        OPENAI,
        ANTHROPIC,
        AZURE_OPENAI,
        OLLAMA,
        OPENAI_COMPATIBLE
    }

    public record ProviderConfig(
            ProviderType type,
            String baseUrl,
            String apiKey,
            String deployment,  // For Azure OpenAI
            String defaultModel
    ) {}

    public record ProviderStatus(
            String name,
            boolean healthy,
            ProviderType type,
            long lastCheck,
            String lastError,
            List<String> models,
            boolean isActive
    ) {}

    private static class ProviderInfo {
        final ProviderConfig config;
        volatile boolean healthy = false;
        volatile long lastCheck = 0;
        volatile String lastError = null;
        volatile List<String> models = new ArrayList<>();

        ProviderInfo(ProviderConfig config) {
            this.config = config;
        }
    }
}
