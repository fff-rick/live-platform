package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry

	HTTPRequests         *prometheus.CounterVec
	HTTPDuration         *prometheus.HistogramVec
	Danmaku              *prometheus.CounterVec
	Likes                prometheus.Counter
	GiftOrders           *prometheus.CounterVec
	RealtimePublish      *prometheus.CounterVec
	StatsBroadcast       *prometheus.CounterVec
	OutboxPending        prometheus.Gauge
	OutboxPublish        *prometheus.CounterVec
	OutboxRetries        prometheus.Counter
	KafkaProduce         *prometheus.CounterVec
	KafkaProduceDur      *prometheus.HistogramVec
	KafkaConsume         *prometheus.CounterVec
	KafkaConsumerLag     *prometheus.GaugeVec
	KafkaBufferedRecords *prometheus.GaugeVec
}

func NewMetrics(service string) *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	const ns = "live"
	m := &Metrics{
		registry:             reg,
		HTTPRequests:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "http_requests_total", Help: "HTTP requests by service, method, route and status."}, []string{"service", "method", "route", "status"}),
		HTTPDuration:         prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "http_request_duration_seconds", Help: "HTTP request latency.", Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}}, []string{"service", "method", "route"}),
		Danmaku:              prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "danmaku_total", Help: "Danmaku requests by result."}, []string{"result"}),
		Likes:                prometheus.NewCounter(prometheus.CounterOpts{Namespace: ns, Name: "likes_total", Help: "Accepted like count."}),
		GiftOrders:           prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "gift_orders_total", Help: "Gift API requests by result."}, []string{"result"}),
		RealtimePublish:      prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "realtime_publish_total", Help: "Centrifugo publish attempts by result."}, []string{"result"}),
		StatsBroadcast:       prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "stats_broadcast_total", Help: "Room stats broadcasts by result."}, []string{"result"}),
		OutboxPending:        prometheus.NewGauge(prometheus.GaugeOpts{Namespace: ns, Name: "outbox_pending", Help: "Outbox rows not yet published."}),
		OutboxPublish:        prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "outbox_publish_total", Help: "Outbox publish attempts by result."}, []string{"result"}),
		OutboxRetries:        prometheus.NewCounter(prometheus.CounterOpts{Namespace: ns, Name: "outbox_retry_total", Help: "Outbox publish retries."}),
		KafkaProduce:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "kafka_produce_total", Help: "Kafka produce attempts by topic and result."}, []string{"topic", "result"}),
		KafkaProduceDur:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: ns, Name: "kafka_produce_duration_seconds", Help: "Kafka synchronous/async delivery latency.", Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5}}, []string{"topic"}),
		KafkaConsume:         prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: ns, Name: "kafka_consume_total", Help: "Kafka consumed records by group, topic and result."}, []string{"group", "topic", "result"}),
		KafkaConsumerLag:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: ns, Name: "kafka_consumer_lag", Help: "Approximate consumer lag from the fetch high watermark after commit."}, []string{"group", "topic", "partition"}),
		KafkaBufferedRecords: prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: ns, Name: "kafka_buffered_records", Help: "Records currently buffered in a franz-go client."}, []string{"client", "direction"}),
	}
	reg.MustRegister(m.HTTPRequests, m.HTTPDuration, m.Danmaku, m.Likes, m.GiftOrders, m.RealtimePublish, m.StatsBroadcast, m.OutboxPending, m.OutboxPublish, m.OutboxRetries, m.KafkaProduce, m.KafkaProduceDur, m.KafkaConsume, m.KafkaConsumerLag, m.KafkaBufferedRecords)
	// Pre-initialize low-cardinality counters so dashboards do not show "no data" before first traffic.
	for _, result := range []string{"success", "failed", "rejected", "replay"} {
		m.Danmaku.WithLabelValues(result).Add(0)
		m.GiftOrders.WithLabelValues(result).Add(0)
	}
	m.OutboxPending.Set(0)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
func (m *Metrics) Registry() prometheus.Registerer { return m.registry }

func (m *Metrics) HTTPMiddleware(service string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(sw.status)
		m.HTTPRequests.WithLabelValues(service, r.Method, route, status).Inc()
		m.HTTPDuration.WithLabelValues(service, r.Method, route).Observe(time.Since(started).Seconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (m *Metrics) KafkaProduced(topic, result string, duration time.Duration) {
	m.KafkaProduce.WithLabelValues(topic, result).Inc()
	m.KafkaProduceDur.WithLabelValues(topic).Observe(duration.Seconds())
}
func (m *Metrics) KafkaConsumed(group, topic, result string) {
	m.KafkaConsume.WithLabelValues(group, topic, result).Inc()
}
func (m *Metrics) KafkaLag(group, topic string, partition int32, lag int64) {
	m.KafkaConsumerLag.WithLabelValues(group, topic, strconv.Itoa(int(partition))).Set(float64(lag))
}
func (m *Metrics) KafkaBuffered(client, direction string, records int64) {
	m.KafkaBufferedRecords.WithLabelValues(client, direction).Set(float64(records))
}

func (m *Metrics) RealtimePublished(result string, duration time.Duration) {
	m.RealtimePublish.WithLabelValues(result).Inc()
}

func (m *Metrics) OutboxPendingSet(n int64)      { m.OutboxPending.Set(float64(n)) }
func (m *Metrics) OutboxPublished(result string) { m.OutboxPublish.WithLabelValues(result).Inc() }
func (m *Metrics) OutboxRetried()                { m.OutboxRetries.Inc() }

func (m *Metrics) StatsBroadcasted(result string) { m.StatsBroadcast.WithLabelValues(result).Inc() }
