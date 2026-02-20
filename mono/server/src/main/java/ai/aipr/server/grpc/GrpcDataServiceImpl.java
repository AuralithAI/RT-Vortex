package ai.aipr.server.grpc;

import ai.aipr.server.service.ReviewService;
import ai.aipr.server.service.IndexingService;
import ai.aipr.server.service.RepositoryService;
import ai.aipr.server.service.LLMService;
import ai.aipr.server.session.SessionManager;
import ai.aipr.session.grpc.*;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import net.devh.boot.grpc.server.service.GrpcService;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Autowired;

import java.util.List;
import java.util.UUID;
import java.util.stream.Collectors;

/**
 * Server-side gRPC implementation for UserSessionRemote service.
 * 
 * <p>This class handles all gRPC requests from clients, validates sessions,
 * and delegates to the appropriate service layer.</p>
 */
@GrpcService
public class GrpcDataServiceImpl extends UserSessionRemoteGrpc.UserSessionRemoteImplBase {

    private static final Logger log = LoggerFactory.getLogger(GrpcDataServiceImpl.class);

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
            // Validate session
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            log.info("Processing review request from user={}, repo={}, pr={}",
                    session.getUserId(), request.getRepositoryId(), request.getPrNumber());

            // Submit review through service layer
            var result = reviewService.submitReview(
                session.getUserId(),
                request.getRepositoryId(),
                request.getPrNumber(),
                request.getHeadCommit(),
                request.getBaseCommit(),
                convertReviewOptions(request.getOptions())
            );

            // Build response
            SessionReviewResponse.Builder responseBuilder = SessionReviewResponse.newBuilder()
                .setReviewId(result.getId())
                .setStatus(result.getStatus())
                .setSummary(result.getSummary() != null ? result.getSummary() : "");

            // Add comments
            if (result.getComments() != null) {
                for (var comment : result.getComments()) {
                    responseBuilder.addComments(ReviewComment.newBuilder()
                        .setId(comment.getId())
                        .setFilePath(comment.getFilePath())
                        .setLine(comment.getLine())
                        .setEndLine(comment.getEndLine())
                        .setSeverity(comment.getSeverity())
                        .setCategory(comment.getCategory())
                        .setMessage(comment.getMessage())
                        .setSuggestion(comment.getSuggestion() != null ? comment.getSuggestion() : "")
                        .setConfidence(comment.getConfidence())
                        .build());
                }
            }

            // Add metrics
            if (result.getMetrics() != null) {
                responseBuilder.setMetrics(ReviewMetrics.newBuilder()
                    .setFilesAnalyzed(result.getMetrics().getFilesAnalyzed())
                    .setLinesAdded(result.getMetrics().getLinesAdded())
                    .setLinesRemoved(result.getMetrics().getLinesRemoved())
                    .setTotalFindings(result.getMetrics().getTotalFindings())
                    .setTokensUsed(result.getMetrics().getTokensUsed())
                    .setLatencyMs(result.getMetrics().getLatencyMs())
                    .build());
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error processing review request", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getReviewStatus(SessionGetReviewRequest request,
                                StreamObserver<SessionReviewResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var result = reviewService.getReview(session.getUserId(), request.getReviewId());
            if (result == null) {
                responseObserver.onError(Status.NOT_FOUND
                    .withDescription("Review not found")
                    .asRuntimeException());
                return;
            }

            // Build and send response (similar to submitReview)
            SessionReviewResponse.Builder responseBuilder = SessionReviewResponse.newBuilder()
                .setReviewId(result.getId())
                .setStatus(result.getStatus())
                .setSummary(result.getSummary() != null ? result.getSummary() : "");

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error getting review status", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void listReviews(SessionListReviewsRequest request,
                            StreamObserver<SessionListReviewsResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var result = reviewService.listReviews(
                session.getUserId(),
                request.getRepositoryId(),
                request.getStatus(),
                request.getPageSize(),
                request.getPageToken()
            );

            SessionListReviewsResponse.Builder responseBuilder = SessionListReviewsResponse.newBuilder()
                .setTotalCount(result.getTotalCount())
                .setNextPageToken(result.getNextPageToken() != null ? result.getNextPageToken() : "");

            for (var review : result.getItems()) {
                responseBuilder.addReviews(SessionReviewResponse.newBuilder()
                    .setReviewId(review.getId())
                    .setStatus(review.getStatus())
                    .setSummary(review.getSummary() != null ? review.getSummary() : "")
                    .build());
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error listing reviews", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void cancelReview(SessionCancelReviewRequest request,
                             StreamObserver<SessionCancelReviewResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            boolean success = reviewService.cancelReview(session.getUserId(), request.getReviewId());

            responseObserver.onNext(SessionCancelReviewResponse.newBuilder()
                .setSuccess(success)
                .setMessage(success ? "Review cancelled" : "Could not cancel review")
                .build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error cancelling review", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
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
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var result = repositoryService.listRepositoriesForUser(
                session.getUserId(),
                request.getPageSize(),
                request.getPageToken()
            );

            SessionListReposResponse.Builder responseBuilder = SessionListReposResponse.newBuilder()
                .setTotalCount(result.getTotalCount())
                .setNextPageToken(result.getNextPageToken() != null ? result.getNextPageToken() : "");

            for (var repo : result.getItems()) {
                responseBuilder.addRepositories(RepositoryInfo.newBuilder()
                    .setId(repo.getId())
                    .setPlatform(repo.getPlatform())
                    .setOwner(repo.getOwner())
                    .setName(repo.getName())
                    .setDefaultBranch(repo.getDefaultBranch())
                    .setRole(repo.getRole())
                    .setIndexed(repo.isIndexed())
                    .build());
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error listing repositories", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getRepository(SessionGetRepoRequest request,
                              StreamObserver<SessionRepoResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var repo = repositoryService.getRepository(session.getUserId(), request.getRepositoryId());
            if (repo == null) {
                responseObserver.onError(Status.NOT_FOUND
                    .withDescription("Repository not found")
                    .asRuntimeException());
                return;
            }

            responseObserver.onNext(SessionRepoResponse.newBuilder()
                .setRepository(RepositoryInfo.newBuilder()
                    .setId(repo.getId())
                    .setPlatform(repo.getPlatform())
                    .setOwner(repo.getOwner())
                    .setName(repo.getName())
                    .setDefaultBranch(repo.getDefaultBranch())
                    .setRole(repo.getRole())
                    .setIndexed(repo.isIndexed())
                    .build())
                .build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error getting repository", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void triggerIndex(SessionIndexRequest request,
                             StreamObserver<SessionIndexResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var job = indexingService.triggerIndex(
                session.getUserId(),
                request.getRepositoryId(),
                request.getFullReindex()
            );

            responseObserver.onNext(SessionIndexResponse.newBuilder()
                .setJobId(job.getId())
                .setStatus(job.getStatus())
                .setMessage("Index job started")
                .build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error triggering index", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void getIndexStatus(SessionGetIndexStatusRequest request,
                               StreamObserver<SessionIndexStatusResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var status = indexingService.getIndexStatus(session.getUserId(), request.getRepositoryId());

            responseObserver.onNext(SessionIndexStatusResponse.newBuilder()
                .setRepositoryId(status.getRepositoryId())
                .setIndexed(status.isIndexed())
                .setTotalFiles(status.getTotalFiles())
                .setIndexedFiles(status.getIndexedFiles())
                .setTotalChunks(status.getTotalChunks())
                .setLastCommit(status.getLastCommit() != null ? status.getLastCommit() : "")
                .setJobStatus(status.getJobStatus() != null ? status.getJobStatus() : "")
                .setJobProgress(status.getJobProgress())
                .build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error getting index status", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // LLM Provider Operations
    // =========================================================================

    @Override
    public void listLLMProviders(SessionListLLMRequest request,
                                 StreamObserver<SessionListLLMResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var providers = llmService.listProviders(session.getUserId());

            SessionListLLMResponse.Builder responseBuilder = SessionListLLMResponse.newBuilder();

            for (var provider : providers) {
                responseBuilder.addProviders(LLMProviderConfig.newBuilder()
                    .setId(provider.getId())
                    .setName(provider.getName())
                    .setProviderType(provider.getProviderType())
                    .setIsDefault(provider.isDefault())
                    .setIsConnected(provider.isConnected())
                    .addAllAvailableModels(provider.getAvailableModels())
                    .build());
                
                if (provider.isDefault()) {
                    responseBuilder.setDefaultProvider(provider.getId());
                }
            }

            responseObserver.onNext(responseBuilder.build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error listing LLM providers", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void testLLMConnection(SessionTestLLMRequest request,
                                  StreamObserver<SessionTestLLMResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var result = llmService.testConnection(session.getUserId(), request.getProviderId());

            responseObserver.onNext(SessionTestLLMResponse.newBuilder()
                .setSuccess(result.isSuccess())
                .setMessage(result.getMessage())
                .setLatencyMs(result.getLatencyMs())
                .addAllAvailableModels(result.getAvailableModels())
                .build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error testing LLM connection", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    @Override
    public void configureLLMProvider(SessionConfigureLLMRequest request,
                                     StreamObserver<SessionConfigureLLMResponse> responseObserver) {
        try {
            var session = validateSession(request.getSessionToken());
            if (session == null) {
                responseObserver.onError(Status.UNAUTHENTICATED
                    .withDescription("Invalid or expired session")
                    .asRuntimeException());
                return;
            }

            var provider = llmService.configureProvider(
                session.getUserId(),
                request.getConfigName(),
                request.getProviderType(),
                request.getBaseUrl(),
                request.getApiKey(),
                request.getDefaultModel(),
                request.getSetAsDefault()
            );

            responseObserver.onNext(SessionConfigureLLMResponse.newBuilder()
                .setSuccess(true)
                .setProviderId(provider.getId())
                .setMessage("LLM provider configured successfully")
                .build());
            responseObserver.onCompleted();

        } catch (Exception e) {
            log.error("Error configuring LLM provider", e);
            responseObserver.onError(Status.INTERNAL
                .withDescription(e.getMessage())
                .asRuntimeException());
        }
    }

    // =========================================================================
    // Helpers
    // =========================================================================

    private SessionManager.ValidatedSession validateSession(String sessionToken) {
        return sessionManager.validateSession(sessionToken);
    }

    private ai.aipr.server.model.ReviewOptions convertReviewOptions(ReviewOptions grpcOptions) {
        if (grpcOptions == null) {
            return null;
        }
        return new ai.aipr.server.model.ReviewOptions(
            grpcOptions.getCategoriesList(),
            grpcOptions.getMinSeverity(),
            grpcOptions.getMaxComments(),
            grpcOptions.getIncludeSuggestions(),
            grpcOptions.getPostToPlatform(),
            grpcOptions.getLlmProvider(),
            grpcOptions.getLlmModel()
        );
    }
}
