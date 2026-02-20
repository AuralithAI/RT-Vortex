# AI-PR-Reviewer Python SDK

Python client library for interacting with the AI-PR-Reviewer API.

## Requirements

- Python 3.10+

## Installation

```bash
pip install aipr-sdk
```

Or with development dependencies:

```bash
pip install aipr-sdk[dev]
```

## Quick Start

### Synchronous Client

```python
from aipr_sdk import AIPRClient, ReviewRequest

# Create client
client = AIPRClient(
    base_url="https://api.aipr.example.com",
    api_key="your-api-key"
)

# Submit a review
response = client.review(ReviewRequest(
    repository_url="https://github.com/owner/repo",
    pull_request_id=123,
    base_sha="abc123",
    head_sha="def456",
))

# Process comments
for comment in response.comments:
    print(f"[{comment.severity}] {comment.file}:{comment.line} - {comment.message}")

# Close client when done
client.close()
```

### Context Manager

```python
from aipr_sdk import AIPRClient, ReviewRequest

with AIPRClient(base_url="https://api.aipr.example.com") as client:
    response = client.review(ReviewRequest(
        repository_url="https://github.com/owner/repo",
        pull_request_id=123,
    ))
    print(response.summary)
```

### Async Client

```python
import asyncio
from aipr_sdk import AsyncAIPRClient, ReviewRequest

async def main():
    async with AsyncAIPRClient(base_url="https://api.aipr.example.com") as client:
        response = await client.review(ReviewRequest(
            repository_url="https://github.com/owner/repo",
            pull_request_id=123,
        ))
        
        # Wait for review completion with polling
        if not response.is_complete:
            response = await client.wait_for_review(
                response.review_id,
                poll_interval=5.0,
                timeout=300.0,
            )
        
        print(f"Review complete: {response.summary}")

asyncio.run(main())
```

## Error Handling

```python
from aipr_sdk import AIPRClient, AIPRAPIError, AIPRTimeoutError
import time

client = AIPRClient(base_url="https://api.aipr.example.com")

try:
    response = client.review(request)
except AIPRTimeoutError:
    print("Request timed out, try again later")
except AIPRAPIError as e:
    if e.is_rate_limit_error:
        print("Rate limited, waiting 60 seconds...")
        time.sleep(60)
    elif e.is_client_error:
        print(f"Invalid request: {e}")
    elif e.is_server_error:
        print(f"Server error: {e}")
```

## Repository Indexing

```python
from aipr_sdk import AIPRClient, IndexRequest
import time

client = AIPRClient(base_url="https://api.aipr.example.com")

# Start indexing
index_job = client.index(IndexRequest(
    repository_url="https://github.com/owner/repo",
    branch="main",
    force_reindex=False,
))

# Poll for completion
while not index_job.is_complete:
    time.sleep(5)
    index_job = client.get_index_status(index_job.job_id)
    print(f"Progress: {index_job.progress_percent:.1f}%")

if index_job.is_success:
    print(f"Indexed {index_job.files_indexed} files")
else:
    print(f"Indexing failed: {index_job.error}")
```

## Configuration

```python
client = AIPRClient(
    base_url="https://api.aipr.example.com",
    api_key="your-api-key",
    timeout=600.0,       # 10 minute timeout for large reviews
    connect_timeout=30.0 # 30 second connection timeout
)
```

## Models

### ReviewRequest

| Field | Type | Description |
|-------|------|-------------|
| `repository_url` | `str` | Repository URL (required) |
| `pull_request_id` | `int` | PR number |
| `base_sha` | `str` | Base commit SHA |
| `head_sha` | `str` | Head commit SHA |
| `diff_content` | `str` | Raw diff content |
| `files` | `list[FileChange]` | Changed files |
| `context` | `ReviewContext` | PR context |
| `config` | `dict` | Review configuration |

### ReviewResponse

| Field | Type | Description |
|-------|------|-------------|
| `review_id` | `str` | Unique review ID |
| `status` | `str` | Status: pending, processing, completed, failed |
| `summary` | `str` | Review summary |
| `comments` | `list[ReviewComment]` | Review comments |
| `metrics` | `ReviewMetrics` | Review metrics |
| `is_complete` | `bool` | Whether review is finished |
| `is_success` | `bool` | Whether review succeeded |

### ReviewComment

| Field | Type | Description |
|-------|------|-------------|
| `file` | `str` | File path |
| `line` | `int` | Line number |
| `severity` | `str` | info, warning, error, critical |
| `category` | `str` | security, performance, testing, etc. |
| `message` | `str` | Comment message |
| `suggestion` | `str` | Suggested fix |
| `confidence` | `float` | Confidence score 0-1 |

## License

Apache License 2.0
