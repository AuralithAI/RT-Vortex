package model

import (
	"time"

	"github.com/google/uuid"
)

// ── Chat Session ────────────────────────────────────────────────────────────

// ChatSession represents a conversation between a user and the AI assistant
// scoped to a specific repository. Messages are E2E encrypted at rest.
type ChatSession struct {
	ID                  uuid.UUID  `json:"id"`
	RepoID              uuid.UUID  `json:"repo_id"`
	UserID              uuid.UUID  `json:"user_id"`
	Title               string     `json:"title"`
	MessageCount        int        `json:"message_count"`
	LastMessageAt       *time.Time `json:"last_message_at,omitempty"`
	Model               string     `json:"model,omitempty"`
	Provider            string     `json:"provider,omitempty"`
	EncryptedSessionKey string     `json:"-"` // never sent to clients
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// ── Chat Message ────────────────────────────────────────────────────────────

// ChatMessageRole identifies the sender of a chat message.
type ChatMessageRole string

const (
	ChatRoleUser      ChatMessageRole = "user"
	ChatRoleAssistant ChatMessageRole = "assistant"
	ChatRoleSystem    ChatMessageRole = "system"
)

// ChatCitation is a reference to a specific code chunk used to answer a question.
type ChatCitation struct {
	FilePath       string   `json:"file_path"`
	StartLine      int      `json:"start_line"`
	EndLine        int      `json:"end_line"`
	Content        string   `json:"content"`
	Language       string   `json:"language"`
	RelevanceScore float32  `json:"relevance_score"`
	Symbols        []string `json:"symbols,omitempty"`
}

// ChatAttachment is a file or code snippet attached to a user message.
type ChatAttachment struct {
	Type     string `json:"type"`     // "file", "code_snippet", "image", "pdf", "audio", "url"
	Filename string `json:"filename"` // original filename or label
	Content  string `json:"content"`  // file content, code text, or URL string
	Language string `json:"language,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int    `json:"size,omitempty"`    // bytes
	DataURI  string `json:"data_uri,omitempty"` // base64 data URI (image thumbnails)
}

// ChatMessage is a single message in a chat session.
type ChatMessage struct {
	ID               uuid.UUID        `json:"id"`
	SessionID        uuid.UUID        `json:"session_id"`
	Role             ChatMessageRole  `json:"role"`
	Content          string           `json:"content"`
	Encrypted        bool             `json:"-"` // internal flag
	Citations        []ChatCitation   `json:"citations,omitempty"`
	Attachments      []ChatAttachment `json:"attachments,omitempty"`
	PromptTokens     int              `json:"prompt_tokens,omitempty"`
	CompletionTokens int              `json:"completion_tokens,omitempty"`
	SearchTimeMs     int              `json:"search_time_ms,omitempty"`
	ChunksRetrieved  int              `json:"chunks_retrieved,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
}

// ── Chat Stream Events ──────────────────────────────────────────────────────

// ChatStreamEvent is sent over SSE during a streaming chat response.
type ChatStreamEvent struct {
	Type    string `json:"type"` // "delta", "citation", "thinking", "done", "error"
	Content string `json:"content,omitempty"`

	// Set on "citation" events
	Citation *ChatCitation `json:"citation,omitempty"`

	// Set on "thinking" events (shows what the engine is doing)
	Phase   string `json:"phase,omitempty"`   // "searching", "retrieving", "synthesizing"
	Message string `json:"message,omitempty"` // human-readable status

	// Set on "done" events
	MessageID        *uuid.UUID `json:"message_id,omitempty"`
	PromptTokens     int        `json:"prompt_tokens,omitempty"`
	CompletionTokens int        `json:"completion_tokens,omitempty"`
	SearchTimeMs     int        `json:"search_time_ms,omitempty"`
	ChunksRetrieved  int        `json:"chunks_retrieved,omitempty"`

	// Set on "error" events
	Error string `json:"error,omitempty"`
}
