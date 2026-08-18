package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTP          HTTPConfig
	MySQL         MySQLConfig
	Redis         RedisConfig
	Centrifugo    CentrifugoConfig
	Auth          AuthConfig
	Danmaku       DanmakuConfig
	Gift          GiftConfig
	Engagement    EngagementConfig
	Wallet        WalletConfig
	Kafka         KafkaConfig
	Outbox        OutboxConfig
	Observability ObservabilityConfig
	Traffic       TrafficConfig
	Worker        WorkerConfig
}

type HTTPConfig struct{ Addr string }
type MySQLConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}
type RedisConfig struct {
	URL            string
	Addr, Password string
	DB             int
}
type CentrifugoConfig struct {
	APIURL               string
	APIKey               string
	TokenSecret          string
	TokenTTL             time.Duration
	SubscriptionTokenTTL time.Duration
}
type AuthConfig struct {
	JWTSecret string
	TokenTTL  time.Duration
}
type DanmakuConfig struct {
	SensitiveWords []string
	UserRateLimit  int
	UserRateWindow time.Duration
}
type GiftConfig struct {
	MaxCountPerRequest int64
	UserRateLimit      int
	UserRateWindow     time.Duration
}
type WalletConfig struct{ DevCreditEnabled bool }
type KafkaConfig struct {
	Brokers              []string
	DanmakuTopic         string
	GiftTopic            string
	DanmakuConsumerGroup string
	GiftConsumerGroup    string
	ConsumerLease        time.Duration
	TLSEnabled           bool
	SASLMechanism        string
	SASLUsername         string
	SASLPassword         string
}

type WorkerConfig struct {
	Roles []string
}
type TrafficConfig struct {
	HotViewers         int64
	ProtectViewers     int64
	HotDanmakuRate     int64
	ProtectDanmakuRate int64
	HotSampleRate      float64
	ProtectSampleRate  float64
	RateWindow         time.Duration
	AdaptiveEnabled    bool
	TargetFanoutRate   float64
	HotFanoutRate      float64
	ProtectFanoutRate  float64
	MinSampleRate      float64
}

type ObservabilityConfig struct {
	WorkerMetricsAddr string
	OTelEnabled       bool
	OTelEndpoint      string
	OTelInsecure      bool
	OTelSampleRatio   float64
	Environment       string
}

type OutboxConfig struct {
	PollInterval   time.Duration
	BatchSize      int
	Lease          time.Duration
	ProduceTimeout time.Duration
}

type EngagementConfig struct {
	ViewerTTL        time.Duration
	StatsInterval    time.Duration
	ActiveRoomWindow time.Duration
	ActiveRoomBatch  int64
}

func Load() (Config, error) {
	mysqlMaxOpenConns, err := strconv.Atoi(env("MYSQL_MAX_OPEN_CONNS", "40"))
	if err != nil || mysqlMaxOpenConns <= 0 {
		return Config{}, fmt.Errorf("MYSQL_MAX_OPEN_CONNS must be a positive integer")
	}
	mysqlMaxIdleConns, err := strconv.Atoi(env("MYSQL_MAX_IDLE_CONNS", "20"))
	if err != nil || mysqlMaxIdleConns < 0 || mysqlMaxIdleConns > mysqlMaxOpenConns {
		return Config{}, fmt.Errorf("MYSQL_MAX_IDLE_CONNS must be between 0 and MYSQL_MAX_OPEN_CONNS")
	}
	mysqlConnMaxLifetime, err := time.ParseDuration(env("MYSQL_CONN_MAX_LIFETIME", "30m"))
	if err != nil || mysqlConnMaxLifetime <= 0 {
		return Config{}, fmt.Errorf("MYSQL_CONN_MAX_LIFETIME must be a positive duration")
	}

	db, err := strconv.Atoi(env("REDIS_DB", "0"))
	if err != nil {
		return Config{}, fmt.Errorf("REDIS_DB: %w", err)
	}
	cfTTL, err := time.ParseDuration(env("CENTRIFUGO_TOKEN_TTL", "1h"))
	if err != nil {
		return Config{}, fmt.Errorf("CENTRIFUGO_TOKEN_TTL: %w", err)
	}
	subTTL, err := time.ParseDuration(env("CENTRIFUGO_SUBSCRIPTION_TOKEN_TTL", "5m"))
	if err != nil {
		return Config{}, fmt.Errorf("CENTRIFUGO_SUBSCRIPTION_TOKEN_TTL: %w", err)
	}
	authTTL, err := time.ParseDuration(env("AUTH_TOKEN_TTL", "24h"))
	if err != nil {
		return Config{}, fmt.Errorf("AUTH_TOKEN_TTL: %w", err)
	}

	viewerTTL, err := time.ParseDuration(env("VIEWER_TTL", "90s"))
	if err != nil || viewerTTL <= 0 {
		return Config{}, fmt.Errorf("VIEWER_TTL must be a positive duration")
	}
	statsInterval, err := time.ParseDuration(env("STATS_INTERVAL", "200ms"))
	if err != nil || statsInterval <= 0 {
		return Config{}, fmt.Errorf("STATS_INTERVAL must be a positive duration")
	}
	activeRoomWindow, err := time.ParseDuration(env("ACTIVE_ROOM_WINDOW", "3m"))
	if err != nil || activeRoomWindow <= viewerTTL {
		return Config{}, fmt.Errorf("ACTIVE_ROOM_WINDOW must be greater than VIEWER_TTL")
	}
	activeRoomBatch, err := strconv.ParseInt(env("ACTIVE_ROOM_BATCH", "1000"), 10, 64)
	if err != nil || activeRoomBatch <= 0 {
		return Config{}, fmt.Errorf("ACTIVE_ROOM_BATCH must be a positive integer")
	}
	devCreditEnabled, err := strconv.ParseBool(env("ENABLE_DEV_WALLET_CREDIT", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("ENABLE_DEV_WALLET_CREDIT: %w", err)
	}

	danmakuUserRateLimit, err := strconv.Atoi(env("DANMAKU_USER_RATE_LIMIT", "5"))
	if err != nil || danmakuUserRateLimit <= 0 {
		return Config{}, fmt.Errorf("DANMAKU_USER_RATE_LIMIT must be a positive integer")
	}
	danmakuUserRateWindow, err := time.ParseDuration(env("DANMAKU_USER_RATE_WINDOW", "10s"))
	if err != nil || danmakuUserRateWindow <= 0 {
		return Config{}, fmt.Errorf("DANMAKU_USER_RATE_WINDOW must be a positive duration")
	}
	giftMaxCount, err := strconv.ParseInt(env("GIFT_MAX_COUNT_PER_REQUEST", "100"), 10, 64)
	if err != nil || giftMaxCount <= 0 || giftMaxCount > 10000 {
		return Config{}, fmt.Errorf("GIFT_MAX_COUNT_PER_REQUEST must be between 1 and 10000")
	}
	giftUserRateLimit, err := strconv.Atoi(env("GIFT_USER_RATE_LIMIT", "10"))
	if err != nil || giftUserRateLimit <= 0 {
		return Config{}, fmt.Errorf("GIFT_USER_RATE_LIMIT must be a positive integer")
	}
	giftUserRateWindow, err := time.ParseDuration(env("GIFT_USER_RATE_WINDOW", "1s"))
	if err != nil || giftUserRateWindow <= 0 {
		return Config{}, fmt.Errorf("GIFT_USER_RATE_WINDOW must be a positive duration")
	}

	workerRoles := splitCSV(env("WORKER_ROLES", "stats,outbox,gift-consumer,danmaku-consumer"))
	if len(workerRoles) == 0 {
		return Config{}, fmt.Errorf("WORKER_ROLES must contain at least one role")
	}
	allowedWorkerRoles := map[string]struct{}{
		"stats": {}, "outbox": {}, "gift-consumer": {}, "danmaku-consumer": {},
	}
	seenWorkerRoles := make(map[string]struct{}, len(workerRoles))
	for i, role := range workerRoles {
		role = strings.ToLower(strings.TrimSpace(role))
		if _, ok := allowedWorkerRoles[role]; !ok {
			return Config{}, fmt.Errorf("WORKER_ROLES contains unsupported role %q", role)
		}
		if _, duplicate := seenWorkerRoles[role]; duplicate {
			return Config{}, fmt.Errorf("WORKER_ROLES contains duplicate role %q", role)
		}
		workerRoles[i] = role
		seenWorkerRoles[role] = struct{}{}
	}

	kafkaBrokers := splitCSV(env("KAFKA_BROKERS", "kafka:9092"))
	if len(kafkaBrokers) == 0 {
		return Config{}, fmt.Errorf("KAFKA_BROKERS must contain at least one broker")
	}
	kafkaTLSEnabled, err := strconv.ParseBool(env("KAFKA_TLS_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("KAFKA_TLS_ENABLED: %w", err)
	}
	kafkaSASLMechanism := strings.ToLower(strings.TrimSpace(os.Getenv("KAFKA_SASL_MECHANISM")))
	switch kafkaSASLMechanism {
	case "", "plain", "scram-sha-256", "scram-sha-512":
	default:
		return Config{}, fmt.Errorf("KAFKA_SASL_MECHANISM must be one of: plain, scram-sha-256, scram-sha-512")
	}
	needsKafkaCredentials := true // live-api uses Kafka for best-effort danmaku persistence.
	if rawRoles := strings.TrimSpace(os.Getenv("WORKER_ROLES")); rawRoles != "" {
		needsKafkaCredentials = false
		for _, role := range workerRoles {
			if role == "outbox" || role == "gift-consumer" || role == "danmaku-consumer" {
				needsKafkaCredentials = true
				break
			}
		}
	}
	if kafkaSASLMechanism != "" && needsKafkaCredentials && (os.Getenv("KAFKA_SASL_USERNAME") == "" || os.Getenv("KAFKA_SASL_PASSWORD") == "") {
		return Config{}, fmt.Errorf("KAFKA_SASL_USERNAME and KAFKA_SASL_PASSWORD are required when Kafka is used with KAFKA_SASL_MECHANISM")
	}
	consumerLease, err := time.ParseDuration(env("KAFKA_CONSUMER_LEASE", "30s"))
	if err != nil || consumerLease <= 0 {
		return Config{}, fmt.Errorf("KAFKA_CONSUMER_LEASE must be a positive duration")
	}
	outboxPoll, err := time.ParseDuration(env("OUTBOX_POLL_INTERVAL", "100ms"))
	if err != nil || outboxPoll <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_POLL_INTERVAL must be a positive duration")
	}
	outboxBatch, err := strconv.Atoi(env("OUTBOX_BATCH_SIZE", "100"))
	if err != nil || outboxBatch <= 0 || outboxBatch > 1000 {
		return Config{}, fmt.Errorf("OUTBOX_BATCH_SIZE must be between 1 and 1000")
	}
	outboxLease, err := time.ParseDuration(env("OUTBOX_LEASE", "30s"))
	if err != nil || outboxLease <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_LEASE must be a positive duration")
	}
	outboxProduceTimeout, err := time.ParseDuration(env("OUTBOX_PRODUCE_TIMEOUT", "5s"))
	if err != nil || outboxProduceTimeout <= 0 {
		return Config{}, fmt.Errorf("OUTBOX_PRODUCE_TIMEOUT must be a positive duration")
	}

	otelEnabled, err := strconv.ParseBool(env("OTEL_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("OTEL_ENABLED: %w", err)
	}
	otelInsecure, err := strconv.ParseBool(env("OTEL_INSECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("OTEL_INSECURE: %w", err)
	}
	otelSampleRatio, err := strconv.ParseFloat(env("OTEL_SAMPLE_RATIO", "1"), 64)
	if err != nil || otelSampleRatio < 0 || otelSampleRatio > 1 {
		return Config{}, fmt.Errorf("OTEL_SAMPLE_RATIO must be between 0 and 1")
	}

	hotViewers, err := strconv.ParseInt(env("DANMAKU_HOT_VIEWERS", "50000"), 10, 64)
	if err != nil || hotViewers <= 0 {
		return Config{}, fmt.Errorf("DANMAKU_HOT_VIEWERS must be a positive integer")
	}
	protectViewers, err := strconv.ParseInt(env("DANMAKU_PROTECT_VIEWERS", "100000"), 10, 64)
	if err != nil || protectViewers < hotViewers {
		return Config{}, fmt.Errorf("DANMAKU_PROTECT_VIEWERS must be >= DANMAKU_HOT_VIEWERS")
	}
	hotRate, err := strconv.ParseInt(env("DANMAKU_HOT_RATE", "500"), 10, 64)
	if err != nil || hotRate <= 0 {
		return Config{}, fmt.Errorf("DANMAKU_HOT_RATE must be a positive integer")
	}
	protectRate, err := strconv.ParseInt(env("DANMAKU_PROTECT_RATE", "2000"), 10, 64)
	if err != nil || protectRate < hotRate {
		return Config{}, fmt.Errorf("DANMAKU_PROTECT_RATE must be >= DANMAKU_HOT_RATE")
	}
	hotSample, err := strconv.ParseFloat(env("DANMAKU_HOT_SAMPLE_RATE", "0.5"), 64)
	if err != nil || hotSample <= 0 || hotSample > 1 {
		return Config{}, fmt.Errorf("DANMAKU_HOT_SAMPLE_RATE must be in (0,1]")
	}
	protectSample, err := strconv.ParseFloat(env("DANMAKU_PROTECT_SAMPLE_RATE", "0.2"), 64)
	if err != nil || protectSample <= 0 || protectSample > hotSample {
		return Config{}, fmt.Errorf("DANMAKU_PROTECT_SAMPLE_RATE must be in (0,DANMAKU_HOT_SAMPLE_RATE]")
	}
	rateWindow, err := time.ParseDuration(env("DANMAKU_RATE_WINDOW", "1s"))
	if err != nil || rateWindow < time.Millisecond {
		return Config{}, fmt.Errorf("DANMAKU_RATE_WINDOW must be at least 1ms")
	}
	adaptiveEnabled, err := strconv.ParseBool(env("DANMAKU_ADAPTIVE_ENABLED", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("DANMAKU_ADAPTIVE_ENABLED: %w", err)
	}
	targetFanout, err := strconv.ParseFloat(env("DANMAKU_TARGET_FANOUT_RATE", "25000"), 64)
	if err != nil || targetFanout <= 0 {
		return Config{}, fmt.Errorf("DANMAKU_TARGET_FANOUT_RATE must be positive")
	}
	hotFanout, err := strconv.ParseFloat(env("DANMAKU_HOT_FANOUT_RATE", "30000"), 64)
	if err != nil || hotFanout < targetFanout {
		return Config{}, fmt.Errorf("DANMAKU_HOT_FANOUT_RATE must be >= DANMAKU_TARGET_FANOUT_RATE")
	}
	protectFanout, err := strconv.ParseFloat(env("DANMAKU_PROTECT_FANOUT_RATE", "40000"), 64)
	if err != nil || protectFanout < hotFanout {
		return Config{}, fmt.Errorf("DANMAKU_PROTECT_FANOUT_RATE must be >= DANMAKU_HOT_FANOUT_RATE")
	}
	minSampleRate, err := strconv.ParseFloat(env("DANMAKU_MIN_SAMPLE_RATE", "0.05"), 64)
	if err != nil || minSampleRate <= 0 || minSampleRate > 1 {
		return Config{}, fmt.Errorf("DANMAKU_MIN_SAMPLE_RATE must be in (0,1]")
	}

	var words []string
	for _, w := range strings.Split(env("DANMAKU_SENSITIVE_WORDS", "spam,banned"), ",") {
		if w = strings.TrimSpace(w); w != "" {
			words = append(words, w)
		}
	}
	return Config{
		HTTP: HTTPConfig{Addr: env("HTTP_ADDR", ":8080")},
		MySQL: MySQLConfig{
			DSN:             env("MYSQL_DSN", "live:live@tcp(mysql:3306)/live?parseTime=true&charset=utf8mb4"),
			MaxOpenConns:    mysqlMaxOpenConns,
			MaxIdleConns:    mysqlMaxIdleConns,
			ConnMaxLifetime: mysqlConnMaxLifetime,
		},
		Redis: RedisConfig{URL: os.Getenv("REDIS_URL"), Addr: env("REDIS_ADDR", "redis:6379"), Password: os.Getenv("REDIS_PASSWORD"), DB: db},
		Centrifugo: CentrifugoConfig{
			APIURL:               env("CENTRIFUGO_API_URL", "http://centrifugo:8000/api"),
			APIKey:               env("CENTRIFUGO_API_KEY", "dev-api-key-change-me"),
			TokenSecret:          env("CENTRIFUGO_TOKEN_SECRET", "dev-token-secret-change-me"),
			TokenTTL:             cfTTL,
			SubscriptionTokenTTL: subTTL,
		},
		Auth:       AuthConfig{JWTSecret: env("AUTH_JWT_SECRET", "dev-app-jwt-secret-change-me"), TokenTTL: authTTL},
		Danmaku:    DanmakuConfig{SensitiveWords: words, UserRateLimit: danmakuUserRateLimit, UserRateWindow: danmakuUserRateWindow},
		Gift:       GiftConfig{MaxCountPerRequest: giftMaxCount, UserRateLimit: giftUserRateLimit, UserRateWindow: giftUserRateWindow},
		Engagement: EngagementConfig{ViewerTTL: viewerTTL, StatsInterval: statsInterval, ActiveRoomWindow: activeRoomWindow, ActiveRoomBatch: activeRoomBatch},
		Wallet:     WalletConfig{DevCreditEnabled: devCreditEnabled},
		Kafka: KafkaConfig{
			Brokers:              kafkaBrokers,
			DanmakuTopic:         env("KAFKA_DANMAKU_TOPIC", "live.danmaku.v1"),
			GiftTopic:            env("KAFKA_GIFT_TOPIC", "live.gift.v1"),
			DanmakuConsumerGroup: env("KAFKA_DANMAKU_CONSUMER_GROUP", "live-danmaku-persist-v1"),
			GiftConsumerGroup:    env("KAFKA_GIFT_CONSUMER_GROUP", "live-gift-realtime-v1"),
			ConsumerLease:        consumerLease,
			TLSEnabled:           kafkaTLSEnabled,
			SASLMechanism:        kafkaSASLMechanism,
			SASLUsername:         os.Getenv("KAFKA_SASL_USERNAME"),
			SASLPassword:         os.Getenv("KAFKA_SASL_PASSWORD"),
		},
		Outbox:  OutboxConfig{PollInterval: outboxPoll, BatchSize: outboxBatch, Lease: outboxLease, ProduceTimeout: outboxProduceTimeout},
		Traffic: TrafficConfig{HotViewers: hotViewers, ProtectViewers: protectViewers, HotDanmakuRate: hotRate, ProtectDanmakuRate: protectRate, HotSampleRate: hotSample, ProtectSampleRate: protectSample, RateWindow: rateWindow, AdaptiveEnabled: adaptiveEnabled, TargetFanoutRate: targetFanout, HotFanoutRate: hotFanout, ProtectFanoutRate: protectFanout, MinSampleRate: minSampleRate},
		Worker:  WorkerConfig{Roles: workerRoles},
		Observability: ObservabilityConfig{
			WorkerMetricsAddr: env("WORKER_METRICS_ADDR", ":9090"),
			OTelEnabled:       otelEnabled,
			OTelEndpoint:      env("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317"),
			OTelInsecure:      otelInsecure,
			OTelSampleRatio:   otelSampleRatio,
			Environment:       env("DEPLOYMENT_ENVIRONMENT", "development"),
		},
	}, nil
}

func splitCSV(raw string) []string {
	var out []string
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
