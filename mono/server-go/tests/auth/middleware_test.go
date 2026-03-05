package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// ── Middleware: Valid Token ──────────────────────────────────────────────────

func TestMiddleware_ValidToken(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()

	tokenStr, err := m.GenerateAccessToken(userID, "mw@test.com", "MW User", "user", uuid.Nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			t.Error("expected userID in context")
		}
		if id != userID {
			t.Errorf("expected userID %s, got %s", userID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── Middleware: Missing Token ────────────────────────────────────────────────

func TestMiddleware_MissingToken(t *testing.T) {
	m := newTestManager()

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without a token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ── Middleware: Invalid Token ────────────────────────────────────────────────

func TestMiddleware_InvalidToken(t *testing.T) {
	m := newTestManager()

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with invalid token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer totally-bogus-jwt-here")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ── Middleware: Expired Token ────────────────────────────────────────────────

func TestMiddleware_ExpiredToken(t *testing.T) {
	m := auth.NewJWTManager(auth.JWTConfig{
		Secret:          "expired-secret-for-middleware-!!",
		Issuer:          "test",
		AccessDuration:  1 * time.Millisecond,
		RefreshDuration: 1 * time.Millisecond,
	})

	tokenStr, err := m.GenerateAccessToken(uuid.New(), "exp@test.com", "Exp", "user", uuid.Nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with expired token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ── Middleware: Refresh Token rejected ───────────────────────────────────────

func TestMiddleware_RefreshTokenRejected(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()

	// A refresh token should be rejected by the middleware (only access tokens allowed)
	tokenStr, err := m.GenerateRefreshToken(userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with refresh token")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ── Middleware: Malformed Authorization Header ──────────────────────────────

func TestMiddleware_MalformedAuthHeader(t *testing.T) {
	m := newTestManager()

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called with malformed header")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Basic some-basic-auth-value")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ── Middleware: Claims in Context ────────────────────────────────────────────

func TestMiddleware_ClaimsInContext(t *testing.T) {
	m := newTestManager()
	userID := uuid.New()
	orgID := uuid.New()

	tokenStr, err := m.GenerateAccessToken(userID, "ctx@test.com", "CTX", "admin", orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	handler := auth.Middleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.ClaimsFromContext(r.Context())
		if !ok {
			t.Fatal("expected claims in context")
		}
		if claims.Email != "ctx@test.com" {
			t.Errorf("expected email ctx@test.com, got %s", claims.Email)
		}
		if claims.Role != "admin" {
			t.Errorf("expected role admin, got %s", claims.Role)
		}
		if claims.OrgID != orgID {
			t.Errorf("expected orgID %s, got %s", orgID, claims.OrgID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── RequireRole ─────────────────────────────────────────────────────────────

func TestRequireRole_Allowed(t *testing.T) {
	m := newTestManager()
	tokenStr, _ := m.GenerateAccessToken(uuid.New(), "admin@test.com", "Admin", "admin", uuid.Nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(m)(auth.RequireRole("admin")(inner))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireRole_Denied(t *testing.T) {
	m := newTestManager()
	tokenStr, _ := m.GenerateAccessToken(uuid.New(), "user@test.com", "User", "user", uuid.Nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for wrong role")
	})
	handler := auth.Middleware(m)(auth.RequireRole("admin")(inner))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestRequireRole_NoClaims(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without claims")
	})
	handler := auth.RequireRole("admin")(inner)

	req := httptest.NewRequest("GET", "/admin", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireRole_MultipleRoles(t *testing.T) {
	m := newTestManager()
	tokenStr, _ := m.GenerateAccessToken(uuid.New(), "org@test.com", "OrgAdmin", "org_admin", uuid.Nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(m)(auth.RequireRole("admin", "org_admin")(inner))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── RequireOrg ──────────────────────────────────────────────────────────────

func TestRequireOrg_WithOrg(t *testing.T) {
	m := newTestManager()
	tokenStr, _ := m.GenerateAccessToken(uuid.New(), "org@test.com", "Org", "user", uuid.New())

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := auth.Middleware(m)(auth.RequireOrg(inner))

	req := httptest.NewRequest("GET", "/org-resource", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireOrg_WithoutOrg(t *testing.T) {
	m := newTestManager()
	tokenStr, _ := m.GenerateAccessToken(uuid.New(), "no-org@test.com", "NoOrg", "user", uuid.Nil)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without org")
	})
	handler := auth.Middleware(m)(auth.RequireOrg(inner))

	req := httptest.NewRequest("GET", "/org-resource", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// ── Context Helpers (without middleware) ─────────────────────────────────────

func TestUserIDFromContext_Absent(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	_, ok := auth.UserIDFromContext(req.Context())
	if ok {
		t.Error("expected userID to be absent from bare context")
	}
}

func TestClaimsFromContext_Absent(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	_, ok := auth.ClaimsFromContext(req.Context())
	if ok {
		t.Error("expected claims to be absent from bare context")
	}
}
