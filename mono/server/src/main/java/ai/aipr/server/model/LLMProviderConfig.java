package ai.aipr.server.model;

import java.util.Map;

/**
 * LLM provider configuration for setup.
 */
public record LLMProviderConfig(
        String providerId,
        String name,
        String providerType,
        String baseUrl,
        String apiKey,
        String defaultModel,
        boolean setAsDefault,
        Map<String, String> extraConfig
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String providerId;
        private String name;
        private String providerType;
        private String baseUrl;
        private String apiKey;
        private String defaultModel;
        private boolean setAsDefault;
        private Map<String, String> extraConfig;
        
        public Builder providerId(String providerId) { this.providerId = providerId; return this; }
        public Builder name(String name) { this.name = name; return this; }
        public Builder providerType(String providerType) { this.providerType = providerType; return this; }
        public Builder baseUrl(String baseUrl) { this.baseUrl = baseUrl; return this; }
        public Builder apiKey(String apiKey) { this.apiKey = apiKey; return this; }
        public Builder defaultModel(String defaultModel) { this.defaultModel = defaultModel; return this; }
        public Builder setAsDefault(boolean setAsDefault) { this.setAsDefault = setAsDefault; return this; }
        public Builder extraConfig(Map<String, String> extraConfig) { this.extraConfig = extraConfig; return this; }
        
        public LLMProviderConfig build() {
            return new LLMProviderConfig(providerId, name, providerType, baseUrl, apiKey, 
                                         defaultModel, setAsDefault, extraConfig);
        }
    }
}
