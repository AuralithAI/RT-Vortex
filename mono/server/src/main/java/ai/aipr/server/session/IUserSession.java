package ai.aipr.server.session;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.model.IndexJobInfo;
import ai.aipr.server.model.IndexStatusInfo;
import ai.aipr.server.model.LLMProviderConfig;
import ai.aipr.server.model.LLMProviderInfo;
import ai.aipr.server.model.LLMTestResult;
import ai.aipr.server.model.RepositoryInfo;
import ai.aipr.server.model.ReviewFilter;
import ai.aipr.server.model.UserInfo;

import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;

/**
 * Local user session interface.
 * 
 * <p>This is the client-side interface that users interact with directly.
 * It delegates all remote operations to {@link IUserSessionRemote} via gRPC.</p>
 */
public interface IUserSession extends AutoCloseable {

    // =========================================================================
    // Session Information
    // =========================================================================

    /**
     * Get the unique session ID.
     */
    String getSessionId();

    /**
     * Get the authenticated user information.
     */
    UserInfo getUser();

    /**
     * Check if the session is still valid.
     */
    boolean isValid();

    /**
     * Get session expiration time in milliseconds since epoch.
     */
    long getExpiresAt();

    /**
     * Refresh the session token to extend validity.
     */
    void refresh();

    // =========================================================================
    // Review Operations
    // =========================================================================

    /**
     * Submit a pull request for review.
     *
     * @param request The review request
     * @return The review response
     */
    ReviewResponse submitReview(ReviewRequest request);

    /**
     * Submit a review asynchronously.
     */
    CompletableFuture<ReviewResponse> submitReviewAsync(ReviewRequest request);

    /**
     * Get a review by ID.
     */
    Optional<ReviewResponse> getReview(String reviewId);

    /**
     * List reviews for a repository.
     */
    List<ReviewResponse> listReviews(String repositoryId, ReviewFilter filter);

    /**
     * Cancel a pending or running review.
     */
    boolean cancelReview(String reviewId);

    // =========================================================================
    // Repository Operations
    // =========================================================================

    /**
     * List accessible repositories.
     */
    List<RepositoryInfo> listRepositories();

    /**
     * Get repository details.
     */
    Optional<RepositoryInfo> getRepository(String repositoryId);

    /**
     * Trigger indexing for a repository.
     */
    IndexJobInfo triggerIndex(String repositoryId, boolean fullReindex);

    /**
     * Get index status for a repository.
     */
    IndexStatusInfo getIndexStatus(String repositoryId);

    // =========================================================================
    // LLM Provider Operations
    // =========================================================================

    /**
     * List configured LLM providers.
     */
    List<LLMProviderInfo> listLLMProviders();

    /**
     * Test connection to an LLM provider.
     */
    LLMTestResult testLLMProvider(String providerId);

    /**
     * Configure a new LLM provider.
     */
    LLMProviderInfo configureLLMProvider(LLMProviderConfig config);

    // =========================================================================
    // Lifecycle
    // =========================================================================

    /**
     * Invalidate and close this session.
     */
    void logout();

    /**
     * Close the session resources.
     */
    @Override
    void close();
}
