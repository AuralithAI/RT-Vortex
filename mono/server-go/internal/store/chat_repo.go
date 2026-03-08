package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── ChatRepository ──────────────────────────────────────────────────────────

// ChatRepository handles chat session and message persistence.
type ChatRepository struct {
	pool *pgxpool.Pool
}

// NewChatRepository creates a chat repository.
func NewChatRepository(pool *pgxpool.Pool) *ChatRepository {
	return &ChatRepository{pool: pool}
}

// ── Sessions ────────────────────────────────────────────────────────────────

const chatSessionColumns = `id, repo_id, user_id, title, message_count, last_message_at,
	model, provider, encrypted_session_key, created_at, updated_at`

func scanSession(row pgx.Row) (*model.ChatSession, error) {
	s := &model.ChatSession{}
	err := row.Scan(&s.ID, &s.RepoID, &s.UserID, &s.Title,
		&s.MessageCount, &s.LastMessageAt,
		&s.Model, &s.Provider, &s.EncryptedSessionKey,
		&s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// CreateSession creates a new chat session.
func (r *ChatRepository) CreateSession(ctx context.Context, s *model.ChatSession) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	_, err := r.pool.Exec(ctx,
		`INSERT INTO chat_sessions (id, repo_id, user_id, title, message_count,
			last_message_at, model, provider, encrypted_session_key,
			created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		s.ID, s.RepoID, s.UserID, s.Title, s.MessageCount,
		s.LastMessageAt, s.Model, s.Provider, s.EncryptedSessionKey,
		s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create chat session: %w", err)
	}
	return nil
}

// GetSession returns a chat session by ID.
func (r *ChatRepository) GetSession(ctx context.Context, id uuid.UUID) (*model.ChatSession, error) {
	s, err := scanSession(r.pool.QueryRow(ctx,
		`SELECT `+chatSessionColumns+` FROM chat_sessions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get chat session: %w", err)
	}
	return s, nil
}

// ListSessions returns chat sessions for a user in a repository, newest first.
func (r *ChatRepository) ListSessions(ctx context.Context, repoID, userID uuid.UUID, limit, offset int) ([]*model.ChatSession, int, error) {
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_sessions WHERE repo_id = $1 AND user_id = $2`,
		repoID, userID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count chat sessions: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+chatSessionColumns+` FROM chat_sessions
		 WHERE repo_id = $1 AND user_id = $2
		 ORDER BY updated_at DESC LIMIT $3 OFFSET $4`,
		repoID, userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list chat sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*model.ChatSession
	for rows.Next() {
		s := &model.ChatSession{}
		if err := rows.Scan(&s.ID, &s.RepoID, &s.UserID, &s.Title,
			&s.MessageCount, &s.LastMessageAt,
			&s.Model, &s.Provider, &s.EncryptedSessionKey,
			&s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan chat session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, total, nil
}

// UpdateSessionTitle updates the title of a chat session.
func (r *ChatRepository) UpdateSessionTitle(ctx context.Context, id uuid.UUID, title string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE chat_sessions SET title = $2, updated_at = NOW() WHERE id = $1`,
		id, title)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSession removes a chat session and all its messages.
func (r *ChatRepository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM chat_sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete chat session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// touchSession updates message count and last message time.
func (r *ChatRepository) touchSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE chat_sessions SET
			message_count = (SELECT COUNT(*) FROM chat_messages WHERE session_id = $1),
			last_message_at = NOW(),
			updated_at = NOW()
		 WHERE id = $1`, sessionID)
	return err
}

// ── Messages ────────────────────────────────────────────────────────────────

const chatMessageColumns = `id, session_id, role, content, encrypted,
	citations, attachments, prompt_tokens, completion_tokens,
	search_time_ms, chunks_retrieved, created_at`

// CreateMessage persists a chat message.
func (r *ChatRepository) CreateMessage(ctx context.Context, m *model.ChatMessage) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.CreatedAt = time.Now().UTC()

	citationsJSON, _ := json.Marshal(m.Citations)
	attachmentsJSON, _ := json.Marshal(m.Attachments)

	_, err := r.pool.Exec(ctx,
		`INSERT INTO chat_messages (id, session_id, role, content, encrypted,
			citations, attachments, prompt_tokens, completion_tokens,
			search_time_ms, chunks_retrieved, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		m.ID, m.SessionID, m.Role, m.Content, m.Encrypted,
		citationsJSON, attachmentsJSON,
		m.PromptTokens, m.CompletionTokens,
		m.SearchTimeMs, m.ChunksRetrieved, m.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create chat message: %w", err)
	}

	// Update session stats.
	_ = r.touchSession(ctx, m.SessionID)

	return nil
}

// ListMessages returns messages for a session in chronological order.
func (r *ChatRepository) ListMessages(ctx context.Context, sessionID uuid.UUID, limit, offset int) ([]*model.ChatMessage, int, error) {
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_messages WHERE session_id = $1`, sessionID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count chat messages: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+chatMessageColumns+` FROM chat_messages
		 WHERE session_id = $1
		 ORDER BY created_at ASC LIMIT $2 OFFSET $3`,
		sessionID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	var msgs []*model.ChatMessage
	for rows.Next() {
		m := &model.ChatMessage{}
		var citationsJSON, attachmentsJSON []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Encrypted,
			&citationsJSON, &attachmentsJSON,
			&m.PromptTokens, &m.CompletionTokens,
			&m.SearchTimeMs, &m.ChunksRetrieved, &m.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan chat message: %w", err)
		}
		if len(citationsJSON) > 0 {
			_ = json.Unmarshal(citationsJSON, &m.Citations)
		}
		if len(attachmentsJSON) > 0 {
			_ = json.Unmarshal(attachmentsJSON, &m.Attachments)
		}
		msgs = append(msgs, m)
	}
	return msgs, total, nil
}

// GetRecentMessages returns the N most recent messages for building LLM context.
func (r *ChatRepository) GetRecentMessages(ctx context.Context, sessionID uuid.UUID, limit int) ([]*model.ChatMessage, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+chatMessageColumns+` FROM chat_messages
		 WHERE session_id = $1
		 ORDER BY created_at DESC LIMIT $2`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get recent messages: %w", err)
	}
	defer rows.Close()

	var msgs []*model.ChatMessage
	for rows.Next() {
		m := &model.ChatMessage{}
		var citationsJSON, attachmentsJSON []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Encrypted,
			&citationsJSON, &attachmentsJSON,
			&m.PromptTokens, &m.CompletionTokens,
			&m.SearchTimeMs, &m.ChunksRetrieved, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		if len(citationsJSON) > 0 {
			_ = json.Unmarshal(citationsJSON, &m.Citations)
		}
		if len(attachmentsJSON) > 0 {
			_ = json.Unmarshal(attachmentsJSON, &m.Attachments)
		}
		msgs = append(msgs, m)
	}

	// Reverse to chronological order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}
