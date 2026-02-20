# AI PR Reviewer - GitHub Action

GitHub Action for automated AI-powered code review on pull requests.

## Features

- 🔍 **Deep Code Analysis**: Reviews code changes using RAG (Retrieval-Augmented Generation) for context-aware feedback
- 🔐 **Security Scanning**: Detects secrets, SQL injection, XSS, and other vulnerabilities
- 🐛 **Bug Detection**: Identifies potential bugs, edge cases, and error handling issues
- ⚡ **Performance Review**: Flags N+1 queries, inefficient algorithms, and scalability concerns
- 📝 **Test Coverage**: Suggests missing test cases and validates test quality
- 🏗️ **Architecture Feedback**: Ensures consistency with codebase patterns

## Usage

### Basic Setup

```yaml
name: AI PR Review

on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    
    steps:
      - uses: actions/checkout@v4
      
      - name: AI PR Review
        uses: your-org/ai-pr-reviewer@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          llm-api-key: ${{ secrets.OPENAI_API_KEY }}
```

### With Custom Configuration

```yaml
- name: AI PR Review
  uses: your-org/ai-pr-reviewer@v1
  with:
    github-token: ${{ secrets.GITHUB_TOKEN }}
    llm-api-key: ${{ secrets.OPENAI_API_KEY }}
    llm-model: 'gpt-4-turbo-preview'
    review-level: 'deep'
    focus-areas: 'security,performance,testing'
    max-comments: 30
    fail-on-critical: true
    ignore-patterns: |
      **/*.generated.*
      **/vendor/**
      docs/**
```

### Using Self-Hosted Server

```yaml
- name: AI PR Review
  uses: your-org/ai-pr-reviewer@v1
  with:
    github-token: ${{ secrets.GITHUB_TOKEN }}
    aipr-endpoint: 'https://aipr.your-company.com'
    aipr-api-key: ${{ secrets.AIPR_API_KEY }}
```

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `github-token` | GitHub token for API access | Yes | `${{ github.token }}` |
| `aipr-endpoint` | Self-hosted AI PR Reviewer endpoint | No | - |
| `aipr-api-key` | API key for self-hosted server | No | - |
| `llm-provider` | LLM provider to use | No | `openai-compatible` |
| `llm-api-key` | API key for LLM | No | - |
| `llm-model` | Model to use | No | `gpt-4-turbo-preview` |
| `llm-base-url` | Base URL for LLM API | No | OpenAI |
| `review-level` | `quick`, `standard`, or `deep` | No | `standard` |
| `max-comments` | Maximum comments to post | No | `25` |
| `ignore-patterns` | Files to ignore (glob patterns) | No | - |
| `focus-areas` | Review focus areas | No | All |
| `auto-approve` | Auto-approve clean PRs | No | `false` |
| `fail-on-critical` | Fail if critical issues found | No | `true` |
| `post-summary` | Post summary comment | No | `true` |
| `inline-comments` | Post inline comments | No | `true` |
| `config-path` | Path to config file | No | `.aipr/config.yml` |

## Outputs

| Output | Description |
|--------|-------------|
| `review-id` | Unique review identifier |
| `assessment` | `approve`, `request_changes`, or `comment` |
| `critical-count` | Number of critical issues |
| `error-count` | Number of error issues |
| `warning-count` | Number of warning issues |
| `total-comments` | Total comments generated |

## Configuration File

Create `.aipr/config.yml` in your repository:

```yaml
# Review configuration
review:
  level: standard
  max_comments: 25
  focus_areas:
    - security
    - reliability
    - performance
    - testing

# Files to ignore
ignore:
  - "**/*.generated.*"
  - "**/vendor/**"
  - "**/node_modules/**"
  - "**/*.min.js"

# Custom rules
rules:
  # Require test files for source changes
  require_tests:
    enabled: true
    source_patterns:
      - "src/**/*.ts"
    test_patterns:
      - "**/*.test.ts"
      - "**/*.spec.ts"

  # Flag specific patterns
  flag_patterns:
    - pattern: "console\\.log"
      message: "Remove console.log before merging"
      severity: warning
    - pattern: "TODO|FIXME"
      message: "Unresolved TODO/FIXME"
      severity: info

# Team-specific settings
team:
  auto_approve: false
  require_human_review_for:
    - "**/*.sql"
    - "**/security/**"
```

## Security

- Never commit API keys directly. Use GitHub Secrets.
- The action only accesses the current repository's pull requests.
- Review comments are posted as the GitHub Actions bot user.
- For enterprise use, consider self-hosting the AI PR Reviewer server.

## Troubleshooting

### Action times out

- Large diffs may take longer. Consider increasing `review-level` to `quick` for very large PRs.
- Check that the LLM endpoint is responsive.

### No comments posted

- Ensure `pull-requests: write` permission is granted.
- Check that the diff is not empty.
- Verify the LLM API key is valid.

### Inconsistent results

- LLM responses can vary. Use `temperature: 0.1` for more consistent results.
- Consider pinning to a specific model version.

## License

MIT License - see LICENSE file for details.
