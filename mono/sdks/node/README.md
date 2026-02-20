# AI-PR-Reviewer Node.js SDK

TypeScript/JavaScript client library for interacting with the AI-PR-Reviewer API.

## Requirements

- Node.js 18+

## Installation

```bash
npm install @auralith/aipr-sdk
# or
yarn add @auralith/aipr-sdk
# or
pnpm add @auralith/aipr-sdk
```

## Quick Start

```typescript
import { AIPRClient, ReviewRequest } from "@auralith/aipr-sdk";

// Create client
const client = new AIPRClient({
  baseUrl: "https://api.aipr.example.com",
  apiKey: "your-api-key",
});

// Submit a review
const response = await client.review({
  repositoryUrl: "https://github.com/owner/repo",
  pullRequestId: 123,
  baseSha: "abc123",
  headSha: "def456",
});

// Process comments
for (const comment of response.comments) {
  console.log(`[${comment.severity}] ${comment.file}:${comment.line} - ${comment.message}`);
}
```

## Waiting for Completion

```typescript
// Submit and wait for completion
const review = await client.review({
  repositoryUrl: "https://github.com/owner/repo",
  pullRequestId: 123,
});

// Poll until complete
const completed = await client.waitForReview(review.reviewId, {
  pollInterval: 5000,  // Check every 5 seconds
  timeout: 300000,     // Timeout after 5 minutes
});

console.log(`Review complete: ${completed.summary}`);
```

## Error Handling

```typescript
import { AIPRClient, AIPRAPIError, AIPRTimeoutError } from "@auralith/aipr-sdk";

const client = new AIPRClient({ baseUrl: "https://api.aipr.example.com" });

try {
  const response = await client.review(request);
} catch (error) {
  if (error instanceof AIPRTimeoutError) {
    console.log("Request timed out, try again later");
  } else if (error instanceof AIPRAPIError) {
    if (error.isRateLimitError) {
      console.log("Rate limited, waiting 60 seconds...");
      await sleep(60000);
    } else if (error.isClientError) {
      console.log(`Invalid request: ${error.message}`);
    } else if (error.isServerError) {
      console.log(`Server error: ${error.message}`);
    }
  }
}
```

## Repository Indexing

```typescript
// Start indexing
const indexJob = await client.index({
  repositoryUrl: "https://github.com/owner/repo",
  branch: "main",
  forceReindex: false,
});

// Wait for completion
const completed = await client.waitForIndex(indexJob.jobId, {
  pollInterval: 5000,
  timeout: 600000,  // 10 minutes
});

if (completed.status === "completed") {
  console.log(`Indexed ${completed.filesIndexed} files`);
} else {
  console.log(`Indexing failed: ${completed.error}`);
}
```

## Configuration

```typescript
const client = new AIPRClient({
  baseUrl: "https://api.aipr.example.com",
  apiKey: "your-api-key",
  timeout: 600000,  // 10 minute timeout for large reviews
});
```

## Types

### ReviewRequest

```typescript
interface ReviewRequest {
  repositoryUrl: string;
  pullRequestId?: number;
  baseSha?: string;
  headSha?: string;
  diffContent?: string;
  files?: FileChange[];
  context?: ReviewContext;
  config?: Record<string, unknown>;
}
```

### ReviewResponse

```typescript
interface ReviewResponse {
  reviewId: string;
  status: ReviewStatus;
  overallAssessment?: string;
  summary?: string;
  comments: ReviewComment[];
  metrics?: ReviewMetrics;
  createdAt?: string;
  completedAt?: string;
  error?: string;
}
```

### ReviewComment

```typescript
interface ReviewComment {
  file: string;
  line?: number;
  endLine?: number;
  severity: CommentSeverity;
  category: CommentCategory;
  message: string;
  suggestion?: string;
  confidence?: number;
}
```

### Enums

```typescript
enum ReviewStatus {
  Pending = "pending",
  Processing = "processing",
  Completed = "completed",
  Failed = "failed",
}

enum CommentSeverity {
  Info = "info",
  Warning = "warning",
  Error = "error",
  Critical = "critical",
}

enum CommentCategory {
  Security = "security",
  Performance = "performance",
  Testing = "testing",
  Architecture = "architecture",
  CodeStyle = "code_style",
  Documentation = "documentation",
  General = "general",
}
```

## CommonJS Usage

```javascript
const { AIPRClient } = require("@auralith/aipr-sdk");

const client = new AIPRClient({
  baseUrl: "https://api.aipr.example.com",
  apiKey: "your-api-key",
});

client.review({
  repositoryUrl: "https://github.com/owner/repo",
  pullRequestId: 123,
}).then((response) => {
  console.log(response.summary);
});
```

## License

Apache License 2.0
