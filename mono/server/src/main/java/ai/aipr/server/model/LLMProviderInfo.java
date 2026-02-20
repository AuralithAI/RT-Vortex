package ai.aipr.server.model;

import java.util.List;

/**
 * LLM provider information.
 */
public class LLMProviderInfo {
    
    private final String id;
    private final String name;
    private final String providerType;
    private final boolean isDefault;
    private final boolean connected;
    private final List<String> availableModels;
    
    public LLMProviderInfo(String id, String name, String providerType, 
                           boolean isDefault, boolean connected, List<String> availableModels) {
        this.id = id;
        this.name = name;
        this.providerType = providerType;
        this.isDefault = isDefault;
        this.connected = connected;
        this.availableModels = availableModels;
    }
    
    public String getId() { return id; }
    public String getName() { return name; }
    public String getProviderType() { return providerType; }
    public boolean isDefault() { return isDefault; }
    public boolean isConnected() { return connected; }
    public List<String> getAvailableModels() { return availableModels; }
}
