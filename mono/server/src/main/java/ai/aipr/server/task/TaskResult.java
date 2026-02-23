package ai.aipr.server.task;

import org.jetbrains.annotations.NotNull;

/**
 * Outcome of a single {@link IBackgroundTask} execution.
 */
public record TaskResult(
    Status status,
    int itemsProcessed,
    String message
) {
    public enum Status { SUCCESS, PARTIAL, SKIPPED, FAILED }

    @NotNull
    public static TaskResult success(int itemsProcessed, String message) {
        return new TaskResult(Status.SUCCESS, itemsProcessed, message);
    }

    @NotNull
    public static TaskResult success(String message) {
        return new TaskResult(Status.SUCCESS, 0, message);
    }

    @NotNull
    public static TaskResult skipped(String reason) {
        return new TaskResult(Status.SKIPPED, 0, reason);
    }

    @NotNull
    public static TaskResult failed(String error) {
        return new TaskResult(Status.FAILED, 0, error);
    }
}

