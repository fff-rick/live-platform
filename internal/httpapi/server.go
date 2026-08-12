package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/live-platform/internal/auth"
	"github.com/example/live-platform/internal/danmaku"
	"github.com/example/live-platform/internal/gift"
	"github.com/example/live-platform/internal/idgen"
	"github.com/example/live-platform/internal/like"
	"github.com/example/live-platform/internal/observability"
	"github.com/example/live-platform/internal/realtime"
	"github.com/example/live-platform/internal/room"
	"github.com/example/live-platform/internal/stats"
	cftoken "github.com/example/live-platform/internal/token"
	"github.com/example/live-platform/internal/viewer"
	"github.com/example/live-platform/internal/wallet"
	"go.opentelemetry.io/otel/trace"
)

type Pinger interface{ Ping(context.Context) error }

type Server struct {
	log        *slog.Logger
	mysql      Pinger
	redis      Pinger
	centrifugo Pinger
	metrics    *observability.Metrics
	auth       *auth.Service
	appTokens  *auth.TokenManager
	cfTokens   *cftoken.Issuer
	cfSubTTL   time.Duration
	rooms      *room.Service
	danmaku    *danmaku.Service
	likes      *like.Service
	viewers    *viewer.Service
	stats      *stats.Service
	gifts      *gift.Service
	wallet     *wallet.Service
	mux        *http.ServeMux
}

type Deps struct {
	Log        *slog.Logger
	MySQL      Pinger
	Redis      Pinger
	Centrifugo Pinger
	Metrics    *observability.Metrics
	Auth       *auth.Service
	AppTokens  *auth.TokenManager
	CFTokens   *cftoken.Issuer
	CFSubTTL   time.Duration
	Rooms      *room.Service
	Danmaku    *danmaku.Service
	Likes      *like.Service
	Viewers    *viewer.Service
	Stats      *stats.Service
	Gifts      *gift.Service
	Wallet     *wallet.Service
}

func New(d Deps) *Server {
	s := &Server{
		log: d.Log, mysql: d.MySQL, redis: d.Redis, centrifugo: d.Centrifugo, metrics: d.Metrics, auth: d.Auth, appTokens: d.AppTokens,
		cfTokens: d.CFTokens, cfSubTTL: d.CFSubTTL, rooms: d.Rooms, danmaku: d.Danmaku,
		likes: d.Likes, viewers: d.Viewers, stats: d.Stats, gifts: d.Gifts, wallet: d.Wallet, mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	base := requestID(s.log, recoverer(s.log, s.mux))
	if s.metrics != nil {
		base = s.metrics.HTTPMiddleware("live-api", base)
	}
	return observability.TraceHTTP(base)
}

//go:embed demo.html
var demoFS embed.FS

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /ready", s.ready)
	if s.metrics != nil {
		s.mux.Handle("GET /metrics", s.metrics.Handler())
	}
	s.mux.HandleFunc("GET /demo", s.demo)

	s.mux.HandleFunc("POST /api/v1/auth/register", s.register)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.HandleFunc("GET /api/v1/me", s.requireAuth(s.me))
	s.mux.HandleFunc("POST /api/v1/realtime/token", s.requireAuth(s.realtimeToken))

	s.mux.HandleFunc("POST /api/v1/rooms", s.requireAuth(s.createRoom))
	s.mux.HandleFunc("GET /api/v1/rooms/{room_id}", s.getRoom)
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/start", s.requireAuth(s.startRoom))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/stop", s.requireAuth(s.stopRoom))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/join", s.requireAuth(s.joinRoom))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/danmaku", s.requireAuth(s.sendDanmaku))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/like", s.requireAuth(s.likeRoom))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/heartbeat", s.requireAuth(s.heartbeatRoom))
	s.mux.HandleFunc("GET /api/v1/rooms/{room_id}/stats", s.roomStats)
	s.mux.HandleFunc("GET /api/v1/gifts", s.listGifts)
	s.mux.HandleFunc("GET /api/v1/wallet", s.requireAuth(s.walletBalance))
	s.mux.HandleFunc("GET /api/v1/wallet/transactions", s.requireAuth(s.walletTransactions))
	s.mux.HandleFunc("POST /api/v1/wallet/dev-credit", s.requireAuth(s.devWalletCredit))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/gifts", s.requireAuth(s.sendGift))
	s.mux.HandleFunc("GET /api/v1/gift-orders/{order_no}", s.requireAuth(s.giftOrder))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/mutes", s.requireAuth(s.muteUser))
	s.mux.HandleFunc("DELETE /api/v1/rooms/{room_id}/mutes/{user_id}", s.requireAuth(s.unmuteUser))
	s.mux.HandleFunc("POST /api/v1/rooms/{room_id}/bans", s.requireAuth(s.banUser))
	s.mux.HandleFunc("DELETE /api/v1/rooms/{room_id}/bans/{user_id}", s.requireAuth(s.unbanUser))
}

func (s *Server) demo(w http.ResponseWriter, _ *http.Request) {
	b, err := demoFS.ReadFile("demo.html")
	if err != nil {
		http.Error(w, "demo unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "milestone": "M7"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 800*time.Millisecond)
	defer cancel()
	var failed []string
	if err := s.mysql.Ping(ctx); err != nil {
		failed = append(failed, "mysql")
	}
	if err := s.redis.Ping(ctx); err != nil {
		failed = append(failed, "redis")
	}
	if s.centrifugo != nil {
		if err := s.centrifugo.Ping(ctx); err != nil {
			failed = append(failed, "centrifugo")
		}
	}
	if len(failed) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "failed": failed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

type registerRequest struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in registerRequest
	if err := decodeJSON(w, r, 32<<10, &in); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := s.auth.Register(r.Context(), in.Username, in.Nickname, in.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "USERNAME_TAKEN", err.Error())
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "INVALID_AUTH_INPUT", err.Error())
		default:
			s.log.Error("register failed", "error", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "register failed")
		}
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := decodeJSON(w, r, 16<<10, &in); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	out, err := s.auth.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", err.Error())
		case errors.Is(err, auth.ErrUserDisabled):
			writeError(w, http.StatusForbidden, "USER_DISABLED", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "login failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, userID int64) {
	u, err := s.auth.User(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) realtimeToken(w http.ResponseWriter, _ *http.Request, userID int64) {
	jwt, exp, err := s.cfTokens.ConnectionToken(strconv.FormatInt(userID, 10))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to issue realtime token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": jwt, "expire_at": exp.Unix()})
}

type createRoomRequest struct {
	Title string `json:"title"`
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request, userID int64) {
	var in createRoomRequest
	if err := decodeJSON(w, r, 16<<10, &in); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	v, err := s.rooms.Create(r.Context(), userID, in.Title)
	if err != nil {
		if errors.Is(err, room.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "INVALID_ROOM_INPUT", err.Error())
		} else {
			s.log.Error("create room failed", "error", err, "user_id", userID)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "create room failed")
		}
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) getRoom(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	v, err := s.rooms.Get(r.Context(), roomID)
	if err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) startRoom(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	v, err := s.rooms.Start(r.Context(), roomID, userID)
	if err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) stopRoom(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	v, err := s.rooms.Stop(r.Context(), roomID, userID)
	if err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type subscription struct {
	Channel string `json:"channel"`
	Token   string `json:"token"`
}

func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	v, err := s.rooms.Join(r.Context(), roomID, userID)
	if err != nil {
		handleRoomError(w, err)
		return
	}
	viewerState, err := s.viewers.Touch(r.Context(), roomID, userID)
	if err != nil {
		s.log.Error("touch viewer on join", "error", err, "room_id", roomID, "user_id", userID)
		writeError(w, http.StatusServiceUnavailable, "STATS_UNAVAILABLE", "viewer tracking unavailable")
		return
	}
	snapshot, err := s.stats.Get(r.Context(), roomID)
	if err != nil {
		s.log.Error("load stats on join", "error", err, "room_id", roomID)
		writeError(w, http.StatusServiceUnavailable, "STATS_UNAVAILABLE", "room stats unavailable")
		return
	}
	snapshot.ViewerCount = viewerState.ViewerCount
	uid := strconv.FormatInt(userID, 10)
	channels := []string{realtime.RoomStream(roomID), realtime.RoomStats(roomID)}
	subs := make([]subscription, 0, len(channels))
	for _, ch := range channels {
		tok, _, err := s.cfTokens.SubscriptionToken(uid, ch, s.cfSubTTL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to authorize room subscription")
			return
		}
		subs = append(subs, subscription{Channel: ch, Token: tok})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room": v, "subscriptions": subs, "personal_channel": realtime.Personal(userID),
		"stats": snapshot, "heartbeat_interval_seconds": maxInt64(5, viewerState.ExpiresIn/3),
	})
}

type danmakuRequest struct {
	Content string `json:"content"`
}

func (s *Server) sendDanmaku(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	var in danmakuRequest
	if err := decodeJSON(w, r, 8<<10, &in); err != nil {
		if s.metrics != nil {
			s.metrics.Danmaku.WithLabelValues("rejected").Inc()
		}
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	e, err := s.danmaku.Send(r.Context(), roomID, userID, in.Content)
	if err != nil {
		if s.metrics != nil {
			result := "failed"
			if errors.Is(err, danmaku.ErrRateLimited) || errors.Is(err, danmaku.ErrMuted) || errors.Is(err, danmaku.ErrSensitive) || errors.Is(err, danmaku.ErrInvalidContent) || errors.Is(err, room.ErrNotFound) || errors.Is(err, room.ErrNotLiving) || errors.Is(err, room.ErrBanned) {
				result = "rejected"
			}
			s.metrics.Danmaku.WithLabelValues(result).Inc()
		}
		switch {
		case errors.Is(err, danmaku.ErrRateLimited):
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", err.Error())
		case errors.Is(err, danmaku.ErrMuted):
			writeError(w, http.StatusForbidden, "USER_MUTED", err.Error())
		case errors.Is(err, danmaku.ErrSensitive):
			writeError(w, http.StatusUnprocessableEntity, "SENSITIVE_CONTENT", err.Error())
		case errors.Is(err, danmaku.ErrInvalidContent):
			writeError(w, http.StatusBadRequest, "INVALID_DANMAKU", err.Error())
		case errors.Is(err, room.ErrNotFound), errors.Is(err, room.ErrNotLiving), errors.Is(err, room.ErrBanned):
			handleRoomError(w, err)
		default:
			s.log.Error("send danmaku failed", "error", err, "room_id", roomID, "user_id", userID)
			writeError(w, http.StatusBadGateway, "DANMAKU_FAILED", "failed to send danmaku")
		}
		return
	}
	if s.metrics != nil {
		s.metrics.Danmaku.WithLabelValues("success").Inc()
	}
	writeJSON(w, http.StatusOK, e)
}

type likeRequest struct {
	Count int64 `json:"count"`
}

func (s *Server) likeRoom(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	var in likeRequest
	if err := decodeJSON(w, r, 8<<10, &in); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	result, err := s.likes.Add(r.Context(), roomID, userID, in.Count)
	if err != nil {
		switch {
		case errors.Is(err, like.ErrInvalidCount):
			writeError(w, http.StatusBadRequest, "INVALID_LIKE_COUNT", err.Error())
		case errors.Is(err, room.ErrNotFound), errors.Is(err, room.ErrNotLiving), errors.Is(err, room.ErrBanned):
			handleRoomError(w, err)
		default:
			s.log.Error("add like failed", "error", err, "room_id", roomID, "user_id", userID)
			writeError(w, http.StatusServiceUnavailable, "LIKE_UNAVAILABLE", "failed to record like")
		}
		return
	}
	if s.metrics != nil {
		s.metrics.Likes.Add(float64(in.Count))
	}
	writeJSON(w, http.StatusAccepted, result)
}

func (s *Server) heartbeatRoom(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	v, err := s.rooms.Get(r.Context(), roomID)
	if err != nil {
		handleRoomError(w, err)
		return
	}
	if v.Status != room.StatusLiving {
		handleRoomError(w, room.ErrNotLiving)
		return
	}
	banned, err := s.rooms.IsBanned(r.Context(), roomID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to check room access")
		return
	}
	if banned {
		handleRoomError(w, room.ErrBanned)
		return
	}
	result, err := s.viewers.Touch(r.Context(), roomID, userID)
	if err != nil {
		s.log.Error("viewer heartbeat failed", "error", err, "room_id", roomID, "user_id", userID)
		writeError(w, http.StatusServiceUnavailable, "STATS_UNAVAILABLE", "viewer tracking unavailable")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) roomStats(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), roomID); err != nil {
		handleRoomError(w, err)
		return
	}
	v, err := s.stats.Get(r.Context(), roomID)
	if err != nil {
		s.log.Error("get room stats failed", "error", err, "room_id", roomID)
		writeError(w, http.StatusServiceUnavailable, "STATS_UNAVAILABLE", "room stats unavailable")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) listGifts(w http.ResponseWriter, r *http.Request) {
	items, err := s.gifts.List(r.Context())
	if err != nil {
		s.log.Error("list gifts failed", "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list gifts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) walletBalance(w http.ResponseWriter, r *http.Request, userID int64) {
	v, err := s.wallet.Balance(r.Context(), userID)
	if err != nil {
		s.log.Error("wallet balance failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load wallet")
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) walletTransactions(w http.ResponseWriter, r *http.Request, userID int64) {
	items, err := s.wallet.Transactions(r.Context(), userID)
	if err != nil {
		s.log.Error("wallet transactions failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load wallet transactions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type walletCreditRequest struct {
	Amount int64 `json:"amount"`
}

func (s *Server) devWalletCredit(w http.ResponseWriter, r *http.Request, userID int64) {
	var in walletCreditRequest
	if err := decodeJSON(w, r, 8<<10, &in); err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	v, err := s.wallet.DevCredit(r.Context(), userID, in.Amount)
	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrCreditDisabled):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "endpoint is disabled")
		case errors.Is(err, wallet.ErrInvalidCredit):
			writeError(w, http.StatusBadRequest, "INVALID_CREDIT_AMOUNT", err.Error())
		default:
			s.log.Error("dev wallet credit failed", "error", err, "user_id", userID)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to credit wallet")
		}
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type giftRequest struct {
	GiftID int64 `json:"gift_id"`
	Count  int64 `json:"count"`
}

func (s *Server) sendGift(w http.ResponseWriter, r *http.Request, userID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	requestID := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if requestID == "" {
		if s.metrics != nil {
			s.metrics.GiftOrders.WithLabelValues("rejected").Inc()
		}
		writeError(w, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required")
		return
	}
	var in giftRequest
	if err := decodeJSON(w, r, 8<<10, &in); err != nil || in.GiftID <= 0 {
		if s.metrics != nil {
			s.metrics.GiftOrders.WithLabelValues("rejected").Inc()
		}
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "valid gift_id and count are required")
		return
	}
	result, err := s.gifts.Send(r.Context(), roomID, userID, in.GiftID, in.Count, requestID)
	if err != nil {
		if s.metrics != nil {
			result := "failed"
			if errors.Is(err, gift.ErrInvalidCount) || errors.Is(err, gift.ErrInvalidRequestID) || errors.Is(err, gift.ErrGiftNotFound) || errors.Is(err, gift.ErrGiftOffline) || errors.Is(err, gift.ErrInsufficientBalance) || errors.Is(err, gift.ErrIdempotencyConflict) || errors.Is(err, room.ErrNotFound) || errors.Is(err, room.ErrNotLiving) || errors.Is(err, room.ErrBanned) {
				result = "rejected"
			}
			s.metrics.GiftOrders.WithLabelValues(result).Inc()
		}
		switch {
		case errors.Is(err, gift.ErrInvalidCount):
			writeError(w, http.StatusBadRequest, "INVALID_GIFT_COUNT", err.Error())
		case errors.Is(err, gift.ErrInvalidRequestID):
			writeError(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", err.Error())
		case errors.Is(err, gift.ErrGiftNotFound):
			writeError(w, http.StatusNotFound, "GIFT_NOT_FOUND", err.Error())
		case errors.Is(err, gift.ErrGiftOffline):
			writeError(w, http.StatusConflict, "GIFT_OFFLINE", err.Error())
		case errors.Is(err, gift.ErrInsufficientBalance):
			writeError(w, http.StatusConflict, "INSUFFICIENT_BALANCE", err.Error())
		case errors.Is(err, gift.ErrIdempotencyConflict):
			writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error())
		case errors.Is(err, room.ErrNotFound), errors.Is(err, room.ErrNotLiving), errors.Is(err, room.ErrBanned):
			handleRoomError(w, err)
		default:
			s.log.Error("send gift failed", "error", err, "room_id", roomID, "user_id", userID)
			writeError(w, http.StatusInternalServerError, "GIFT_FAILED", "failed to send gift")
		}
		return
	}
	if s.metrics != nil {
		metricResult := "success"
		if result.IdempotentReplay {
			metricResult = "replay"
		}
		s.metrics.GiftOrders.WithLabelValues(metricResult).Inc()
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) giftOrder(w http.ResponseWriter, r *http.Request, userID int64) {
	orderNo := strings.TrimSpace(r.PathValue("order_no"))
	if len(orderNo) < 2 || len(orderNo) > 64 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid order_no")
		return
	}
	v, err := s.gifts.Order(r.Context(), orderNo, userID)
	if err != nil {
		switch {
		case errors.Is(err, gift.ErrOrderNotFound):
			writeError(w, http.StatusNotFound, "GIFT_ORDER_NOT_FOUND", err.Error())
		case errors.Is(err, gift.ErrOrderForbidden):
			writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
		default:
			s.log.Error("get gift order failed", "error", err, "order_no", orderNo, "user_id", userID)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load gift order")
		}
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type muteRequest struct {
	UserID          int64  `json:"user_id"`
	DurationSeconds int64  `json:"duration_seconds"`
	Reason          string `json:"reason"`
}

func (s *Server) muteUser(w http.ResponseWriter, r *http.Request, actorID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	var in muteRequest
	const maxMuteSeconds = int64(30 * 24 * 60 * 60)
	if err := decodeJSON(w, r, 8<<10, &in); err != nil || in.UserID <= 0 || in.DurationSeconds < 0 || in.DurationSeconds > maxMuteSeconds {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "valid user_id and duration_seconds between 0 and 2592000 are required")
		return
	}
	if err := s.rooms.Mute(r.Context(), roomID, actorID, in.UserID, time.Duration(in.DurationSeconds)*time.Second, in.Reason); err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "muted", "user_id": in.UserID})
}

func (s *Server) unmuteUser(w http.ResponseWriter, r *http.Request, actorID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	targetID, ok := pathInt64(w, r, "user_id")
	if !ok {
		return
	}
	if err := s.rooms.Unmute(r.Context(), roomID, actorID, targetID); err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "unmuted", "user_id": targetID})
}

type banRequest struct {
	UserID int64  `json:"user_id"`
	Reason string `json:"reason"`
}

func (s *Server) banUser(w http.ResponseWriter, r *http.Request, actorID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	var in banRequest
	if err := decodeJSON(w, r, 8<<10, &in); err != nil || in.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "valid user_id is required")
		return
	}
	if err := s.rooms.Ban(r.Context(), roomID, actorID, in.UserID, in.Reason); err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "banned", "user_id": in.UserID})
}

func (s *Server) unbanUser(w http.ResponseWriter, r *http.Request, actorID int64) {
	roomID, ok := pathInt64(w, r, "room_id")
	if !ok {
		return
	}
	targetID, ok := pathInt64(w, r, "user_id")
	if !ok {
		return
	}
	if err := s.rooms.Unban(r.Context(), roomID, actorID, targetID); err != nil {
		handleRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "unbanned", "user_id": targetID})
}

type authedHandler func(http.ResponseWriter, *http.Request, int64)

func (s *Server) requireAuth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(h, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing bearer token")
			return
		}
		claims, err := s.appTokens.Verify(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		next(w, r, claims.UserID)
	}
}

func handleRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrNotFound):
		writeError(w, http.StatusNotFound, "ROOM_NOT_FOUND", err.Error())
	case errors.Is(err, room.ErrNotLiving):
		writeError(w, http.StatusConflict, "ROOM_NOT_LIVING", err.Error())
	case errors.Is(err, room.ErrBanned):
		writeError(w, http.StatusForbidden, "ROOM_BANNED", err.Error())
	case errors.Is(err, room.ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, room.ErrSelfModeration):
		writeError(w, http.StatusBadRequest, "SELF_MODERATION_NOT_ALLOWED", err.Error())
	case errors.Is(err, room.ErrInvalidState):
		writeError(w, http.StatusConflict, "INVALID_ROOM_STATE", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "room operation failed")
	}
}

func pathInt64(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v <= 0 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", name+" must be a positive integer")
		return 0, false
	}
	return v, true
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

func requestID(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = idgen.New()
		}
		w.Header().Set("X-Request-ID", id)
		start := time.Now()
		next.ServeHTTP(w, r)
		attrs := []any{"request_id", id, "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds()}
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
				log.Error("panic recovered", "panic", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"code": errCode, "message": msg})
}
