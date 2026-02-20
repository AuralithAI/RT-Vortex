package ai.aipr.server.service;

import ai.aipr.server.dto.LLMResponse;
import ai.aipr.server.llm.LLMClient;
import ai.aipr.server.model.LLMProviderConfig;
import ai.aipr.server.model.LLMProviderInfo;
import ai.aipr.server.model.LLMTestResult;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;

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
    
    private final LLMClient defaultClient;
    private final LLMProvidersConfig providersConfig;
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
     * Test LLM configuration.
     */
    public LLMTestResult testConfiguration(LLMProviderConfig config) {
        try {
            // Simple test prompt
            String testPrompt = "Say 'Hello' if you can read this.";
            
            // TODO: Create a temporary client with the test config
            LLMResponse response = defaultClient.complete(testPrompt);
            
            boolean success = response.content() != null && !response.content().isBlank();
            return new LLMTestResult(
                    success,
                    success ? "Configuration test successful" : "No response received",
                    (int) response.latencyMs(),
                    response.model() != null ? List.of(response.model()) : List.of()
            );
            
        } catch (Exception e) {
            log.error("LLM configuration test failed", e);
            return new LLMTestResult(false, "Test failed: " + e.getMessage(), 0, null);
        }
    }
    
    /**
     * Complete a prompt using the configured provider.
     */
    public LLMResponse complete(String userId, String providerId, String prompt, String systemPrompt) {
        // TODO: Use user-specific configuration if available
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
            // Return configured providers, or empty list if none configured
            // Users should configure their providers in application.yml
            return providers;
        }
        
        public void setProviders(List<LLMProviderInfo> providers) {
            this.providers = providers != null ? providers : new ArrayList<>();
        }
    }
}
