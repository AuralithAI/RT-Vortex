// Package swarmauth provides per-agent JWT issuance and Redis-backed token
// validation for the Vortex Agent Swarm.
//
// Each agent gets a 3-hour JWT with claims:
//
//	{sub: "agent-{uuid}", type: "agent", role: "senior_dev", team_id: "..."}
//
// Tokens are stored in Redis with a matching TTL so they auto-expire.
// Go middleware validates tokens by comparing SHA-256 hashes against Redis.
package swarmauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// AgentTokenTTL is the lifetime of an agent JWT.
const AgentTokenTTL = 3 * time.Hour

// ── Agent Claims ────────────────────────────────────────────────────────────

// AgentClaims extends standard JWT claims for swarm agents.
type AgentClaims struct {
	jwt.RegisteredClaims
	Type     string `json:"type"`      // always "agent"
	Role     string `json:"role"`      // orchestrator, senior_dev, etc.
	TeamID   string `json:"team_id"`   // team UUID
	AgentSeq int    `json:"agent_seq"` // sequence within team
}

// ── Registration Request / Response ─────────────────────────────────────────

// RegisterRequest is the JSON body for POST /internal/swarm/auth/register.
type RegisterRequest struct {
	AgentID  string `json:"agent_id"`
	Role     string `json:"role"`
	TeamID   string `json:"team_id"`
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
}

// RegisterResponse is returned on successful registration.
type RegisterResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"` // seconds
}

// ── Redis Token Record ──────────────────────────────────────────────────────

type tokenRecord struct {
	IssuedAt  int64  `json:"issued_at"`
	Role      string `json:"role"`
	TeamID    string `json:"team_id"`
	TokenHash string `json:"token_hash"` // SHA-256 of the JWT string
}

// ── Auth Service ────────────────────────────────────────────────────────────

// Service handles agent registration, JWT issuance, and token validation.
type Service struct {
	jwtSecret     []byte
	serviceSecret string // shared secret between Go and Python (env var)
	redis         *redis.Client
}

// NewService creates a new swarm auth service.
func NewService(jwtSecret []byte, serviceSecret string, redisClient *redis.Client) *Service {
	return &Service{
		jwtSecret:     jwtSecret,
		serviceSecret: serviceSecret,
		redis:         redisClient,
	}
}

// Register creates a new agent JWT and stores its hash in Redis.
// The caller must have already validated X-Service-Secret.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	agentID := req.AgentID
	if agentID == "" {
		agentID = "agent-" + uuid.New().String()
	}

	now := time.Now().UTC()
	claims := AgentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "rtvortex",
			Subject:   agentID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AgentTokenTTL)),
			ID:        uuid.New().String(),
		},
		Type:   "agent",
		Role:   req.Role,
		TeamID: req.TeamID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("signing agent JWT: %w", err)
	}

	// Store token hash in Redis with TTL matching the JWT expiry.
	hash := sha256Hash(tokenStr)
	rec := tokenRecord{
		IssuedAt:  now.Unix(),
		Role:      req.Role,
		TeamID:    req.TeamID,
		TokenHash: hash,
	}
	recJSON, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("marshalling token record: %w", err)
	}

	key := redisKey(agentID)
	if err := s.redis.Set(ctx, key, recJSON, AgentTokenTTL).Err(); err != nil {
		return nil, fmt.Errorf("storing token in Redis: %w", err)
	}

	slog.Info("swarm agent registered",
		"agent_id", agentID,
		"role", req.Role,
		"team_id", req.TeamID,
		"hostname", req.Hostname,
		"ttl", AgentTokenTTL,
	)

	return &RegisterResponse{
		AccessToken: tokenStr,
		ExpiresIn:   int64(AgentTokenTTL.Seconds()),
	}, nil
}

// ValidateToken parses an agent JWT, then verifies its hash against Redis.
// Returns the claims on success.
func (s *Service) ValidateToken(ctx context.Context, tokenStr string) (*AgentClaims, error) {
	// Parse and validate JWT signature + expiry.
	token, err := jwt.ParseWithClaims(tokenStr, &AgentClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid agent token: %w", err)
	}

	claims, ok := token.Claims.(*AgentClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid agent token claims")
	}

	if claims.Type != "agent" {
		return nil, fmt.Errorf("token type is %q, expected \"agent\"", claims.Type)
	}

	// Check Redis for the stored hash.
	key := redisKey(claims.Subject)
	recJSON, err := s.redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("agent token revoked or expired (not in Redis)")
	}
	if err != nil {
		return nil, fmt.Errorf("checking Redis: %w", err)
	}

	var rec tokenRecord
	if err := json.Unmarshal(recJSON, &rec); err != nil {
		return nil, fmt.Errorf("unmarshalling token record: %w", err)
	}

	// Compare hashes.
	if sha256Hash(tokenStr) != rec.TokenHash {
		return nil, fmt.Errorf("token hash mismatch (superseded)")
	}

	return claims, nil
}

// Revoke deletes an agent's token from Redis (soft revocation).
func (s *Service) Revoke(ctx context.Context, agentID string) error {
	key := redisKey(agentID)
	return s.redis.Del(ctx, key).Err()
}

// ValidateServiceSecret checks the X-Service-Secret header value.
func (s *Service) ValidateServiceSecret(secret string) bool {
	return s.serviceSecret != "" && secret == s.serviceSecret
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func redisKey(agentID string) string {
	return "swarm:agent:token:" + agentID
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
