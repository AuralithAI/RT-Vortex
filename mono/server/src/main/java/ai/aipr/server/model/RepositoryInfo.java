package ai.aipr.server.model;

/**
 * Repository information.
 */
public class RepositoryInfo {
    
    private final String id;
    private final String platform;
    private final String owner;
    private final String name;
    private final String defaultBranch;
    private final String role;
    private final boolean indexed;
    
    public RepositoryInfo(String id, String platform, String owner, String name,
                          String defaultBranch, String role, boolean indexed) {
        this.id = id;
        this.platform = platform;
        this.owner = owner;
        this.name = name;
        this.defaultBranch = defaultBranch;
        this.role = role;
        this.indexed = indexed;
    }
    
    public String getId() { return id; }
    public String getPlatform() { return platform; }
    public String getOwner() { return owner; }
    public String getName() { return name; }
    public String getDefaultBranch() { return defaultBranch; }
    public String getRole() { return role; }
    public boolean isIndexed() { return indexed; }
    
    public String getFullName() {
        return owner + "/" + name;
    }
}
