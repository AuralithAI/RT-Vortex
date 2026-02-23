package ai.aipr.server.repository;

import ai.aipr.server.persistence.Persister;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.sql.Timestamp;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for user sessions backed by PostgreSQL via {@link Persister}.
 */
@Repository
public class UserSessionRepository {

    private final Persister db;

    public UserSessionRepository(Persister db) {
        this.db = db;
    }

    public record UserSession(
            String sessionId,
            String userId,
            long createdAt,
            long lastAccessedAt,
            long expiresAt
    ) {}

    public UserSession createSession(String userId, long ttlMs) {
        String sessionToken = UUID.randomUUID().toString().replace("-", "")
                + UUID.randomUUID().toString().replace("-", "");
        String sessionId = UUID.randomUUID().toString();
        long now = System.currentTimeMillis();
        Timestamp createdTs = Timestamp.from(Instant.ofEpochMilli(now));
        Timestamp expiresTs = Timestamp.from(Instant.ofEpochMilli(now + ttlMs));

        db.update("""
            INSERT INTO user_sessions (id, user_id, session_token, status, created_at, expires_at, last_activity_at)
            VALUES (?::uuid, ?::uuid, ?, 'active', ?, ?, ?)
            """,
            sessionId, userId, sessionToken, createdTs, expiresTs, createdTs
        );

        return new UserSession(sessionToken, userId, now, now, now + ttlMs);
    }

    public Optional<UserSession> findById(String sessionToken) {
        Optional<UserSession> opt = db.queryForOptional(
            "SELECT * FROM user_sessions WHERE session_token = ? AND status = 'active'",
            SESSION_ROW_MAPPER, sessionToken);

        if (opt.isPresent() && System.currentTimeMillis() > opt.get().expiresAt()) {
            invalidate(sessionToken);
            return Optional.empty();
        }
        return opt;
    }

    public void touch(String sessionToken) {
        db.update(
            "UPDATE user_sessions SET last_activity_at = NOW() WHERE session_token = ? AND status = 'active'",
            sessionToken);
    }

    public void invalidate(String sessionToken) {
        db.update(
            "UPDATE user_sessions SET status = 'revoked', revoked_at = NOW() WHERE session_token = ?",
            sessionToken);
    }

    public void revokeSession(String sessionToken) {
        invalidate(sessionToken);
    }

    public Optional<UserSession> findActiveSession(String sessionToken) {
        return findById(sessionToken);
    }

    public int revokeAllUserSessions(String userId) {
        return invalidateAllForUser(userId);
    }

    public int invalidateAllForUser(String userId) {
        return db.update(
            "UPDATE user_sessions SET status = 'revoked', revoked_at = NOW() WHERE user_id = ?::uuid AND status = 'active'",
            userId);
    }

    public int cleanupExpired() {
        return db.update(
            "UPDATE user_sessions SET status = 'expired' WHERE status = 'active' AND expires_at < NOW()");
    }

    private static final RowMapper<UserSession> SESSION_ROW_MAPPER = (rs, rowNum) -> {
        Timestamp created = rs.getTimestamp("created_at");
        Timestamp lastActivity = rs.getTimestamp("last_activity_at");
        Timestamp expires = rs.getTimestamp("expires_at");
        return new UserSession(
            rs.getString("session_token"),
            rs.getString("user_id"),
            created != null ? created.getTime() : 0,
            lastActivity != null ? lastActivity.getTime() : 0,
            expires != null ? expires.getTime() : 0
        );
    };
}
