// Package commerceapi exposes the Stage 2 wallet and gift boundary. It keeps
// the existing public v1 paths so live-api can proxy traffic without client
// changes during the strangler migration.
package commerceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/gift"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/viewer"
	"github.com/example/live-platform/internal/wallet"
)

type Server struct {
	log     *slog.Logger
	tokens  *auth.TokenManager
	gifts   *gift.Service
	wallet  *wallet.Service
	viewers *viewer.Service // Transitional: becomes an interaction projection in Stage 3.
	metrics *observability.Metrics
	mysql   Pinger
	redis   Pinger
	mux     *http.ServeMux
}

type Pinger interface{ Ping(context.Context) error }

type Deps struct {
	Log     *slog.Logger
	Tokens  *auth.TokenManager
	Gifts   *gift.Service
	Wallet  *wallet.Service
	Viewers *viewer.Service
	Metrics *observability.Metrics
	MySQL   Pinger
	Redis   Pinger
}

func New(d Deps) *Server {
	s := &Server{log: d.Log, tokens: d.Tokens, gifts: d.Gifts, wallet: d.Wallet, viewers: d.Viewers, metrics: d.Metrics, mysql: d.MySQL, redis: d.Redis, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "live-commerce"})
	})
	s.mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
		defer cancel()
		var failed []string
		if s.mysql != nil && s.mysql.Ping(ctx) != nil {
			failed = append(failed, "mysql")
		}
		if s.redis != nil && s.redis.Ping(ctx) != nil {
			failed = append(failed, "redis")
		}
		if len(failed) > 0 {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "failed": failed})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "service": "live-commerce"})
	})
	if s.metrics != nil {
		s.mux.Handle("GET /metrics", s.metrics.Handler())
	}
	s.mux.HandleFunc("GET /api/v1/gifts", s.listGifts)
	s.mux.HandleFunc("GET /api/v1/wallet", s.requireAuth(s.walletBalance))
	s.mux.HandleFunc("GET /api/v1/wallet/transactions", s.requireAuth(s.walletTransactions))
	s.mux.HandleFunc("POST /api/v1/wallet/dev-credit", s.requireAuth(s.devWalletCredit))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/gifts", s.requireAuth(s.sendGift))
	s.mux.HandleFunc("GET /api/v1/gift-orders/{order_no}", s.requireAuth(s.giftOrder))
	return s
}

func (s *Server) Handler() http.Handler {
	base := recoverer(s.log, s.mux)
	if s.metrics != nil {
		base = s.metrics.HTTPMiddleware("live-commerce", base)
	}
	return base
}

func (s *Server) listGifts(w http.ResponseWriter, r *http.Request) {
	items, err := s.gifts.List(r.Context())
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", "failed to list gifts")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) walletBalance(w http.ResponseWriter, r *http.Request, userID int64) {
	v, err := s.wallet.Balance(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", "failed to load wallet")
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) walletTransactions(w http.ResponseWriter, r *http.Request, userID int64) {
	items, err := s.wallet.Transactions(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", "failed to load wallet transactions")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) devWalletCredit(w http.ResponseWriter, r *http.Request, userID int64) {
	var in struct {
		Amount int64 `json:"amount"`
	}
	if err := decodeJSON(w, r, 8<<10, &in); err != nil {
		writeError(w, 400, "BAD_REQUEST", err.Error())
		return
	}
	v, err := s.wallet.DevCredit(r.Context(), userID, in.Amount)
	if errors.Is(err, wallet.ErrCreditDisabled) {
		writeError(w, 404, "NOT_FOUND", "endpoint is disabled")
		return
	}
	if errors.Is(err, wallet.ErrInvalidCredit) {
		writeError(w, 400, "INVALID_CREDIT_AMOUNT", err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", "failed to credit wallet")
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) sendGift(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, err := positivePathInt(r, "room_id")
	if err != nil {
		writeError(w, 400, "BAD_REQUEST", "room_id must be a positive integer")
		return
	}
	requestID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if requestID == "" {
		s.giftMetric("rejected")
		writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required")
		return
	}
	var in struct {
		GiftID int64 `json:"gift_id"`
		Count  int64 `json:"count"`
	}
	if err := decodeJSON(w, r, 8<<10, &in); err != nil || in.GiftID <= 0 {
		s.giftMetric("rejected")
		writeError(w, 400, "BAD_REQUEST", "valid gift_id and count are required")
		return
	}
	result, err := s.gifts.Send(r.Context(), roomID, userID, in.GiftID, in.Count, requestID)
	if err != nil {
		s.handleGiftError(w, err)
		return
	}
	if result.EventQueued && s.viewers != nil {
		_ = s.viewers.AddGiftValue(r.Context(), roomID, userID, result.Order.TotalAmount)
	}
	if result.IdempotentReplay {
		s.giftMetric("replay")
	} else {
		s.giftMetric("success")
	}
	writeJSON(w, 200, result)
}

func (s *Server) giftOrder(w http.ResponseWriter, r *http.Request, userID int64) {
	orderNo := strings.TrimSpace(r.PathValue("order_no"))
	if len(orderNo) < 2 || len(orderNo) > 64 {
		writeError(w, 400, "BAD_REQUEST", "invalid order_no")
		return
	}
	v, err := s.gifts.Order(r.Context(), orderNo, userID)
	if errors.Is(err, gift.ErrOrderNotFound) {
		writeError(w, 404, "GIFT_ORDER_NOT_FOUND", err.Error())
		return
	}
	if errors.Is(err, gift.ErrOrderForbidden) {
		writeError(w, 403, "FORBIDDEN", err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "INTERNAL_ERROR", "failed to load gift order")
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) handleGiftError(w http.ResponseWriter, err error) {
	result := "failed"
	code, message := 500, "GIFT_FAILED"
	switch {
	case errors.Is(err, gift.ErrInvalidCount):
		code, message = 400, "INVALID_GIFT_COUNT"
	case errors.Is(err, gift.ErrInvalidRequestID):
		code, message = 400, "INVALID_IDEMPOTENCY_KEY"
	case errors.Is(err, gift.ErrGiftNotFound):
		code, message = 404, "GIFT_NOT_FOUND"
	case errors.Is(err, gift.ErrGiftOffline):
		code, message = 409, "GIFT_OFFLINE"
	case errors.Is(err, gift.ErrInsufficientBalance):
		code, message = 409, "INSUFFICIENT_BALANCE"
	case errors.Is(err, gift.ErrIdempotencyConflict):
		code, message = 409, "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, gift.ErrRateLimited):
		code, message, result = 429, "GIFT_RATE_LIMITED", "rejected"
	case errors.Is(err, room.ErrNotFound):
		code, message, result = 404, "ROOM_NOT_FOUND", "rejected"
	case errors.Is(err, room.ErrNotLiving):
		code, message, result = 409, "ROOM_NOT_LIVING", "rejected"
	case errors.Is(err, room.ErrBanned):
		code, message, result = 403, "ROOM_BANNED", "rejected"
	default:
		writeError(w, code, message, "failed to send gift")
		s.giftMetric(result)
		return
	}
	if result != "failed" || code < 500 {
		result = "rejected"
	}
	if errors.Is(err, gift.ErrRateLimited) && s.metrics != nil {
		s.metrics.GiftRateLimited.Inc()
	}
	s.giftMetric(result)
	writeError(w, code, message, err.Error())
}

type authedHandler func(http.ResponseWriter, *http.Request, int64)

func (s *Server) requireAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, 401, "UNAUTHORIZED", "missing bearer token")
			return
		}
		claims, err := s.tokens.Verify(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
		if err != nil {
			writeError(w, 401, "UNAUTHORIZED", err.Error())
			return
		}
		next(w, r, claims.UserID)
	}
}
func (s *Server) giftMetric(result string) {
	if s.metrics != nil {
		s.metrics.GiftOrders.WithLabelValues(result).Inc()
	}
}
func positivePathInt(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v <= 0 {
		return 0, errors.New("invalid path")
	}
	return v, nil
}
func decodeJSON(w http.ResponseWriter, r *http.Request, max int64, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, max))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid json body")
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("json body must contain exactly one object")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, code int, errorCode, msg string) {
	writeJSON(w, code, map[string]any{"code": errorCode, "message": msg})
}
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if log != nil {
					log.Error("panic recovered", "panic", v)
				}
				writeError(w, 500, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
