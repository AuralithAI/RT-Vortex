package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.time.Instant;
import java.util.List;

/**
 * Status of an indexing job.
 */
public record IndexStatus(
        String jobId,
        String repoId,
        IndexState state,
        int progress,
        String message,
        String error,
        int filesProcessed,
        Instant startTime,
        Instant endTime,
        IndexStats stats,
        List<String> errors
) {
    /**
     * Get the status string.
     */
    @NotNull
    public String status() {
        return state != null ? state.name() : "UNKNOWN";
    }

    /**
     * Check if the indexing is completed.
     */
    public boolean isCompleted() {
        return state == IndexState.COMPLETED;
    }

    /**
     * Get progress as float (0.0 to 1.0).
     */
    public float progressFloat() {
        return progress / 100.0f;
    }

    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String jobId;
        private String repoId;
        private IndexState state = IndexState.PENDING;
        private int progress = 0;
        private String message;
        private String error;
        private int filesProcessed = 0;
        private Instant startTime;
        private Instant endTime;
        private IndexStats stats;
        private List<String> errors = List.of();

        public Builder jobId(String jobId) { this.jobId = jobId; return this; }
        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder state(IndexState state) { this.state = state; return this; }
        public Builder progress(int progress) { this.progress = progress; return this; }
        public Builder message(String message) { this.message = message; return this; }
        public Builder error(String error) { this.error = error; return this; }
        public Builder filesProcessed(int filesProcessed) { this.filesProcessed = filesProcessed; return this; }
        public Builder chunksCreated(int chunksCreated) { return this; } // Ignored, use stats
        public Builder startTime(Instant startTime) { this.startTime = startTime; return this; }
        public Builder endTime(Instant endTime) { this.endTime = endTime; return this; }
        public Builder stats(IndexStats stats) { this.stats = stats; return this; }
        public Builder errors(List<String> errors) { this.errors = errors; return this; }

        public IndexStatus build() {
            return new IndexStatus(jobId, repoId, state, progress, message, error, filesProcessed, startTime, endTime, stats, errors);
        }
    }
}
