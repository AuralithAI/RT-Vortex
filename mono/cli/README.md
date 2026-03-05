# RTVortex CLI

Production-grade command-line interface for [RTVortex](https://github.com/AuralithAI/RT-AI-PR-Reviewer) — AI-powered code review.

## Installation

```bash
pip install rtvortex-cli
```

Or with [pipx](https://pipx.pypa.io/) (recommended):

```bash
pipx install rtvortex-cli
```

## Quick Start

```bash
# Authenticate (paste API token from Web UI)
rtvortex auth login

# Trigger a review
rtvortex review --repo-id <REPO_ID> --pr 42

# Watch review progress in real time
rtvortex review --repo-id <REPO_ID> --pr 42 --watch

# Trigger codebase indexing
rtvortex index --repo-id <REPO_ID>

# Check system status
rtvortex status
```

## Authentication

Generate an API token from the RTVortex Web UI and authenticate:

```bash
rtvortex auth login
```

The token is stored in `~/.config/rtvortex/config.yaml` (Linux/macOS) with `0600` permissions. You can also set the `RTVORTEX_TOKEN` environment variable.

## Commands

| Command | Description |
|---|---|
| `rtvortex auth login` | Save API token |
| `rtvortex auth logout` | Remove stored token |
| `rtvortex auth whoami` | Show current user info |
| `rtvortex review` | Trigger a PR review |
| `rtvortex index` | Trigger repository indexing |
| `rtvortex status` | Show server health & stats |
| `rtvortex config show` | Display current configuration |
| `rtvortex config set KEY VALUE` | Set a configuration value |

### Review Options

```bash
rtvortex review \
  --repo-id <UUID> \
  --pr <NUMBER> \
  --watch              # Poll until complete with progress display
  --output table|json|markdown
```

### Index Options

```bash
rtvortex index \
  --repo-id <UUID> \
  --follow             # Poll index status with progress bar
```

## Configuration

Configuration is loaded in priority order:

1. Command-line flags
2. Environment variables (`RTVORTEX_TOKEN`, `RTVORTEX_SERVER`)
3. Config file (`~/.config/rtvortex/config.yaml`)

### Environment Variables

| Variable | Description | Default |
|---|---|---|
| `RTVORTEX_TOKEN` | API token | — |
| `RTVORTEX_SERVER` | Server URL | `http://localhost:8080` |

## Development

```bash
git clone https://github.com/AuralithAI/RT-AI-PR-Reviewer.git
cd mono/cli
pip install -e ".[dev]"
pytest
ruff check src/
mypy src/
```

## License

Apache-2.0 — see [LICENSE](../../LICENSE).
