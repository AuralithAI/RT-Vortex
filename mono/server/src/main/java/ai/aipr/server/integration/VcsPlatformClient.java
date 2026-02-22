package ai.aipr.server.integration;

import ai.aipr.server.dto.ReviewResponse;
import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * Platform-agnostic interface for VCS (Version Control System) operations.
 *
 * <p>All platform clients (GitHub, GitLab, Bitbucket, etc.) implement this
 * interface, enabling the review pipeline to work uniformly regardless of
 * the underlying platform.</p>
 *
 * <p>The {@code repoId} parameter is the platform's canonical repository
 * identifier:</p>
 * <ul>
 *   <li><b>GitHub</b> — {@code "owner/repo"}</li>
 *   <li><b>GitLab</b> — {@code "group/project"} (path with namespace)</li>
 *   <li><b>Bitbucket</b> — {@code "workspace/repo-slug"}</li>
 * </ul>
 *
 * <p>The {@code prNumber} is the platform's PR/MR identifier (GitHub PR number,
 * GitLab MR IID, Bitbucket PR ID).</p>
 */
public interface VcsPlatformClient {

    /**
     * Returns the platform name (e.g., "GitHub", "gitlab", "bitbucket").
     */
    @NotNull
    String platform();

    /**
     * Fetch the unified diff for a pull/merge request.
     *
     * @param repoId   the repository identifier
     * @param prNumber the PR/MR number
     * @return the unified diff content
     */
    @NotNull
    String getDiff(String repoId, int prNumber);

    /**
     * Fetch the list of changed file paths in a pull/merge request.
     *
     * @param repoId   the repository identifier
     * @param prNumber the PR/MR number
     * @return list of changed file paths (relative to repo root)
     */
    @NotNull
    List<String> getChangedFiles(String repoId, int prNumber);

    /**
     * Submit a full review (summary + inline comments) to the platform.
     *
     * @param repoId   the repository identifier
     * @param prNumber the PR/MR number
     * @param commitSha the HEAD commit SHA (used for positioning inline comments)
     * @param review   the review response from the AI engine
     */
    void submitReview(String repoId, int prNumber, String commitSha,
                      @NotNull ReviewResponse review);

    /**
     * Post a general comment (not inline) on a pull/merge request.
     * Used as a fallback when inline comment submission fails.
     *
     * @param repoId   the repository identifier
     * @param prNumber the PR/MR number
     * @param body     the comment body (markdown)
     */
    void postComment(String repoId, int prNumber, String body);
}

