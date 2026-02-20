package ai.aipr.server.session;

import ai.aipr.server.config.Environment;
import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewMetrics;
import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.grpc.GrpcDataServiceDelegator;
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
import ai.aipr.session.grpc.ReviewOptions;
import ai.aipr.session.grpc.SessionCancelReviewRequest;
import ai.aipr.session.grpc.SessionCancelReviewResponse;
import ai.aipr.session.grpc.SessionConfigureLLMRequest;
import ai.aipr.session.grpc.SessionConfigureLLMResponse;
import ai.aipr.session.grpc.SessionGetIndexStatusRequest;
import ai.aipr.session.grpc.SessionGetRepoRequest;
import ai.aipr.session.grpc.SessionGetReviewRequest;
import ai.aipr.session.grpc.SessionIndexRequest;
import ai.aipr.session.grpc.SessionIndexResponse;
import ai.aipr.session.grpc.SessionIndexStatusResponse;
import ai.aipr.session.grpc.SessionListLLMRequest;
import ai.aipr.session.grpc.SessionListLLMResponse;
import ai.aipr.session.grpc.SessionListReposRequest;
import ai.aipr.session.grpc.SessionListReposResponse;
import ai.aipr.session.grpc.SessionListReviewsRequest;
import ai.aipr.session.grpc.SessionListReviewsResponse;
import ai.aipr.session.grpc.SessionRepoResponse;
import ai.aipr.session.grpc.SessionReviewRequest;
import ai.aipr.session.grpc.SessionReviewResponse;
import ai.aipr.session.grpc.SessionTestLLMRequest;
import ai.aipr.session.grpc.SessionTestLLMResponse;
import ai.aipr.session.grpc.UserSessionRemoteGrpc;
import com.google.common.util.concurrent.FutureCallback;
import com.google.common.util.concurrent.Futures;
import com.google.common.util.concurrent.ListenableFuture;
import com.google.common.util.concurrent.MoreExecutors;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import io.grpc.Status;
import io.grpc.StatusRuntimeException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;

/**
 * Remote user session implementation that communicates via gRPC.
 * 
 * <p>This implementation makes actual gRPC calls to the UserSessionRemote service
 * defined in session.proto. It handles conversion between Java model objects and
 * protobuf messages.</p>
 */
public class UserSessionRemote implements IUserSessionRemote {
    
    private static final Logger log = LoggerFactory.getLogger(UserSessionRemote.class);
    private static final int DEFAULT_TIMEOUT_SECONDS = 30;
    
    private final String sessionToken;
    private final String channelId;
    private final String serverAddress;
    private ManagedChannel channel;
    private UserSessionRemoteGrpc.UserSessionRemoteBlockingStub blockingStub;
    private UserSessionRemoteGrpc.UserSessionRemoteFutureStub futureStub;
    private volatile boolean connected = false;
    
    /**
     * Create a remote session with explicit server address.
     *
     * @param sessionToken Session authentication token
     * @param channelId gRPC channel identifier
     * @param serverAddress Server address in format "host:port"
     */
    public UserSessionRemote(String sessionToken, String channelId, String serverAddress) {
        this.sessionToken = sessionToken;
        this.channelId = channelId;
        this.serverAddress = serverAddress != null ? serverAddress : getDefaultServerAddress();
        connect(this.serverAddress);
    }
    
    /**
     * Create a remote session using a delegator for address configuration.
     *
     * @param sessionToken Session authentication token
     * @param channelId gRPC channel identifier
     * @param delegator Service delegator with server configuration
     */
    public UserSessionRemote(String sessionToken, String channelId, GrpcDataServiceDelegator delegator) {
        this.sessionToken = sessionToken;
        this.channelId = channelId;
        this.serverAddress = delegator != null ? delegator.getServerAddress() : getDefaultServerAddress();
        connect(this.serverAddress);
    }
    
    private String getDefaultServerAddress() {
        // Priority 1: Read from XML configuration (rtserverprops.xml)
        try {
            Environment.ConfigReader config = Environment.server();
            String host = config.get("engine.host");
            String port = config.get("engine.port");
            if (host != null && !host.isEmpty() && port != null && !port.isEmpty()) {
                String address = host + ":" + port;
                log.debug("Using gRPC server address from rtserverprops.xml: {}", address);
                return address;
            }
        } catch (Exception e) {
            log.debug("Could not read gRPC config from XML: {}", e.getMessage());
        }
        
        // Priority 2: Environment variable
        String address = System.getenv("AIPR_GRPC_SERVER_ADDRESS");
        if (address != null && !address.isEmpty()) {
            log.debug("Using gRPC server address from AIPR_GRPC_SERVER_ADDRESS: {}", address);
            return address;
        }
        
        // Priority 3: System property
        address = System.getProperty("aipr.grpc.server.address");
        if (address != null && !address.isEmpty()) {
            log.debug("Using gRPC server address from system property: {}", address);
            return address;
        }
        
        // Priority 4: Default fallback
        log.warn("No gRPC server address configured, using default localhost:50051");
        return "localhost:50051";
    }
    
    private void connect(String address) {
        try {
            String host = "localhost";
            int port = 50051;  // Default gRPC port matching rtserverprops.xml
            
            if (address != null && !address.isEmpty()) {
                String[] parts = address.split(":");
                host = parts[0];
                if (parts.length > 1) {
                    port = Integer.parseInt(parts[1]);
                }
            }
            
            channel = ManagedChannelBuilder.forAddress(host, port)
                    .usePlaintext()
                    .build();
            
            blockingStub = UserSessionRemoteGrpc.newBlockingStub(channel)
                    .withDeadlineAfter(DEFAULT_TIMEOUT_SECONDS, TimeUnit.SECONDS);
            futureStub = UserSessionRemoteGrpc.newFutureStub(channel);
            connected = true;
            
            log.info("Connected to gRPC server at {}:{}", host, port);
        } catch (Exception e) {
            log.error("Failed to connect to gRPC server at {}", address, e);
            connected = false;
        }
    }
    
    // =========================================================================
    // Session Management
    // =========================================================================
    
    @Override
    public SessionInfo validateSession() {
        // Session validation is typically done via SessionService, not UserSessionRemote
        // Return current session info based on what we have
        if (!isConnected()) {
            log.warn("Session validation failed: not connected");
            return null;
        }
        return new SessionInfo(
                sessionToken,
                channelId,  // user-id derived from session
                channelId,  // username derived from session
                channelId,
                System.currentTimeMillis() + 3600000 // 1 hour from now
        );
    }
    
    @Override
    public boolean heartbeat() {
        // Check if connection is still alive
        return isConnected();
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
        log.info("Submitting review for repo={}, pr={}", request.repoId(), request.prNumber());
        
        try {
            SessionReviewRequest grpcRequest = buildSessionReviewRequest(request);
            SessionReviewResponse grpcResponse = blockingStub.submitReview(grpcRequest);
            return convertToReviewResponse(grpcResponse, request);
        } catch (StatusRuntimeException e) {
            log.error("Failed to submit review: {}", e.getStatus(), e);
            throw new RuntimeException("Failed to submit review: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public CompletableFuture<ReviewResponse> submitReviewAsync(SessionContext context, ReviewRequest request) {
        log.info("Submitting review async for repo={}, pr={}", request.repoId(), request.prNumber());
        
        CompletableFuture<ReviewResponse> future = new CompletableFuture<>();
        SessionReviewRequest grpcRequest = buildSessionReviewRequest(request);
        
        ListenableFuture<SessionReviewResponse> listenableFuture = futureStub.submitReview(grpcRequest);
        
        Futures.addCallback(listenableFuture, new FutureCallback<SessionReviewResponse>() {
            @Override
            public void onSuccess(SessionReviewResponse result) {
                future.complete(convertToReviewResponse(result, request));
            }
            
            @Override
            public void onFailure(Throwable t) {
                log.error("Async review submission failed", t);
                future.completeExceptionally(t);
            }
        }, MoreExecutors.directExecutor());
        
        return future;
    }
    
    @Override
    public ReviewResponse getReview(SessionContext context, String reviewId) {
        log.info("Getting review {}", reviewId);
        
        try {
            SessionGetReviewRequest grpcRequest = SessionGetReviewRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setReviewId(reviewId)
                    .build();
            
            SessionReviewResponse grpcResponse = blockingStub.getReviewStatus(grpcRequest);
            return convertToReviewResponse(grpcResponse, null);
        } catch (StatusRuntimeException e) {
            log.error("Failed to get review {}: {}", reviewId, e.getStatus(), e);
            throw new RuntimeException("Failed to get review: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public PagedResult<ReviewResponse> listReviews(SessionContext context, String repositoryId,
                                                    ReviewFilter filter, PageRequest page) {
        log.info("Listing reviews for repo={}", repositoryId);
        
        try {
            SessionListReviewsRequest.Builder requestBuilder = SessionListReviewsRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .setPageSize(page.size());
            
            if (filter != null && filter.status() != null) {
                requestBuilder.setStatus(filter.status());
            }
            
            if (page.token() != null) {
                requestBuilder.setPageToken(page.token());
            }
            
            SessionListReviewsResponse grpcResponse = blockingStub.listReviews(requestBuilder.build());
            
            List<ReviewResponse> reviews = grpcResponse.getReviewsList().stream()
                    .map(r -> convertToReviewResponse(r, null))
                    .collect(Collectors.toList());
            
            return new PagedResult<>(reviews, grpcResponse.getTotalCount(), page.page(), page.size());
        } catch (StatusRuntimeException e) {
            log.error("Failed to list reviews: {}", e.getStatus(), e);
            throw new RuntimeException("Failed to list reviews: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public OperationResult cancelReview(SessionContext context, String reviewId) {
        log.info("Cancelling review {}", reviewId);
        
        try {
            SessionCancelReviewRequest grpcRequest = SessionCancelReviewRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setReviewId(reviewId)
                    .build();
            
            SessionCancelReviewResponse grpcResponse = blockingStub.cancelReview(grpcRequest);
            return new OperationResult(grpcResponse.getSuccess(), grpcResponse.getMessage());
        } catch (StatusRuntimeException e) {
            log.error("Failed to cancel review {}: {}", reviewId, e.getStatus(), e);
            return new OperationResult(false, "Failed to cancel review: " + e.getStatus().getDescription());
        }
    }
    
    // =========================================================================
    // Repository Operations
    // =========================================================================
    
    @Override
    public PagedResult<RepositoryInfo> listRepositories(SessionContext context, PageRequest page) {
        log.info("Listing repositories");
        
        try {
            SessionListReposRequest.Builder requestBuilder = SessionListReposRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setPageSize(page.size());
            
            if (page.token() != null) {
                requestBuilder.setPageToken(page.token());
            }
            
            SessionListReposResponse grpcResponse = blockingStub.listRepositories(requestBuilder.build());
            
            List<RepositoryInfo> repositories = grpcResponse.getRepositoriesList().stream()
                    .map(this::convertToRepositoryInfo)
                    .collect(Collectors.toList());
            
            return new PagedResult<>(repositories, grpcResponse.getTotalCount(), page.page(), page.size());
        } catch (StatusRuntimeException e) {
            log.error("Failed to list repositories: {}", e.getStatus(), e);
            throw new RuntimeException("Failed to list repositories: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public RepositoryInfo getRepository(SessionContext context, String repositoryId) {
        log.info("Getting repository {}", repositoryId);
        
        try {
            SessionGetRepoRequest grpcRequest = SessionGetRepoRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .build();
            
            SessionRepoResponse grpcResponse = blockingStub.getRepository(grpcRequest);
            return convertToRepositoryInfo(grpcResponse.getRepository());
        } catch (StatusRuntimeException e) {
            log.error("Failed to get repository {}: {}", repositoryId, e.getStatus(), e);
            throw new RuntimeException("Failed to get repository: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public IndexJobInfo triggerIndex(SessionContext context, String repositoryId, boolean fullReindex) {
        log.info("Triggering index for repo={}, fullReindex={}", repositoryId, fullReindex);
        
        try {
            SessionIndexRequest grpcRequest = SessionIndexRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .setFullReindex(fullReindex)
                    .build();
            
            SessionIndexResponse grpcResponse = blockingStub.triggerIndex(grpcRequest);
            return new IndexJobInfo(
                    grpcResponse.getJobId(),
                    grpcResponse.getStatus(),
                    grpcResponse.getMessage()
            );
        } catch (StatusRuntimeException e) {
            log.error("Failed to trigger index for repo {}: {}", repositoryId, e.getStatus(), e);
            throw new RuntimeException("Failed to trigger index: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public IndexStatusInfo getIndexStatus(SessionContext context, String repositoryId) {
        log.info("Getting index status for repo={}", repositoryId);
        
        try {
            SessionGetIndexStatusRequest grpcRequest = SessionGetIndexStatusRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setRepositoryId(repositoryId)
                    .build();
            
            SessionIndexStatusResponse grpcResponse = blockingStub.getIndexStatus(grpcRequest);
            return new IndexStatusInfo(
                    grpcResponse.getRepositoryId(),
                    grpcResponse.getIndexed(),
                    grpcResponse.getTotalFiles(),
                    grpcResponse.getIndexedFiles(),
                    grpcResponse.getTotalChunks(),
                    grpcResponse.getLastCommit(),
                    grpcResponse.getJobStatus(),
                    grpcResponse.getJobProgress()
            );
        } catch (StatusRuntimeException e) {
            log.error("Failed to get index status for repo {}: {}", repositoryId, e.getStatus(), e);
            throw new RuntimeException("Failed to get index status: " + e.getStatus().getDescription(), e);
        }
    }
    
    // =========================================================================
    // LLM Provider Operations
    // =========================================================================
    
    @Override
    public List<LLMProviderInfo> listLLMProviders(SessionContext context) {
        log.info("Listing LLM providers");
        
        try {
            SessionListLLMRequest grpcRequest = SessionListLLMRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .build();
            
            SessionListLLMResponse grpcResponse = blockingStub.listLLMProviders(grpcRequest);
            String defaultProvider = grpcResponse.getDefaultProvider();
            
            return grpcResponse.getProvidersList().stream()
                    .map(config -> convertToLLMProviderInfo(config, defaultProvider))
                    .collect(Collectors.toList());
        } catch (StatusRuntimeException e) {
            log.error("Failed to list LLM providers: {}", e.getStatus(), e);
            throw new RuntimeException("Failed to list LLM providers: " + e.getStatus().getDescription(), e);
        }
    }
    
    @Override
    public LLMTestResult testLLMProvider(SessionContext context, String providerId) {
        log.info("Testing LLM provider {}", providerId);
        
        try {
            SessionTestLLMRequest grpcRequest = SessionTestLLMRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setProviderId(providerId)
                    .build();
            
            SessionTestLLMResponse grpcResponse = blockingStub.testLLMConnection(grpcRequest);
            return new LLMTestResult(
                    grpcResponse.getSuccess(),
                    grpcResponse.getMessage(),
                    grpcResponse.getLatencyMs(),
                    new ArrayList<>(grpcResponse.getAvailableModelsList())
            );
        } catch (StatusRuntimeException e) {
            log.error("Failed to test LLM provider {}: {}", providerId, e.getStatus(), e);
            return new LLMTestResult(false, "Failed to test connection: " + e.getStatus().getDescription(), 0, List.of());
        }
    }
    
    @Override
    public LLMProviderInfo configureLLMProvider(SessionContext context, LLMProviderConfig config) {
        log.info("Configuring LLM provider {}", config.name());
        
        try {
            SessionConfigureLLMRequest.Builder requestBuilder = SessionConfigureLLMRequest.newBuilder()
                    .setSessionToken(sessionToken)
                    .setConfigName(config.name())
                    .setProviderType(config.providerType())
                    .setSetAsDefault(config.setAsDefault());
            
            if (config.baseUrl() != null) {
                requestBuilder.setBaseUrl(config.baseUrl());
            }
            if (config.apiKey() != null) {
                requestBuilder.setApiKey(config.apiKey());
            }
            if (config.defaultModel() != null) {
                requestBuilder.setDefaultModel(config.defaultModel());
            }
            if (config.extraConfig() != null) {
                requestBuilder.putAllExtraConfig(config.extraConfig());
            }
            
            SessionConfigureLLMResponse grpcResponse = blockingStub.configureLLMProvider(requestBuilder.build());
            
            return LLMProviderInfo.builder()
                    .id(grpcResponse.getProviderId())
                    .name(config.name())
                    .description(grpcResponse.getMessage())
                    .providerType(config.providerType())
                    .isDefault(config.setAsDefault())
                    .build();
        } catch (StatusRuntimeException e) {
            log.error("Failed to configure LLM provider {}: {}", config.name(), e.getStatus(), e);
            throw new RuntimeException("Failed to configure LLM provider: " + e.getStatus().getDescription(), e);
        }
    }
    
    // =========================================================================
    // Connection Management
    // =========================================================================
    
    @Override
    public boolean isConnected() {
        return connected && channel != null && !channel.isShutdown();
    }
    
    @Override
    public void reconnect() {
        close();
        connect(serverAddress);
    }
    
    @Override
    public void close() {
        connected = false;
        if (channel != null) {
            try {
                channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
            } catch (InterruptedException e) {
                channel.shutdownNow();
                Thread.currentThread().interrupt();
            }
        }
    }
    
    // =========================================================================
    // Conversion Helpers - Proto to Model
    // =========================================================================
    
    private SessionReviewRequest buildSessionReviewRequest(ReviewRequest request) {
        SessionReviewRequest.Builder builder = SessionReviewRequest.newBuilder()
                .setSessionToken(sessionToken)
                .setRepositoryId(request.repoId())
                .setPrNumber(request.prNumber());
        
        if (request.headCommit() != null) {
            builder.setHeadCommit(request.headCommit());
        }
        
        // Build review options from request config
        if (request.config() != null) {
            ReviewOptions.Builder optionsBuilder = ReviewOptions.newBuilder();
            
            if (request.config().categories() != null) {
                optionsBuilder.addAllCategories(request.config().categories());
            }
            if (request.config().minSeverity() != null) {
                optionsBuilder.setMinSeverity(request.config().minSeverity());
            }
            if (request.config().maxComments() != null) {
                optionsBuilder.setMaxComments(request.config().maxComments());
            }
            optionsBuilder.setIncludeSuggestions(request.config().includeSuggestions());
            optionsBuilder.setPostToPlatform(request.config().postToPlatform());
            
            if (request.config().llmProvider() != null) {
                optionsBuilder.setLlmProvider(request.config().llmProvider());
            }
            if (request.config().llmModel() != null) {
                optionsBuilder.setLlmModel(request.config().llmModel());
            }
            
            builder.setOptions(optionsBuilder.build());
        }
        
        return builder.build();
    }
    
    private ReviewResponse convertToReviewResponse(SessionReviewResponse grpcResponse, ReviewRequest originalRequest) {
        ReviewResponse.Builder builder = ReviewResponse.builder()
                .reviewId(grpcResponse.getReviewId())
                .status(grpcResponse.getStatus())
                .summary(grpcResponse.getSummary());
        
        if (originalRequest != null) {
            builder.repoId(originalRequest.repoId())
                    .prNumber(originalRequest.prNumber());
        }
        
        // Convert comments
        if (grpcResponse.getCommentsCount() > 0) {
            List<ReviewComment> comments = grpcResponse.getCommentsList().stream()
                    .map(this::convertToReviewComment)
                    .collect(Collectors.toList());
            builder.comments(comments);
        }
        
        // Convert metrics
        if (grpcResponse.hasMetrics()) {
            ai.aipr.session.grpc.ReviewMetrics grpcMetrics = grpcResponse.getMetrics();
            builder.metrics(ReviewMetrics.builder()
                    .filesAnalyzed(grpcMetrics.getFilesAnalyzed())
                    .linesAdded(grpcMetrics.getLinesAdded())
                    .linesRemoved(grpcMetrics.getLinesRemoved())
                    .totalFindings(grpcMetrics.getTotalFindings())
                    .tokensUsed(grpcMetrics.getTokensUsed())
                    .latencyMs(grpcMetrics.getLatencyMs())
                    .build());
        }
        
        return builder.build();
    }
    
    private ReviewComment convertToReviewComment(ai.aipr.session.grpc.ReviewComment grpcComment) {
        return ReviewComment.builder()
                .id(grpcComment.getId())
                .filePath(grpcComment.getFilePath())
                .line(grpcComment.getLine())
                .endLine(grpcComment.getEndLine())
                .severity(grpcComment.getSeverity())
                .category(grpcComment.getCategory())
                .message(grpcComment.getMessage())
                .suggestion(grpcComment.getSuggestion())
                .confidence((double) grpcComment.getConfidence())
                .build();
    }
    
    private RepositoryInfo convertToRepositoryInfo(ai.aipr.session.grpc.RepositoryInfo grpcRepo) {
        return RepositoryInfo.builder()
                .id(grpcRepo.getId())
                .platform(grpcRepo.getPlatform())
                .owner(grpcRepo.getOwner())
                .name(grpcRepo.getName())
                .defaultBranch(grpcRepo.getDefaultBranch())
                .role(grpcRepo.getRole())
                .indexed(grpcRepo.getIndexed())
                .build();
    }
    
    private LLMProviderInfo convertToLLMProviderInfo(ai.aipr.session.grpc.LLMProviderConfig grpcConfig, 
                                                      String defaultProviderId) {
        return LLMProviderInfo.builder()
                .id(grpcConfig.getId())
                .name(grpcConfig.getName())
                .providerType(grpcConfig.getProviderType())
                .availableModels(new ArrayList<>(grpcConfig.getAvailableModelsList()))
                .isDefault(grpcConfig.getId().equals(defaultProviderId) || grpcConfig.getIsDefault())
                .isConnected(grpcConfig.getIsConnected())
                .build();
    }
}
