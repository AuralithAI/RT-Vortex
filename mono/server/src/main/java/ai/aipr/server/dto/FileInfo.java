package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

/**
 * Information about a file in the repository.
 */
public record FileInfo(
        String path,
        String blobSha,
        String language,
        long sizeBytes,
        int lineCount,
        boolean isBinary,
        boolean isGenerated
) {
    @NotNull
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String path;
        private String blobSha;
        private String language;
        private long sizeBytes;
        private int lineCount;
        private boolean isBinary;
        private boolean isGenerated;

        public Builder path(String path) { this.path = path; return this; }
        public Builder blobSha(String blobSha) { this.blobSha = blobSha; return this; }
        public Builder language(String language) { this.language = language; return this; }
        public Builder sizeBytes(long sizeBytes) { this.sizeBytes = sizeBytes; return this; }
        public Builder lineCount(int lineCount) { this.lineCount = lineCount; return this; }
        public Builder isBinary(boolean isBinary) { this.isBinary = isBinary; return this; }
        public Builder isGenerated(boolean isGenerated) { this.isGenerated = isGenerated; return this; }

        public FileInfo build() {
            return new FileInfo(path, blobSha, language, sizeBytes, lineCount, isBinary, isGenerated);
        }
    }
}
