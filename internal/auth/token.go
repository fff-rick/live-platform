package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

type Claims struct {
	UserID int64
	Exp    time.Time
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type accessClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
	Typ string `json:"typ"`
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

func (m *TokenManager) Issue(userID int64) (string, time.Time, error) {
	if userID <= 0 {
		return "", time.Time{}, errors.New("user id is required")
	}
	now := time.Now().UTC()
	exp := now.Add(m.ttl)
	claims := accessClaims{Sub: strconv.FormatInt(userID, 10), Exp: exp.Unix(), Iat: now.Unix(), Typ: "access"}
	tok, err := signHS256(m.secret, claims)
	return tok, exp, err
}

func (m *TokenManager) Verify(raw string) (Claims, error) {
	if len(raw) == 0 || len(raw) > 4096 {
		return Claims{}, errors.New("invalid token")
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, errors.New("invalid token header")
	}
	var header jwtHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Alg != "HS256" || header.Typ != "JWT" {
		return Claims{}, errors.New("invalid token header")
	}
	unsigned := parts[0] + "." + parts[1]
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, errors.New("invalid token signature")
	}
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(unsigned))
	if !hmac.Equal(gotSig, mac.Sum(nil)) {
		return Claims{}, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("invalid token payload")
	}
	var c accessClaims
	if err := json.Unmarshal(payload, &c); err != nil || c.Typ != "access" {
		return Claims{}, errors.New("invalid token claims")
	}
	uid, err := strconv.ParseInt(c.Sub, 10, 64)
	if err != nil || uid <= 0 {
		return Claims{}, errors.New("invalid token subject")
	}
	exp := time.Unix(c.Exp, 0).UTC()
	if !exp.After(time.Now().UTC()) {
		return Claims{}, errors.New("token expired")
	}
	return Claims{UserID: uid, Exp: exp}, nil
}

func signHS256(secret []byte, claims any) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal jwt claims: %w", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
