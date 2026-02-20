package ai.aipr.server.repository;

import ai.aipr.server.model.UserInfo;
import org.springframework.stereotype.Repository;

import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Repository for user sessions.
 */
@Repository
public class UserSessionRepository {
    
    private final ConcurrentHashMap<String, UserSession> sessionMap = new ConcurrentHashMap<>();
    
    public record UserSession(
            String sessionId,
            String userId,
            long createdAt,
            long lastAccessedAt,
            long expiresAt
    ) {}
    
    /**
     * Create a new session.
     */
    public UserSession createSession(String userId, long ttlMs) {
        String sessionId = java.util.UUID.randomUUID().toString();
        long now = System.currentTimeMillis();
        
        var session = new UserSession(
                sessionId,
                userId,
                now,
                now,
                now + ttlMs
        );
        
        sessionMap.put(sessionId, session);
        return session;
    }
    
    /**
     * Find a session by ID.
     */
    public Optional<UserSession> findById(String sessionId) {
        var session = sessionMap.get(sessionId);
        if (session == null) {
            return Optional.empty();
        }
        
        // Check expiration
        if (System.currentTimeMillis() > session.expiresAt()) {
            sessionMap.remove(sessionId);
            return Optional.empty();
        }
        
        return Optional.of(session);
    }
    
    /**
     * Update the last accessed time.
     */
    public void touch(String sessionId) {
        var existing = sessionMap.get(sessionId);
        if (existing != null && System.currentTimeMillis() < existing.expiresAt()) {
            var updated = new UserSession(
                    existing.sessionId(),
                    existing.userId(),
                    existing.createdAt(),
                    System.currentTimeMillis(),
                    existing.expiresAt()
            );
            sessionMap.put(sessionId, updated);
        }
    }
    
    /**
     * Invalidate a session.
     */
    public void invalidate(String sessionId) {
        sessionMap.remove(sessionId);
    }
    
    /**
     * Revoke a session (alias for invalidate).
     */
    public void revokeSession(String sessionToken) {
        // Session token is the session ID in this simple implementation
        sessionMap.remove(sessionToken);
    }
    
    /**
     * Find an active session by token.
     */
    public Optional<UserSession> findActiveSession(String sessionToken) {
        return findById(sessionToken);
    }
    
    /**
     * Revoke all sessions for a user.
     */
    public int revokeAllUserSessions(String userId) {
        return invalidateAllForUser(userId);
    }
    
    /**
     * Invalidate all sessions for a user.
     */
    public int invalidateAllForUser(String userId) {
        var toRemove = sessionMap.entrySet().stream()
                .filter(e -> e.getValue().userId().equals(userId))
                .map(e -> e.getKey())
                .toList();
        
        toRemove.forEach(sessionMap::remove);
        return toRemove.size();
    }
    
    /**
     * Clean up expired sessions.
     */
    public int cleanupExpired() {
        long now = System.currentTimeMillis();
        var toRemove = sessionMap.entrySet().stream()
                .filter(e -> now > e.getValue().expiresAt())
                .map(e -> e.getKey())
                .toList();
        
        toRemove.forEach(sessionMap::remove);
        return toRemove.size();
    }
}
