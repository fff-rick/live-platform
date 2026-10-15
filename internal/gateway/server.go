package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/webui"
	"go.opentelemetry.io/otel/trace"
)

type Deps struct {
	Log             *slog.Logger
	Metrics         *observability.Metrics
	CommerceURL     string
	InteractionURL  string
	IdentityRoomURL string
	QueryURL        string
}

type Server struct {
	log     *slog.Logger
	metrics *observability.Metrics
	mux     *http.ServeMux
}

func New(d Deps) (*Server, error) {
	if d.Log == nil {
		return nil, errors.New("gateway logger is required")
	}
	s := &Server{log: d.Log, metrics: d.Metrics, mux: http.NewServeMux()}
	identity, err := s.proxy("identity-room", d.IdentityRoomURL, "IDENTITY_ROOM_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	interaction, err := s.proxy("interaction", d.InteractionURL, "INTERACTION_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	commerce, err := s.proxy("commerce", d.CommerceURL, "COMMERCE_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	query, err := s.proxy("query", d.QueryURL, "QUERY_UNAVAILABLE")
	if err != nil {
		return nil, err
	}
	s.routes(identity, interaction, commerce, query)
	return s, nil
}

func (s *Server) proxy(name, rawURL, errorCode string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("invalid %s upstream URL %q", name, rawURL)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		director(r)
		r.Header.Del("X-User-ID")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		s.log.ErrorContext(r.Context(), "upstream proxy failed", "upstream", name, "error", err)
		writeError(w, http.StatusBadGateway, errorCode, name+" service unavailable")
	}
	return proxy, nil
}

func (s *Server) routes(identity, interaction, commerce, query http.Handler) {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "milestone": "M8"})
	})
	s.mux.HandleFunc("GET /ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	if s.metrics != nil {
		s.mux.Handle("GET /metrics", s.metrics.Handler())
	}

	s.mux.Handle("POST /api/v1/auth/register", identity)
	s.mux.Handle("POST /api/v1/auth/login", identity)
	s.mux.Handle("GET /api/v1/me", identity)
	s.mux.Handle("POST /api/v1/realtime/token", identity)
	s.mux.Handle("POST /api/v1/rooms", identity)
	s.mux.Handle("GET /api/v1/rooms", identity)
	s.mux.Handle("GET /api/v1/rooms/{room_id}", identity)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/start", identity)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/stop", identity)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/mutes", identity)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/mutes", identity)
	s.mux.Handle("DELETE /api/v1/rooms/{room_id}/mutes/{user_id}", identity)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/bans", identity)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/bans", identity)
	s.mux.Handle("DELETE /api/v1/rooms/{room_id}/bans/{user_id}", identity)

	s.mux.Handle("POST /api/v1/rooms/{room_id}/join", interaction)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/leave", interaction)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/danmaku", interaction)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/like", interaction)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/heartbeat", interaction)

	s.mux.Handle("GET /api/v1/gifts", commerce)
	s.mux.Handle("GET /api/v1/wallet", commerce)
	s.mux.Handle("GET /api/v1/wallet/transactions", commerce)
	s.mux.Handle("POST /api/v1/wallet/dev-credit", commerce)
	s.mux.Handle("POST /api/v1/rooms/{room_id}/gifts", commerce)
	s.mux.Handle("GET /api/v1/gift-orders/{order_no}", commerce)

	s.mux.Handle("GET /api/v1/rooms/{room_id}/messages", query)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/top-viewers", query)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/viewers", query)
	s.mux.Handle("GET /api/v1/rooms/{room_id}/stats", query)

	for _, pattern := range []string{"GET /internal/", "POST /internal/", "PUT /internal/", "PATCH /internal/", "DELETE /internal/"} {
		s.mux.HandleFunc(pattern, http.NotFound)
	}
	s.mux.Handle("GET /", webui.Handler())
}

func (s *Server) Handler() http.Handler {
	base := requestID(s.log, recoverer(s.log, s.mux))
	if s.metrics != nil {
		base = s.metrics.HTTPMiddleware("live-api", base)
	}
	return observability.TraceHTTP(base)
}

func requestID(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = idgen.New()
		}
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		started := time.Now()
		next.ServeHTTP(w, r)
		attrs := []any{"request_id", id, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds()}
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			attrs = append(attrs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
		}
		log.InfoContext(r.Context(), "http request", attrs...)
	})
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic recovered", "panic", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, code int, errorCode, message string) {
	writeJSON(w, code, map[string]any{"code": errorCode, "message": message})
}
