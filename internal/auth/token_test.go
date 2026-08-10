package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestAccessToken(t *testing.T) {
	m := NewTokenManager("secret", time.Hour)
	tok, _, err := m.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	c, err := m.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != 42 {
		t.Fatalf("user id=%d", c.UserID)
	}
}

func TestAccessTokenRejectsTamper(t *testing.T) {
	m := NewTokenManager("secret", time.Hour)
	tok, _, _ := m.Issue(42)
	tok += "x"
	if _, err := m.Verify(tok); err == nil {
		t.Fatal("expected tampered token to fail")
	}
}

func TestAccessTokenRejectsWrongHeader(t *testing.T) {
	m := NewTokenManager("secret", time.Hour)
	claims := accessClaims{Sub: "42", Exp: time.Now().Add(time.Hour).Unix(), Iat: time.Now().Unix(), Typ: "access"}
	payload, _ := json.Marshal(claims)
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(unsigned))
	tok := unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, err := m.Verify(tok); err == nil {
		t.Fatal("expected wrong JWT alg header to be rejected")
	}
}
