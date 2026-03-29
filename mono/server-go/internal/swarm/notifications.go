package swarm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ── Notification Types ──────────────────────────────────────────────────────

const (
	NotifyTaskCompleted = "task_completed"
	NotifyTaskFailed    = "task_failed"
	NotifyPRCreated     = "pr_created"
	NotifyHITLQuestion  = "hitl_question"
)

// NotificationConfig holds notification channel settings.
type NotificationConfig struct {
	SlackWebhookURL string
	TeamsWebhookURL string
	SMTPHost        string
	SMTPPort        int
	SMTPFrom        string
	SMTPUser        string
	SMTPPass        string
	DashboardURL    string
}

// NotificationService sends notifications via configured channels.
type NotificationService struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	config NotificationConfig
}

// NewNotificationService creates a notification service.
func NewNotificationService(db *pgxpool.Pool, rdb *redis.Client, config NotificationConfig) *NotificationService {
	return &NotificationService{db: db, rdb: rdb, config: config}
}

// NotificationPayload contains the data for a notification.
type NotificationPayload struct {
	Type        string            `json:"type"`
	TaskID      string            `json:"task_id"`
	TaskTitle   string            `json:"task_title"`
	RepoID      string            `json:"repo_id"`
	Status      string            `json:"status"`
	PRUrl       string            `json:"pr_url,omitempty"`
	PRNumber    int               `json:"pr_number,omitempty"`
	AgentCount  int               `json:"agent_count,omitempty"`
	Duration    string            `json:"duration,omitempty"`
	Error       string            `json:"error,omitempty"`
	Question    string            `json:"question,omitempty"`
	ApproveLink string            `json:"approve_link,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
}

// Notify dispatches a notification to all configured channels.
func (s *NotificationService) Notify(ctx context.Context, payload NotificationPayload) {
	payload.Timestamp = time.Now()

	if payload.ApproveLink == "" && payload.PRUrl != "" && s.config.DashboardURL != "" {
		payload.ApproveLink = fmt.Sprintf("%s/swarm/tasks/%s?action=approve",
			strings.TrimRight(s.config.DashboardURL, "/"), payload.TaskID)
	}

	// Persist to Redis queue for async delivery.
	data, _ := json.Marshal(payload)
	s.rdb.LPush(ctx, "swarm:notifications:pending", data)

	// Fire-and-forget delivery.
	go s.deliver(context.Background(), payload)
}

func (s *NotificationService) deliver(ctx context.Context, p NotificationPayload) {
	if s.config.SlackWebhookURL != "" {
		s.sendSlack(ctx, p)
	}
	if s.config.TeamsWebhookURL != "" {
		s.sendTeams(ctx, p)
	}
	if s.config.SMTPHost != "" {
		s.sendEmail(ctx, p)
	}
}

// ── Slack ────────────────────────────────────────────────────────────────────

var slackTemplate = template.Must(template.New("slack").Parse(`{
	"blocks": [
		{
			"type": "header",
			"text": {"type": "plain_text", "text": "{{.Title}}"}
		},
		{
			"type": "section",
			"text": {"type": "mrkdwn", "text": "{{.Body}}"}
		}
		{{- if .ActionURL }},
		{
			"type": "actions",
			"elements": [
				{
					"type": "button",
					"text": {"type": "plain_text", "text": "{{.ActionText}}"},
					"url": "{{.ActionURL}}",
					"style": "primary"
				}
			]
		}
		{{- end }}
	]
}`))

type slackData struct {
	Title      string
	Body       string
	ActionURL  string
	ActionText string
}

func (s *NotificationService) sendSlack(ctx context.Context, p NotificationPayload) {
	d := buildSlackData(p)
	var buf bytes.Buffer
	if err := slackTemplate.Execute(&buf, d); err != nil {
		slog.Error("slack template error", "error", err)
		return
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.config.SlackWebhookURL, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("slack notification failed", "error", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

func buildSlackData(p NotificationPayload) slackData {
	d := slackData{}
	switch p.Type {
	case NotifyTaskCompleted:
		d.Title = "✅ Swarm Task Completed"
		d.Body = fmt.Sprintf("*%s* completed in %s for repo `%s`.", p.TaskTitle, p.Duration, p.RepoID)
		if p.PRUrl != "" {
			d.Body += fmt.Sprintf("\n<<%s|View PR #%d>>", p.PRUrl, p.PRNumber)
		}
		if p.ApproveLink != "" {
			d.ActionURL = p.ApproveLink
			d.ActionText = "Approve & Merge"
		}
	case NotifyTaskFailed:
		d.Title = "❌ Swarm Task Failed"
		d.Body = fmt.Sprintf("*%s* failed for repo `%s`.\nReason: %s", p.TaskTitle, p.RepoID, p.Error)
	case NotifyPRCreated:
		d.Title = "🔀 PR Created by Swarm"
		d.Body = fmt.Sprintf("PR #%d created for *%s*: <%s|View PR>", p.PRNumber, p.TaskTitle, p.PRUrl)
		if p.ApproveLink != "" {
			d.ActionURL = p.ApproveLink
			d.ActionText = "Approve & Merge"
		}
	case NotifyHITLQuestion:
		d.Title = "🙋 Agent Needs Human Input"
		d.Body = fmt.Sprintf("An agent working on *%s* has a question:\n> %s", p.TaskTitle, p.Question)
		if p.ApproveLink != "" {
			d.ActionURL = p.ApproveLink
			d.ActionText = "Answer"
		}
	default:
		d.Title = "🤖 Swarm Notification"
		d.Body = fmt.Sprintf("Task: %s | Status: %s", p.TaskTitle, p.Status)
	}
	return d
}

// ── Teams ────────────────────────────────────────────────────────────────────

func (s *NotificationService) sendTeams(ctx context.Context, p NotificationPayload) {
	d := buildSlackData(p)
	card := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": "0076D7",
		"summary":    d.Title,
		"sections": []map[string]interface{}{
			{
				"activityTitle": d.Title,
				"text":          d.Body,
				"markdown":      true,
			},
		},
	}
	if d.ActionURL != "" {
		card["potentialAction"] = []map[string]interface{}{
			{
				"@type": "OpenUri",
				"name":  d.ActionText,
				"targets": []map[string]string{
					{"os": "default", "uri": d.ActionURL},
				},
			},
		}
	}
	body, _ := json.Marshal(card)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.config.TeamsWebhookURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("teams notification failed", "error", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}

// ── Email (SMTP) ────────────────────────────────────────────────────────────

func (s *NotificationService) sendEmail(ctx context.Context, p NotificationPayload) {
	_ = ctx
	d := buildSlackData(p)

	to := s.config.SMTPFrom // fallback: notify self. In production, resolved from user prefs.
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n"+
		"<h2>%s</h2><p>%s</p>",
		s.config.SMTPFrom, to, d.Title, d.Title, strings.ReplaceAll(d.Body, "\n", "<br>"))

	if d.ActionURL != "" {
		msg += fmt.Sprintf(`<br><a href="%s" style="display:inline-block;padding:10px 20px;background:#0076D7;color:#fff;text-decoration:none;border-radius:4px;">%s</a>`,
			d.ActionURL, d.ActionText)
	}

	addr := fmt.Sprintf("%s:%d", s.config.SMTPHost, s.config.SMTPPort)
	var auth smtp.Auth
	if s.config.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.config.SMTPUser, s.config.SMTPPass, s.config.SMTPHost)
	}

	if err := smtp.SendMail(addr, auth, s.config.SMTPFrom, []string{to}, []byte(msg)); err != nil {
		slog.Error("email notification failed", "error", err)
	}
}

// ── PR Comment Templates ────────────────────────────────────────────────────

var defaultPRTemplate = template.Must(template.New("pr_comment").Parse(
	`## 🤖 RTVortex Swarm Review

**Task:** {{.TaskTitle}}
**Agents:** {{.AgentCount}} | **Duration:** {{.Duration}}

### Summary
{{.Summary}}

### Changes
{{range .Files}}- ` + "`{{.Path}}`" + ` — {{.ChangeType}} ({{.Additions}}+ / {{.Deletions}}-)
{{end}}

{{if .Issues}}### Issues Found
{{range .Issues}}- {{.Severity}}: {{.Description}} ({{.File}}:{{.Line}})
{{end}}{{end}}

---
*Generated by [RTVortex Swarm]({{.DashboardURL}}) • [View Task]({{.TaskURL}})*
`))

// PRCommentData holds data for rendering a PR comment template.
type PRCommentData struct {
	TaskTitle    string
	AgentCount   int
	Duration     string
	Summary      string
	DashboardURL string
	TaskURL      string
	Files        []PRFileChange
	Issues       []PRIssue
}

// PRFileChange describes a file change in a PR.
type PRFileChange struct {
	Path       string
	ChangeType string
	Additions  int
	Deletions  int
}

// PRIssue describes an issue found during review.
type PRIssue struct {
	Severity    string
	Description string
	File        string
	Line        int
}

// RenderPRComment renders a PR comment from the default template.
func RenderPRComment(data PRCommentData) (string, error) {
	var buf bytes.Buffer
	if err := defaultPRTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render PR comment: %w", err)
	}
	return buf.String(), nil
}
