package ai.aipr.server.session;

import ai.aipr.server.config.RedisConfig;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.boot.autoconfigure.condition.ConditionalOnBean;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.stereotype.Component;

import java.io.Serializable;
import java.time.Duration;
import java.time.Instant;
import java.util.Set;
import java.util.concurrent.TimeUnit;

/**
 * Redis-backed session cache for horizontal scaling.
 *
 * <p>Uses a secondary index ({@code aipr:user-sessions:<userId>}) to track
 * session keys per user, avoiding full-keyspace SCAN operations.</p>
 */
@Component
@ConditionalOnBean(RedisConfig.class)
public class RedisSessionStore {

    private static final Logger log = LoggerFactory.getLogger(RedisSessionStore.class);
    private static final String USER_SESSIONS_PREFIX = "aipr:user-sessions:";

    private final RedisTemplate<String, Object> redisTemplate;
    private final Duration defaultTtl;

    public RedisSessionStore(RedisTemplate<String, Object> redisTemplate) {
        this.redisTemplate = redisTemplate;
        this.defaultTtl = Duration.ofHours(24);
    }

    /**
     * Session data stored in Redis.
     */
    public record SessionData(
        String sessionId,
        String userId,
        String username,
        String grpcChannelId,
        long expiresAt,
        long createdAt,
        long lastActivityAt
    ) implements Serializable {

        public boolean isExpired() {
            return Instant.now().toEpochMilli() >= expiresAt;
        }

        @NotNull
        public SessionManager.ValidatedSession toValidatedSession() {
            return new SessionManager.ValidatedSession(
                sessionId, userId, username, grpcChannelId, expiresAt
            );
        }
    }

    /**
     * Store a session in Redis and register it in the user's session set.
     */
    public void put(String sessionToken, SessionData session) {
        String key = RedisConfig.Keys.session(sessionToken);

        try {
            redisTemplate.opsForValue().set(key, session);

            // Set TTL — use session expiration or fall back to defaultTtl
            long ttlSeconds = (session.expiresAt() - Instant.now().toEpochMilli()) / 1000;
            if (ttlSeconds <= 0) {
                ttlSeconds = defaultTtl.getSeconds();
            }
            redisTemplate.expire(key, ttlSeconds, TimeUnit.SECONDS);

            // Track session key in user's session set (secondary index)
            String userSetKey = USER_SESSIONS_PREFIX + session.userId();
            redisTemplate.opsForSet().add(userSetKey, sessionToken);
            redisTemplate.expire(userSetKey, ttlSeconds + 60, TimeUnit.SECONDS);

            log.debug("Stored session {} for user {}", session.sessionId(), session.userId());
        } catch (Exception e) {
            log.error("Failed to store session in Redis: {}", e.getMessage());
        }
    }

    /**
     * Get a session from Redis.
     */
    public SessionData get(String sessionToken) {
        String key = RedisConfig.Keys.session(sessionToken);

        try {
            Object value = redisTemplate.opsForValue().get(key);
            if (value instanceof SessionData session) {
                if (session.isExpired()) {
                    remove(sessionToken);
                    return null;
                }
                return session;
            }
            return null;
        } catch (Exception e) {
            log.error("Failed to get session from Redis: {}", e.getMessage());
            return null;
        }
    }

    /**
     * Remove a session from Redis and its user's session set.
     */
    public void remove(String sessionToken) {
        String key = RedisConfig.Keys.session(sessionToken);

        try {
            // Try to read session to remove from user set
            Object value = redisTemplate.opsForValue().get(key);
            if (value instanceof SessionData session) {
                String userSetKey = USER_SESSIONS_PREFIX + session.userId();
                redisTemplate.opsForSet().remove(userSetKey, sessionToken);
            }
            redisTemplate.delete(key);
            log.debug("Removed session with token {}...", sessionToken.substring(0, Math.min(8, sessionToken.length())));
        } catch (Exception e) {
            log.error("Failed to remove session from Redis: {}", e.getMessage());
        }
    }

    /**
     * Update last activity time for a session.
     */
    public void touch(String sessionToken) {
        SessionData existing = get(sessionToken);
        if (existing != null) {
            SessionData updated = new SessionData(
                existing.sessionId(),
                existing.userId(),
                existing.username(),
                existing.grpcChannelId(),
                existing.expiresAt(),
                existing.createdAt(),
                Instant.now().toEpochMilli()
            );
            put(sessionToken, updated);
        }
    }

    /**
     * Remove all sessions for a user using the secondary index (no keyspace SCAN).
     */
    public void removeAllForUser(String userId) {
        String userSetKey = USER_SESSIONS_PREFIX + userId;
        int removed = 0;

        try {
            Set<Object> tokens = redisTemplate.opsForSet().members(userSetKey);
            if (tokens != null) {
                for (Object token : tokens) {
                    String sessionKey = RedisConfig.Keys.session(token.toString());
                    redisTemplate.delete(sessionKey);
                    removed++;
                }
            }
            redisTemplate.delete(userSetKey);
            if (removed > 0) {
                log.info("Removed {} sessions for user {}", removed, userId);
            }
        } catch (Exception e) {
            log.error("Failed to remove sessions for user {}: {}", userId, e.getMessage());
        }
    }


    /**
     * Create a new SessionData instance.
     */
    @NotNull
    public static SessionData createSession(String sessionId, String userId, String username,
                                            String grpcChannelId, long expiresAt) {
        long now = Instant.now().toEpochMilli();
        return new SessionData(sessionId, userId, username, grpcChannelId, expiresAt, now, now);
    }
}
