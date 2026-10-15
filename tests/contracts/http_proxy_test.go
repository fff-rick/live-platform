package contracts_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/commerceapi"
	"github.com/example/live-platform/internal/gateway"
	"github.com/example/live-platform/internal/httpapi"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestGatewayAndServicesKeepAuthenticationContractOverHTTP(t *testing.T) {
	tokens := auth.NewTokenManager("contract-test-secret", time.Hour)
	identity := httptest.NewServer(httpapi.NewIdentityRoom(httpapi.Deps{Log: testLogger(), AppTokens: tokens}))
	interaction := httptest.NewServer(httpapi.NewInteraction(httpapi.Deps{Log: testLogger(), AppTokens: tokens}))
	commerce := httptest.NewServer(commerceapi.New(commerceapi.Deps{Log: testLogger(), Tokens: tokens}).Handler())
	t.Cleanup(identity.Close)
	t.Cleanup(interaction.Close)
	t.Cleanup(commerce.Close)

	gatewayServer, err := gateway.New(gateway.Deps{
		Log: testLogger(), IdentityRoomURL: identity.URL, InteractionURL: interaction.URL, CommerceURL: commerce.URL, QueryURL: identity.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(gatewayServer.Handler())
	t.Cleanup(edge.Close)

	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/me"},
		{http.MethodPost, "/api/v1/rooms/9/join"},
		{http.MethodGet, "/api/v1/wallet"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, edge.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			var body map[string]any
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != http.StatusUnauthorized || body["code"] != "UNAUTHORIZED" {
				t.Fatalf("status=%d body=%v", res.StatusCode, body)
			}
		})
	}
}

type observedRequest struct {
	path, authorization, idempotencyKey, requestID, userID, body string
}

func TestGatewayPreservesUpstreamHTTPContract(t *testing.T) {
	var mu sync.Mutex
	observed := map[string]observedRequest{}
	upstream := func(name string, status int, code string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			observed[name] = observedRequest{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key"), r.Header.Get("X-Request-ID"), r.Header.Get("X-User-ID"), string(raw)}
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": name})
		}))
	}
	identity := upstream("identity", http.StatusConflict, "USERNAME_TAKEN")
	interaction := upstream("interaction", http.StatusUnprocessableEntity, "SENSITIVE_CONTENT")
	commerce := upstream("commerce", http.StatusTooManyRequests, "GIFT_RATE_LIMITED")
	query := upstream("query", http.StatusServiceUnavailable, "STATS_UNAVAILABLE")
	t.Cleanup(identity.Close)
	t.Cleanup(interaction.Close)
	t.Cleanup(commerce.Close)
	t.Cleanup(query.Close)

	gatewayServer, err := gateway.New(gateway.Deps{
		Log: testLogger(), IdentityRoomURL: identity.URL, InteractionURL: interaction.URL, CommerceURL: commerce.URL, QueryURL: query.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(gatewayServer.Handler())
	t.Cleanup(edge.Close)

	cases := []struct {
		name, path, body, code string
		status                 int
	}{
		{"identity", "/api/v1/auth/register", `{"username":"same"}`, "USERNAME_TAKEN", http.StatusConflict},
		{"interaction", "/api/v1/rooms/9/danmaku", `{"content":"blocked"}`, "SENSITIVE_CONTENT", http.StatusUnprocessableEntity},
		{"commerce", "/api/v1/rooms/9/gifts", `{"gift_id":1,"count":1}`, "GIFT_RATE_LIMITED", http.StatusTooManyRequests},
		{"query", "/api/v1/rooms/9/stats", `{"unused":true}`, "STATS_UNAVAILABLE", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := http.MethodPost
			if tc.name == "query" {
				method = http.MethodGet
			}
			req, err := http.NewRequest(method, edge.URL+tc.path, bytes.NewBufferString(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer opaque-contract-token")
			req.Header.Set("Idempotency-Key", "idem-1")
			req.Header.Set("X-Request-ID", "request-contract-1")
			req.Header.Set("X-User-ID", "forged")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			var body map[string]any
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tc.status || body["code"] != tc.code || res.Header.Get("X-Request-ID") != "request-contract-1" {
				t.Fatalf("status=%d body=%v request_id=%q", res.StatusCode, body, res.Header.Get("X-Request-ID"))
			}
			mu.Lock()
			got := observed[tc.name]
			mu.Unlock()
			if got.path != tc.path || got.authorization != "Bearer opaque-contract-token" || got.idempotencyKey != "idem-1" || got.requestID != "request-contract-1" || got.userID != "" || got.body != tc.body {
				t.Fatalf("upstream request changed: %+v", got)
			}
		})
	}
}

func TestGatewayDoesNotExposeInternalRoutes(t *testing.T) {
	server, err := gateway.New(gateway.Deps{
		Log: testLogger(), IdentityRoomURL: "http://127.0.0.1:1", InteractionURL: "http://127.0.0.1:1",
		CommerceURL: "http://127.0.0.1:1", QueryURL: "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/users/1", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", res.Code)
	}
}
