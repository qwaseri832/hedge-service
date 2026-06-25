package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// TransfersTotal — счётчик входящих переводов по статусу
	TransfersTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hedge_transfers_total",
		Help: "Total number of incoming transfers by status",
	}, []string{"status"})

	// OrdersTotal — счётчик ордеров по статусу
	OrdersTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hedge_orders_total",
		Help: "Total number of exchange orders by status",
	}, []string{"status", "symbol"})

	// OrderExecutionSeconds — гистограмма времени выставления ордера
	OrderExecutionSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "hedge_order_execution_seconds",
		Help:    "Time to place and confirm an order on exchange",
		Buckets: prometheus.DefBuckets,
	})

	// NotificationsTotal — счётчик уведомлений
	NotificationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hedge_notifications_total",
		Help: "Total outbox notifications by status and channel",
	}, []string{"status", "channel"})

	// WebhookRequestsTotal — счётчик входящих webhook запросов
	WebhookRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hedge_webhook_requests_total",
		Help: "Total webhook requests by result",
	}, []string{"result"})

	// HTTPRequestDuration — латентность HTTP эндпоинтов
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "hedge_http_request_duration_seconds",
		Help:    "HTTP request duration by endpoint",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.5, 1.0},
	}, []string{"method", "path", "status"})

	// PendingTransfers — gauge текущего числа необработанных переводов
	PendingTransfers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hedge_pending_transfers",
		Help: "Current number of pending transfers in queue",
	})
)
