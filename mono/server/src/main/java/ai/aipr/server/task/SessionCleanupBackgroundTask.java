package ai.aipr.server.task;

import ai.aipr.server.repository.UserSessionRepository;
import ai.aipr.server.session.SessionManager;
import org.springframework.stereotype.Component;

/**
 * Cleans up expired sessions from both the cache layer (Redis or in-memory)
 * and the database. Runs every 15 minutes as superuser.
 */
@Component
public class SessionCleanupBackgroundTask implements IBackgroundTask {

    private final SessionManager sessionManager;
    private final UserSessionRepository sessionRepository;

    public SessionCleanupBackgroundTask(SessionManager sessionManager,
                                        UserSessionRepository sessionRepository) {
        this.sessionManager = sessionManager;
        this.sessionRepository = sessionRepository;
    }

    @Override
    public String name() {
        return "session-cleanup";
    }

    @Override
    public String cronExpression() {
        return "0 0/15 * * * ?";  // Every 15 minutes
    }

    @Override
    public TaskResult execute(TaskContext context) {
        try {
            // Clean cache (Redis TTL or in-memory eviction)
            sessionManager.cleanupExpiredSessions();

            // Clean database — mark expired sessions
            int dbExpired = sessionRepository.cleanupExpired();

            if (dbExpired > 0) {
                return TaskResult.success(dbExpired, "Marked " + dbExpired + " expired sessions");
            }
            return TaskResult.skipped("No expired sessions found");
        } catch (Exception e) {
            return TaskResult.failed("Session cleanup error: " + e.getMessage());
        }
    }
}

