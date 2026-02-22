package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.time.Instant;

/**
 * Information about an existing index.
 */
public record IndexInfo(
        String repoId,
        String indexVersion,
        String commitSha,
        String branch,
        int fileCount,
        int chunkCount,
        int symbolCount,
        Instant lastIndexedAt,
        Instant createdAt,
        Instant updatedAt,
        IndexStats stats,
        IndexState state
) {
    @NotNull
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String repoId;
        private String indexVersion;
        private String commitSha;
        private String branch;
        private int fileCount;
        private int chunkCount;
        private int symbolCount;
        private Instant lastIndexedAt;
        private Instant createdAt;
        private Instant updatedAt;
        private IndexStats stats;
        private IndexState state;

        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder indexVersion(String indexVersion) { this.indexVersion = indexVersion; return this; }
        public Builder commitSha(String commitSha) { this.commitSha = commitSha; return this; }
        public Builder branch(String branch) { this.branch = branch; return this; }
        public Builder fileCount(int fileCount) { this.fileCount = fileCount; return this; }
        public Builder chunkCount(int chunkCount) { this.chunkCount = chunkCount; return this; }
        public Builder symbolCount(int symbolCount) { this.symbolCount = symbolCount; return this; }
        public Builder lastIndexedAt(Instant lastIndexedAt) { this.lastIndexedAt = lastIndexedAt; return this; }
        public Builder createdAt(Instant createdAt) { this.createdAt = createdAt; return this; }
        public Builder updatedAt(Instant updatedAt) { this.updatedAt = updatedAt; return this; }
        public Builder stats(IndexStats stats) { this.stats = stats; return this; }
        public Builder state(IndexState state) { this.state = state; return this; }

        public IndexInfo build() {
            return new IndexInfo(repoId, indexVersion, commitSha, branch, fileCount, chunkCount, symbolCount, lastIndexedAt, createdAt, updatedAt, stats, state);
        }
    }
}
