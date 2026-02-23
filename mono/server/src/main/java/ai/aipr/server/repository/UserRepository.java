package ai.aipr.server.repository;

import ai.aipr.server.model.UserInfo;
import ai.aipr.server.persistence.Persister;
import org.jetbrains.annotations.NotNull;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.sql.Timestamp;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for users backed by PostgreSQL via {@link Persister}.
 */
@Repository
public class UserRepository {

    private final Persister db;

    public UserRepository(Persister db) {
        this.db = db;
    }

    public void save(@NotNull UserInfo user) {
        String id = user.id() != null ? user.id() : UUID.randomUUID().toString();
        db.update("""
            INSERT INTO users (id, platform, username, email, display_name, avatar_url, updated_at)
            VALUES (?::uuid, ?, ?, ?, ?, ?, NOW())
            ON CONFLICT (id) DO UPDATE
              SET username     = EXCLUDED.username,
                  email        = EXCLUDED.email,
                  display_name = EXCLUDED.display_name,
                  avatar_url   = EXCLUDED.avatar_url,
                  updated_at   = NOW()
            """,
            id, user.platform(), user.username(), user.email(),
            user.displayName(), user.avatarUrl()
        );
    }

    public Optional<UserInfo> findById(String userId) {
        return db.queryForOptional("SELECT * FROM users WHERE id = ?::uuid", ROW_MAPPER, userId);
    }

    public Optional<UserInfo> findByEmail(String email) {
        return db.queryForOptional("SELECT * FROM users WHERE LOWER(email) = LOWER(?)", ROW_MAPPER, email);
    }

    public boolean existsById(String userId) {
        return db.queryScalar("SELECT COUNT(*) FROM users WHERE id = ?::uuid", Integer.class, 0, userId) > 0;
    }

    public boolean existsByEmail(String email) {
        return db.queryScalar("SELECT COUNT(*) FROM users WHERE LOWER(email) = LOWER(?)", Integer.class, 0, email) > 0;
    }

    public void deleteById(String userId) {
        db.update("DELETE FROM users WHERE id = ?::uuid", userId);
    }

    public List<UserInfo> findAll() {
        return db.query("SELECT * FROM users ORDER BY username", ROW_MAPPER);
    }

    public long count() {
        return db.queryScalar("SELECT COUNT(*) FROM users", Long.class, 0L);
    }

    /**
     * Get a user's subscription tier.
     *
     * @return tier string (FREE, PRO, ENTERPRISE), defaults to FREE if not found
     */
    public String findTierByUserId(String userId) {
        return db.queryScalar(
            "SELECT subscription_tier FROM users WHERE id = ?::uuid",
            String.class, "FREE", userId);
    }

    /**
     * Update a user's subscription tier (logs change in subscription_history).
     */
    public void updateTier(String userId, String newTier, String changedBy, String reason) {
        db.call("update_subscription_tier", userId, newTier, changedBy, reason);
    }

    /**
     * Find user IDs that have not logged in for the given number of days.
     * Uses DB-side filtering (not loading all users into memory).
     */
    public List<String> findInactiveUserIds(int inactiveDays) {
        return db.jdbc().queryForList(
            "SELECT id::text FROM users WHERE last_login_at IS NOT NULL AND last_login_at < NOW() - INTERVAL '1 day' * ?",
            String.class, inactiveDays);
    }

    private static final RowMapper<UserInfo> ROW_MAPPER = (rs, rowNum) -> {
        Timestamp lastLogin = rs.getTimestamp("last_login_at");
        return UserInfo.builder()
            .id(rs.getString("id"))
            .platform(rs.getString("platform"))
            .username(rs.getString("username"))
            .email(rs.getString("email"))
            .displayName(rs.getString("display_name"))
            .avatarUrl(rs.getString("avatar_url"))
            .lastLoginAt(lastLogin != null ? lastLogin.toInstant() : null)
            .build();
    };
}
