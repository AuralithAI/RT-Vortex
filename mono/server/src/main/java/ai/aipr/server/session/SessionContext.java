package ai.aipr.server.session;

/**
 * Holds session context for remote operations.
 */
public class SessionContext {
    
    private final String sessionId;
    private final String sessionToken;
    private final String userId;
    
    public SessionContext(String sessionId, String sessionToken, String userId) {
        this.sessionId = sessionId;
        this.sessionToken = sessionToken;
        this.userId = userId;
    }
    
    public String getSessionId() { return sessionId; }
    public String getSessionToken() { return sessionToken; }
    public String getUserId() { return userId; }
}
