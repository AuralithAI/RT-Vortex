package ai.aipr.server.session;

/**
 * Session information returned from validation.
 */
public class SessionInfo {
    
    private final String sessionId;
    private final String userId;
    private final String username;
    private final String grpcChannelId;
    private final long expiresAt;
    
    public SessionInfo(String sessionId, String userId, String username, 
                       String grpcChannelId, long expiresAt) {
        this.sessionId = sessionId;
        this.userId = userId;
        this.username = username;
        this.grpcChannelId = grpcChannelId;
        this.expiresAt = expiresAt;
    }
    
    public String getSessionId() { return sessionId; }
    public String getUserId() { return userId; }
    public String getUsername() { return username; }
    public String getGrpcChannelId() { return grpcChannelId; }
    public long getExpiresAt() { return expiresAt; }
}
