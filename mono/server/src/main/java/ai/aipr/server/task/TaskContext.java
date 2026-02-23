package ai.aipr.server.task;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.time.Duration;
import java.time.Instant;

/**
 * Context passed to each {@link IBackgroundTask#execute(TaskContext)} invocation.
 * Provides a logger scoped to the task name, start timestamp, and cancellation flag.
 */
public class TaskContext {

    private final String taskName;
    private final Logger log;
    private final Instant startTime;
    private volatile boolean cancelled;

    public TaskContext(String taskName) {
        this.taskName = taskName;
        this.log = LoggerFactory.getLogger("ai.aipr.task." + taskName);
        this.startTime = Instant.now();
    }

    public String taskName() { return taskName; }
    public Logger log() { return log; }
    public Instant startTime() { return startTime; }

    /** Check whether the scheduler has requested cancellation. */
    public boolean isCancelled() { return cancelled; }

    /** Request cancellation (called by the scheduler on shutdown). */
    public void cancel() { this.cancelled = true; }

    /** Elapsed milliseconds since the task started. */
    public long elapsedMs() {
        return Duration.between(startTime, Instant.now()).toMillis();
    }
}

