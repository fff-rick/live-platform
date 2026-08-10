package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

type connectionClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
}

type subscriptionClaims struct {
	Sub      string `json:"sub"`
	Channel  string `json:"channel"`
	Exp      int64  `json:"exp"`
	ExpireAt int64  `json:"expire_at"`
	Iat      int64  `json:"iat"`
}

func (i *Issuer) ConnectionToken(userID string) (string, time.Time, error) {
	if strings.TrimSpace(userID) == "" {
		return "", time.Time{}, errors.New("user id is required")
	}
	now := time.Now().UTC()
	exp := now.Add(i.ttl)
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := connectionClaims{Sub: userID, Exp: exp.Unix(), Iat: now.Unix()}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	unsigned := encode(h) + "." + encode(c)
	mac := hmac.New(sha256.New, i.secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + encode(mac.Sum(nil)), exp, nil
}

func (i *Issuer) SubscriptionToken(userID, channel string, ttl time.Duration) (string, time.Time, error) {
	userID = strings.TrimSpace(userID)
	channel = strings.TrimSpace(channel)
	if userID == "" || channel == "" {
		return "", time.Time{}, errors.New("user id and channel are required")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	now := time.Now().UTC()
	exp := now.Add(ttl)
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := subscriptionClaims{Sub: userID, Channel: channel, Exp: exp.Unix(), ExpireAt: 0, Iat: now.Unix()}
	h, _ := json.Marshal(header)
	c, _ := json.Marshal(claims)
	unsigned := encode(h) + "." + encode(c)
	mac := hmac.New(sha256.New, i.secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + encode(mac.Sum(nil)), exp, nil
}

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
