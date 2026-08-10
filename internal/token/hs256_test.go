package token

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConnectionTokenHasSubject(t *testing.T) {
	issuer := NewIssuer("secret", time.Hour)
	got, _, err := issuer.ConnectionToken("10086")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(got, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 jwt parts, got %d", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "10086" {
		t.Fatalf("unexpected sub: %v", claims["sub"])
	}
}

func TestSubscriptionToken(t *testing.T) {
	i := NewIssuer("secret", time.Hour)
	tok, _, err := i.SubscriptionToken("42", "room:1:stream", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("parts=%d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != "42" || claims["channel"] != "room:1:stream" {
		t.Fatalf("claims=%v", claims)
	}
	if claims["expire_at"] != float64(0) {
		t.Fatalf("expire_at=%v", claims["expire_at"])
	}
}
