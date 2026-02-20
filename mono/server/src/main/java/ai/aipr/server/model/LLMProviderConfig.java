package ai.aipr.server.model;

import java.util.Map;

/**
 * LLM provider configuration for setup.
 */
public class LLMProviderConfig {
    
    private final String name;
    private final String providerType;
    private final String baseUrl;
    private final String apiKey;
    private final String defaultModel;
    private final boolean setAsDefault;
    private final Map<String, String> extraConfig;
    
    public LLMProviderConfig(String name, String providerType, String baseUrl, 
                             String apiKey, String defaultModel, boolean setAsDefault,
                             Map<String, String> extraConfig) {
        this.name = name;
        this.providerType = providerType;
        this.baseUrl = baseUrl;
        this.apiKey = apiKey;
        this.defaultModel = defaultModel;
        this.setAsDefault = setAsDefault;
        this.extraConfig = extraConfig;
    }
    
    public String getName() { return name; }
    public String getProviderType() { return providerType; }
    public String getBaseUrl() { return baseUrl; }
    public String getApiKey() { return apiKey; }
    public String getDefaultModel() { return defaultModel; }
    public boolean isSetAsDefault() { return setAsDefault; }
    public Map<String, String> getExtraConfig() { return extraConfig; }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String name;
        private String providerType;
        private String baseUrl;
        private String apiKey;
        private String defaultModel;
        private boolean setAsDefault;
        private Map<String, String> extraConfig;
        
        public Builder name(String name) {
            this.name = name;
            return this;
        }
        
        public Builder providerType(String providerType) {
            this.providerType = providerType;
            return this;
        }
        
        public Builder baseUrl(String baseUrl) {
            this.baseUrl = baseUrl;
            return this;
        }
        
        public Builder apiKey(String apiKey) {
            this.apiKey = apiKey;
            return this;
        }
        
        public Builder defaultModel(String defaultModel) {
            this.defaultModel = defaultModel;
            return this;
        }
        
        public Builder setAsDefault(boolean setAsDefault) {
            this.setAsDefault = setAsDefault;
            return this;
        }
        
        public Builder extraConfig(Map<String, String> extraConfig) {
            this.extraConfig = extraConfig;
            return this;
        }
        
        public LLMProviderConfig build() {
            return new LLMProviderConfig(name, providerType, baseUrl, apiKey, 
                                         defaultModel, setAsDefault, extraConfig);
        }
    }
}
