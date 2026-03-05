# RTVortex Java SDK

Official Java SDK for the **RTVortex** AI code-review API.

## Requirements

- Java 17+
- Maven 3.9+

## Installation

### Maven

```xml
<dependency>
    <groupId>dev.rtvortex</groupId>
    <artifactId>rtvortex-sdk</artifactId>
    <version>0.1.0</version>
</dependency>
```

### Gradle

```groovy
implementation 'dev.rtvortex:rtvortex-sdk:0.1.0'
```

## Quick Start

```java
import dev.rtvortex.sdk.RTVortexClient;
import dev.rtvortex.sdk.model.*;

RTVortexClient client = new RTVortexClient.Builder()
    .token(System.getenv("RTVORTEX_TOKEN"))
    .build();

// Get current user
User user = client.me();
System.out.println(user.getEmail());

// Trigger a review
Review review = client.triggerReview("repo-123", 42);

// Get comments
List<ReviewComment> comments = client.getReviewComments(review.getId());
for (ReviewComment c : comments) {
    System.out.printf("[%s] %s%n", c.getSeverity(), c.getMessage());
}

// Always close when done
client.close();
```

## Error Handling

```java
import dev.rtvortex.sdk.*;

try {
    client.getReview("nonexistent");
} catch (NotFoundException e) {
    System.out.println("Not found: " + e.getMessage());
} catch (AuthenticationException e) {
    System.out.println("Auth failed: " + e.getMessage());
} catch (RTVortexException e) {
    System.out.printf("API error %d: %s%n", e.getStatusCode(), e.getMessage());
}
```

## API Reference

| Method | Description |
|--------|-------------|
| `me()` | Get authenticated user |
| `updateMe(fields)` | Update user profile |
| `listOrgs(limit, offset)` | List organizations |
| `createOrg(name, slug, plan)` | Create organization |
| `getOrg(id)` | Get organization |
| `updateOrg(id, fields)` | Update organization |
| `listMembers(orgId, limit, offset)` | List org members |
| `inviteMember(orgId, email, role)` | Invite member |
| `removeMember(orgId, userId)` | Remove member |
| `listRepos(limit, offset)` | List repositories |
| `registerRepo(data)` | Register repository |
| `getRepo(id)` | Get repository |
| `updateRepo(id, fields)` | Update repository |
| `deleteRepo(id)` | Delete repository |
| `triggerReview(repoId, prNumber)` | Trigger a review |
| `getReview(id)` | Get review |
| `getReviewComments(id)` | Get review comments |
| `triggerIndex(repoId)` | Trigger indexing |
| `getIndexStatus(repoId)` | Get index status |
| `getStats()` | Get admin stats |
| `health()` | Health check |
| `healthDetailed()` | Detailed health check |

## License

Apache-2.0
