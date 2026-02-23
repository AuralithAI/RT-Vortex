package ai.aipr.server.session;

import ai.aipr.server.repository.UserSessionRepository;
import ai.aipr.server.repository.UserRepository;
import org.jetbrains.annotations.NotNull;
import org.jetbrains.annotations.Nullable;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.stereotype.Service;

import java.security.SecureRandom;
import java.time.Instant;
import java.util.Base64;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Manages user sessions, including creation, validation, and cleanup.
 * When Redis is available (RedisSessionStore is injected), sessions are stored
 * in Redis for horizontal scaling. Otherwise, falls back to in-memory cache.
 */
@Service
public class SessionManager {

    private static final Logger log = LoggerFactory.getLogger(SessionManager.class);

    private final ConcurrentHashMap<String, ValidatedSession> localSessionCache = new ConcurrentHashMap<>();
    @Nullable
    private final RedisSessionStore redisSessionStore;

    private final UserSessionRepository sessionRepository;
    private final UserRepository userRepository;

    public SessionManager(UserSessionRepository sessionRepository,
                          UserRepository userRepository,
                          @NotNull ObjectProvider<RedisSessionStore> redisSessionStoreProvider) {
        this.sessionRepository = sessionRepository;
        this.userRepository = userRepository;
        this.redisSessionStore = redisSessionStoreProvider.getIfAvailable();

        if (redisSessionStore != null) {
            log.info("SessionManager initialized with Redis-backed session store");
        } else {
            log.info("SessionManager initialized with in-memory session cache (single-instance mode)");
        }
    }

    /**
         * Validated session information.
         */
        public record ValidatedSession(String sessionId, String userId, String username, String grpcChannelId,
                                       long expiresAt) {

        public boolean isExpired() {
                return Instant.now().toEpochMilli() >= expiresAt;
            }
        }

    /**
     * Create a new session for a user.
     *
     * @param userId        The user ID
     * @param platform      The platform (GitHub, gitlab, etc.)
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

        // Cache for fast access - use Redis if available, otherwise local cache
        if (redisSessionStore != null) {
            RedisSessionStore.SessionData sessionData = RedisSessionStore.createSession(
                sessionId, userId, username, grpcChannelId, expiresAt
            );
            redisSessionStore.put(sessionToken, sessionData);
        } else {
            localSessionCache.put(sessionToken, session);
        }

        log.info("Created session {} for user {} [platform={}, client={}/{}]",
                sessionId, userId, platform, clientType, clientVersion);
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

        // Check Redis cache first if available
        if (redisSessionStore != null) {
            RedisSessionStore.SessionData cached = redisSessionStore.get(sessionToken);
            if (cached != null) {
                if (cached.isExpired()) {
                    redisSessionStore.remove(sessionToken);
                    return null;
                }
                return cached.toValidatedSession();
            }
        } else {
            // Check local cache
            ValidatedSession cached = localSessionCache.get(sessionToken);
            if (cached != null) {
                if (cached.isExpired()) {
                    localSessionCache.remove(sessionToken);
                    return null;
                }
                return cached;
            }
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
        if (redisSessionStore != null) {
            RedisSessionStore.SessionData sessionData = RedisSessionStore.createSession(
                session.sessionId(), session.userId(), session.username(),
                session.grpcChannelId(), session.expiresAt()
            );
            redisSessionStore.put(sessionToken, sessionData);
        } else {
            localSessionCache.put(sessionToken, session);
        }

        // Touch to update last activity in DB and Redis
        sessionRepository.touch(dbSession.sessionId());
        if (redisSessionStore != null) {
            redisSessionStore.touch(sessionToken);
        }

        return session;
    }

    /**
     * Revoke a session.
     */
    public void revokeSession(String sessionToken) {
        if (redisSessionStore != null) {
            redisSessionStore.remove(sessionToken);
        } else {
            localSessionCache.remove(sessionToken);
        }
        sessionRepository.revokeSession(sessionToken);
        log.info("Revoked session with token {}", sessionToken.substring(0, 8) + "...");
    }

    /**
     * Revoke all sessions for a user.
     */
    public int revokeAllUserSessions(String userId) {
        // Remove from cache
        if (redisSessionStore != null) {
            redisSessionStore.removeAllForUser(userId);
        } else {
            localSessionCache.entrySet().removeIf(e -> e.getValue().userId().equals(userId));
        }

        int count = sessionRepository.revokeAllUserSessions(userId);
        log.info("Revoked {} sessions for user {}", count, userId);
        return count;
    }

    /**
     * Cleanup expired sessions from cache.
     * Note: When using Redis, expiration is handled automatically via TTL.
     */
    public void cleanupExpiredSessions() {
        if (redisSessionStore != null) {
            // Redis handles TTL automatically
            log.debug("Redis session store handles expiration via TTL");
            return;
        }

        int removed = 0;
        for (var entry : localSessionCache.entrySet()) {
            if (entry.getValue().isExpired()) {
                localSessionCache.remove(entry.getKey());
                removed++;
            }
        }
        if (removed > 0) {
            log.debug("Cleaned up {} expired sessions from local cache", removed);
        }
    }

    private static final SecureRandom SECURE_RANDOM = new SecureRandom();

    private String generateSecureToken() {
        // Generate a secure random token
        byte[] bytes = new byte[32];
        SECURE_RANDOM.nextBytes(bytes);
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes);
    }
}
