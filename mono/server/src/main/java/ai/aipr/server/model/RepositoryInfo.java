package ai.aipr.server.model;

/**
 * Repository information.
 * 
 * <p>Repository details are provided by the platform integration (GitHub, GitLab, etc.)
 * and not hardcoded. The URL is constructed based on the platform type and
 * can be customized for enterprise instances.</p>
 */
public record RepositoryInfo(
        String id,
        String platform,
        String owner,
        String name,
        String defaultBranch,
        String role,
        boolean indexed,
        String customBaseUrl  // Optional: for enterprise instances
) {
    // Constructor without customBaseUrl for backward compatibility
    public RepositoryInfo(String id, String platform, String owner, String name,
                          String defaultBranch, String role, boolean indexed) {
        this(id, platform, owner, name, defaultBranch, role, indexed, null);
    }
    
    /**
     * Get the full repository URL.
     * 
     * <p>Uses customBaseUrl if provided (for enterprise instances),
     * otherwise uses standard platform URLs.</p>
     */
    public String url() {
        if (customBaseUrl != null && !customBaseUrl.isEmpty()) {
            return customBaseUrl + "/" + owner + "/" + name;
        }
        
        // Standard platform URLs
        return switch (platform) {
            case "github" -> "https://github.com/" + owner + "/" + name;
            case "gitlab" -> "https://gitlab.com/" + owner + "/" + name;
            case "bitbucket" -> "https://bitbucket.org/" + owner + "/" + name;
            default -> owner + "/" + name;  // Just return the path for unknown platforms
        };
    }
    
    /**
     * Get the full name (owner/repo).
     */
    public String fullName() {
        return owner + "/" + name;
    }
    
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String id;
        private String platform;
        private String owner;
        private String name;
        private String defaultBranch;  // No default - should be provided by platform
        private String role = "viewer";
        private boolean indexed = false;
        private String customBaseUrl;
        
        public Builder id(String id) { this.id = id; return this; }
        public Builder platform(String platform) { this.platform = platform; return this; }
        public Builder owner(String owner) { this.owner = owner; return this; }
        public Builder name(String name) { this.name = name; return this; }
        public Builder defaultBranch(String defaultBranch) { this.defaultBranch = defaultBranch; return this; }
        public Builder role(String role) { this.role = role; return this; }
        public Builder indexed(boolean indexed) { this.indexed = indexed; return this; }
        public Builder customBaseUrl(String customBaseUrl) { this.customBaseUrl = customBaseUrl; return this; }
        
        public RepositoryInfo build() {
            return new RepositoryInfo(id, platform, owner, name, defaultBranch, role, indexed, customBaseUrl);
        }
    }
}
