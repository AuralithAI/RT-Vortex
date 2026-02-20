package ai.aipr.server.service;

import ai.aipr.server.dto.ReviewRequest;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.engine.EngineClient;
import ai.aipr.server.llm.LLMClient;
import ai.aipr.server.repository.ReviewRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.Mockito.when;

/**
 * Unit tests for ReviewService.
 * 
 * Note: Uses mocks only where necessary (external dependencies like EngineClient, LLMClient).
 * Prefer real implementations and integration tests where possible.
 */
@ExtendWith(MockitoExtension.class)
class ReviewServiceTest {

    private static final String TEST_REVIEW_ID = "review-123";
    private static final String TEST_REPO_ID = "repo-1";
    private static final int TEST_PR_NUMBER = 42;

    @Mock
    private EngineClient engineClient;

    @Mock
    private LLMClient llmClient;

    @Mock
    private ReviewRepository reviewRepository;

    @Mock
    private PromptBuilder promptBuilder;

    private ReviewService reviewService;

    @BeforeEach
    void setUp() {
        reviewService = new ReviewService(engineClient, llmClient, reviewRepository, promptBuilder);
    }

    @Test
    @DisplayName("getReview should return empty when review not found")
    void getReviewShouldReturnEmptyWhenNotFound() {
        when(reviewRepository.findById(anyString())).thenReturn(Optional.empty());

        Optional<ReviewResponse> result = reviewService.getReview("non-existent-id");

        assertFalse(result.isPresent());
    }

    @Test
    @DisplayName("getReview should return review when found")
    void getReviewShouldReturnReviewWhenFound() {
        ReviewResponse expectedReview = ReviewResponse.builder()
                .reviewId(TEST_REVIEW_ID)
                .repoId(TEST_REPO_ID)
                .prNumber(TEST_PR_NUMBER)
                .status("completed")
                .summary("Test review summary")
                .comments(java.util.Collections.emptyList())
                .build();
        when(reviewRepository.findById(TEST_REVIEW_ID)).thenReturn(Optional.of(expectedReview));

        Optional<ReviewResponse> result = reviewService.getReview(TEST_REVIEW_ID);

        assertTrue(result.isPresent());
        assertEquals(TEST_REVIEW_ID, result.get().reviewId());
        assertEquals(TEST_REPO_ID, result.get().repoId());
        assertEquals(TEST_PR_NUMBER, result.get().prNumber());
    }

    @Test
    @DisplayName("listReviews should return paginated results")
    void listReviewsShouldReturnPaginatedResults() {
        when(reviewRepository.findByRepoId(anyString(), org.mockito.ArgumentMatchers.anyInt(), org.mockito.ArgumentMatchers.anyInt()))
                .thenReturn(java.util.Collections.emptyList());

        var results = reviewService.listReviews(TEST_REPO_ID, 0, 20);

        assertNotNull(results);
    }
}
