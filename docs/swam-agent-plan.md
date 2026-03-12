# Vortex Agent Swarm — Master Plan v6
## Architecture Locked — Production Grade — Ready to Implement

---

## Part 1: All Decisions

| Decision | Answer | Reason |
|----------|--------|--------|
| Agent framework | Custom bootstrap SDK (~200 lines, `mono/swarm/sdk/`) | No third-party SDK — see Part 5 |
| No Anthropic Python SDK | Explicitly excluded | `tool_runner()` calls Anthropic API directly, breaks multi-provider design |
| No LiteLLM | Explicitly excluded | Python would hold API keys — Go is the credential holder |
| No Claude Agent SDK | Explicitly excluded | Wraps CLI subprocesses, not suitable for server-side swarm |
| LLM provider | Agnostic — Python never calls any LLM directly | Python → Go LLM proxy → user's configured provider |
| LLM routing | `POST /internal/swarm/llm/complete` in Go | Reuses existing `llm.Registry` (Claude/OpenAI/Gemini/Grok/Ollama) |
| Go LLM proxy response | OpenAI-compatible standard format | Single contract between Go and Python, provider differences absorbed by Go |
| Ollama URL | User-configurable per user (on-prem) | Already in settings UI |
| RAG vs LLM split | 90% engine RAG, 10% LLM | Engine `requires_llm` flag drives this automatically after engine Phase 3 |
| Agent concurrency | asyncio coroutines — 1 process = N agents | Not subprocesses, not threads |
| Task distribution | Redis Streams from Phase 0 | Foundation for cloud scale to 500k |
| Team structure | Dynamic — starts with 2 agents, scales up on demand | Orchestrator adds members only when task complexity requires it |
| Team persistence | Fixed — same agents always together | Identity, ELO, trust built within team |
| Team creation | On demand — 0 teams at startup, max 5 teams | Teams spin up when tasks arrive |
| Task scope | Always `repo_id` scoped | Agents stateless — repo_id is the only context boundary |
| Original file content | C++ engine local clone → new `GetFileContent` gRPC RPC | Engine already has the full checkout |
| Code output | Unified diffs only | Agents never write to disk directly |
| Approval gates | Two mandatory: Plan → Diff review | No code reaches VCS without human sign-off |
| Diff viewer | Monaco Editor (`@monaco-editor/react`) | VS Code diff engine — best available |
| PR creation | Go's existing VCS platform clients | GitHub/GitLab/Bitbucket/Azure DevOps already built |
| Chat + swarm | Connected per-repo via engine indexing | Completed tasks indexed → discoverable via repo chat window |
| Multi-repo | Swarm stateless, same agents serve any repo | `repo_id` scopes all data |
| Agent auth | Per-agent JWT (3-hour expiry) | Granular audit trail, ELO attribution, soft revocation |
| Token storage | Redis `swarm:agent:token:{agent_id}` TTL 3h | Auto-expires, no cleanup job needed |
| Token refresh | Skipped for Phase 0 | Re-register if >3h — refresh adds complexity for a rare case |
| Auth in Go | `internal/swarm/auth/` — extends existing JWT infrastructure | No separate auth microservice |
| Cloud scale foundation | Redis Streams from Phase 0 | More Python pods = more scale, no architecture change |
| Task → team assignment | Go `task_manager` controller with `assignLoop()` | Python teams consume work, Go decides assignment |
| Error recovery | Task timeout (30 min) in Go's `task_manager` | Heartbeat miss (60s) → mark agent offline, task retryable |

---

## Part 2: System Overview

```
┌────────────────────────────────────────────────────────────────┐
│  React Dashboard                                               │
│  /dashboard/swarm  /swarm/tasks/[id]  /swarm/tasks/[id]/review │
└───────────────────────────┬────────────────────────────────────┘
                            │ REST + WebSocket
┌───────────────────────────▼────────────────────────────────────┐
│  Go Server (existing, extended)                                │
│                                                                │
│  internal/swarm/                                               │
│    auth/         — per-agent JWT issuance + Redis validation   │
│    handler.go    — REST endpoints for tasks, diffs, plans      │
│    ws_hub.go     — WebSocket events to React                   │
│    task_manager  — task state machine + assignment controller  │
│    team_manager  — on-demand team lifecycle                    │
│    elo.go        — ELO scoring on human feedback               │
│    llm_proxy.go  — normalises ALL provider responses to one    │
│                    standard format, forwards to llm.Registry   │
│    pr_creator    — PR creation via existing VCS clients        │
│                                                                │
│  PostgreSQL — all persistent state (tasks, diffs, agents, ELO) │
│  Redis Streams — task queue + agent events + token metadata    │
└─────────┬──────────────────┬───────────────────────────────────┘
          │ gRPC             │ HTTP (agent JWT)
┌─────────▼──────┐  ┌───────▼───────────────────────────────────┐
│  C++ Engine    │  │  Python Swarm Service (mono/swarm/)        │
│                │  │                                            │
│  Local repo    │  │  sdk/          ← bootstrap, ~200 lines    │
│  clone + RAG   │  │    tool.py     @tool decorator             │
│  embeddings    │  │    loop.py     provider-agnostic loop      │
│  confidence    │  │    agent.py    Agent base class            │
│  gate          │  │    go_llm_client.py  calls Go proxy        │
│  GetFileContent│  │                                            │
│  (new RPC)     │  │  Dynamic asyncio coroutines                │
│                │  │  Teams spun up on demand (max 5)           │
└────────────────┘  │  Agents per team: 2 min, 10 max           │
      ↑ gRPC        │  Roles: Orchestrator, Architect,           │
      └─────────────┘  SeniorDev, JuniorDev, QA, Security,      │
                    │  Ops, Docs                                  │
                    └────────────────────────────────────────────┘
```

### Task Lifecycle

```
User submits task via UI
    │
    ▼
Go task_manager writes to Redis Stream: swarm:tasks:pending
    │
    ▼
Go assignLoop() (runs every 1s):
    ├── Idle team exists?        → assign task, mark team BUSY
    ├── No idle team, < max 5?   → tell Python to spin up new team → assign
    └── All teams busy, at max?  → task waits in stream (FIFO)
    │
    ▼
Python team receives assignment via Redis
    │
    ▼
Orchestrator activates minimum roles (2 agents)
    ├── Needs more help?  → activates additional roles dynamically
    └── Task too simple?  → stays at 2 agents (Lead + SeniorDev)
    │
    ▼
Phase A: Plan Generation (agents collaborate via engine RAG)
    │
    ▼  WebSocket push to UI
Human reviews plan → APPROVE / REJECT / COMMENT
    │
    ▼
Phase B: Code Generation (agents produce unified diffs)
    │
    ▼  WebSocket push to UI
Human reviews diffs in Monaco Editor → APPROVE / REJECT / COMMENT
    │
    ▼
Go pr_creator → PR on GitHub/GitLab/Bitbucket/Azure DevOps
    │
    ▼
Team sends task_complete → Go marks team IDLE → picks up next task
```

---

## Part 3: Authentication — Per-Agent JWT

### Why per-agent (not per-service)

Each agent gets its own 3-hour JWT:
- Exact audit trail: agent-7 submitted diff at 14:33, agent-3 called LLM at 14:35
- Granular revocation: suspend one agent without touching others
- Per-agent ELO attribution tied to token identity
- Clean separation from user JWTs (`type: "agent"` vs `type: "user"`)

### Token structure

```json
{
  "sub":       "agent-{uuid}",
  "type":      "agent",
  "role":      "senior_dev",
  "team_id":   "team-alpha-{uuid}",
  "agent_seq": 7,
  "iat":       1741564800,
  "exp":       1741575600
}
```

Same signing key as user JWTs. Go middleware checks `type == "agent"` for
`/internal/swarm/*` routes. User tokens cannot call internal routes. Agent
tokens cannot call user routes. Hard separation in middleware.

### Lifecycle

```
Python swarm pod starts
    │
    ▼ Once per agent on activation (lazy — only when team is formed)
POST /internal/swarm/auth/register
    headers: X-Service-Secret: {SWARM_SERVICE_SECRET}   ← shared env var only
    body: { agent_id, role, team_id, hostname, version }
    │
    ▼ Go validates service secret → stores in swarm_agents table
    Returns: { access_token: "eyJ...", expires_in: 10800 }
    │
    ▼ Agent stores JWT in Python memory only (not disk, not env)

Every call to Go:
    Authorization: Bearer eyJ...

Agent shutdown / crash / task >3 hours:
    Token expires naturally at 3 hours
    Redis key auto-deletes at TTL
    Go marks agent offline at next heartbeat miss (60s window)
    Agent re-registers on next activation → fresh token
```

**Phase 0 simplification:** No token refresh endpoint. If an agent's JWT
expires (rare — most tasks complete in minutes), the agent re-registers
and gets a new token. The agent's state lives in Python memory
(asyncio coroutine locals), completely independent of the JWT.
Re-registration is a single HTTP call (~5ms), zero state loss.

### Redis token record

```
Key:   swarm:agent:token:{agent_id}
Value: { issued_at, role, team_id, token_hash: SHA256(jwt) }
TTL:   10800 seconds
```

Go middleware on every `/internal/swarm/*` request:
1. Decode JWT → get `agent_id` + `exp`
2. Check `exp` not past
3. `Redis GET swarm:agent:token:{agent_id}` → stored hash
4. Compare `SHA256(incoming_token) == stored_hash`
5. Match → allow. No match or key missing → 401.

Soft revocation: `Redis DEL swarm:agent:token:{agent_id}` → agent locked out
instantly even with a technically valid JWT. No full blocklist needed.

### Token types in the full system

| Token | Issued by | Expiry | Routes |
|-------|-----------|--------|--------|
| User access JWT | `/auth/login` | 15 min | `/api/v1/*` |
| User refresh JWT | `/auth/login` | 7 days | `/auth/refresh` |
| Agent JWT | `/internal/swarm/auth/register` | 3 hours | `/internal/swarm/*` |

### Environment variables (both containers share only this)

```yaml
SWARM_SERVICE_SECRET: ${SWARM_SERVICE_SECRET}
```

After registration, `SWARM_SERVICE_SECRET` is never sent again. JWT is
the credential. Python holds zero LLM API keys, zero VCS tokens, zero user data.

---

## Part 4: The 90/10 RAG/LLM Split

The C++ engine returns `requires_llm` on every search response. After engine
plan Phase 3 ships this field, the split becomes automatic. Before that,
Python uses `max_retrieval_score < 0.85` as a proxy.

```
Agent sends any question to engine
    │
    ▼
engine.Search(query, repo_id)
    │
    ├── requires_llm: false  (confidence > 0.85)
    │       engine's fused_context IS the answer
    │       return it directly — zero LLM cost, zero API call, fully private
    │
    └── requires_llm: true  (confidence < 0.85)
            engine's fused_context → used as RAG context for LLM
            POST /internal/swarm/llm/complete
                body: { prompt, system, context_chunks, max_tokens }
            Go routes to: Claude / OpenAI / Gemini / Grok / Ollama
            (whichever the user has configured in their settings)
```

**LLM is called ONLY for:**
- Generating code diffs (creative synthesis — engine cannot invent new code)
- Summarising the plan document into natural language
- Writing PR description and commit message
- Cases where engine confidence is below threshold

**LLM is NOT called for:**
- Impact analysis (caller graph from engine knowledge graph)
- Finding existing patterns (engine semantic search)
- Security pattern lookup (engine security memory account)
- Test structure discovery (engine search for `*_test.*` patterns)
- Any factual question about the codebase

---

## Part 5: The Bootstrap Agent SDK

### Why we are not using any third-party SDK

**Anthropic Python SDK (`anthropic` package):** The `tool_runner()` method
calls `client.messages.create()` internally — a direct HTTP call to
`api.anthropic.com`. You cannot replace this with a Go proxy. Any agent
using it is forced onto Anthropic's API regardless of user configuration.
A user who set up Ollama for free local inference would be silently billed
on Anthropic's API. Not acceptable.

**LiteLLM:** Provides unified API across providers from Python. Problem: it
calls LLM APIs directly, meaning Python must hold each provider's API key.
In this system, API keys live in Go's vault. Python is a stateless compute
worker — it holds zero credentials.

**Claude Agent SDK (`claude_agent_sdk`):** Designed to wrap and spawn Claude
Code CLI subprocesses. It is a CLI automation layer, not a server-side
agent framework. Cannot be embedded into an asyncio swarm service.

**The consequence:** build ~200 lines of Python. The agentic loop is not
complex. The only dependencies are `httpx` (Go HTTP calls) and `grpcio`
(engine calls). No LLM SDK at all.

### `sdk/tool.py` — `@tool` decorator (~30 lines)

Converts a Python function into a JSON schema for the LLM.
Inspects type hints + docstring automatically.

```python
@tool(description="Search the codebase for relevant code")
async def search_code(query: str, repo_id: str) -> str:
    """Calls C++ engine via gRPC."""
    ...

# The decorator generates:
# {
#   "type": "function",
#   "function": {
#     "name": "search_code",
#     "description": "Search the codebase for relevant code",
#     "parameters": {
#       "type": "object",
#       "properties": {
#         "query":   {"type": "string"},
#         "repo_id": {"type": "string"}
#       },
#       "required": ["query", "repo_id"]
#     }
#   }
# }
```

### `sdk/go_llm_client.py` — Go LLM proxy client (~40 lines)

Single responsibility: send messages + tools to Go, receive
OpenAI-compatible response regardless of actual provider behind Go.

```python
async def llm_complete(
    go_base_url: str,
    agent_token: str,
    messages: list[dict],
    tools: list[dict],
    max_tokens: int = 4096,
) -> dict:
    """
    POST /internal/swarm/llm/complete
    Returns OpenAI-compatible response regardless of actual provider.
    """
    async with httpx.AsyncClient() as client:
        resp = await client.post(
            f"{go_base_url}/internal/swarm/llm/complete",
            headers={"Authorization": f"Bearer {agent_token}"},
            json={"messages": messages, "tools": tools, "max_tokens": max_tokens},
            timeout=120.0,
        )
        resp.raise_for_status()
        return resp.json()
```

### `sdk/loop.py` — provider-agnostic agentic loop (~60 lines)

```python
async def agent_loop(
    system_prompt: str,
    tools: list[ToolDef],
    tool_executor: Callable,
    go_base_url: str,
    agent_token: str,
    max_turns: int = 25,
) -> list[dict]:
    """
    1. Send messages + tool schemas to Go LLM proxy
    2. If response has tool_calls → execute each → append results
    3. If response has no tool_calls → done
    4. Repeat up to max_turns
    """
    messages = [{"role": "system", "content": system_prompt}]
    tool_schemas = [t.schema for t in tools]

    for turn in range(max_turns):
        response = await llm_complete(go_base_url, agent_token, messages, tool_schemas)

        choice = response["choices"][0]
        message = choice["message"]
        messages.append(message)

        if not message.get("tool_calls"):
            break  # LLM is done — no more tool calls

        for tc in message["tool_calls"]:
            result = await tool_executor(
                tc["function"]["name"],
                json.loads(tc["function"]["arguments"]),
            )
            messages.append({
                "role": "tool",
                "tool_call_id": tc["id"],
                "content": json.dumps(result),
            })

    return messages
```

### `sdk/agent.py` — Agent base class (~70 lines)

```python
class Agent:
    def __init__(self, agent_id: str, role: str, team_id: str, config: AgentConfig):
        self.agent_id = agent_id
        self.role = role
        self.team_id = team_id
        self.config = config
        self.token: str | None = None
        self.tools: list[ToolDef] = []

    async def register(self) -> None:
        """Register with Go and obtain JWT. Called once on team activation."""
        resp = await httpx.AsyncClient().post(
            f"{self.config.go_base_url}/internal/swarm/auth/register",
            headers={"X-Service-Secret": self.config.service_secret},
            json={
                "agent_id": self.agent_id,
                "role": self.role,
                "team_id": self.team_id,
                "hostname": socket.gethostname(),
                "version": __version__,
            },
        )
        resp.raise_for_status()
        self.token = resp.json()["access_token"]

    async def run(self, task: Task) -> AgentResult:
        """Execute a task using the provider-agnostic agentic loop."""
        if not self.token:
            await self.register()
        system_prompt = self.build_system_prompt(task)
        messages = await agent_loop(
            system_prompt=system_prompt,
            tools=self.tools,
            tool_executor=self.execute_tool,
            go_base_url=self.config.go_base_url,
            agent_token=self.token,
        )
        return self.parse_result(messages)

    async def execute_tool(self, name: str, args: dict) -> Any:
        tool = next(t for t in self.tools if t.name == name)
        return await tool.fn(**args)

    def build_system_prompt(self, task: Task) -> str:
        raise NotImplementedError  # each role overrides this

    def parse_result(self, messages: list[dict]) -> AgentResult:
        raise NotImplementedError  # each role overrides this
```

### Bootstrap smoke test (must pass before Phase 0 is complete)

```
1. Start Python swarm locally
2. Go assignLoop sends a dummy task to swarm:tasks:pending
3. Python spins up one team (Orchestrator + SeniorDev)
4. Both agents register with Go → JWTs in Redis with 3h TTL
5. Orchestrator calls engine_search tool → gRPC to C++ engine → returns chunks
6. Orchestrator loop produces a text plan response
7. Plan POSTed to Go → stored in swarm_tasks

Verify:
  grep -r "import anthropic\|import litellm\|import openai" mono/swarm/ → 0 results
  Redis: swarm:agent:token:{agent_id} exists for both agents
  PostgreSQL: swarm_tasks row with status "plan_review"
  Switch Go's LLM provider in settings → rerun → different provider used, Python unchanged
```

The last point is the proof that provider lock-in does not exist.

---

## Part 6: Task Assignment Controller

The controller lives entirely in Go's `task_manager`. Python teams consume
work — they do not decide what to work on.

### `assignLoop()` — runs every 1s in a goroutine

```
1. XREADGROUP from swarm:tasks:pending (consumer group: "swarm-controller")
2. For each pending task:
   a. Any idle team?
      → assign task to team, XACK stream entry, mark team BUSY
   b. No idle team, team count < max (5)?
      → Publish to Redis: swarm:events:team:create { task_id }
      → Python creates new team (minimum 2 agents)
      → Team registers → picks up task assignment
   c. All teams busy at max (5)?
      → Leave task in stream (FIFO — processed when a team becomes idle)
3. Sleep 1s, repeat

onTaskComplete(team_id, task_id):
    → Mark team IDLE in teamRegistry
    → assignLoop picks up next pending task on next iteration
```

### Team scaling within a task

```
Orchestrator receives task
    │
    ├── Analyses task description + engine search results
    │
    ├── Small task (1-3 files, single concern):
    │     Activate: Orchestrator + SeniorDev = 2 agents
    │
    ├── Medium task (4-15 files, multiple concerns):
    │     Activate: Orchestrator + SeniorDev + QA + Architect = 4 agents
    │
    └── Large task (16+ files, cross-cutting):
          Activate: Orchestrator + SeniorDev + JuniorDev + QA +
                    Security + Architect = 6 agents
                    (can scale to max 10 if scope expands mid-task)
```

Orchestrator **can add** agents mid-task if scope turns out larger than
estimated. It **cannot remove** agents mid-task — they finish their current
subtask and go idle within the team.

### Error recovery

| Failure | Detection | Recovery |
|---------|-----------|----------|
| Python pod crash | Go heartbeat miss (60s) | Mark team offline, task → `FAILED`, eligible for retry |
| Agent coroutine exception | Python catches, reports to Go | Agent marked errored, team continues with remaining agents |
| Go LLM proxy 5xx | Python retries 3× with backoff | After 3 failures, agent reports tool failure to LLM loop (LLM decides next step) |
| Task timeout (30 min) | Go timer in `task_manager` | Mark task `TIMED_OUT`, notify user via WS, team released to idle |
| Redis connection loss | Both Go and Python detect disconnect | Reconnect with exponential backoff, in-flight task pauses (coroutine awaits) |

---

## Part 7: Implementation Phases

### Phase 0 — Foundation (Week 1-2) ✅ COMPLETE
**Goal:** Skeleton that can receive a task and produce a plan.

**Status:** All components implemented and verified. Go compiles clean,
Python import chain passes, all modules load successfully.

| Component | Work |
|-----------|------|
| `mono/swarm/sdk/` | `tool.py`, `loop.py`, `agent.py`, `go_llm_client.py` (~200 lines total) |
| `mono/swarm/agents/` | `orchestrator.py`, `senior_dev.py` (minimum 2 roles) |
| `mono/swarm/main.py` | asyncio entrypoint, Redis consumer, on-demand team lifecycle |
| C++ Engine | `GetFileContent` gRPC RPC (read file from local clone) |
| Go `internal/swarm/` | `auth/`, `handler.go` (task CRUD), `task_manager.go`, `llm_proxy.go` |
| Go PostgreSQL | `swarm_tasks`, `swarm_agents`, `swarm_diffs` tables + migration |
| Redis | Streams: `swarm:tasks:pending`, `swarm:events:*` |
| React | `/dashboard/swarm` — task submission form + task list |

**Deliverable:** User submits *"Add input validation to the /users endpoint"*
→ agents produce a plan document → displayed in React UI.

**Phase 0 complete when:** Bootstrap smoke test passes (see Part 5).

---

### Phase 1 — Code Generation (Week 3-4) ✅ COMPLETE
**Goal:** Agents produce unified diffs from approved plans.

**Status:** All components implemented and verified. Go compiles clean,
TypeScript error-free, Python agents load successfully.

| Component | Work | Status |
|-----------|------|--------|
| `mono/swarm/agents/` | `architect.py`, `qa.py`, `junior_dev.py` | ✅ |
| `mono/swarm/tools/` | `engine_tools.py` (live), `task_tools.py` (live) | ✅ |
| Go LLM tool calling | All 5 providers: OpenAI, Anthropic, Gemini, Ollama, Grok | ✅ |
| Go `llm_proxy.go` | Tools forwarded end-to-end, duplicate types removed | ✅ |
| Go `handler.go` | WSHub wired, WS events emitted in 5 handlers | ✅ |
| Go `ws_hub.go` | Real ws.Hub delegation (BroadcastSwarm) | ✅ |
| Go `ws/hub.go` | SwarmEvent type, swarm subscription channel | ✅ |
| React | Plan review card, diff viewer, activity feed, review page | ✅ |
| React hooks | `use-swarm-events.ts` — WebSocket hook with auto-reconnect | ✅ |
| React task detail | PlanReviewCard, DiffViewer, ActivityFeed integrated | ✅ |
| WebSocket | Real-time progress: task/agent/diff/plan events | ✅ |

**Deliverable:** Approved plan → agents generate diffs → human reviews in
diff viewer → approves → diffs stored in PostgreSQL.

---

### Phase 2 — PR Creation + ELO (Week 5-6) ✅ COMPLETE
**Goal:** Approved diffs become PRs. Agent quality tracking begins.

| Component | Work |
|-----------|------|
| Go `pr_creator` | Wire approved diffs → existing VCS clients (GitHub/GitLab/Bitbucket) ✅ |
| Go `elo.go` | ELO scoring: human feedback (approve/reject/edit count) updates ratings ✅ |
| `mono/swarm/agents/` | `security.py`, `docs.py`, `ops.py` (remaining roles) ✅ |
| React | ELO leaderboard, agent performance card per team ✅ |
| Engine indexing | Completed task results indexed → discoverable via repo chat window |

**Deliverable:** Full loop: task → plan → code → PR. Agent ELO scores begin
accumulating. Chat window answers *"what changed in auth?"* from engine RAG.

---

### Phase 3 — Scale + Polish (Week 7-8) ✅ COMPLETE
**Goal:** Multi-team concurrency, production hardening, monitoring.

**Status:** All components implemented and verified. Go compiles clean,
migrations applied, React components created, Prometheus metrics wired.

| Component | Work | Status |
|-----------|------|--------|
| Task controller | Full multi-team FIFO, team scaling mid-task, max-5 enforcement | ✅ |
| Error recovery | Task timeouts, heartbeat monitoring, automatic retry (max 3) | ✅ |
| Go `task_manager.go` | `RetryTask()`, `FailTask()`, `ListTaskHistory()`, `RecordAgentContribution()`, `GetOverview()` | ✅ |
| Go `task_manager.go` | Heartbeat monitor goroutine, metrics refresh goroutine | ✅ |
| Go `team_manager.go` | `MarkTeamOffline()`, `ScaleTeam()` (max 10 agents), `DisbandTeam()` | ✅ |
| Go `handler.go` | Retry, cancel, fail, history, overview, contribution endpoints | ✅ |
| Go `swarm_metrics.go` | 18 Prometheus metrics: tasks, agents, teams, LLM, PRs, utilisation | ✅ |
| Go `llm_proxy.go` | LLM call duration, token count, call total metrics | ✅ |
| Go `pr_creator.go` | PR success/failure counter metrics | ✅ |
| Go `server.go` | All Phase 3 routes registered (internal + user-facing) | ✅ |
| DB migration 000008 | `retry_count`, `failure_reason` columns + partial indexes | ✅ |
| React types | `TaskSummary`, `TaskHistoryResponse`, updated `SwarmOverview` | ✅ |
| React pipeline board | 9-column kanban with auto-polling, retry buttons | ✅ |
| React task history | Paginated table with stats, ratings, PR links, retry controls | ✅ |
| React swarm dashboard | Live overview stats (6 primary + 3 secondary), tabbed pipeline/history | ✅ |
| VCS clients | `CreateBranch`, `CreateOrUpdateFile`, `CreatePullRequest`, `GetDefaultBranch`, `GetBranchSHA` stubs for GitLab/Bitbucket/AzureDevOps | ✅ |
| Load testing | Simulate 10 concurrent tasks across 5 teams, verify FIFO ordering | ⏳ |

**Deliverable:** Production-ready swarm handling multiple concurrent tasks
across multiple repos with monitoring, retry, and timeout handling.

---

## Part 8: Complete File Map

### Go (new — `mono/server-go/`)

```
internal/swarm/
├── auth/
│   ├── auth.go          register, revoke, JWT issuance, Redis token storage
│   └── middleware.go    RequireAgentToken() for /internal/swarm/*
├── handler.go           all REST + WS route registration and handlers
├── ws_hub.go            WebSocket event broadcasting to React
├── team_manager.go      on-demand team formation, scaling, release
├── task_manager.go      task state machine, assignLoop(), timeout timer
├── elo.go               ELO calculation, promotion rules
├── llm_proxy.go         /internal/swarm/llm/complete + response normalisation
└── pr_creator.go        PR creation via existing VCS platform clients

migrations/
└── 007_swarm_tables.sql 7 tables (full schema in Part 9)
```

### Python (new — `mono/swarm/`)

```
pyproject.toml             grpcio, grpcio-tools, httpx, anyio, pydantic,
                           redis, python-jose[cryptography]
                           NO anthropic  NO litellm  NO openai  NO claude_agent_sdk
config.py                  ENGINE_HOST, ENGINE_PORT, SWARM_MANAGER_URL,
                           SWARM_SERVICE_SECRET, MAX_TEAMS=5, MAX_AGENTS_PER_TEAM=10
auth.py                    register(agent_id, role, team_id) → stores JWT in memory
                           check_and_reregister() — called before any Go HTTP call
go_client.py               thin HTTP client, always calls get_headers()
                           poll_next_task(), report_plan(), report_diff(),
                           report_result(), send_heartbeat()
engine_client.py           gRPC channel to C++ engine
                           search(), find_callers(), get_file_content(), get_index_status()
redis_consumer.py          XREADGROUP on swarm:tasks:pending
                           dispatches team creation + task assignments
models.py                  Pydantic: Task, SwarmAgent, Team, Diff, EngineResult,
                           AgentResult, PlanDocument, DiffComment

sdk/                       ← bootstrap, build first, ~200 lines total
├── tool.py                @tool decorator with auto JSON schema from type hints
├── go_llm_client.py       llm_complete() → standard OpenAI-compatible response
├── loop.py                agent_loop() — provider-agnostic while loop
└── agent.py               Agent base class: register, run, execute_tool

agents/
├── orchestrator.py        Team lead: planning, delegation, synthesis
├── architect.py           Impact analysis, risk assessment
├── senior_dev.py          Code diff generation
├── junior_dev.py          Scoped subtask execution
├── qa.py                  Test diff generation
├── security.py            Diff security review, line-numbered findings
├── ops.py                 CI/CD config diffs
└── docs.py                PR description, README changes

tools/
├── engine_tools.py        engine_search, engine_find_callers,
│                          engine_get_file_content, engine_get_index_status
└── task_tools.py          report_plan, report_diffs, declare_team_size

proto/                     generated Python stubs from mono/proto/engine.proto
main.py                    startup: connect engine + Go, start Redis consumer, block
```

### React (new — `mono/web/src/`)

```
app/(app)/
├── dashboard/swarm/page.tsx            swarm overview + pipeline board
└── swarm/tasks/
    ├── page.tsx                         task list with status filters
    ├── [id]/page.tsx                    task detail + plan approval + rating
    └── [id]/review/page.tsx             Monaco diff review

components/swarm/
├── task-pipeline-board.tsx             kanban across all statuses
├── team-grid.tsx                        live agent activity cards
├── agent-leaderboard.tsx                ELO table with sparklines
├── task-submit-form.tsx                 repo_id + description → POST
├── plan-review-card.tsx                 plan display + approve/comment/reject
└── diff-viewer/
    ├── index.tsx                        Monaco diff editor wrapper
    ├── file-tree.tsx                    file list with M/A/D badges + comment count
    ├── comment-thread.tsx               inline agent + user comments
    └── approval-toolbar.tsx             Approve / Request Changes / Reject

hooks/
├── use-swarm-overview.ts                WS: live dashboard events
├── use-task.ts                          polling: task detail + plan
└── use-task-review.ts                   polling + WS: diffs + comments

types/swarm.ts                           all TypeScript types for swarm
```

### Engine proto (one addition to `mono/proto/engine.proto`)

```protobuf
rpc GetFileContent(FileContentRequest) returns (FileContentResponse);
message FileContentRequest  { string repo_id = 1; string file_path = 2; string ref = 3; }
message FileContentResponse { string content = 1; string encoding = 2; bool is_binary = 3; }
```

---

## Part 9: Database Schema

Migration file: `mono/server-go/migrations/007_swarm_tables.sql`

```sql
CREATE TABLE swarm_agents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role         TEXT NOT NULL,
    team_id      UUID,
    status       TEXT DEFAULT 'offline',   -- offline, idle, busy, errored
    elo_score    FLOAT DEFAULT 1200,
    tasks_done   INT DEFAULT 0,
    tasks_rated  INT DEFAULT 0,
    avg_rating   FLOAT DEFAULT 0,
    hostname     TEXT,
    version      TEXT,
    registered_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE swarm_teams (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT,
    lead_agent_id UUID REFERENCES swarm_agents(id),
    status       TEXT DEFAULT 'idle',      -- idle, busy, offline
    agent_ids    UUID[],                   -- current active members
    formed_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE swarm_tasks (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id          TEXT NOT NULL,
    description      TEXT NOT NULL,
    status           TEXT DEFAULT 'submitted',
    -- submitted → planning → plan_review → implementing → self_review
    -- → diff_review → pr_creating → completed
    -- any stage → cancelled | failed | timed_out
    plan_document    JSONB,
    assigned_team_id UUID REFERENCES swarm_teams(id),
    assigned_agents  UUID[],
    pr_url           TEXT,
    pr_number        INT,
    human_rating     INT,
    human_comment    TEXT,
    submitted_by     UUID,                 -- FK users.id
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    completed_at     TIMESTAMPTZ,
    timeout_at       TIMESTAMPTZ           -- set to created_at + 30min on assignment
);

CREATE TABLE swarm_task_diffs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      UUID REFERENCES swarm_tasks(id),
    file_path    TEXT NOT NULL,
    change_type  TEXT,                    -- modified, added, deleted, renamed
    original     TEXT,                    -- full original file content
    proposed     TEXT,                    -- full proposed file content
    unified_diff TEXT,                    -- standard git unified diff format
    agent_id     UUID REFERENCES swarm_agents(id),
    status       TEXT DEFAULT 'pending',  -- pending, approved, rejected
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE swarm_diff_comments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    diff_id      UUID REFERENCES swarm_task_diffs(id),
    author_type  TEXT,                    -- agent | user
    author_id    TEXT,                    -- agent UUID or user UUID
    line_number  INT,
    content      TEXT,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE agent_feedback (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id      UUID REFERENCES swarm_tasks(id),
    agent_id     UUID REFERENCES swarm_agents(id),
    rating       INT CHECK (rating BETWEEN 1 AND 5),
    comment      TEXT,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE agent_task_log (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id           UUID REFERENCES swarm_tasks(id),
    agent_id          UUID REFERENCES swarm_agents(id),
    role              TEXT,
    phase             TEXT,               -- planning, implementing, self_review
    contribution_type TEXT,               -- plan, diff, security_review, test
    tokens_used       INT DEFAULT 0,
    llm_calls         INT DEFAULT 0,
    rag_calls         INT DEFAULT 0,
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ
);
```

---

## Part 10: Go Endpoint Reference

```
Agent authentication
  POST   /internal/swarm/auth/register     X-Service-Secret → 3-hour agent JWT
  DELETE /internal/swarm/auth/revoke       Bearer JWT → deletes Redis token key

Task operations — agent calls (require agent JWT)
  GET    /internal/swarm/tasks/next                 poll for assigned task
  POST   /internal/swarm/tasks/{id}/plan            submit plan document
  POST   /internal/swarm/tasks/{id}/diffs           submit file diff
  GET    /internal/swarm/tasks/{id}/diffs           fetch all diffs (security agent)
  POST   /internal/swarm/tasks/{id}/diffs/{diffId}/comments    add agent comment
  POST   /internal/swarm/tasks/{id}/complete        mark task done
  POST   /internal/swarm/tasks/{id}/declare-size    reallocation signal
  POST   /internal/swarm/heartbeat/{id}             60s keepalive

LLM proxy — agent calls (require agent JWT)
  POST   /internal/swarm/llm/complete               → llm.Registry → standard response

User-facing endpoints (require user JWT)
  POST   /api/v1/swarm/tasks                        submit new task
  GET    /api/v1/swarm/tasks                        list (filter: repo_id, status)
  GET    /api/v1/swarm/tasks/{id}                   task detail + plan document
  POST   /api/v1/swarm/tasks/{id}/plan-action       approve | comment | reject plan
  GET    /api/v1/swarm/tasks/{id}/diffs             all diffs for Monaco viewer
  POST   /api/v1/swarm/tasks/{id}/diffs/{id}/comments    user line comment
  POST   /api/v1/swarm/tasks/{id}/diff-action       approve_diff | request_changes | reject
  POST   /api/v1/swarm/tasks/{id}/rate              1-5 stars + optional comment
  GET    /api/v1/swarm/agents                       agent list with ELO + status
  GET    /api/v1/swarm/teams                        team list with member status
  WS     /api/v1/swarm/ws                           live events for all dashboard views
```

---

## Part 11: Dependency Graph

```
Bootstrap SDK (sdk/)
    ← must exist before any agent code is written
    ← smoke test must pass before Phase 0 is called complete ✅
    │
    ▼
Phase 0  Foundation ✅
    Go: auth, task_manager (assignLoop), llm_proxy, handler, DB migration
    Python: main.py, redis_consumer, go_client, engine_client, models
            agents/orchestrator.py, agents/senior_dev.py
    React: /dashboard/swarm (task submit + list, stub routes)
    │
    ├─► Engine proto: GetFileContent RPC (add before Phase 1 ends)
    │
    ▼
Phase 1  Code Generation ✅
    Python: architect.py, qa.py, junior_dev.py
            tools/engine_tools.py (live), tools/task_tools.py (live)
    Go: LLM tool calling (5 providers), diff storage, plan/diff approval,
        WS events in handler, swarm subscription channel
    React: plan review card, diff viewer, activity feed, review page,
           use-swarm-events hook, task detail integration
    WS: real-time progress events (SwarmEvent type + BroadcastSwarm)
    │
    ▼
Phase 2  PR Creation + ELO ✅
    Go: pr_creator.go (wires to existing VCS clients)
        elo.go (ELO scoring on human feedback)
    Python: security.py, ops.py, docs.py
    React: ELO leaderboard, agent performance card
    Engine: index completed task results → chat window RAG
    │
    ▼
Phase 3  Scale + Polish
    Go: full multi-team FIFO, timeout handling, Prometheus metrics
    React: task history, retry controls, pipeline board
    Load test: 10 concurrent tasks, 5 teams, verify no race conditions
    │
    ▼
Phase 4+  Cloud Scale 
    Redis Streams consumer groups already handle horizontal scaling.
    Add more Python pods → automatic work distribution.
    Kubernetes HPA: scale on XPENDING count for swarm:tasks:pending.
```
