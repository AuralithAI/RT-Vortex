package ai.aipr.server.model;

/**
 * User information.
 */
public record UserInfo(
        String id,
        String platform,
        String username,
        String email,
        String displayName,
        String avatarUrl
) {
    public static Builder builder() {
        return new Builder();
    }
    
    public static class Builder {
        private String id;
        private String platform;
        private String username;
        private String email;
        private String displayName;
        private String avatarUrl;
        
        public Builder id(String id) { this.id = id; return this; }
        public Builder platform(String platform) { this.platform = platform; return this; }
        public Builder username(String username) { this.username = username; return this; }
        public Builder email(String email) { this.email = email; return this; }
        public Builder displayName(String displayName) { this.displayName = displayName; return this; }
        public Builder avatarUrl(String avatarUrl) { this.avatarUrl = avatarUrl; return this; }
        
        public UserInfo build() {
            return new UserInfo(id, platform, username, email, displayName, avatarUrl);
        }
    }
}
