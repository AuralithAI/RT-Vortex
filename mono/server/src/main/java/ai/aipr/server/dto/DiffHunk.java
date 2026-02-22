package ai.aipr.server.dto;

import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * A hunk from a diff.
 */
public record DiffHunk(
        String filePath,
        int oldStart,
        int oldLines,
        int newStart,
        int newLines,
        String header,
        List<String> lines
) {
    @NotNull
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String filePath;
        private int oldStart;
        private int oldLines;
        private int newStart;
        private int newLines;
        private String header;
        private List<String> lines = List.of();

        public Builder filePath(String filePath) { this.filePath = filePath; return this; }
        public Builder oldStart(int oldStart) { this.oldStart = oldStart; return this; }
        public Builder oldLines(int oldLines) { this.oldLines = oldLines; return this; }
        public Builder newStart(int newStart) { this.newStart = newStart; return this; }
        public Builder newLines(int newLines) { this.newLines = newLines; return this; }
        public Builder header(String header) { this.header = header; return this; }
        public Builder lines(List<String> lines) { this.lines = lines; return this; }

        public DiffHunk build() {
            return new DiffHunk(filePath, oldStart, oldLines, newStart, newLines, header, lines);
        }
    }
}
