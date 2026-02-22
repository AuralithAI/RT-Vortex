package ai.aipr.server.session;

import ai.aipr.server.repository.UserSessionRepository;
import ai.aipr.server.repository.UserRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Manages user sessions, including creation, validation, and cleanup.
 */
@Service
public class SessionManager {

    private static final Logger log = LoggerFactory.getLogger(SessionManager.class);

    // Cache of validated sessions for fast lookups
    private final ConcurrentHashMap<String, ValidatedSession> sessionCache = new ConcurrentHashMap<>();

    private final UserSessionRepository sessionRepository;
    private final UserRepository userRepository;

    @Autowired
    public SessionManager(UserSessionRepository sessionRepository, 
                         UserRepository userRepository) {
        this.sessionRepository = sessionRepository;
        this.userRepository = userRepository;
    }

    /**
     * Validated session information.
     */
    public static class ValidatedSession {
        private final String sessionId;
        private final String userId;
        private final String username;
        private final String grpcChannelId;
        private final long expiresAt;

        public ValidatedSession(String sessionId, String userId, String username, 
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

        public boolean isExpired() {
            return Instant.now().toEpochMilli() >= expiresAt;
        }
    }

    /**
     * Create a new session for a user.
     *
     * @param userId        The user ID
     * @param platform      The platform (github, gitlab, etc.)
     * @param clientType    The type of client (sdk_java, sdk_python, cli, web)
     * @param clientVersion The version of the client
     * @return The created session
     */
    public ValidatedSession createSession(String userId, String platform, 
                                          String clientType, String clientVersion) {
        String sessionId = UUID.randomUUID().toString();
        String sessionToken = generateSecureToken();
        String grpcChannelId = UUID.randomUUID().toString();
        long expiresAt = Instant.now().plusSeconds(86400).toEpochMilli(); // 24 hours

        // Get user info
        var user = userRepository.findById(userId).orElse(null);
        String username = user != null ? user.username() : "unknown";

        // Store in repository
        sessionRepository.createSession(userId, 86400 * 1000L); // 24 hours in ms

        ValidatedSession session = new ValidatedSession(
            sessionId, userId, username, grpcChannelId, expiresAt
        );

        // Cache for fast access
        sessionCache.put(sessionToken, session);

        log.info("Created session {} for user {}", sessionId, userId);
        return session;
    }

    /**
     * Validate a session token and return session info if valid.
     *
     * @param sessionToken The session token to validate
     * @return The validated session, or null if invalid
     */
    public ValidatedSession validateSession(String sessionToken) {
        if (sessionToken == null || sessionToken.isEmpty()) {
            return null;
        }

        // Check cache first
        ValidatedSession cached = sessionCache.get(sessionToken);
        if (cached != null) {
            if (cached.isExpired()) {
                sessionCache.remove(sessionToken);
                return null;
            }
            return cached;
        }

        // Query repository
        var dbSessionOpt = sessionRepository.findActiveSession(sessionToken);
        if (dbSessionOpt.isEmpty()) {
            return null;
        }
        var dbSession = dbSessionOpt.get();

        // Get user info
        var user = userRepository.findById(dbSession.userId()).orElse(null);
        if (user == null) {
            return null;
        }

        ValidatedSession session = new ValidatedSession(
            dbSession.sessionId(),
            dbSession.userId(),
            user.username(),
            UUID.randomUUID().toString(),
            dbSession.expiresAt()
        );

        // Cache for future requests
        sessionCache.put(sessionToken, session);

        // Touch to update last activity
        sessionRepository.touch(dbSession.sessionId());

        return session;
    }

    /**
     * Revoke a session.
     */
    public void revokeSession(String sessionToken) {
        sessionCache.remove(sessionToken);
        sessionRepository.revokeSession(sessionToken);
        log.info("Revoked session with token {}", sessionToken.substring(0, 8) + "...");
    }

    /**
     * Revoke all sessions for a user.
     */
    public int revokeAllUserSessions(String userId) {
        // Remove from cache
        sessionCache.entrySet().removeIf(e -> e.getValue().getUserId().equals(userId));
        
        int count = sessionRepository.revokeAllUserSessions(userId);
        log.info("Revoked {} sessions for user {}", count, userId);
        return count;
    }

    /**
     * Cleanup expired sessions from cache.
     */
    public void cleanupExpiredSessions() {
        int removed = 0;
        for (var entry : sessionCache.entrySet()) {
            if (entry.getValue().isExpired()) {
                sessionCache.remove(entry.getKey());
                removed++;
            }
        }
        if (removed > 0) {
            log.debug("Cleaned up {} expired sessions from cache", removed);
        }
    }

    private String generateSecureToken() {
        // Generate a secure random token
        byte[] bytes = new byte[32];
        new java.security.SecureRandom().nextBytes(bytes);
        return java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
    }
}
