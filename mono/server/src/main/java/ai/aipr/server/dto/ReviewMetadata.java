package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.time.Instant;

/**
 * Metadata about the review process.
 */
public record ReviewMetadata(
        Instant startTime,
        Instant endTime,
        String engineVersion,
        String modelUsed,
        int contextChunksUsed,
        int tokensUsed
) {
    @NotNull public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private Instant startTime;
        private Instant endTime;
        private String engineVersion;
        private String modelUsed;
        private int contextChunksUsed;
        private int tokensUsed;

        public Builder startTime(Instant startTime) { this.startTime = startTime; return this; }
        public Builder endTime(Instant endTime) { this.endTime = endTime; return this; }
        public Builder engineVersion(String engineVersion) { this.engineVersion = engineVersion; return this; }
        public Builder modelUsed(String modelUsed) { this.modelUsed = modelUsed; return this; }
        public Builder contextChunksUsed(int contextChunksUsed) { this.contextChunksUsed = contextChunksUsed; return this; }
        public Builder tokensUsed(int tokensUsed) { this.tokensUsed = tokensUsed; return this; }

        public ReviewMetadata build() {
            return new ReviewMetadata(startTime, endTime, engineVersion,
                    modelUsed, contextChunksUsed, tokensUsed);
        }
    }
}
