package sandbox

import (
	"regexp"
	"strings"
)

// redactPatterns are compiled regular expressions matching common secret
// and PII formats found in build logs.  Values are replaced with
// "[REDACTED]" before the logs are persisted or returned in responses.
var redactPatterns = []*regexp.Regexp{
	// API keys / tokens (generic long hex/base64 strings preceded by a key-like label).
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|token|bearer|authorization|auth[_-]?token|access[_-]?key|secret[_-]?key)\s*[:=]\s*['"]?([A-Za-z0-9_\-/.+]{16,})['"]?`),

	// AWS keys.
	regexp.MustCompile(`(?:AKIA|ABIA|ACCA|ASIA)[A-Z0-9]{16}`),
	regexp.MustCompile(`(?i)(aws[_-]?secret[_-]?access[_-]?key)\s*[:=]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`),

	// GitHub / GitLab tokens.
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{20,}`),

	// Generic passwords in env-var, config, or JSON assignments.
	regexp.MustCompile(`(?i)["']?(password|passwd|pwd|secret|credential)["']?\s*[:=]\s*["']?(\S{8,})["']?`),

	// Private keys (PEM).
	regexp.MustCompile(`-----BEGIN\s+(RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),

	// JWT tokens (three base64 segments separated by dots).
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),

	// Connection strings with embedded passwords.
	regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis|amqp)://[^:]+:[^@]+@`),

	// Email addresses (PII).
	regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),

	// IP addresses with ports (potential internal infra leaks).
	regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}:\d{2,5}\b`),
}

// RedactLogs scrubs known secret patterns, injected env var values, and PII
// from build output before it is stored or returned.
func RedactLogs(logs string, secretValues map[string]string) string {
	result := logs

	// First pass: replace exact secret values that were injected into the
	// container's environment.  This catches secrets that appear verbatim
	// in build output (e.g. an accidental echo of $DB_PASSWORD).
	for name, val := range secretValues {
		if val == "" {
			continue
		}
		result = strings.ReplaceAll(result, val, "[REDACTED:"+name+"]")
	}

	// Second pass: apply regex patterns for common secret/PII formats.
	for _, re := range redactPatterns {
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			return "[REDACTED]"
		})
	}

	return result
}

// RedactLogSummary is a convenience wrapper that redacts and truncates logs
// for database persistence.  maxBytes controls the truncation limit.
func RedactLogSummary(logs string, secretValues map[string]string, maxBytes int) string {
	redacted := RedactLogs(logs, secretValues)
	if maxBytes > 0 && len(redacted) > maxBytes {
		redacted = redacted[:maxBytes] + "\n... [truncated]"
	}
	return redacted
}

// ContainsSecret returns true if any known secret value appears verbatim
// in the text.  Used as a fast pre-check before persisting data.
func ContainsSecret(text string, secretValues map[string]string) bool {
	for _, val := range secretValues {
		if val != "" && strings.Contains(text, val) {
			return true
		}
	}
	return false
}

// MaskField replaces all but the first and last 2 characters of a value
// with asterisks, for audit log display.
func MaskField(val string) string {
	if len(val) <= 6 {
		return "****"
	}
	return val[:2] + strings.Repeat("*", len(val)-4) + val[len(val)-2:]
}
