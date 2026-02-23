package ai.aipr.server.test;

import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.SQLException;

/**
 * H2 user-defined functions that mirror PostgreSQL stored procedures/functions.
 * Used only in integration tests with H2 MODE=PostgreSQL.
 */
public final class H2Functions {

    private H2Functions() {}

    /**
     * Mirrors the PostgreSQL {@code update_subscription_tier} function.
     */
    public static void updateSubscriptionTier(Connection conn, String userId, String newTier,
                                               String changedBy, String reason) throws SQLException {
        String oldTier = "FREE";
        try (PreparedStatement ps = conn.prepareStatement(
                "SELECT subscription_tier FROM users WHERE id = CAST(? AS UUID)")) {
            ps.setString(1, userId);
            var rs = ps.executeQuery();
            if (rs.next()) {
                oldTier = rs.getString(1);
                if (oldTier == null) oldTier = "FREE";
            }
        }

        try (PreparedStatement ps = conn.prepareStatement(
                "UPDATE users SET subscription_tier = ?, updated_at = CURRENT_TIMESTAMP WHERE id = CAST(? AS UUID)")) {
            ps.setString(1, newTier);
            ps.setString(2, userId);
            ps.executeUpdate();
        }

        try (PreparedStatement ps = conn.prepareStatement(
                "INSERT INTO subscription_history (id, user_id, old_tier, new_tier, changed_by, reason) " +
                "VALUES (RANDOM_UUID(), CAST(? AS UUID), ?, ?, ?, ?)")) {
            ps.setString(1, userId);
            ps.setString(2, oldTier);
            ps.setString(3, newTier);
            ps.setString(4, changedBy);
            ps.setString(5, reason);
            ps.executeUpdate();
        }
    }
}

