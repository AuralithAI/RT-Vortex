package ai.aipr.server.task;

/**
 * Contract for background tasks executed by the task scheduler.
 *
 * <p>Each implementation represents a single unit of periodic work
 * (session cleanup, index pruning, metrics aggregation, etc.).
 * Tasks run with superuser privileges — no user session is required.</p>
 *
 * <p>Implementations must be thread-safe: the scheduler may invoke
 * {@link #execute(TaskContext)} concurrently if a previous run overflows
 * into the next interval.</p>
 */
public interface IBackgroundTask {

    /**
     * Unique name for this task (used in logs, metrics, and the task registry).
     */
    String name();

    /**
     * Cron expression controlling execution frequency.
     * Uses standard Quartz cron format (6 fields: second minute hour dayOfMonth month dayOfWeek).
     *
     * <p>Examples:</p>
     * <ul>
     *   <li>{@code 0 0/15 * * * ?} — every 15 minutes</li>
     *   <li>{@code 0 0 * * * ?}    — every hour on the hour</li>
     *   <li>{@code 0 0 2 * * ?}    — daily at 2 AM</li>
     * </ul>
     */
    String cronExpression();

    /**
     * Execute the task.
     *
     * @param context provides access to logging, metrics, and cancellation
     * @return result describing what happened
     */
    TaskResult execute(TaskContext context);

    /**
     * Whether this task is enabled. Default true.
     * Implementations can read from config to disable at runtime.
     */
    default boolean isEnabled() {
        return true;
    }

    /**
     * Whether overlapping executions are allowed. Default false.
     * When false, the scheduler skips a run if the previous one is still in progress.
     */
    default boolean allowConcurrentExecution() {
        return false;
    }
}

