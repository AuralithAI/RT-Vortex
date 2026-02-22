package ai.aipr.server.model;

import java.util.List;

/**
 * LLM provider information.
 */
public record LLMProviderInfo(
        String id,
        String name,
        String providerType,
        List<String> availableModels,
        boolean isDefault,
        boolean isConnected,
        String description,
        boolean supportsStreaming
) {
    // Constructor for basic info
    public LLMProviderInfo(String id, String name, List<String> availableModels, boolean isDefault) {
        this(id, name, null, availableModels, isDefault, false, null, false);
    }
    
    // Additional constructor for backward compatibility
    public LLMProviderInfo(String id, String name, String providerType, 
                           boolean isDefault, boolean connected, List<String> availableModels) {
        this(id, name, providerType, availableModels, isDefault, connected, null, false);
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String id;
        private String name;
        private String providerType;
        private List<String> availableModels = List.of();
        private boolean isDefault = false;
        private boolean isConnected = false;
        private String description;
        private boolean supportsStreaming = false;
        
        public Builder id(String id) { this.id = id; return this; }
        public Builder name(String name) { this.name = name; return this; }
        public Builder providerType(String providerType) { this.providerType = providerType; return this; }
        public Builder availableModels(List<String> availableModels) { this.availableModels = availableModels; return this; }
        public Builder isDefault(boolean isDefault) { this.isDefault = isDefault; return this; }
        public Builder isConnected(boolean isConnected) { this.isConnected = isConnected; return this; }
        public Builder description(String description) { this.description = description; return this; }
        public Builder supportsStreaming(boolean supportsStreaming) { this.supportsStreaming = supportsStreaming; return this; }
        
        public LLMProviderInfo build() {
            return new LLMProviderInfo(id, name, providerType, availableModels, isDefault, 
                                        isConnected, description, supportsStreaming);
        }
    }
}
