package ai.aipr.server.session;

import ai.aipr.server.model.*;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.time.Instant;
import java.util.List;
import java.util.Objects;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;

/**
 * Implementation of {@link IUserSession}.
 * 
 * <p>This class manages the local session state and delegates all remote
 * operations to {@link IUserSessionRemote}.</p>
 * 
 * <p>Each user gets their own UserSession instance which maintains:</p>
 * <ul>
 *   <li>Session credentials and tokens</li>
 *   <li>User information</li>
 *   <li>Reference to the remote session for gRPC calls</li>
 * </ul>
 */
public class UserSession implements IUserSession {

    private static final Logger log = LoggerFactory.getLogger(UserSession.class);

    private final String sessionId;
    private final String sessionToken;
    private final UserInfo user;
    private final IUserSessionRemote remote;
    private final SessionContext context;

    private volatile long expiresAt;
    private volatile boolean closed = false;

    /**
     * Create a new UserSession.
     *
     * @param sessionId     Unique session identifier
     * @param sessionToken  Authentication token
     * @param user          User information
     * @param expiresAt     Expiration timestamp (millis since epoch)
     * @param remote        Remote session for gRPC operations
     */
    public UserSession(String sessionId, String sessionToken, UserInfo user, 
                       long expiresAt, IUserSessionRemote remote) {
        this.sessionId = Objects.requireNonNull(sessionId, "sessionId required");
        this.sessionToken = Objects.requireNonNull(sessionToken, "sessionToken required");
        this.user = Objects.requireNonNull(user, "user required");
        this.expiresAt = expiresAt;
        this.remote = Objects.requireNonNull(remote, "remote required");
        this.context = new SessionContext(sessionId, sessionToken, user.getId());
    }

    // =========================================================================
    // Session Information
    // =========================================================================

    @Override
    public String getSessionId() {
        return sessionId;
    }

    @Override
    public UserInfo getUser() {
        return user;
    }

    @Override
    public boolean isValid() {
        if (closed) {
            return false;
        }
        if (Instant.now().toEpochMilli() >= expiresAt) {
            return false;
        }
        // Optionally validate with server
        return remote.heartbeat();
    }

    @Override
    public long getExpiresAt() {
        return expiresAt;
    }

    @Override
    public void refresh() {
        ensureValid();
        SessionInfo info = remote.validateSession();
        if (info != null) {
            this.expiresAt = info.getExpiresAt();
            log.debug("Session {} refreshed, expires at {}", sessionId, expiresAt);
        }
    }

    // =========================================================================
    // Review Operations
    // =========================================================================

    @Override
    public ReviewResponse submitReview(ReviewRequest request) {
        ensureValid();
        log.info("Submitting review for repo={}, pr={}", 
                request.getRepositoryId(), request.getPrNumber());
        return remote.submitReview(context, request);
    }

    @Override
    public CompletableFuture<ReviewResponse> submitReviewAsync(ReviewRequest request) {
        ensureValid();
        log.info("Submitting async review for repo={}, pr={}", 
                request.getRepositoryId(), request.getPrNumber());
        return remote.submitReviewAsync(context, request);
    }

    @Override
    public Optional<ReviewResponse> getReview(String reviewId) {
        ensureValid();
        ReviewResponse response = remote.getReview(context, reviewId);
        return Optional.ofNullable(response);
    }

    @Override
    public List<ReviewResponse> listReviews(String repositoryId, ReviewFilter filter) {
        ensureValid();
        PagedResult<ReviewResponse> result = remote.listReviews(
            context, repositoryId, filter, PageRequest.DEFAULT);
        return result.getItems();
    }

    @Override
    public boolean cancelReview(String reviewId) {
        ensureValid();
        OperationResult result = remote.cancelReview(context, reviewId);
        return result.isSuccess();
    }

    // =========================================================================
    // Repository Operations
    // =========================================================================

    @Override
    public List<RepositoryInfo> listRepositories() {
        ensureValid();
        PagedResult<RepositoryInfo> result = remote.listRepositories(context, PageRequest.DEFAULT);
        return result.getItems();
    }

    @Override
    public Optional<RepositoryInfo> getRepository(String repositoryId) {
        ensureValid();
        RepositoryInfo info = remote.getRepository(context, repositoryId);
        return Optional.ofNullable(info);
    }

    @Override
    public IndexJobInfo triggerIndex(String repositoryId, boolean fullReindex) {
        ensureValid();
        log.info("Triggering {} index for repository {}", 
                fullReindex ? "full" : "incremental", repositoryId);
        return remote.triggerIndex(context, repositoryId, fullReindex);
    }

    @Override
    public IndexStatusInfo getIndexStatus(String repositoryId) {
        ensureValid();
        return remote.getIndexStatus(context, repositoryId);
    }

    // =========================================================================
    // LLM Provider Operations
    // =========================================================================

    @Override
    public List<LLMProviderInfo> listLLMProviders() {
        ensureValid();
        return remote.listLLMProviders(context);
    }

    @Override
    public LLMTestResult testLLMProvider(String providerId) {
        ensureValid();
        return remote.testLLMProvider(context, providerId);
    }

    @Override
    public LLMProviderInfo configureLLMProvider(LLMProviderConfig config) {
        ensureValid();
        log.info("Configuring LLM provider: {}", config.getName());
        return remote.configureLLMProvider(context, config);
    }

    // =========================================================================
    // Lifecycle
    // =========================================================================

    @Override
    public void logout() {
        if (!closed) {
            log.info("Logging out session {}", sessionId);
            try {
                // Server will mark session as revoked
                remote.close();
            } finally {
                closed = true;
            }
        }
    }

    @Override
    public void close() {
        logout();
    }

    // =========================================================================
    // Internal
    // =========================================================================

    private void ensureValid() {
        if (closed) {
            throw new SessionClosedException("Session has been closed");
        }
        if (Instant.now().toEpochMilli() >= expiresAt) {
            throw new SessionExpiredException("Session has expired");
        }
    }
}
