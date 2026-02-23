package ai.aipr.server.persistence;

import org.jetbrains.annotations.NotNull;
import org.springframework.dao.EmptyResultDataAccessException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Component;

import java.util.List;
import java.util.Optional;

/**
 * Thin data-access wrapper around {@link JdbcTemplate}.
 *
 * <p>Every repository injects this instead of raw {@code JdbcTemplate}.
 * This gives a single place to add cross-cutting concerns (logging,
 * metrics, tracing, connection-pool health) without touching every
 * repository class.</p>
 *
 * <p>Methods that return a single row use {@link Optional} instead of
 * throwing {@link EmptyResultDataAccessException}.</p>
 */
@Component
public class Persister {

    private final JdbcTemplate jdbc;

    public Persister(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    // =====================================================================
    // Single-row queries
    // =====================================================================

    /**
     * Query for a single object, returning {@link Optional#empty()} when no row matches.
     */
    public <T> Optional<T> queryForOptional(String sql, RowMapper<T> mapper, Object... args) {
        try {
            return Optional.ofNullable(jdbc.queryForObject(sql, mapper, args));
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    /**
     * Query for a single scalar value, returning {@link Optional#empty()} when no row matches.
     */
    public <T> Optional<T> queryForOptional(String sql, Class<T> type, Object... args) {
        try {
            return Optional.of(jdbc.queryForObject(sql, type, args));
        } catch (EmptyResultDataAccessException e) {
            return Optional.empty();
        }
    }

    /**
     * Query for a single scalar, returning a default if no row matches.
     */
    public <T> T queryScalar(String sql, Class<T> type, T defaultValue, Object... args) {
        return queryForOptional(sql, type, args).orElse(defaultValue);
    }

    // =====================================================================
    // Multi-row queries
    // =====================================================================

    /**
     * Query for a list of objects.
     */
    public <T> List<T> query(String sql, RowMapper<T> mapper, Object... args) {
        return jdbc.query(sql, mapper, args);
    }

    // =====================================================================
    // Writes
    // =====================================================================

    /**
     * Execute an INSERT / UPDATE / DELETE and return the number of affected rows.
     */
    public int update(String sql, Object... args) {
        return jdbc.update(sql, args);
    }

    /**
     * Call a stored function/procedure by name with positional arguments.
     * Uses {@code CALL function_name(?, ?, ...)} syntax which is compatible
     * with both PostgreSQL (via CREATE FUNCTION) and H2 (via CREATE ALIAS).
     *
     * @param functionName the function/procedure name
     * @param args         positional arguments
     */
    public void call(String functionName, @NotNull Object... args) {
        StringBuilder sql = new StringBuilder("CALL ");
        sql.append(functionName).append('(');
        for (int i = 0; i < args.length; i++) {
            if (i > 0) sql.append(", ");
            sql.append('?');
        }
        sql.append(')');
        jdbc.execute(sql.toString(), (java.sql.PreparedStatement ps) -> {
            for (int i = 0; i < args.length; i++) {
                ps.setObject(i + 1, args[i]);
            }
            ps.execute();
            return null;
        });
    }

    // =====================================================================
    // Access to raw JdbcTemplate (escape hatch)
    // =====================================================================

    /**
     * Returns the underlying {@link JdbcTemplate} for advanced operations
     * (e.g. batch updates, dynamic SQL building).
     */
    public JdbcTemplate jdbc() {
        return jdbc;
    }
}

