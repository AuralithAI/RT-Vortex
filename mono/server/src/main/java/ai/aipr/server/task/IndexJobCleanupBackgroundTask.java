package ai.aipr.server.task;

import ai.aipr.server.repository.IndexRepository;
import org.springframework.stereotype.Component;

/**
 * Cleans up completed/failed index jobs older than 7 days.
 * Runs every hour as superuser.
 */
@Component
public class IndexJobCleanupBackgroundTask implements IBackgroundTask {

    private static final long SEVEN_DAYS_MS = 7L * 24 * 60 * 60 * 1000;

    private final IndexRepository indexRepository;

    public IndexJobCleanupBackgroundTask(IndexRepository indexRepository) {
        this.indexRepository = indexRepository;
    }

    @Override
    public String name() {
        return "index-job-cleanup";
    }

    @Override
    public String cronExpression() {
        return "0 0 * * * ?";  // Every hour on the hour
    }

    @Override
    public TaskResult execute(TaskContext context) {
        try {
            int removed = indexRepository.cleanupOldJobs(SEVEN_DAYS_MS);
            if (removed > 0) {
                return TaskResult.success(removed, "Removed " + removed + " old index jobs");
            }
            return TaskResult.skipped("No old index jobs to clean");
        } catch (Exception e) {
            return TaskResult.failed("Index job cleanup error: " + e.getMessage());
        }
    }
}

