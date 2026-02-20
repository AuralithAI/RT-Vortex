package ai.aipr.server.model;

/**
 * Index job information.
 */
public class IndexJobInfo {
    
    private final String id;
    private final String status;
    private final String message;
    
    public IndexJobInfo(String id, String status, String message) {
        this.id = id;
        this.status = status;
        this.message = message;
    }
    
    public String getId() { return id; }
    public String getStatus() { return status; }
    public String getMessage() { return message; }
}
