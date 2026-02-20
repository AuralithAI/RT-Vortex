# AI-PR-Reviewer Java SDK

Java client library for interacting with the AI-PR-Reviewer API.

## Requirements

- Java 17 or later
- Maven or Gradle

## Installation

### Maven

```xml
<dependency>
    <groupId>ai.auralith</groupId>
    <artifactId>aipr-java-sdk</artifactId>
    <version>0.1.0</version>
</dependency>
```

### Gradle

```groovy
implementation 'ai.auralith:aipr-java-sdk:0.1.0'
```

## Quick Start

```java
import ai.auralith.aipr.AIPRClient;
import ai.auralith.aipr.model.*;

// Create client
AIPRClient client = AIPRClient.builder()
    .baseUrl("https://api.aipr.example.com")
    .apiKey("your-api-key")
    .build();

// Submit a review
ReviewResponse response = client.review(ReviewRequest.builder()
    .repositoryUrl("https://github.com/owner/repo")
    .pullRequestId(123)
    .baseSha("abc123")
    .headSha("def456")
    .build());

// Process comments
for (ReviewResponse.ReviewComment comment : response.getComments()) {
    System.out.printf("[%s] %s:%d - %s%n",
        comment.getSeverity(),
        comment.getFile(),
        comment.getLine(),
        comment.getMessage());
}

// Close client when done
client.close();
```

## Async Operations

```java
// Async review
CompletableFuture<ReviewResponse> future = client.reviewAsync(request);

future.thenAccept(response -> {
    System.out.println("Review completed: " + response.getSummary());
}).exceptionally(e -> {
    System.err.println("Review failed: " + e.getMessage());
    return null;
});
```

## Configuration

```java
AIPRClient client = AIPRClient.builder()
    .baseUrl("https://api.aipr.example.com")
    .apiKey("your-api-key")
    .connectTimeout(Duration.ofSeconds(30))
    .readTimeout(Duration.ofMinutes(10))  // Long timeout for large reviews
    .writeTimeout(Duration.ofSeconds(30))
    .build();
```

## Error Handling

```java
try {
    ReviewResponse response = client.review(request);
} catch (AIPRException e) {
    if (e.isRateLimitError()) {
        // Handle rate limiting
        Thread.sleep(60000);
    } else if (e.isClientError()) {
        // Handle client errors (4xx)
        System.err.println("Invalid request: " + e.getMessage());
    } else if (e.isServerError()) {
        // Handle server errors (5xx)
        System.err.println("Server error: " + e.getMessage());
    }
}
```

## Repository Indexing

```java
// Start indexing
IndexResponse indexJob = client.index(IndexRequest.builder()
    .repositoryUrl("https://github.com/owner/repo")
    .branch("main")
    .forceReindex(false)
    .build());

// Poll for completion
while (!indexJob.isComplete()) {
    Thread.sleep(5000);
    indexJob = client.getIndexStatus(indexJob.getJobId());
    System.out.printf("Progress: %.1f%%%n", indexJob.getProgressPercent());
}

if (indexJob.isSuccess()) {
    System.out.println("Indexed " + indexJob.getFilesIndexed() + " files");
}
```

## License

Apache License 2.0
