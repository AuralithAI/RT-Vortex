package ai.aipr.server.session;

import ai.aipr.server.grpc.GrpcDataServiceDelegator;
import ai.aipr.server.model.*;
import ai.aipr.session.grpc.*;
import io.grpc.ManagedChannel;
import io.grpc.StatusRuntimeException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

/**
 * Implementation of {@link IUserSessionRemote} that wraps gRPC stubs.
 * 
 * <p>This class communicates with the server via gRPC, using the
 * {@link GrpcDataServiceDelegator} for connection management and routing.</p>
 */
public class UserSessionRemote implements IUserSessionRemote {

    private static final Logger log = LoggerFactory.getLogger(UserSessionRemote.class);

    private final String sessionToken;
    private final String channelId;
    private final GrpcDataServiceDelegator delegator;
    private final UserSessionRemoteGrpc.UserSessionRemoteBlockingStub blockingStub;
    private final UserSessionRemoteGrpc.UserSessionRemoteFutureStub futureStub;

    /**
     * Create a new UserSessionRemote.
     *
     * @param sessionToken The session token for authentication
     * @param channelId    The gRPC channel identifier
     * @param delegator    The gRPC delegator for connection management
     */
    public UserSessionRemote(String sessionToken, String channelId, 
                             GrpcDataServiceDelegator delegator) {
        this.sessionToken = sessionToken;
        this.channelId = channelId;
        this.delegator = delegator;
        
        ManagedChannel channel = delegator.getChannel();
        this.blockingStub = UserSessionRemoteGrpc.newBlockingStub(channel);
        this.futureStub = UserSessionRemoteGrpc.newFutureStub(channel);
    }

    // =========================================================================
    // Session Management
    // =========================================================================

    @Override
    public SessionInfo validateSession() {
        try {
            // Use the SessionService for validation
            SessionServiceGrpc.SessionServiceBlockingStub sessionStub = 
                SessionServiceGrpc.newBlockingStub(delegator.getChannel());
            
            ai.aipr.session.grpc.SessionInfo grpcInfo = sessionStub.getSession(
                GetSessionRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .build()
            );
            
            return convertSessionInfo(grpcInfo);
        } catch (StatusRuntimeException e) {
            log.warn("Session validation failed: {}", e.getStatus());
            return null;
        }
    }

    @Override
    public boolean heartbeat() {
        try {
            SessionServiceGrpc.SessionServiceBlockingStub sessionStub = 
                SessionServiceGrpc.newBlockingStub(delegator.getChannel());
            
            HeartbeatResponse response = sessionStub.heartbeat(
                HeartbeatRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setChannelId(channelId)
                    .build()
            );
            
            return response.getValid();
        } catch (StatusRuntimeException e) {
            log.warn("Heartbeat failed: {}", e.getStatus());
            return false;
        }
    }

    @Override
    public String getChannelId() {
        return channelId;
    }

    // =========================================================================
    // Review Operations
    // =========================================================================

    @Override
    public ReviewResponse submitReview(SessionContext context, ReviewRequest request) {
        try {
            SessionReviewResponse grpcResponse = blockingStub.submitReview(
                buildSessionReviewRequest(request)
            );
            return convertReviewResponse(grpcResponse);
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to submit review", e);
        }
    }

    @Override
    public CompletableFuture<ReviewResponse> submitReviewAsync(SessionContext context, 
                                                                ReviewRequest request) {
        CompletableFuture<ReviewResponse> future = new CompletableFuture<>();
        
        try {
            var grpcFuture = futureStub.submitReview(buildSessionReviewRequest(request));
            grpcFuture.addListener(() -> {
                try {
                    SessionReviewResponse grpcResponse = grpcFuture.get();
                    future.complete(convertReviewResponse(grpcResponse));
                } catch (Exception e) {
                    future.completeExceptionally(new RemoteOperationException("Async review failed", e));
                }
            }, Runnable::run);
        } catch (Exception e) {
            future.completeExceptionally(e);
        }
        
        return future;
    }

    @Override
    public ReviewResponse getReview(SessionContext context, String reviewId) {
        try {
            SessionReviewResponse grpcResponse = blockingStub.getReviewStatus(
                SessionGetReviewRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setReviewId(reviewId)
                    .build()
            );
            return convertReviewResponse(grpcResponse);
        } catch (StatusRuntimeException e) {
            if (e.getStatus().getCode() == io.grpc.Status.Code.NOT_FOUND) {
                return null;
            }
            throw new RemoteOperationException("Failed to get review", e);
        }
    }

    @Override
    public PagedResult<ReviewResponse> listReviews(SessionContext context, String repositoryId,
                                                    ReviewFilter filter, PageRequest page) {
        try {
            SessionListReviewsResponse grpcResponse = blockingStub.listReviews(
                SessionListReviewsRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .setStatus(filter != null ? filter.getStatus() : "")
                    .setPageSize(page.getSize())
                    .setPageToken(page.getToken() != null ? page.getToken() : "")
                    .build()
            );
            
            List<ReviewResponse> items = grpcResponse.getReviewsList().stream()
                .map(this::convertReviewResponse)
                .collect(Collectors.toList());
            
            return new PagedResult<>(items, grpcResponse.getNextPageToken(), 
                                     grpcResponse.getTotalCount());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to list reviews", e);
        }
    }

    @Override
    public OperationResult cancelReview(SessionContext context, String reviewId) {
        try {
            SessionCancelReviewResponse grpcResponse = blockingStub.cancelReview(
                SessionCancelReviewRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setReviewId(reviewId)
                    .build()
            );
            return new OperationResult(grpcResponse.getSuccess(), grpcResponse.getMessage());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to cancel review", e);
        }
    }

    // =========================================================================
    // Repository Operations
    // =========================================================================

    @Override
    public PagedResult<RepositoryInfo> listRepositories(SessionContext context, PageRequest page) {
        try {
            SessionListReposResponse grpcResponse = blockingStub.listRepositories(
                SessionListReposRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setPageSize(page.getSize())
                    .setPageToken(page.getToken() != null ? page.getToken() : "")
                    .build()
            );
            
            List<RepositoryInfo> items = grpcResponse.getRepositoriesList().stream()
                .map(this::convertRepositoryInfo)
                .collect(Collectors.toList());
            
            return new PagedResult<>(items, grpcResponse.getNextPageToken(), 
                                     grpcResponse.getTotalCount());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to list repositories", e);
        }
    }

    @Override
    public RepositoryInfo getRepository(SessionContext context, String repositoryId) {
        try {
            SessionRepoResponse grpcResponse = blockingStub.getRepository(
                SessionGetRepoRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .build()
            );
            return convertRepositoryInfo(grpcResponse.getRepository());
        } catch (StatusRuntimeException e) {
            if (e.getStatus().getCode() == io.grpc.Status.Code.NOT_FOUND) {
                return null;
            }
            throw new RemoteOperationException("Failed to get repository", e);
        }
    }

    @Override
    public IndexJobInfo triggerIndex(SessionContext context, String repositoryId, 
                                      boolean fullReindex) {
        try {
            SessionIndexResponse grpcResponse = blockingStub.triggerIndex(
                SessionIndexRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .setFullReindex(fullReindex)
                    .build()
            );
            return new IndexJobInfo(grpcResponse.getJobId(), grpcResponse.getStatus(), 
                                    grpcResponse.getMessage());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to trigger index", e);
        }
    }

    @Override
    public IndexStatusInfo getIndexStatus(SessionContext context, String repositoryId) {
        try {
            SessionIndexStatusResponse grpcResponse = blockingStub.getIndexStatus(
                SessionGetIndexStatusRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .build()
            );
            return convertIndexStatus(grpcResponse);
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to get index status", e);
        }
    }

    // =========================================================================
    // LLM Provider Operations
    // =========================================================================

    @Override
    public List<LLMProviderInfo> listLLMProviders(SessionContext context) {
        try {
            SessionListLLMResponse grpcResponse = blockingStub.listLLMProviders(
                SessionListLLMRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .build()
            );
            return grpcResponse.getProvidersList().stream()
                .map(this::convertLLMProvider)
                .collect(Collectors.toList());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to list LLM providers", e);
        }
    }

    @Override
    public LLMTestResult testLLMProvider(SessionContext context, String providerId) {
        try {
            SessionTestLLMResponse grpcResponse = blockingStub.testLLMConnection(
                SessionTestLLMRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setProviderId(providerId)
                    .build()
            );
            return new LLMTestResult(grpcResponse.getSuccess(), grpcResponse.getMessage(),
                                     grpcResponse.getLatencyMs(), 
                                     grpcResponse.getAvailableModelsList());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to test LLM provider", e);
        }
    }

    @Override
    public LLMProviderInfo configureLLMProvider(SessionContext context, LLMProviderConfig config) {
        try {
            SessionConfigureLLMResponse grpcResponse = blockingStub.configureLLMProvider(
                SessionConfigureLLMRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setConfigName(config.getName())
                    .setProviderType(config.getProviderType())
                    .setBaseUrl(config.getBaseUrl() != null ? config.getBaseUrl() : "")
                    .setApiKey(config.getApiKey() != null ? config.getApiKey() : "")
                    .setDefaultModel(config.getDefaultModel() != null ? config.getDefaultModel() : "")
                    .setSetAsDefault(config.isSetAsDefault())
                    .build()
            );
            
            if (!grpcResponse.getSuccess()) {
                throw new RemoteOperationException("Failed to configure LLM: " + grpcResponse.getMessage());
            }
            
            return new LLMProviderInfo(grpcResponse.getProviderId(), config.getName(),
                                       config.getProviderType(), config.isSetAsDefault(),
                                       true, List.of());
        } catch (StatusRuntimeException e) {
            throw new RemoteOperationException("Failed to configure LLM provider", e);
        }
    }

    // =========================================================================
    // Connection Management
    // =========================================================================

    @Override
    public boolean isConnected() {
        return delegator.isHealthy();
    }

    @Override
    public void reconnect() {
        delegator.reconnect();
    }

    @Override
    public void close() {
        try {
            delegator.shutdown(5, TimeUnit.SECONDS);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            log.warn("Interrupted while closing remote session");
        }
    }

    // =========================================================================
    // Converters
    // =========================================================================

    private SessionReviewRequest buildSessionReviewRequest(ReviewRequest request) {
        SessionReviewRequest.Builder builder = SessionReviewRequest.newBuilder()
            .setSessionToken(sessionToken)
            .setRepositoryId(request.getRepositoryId())
            .setPrNumber(request.getPrNumber());
        
        if (request.getHeadCommit() != null) {
            builder.setHeadCommit(request.getHeadCommit());
        }
        if (request.getBaseCommit() != null) {
            builder.setBaseCommit(request.getBaseCommit());
        }
        
        // Add review options if present
        if (request.getOptions() != null) {
            ReviewOptions.Builder optionsBuilder = ReviewOptions.newBuilder();
            if (request.getOptions().getCategories() != null) {
                optionsBuilder.addAllCategories(request.getOptions().getCategories());
            }
            if (request.getOptions().getMinSeverity() != null) {
                optionsBuilder.setMinSeverity(request.getOptions().getMinSeverity());
            }
            builder.setOptions(optionsBuilder.build());
        }
        
        return builder.build();
    }

    private SessionInfo convertSessionInfo(ai.aipr.session.grpc.SessionInfo grpc) {
        return new SessionInfo(
            grpc.getSessionId(),
            grpc.getUserId(),
            grpc.getUsername(),
            grpc.getGrpcChannelId(),
            grpc.getExpiresAt().getSeconds() * 1000
        );
    }

    private ReviewResponse convertReviewResponse(SessionReviewResponse grpc) {
        List<ReviewComment> comments = grpc.getCommentsList().stream()
            .map(c -> new ReviewComment(
                c.getId(), c.getFilePath(), c.getLine(), c.getEndLine(),
                c.getSeverity(), c.getCategory(), c.getMessage(),
                c.getSuggestion(), c.getConfidence()
            ))
            .collect(Collectors.toList());
        
        return new ReviewResponse(
            grpc.getReviewId(), grpc.getStatus(), grpc.getSummary(),
            comments, convertMetrics(grpc.getMetrics())
        );
    }

    private ai.aipr.server.model.ReviewMetrics convertMetrics(ReviewMetrics grpc) {
        return new ai.aipr.server.model.ReviewMetrics(
            grpc.getFilesAnalyzed(), grpc.getLinesAdded(), grpc.getLinesRemoved(),
            grpc.getTotalFindings(), grpc.getTokensUsed(), grpc.getLatencyMs()
        );
    }

    private RepositoryInfo convertRepositoryInfo(ai.aipr.session.grpc.RepositoryInfo grpc) {
        return new RepositoryInfo(
            grpc.getId(), grpc.getPlatform(), grpc.getOwner(), grpc.getName(),
            grpc.getDefaultBranch(), grpc.getRole(), grpc.getIndexed()
        );
    }

    private IndexStatusInfo convertIndexStatus(SessionIndexStatusResponse grpc) {
        return new IndexStatusInfo(
            grpc.getRepositoryId(), grpc.getIndexed(),
            grpc.getTotalFiles(), grpc.getIndexedFiles(), grpc.getTotalChunks(),
            grpc.getLastCommit(), grpc.getJobStatus(), grpc.getJobProgress()
        );
    }

    private LLMProviderInfo convertLLMProvider(LLMProviderConfig grpc) {
        return new LLMProviderInfo(
            grpc.getId(), grpc.getName(), grpc.getProviderType(),
            grpc.getIsDefault(), grpc.getIsConnected(),
            grpc.getAvailableModelsList()
        );
    }
}
