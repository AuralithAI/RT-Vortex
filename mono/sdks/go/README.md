# RTVortex Go SDK

Official Go client for the [RTVortex](https://github.com/AuralithAI/rtvortex) AI-powered code review API.

## Installation

```bash
go get github.com/AuralithAI/rtvortex-sdk-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    rtvortex "github.com/AuralithAI/rtvortex-sdk-go"
    "github.com/google/uuid"
)

func main() {
    ctx := context.Background()

    client := rtvortex.NewClient("https://api.rtvortex.example.com",
        rtvortex.WithToken("your-jwt-token"),
    )

    // Create a swarm review task
    task, err := client.Swarm.CreateTask(ctx, rtvortex.CreateTaskRequest{
        RepoID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
        Title:  "Review PR #42",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Task created: %s (status: %s)\n", task.ID, task.Status)

    // Stream real-time events
    events, err := client.Swarm.StreamEvents(ctx, task.ID)
    if err != nil {
        log.Fatal(err)
    }
    for evt := range events {
        fmt.Printf("[%s] %s\n", evt.Type, evt.Event)
    }

    // Rate the task
    _ = client.Swarm.RateTask(ctx, task.ID, 5, "Excellent review!")

    // Get swarm overview
    overview, _ := client.Swarm.Overview(ctx)
    fmt.Printf("Active tasks: %d, Agents: %d\n", overview.ActiveTasks, overview.ActiveAgents)
}
```

## Available Methods

### Swarm

| Method | Description |
|--------|-------------|
| `Swarm.CreateTask()` | Create a new swarm review task |
| `Swarm.ListTasks()` | List tasks with status filter |
| `Swarm.GetTask()` | Get task details |
| `Swarm.DeleteTask()` | Delete a task |
| `Swarm.CancelTask()` | Cancel a running task |
| `Swarm.RetryTask()` | Retry a failed task |
| `Swarm.RateTask()` | Rate a completed task (ELO) |
| `Swarm.PlanAction()` | Approve/reject swarm plan |
| `Swarm.DiffAction()` | Approve/reject produced diffs |
| `Swarm.ListDiffs()` | List diffs for a task |
| `Swarm.ListAgents()` | List all registered agents |
| `Swarm.Overview()` | Dashboard summary stats |
| `Swarm.HITLRespond()` | Human-in-the-loop response |
| `Swarm.StreamEvents()` | WebSocket event stream |

### Repos

| Method | Description |
|--------|-------------|
| `Repos.List()` | List repositories |
| `Repos.Get()` | Get repository details |
| `Repos.TriggerIndex()` | Start re-indexing |

### Reviews

| Method | Description |
|--------|-------------|
| `Reviews.List()` | List reviews |
| `Reviews.Get()` | Get review details |

## Error Handling

All methods return `*rtvortex.APIError` for HTTP errors:

```go
task, err := client.Swarm.GetTask(ctx, taskID)
if err != nil {
    var apiErr *rtvortex.APIError
    if errors.As(err, &apiErr) {
        fmt.Printf("HTTP %d: %s\n", apiErr.StatusCode, apiErr.Body)
    }
}
```

## License

MIT — see [LICENSE](../../LICENSE).
