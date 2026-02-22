package ai.aipr.server.integration.github;

import ai.aipr.server.integration.VcsPlatformClient;
import com.fasterxml.jackson.databind.JsonNode;

/**
 * GitHub-specific extension of {@link VcsPlatformClient}.
 *
 * <p>Adds methods unique to GitHub's API that are not part of the
 * platform-agnostic contract (e.g., fetching PR metadata for
 * issue_comment webhooks, posting individual line comments as fallback).</p>
 */
public interface GitHubPlatformClient extends VcsPlatformClient {

    /**
     * Get pull request metadata.
     * Needed when handling {@code issue_comment} webhooks, which don't include
     * full PR data in the payload — requires a separate API call.
     *
     * @param repoId   full repo name (e.g., "owner/repo")
     * @param prNumber the PR number
     * @return parsed JSON of the PR object
     */
    JsonNode getPullRequestInfo(String repoId, int prNumber);

    /**
     * Post an individual line comment on a pull request.
     * Used as a fallback when full review submission fails.
     *
     * @param repoId   full repo name
     * @param prNumber the PR number
     * @param commitId the commit SHA to comment on
     * @param path     the file path relative to repo root
     * @param line     the line number in the diff
     * @param body     the comment body (markdown)
     */
    void postLineComment(String repoId, int prNumber, String commitId,
                         String path, int line, String body);
}

