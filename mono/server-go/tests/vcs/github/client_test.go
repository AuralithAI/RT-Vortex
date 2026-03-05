package github_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	ghclient "github.com/AuralithAI/rtvortex-server/internal/vcs/github"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
)

func TestClient_Type(t *testing.T) {
	c := ghclient.New(ghclient.Config{Token: "test-token"})
	if c.Type() != vcs.PlatformGitHub {
		t.Errorf("expected github, got %s", c.Type())
	}
}

func TestValidateWebhookSignature_Valid(t *testing.T) {
	secret := "test-webhook-secret"
	c := ghclient.New(ghclient.Config{Token: "tok", WebhookSecret: secret})

	payload := []byte(`{"action":"opened","pull_request":{"number":1}}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !c.ValidateWebhookSignature(payload, signature) {
		t.Error("expected valid signature to pass")
	}
}

func TestValidateWebhookSignature_Invalid(t *testing.T) {
	secret := "test-webhook-secret"
	c := ghclient.New(ghclient.Config{Token: "tok", WebhookSecret: secret})

	payload := []byte(`{"action":"opened"}`)
	signature := "sha256=0000000000000000000000000000000000000000000000000000000000000000"

	if c.ValidateWebhookSignature(payload, signature) {
		t.Error("expected invalid signature to fail")
	}
}

func TestValidateWebhookSignature_NoSecret(t *testing.T) {
	c := ghclient.New(ghclient.Config{Token: "tok", WebhookSecret: ""})

	payload := []byte(`{"action":"opened"}`)
	// With no secret configured, validation should pass (warn and accept)
	if !c.ValidateWebhookSignature(payload, "sha256=anything") {
		t.Error("expected no-secret mode to pass")
	}
}

func TestValidateWebhookSignature_TamperedPayload(t *testing.T) {
	secret := "my-secret"
	c := ghclient.New(ghclient.Config{Token: "tok", WebhookSecret: secret})

	original := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(original)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tampered := []byte(`{"action":"closed"}`)
	if c.ValidateWebhookSignature(tampered, signature) {
		t.Error("expected tampered payload to fail validation")
	}
}

func TestValidateWebhookSignature_EmptyPayload(t *testing.T) {
	secret := "my-secret"
	c := ghclient.New(ghclient.Config{Token: "tok", WebhookSecret: secret})

	payload := []byte{}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !c.ValidateWebhookSignature(payload, signature) {
		t.Error("expected empty payload with valid signature to pass")
	}
}

func TestNew_DefaultBaseURL(t *testing.T) {
	c := ghclient.New(ghclient.Config{Token: "tok"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	// Verify it creates successfully with defaults
	if c.Type() != vcs.PlatformGitHub {
		t.Error("expected github type")
	}
}
