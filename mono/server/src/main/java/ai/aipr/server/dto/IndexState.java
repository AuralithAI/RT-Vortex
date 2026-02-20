package ai.aipr.server.dto;

/**
 * State of an indexing job.
 */
public enum IndexState {
    PENDING,
    RUNNING,
    COMPLETED,
    FAILED,
    CANCELLED
}
