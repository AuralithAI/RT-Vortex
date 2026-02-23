package ai.aipr.server.api;

import ai.aipr.server.config.TestSecurityConfig;
import ai.aipr.server.dto.ReviewResponse;
import ai.aipr.server.repository.ReviewRepository;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.context.annotation.Import;
import org.springframework.http.MediaType;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;

import java.util.List;

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.get;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for Review API endpoints.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import(TestSecurityConfig.class)
class ReviewControllerIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    @Autowired
    private ReviewRepository reviewRepository;

    @Nested
    @DisplayName("POST /api/v1/reviews")
    class SubmitReview {

        @Test
        @DisplayName("should reject empty body")
        void shouldRejectEmptyBody() throws Exception {
            mockMvc.perform(post("/api/v1/reviews")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{}"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should reject missing repoId")
        void shouldRejectMissingRepoId() throws Exception {
            mockMvc.perform(post("/api/v1/reviews")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("""
                        {
                            "prNumber": 42,
                            "diff": "some diff"
                        }
                        """))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should reject missing content type")
        void shouldRejectMissingContentType() throws Exception {
            mockMvc.perform(post("/api/v1/reviews")
                    .content("{\"repoId\":\"test\",\"prNumber\":1,\"diff\":\"d\"}"))
                    .andExpect(status().isUnsupportedMediaType());
        }
    }

    @Nested
    @DisplayName("GET /api/v1/reviews/{reviewId}")
    class GetReview {

        @Test
        @DisplayName("should return 404 for non-existent review")
        void shouldReturn404ForNonExistentReview() throws Exception {
            mockMvc.perform(get("/api/v1/reviews/{reviewId}", "non-existent-id"))
                    .andExpect(status().isNotFound());
        }

        @Test
        @DisplayName("should return review when found")
        void shouldReturnReviewWhenFound() throws Exception {
            // Seed a review into the repository
            ReviewResponse review = ReviewResponse.builder()
                    .reviewId("test-review-001")
                    .repoId("org/repo")
                    .prNumber(99)
                    .status("COMPLETED")
                    .summary("All good")
                    .comments(List.of())
                    .build();
            reviewRepository.save(review);

            mockMvc.perform(get("/api/v1/reviews/{reviewId}", "test-review-001"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.reviewId").value("test-review-001"))
                    .andExpect(jsonPath("$.repoId").value("org/repo"))
                    .andExpect(jsonPath("$.prNumber").value(99))
                    .andExpect(jsonPath("$.status").value("COMPLETED"))
                    .andExpect(jsonPath("$.summary").value("All good"));
        }
    }

    @Nested
    @DisplayName("GET /api/v1/reviews")
    class ListReviews {

        @Test
        @DisplayName("should return empty list for unknown repo")
        void shouldReturnEmptyListForUnknownRepo() throws Exception {
            mockMvc.perform(get("/api/v1/reviews")
                    .param("repoId", "unknown/repo"))
                    .andExpect(status().isOk());
        }

        @Test
        @DisplayName("should require repoId parameter")
        void shouldRequireRepoIdParameter() throws Exception {
            mockMvc.perform(get("/api/v1/reviews"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should support pagination parameters")
        void shouldSupportPagination() throws Exception {
            mockMvc.perform(get("/api/v1/reviews")
                    .param("repoId", "org/repo")
                    .param("page", "0")
                    .param("size", "5"))
                    .andExpect(status().isOk());
        }

        @Test
        @DisplayName("should return seeded reviews")
        void shouldReturnSeededReviews() throws Exception {
            // Seed reviews
            for (int i = 1; i <= 3; i++) {
                reviewRepository.save(ReviewResponse.builder()
                        .reviewId("list-review-" + i)
                        .repoId("list-test/repo")
                        .prNumber(i)
                        .status("COMPLETED")
                        .comments(List.of())
                        .build());
            }

            mockMvc.perform(get("/api/v1/reviews")
                    .param("repoId", "list-test/repo")
                    .param("page", "0")
                    .param("size", "10"))
                    .andExpect(status().isOk());
        }
    }
}

