package ai.aipr.server.repository;

import ai.aipr.server.model.UserInfo;
import org.jetbrains.annotations.NotNull;
import org.springframework.dao.EmptyResultDataAccessException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for users backed by PostgreSQL via {@link JdbcTemplate}.
 * Maps to the {@code users} table created by {@code V2__user_sessions.sql}.
 */
@Repository
public class UserRepository {

    private final JdbcTemplate jdbc;

    public UserRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void save(@NotNull UserInfo user) {
        String id = user.id() != null ? user.id() : UUID.randomUUID().toString();
        jdbc.update("""
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
        try {
            return Optional.ofNullable(
                jdbc.queryForObject("SELECT * FROM users WHERE id = ?::uuid", ROW_MAPPER, userId));
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    public Optional<UserInfo> findByEmail(String email) {
        try {
            return Optional.ofNullable(
                jdbc.queryForObject("SELECT * FROM users WHERE LOWER(email) = LOWER(?)", ROW_MAPPER, email));
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    public boolean existsById(String userId) {
        Integer count = jdbc.queryForObject(
            "SELECT COUNT(*) FROM users WHERE id = ?::uuid", Integer.class, userId);
        return count != null && count > 0;
    }

    public boolean existsByEmail(String email) {
        Integer count = jdbc.queryForObject(
            "SELECT COUNT(*) FROM users WHERE LOWER(email) = LOWER(?)", Integer.class, email);
        return count != null && count > 0;
    }

    public void deleteById(String userId) {
        jdbc.update("DELETE FROM users WHERE id = ?::uuid", userId);
    }

    public List<UserInfo> findAll() {
        return jdbc.query("SELECT * FROM users ORDER BY username", ROW_MAPPER);
    }

    public long count() {
        Long c = jdbc.queryForObject("SELECT COUNT(*) FROM users", Long.class);
        return c != null ? c : 0;
    }

    private static final RowMapper<UserInfo> ROW_MAPPER = (rs, rowNum) ->
        UserInfo.builder()
            .id(rs.getString("id"))
            .platform(rs.getString("platform"))
            .username(rs.getString("username"))
            .email(rs.getString("email"))
            .displayName(rs.getString("display_name"))
            .avatarUrl(rs.getString("avatar_url"))
            .build();
}
