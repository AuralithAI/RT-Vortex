# @rtvortex/sdk

Official Node.js / TypeScript SDK for the **RTVortex** AI code-review platform.

## Installation

```bash
npm install @rtvortex/sdk
# or
yarn add @rtvortex/sdk
# or
pnpm add @rtvortex/sdk
```

## Quick Start

```typescript
import { RTVortexClient } from "@rtvortex/sdk";

const client = new RTVortexClient({ token: process.env.RTVORTEX_TOKEN! });

// Get current user
const user = await client.me();
console.log(user.email);

// Trigger a review
const review = await client.triggerReview({
  repo_id: "repo-123",
  pr_number: 42,
});

// Stream progress
for await (const event of client.streamReview(review.id)) {
  console.log(`[${event.step}] ${event.status}: ${event.message}`);
}

// Get comments
const comments = await client.getReviewComments(review.id);
comments.forEach((c) => console.log(`${c.severity}: ${c.message}`));
```

## API Reference

| Method | Description |
|--------|-------------|
| `me()` | Get authenticated user |
| `updateMe(data)` | Update user profile |
| `listOrgs(pagination?)` | List organizations |
| `createOrg(data)` | Create organization |
| `getOrg(id)` | Get organization |
| `updateOrg(id, data)` | Update organization |
| `listMembers(orgId, pagination?)` | List org members |
| `inviteMember(orgId, data)` | Invite member |
| `removeMember(orgId, userId)` | Remove member |
| `listRepos(pagination?)` | List repositories |
| `registerRepo(data)` | Register repository |
| `getRepo(id)` | Get repository |
| `updateRepo(id, fields)` | Update repository |
| `deleteRepo(id)` | Delete repository |
| `listReviews(pagination?)` | List reviews |
| `triggerReview(data)` | Trigger a review |
| `getReview(id)` | Get review |
| `getReviewComments(id)` | Get review comments |
| `streamReview(id)` | Stream review progress (SSE) |
| `triggerIndex(repoId)` | Trigger indexing |
| `getIndexStatus(repoId)` | Get index status |
| `getStats()` | Get admin stats |
| `health()` | Health check |
| `healthDetailed()` | Detailed health check |

## Error Handling

```typescript
import {
  RTVortexError,
  AuthenticationError,
  NotFoundError,
} from "@rtvortex/sdk";

try {
  await client.getReview("nonexistent");
} catch (err) {
  if (err instanceof NotFoundError) {
    console.log("Review not found");
  } else if (err instanceof AuthenticationError) {
    console.log("Invalid token");
  } else if (err instanceof RTVortexError) {
    console.log(`API error ${err.statusCode}: ${err.message}`);
  }
}
```

## Requirements

- Node.js 18+ (uses native `fetch`)
- TypeScript 5.0+ (recommended)

## License

Apache-2.0
