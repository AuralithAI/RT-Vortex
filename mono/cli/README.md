# AI PR Reviewer CLI

Command-line interface for AI-powered code review.

## Installation

```bash
pip install aipr-cli
```

Or install from source:

```bash
cd cli
pip install -e .
```

## Quick Start

### Review a PR

```bash
# Review current changes
aipr review

# Review specific PR
aipr review --repo owner/repo --pr 123

# Review from diff file
aipr review --diff-file changes.diff

# Read diff from stdin
git diff | aipr review --stdin
```

### Index a Repository

```bash
# Index current repository
aipr index

# Full reindex
aipr index --full

# Index specific repository
aipr index --repo owner/repo --path /path/to/repo
```

### View Configuration

```bash
aipr config --show
```

## Configuration

### Environment Variables

```bash
export AIPR_SERVER_URL="http://localhost:8080"
export AIPR_API_KEY="your-api-key"
export AIPR_LLM_API_KEY="sk-..."
export AIPR_LLM_MODEL="gpt-4-turbo-preview"
export GITHUB_TOKEN="ghp_..."  # For GitHub PR access
```

### Config File

Create `~/.aipr/config.yml`:

```yaml
server_url: http://localhost:8080
api_key: your-api-key

llm_provider: openai-compatible
llm_api_key: sk-...
llm_model: gpt-4-turbo-preview
llm_base_url: https://api.openai.com/v1

output_format: rich  # rich, json, github
```

## Output Formats

### Rich (default)

Human-readable output with colors and formatting.

### JSON

```bash
aipr review --output json
```

Machine-readable JSON output for CI/CD integration.

### GitHub

```bash
aipr review --output github
```

GitHub Actions compatible output with `::error::` and `::warning::` annotations.

## Exit Codes

- `0` - Success, no critical issues
- `1` - Critical issues found (with `--fail-on-critical`)
- `2` - Error during execution

## CI/CD Integration

### GitHub Actions

```yaml
- name: Review PR
  run: |
    pip install aipr-cli
    aipr review --pr ${{ github.event.pull_request.number }} \
      --repo ${{ github.repository }} \
      --output github \
      --fail-on-critical
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    AIPR_LLM_API_KEY: ${{ secrets.OPENAI_API_KEY }}
```

### GitLab CI

```yaml
review:
  script:
    - pip install aipr-cli
    - aipr review --output json > review.json
  artifacts:
    reports:
      codequality: review.json
```

## License

MIT License
