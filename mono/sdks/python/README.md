# RTVortex Python SDK

Official Python SDK for the [RTVortex](https://github.com/AuralithAI/RT-AI-PR-Reviewer) AI code review API.

## Installation

```bash
pip install rtvortex-sdk
```

## Quick Start

```python
from rtvortex_sdk import RTVortexClient

client = RTVortexClient(token="your-api-token")

# Trigger a review
review = client.trigger_review(repo_id="...", pr_number=42)

# List organizations
orgs = client.list_orgs()

# Stream review progress (SSE)
for event in client.stream_review(review.id):
    print(f"Step {event.step_index}/{event.total_steps}: {event.message}")
```

### Async Usage

```python
import asyncio
from rtvortex_sdk import AsyncRTVortexClient

async def main():
    async with AsyncRTVortexClient(token="your-api-token") as client:
        review = await client.trigger_review(repo_id="...", pr_number=42)
        async for event in client.stream_review(review.id):
            print(event)

asyncio.run(main())
```

## API Coverage

| Method | Endpoint | Description |
|---|---|---|
| `me()` | GET /user/me | Current user profile |
| `update_me(...)` | PUT /user/me | Update profile |
| `list_orgs(...)` | GET /orgs | List organizations |
| `create_org(...)` | POST /orgs | Create organization |
| `get_org(id)` | GET /orgs/{id} | Get org details |
| `update_org(...)` | PUT /orgs/{id} | Update org |
| `list_members(...)` | GET /orgs/{id}/members | List members |
| `invite_member(...)` | POST /orgs/{id}/members | Invite member |
| `remove_member(...)` | DELETE /orgs/{id}/members/{uid} | Remove member |
| `list_repos(...)` | GET /repos | List repositories |
| `register_repo(...)` | POST /repos | Register repo |
| `get_repo(id)` | GET /repos/{id} | Get repo details |
| `trigger_review(...)` | POST /reviews | Trigger review |
| `get_review(id)` | GET /reviews/{id} | Get review |
| `get_review_comments(id)` | GET /reviews/{id}/comments | Get comments |
| `stream_review(id)` | GET /reviews/{id} (SSE) | Stream progress |
| `trigger_index(id)` | POST /repos/{id}/index | Trigger indexing |
| `get_index_status(id)` | GET /repos/{id}/index/status | Index status |
| `get_stats()` | GET /admin/stats | Admin stats |
| `health()` | GET /ready | Server health |

## License

Apache-2.0
