package ai.aipr.server.model;

/**
 * User information.
 */
public class UserInfo {
    
    private final String id;
    private final String platform;
    private final String username;
    private final String email;
    private final String displayName;
    private final String avatarUrl;
    
    public UserInfo(String id, String platform, String username, 
                    String email, String displayName, String avatarUrl) {
        this.id = id;
        this.platform = platform;
        this.username = username;
        this.email = email;
        this.displayName = displayName;
        this.avatarUrl = avatarUrl;
    }
    
    public String getId() { return id; }
    public String getPlatform() { return platform; }
    public String getUsername() { return username; }
    public String getEmail() { return email; }
    public String getDisplayName() { return displayName; }
    public String getAvatarUrl() { return avatarUrl; }
}
