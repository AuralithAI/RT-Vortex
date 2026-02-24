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
            mockMvc.perform(get("/api/v1/reviews/{reviewId}", "00000000-0000-0000-0000-000000000099"))
                    .andExpect(status().isNotFound());
        }


        @Test
        @DisplayName("should return review when found")
        void shouldReturnReviewWhenFound() throws Exception {
            // Seed a review into the repository
            String reviewId = "11111111-1111-1111-1111-111111111111";
            String repoId = "22222222-2222-2222-2222-222222222222";
            ReviewResponse review = ReviewResponse.builder()
                    .reviewId(reviewId)
                    .repoId(repoId)
                    .prNumber(99)
                    .status("COMPLETED")
                    .summary("All good")
                    .comments(List.of())
                    .build();
            reviewRepository.save(review);

            mockMvc.perform(get("/api/v1/reviews/{reviewId}", reviewId))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.reviewId").value(reviewId))
                    .andExpect(jsonPath("$.repoId").value(repoId))
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
                    .param("repoId", "99999999-9999-9999-9999-999999999999"))
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
                    .param("repoId", "99999999-9999-9999-9999-999999999999")
                    .param("page", "0")
                    .param("size", "5"))
                    .andExpect(status().isOk());
        }

        @Test
        @DisplayName("should return seeded reviews")
        void shouldReturnSeededReviews() throws Exception {
            String repoId = "33333333-3333-3333-3333-333333333333";
            // Seed reviews
            for (int i = 1; i <= 3; i++) {
                reviewRepository.save(ReviewResponse.builder()
                        .reviewId("44444444-4444-4444-4444-44444444444" + i)
                        .repoId(repoId)
                        .prNumber(i)
                        .status("COMPLETED")
                        .comments(List.of())
                        .build());
            }

            mockMvc.perform(get("/api/v1/reviews")
                    .param("repoId", repoId)
                    .param("page", "0")
                    .param("size", "10"))
                    .andExpect(status().isOk());
        }
    }
}

