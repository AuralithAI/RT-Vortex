package ai.aipr.server.grpc;

import ai.aipr.server.dto.IndexConfig;
import ai.aipr.server.dto.ReviewConfig;
import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.engine.EngineClient;
import ai.aipr.server.model.LLMProviderConfig;
import ai.aipr.server.service.ReviewService;
import ai.aipr.server.service.IndexingService;
import ai.aipr.server.service.RepositoryService;
import ai.aipr.server.service.LLMService;
import ai.aipr.server.session.SessionManager;
import ai.aipr.server.session.SessionManager.ValidatedSession;
import ai.aipr.session.grpc.RepositoryInfo;
import ai.aipr.session.grpc.ReviewOptions;
import ai.aipr.session.grpc.SessionCancelReviewRequest;
import ai.aipr.session.grpc.SessionCancelReviewResponse;
import ai.aipr.session.grpc.SessionConfigureLLMRequest;
import ai.aipr.session.grpc.SessionConfigureLLMResponse;
import ai.aipr.session.grpc.SessionContextChunk;
import ai.aipr.session.grpc.SessionDeleteIndexRequest;
import ai.aipr.session.grpc.SessionDeleteIndexResponse;
import ai.aipr.session.grpc.SessionEngineDiagnosticsRequest;
import ai.aipr.session.grpc.SessionEngineDiagnosticsResponse;
import ai.aipr.session.grpc.SessionEngineHealthRequest;
import ai.aipr.session.grpc.SessionEngineHealthResponse;
import ai.aipr.session.grpc.SessionGetIndexStatusRequest;
import ai.aipr.session.grpc.SessionGetRepoRequest;
import ai.aipr.session.grpc.SessionGetReviewRequest;
import ai.aipr.session.grpc.SessionIndexDiagnostic;
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
import ai.aipr.session.grpc.SessionSearchRequest;
import ai.aipr.session.grpc.SessionSearchResponse;
import ai.aipr.session.grpc.SessionTestLLMRequest;
import ai.aipr.session.grpc.SessionTestLLMResponse;
import ai.aipr.session.grpc.UserSessionRemoteGrpc;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import net.devh.boot.grpc.server.service.GrpcService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;

import java.util.List;

/**
 * Server-side gRPC implementation for the UserSessionRemote service.
 *
 * <p><b>How this class is used:</b> The {@code @GrpcService} annotation causes
 * {@code grpc-server-spring-boot-starter} to auto-discover this bean at startup and
 * register it on the gRPC server (default port 9090, configured via {@code grpc.server.port}).
 * There are no direct Java references to this class — it is wired by the framework.</p>
 *
 * <p>This class handles all gRPC requests from clients, validates sessions,
 * and delegates to the appropriate service layer.</p>
 */
@GrpcService
public class GrpcDataServiceImpl extends UserSessionRemoteGrpc.UserSessionRemoteImplBase {

    private static final Logger log = LoggerFactory.getLogger(GrpcDataServiceImpl.class);
    private static final String UNAUTHENTICATED_MSG = "Invalid or expired session";
    private static final String INTERNAL_ERROR_PREFIX = "Internal error: ";

    private final SessionManager sessionManager;
    private final ReviewService reviewService;
    private final IndexingService indexingService;
    private final RepositoryService repositoryService;
    private final LLMService llmService;
    private final EngineClient engineClient;

    @Autowired
    public GrpcDataServiceImpl(SessionManager sessionManager,
                               ReviewService reviewService,
                               IndexingService indexingService,
                               RepositoryService repositoryService,
                               LLMService llmService,
                               EngineClient engineClient) {
        this.sessionManager = sessionManager;
        this.reviewService = reviewService;
        this.indexingService = indexingService;
        this.repositoryService = repositoryService;
        this.llmService = llmService;
        this.engineClient = engineClient;
    }

    // =========================================================================
    // Review Operations
    // =========================================================================

    @Override
    public void submitReview(SessionReviewRequest request,
                             StreamObserver<SessionReviewResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            log.info("Processing review request from user={}, repo={}, pr={}",
                    session.userId(), request.getRepositoryId(), request.getPrNumber());

            // Build review request with all available fields
            ReviewRequest.Builder reqBuilder = ReviewRequest.builder()
                    .repoId(request.getRepositoryId())
                    .prNumber(request.getPrNumber());

            if (!request.getDiff().isEmpty()) {
                reqBuilder.diff(request.getDiff());
            }
            if (!request.getPrTitle().isEmpty()) {
                reqBuilder.prTitle(request.getPrTitle());
            }
            if (!request.getPrDescription().isEmpty()) {
                reqBuilder.prDescription(request.getPrDescription());
            }
            if (!request.getHeadCommit().isEmpty()) {
                reqBuilder.headCommit(request.getHeadCommit());
            }
            if (!request.getBaseBranch().isEmpty()) {
                reqBuilder.baseBranch(request.getBaseBranch());
            }
            if (!request.getHeadBranch().isEmpty()) {
                reqBuilder.headBranch(request.getHeadBranch());
            }

            // Map gRPC review options to ReviewConfig
            if (request.hasOptions()) {
                ReviewOptions opts = request.getOptions();
                ReviewConfig.Builder cfgBuilder = ReviewConfig.builder();
                if (!opts.getLlmProvider().isEmpty()) {
                    cfgBuilder.llmProvider(opts.getLlmProvider());
                }
                if (!opts.getLlmModel().isEmpty()) {
                    cfgBuilder.llmModel(opts.getLlmModel());
                }
                if (opts.getCategoriesCount() > 0) {
                    cfgBuilder.categories(opts.getCategoriesList());
                }
                if (!opts.getMinSeverity().isEmpty()) {
                    cfgBuilder.minSeverity(opts.getMinSeverity());
                }
                if (opts.getMaxComments() > 0) {
                    cfgBuilder.maxComments(opts.getMaxComments());
                }
                cfgBuilder.includeSuggestions(opts.getIncludeSuggestions());
                cfgBuilder.postToPlatform(opts.getPostToPlatform());
                reqBuilder.config(cfgBuilder.build());
            }

            ReviewRequest reviewRequest = reqBuilder.build();

            // Submit review through service layer asynchronously
            reviewService.reviewPullRequest(reviewRequest)
                .thenAccept(result -> {
                    SessionReviewResponse.Builder responseBuilder = SessionReviewResponse.newBuilder()
                        .setReviewId(result.reviewId())
                        .setStatus(result.status() != null ? result.status() : "completed")
                        .setSummary(result.summary() != null ? result.summary() : "")
                        .setOverallAssessment(result.overallAssessment() != null ? result.overallAssessment() : "");

                    // Include comments in response
                    if (result.comments() != null) {
                        for (var comment : result.comments()) {
                            ai.aipr.session.grpc.ReviewComment.Builder cb = ai.aipr.session.grpc.ReviewComment.newBuilder()
                                .setFilePath(comment.filePath() != null ? comment.filePath() : "")
                                .setLine(comment.line())
                                .setSeverity(comment.severity() != null ? comment.severity() : "")
                                .setCategory(comment.category() != null ? comment.category() : "")
                                .setMessage(comment.message() != null ? comment.message() : "");
                            if (comment.id() != null) cb.setId(comment.id());
                            if (comment.endLine() != null) cb.setEndLine(comment.endLine());
                            if (comment.suggestion() != null) cb.setSuggestion(comment.suggestion());
                            if (comment.confidence() != null) cb.setConfidence(comment.confidence().floatValue());
                            responseBuilder.addComments(cb.build());
                        }
                    }

                    // Include metrics in response
                    if (result.metrics() != null) {
                        var m = result.metrics();
                        ai.aipr.session.grpc.ReviewMetrics.Builder mb = ai.aipr.session.grpc.ReviewMetrics.newBuilder();
                        if (m.filesAnalyzed() != null) mb.setFilesAnalyzed(m.filesAnalyzed());
                        if (m.linesAdded() != null) mb.setLinesAdded(m.linesAdded());
                        if (m.linesRemoved() != null) mb.setLinesRemoved(m.linesRemoved());
                        if (m.totalFindings() != null) mb.setTotalFindings(m.totalFindings());
                        if (m.tokensUsed() != null) mb.setTokensUsed(m.tokensUsed());
                        if (m.latencyMs() != null) mb.setLatencyMs(m.latencyMs());
                        responseBuilder.setMetrics(mb.build());
                    }

                    // Include suggestions in response
                    if (result.suggestions() != null) {
                        responseBuilder.addAllSuggestions(result.suggestions());
                    }

                    responseObserver.onNext(responseBuilder.build());
                    responseObserver.onCompleted();
                })
                .exceptionally(ex -> {
                    responseObserver.onError(Status.INTERNAL
                        .withDescription("Review failed: " + ex.getMessage())
                        .asRuntimeException());
                    return null;
                });

        } catch (Exception e) {
            log.error("Error in submitReview", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getReviewStatus(SessionGetReviewRequest request,
                                StreamObserver<SessionReviewResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            var reviewOpt = reviewService.getReview(request.getReviewId());
            if (reviewOpt.isEmpty()) {
                responseObserver.onError(Status.NOT_FOUND
                    .withDescription("Review not found")
                    .asRuntimeException());
                return;
            }

            var review = reviewOpt.get();
            var response = SessionReviewResponse.newBuilder()
                    .setReviewId(review.reviewId())
                    .setStatus(review.status() != null ? review.status() : "completed")
                    .setSummary(review.summary() != null ? review.summary() : "")
                    .setOverallAssessment(review.overallAssessment() != null ? review.overallAssessment() : "");

            // Include comments
            if (review.comments() != null) {
                for (var comment : review.comments()) {
                    ai.aipr.session.grpc.ReviewComment.Builder cb = ai.aipr.session.grpc.ReviewComment.newBuilder()
                        .setFilePath(comment.filePath() != null ? comment.filePath() : "")
                        .setLine(comment.line())
                        .setSeverity(comment.severity() != null ? comment.severity() : "")
                        .setCategory(comment.category() != null ? comment.category() : "")
                        .setMessage(comment.message() != null ? comment.message() : "");
                    if (comment.id() != null) cb.setId(comment.id());
                    if (comment.endLine() != null) cb.setEndLine(comment.endLine());
                    if (comment.suggestion() != null) cb.setSuggestion(comment.suggestion());
                    if (comment.confidence() != null) cb.setConfidence(comment.confidence().floatValue());
                    response.addComments(cb.build());
                }
            }

            // Include metrics
            if (review.metrics() != null) {
                var m = review.metrics();
                ai.aipr.session.grpc.ReviewMetrics.Builder mb = ai.aipr.session.grpc.ReviewMetrics.newBuilder();
                if (m.filesAnalyzed() != null) mb.setFilesAnalyzed(m.filesAnalyzed());
                if (m.linesAdded() != null) mb.setLinesAdded(m.linesAdded());
                if (m.linesRemoved() != null) mb.setLinesRemoved(m.linesRemoved());
                if (m.totalFindings() != null) mb.setTotalFindings(m.totalFindings());
                if (m.tokensUsed() != null) mb.setTokensUsed(m.tokensUsed());
                if (m.latencyMs() != null) mb.setLatencyMs(m.latencyMs());
                response.setMetrics(mb.build());
            }

            // Include suggestions
            if (review.suggestions() != null) {
                response.addAllSuggestions(review.suggestions());
            }

            responseObserver.onNext(response.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in getReviewStatus", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void listReviews(SessionListReviewsRequest request,
                            StreamObserver<SessionListReviewsResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            String repoId = request.getRepositoryId();
            int pageSize = request.getPageSize() > 0 ? request.getPageSize() : 20;
            int page = 0;
            if (!request.getPageToken().isEmpty()) {
                try {
                    page = Integer.parseInt(request.getPageToken());
                } catch (NumberFormatException ignored) {}
            }

            var reviews = reviewService.listReviews(repoId, page, pageSize);

            var responseBuilder = SessionListReviewsResponse.newBuilder()
                    .setTotalCount(reviews.size());

            for (var review : reviews) {
                SessionReviewResponse.Builder rb = SessionReviewResponse.newBuilder()
                        .setReviewId(review.reviewId())
                        .setStatus(review.status() != null ? review.status() : "completed")
                        .setSummary(review.summary() != null ? review.summary() : "")
                        .setOverallAssessment(review.overallAssessment() != null ? review.overallAssessment() : "");
                if (review.suggestions() != null) {
                    rb.addAllSuggestions(review.suggestions());
                }
                responseBuilder.addReviews(rb.build());
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in listReviews", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void cancelReview(SessionCancelReviewRequest request,
                             StreamObserver<SessionCancelReviewResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            String reviewId = request.getReviewId();
            log.info("Cancelling review: reviewId={}, user={}", reviewId, session.userId());

            boolean cancelled = reviewService.cancelReview(reviewId);

            var response = SessionCancelReviewResponse.newBuilder()
                    .setSuccess(cancelled)
                    .setMessage(cancelled ? "Review cancelled" : "Review not found or already completed")
                    .build();

            responseObserver.onNext(response);
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in cancelReview", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Indexing Operations
    // =========================================================================

    @Override
    public void triggerIndex(SessionIndexRequest request,
                             StreamObserver<SessionIndexResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            log.info("Starting repository indexing: repo={}", request.getRepositoryId());

            // Create indexing request from proto fields
            ai.aipr.server.dto.IndexRequest.Builder indexReqBuilder =
                    ai.aipr.server.dto.IndexRequest.builder()
                            .repoId(request.getRepositoryId())
                            .config(IndexConfig.defaultsWithModel(null));

            // Use branch from request, fall back to repo's default branch if empty
            if (!request.getBranch().isEmpty()) {
                indexReqBuilder.branch(request.getBranch());
            }
            if (!request.getCommitSha().isEmpty()) {
                indexReqBuilder.commitSha(request.getCommitSha());
            }
            if (!request.getSinceCommit().isEmpty()) {
                indexReqBuilder.sinceCommit(request.getSinceCommit());
            }

            ai.aipr.server.dto.IndexRequest indexRequest = indexReqBuilder.build();

            // Submit indexing job asynchronously
            indexingService.indexRepository(indexRequest, request.getFullReindex())
                .thenAccept(indexStatus -> {
                    SessionIndexResponse response = SessionIndexResponse.newBuilder()
                            .setJobId(indexStatus.jobId())
                            .setStatus(indexStatus.status())
                            .setMessage(indexStatus.message() != null ? indexStatus.message() : "")
                            .build();
                    responseObserver.onNext(response);
                    responseObserver.onCompleted();
                })
                .exceptionally(ex -> {
                    responseObserver.onError(Status.INTERNAL
                        .withDescription("Indexing failed: " + ex.getMessage())
                        .asRuntimeException());
                    return null;
                });

        } catch (Exception e) {
            log.error("Error in triggerIndex", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getIndexStatus(SessionGetIndexStatusRequest request,
                               StreamObserver<SessionIndexStatusResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            var statusOpt = indexingService.getStatusByRepoId(request.getRepositoryId());

            SessionIndexStatusResponse.Builder responseBuilder = SessionIndexStatusResponse.newBuilder()
                    .setRepositoryId(request.getRepositoryId());

            statusOpt.ifPresent(status -> responseBuilder
                .setIndexed(status.isCompleted())
                .setJobStatus(status.status())
                .setJobProgress(status.progressFloat()));

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in getIndexStatus", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Repository Operations
    // =========================================================================

    @Override
    public void listRepositories(SessionListReposRequest request,
                                 StreamObserver<SessionListReposResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            var repos = repositoryService.listRepositories(session.userId());

            SessionListReposResponse.Builder responseBuilder = SessionListReposResponse.newBuilder();
            for (var repo : repos) {
                responseBuilder.addRepositories(RepositoryInfo.newBuilder()
                        .setId(repo.id())
                        .setName(repo.name())
                        .setOwner(repo.owner())
                        .setPlatform(repo.platform())
                        .setDefaultBranch(repo.defaultBranch())
                        .setRole(repo.role())
                        .setIndexed(repo.indexed())
                        .build());
            }
            responseBuilder.setTotalCount(repos.size());

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in listRepositories", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getRepository(SessionGetRepoRequest request,
                              StreamObserver<SessionRepoResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            var repoOpt = repositoryService.getRepository(request.getRepositoryId());
            if (repoOpt.isEmpty()) {
                responseObserver.onError(Status.NOT_FOUND
                    .withDescription("Repository not found: " + request.getRepositoryId())
                    .asRuntimeException());
                return;
            }

            var repo = repoOpt.get();
            var repoInfo = RepositoryInfo.newBuilder()
                    .setId(repo.id())
                    .setName(repo.name())
                    .setOwner(repo.owner())
                    .setPlatform(repo.platform())
                    .setDefaultBranch(repo.defaultBranch())
                    .setRole(repo.role())
                    .setIndexed(repo.indexed())
                    .build();

            var response = SessionRepoResponse.newBuilder()
                    .setRepository(repoInfo)
                    .build();

            responseObserver.onNext(response);
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in getRepository", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // LLM Operations
    // =========================================================================

    @Override
    public void listLLMProviders(SessionListLLMRequest request,
                                 StreamObserver<SessionListLLMResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            var providers = llmService.listProviders();

            SessionListLLMResponse.Builder responseBuilder = SessionListLLMResponse.newBuilder();
            String defaultProvider = null;

            for (var provider : providers) {
                responseBuilder.addProviders(ai.aipr.session.grpc.LLMProviderConfig.newBuilder()
                        .setId(provider.id())
                        .setName(provider.name())
                        .setProviderType(provider.providerType() != null ? provider.providerType() : provider.id())
                        .setIsDefault(provider.isDefault())
                        .setIsConnected(provider.isConnected())
                        .addAllAvailableModels(provider.availableModels() != null ? provider.availableModels() : List.of())
                        .build());

                if (provider.isDefault()) {
                    defaultProvider = provider.id();
                }
            }

            if (defaultProvider != null) {
                responseBuilder.setDefaultProvider(defaultProvider);
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in listLLMProviders", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void testLLMConnection(SessionTestLLMRequest request,
                                  StreamObserver<SessionTestLLMResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            String providerId = request.getProviderId();
            log.info("Testing LLM connection: provider={}, user={}", providerId, session.userId());

            // Get user-specific config if available, otherwise test with defaults
            var providerConfig = llmService.getProviderConfig(session.userId(), providerId);
            var testResult = llmService.testConfiguration(
                    providerConfig != null ? providerConfig
                            : LLMProviderConfig.builder().providerId(providerId).build());

            var response = SessionTestLLMResponse.newBuilder()
                    .setSuccess(testResult.isSuccess())
                    .setMessage(testResult.getMessage())
                    .setLatencyMs(testResult.getLatencyMs())
                    .addAllAvailableModels(testResult.getAvailableModels() != null
                            ? testResult.getAvailableModels() : List.of())
                    .build();

            responseObserver.onNext(response);
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in testLLMConnection", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void configureLLMProvider(SessionConfigureLLMRequest request,
                                     StreamObserver<SessionConfigureLLMResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            log.info("Configuring LLM provider: name={}, user={}", request.getConfigName(), session.userId());

            // Build provider config from gRPC request
            LLMProviderConfig.Builder configBuilder = LLMProviderConfig.builder()
                    .providerId(request.getConfigName())
                    .name(request.getConfigName())
                    .providerType(request.getProviderType())
                    .setAsDefault(request.getSetAsDefault());

            if (!request.getBaseUrl().isEmpty()) {
                configBuilder.baseUrl(request.getBaseUrl());
            }
            if (!request.getApiKey().isEmpty()) {
                configBuilder.apiKey(request.getApiKey());
            }
            if (!request.getDefaultModel().isEmpty()) {
                configBuilder.defaultModel(request.getDefaultModel());
            }
            if (request.getExtraConfigCount() > 0) {
                configBuilder.extraConfig(request.getExtraConfigMap());
            }

            LLMProviderConfig config = configBuilder.build();
            llmService.configureProvider(session.userId(), config);

            var response = SessionConfigureLLMResponse.newBuilder()
                    .setSuccess(true)
                    .setProviderId(request.getConfigName())
                    .setMessage("Provider configured successfully")
                    .build();

            responseObserver.onNext(response);
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in configureLLMProvider", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Index Delete Operations
    // =========================================================================

    @Override
    public void deleteIndex(SessionDeleteIndexRequest request,
                            StreamObserver<SessionDeleteIndexResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            log.info("Deleting index: repo={}, user={}", request.getRepositoryId(), session.userId());

            engineClient.deleteIndex(request.getRepositoryId());

            var response = SessionDeleteIndexResponse.newBuilder()
                    .setSuccess(true)
                    .setMessage("Index deleted successfully")
                    .build();

            responseObserver.onNext(response);
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in deleteIndex", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Search Operations
    // =========================================================================

    @Override
    public void searchCode(SessionSearchRequest request,
                           StreamObserver<SessionSearchResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            log.info("Searching code: repo={}, query='{}', user={}",
                    request.getRepositoryId(), request.getQuery(), session.userId());

            int topK = request.getTopK() > 0 ? request.getTopK() : 10;
            var chunks = engineClient.search(
                    request.getRepositoryId(),
                    request.getQuery(),
                    request.getTouchedSymbolsList(),
                    topK
            );

            var responseBuilder = SessionSearchResponse.newBuilder()
                    .setTotalResults(chunks.size());

            for (var chunk : chunks) {
                responseBuilder.addChunks(SessionContextChunk.newBuilder()
                        .setId(chunk.id() != null ? chunk.id() : "")
                        .setFilePath(chunk.filePath() != null ? chunk.filePath() : "")
                        .setStartLine(chunk.startLine())
                        .setEndLine(chunk.endLine())
                        .setContent(chunk.content() != null ? chunk.content() : "")
                        .setLanguage(chunk.language() != null ? chunk.language() : "")
                        .addAllSymbols(chunk.symbols() != null ? chunk.symbols() : List.of())
                        .setRelevanceScore(chunk.relevanceScore())
                        .build());
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in searchCode", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Engine Health & Diagnostics
    // =========================================================================

    @Override
    public void getEngineHealth(SessionEngineHealthRequest request,
                                StreamObserver<SessionEngineHealthResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            boolean healthy = engineClient.isHealthy();
            String version = engineClient.getVersion();

            var response = SessionEngineHealthResponse.newBuilder()
                    .setHealthy(healthy)
                    .setEngineVersion(version != null ? version : "unknown")
                    .build();

            responseObserver.onNext(response);
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in getEngineHealth", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getEngineDiagnostics(SessionEngineDiagnosticsRequest request,
                                     StreamObserver<SessionEngineDiagnosticsResponse> responseObserver) {
        try {
            ValidatedSession session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription(UNAUTHENTICATED_MSG)
                    .asRuntimeException());
                return;
            }

            var diagnostics = engineClient.getDiagnostics(
                    request.getIncludeMemory(), request.getIncludeIndices());

            var responseBuilder = SessionEngineDiagnosticsResponse.newBuilder()
                    .putAllConfig(diagnostics.getConfigMap());

            if (diagnostics.hasMemory()) {
                responseBuilder
                    .setHeapUsedBytes(diagnostics.getMemory().getHeapUsedBytes())
                    .setHeapTotalBytes(diagnostics.getMemory().getHeapTotalBytes())
                    .setRssBytes(diagnostics.getMemory().getRssBytes());
            }

            for (var idx : diagnostics.getIndicesList()) {
                responseBuilder.addIndices(SessionIndexDiagnostic.newBuilder()
                        .setRepoId(idx.getRepoId())
                        .setSizeBytes(idx.getSizeBytes())
                        .setLastUpdated(idx.getLastUpdated())
                        .setIsLoaded(idx.getIsLoaded())
                        .build());
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();
        } catch (Exception e) {
            log.error("Error in getEngineDiagnostics", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(INTERNAL_ERROR_PREFIX + e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Session Management Helpers
    // =========================================================================

    private ValidatedSession validateSession(String sessionToken) {
        if (sessionToken == null || sessionToken.isEmpty()) {
            return null;
        }
        return sessionManager.validateSession(sessionToken);
    }
}
