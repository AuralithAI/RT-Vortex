package ai.aipr.server.api;

import ai.aipr.server.config.TestSecurityConfig;
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

import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.post;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.jsonPath;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

/**
 * Integration tests for Webhook API endpoints.
 * Tests HTTP routing, header validation, and error handling.
 */
@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("test")
@Import(TestSecurityConfig.class)
class WebhookControllerIntegrationTest {

    @Autowired
    private MockMvc mockMvc;

    // =====================================================================
    // GitHub Webhooks
    // =====================================================================

    @Nested
    @DisplayName("POST /api/v1/webhooks/github")
    class GitHubWebhook {

        @Test
        @DisplayName("should require X-GitHub-Event header")
        void shouldRequireEventHeader() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/github")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{\"action\":\"opened\"}"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should accept ping event")
        void shouldAcceptPingEvent() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/github")
                    .contentType(MediaType.APPLICATION_JSON)
                    .header("X-GitHub-Event", "ping")
                    .header("X-GitHub-Delivery", "test-delivery-001")
                    .content("{\"zen\":\"Speak like a human.\"}"))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.status").value("accepted"))
                    .andExpect(jsonPath("$.event").value("ping"));
        }

        @Test
        @DisplayName("should handle pull_request event with minimal payload")
        void shouldHandlePullRequestEvent() throws Exception {
            String payload = """
                {
                    "action": "opened",
                    "number": 1,
                    "pull_request": {
                        "number": 1,
                        "title": "Test PR",
                        "body": "Test description",
                        "head": {"sha": "abc123", "ref": "feature"},
                        "base": {"ref": "main"},
                        "user": {"login": "testuser"}
                    },
                    "repository": {
                        "full_name": "org/repo"
                    }
                }
                """;

            mockMvc.perform(post("/api/v1/webhooks/github")
                    .contentType(MediaType.APPLICATION_JSON)
                    .header("X-GitHub-Event", "pull_request")
                    .header("X-GitHub-Delivery", "test-delivery-002")
                    .content(payload))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.event").value("pull_request"));
        }

        @Test
        @DisplayName("should accept optional signature header")
        void shouldAcceptOptionalSignature() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/github")
                    .contentType(MediaType.APPLICATION_JSON)
                    .header("X-GitHub-Event", "ping")
                    .header("X-Hub-Signature-256", "sha256=invalid")
                    .content("{\"zen\":\"test\"}"))
                    .andExpect(jsonPath("$.event").value("ping"));
        }
    }

    // =====================================================================
    // GitLab Webhooks
    // =====================================================================

    @Nested
    @DisplayName("POST /api/v1/webhooks/gitlab")
    class GitLabWebhook {

        @Test
        @DisplayName("should require X-Gitlab-Event header")
        void shouldRequireEventHeader() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/gitlab")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{\"object_kind\":\"merge_request\"}"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should accept merge request event")
        void shouldAcceptMergeRequestEvent() throws Exception {
            String payload = """
                {
                    "object_kind": "merge_request",
                    "object_attributes": {
                        "iid": 1,
                        "title": "Test MR",
                        "action": "open",
                        "state": "opened",
                        "source_branch": "feature",
                        "target_branch": "main",
                        "last_commit": {"id": "abc123"}
                    },
                    "project": {
                        "path_with_namespace": "group/project"
                    }
                }
                """;

            mockMvc.perform(post("/api/v1/webhooks/gitlab")
                    .contentType(MediaType.APPLICATION_JSON)
                    .header("X-Gitlab-Event", "Merge Request Hook")
                    .content(payload))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.event").value("Merge Request Hook"));
        }

        @Test
        @DisplayName("should accept optional token header")
        void shouldAcceptOptionalTokenHeader() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/gitlab")
                    .contentType(MediaType.APPLICATION_JSON)
                    .header("X-Gitlab-Event", "Push Hook")
                    .header("X-Gitlab-Token", "some-token")
                    .content("{\"object_kind\":\"push\"}"))
                    .andExpect(status().isOk());
        }
    }

    // =====================================================================
    // Bitbucket Webhooks
    // =====================================================================

    @Nested
    @DisplayName("POST /api/v1/webhooks/bitbucket")
    class BitbucketWebhook {

        @Test
        @DisplayName("should require X-Event-Key header")
        void shouldRequireEventKeyHeader() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/bitbucket")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{\"actor\":{}}"))
                    .andExpect(status().isBadRequest());
        }

        @Test
        @DisplayName("should accept pullrequest:created event")
        void shouldAcceptPrCreatedEvent() throws Exception {
            String payload = """
                {
                    "pullrequest": {
                        "id": 1,
                        "title": "Test PR",
                        "state": "OPEN",
                        "source": {
                            "branch": {"name": "feature"},
                            "commit": {"hash": "abc123"}
                        },
                        "destination": {
                            "branch": {"name": "main"}
                        },
                        "author": {"display_name": "testuser"}
                    },
                    "repository": {
                        "full_name": "workspace/repo"
                    }
                }
                """;

            mockMvc.perform(post("/api/v1/webhooks/bitbucket")
                    .contentType(MediaType.APPLICATION_JSON)
                    .header("X-Event-Key", "pullrequest:created")
                    .header("X-Request-UUID", "test-uuid-001")
                    .content(payload))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.event").value("pullrequest:created"));
        }
    }

    // =====================================================================
    // Azure DevOps Webhooks
    // =====================================================================

    @Nested
    @DisplayName("POST /api/v1/webhooks/azure-devops")
    class AzureDevOpsWebhook {

        @Test
        @DisplayName("should return skipped when handler not enabled")
        void shouldReturnSkippedWhenNotEnabled() throws Exception {
            // Azure DevOps handler is conditional — not enabled in test env
            String payload = """
                {
                    "eventType": "git.pullrequest.created",
                    "resource": {
                        "pullRequestId": 1,
                        "title": "Test PR"
                    }
                }
                """;

            mockMvc.perform(post("/api/v1/webhooks/azure-devops")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content(payload))
                    .andExpect(status().isOk())
                    .andExpect(jsonPath("$.status").value("skipped"))
                    .andExpect(jsonPath("$.reason").exists());
        }

        @Test
        @DisplayName("should accept request without Authorization header")
        void shouldAcceptWithoutAuthHeader() throws Exception {
            mockMvc.perform(post("/api/v1/webhooks/azure-devops")
                    .contentType(MediaType.APPLICATION_JSON)
                    .content("{\"eventType\":\"test\"}"))
                    .andExpect(status().isOk());
        }
    }
}

