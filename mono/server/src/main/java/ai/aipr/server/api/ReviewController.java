package ai.aipr.server.api;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.service.ReviewService;
import jakarta.validation.Valid;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

import java.util.concurrent.CompletableFuture;

/**
 * REST API for PR reviews.
 */
@RestController
@RequestMapping("/api/v1/reviews")
public class ReviewController {
    
    private static final Logger log = LoggerFactory.getLogger(ReviewController.class);
    
    private final ReviewService reviewService;
    
    public ReviewController(ReviewService reviewService) {
        this.reviewService = reviewService;
    }
    
    /**
     * Submit a PR for review.
     */
    @PostMapping
    public CompletableFuture<ResponseEntity<ReviewResponse>> submitReview(
            @Valid @RequestBody ReviewRequest request,
            @RequestHeader(value = "X-Request-ID", required = false) String requestId
    ) {
        log.info("Review request received: repo={}, pr={}", request.repoId(), request.prNumber());
        
        return reviewService.reviewPullRequest(request)
                .thenApply(ResponseEntity::ok)
                .exceptionally(ex -> {
                    log.error("Review failed: repo={}, pr={}, error={}", 
                            request.repoId(), request.prNumber(), ex.getMessage(), ex);
                    throw new RuntimeException("Review failed", ex);
                });
    }
    
    /**
     * Get review by ID.
     */
    @GetMapping("/{reviewId}")
    public ResponseEntity<ReviewResponse> getReview(@PathVariable String reviewId) {
        return reviewService.getReview(reviewId)
                .map(ResponseEntity::ok)
                .orElse(ResponseEntity.notFound().build());
    }
    
    /**
     * Get reviews for a repository.
     */
    @GetMapping
    public ResponseEntity<?> listReviews(
            @RequestParam String repoId,
            @RequestParam(defaultValue = "0") int page,
            @RequestParam(defaultValue = "20") int size
    ) {
        return ResponseEntity.ok(reviewService.listReviews(repoId, page, size));
    }
}
