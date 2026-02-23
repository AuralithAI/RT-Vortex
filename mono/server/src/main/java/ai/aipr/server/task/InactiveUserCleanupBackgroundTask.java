package ai.aipr.server.task;

import ai.aipr.server.repository.UserRepository;
import ai.aipr.server.session.SessionManager;
import org.springframework.stereotype.Component;

import java.util.List;

/**
 * Revokes sessions for users who have not logged in for a configurable
 * number of days. Runs daily at 3 AM as superuser.
 *
 * <p>Uses a DB-side query to find inactive users (does not load all
 * users into memory). For each inactive user, revokes all active
 * sessions to free resources and enforce security hygiene.</p>
 */
@Component
public class InactiveUserCleanupBackgroundTask implements IBackgroundTask {

    /** Days since last login after which sessions are revoked. */
    private static final int INACTIVE_DAYS_THRESHOLD = 30;

    private final UserRepository userRepository;
    private final SessionManager sessionManager;

    public InactiveUserCleanupBackgroundTask(UserRepository userRepository,
                                             SessionManager sessionManager) {
        this.userRepository = userRepository;
        this.sessionManager = sessionManager;
    }

    @Override
    public String name() {
        return "inactive-user-cleanup";
    }

    @Override
    public String cronExpression() {
        return "0 0 3 * * ?";  // Daily at 3 AM
    }

    @Override
    public TaskResult execute(TaskContext context) {
        List<String> inactiveUserIds = userRepository.findInactiveUserIds(INACTIVE_DAYS_THRESHOLD);

        if (inactiveUserIds.isEmpty()) {
            return TaskResult.skipped("No inactive users found");
        }

        int totalRevoked = 0;
        for (String userId : inactiveUserIds) {
            if (context.isCancelled()) {
                return TaskResult.success(totalRevoked,
                    "Cancelled after revoking " + totalRevoked + " sessions for " + inactiveUserIds.size() + " users");
            }

            try {
                int revoked = sessionManager.revokeAllUserSessions(userId);
                totalRevoked += revoked;
                if (revoked > 0) {
                    context.log().debug("Revoked {} sessions for inactive user {}", revoked, userId);
                }
            } catch (Exception e) {
                context.log().warn("Failed to revoke sessions for user {}: {}", userId, e.getMessage());
            }
        }

        return TaskResult.success(totalRevoked,
            "Revoked " + totalRevoked + " sessions across " + inactiveUserIds.size() + " inactive users");
    }
}
