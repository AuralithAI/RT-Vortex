package ai.aipr.server.grpc;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.service.ReviewService;
import ai.aipr.server.service.IndexingService;
import ai.aipr.server.service.RepositoryService;
import ai.aipr.server.service.LLMService;
import ai.aipr.server.session.SessionManager;
import ai.aipr.server.session.SessionManager.ValidatedSession;
import ai.aipr.session.grpc.*;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import net.devh.boot.grpc.server.service.GrpcService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;

/**
 * Server-side gRPC implementation for UserSessionRemote service.
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

    @Autowired
    public GrpcDataServiceImpl(SessionManager sessionManager,
                               ReviewService reviewService,
                               IndexingService indexingService,
                               RepositoryService repositoryService,
                               LLMService llmService) {
        this.sessionManager = sessionManager;
        this.reviewService = reviewService;
        this.indexingService = indexingService;
        this.repositoryService = repositoryService;
        this.llmService = llmService;
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
                    session.getUserId(), request.getRepositoryId(), request.getPrNumber());

            // Build review request
            ReviewRequest reviewRequest = ReviewRequest.builder()
                    .repoId(request.getRepositoryId())
                    .prNumber(request.getPrNumber())
                    .build();

            // Submit review through service layer asynchronously
            reviewService.reviewPullRequest(reviewRequest)
                .thenAccept(result -> {
                    SessionReviewResponse.Builder responseBuilder = SessionReviewResponse.newBuilder()
                        .setReviewId(result.reviewId())
                        .setStatus(result.status() != null ? result.status() : "completed")
                        .setSummary(result.summary() != null ? result.summary() : "");
                    
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
                    .build();

            responseObserver.onNext(response);
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

            // TODO: Implement review listing
            var response = SessionListReviewsResponse.newBuilder()
                    .setTotalCount(0)
                    .build();

            responseObserver.onNext(response);
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

            // TODO: Implement review cancellation
            var response = SessionCancelReviewResponse.newBuilder()
                    .setSuccess(true)
                    .setMessage("Review cancelled")
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

            // Create indexing request
            ai.aipr.server.dto.IndexRequest indexRequest = ai.aipr.server.dto.IndexRequest.builder()
                    .repoId(request.getRepositoryId())
                    .build();

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

            var statusOpt = indexingService.getStatus(request.getRepositoryId());

            SessionIndexStatusResponse.Builder responseBuilder = SessionIndexStatusResponse.newBuilder()
                    .setRepositoryId(request.getRepositoryId());

            if (statusOpt.isPresent()) {
                var status = statusOpt.get();
                responseBuilder
                    .setIndexed(status.isCompleted())
                    .setJobStatus(status.status())
                    .setJobProgress(status.progressFloat());
            }

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

            var repos = repositoryService.listRepositories(session.getUserId());
            
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

            // TODO: Implement getRepository
            responseObserver.onError(Status.NOT_FOUND
                .withDescription("Repository not found")
                .asRuntimeException());
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
                responseBuilder.addProviders(LLMProviderConfig.newBuilder()
                        .setId(provider.id())
                        .setName(provider.name())
                        .setProviderType(provider.id()) // provider type = id for now
                        .setIsDefault(provider.isDefault())
                        .setIsConnected(true) // assume connected
                        .addAllAvailableModels(provider.availableModels())
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

            // TODO: Implement actual LLM connection test
            var response = SessionTestLLMResponse.newBuilder()
                    .setSuccess(true)
                    .setMessage("Connection successful")
                    .setLatencyMs(100)
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

            // TODO: Implement actual LLM configuration
            var response = SessionConfigureLLMResponse.newBuilder()
                    .setSuccess(true)
                    .setProviderId(request.getConfigName())
                    .setMessage("Configuration saved")
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
    // Session Management Helpers
    // =========================================================================

    private ValidatedSession validateSession(String sessionToken) {
        if (sessionToken == null || sessionToken.isEmpty()) {
            return null;
        }
        return sessionManager.validateSession(sessionToken);
    }
}
