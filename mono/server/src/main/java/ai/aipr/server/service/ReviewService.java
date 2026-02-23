package ai.aipr.server.service;

import ai.aipr.server.dto.ContextPack;
import ai.aipr.server.dto.HeuristicFinding;
import ai.aipr.server.dto.LLMResponse;
import ai.aipr.server.dto.ReviewComment;
import ai.aipr.server.dto.ReviewConfig;
import ai.aipr.server.dto.ReviewMetadata;
import ai.aipr.server.dto.ReviewMetrics;
import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.dto.ReviewResult;
import ai.aipr.server.dto.Severity;
import ai.aipr.server.engine.EngineClient;
import ai.aipr.server.llm.LLMClient;
import ai.aipr.server.repository.ReviewRepository;
import org.jetbrains.annotations.NotNull;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.scheduling.annotation.Async;
import org.springframework.stereotype.Service;

import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;

/**
 * Service for conducting PR reviews.
 */
@Service
public class ReviewService {

    private static final Logger log = LoggerFactory.getLogger(ReviewService.class);

    private final EngineClient engineClient;
    private final LLMClient llmClient;
    private final ReviewRepository reviewRepository;
    private final PromptBuilder promptBuilder;

    public ReviewService(
            EngineClient engineClient,
            LLMClient llmClient,
            ReviewRepository reviewRepository,
            PromptBuilder promptBuilder
    ) {
        this.engineClient = engineClient;
        this.llmClient = llmClient;
        this.reviewRepository = reviewRepository;
        this.promptBuilder = promptBuilder;
    }

    /**
     * Conduct a PR review asynchronously.
     */
    @Async
    public CompletableFuture<ReviewResponse> reviewPullRequest(@NotNull ReviewRequest request) {
        String reviewId = UUID.randomUUID().toString();
        Instant startTime = Instant.now();

        log.info("Starting review: id={}, repo={}, pr={}",
                reviewId, request.repoId(), request.prNumber());

        try {
            // Ensure we always have a valid review config
            var config = request.config() != null ? request.config() : ReviewConfig.defaults();

            // 1. Parse diff and identify touched symbols
            var diffAnalysis = engineClient.analyzeDiff(
                    request.repoId(),
                    request.diff()
            );

            // 2. Run heuristic checks (secrets, risky APIs, etc.)
            var heuristicFindings = engineClient.runHeuristics(
                    request.repoId(),
                    request.diff()
            );

            // 3. Retrieve relevant context from the index
            var contextPackFromEngine = engineClient.buildContext(
                    request.repoId(),
                    request.diff(),
                    diffAnalysis,
                    request.prTitle(),
                    request.prDescription()
            );

            // Ensure the raw diff string is included in the context pack
            var contextPack = ContextPack.builder()
                    .repoId(contextPackFromEngine.repoId())
                    .prTitle(contextPackFromEngine.prTitle())
                    .prDescription(contextPackFromEngine.prDescription())
                    .diff(request.diff())
                    .changedFiles(diffAnalysis.changedFiles())
                    .contextChunks(contextPackFromEngine.contextChunks())
                    .chunks(contextPackFromEngine.chunks())
                    .touchedSymbols(diffAnalysis.touchedSymbols())
                    .heuristicWarnings(contextPackFromEngine.heuristicWarnings())
                    .totalTokens(contextPackFromEngine.totalTokens())
                    .build();

            // 4. Build LLM prompt
            var prompt = promptBuilder.buildReviewPrompt(
                    contextPack,
                    heuristicFindings,
                    config
            );

            // 5. Call LLM for review
            var llmResponse = llmClient.complete(prompt.userPrompt(), prompt.systemPrompt());

            // 6. Parse and validate LLM response
            var reviewResult = parseReviewResult(llmResponse);

            // 7. Merge heuristic findings with LLM comments
            var mergedComments = mergeComments(
                    reviewResult.comments(),
                    heuristicFindings
            );

            // 8. Build metrics combining LLM scores with computed quantitative data
            Instant endTime = Instant.now();
            long latencyMs = java.time.Duration.between(startTime, endTime).toMillis();

            ReviewMetrics.Builder metricsBuilder = ReviewMetrics.builder()
                    .filesAnalyzed(diffAnalysis.changedFiles().size())
                    .linesAdded(diffAnalysis.totalAdditions())
                    .linesRemoved(diffAnalysis.totalDeletions())
                    .totalFindings(mergedComments.size())
                    .tokensUsed(llmResponse.tokensUsed())
                    .promptTokens(llmResponse.promptTokens())
                    .completionTokens(llmResponse.completionTokens())
                    .latencyMs((int) latencyMs)
                    .llmLatencyMs((int) llmResponse.latencyMs());

            // Merge score-based metrics from LLM result if available
            if (reviewResult.metrics() != null) {
                var llmMetrics = reviewResult.metrics();
                if (llmMetrics.securityScore() != null) metricsBuilder.securityScore(llmMetrics.securityScore());
                if (llmMetrics.reliabilityScore() != null) metricsBuilder.reliabilityScore(llmMetrics.reliabilityScore());
                if (llmMetrics.performanceScore() != null) metricsBuilder.performanceScore(llmMetrics.performanceScore());
                if (llmMetrics.testingScore() != null) metricsBuilder.testingScore(llmMetrics.testingScore());
                if (llmMetrics.documentationScore() != null) metricsBuilder.documentationScore(llmMetrics.documentationScore());
                if (llmMetrics.overallScore() != null) metricsBuilder.overallScore(llmMetrics.overallScore());
            }

            ReviewMetrics metrics = metricsBuilder.build();

            // 9. Build response with suggestions as a proper field
            var response = ReviewResponse.builder()
                    .reviewId(reviewId)
                    .repoId(request.repoId())
                    .prNumber(request.prNumber())
                    .status("completed")
                    .summary(reviewResult.summary())
                    .overallAssessment(reviewResult.overallAssessment())
                    .comments(mergedComments)
                    .suggestions(reviewResult.suggestions() != null
                            ? reviewResult.suggestions() : List.of())
                    .metrics(metrics)
                    .metadata(ReviewMetadata.builder()
                            .startTime(startTime)
                            .endTime(endTime)
                            .engineVersion(engineClient.getVersion())
                            .modelUsed(llmClient.getModel())
                            .contextChunksUsed(contextPack.contextChunks().size())
                            .tokensUsed(llmResponse.tokensUsed())
                            .build())
                    .build();

            // 11. Persist review
            reviewRepository.save(response);

            log.info("Review completed: id={}, assessment={}, comments={}",
                    reviewId, response.overallAssessment(), mergedComments.size());

            return CompletableFuture.completedFuture(response);

        } catch (Exception e) {
            log.error("Review failed: id={}, error={}", reviewId, e.getMessage(), e);
            throw new RuntimeException("Review failed: " + e.getMessage(), e);
        }
    }

    /**
     * Get a review by ID.
     */
    public Optional<ReviewResponse> getReview(String reviewId) {
        return reviewRepository.findById(reviewId);
    }

    /**
     * List reviews for a repository.
     */
    public List<ReviewResponse> listReviews(String repoId, int page, int size) {
        return reviewRepository.findByRepoId(repoId, page, size);
    }

    /**
     * Cancel a pending or running review.
     *
     * @param reviewId The review ID to cancel
     * @return true if the review was found and cancelled, false otherwise
     */
    public boolean cancelReview(String reviewId) {
        var reviewOpt = reviewRepository.findById(reviewId);
        if (reviewOpt.isEmpty()) {
            log.warn("Cannot cancel review {}: not found", reviewId);
            return false;
        }

        var review = reviewOpt.get();
        // Only cancel reviews that are still in progress
        if ("completed".equals(review.status()) || "cancelled".equals(review.status())) {
            log.warn("Cannot cancel review {}: already in state '{}'", reviewId, review.status());
            return false;
        }

        // Update review status to cancelled
        var cancelled = ReviewResponse.builder()
                .reviewId(review.reviewId())
                .repoId(review.repoId())
                .prNumber(review.prNumber())
                .status("cancelled")
                .summary(review.summary())
                .overallAssessment(review.overallAssessment())
                .comments(review.comments())
                .suggestions(review.suggestions() != null ? review.suggestions() : List.of())
                .metrics(review.metrics())
                .metadata(review.metadata())
                .build();

        reviewRepository.save(cancelled);
        log.info("Review cancelled: id={}", reviewId);
        return true;
    }

    private ReviewResult parseReviewResult(LLMResponse response) {
        try {
            var result = ReviewResult.fromJson(response.content());

            // Validate and normalize overallAssessment using the enum
            String assessment = result.overallAssessment();
            String normalizedAssessment;
            try {
                normalizedAssessment = ReviewResult.OverallAssessment.valueOf(
                        assessment != null ? assessment.toUpperCase().replace(" ", "_") : "COMMENT"
                ).name();
            } catch (IllegalArgumentException e) {
                log.warn("Unknown overallAssessment '{}' from LLM, defaulting to COMMENT", assessment);
                normalizedAssessment = ReviewResult.OverallAssessment.COMMENT.name();
            }

            // Return result with validated assessment
            if (!normalizedAssessment.equals(assessment)) {
                return ReviewResult.builder()
                        .summary(result.summary())
                        .overallAssessment(normalizedAssessment)
                        .comments(result.comments())
                        .suggestions(result.suggestions())
                        .metrics(result.metrics())
                        .build();
            }

            return result;
        } catch (Exception e) {
            log.warn("Failed to parse LLM response as JSON, falling back", e);
            return ReviewResult.empty();
        }
    }

    @NotNull
    private List<ReviewComment> mergeComments(
        List<ReviewComment> llmComments,
        @NotNull List<HeuristicFinding> heuristicFindings
    ) {
        // Convert heuristic findings to comments
        var heuristicComments = heuristicFindings.stream()
                .map(this::toComment)
                .toList();

        // Merge, avoiding duplicates
        var merged = new java.util.ArrayList<>(llmComments);

        for (var hc : heuristicComments) {
            boolean isDuplicate = llmComments.stream()
                    .anyMatch(lc ->
                            lc.filePath().equals(hc.filePath()) &&
                            Math.abs(lc.line() - hc.line()) <= 2 &&
                            lc.category().equals(hc.category())
                    );

            if (!isDuplicate) {
                merged.add(hc);
            }
        }

        return merged;
    }

    private ReviewComment toComment(@NotNull HeuristicFinding finding) {
        return ReviewComment.builder()
                .id("heuristic-" + finding.id())
                .filePath(finding.filePath())
                .line(finding.line())
                .endLine(finding.endLine())
                .severity(Severity.fromString(finding.severity()).getValue())
                .category(finding.category())
                .message(finding.message())
                .suggestion(finding.suggestion())
                .references(List.of())
                .confidence(0.9)  // High confidence for heuristic rules
                .source("heuristic:" + finding.checkId())
                .build();
    }
}
