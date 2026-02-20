# Output JSON Format

You MUST respond with valid JSON matching this schema. No markdown, no explanation outside the JSON.

```json
{
  "summary": "Brief 1-2 sentence summary of the review",
  "overall_assessment": "approve|request_changes|comment",
  "confidence": 0.85,
  "comments": [
    {
      "id": "comment-1",
      "file_path": "src/module/file.ts",
      "line": 42,
      "end_line": 45,
      "severity": "warning",
      "category": "reliability",
      "message": "Clear description of the issue",
      "suggestion": "Concrete code suggestion or improvement",
      "references": [
        {
          "file": "src/other/file.ts",
          "line": 100,
          "reason": "Similar pattern used here"
        }
      ],
      "confidence": 0.9
    }
  ],
  "metrics": {
    "security_score": 0.95,
    "reliability_score": 0.8,
    "performance_score": 0.9,
    "testing_score": 0.7,
    "documentation_score": 0.85
  },
  "requires_human_review": false,
  "suggested_reviewers": ["@security-team"],
  "tags": ["breaking-change", "needs-tests"]
}
```

## Field Descriptions

### Root Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `summary` | string | Yes | 1-2 sentence overview of the PR and key findings |
| `overall_assessment` | enum | Yes | `approve`, `request_changes`, or `comment` |
| `confidence` | number | Yes | 0.0-1.0, overall confidence in the review |
| `comments` | array | Yes | List of review comments (can be empty) |
| `metrics` | object | No | Category scores for analytics |
| `requires_human_review` | boolean | No | Flag if human should double-check |
| `suggested_reviewers` | array | No | Suggested additional reviewers |
| `tags` | array | No | Labels for the PR |

### Comment Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique identifier for the comment |
| `file_path` | string | Yes | Path relative to repo root |
| `line` | number | Yes | Line number in the new file |
| `end_line` | number | No | For multi-line comments |
| `severity` | enum | Yes | `critical`, `error`, `warning`, `info`, `suggestion` |
| `category` | enum | Yes | `security`, `reliability`, `performance`, `testing`, `documentation`, `architecture`, `other` |
| `message` | string | Yes | Description of the issue |
| `suggestion` | string | No | Proposed fix or improvement |
| `references` | array | No | Related code references |
| `confidence` | number | No | 0.0-1.0, confidence in this specific finding |

### Reference Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `file` | string | Yes | Path to referenced file |
| `line` | number | No | Line number in referenced file |
| `reason` | string | Yes | Why this reference is relevant |

## Guidelines

### When to use `overall_assessment`:

- **approve**: No critical/error issues, PR is ready to merge
- **request_changes**: Has critical or multiple error issues that must be fixed
- **comment**: Has suggestions or minor issues but doesn't block merge

### Confidence Scores:

- **0.9-1.0**: Very confident, clear-cut issue or approval
- **0.7-0.9**: Confident, but context might change assessment
- **0.5-0.7**: Moderate confidence, may need human review
- **0.0-0.5**: Low confidence, definitely needs human review

### When to set `requires_human_review`:

- Complex architectural decisions
- Security-sensitive changes
- Low confidence in assessment
- Unfamiliar code patterns
- Breaking changes to public APIs

### Examples of Good Comments:

```json
{
  "id": "sql-injection-1",
  "file_path": "api/users.py",
  "line": 78,
  "severity": "critical",
  "category": "security",
  "message": "SQL injection vulnerability: f-string used in query with user input",
  "suggestion": "Use parameterized query:\n```python\ncursor.execute('SELECT * FROM users WHERE id = %s', (user_id,))\n```",
  "confidence": 0.98
}
```

```json
{
  "id": "error-handling-1",
  "file_path": "services/payment.ts",
  "line": 145,
  "end_line": 150,
  "severity": "error",
  "category": "reliability",
  "message": "Network errors are silently swallowed, causing failed payments to appear successful",
  "suggestion": "Re-throw or handle the error explicitly:\n```typescript\ncatch (error) {\n  logger.error('Payment failed', { error, paymentId });\n  throw new PaymentError('Payment processing failed', error);\n}\n```",
  "references": [
    {
      "file": "services/payment.ts",
      "line": 80,
      "reason": "Similar pattern handled correctly here"
    }
  ],
  "confidence": 0.95
}
```

## Response Requirements

1. Output ONLY the JSON object, no surrounding text
2. Ensure valid JSON (proper escaping, no trailing commas)
3. Include at least `summary`, `overall_assessment`, `confidence`, and `comments`
4. Empty comments array is valid if no issues found
5. Every comment must have `id`, `file_path`, `line`, `severity`, `category`, `message`
