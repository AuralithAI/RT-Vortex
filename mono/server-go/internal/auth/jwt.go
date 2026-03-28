package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrTokenExpired   = errors.New("token expired")
	ErrTokenInvalid   = errors.New("token invalid")
	ErrTokenMalformed = errors.New("token malformed")
	ErrSigningMethod  = errors.New("unexpected signing method")
	ErrMissingClaims  = errors.New("missing required claims")
)

// ── Claims ──────────────────────────────────────────────────────────────────

// TokenType distinguishes access tokens from refresh tokens.
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// Claims extends the standard JWT claims with RTVortex-specific fields.
type Claims struct {
	jwt.RegisteredClaims

	UserID uuid.UUID `json:"uid"`
	Email  string    `json:"email,omitempty"`
	Name   string    `json:"name,omitempty"`
	Role   string    `json:"role,omitempty"`
	OrgID  uuid.UUID `json:"oid,omitempty"`
	Type   TokenType `json:"typ"`
}

// ── JWT Manager ─────────────────────────────────────────────────────────────

// JWTConfig holds token signing parameters.
type JWTConfig struct {
	Secret          string
	Issuer          string
	AccessDuration  time.Duration
	RefreshDuration time.Duration
}

// JWTManager issues and validates tokens.
type JWTManager struct {
	secret          []byte
	issuer          string
	accessDuration  time.Duration
	refreshDuration time.Duration
}

// NewJWTManager creates a manager from config.
func NewJWTManager(cfg JWTConfig) *JWTManager {
	return &JWTManager{
		secret:          []byte(cfg.Secret),
		issuer:          cfg.Issuer,
		accessDuration:  cfg.AccessDuration,
		refreshDuration: cfg.RefreshDuration,
	}
}

// GenerateAccessToken creates a short-lived access token.
func (m *JWTManager) GenerateAccessToken(userID uuid.UUID, email, name, role string, orgID uuid.UUID) (string, error) {
	return m.generateToken(userID, email, name, role, orgID, AccessToken, m.accessDuration)
}

// GenerateRefreshToken creates a long-lived refresh token.
func (m *JWTManager) GenerateRefreshToken(userID uuid.UUID) (string, error) {
	return m.generateToken(userID, "", "", "", uuid.Nil, RefreshToken, m.refreshDuration)
}

func (m *JWTManager) generateToken(
	userID uuid.UUID,
	email, name, role string,
	orgID uuid.UUID,
	tokenType TokenType,
	duration time.Duration,
) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(duration)),
			ID:        uuid.New().String(),
		},
		UserID: userID,
		Email:  email,
		Name:   name,
		Role:   role,
		OrgID:  orgID,
		Type:   tokenType,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// ValidateToken parses and validates a JWT, returning the claims.
func (m *JWTManager) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: %v", ErrSigningMethod, t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenMalformed
	}

	if claims.UserID == uuid.Nil {
		return nil, ErrMissingClaims
	}

	return claims, nil
}

// ── Token Pair ──────────────────────────────────────────────────────────────

// TokenPair holds both tokens for a login response.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// GenerateTokenPair creates both access and refresh tokens for a user.
func (m *JWTManager) GenerateTokenPair(userID uuid.UUID, email, name, role string, orgID uuid.UUID) (*TokenPair, error) {
	access, err := m.GenerateAccessToken(userID, email, name, role, orgID)
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	refresh, err := m.GenerateRefreshToken(userID)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(m.accessDuration.Seconds()),
	}, nil
}

// ── Utilities ───────────────────────────────────────────────────────────────

// GenerateRandomSecret creates a cryptographically secure random string,
// suitable for JWT signing secrets or CSRF tokens.
func GenerateRandomSecret(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
