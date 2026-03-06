package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ── Session Manager ─────────────────────────────────────────────────────────

const (
	sessionPrefix = "session:"
	statePrefix   = "oauth_state:"
	defaultTTL    = 24 * time.Hour
)

// SessionData holds the data stored in a user session.
type SessionData struct {
	UserID    uuid.UUID `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	OrgID     uuid.UUID `json:"org_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Manager handles session lifecycle in Redis.
type Manager struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewManager creates a session manager.
func NewManager(rdb *redis.Client, ttl time.Duration) *Manager {
	if ttl == 0 {
		ttl = defaultTTL
	}
	return &Manager{rdb: rdb, ttl: ttl}
}

// Create stores a new session and returns the session ID.
func (m *Manager) Create(ctx context.Context, data *SessionData) (string, error) {
	sessionID := uuid.New().String()
	data.CreatedAt = time.Now().UTC()

	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}

	key := sessionPrefix + sessionID
	if err := m.rdb.Set(ctx, key, jsonData, m.ttl).Err(); err != nil {
		return "", fmt.Errorf("store session: %w", err)
	}
	return sessionID, nil
}

// Get retrieves session data by session ID.
func (m *Manager) Get(ctx context.Context, sessionID string) (*SessionData, error) {
	key := sessionPrefix + sessionID
	val, err := m.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("session not found")
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	var data SessionData
	if err := json.Unmarshal(val, &data); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &data, nil
}

// Refresh extends the TTL of an existing session.
func (m *Manager) Refresh(ctx context.Context, sessionID string) error {
	key := sessionPrefix + sessionID
	ok, err := m.rdb.Expire(ctx, key, m.ttl).Result()
	if err != nil {
		return fmt.Errorf("refresh session: %w", err)
	}
	if !ok {
		return fmt.Errorf("session not found")
	}
	return nil
}

// Destroy removes a session.
func (m *Manager) Destroy(ctx context.Context, sessionID string) error {
	key := sessionPrefix + sessionID
	return m.rdb.Del(ctx, key).Err()
}

// ── OAuth State ─────────────────────────────────────────────────────────────

const oauthStateTTL = 10 * time.Minute

// StoreOAuthState stores a random state parameter for CSRF protection.
func (m *Manager) StoreOAuthState(ctx context.Context, state, provider string, redirectURL string) error {
	data := map[string]string{
		"provider":     provider,
		"redirect_url": redirectURL,
	}
	jsonData, _ := json.Marshal(data)
	key := statePrefix + state
	return m.rdb.Set(ctx, key, jsonData, oauthStateTTL).Err()
}

// ValidateOAuthState validates and consumes an OAuth state parameter.
// Uses GET + DEL instead of GETDEL for compatibility with Redis < 6.2.
func (m *Manager) ValidateOAuthState(ctx context.Context, state string) (provider string, redirectURL string, err error) {
	key := statePrefix + state
	val, err := m.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return "", "", fmt.Errorf("invalid or expired OAuth state")
	}
	if err != nil {
		return "", "", fmt.Errorf("get oauth state: %w", err)
	}
	// Consume the state so it can't be reused (CSRF protection).
	m.rdb.Del(ctx, key)

	var data map[string]string
	if err := json.Unmarshal(val, &data); err != nil {
		return "", "", fmt.Errorf("unmarshal state: %w", err)
	}
	return data["provider"], data["redirect_url"], nil
}
