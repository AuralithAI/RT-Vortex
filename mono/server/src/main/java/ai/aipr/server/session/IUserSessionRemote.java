package ai.aipr.server.session;

import ai.aipr.server.model.IndexJobInfo;
import ai.aipr.server.model.IndexStatusInfo;
import ai.aipr.server.model.LLMProviderConfig;
import ai.aipr.server.model.LLMProviderInfo;
import ai.aipr.server.model.LLMTestResult;
import ai.aipr.server.model.OperationResult;
import ai.aipr.server.model.PagedResult;
import ai.aipr.server.model.PageRequest;
import ai.aipr.server.model.RepositoryInfo;
import ai.aipr.server.model.ReviewFilter;

import java.util.List;
import java.util.concurrent.CompletableFuture;

/**
 * Remote user session interface - maps to gRPC UserSessionRemote service.
 * 
 * <p>This interface defines the contract for remote session operations.
 * It is implemented by {@link UserSessionRemote} which wraps the gRPC stub.</p>
 */
public interface IUserSessionRemote {

    // =========================================================================
    // Session Management
    // =========================================================================

    /**
     * Validate the session and refresh TTL.
     *
     * @return Session info if valid, null otherwise
     */
    SessionInfo validateSession();

    /**
     * Send heartbeat to keep connection alive.
     *
     * @return true if session is still valid
     */
    boolean heartbeat();

    /**
     * Get the gRPC channel ID for this session.
     */
    String getChannelId();

    // =========================================================================
    // Review Operations
    // =========================================================================

    /**
     * Submit a review request.
     *
     * @param request The review request with session context
     * @return Review response from server
     */
    ReviewResponse submitReview(SessionContext context, ReviewRequest request);

    /**
     * Submit a review request asynchronously.
     */
    CompletableFuture<ReviewResponse> submitReviewAsync(SessionContext context, ReviewRequest request);

    /**
     * Get review by ID.
     */
    ReviewResponse getReview(SessionContext context, String reviewId);

    /**
     * List reviews with filtering.
     */
    PagedResult<ReviewResponse> listReviews(SessionContext context, String repositoryId, 
                                             ReviewFilter filter, PageRequest page);

    /**
     * Cancel a review.
     */
    OperationResult cancelReview(SessionContext context, String reviewId);

    // =========================================================================
    // Repository Operations
    // =========================================================================

    /**
     * List accessible repositories.
     */
    PagedResult<RepositoryInfo> listRepositories(SessionContext context, PageRequest page);

    /**
     * Get repository details.
     */
    RepositoryInfo getRepository(SessionContext context, String repositoryId);

    /**
     * Trigger repository indexing.
     */
    IndexJobInfo triggerIndex(SessionContext context, String repositoryId, boolean fullReindex);

    /**
     * Get index status.
     */
    IndexStatusInfo getIndexStatus(SessionContext context, String repositoryId);

    // =========================================================================
    // LLM Provider Operations
    // =========================================================================

    /**
     * List configured LLM providers.
     */
    List<LLMProviderInfo> listLLMProviders(SessionContext context);

    /**
     * Test LLM provider connection.
     */
    LLMTestResult testLLMProvider(SessionContext context, String providerId);

    /**
     * Configure an LLM provider.
     */
    LLMProviderInfo configureLLMProvider(SessionContext context, LLMProviderConfig config);

    // =========================================================================
    // Connection Management
    // =========================================================================

    /**
     * Check if the remote connection is healthy.
     */
    boolean isConnected();

    /**
     * Reconnect if disconnected.
     */
    void reconnect();

    /**
     * Close the remote connection.
     */
    void close();
}
