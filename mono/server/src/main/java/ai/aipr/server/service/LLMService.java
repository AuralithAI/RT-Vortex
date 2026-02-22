package ai.aipr.server.service;

import ai.aipr.server.dto.LLMResponse;
import ai.aipr.server.llm.LLMClient;
import ai.aipr.server.model.LLMProviderConfig;
import ai.aipr.server.model.LLMProviderInfo;
import ai.aipr.server.model.LLMTestResult;
import com.fasterxml.jackson.databind.ObjectMapper;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Service for managing LLM providers and configurations.
 *
 * <p>Provider definitions are loaded from application configuration rather than
 * being hardcoded, allowing new providers/models to be added without code changes.</p>
 *
 * <p>Configure in application.yml:</p>
 * <pre>
 * aipr:
 *   llm:
 *     providers:
 *       - id: openai
 *         name: OpenAI
 *         description: OpenAI GPT models
 *         provider-type: openai
 *         supports-streaming: true
 *         available-models: gpt-4-turbo-preview,gpt-4,gpt-3.5-turbo
 *       - id: anthropic
 *         name: Anthropic
 *         ...
 * </pre>
 */
@Service
public class LLMService {

    private static final Logger log = LoggerFactory.getLogger(LLMService.class);
    private static final MediaType JSON = MediaType.parse("application/json");

    private final LLMClient defaultClient;
    private final LLMProvidersConfig providersConfig;
    private final ObjectMapper objectMapper = new ObjectMapper();
    private final Map<String, LLMProviderConfig> userProviderConfigs = new ConcurrentHashMap<>();

    public LLMService(LLMClient defaultClient, LLMProvidersConfig providersConfig) {
        this.defaultClient = defaultClient;
        this.providersConfig = providersConfig;
        log.info("LLMService initialized with {} configured providers",
                providersConfig.getProviders().size());
    }

    /**
     * List available LLM providers.
     *
     * <p>Returns the list of providers configured in application properties.</p>
     */
    public List<LLMProviderInfo> listProviders() {
        return providersConfig.getProviders();
    }

    /**
     * Configure an LLM provider for a specific user.
     */
    public void configureProvider(String userId, LLMProviderConfig config) {
        String key = userId + ":" + config.providerId();
        userProviderConfigs.put(key, config);
        log.info("Configured LLM provider {} for user {}", config.providerId(), userId);
    }

    /**
     * Get provider configuration for a user.
     */
    public LLMProviderConfig getProviderConfig(String userId, String providerId) {
        return userProviderConfigs.get(userId + ":" + providerId);
    }

    /**
     * Test an LLM provider configuration by sending a real minimal request
     * against the provided {@code baseUrl}, {@code apiKey}, and {@code model}.
     *
     * <p>A temporary OkHttpClient is created per-test so the default client is
     * never used, and the caller's credentials are never cached.</p>
     */
    public LLMTestResult testConfiguration(LLMProviderConfig config) {
        if (config == null) {
            return new LLMTestResult(false, "No configuration provided", 0, List.of());
        }

        String baseUrl  = config.baseUrl()       != null ? config.baseUrl()       : "";
        String apiKey   = config.apiKey()        != null ? config.apiKey()        : "";
        String model    = config.defaultModel()  != null ? config.defaultModel()  : defaultClient.getModel();

        if (baseUrl.isBlank()) {
            return new LLMTestResult(false, "baseUrl is required", 0, List.of());
        }
        if (apiKey.isBlank()) {
            return new LLMTestResult(false, "apiKey is required", 0, List.of());
        }

        long startMs = System.currentTimeMillis();
        try {
            OkHttpClient tempClient = new OkHttpClient.Builder()
                    .connectTimeout(Duration.ofSeconds(10))
                    .readTimeout(Duration.ofSeconds(30))
                    .writeTimeout(Duration.ofSeconds(10))
                    .build();

            var messages = List.of(Map.of("role", "user", "content", "Say 'OK'"));
            var body = Map.of(
                    "model", model,
                    "messages", messages,
                    "max_tokens", 5,
                    "temperature", 0.0
            );

            String url = baseUrl.endsWith("/") ? baseUrl : baseUrl + "/";
            Request request = new Request.Builder()
                    .url(url + "chat/completions")
                    .header("Authorization", "Bearer " + apiKey)
                    .header("Content-Type", "application/json")
                    .post(RequestBody.create(objectMapper.writeValueAsString(body), JSON))
                    .build();

            try (var response = tempClient.newCall(request).execute()) {
                int latencyMs = (int) (System.currentTimeMillis() - startMs);
                String respBody = response.body() != null ? response.body().string() : "";

                if (!response.isSuccessful()) {
                    log.warn("LLM config test failed: status={}, body={}", response.code(), respBody);
                    return new LLMTestResult(false,
                            "Provider returned HTTP " + response.code(), latencyMs, List.of());
                }

                var node = objectMapper.readTree(respBody);
                String detectedModel = node.path("model").asText(model);
                boolean success = node.has("choices") && !node.get("choices").isEmpty();

                return new LLMTestResult(
                        success,
                        success ? "Configuration test successful" : "No choices in response",
                        latencyMs,
                        List.of(detectedModel)
                );
            }

        } catch (Exception e) {
            int latencyMs = (int) (System.currentTimeMillis() - startMs);
            log.error("LLM configuration test failed", e);
            return new LLMTestResult(false, "Test failed: " + e.getMessage(), latencyMs, List.of());
        }
    }

    /**
     * Complete a prompt using the configured provider for a user.
     * Falls back to the default client when no user-specific config is available.
     */
    public LLMResponse complete(String userId, String providerId, String prompt, String systemPrompt) {
        // TODO: instantiate a per-user LLMClient when user-specific config is present
        return defaultClient.complete(prompt, systemPrompt);
    }

    /**
     * Get the default LLM model.
     */
    public String getDefaultModel() {
        return defaultClient.getModel();
    }

    /**
     * Configuration properties for LLM providers.
     * Bean is created in LLMConfiguration.
     */
    public static class LLMProvidersConfig {

        private List<LLMProviderInfo> providers = new ArrayList<>();

        public List<LLMProviderInfo> getProviders() {
            return providers;
        }

        public void setProviders(List<LLMProviderInfo> providers) {
            this.providers = providers != null ? providers : new ArrayList<>();
        }
    }
}
