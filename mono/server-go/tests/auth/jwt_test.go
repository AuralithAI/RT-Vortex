package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// ── Helpers ─────────────────────────────────────────────────────────────────

func newTestManager() *auth.JWTManager {
	return auth.NewJWTManager(auth.JWTConfig{
		Secret:          "test-secret-for-jwt-unit-tests!!",
		Issuer:          "rtvortex-test",
		AccessDuration:  15 * time.Minute,
		RefreshDuration: 24 * time.Hour,
	})
}

// ── NewJWTManager ───────────────────────────────────────────────────────────

func TestNewJWTManager(t *testing.T) {
	m := auth.NewJWTManager(auth.JWTConfig{
		Secret:          "test-secret-key-for-jwt-manager!",
		Issuer:          "rtvortex-test",
		AccessDuration:  30 * time.Minute,
		RefreshDuration: 48 * time.Hour,
	})
	if m == nil {
		t.Fatal("expected non-nil JWTManager")
	}
}

// ── GenerateAccessToken / ValidateToken ─────────────────────────────────────

func TestGenerateAccessToken_ValidToken(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()
	orgID := uuid.New()

	tokenStr, err := m.GenerateAccessToken(userID, "test@example.com", "Test User", "user", orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token")
	}

	parsed, err := m.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if parsed.UserID != userID {
		t.Errorf("expected userID %s, got %s", userID, parsed.UserID)
	}
	if parsed.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", parsed.Email)
	}
	if parsed.Name != "Test User" {
		t.Errorf("expected name 'Test User', got %s", parsed.Name)
	}
	if parsed.Role != "user" {
		t.Errorf("expected role 'user', got %s", parsed.Role)
	}
	if parsed.Type != auth.AccessToken {
		t.Errorf("expected type 'access', got %s", parsed.Type)
	}
	if parsed.OrgID != orgID {
		t.Errorf("expected orgID %s, got %s", orgID, parsed.OrgID)
	}
}

func TestGenerateAccessToken_NilOrgID(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()

	tokenStr, err := m.GenerateAccessToken(userID, "no-org@test.com", "No Org", "user", uuid.Nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := m.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if parsed.OrgID != uuid.Nil {
		t.Errorf("expected nil orgID, got %s", parsed.OrgID)
	}
}

// ── GenerateRefreshToken ────────────────────────────────────────────────────

func TestGenerateRefreshToken_ValidToken(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()

	tokenStr, err := m.GenerateRefreshToken(userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := m.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if parsed.Type != auth.RefreshToken {
		t.Errorf("expected type 'refresh', got %s", parsed.Type)
	}
	if parsed.UserID != userID {
		t.Errorf("expected userID %s, got %s", userID, parsed.UserID)
	}
	// Refresh tokens should not carry profile fields
	if parsed.Email != "" {
		t.Errorf("expected empty email in refresh token, got %s", parsed.Email)
	}
	if parsed.Name != "" {
		t.Errorf("expected empty name in refresh token, got %s", parsed.Name)
	}
}

// ── Token Expiry ────────────────────────────────────────────────────────────

func TestValidateToken_Expired(t *testing.T) {
	m := auth.NewJWTManager(auth.JWTConfig{
		Secret:          "test-secret-for-expiry-test-now!",
		Issuer:          "test",
		AccessDuration:  1 * time.Millisecond,
		RefreshDuration: 1 * time.Millisecond,
	})

	tokenStr, err := m.GenerateAccessToken(uuid.New(), "expired@test.com", "Exp", "user", uuid.Nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	_, err = m.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if err != auth.ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

// ── Invalid Tokens ──────────────────────────────────────────────────────────

func TestValidateToken_InvalidToken(t *testing.T) {
	m := newTestManager()
	_, err := m.ValidateToken("invalid.token.string")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	m1 := auth.NewJWTManager(auth.JWTConfig{Secret: "secret-one-for-signing-tokens-!", Issuer: "t", AccessDuration: 15 * time.Minute, RefreshDuration: 24 * time.Hour})
	m2 := auth.NewJWTManager(auth.JWTConfig{Secret: "secret-two-for-signing-tokens-!", Issuer: "t", AccessDuration: 15 * time.Minute, RefreshDuration: 24 * time.Hour})

	tokenStr, err := m1.GenerateAccessToken(uuid.New(), "wrong@test.com", "WS", "user", uuid.Nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = m2.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for token signed with different secret")
	}
}

func TestValidateToken_EmptyString(t *testing.T) {
	m := newTestManager()
	_, err := m.ValidateToken("")
	if err == nil {
		t.Fatal("expected error for empty token string")
	}
}

// ── GenerateTokenPair ───────────────────────────────────────────────────────

func TestGenerateTokenPair(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()
	orgID := uuid.New()

	pair, err := m.GenerateTokenPair(userID, "pair@example.com", "Pair User", "admin", orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" {
		t.Fatal("expected non-empty access token")
	}
	if pair.RefreshToken == "" {
		t.Fatal("expected non-empty refresh token")
	}
	if pair.AccessToken == pair.RefreshToken {
		t.Fatal("access and refresh tokens should differ")
	}
	if pair.TokenType != "Bearer" {
		t.Errorf("expected token type 'Bearer', got %s", pair.TokenType)
	}
	if pair.ExpiresIn <= 0 {
		t.Errorf("expected positive ExpiresIn, got %d", pair.ExpiresIn)
	}

	// Validate access token
	aClaims, err := m.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("access token validation failed: %v", err)
	}
	if aClaims.Type != auth.AccessToken {
		t.Errorf("expected access type, got %s", aClaims.Type)
	}
	if aClaims.OrgID != orgID {
		t.Errorf("expected orgID %s, got %s", orgID, aClaims.OrgID)
	}

	// Validate refresh token
	rClaims, err := m.ValidateToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh token validation failed: %v", err)
	}
	if rClaims.Type != auth.RefreshToken {
		t.Errorf("expected refresh type, got %s", rClaims.Type)
	}
}

// ── GenerateRandomSecret ────────────────────────────────────────────────────

func TestGenerateRandomSecret(t *testing.T) {
	secret, err := auth.GenerateRandomSecret(32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secret) == 0 {
		t.Fatal("expected non-empty secret")
	}

	secret2, err := auth.GenerateRandomSecret(32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret == secret2 {
		t.Error("expected different random secrets")
	}
}

func TestGenerateRandomSecret_DifferentLengths(t *testing.T) {
	for _, length := range []int{16, 32, 64} {
		s, err := auth.GenerateRandomSecret(length)
		if err != nil {
			t.Fatalf("unexpected error for length %d: %v", length, err)
		}
		if len(s) == 0 {
			t.Errorf("expected non-empty secret for length %d", length)
		}
	}
}

// ── Issuer claim ────────────────────────────────────────────────────────────

func TestAccessToken_HasIssuerClaim(t *testing.T) {
	m := auth.NewJWTManager(auth.JWTConfig{
		Secret:          "issuer-test-secret-for-jwt-test!",
		Issuer:          "rtvortex-prod",
		AccessDuration:  15 * time.Minute,
		RefreshDuration: 24 * time.Hour,
	})

	tokenStr, err := m.GenerateAccessToken(uuid.New(), "iss@test.com", "Iss", "user", uuid.Nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	claims, err := m.ValidateToken(tokenStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Issuer != "rtvortex-prod" {
		t.Errorf("expected issuer 'rtvortex-prod', got %s", claims.Issuer)
	}
}

// ── Multiple roles ──────────────────────────────────────────────────────────

func TestAccessToken_RolePreserved(t *testing.T) {
	m := newTestManager()

	roles := []string{"user", "admin", "org_admin", "superadmin"}
	for _, role := range roles {
		tokenStr, err := m.GenerateAccessToken(uuid.New(), "role@test.com", "R", role, uuid.Nil)
		if err != nil {
			t.Fatalf("unexpected error for role %s: %v", role, err)
		}
		claims, err := m.ValidateToken(tokenStr)
		if err != nil {
			t.Fatalf("validation failed for role %s: %v", role, err)
		}
		if claims.Role != role {
			t.Errorf("expected role %s, got %s", role, claims.Role)
		}
	}
}
