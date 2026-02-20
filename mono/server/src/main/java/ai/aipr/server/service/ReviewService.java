package ai.aipr.server.service;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.engine.EngineClient;
import ai.aipr.server.llm.LLMClient;
import ai.aipr.server.repository.ReviewRepository;
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
    public CompletableFuture<ReviewResponse> reviewPullRequest(ReviewRequest request) {
        String reviewId = UUID.randomUUID().toString();
        Instant startTime = Instant.now();
        
        log.info("Starting review: id={}, repo={}, pr={}", 
                reviewId, request.repoId(), request.prNumber());
        
        try {
            // 1. Parse diff and identify touched symbols
            var diffAnalysis = engineClient.analyzeDiff(
                    request.repoId(), 
                    request.diff()
            );
            
            // 2. Run heuristic checks (secrets, risky APIs, etc.)
            var heuristicFindings = engineClient.runHeuristics(
                    request.repoId(), 
                    diffAnalysis
            );
            
            // 3. Retrieve relevant context from the index
            var contextPack = engineClient.buildContext(
                    request.repoId(),
                    diffAnalysis,
                    request.prTitle(),
                    request.prDescription()
            );
            
            // 4. Build LLM prompt
            var prompt = promptBuilder.buildReviewPrompt(
                    contextPack,
                    heuristicFindings,
                    request.config()
            );
            
            // 5. Call LLM for review
            var llmResponse = llmClient.complete(prompt);
            
            // 6. Parse and validate LLM response
            var reviewResult = parseReviewResult(llmResponse);
            
            // 7. Merge heuristic findings with LLM comments
            var mergedComments = mergeComments(
                    reviewResult.comments(),
                    heuristicFindings
            );
            
            // 8. Build response
            var response = ReviewResponse.builder()
                    .reviewId(reviewId)
                    .repoId(request.repoId())
                    .prNumber(request.prNumber())
                    .summary(reviewResult.summary())
                    .overallAssessment(reviewResult.overallAssessment())
                    .comments(mergedComments)
                    .metrics(reviewResult.metrics())
                    .metadata(ReviewMetadata.builder()
                            .startTime(startTime)
                            .endTime(Instant.now())
                            .engineVersion(engineClient.getVersion())
                            .modelUsed(llmClient.getModel())
                            .contextChunksUsed(contextPack.chunks().size())
                            .tokensUsed(llmResponse.tokensUsed())
                            .build())
                    .build();
            
            // 9. Persist review
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
    
    private ReviewResult parseReviewResult(LLMResponse response) {
        // Parse JSON response from LLM
        try {
            return ReviewResult.fromJson(response.content());
        } catch (Exception e) {
            log.warn("Failed to parse LLM response as JSON, falling back", e);
            return ReviewResult.empty();
        }
    }
    
    private List<ReviewComment> mergeComments(
            List<ReviewComment> llmComments,
            List<HeuristicFinding> heuristicFindings
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
    
    private ReviewComment toComment(HeuristicFinding finding) {
        return ReviewComment.builder()
                .id("heuristic-" + finding.id())
                .filePath(finding.filePath())
                .line(finding.line())
                .severity(finding.severity())
                .category(finding.category())
                .message(finding.message())
                .suggestion(finding.suggestion())
                .source("heuristic:" + finding.checkId())
                .build();
    }
}
