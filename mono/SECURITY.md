# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x.x   | :white_check_mark: |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them via email to: security@auralithai.com

You should receive a response within 48 hours. If for some reason you do not, please follow up via email to ensure we received your original message.

Please include the following information (as much as you can provide):

- Type of issue (e.g., buffer overflow, SQL injection, cross-site scripting, etc.)
- Full paths of source file(s) related to the manifestation of the issue
- The location of the affected source code (tag/branch/commit or direct URL)
- Any special configuration required to reproduce the issue
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue, including how an attacker might exploit it

## Security Considerations for Deployment

### Secrets Management

- Never commit API keys, tokens, or credentials to the repository
- Use environment variables or secure secret stores
- Enable secret scanning in your CI/CD pipeline

### Network Security

- Deploy the server behind a reverse proxy with TLS
- Use API key authentication for all endpoints
- Consider network segmentation for the engine workers

### Data Protection

- Enable encryption at rest for index storage
- Implement proper RBAC for multi-tenant deployments
- Audit logs should be immutable and retained per compliance requirements

### Model Security

- Validate and sanitize all inputs before sending to LLM
- Use allow-lists for model endpoints
- Monitor for prompt injection attempts

## Security Features

- **Redaction**: Automatic masking of secrets, API keys, and tokens in review output
- **RBAC**: Role-based access control for API endpoints
- **Audit Logging**: Comprehensive audit trail for all operations
- **Encryption**: Support for encryption at rest and in transit
