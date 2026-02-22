package ai.aipr.server.dto;


import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * Request to review a PR.
 */
public record ReviewRequest(
        @NotNull String repoId,
        @NotNull Integer prNumber,
        @NotNull String diff,
        String prTitle,
        String prDescription,
        String baseBranch,
        String headBranch,
        String headCommit,
        List<String> changedFiles,
        ReviewConfig config
) {
    @NotNull
    public static Builder builder() {
        return new Builder();
    }

    public static class Builder {
        private String repoId;
        private Integer prNumber;
        private String diff;
        private String prTitle;
        private String prDescription;
        private String baseBranch;
        private String headBranch;
        private String headCommit;
        private List<String> changedFiles;
        private ReviewConfig config;

        public Builder repoId(String repoId) { this.repoId = repoId; return this; }
        public Builder prNumber(Integer prNumber) { this.prNumber = prNumber; return this; }
        public Builder diff(String diff) { this.diff = diff; return this; }
        public Builder prTitle(String prTitle) { this.prTitle = prTitle; return this; }
        public Builder prDescription(String prDescription) { this.prDescription = prDescription; return this; }
        public Builder baseBranch(String baseBranch) { this.baseBranch = baseBranch; return this; }
        public Builder headBranch(String headBranch) { this.headBranch = headBranch; return this; }
        public Builder headCommit(String headCommit) { this.headCommit = headCommit; return this; }
        public Builder changedFiles(List<String> changedFiles) { this.changedFiles = changedFiles; return this; }
        public Builder config(ReviewConfig config) { this.config = config; return this; }

        public ReviewRequest build() {
            return new ReviewRequest(repoId, prNumber, diff, prTitle, prDescription,
                    baseBranch, headBranch, headCommit, changedFiles, config);
        }
    }
}
