package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"

	"github.com/yourname/hedge-service/internal/domain"
	"github.com/yourname/hedge-service/internal/metrics"
	"github.com/yourname/hedge-service/internal/usecase"
)

type Handler struct {
	uc            *usecase.HedgeUseCase
	transfers     domain.TransferRepository
	orders        domain.OrderRepository
	logger        *slog.Logger
	webhookSecret string
}

func New(
	uc *usecase.HedgeUseCase,
	transfers domain.TransferRepository,
	orders domain.OrderRepository,
	logger *slog.Logger,
	webhookSecret string,
) *Handler {
	return &Handler{
		uc:            uc,
		transfers:     transfers,
		orders:        orders,
		logger:        logger,
		webhookSecret: webhookSecret,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook/transfer", h.handleTransferWebhook)
	mux.HandleFunc("GET /transfers/{id}", h.handleGetTransfer)
	mux.HandleFunc("GET /orders/{id}", h.handleGetOrder)
	mux.HandleFunc("GET /health", h.handleHealth)
	// Prometheus метрики — стандартный эндпоинт который скрейпит Prometheus
	mux.Handle("GET /metrics", promhttp.Handler())

	return h.metricsMiddleware(mux)
}

// metricsMiddleware — замеряет латентность и считает запросы по каждому эндпоинту.
func (h *Handler) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)

		// Не считаем /metrics в метриках — избегаем рекурсии
		if r.URL.Path != "/metrics" {
			metrics.HTTPRequestDuration.WithLabelValues(
				r.Method,
				r.URL.Path,
				strconv.Itoa(rw.statusCode),
			).Observe(duration.Seconds())
		}
	})
}

// responseWriter — обёртка чтобы перехватить статус код ответа.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (h *Handler) handleTransferWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		metrics.WebhookRequestsTotal.WithLabelValues("read_error").Inc()
		h.writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	if h.webhookSecret != "" {
		sig := r.Header.Get("X-Signature")
		if !h.verifyHMAC(body, sig) {
			metrics.WebhookRequestsTotal.WithLabelValues("invalid_signature").Inc()
			h.writeError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	var req struct {
		ExternalID string `json:"external_id"`
		ClientID   string `json:"client_id"`
		Amount     string `json:"amount"`
		Currency   string `json:"currency"`
		WalletAddr string `json:"wallet_addr"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		metrics.WebhookRequestsTotal.WithLabelValues("invalid_json").Inc()
		h.writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		metrics.WebhookRequestsTotal.WithLabelValues("invalid_amount").Inc()
		h.writeError(w, http.StatusBadRequest, "invalid amount")
		return
	}

	t, err := h.uc.RegisterTransfer(r.Context(), usecase.RegisterTransferRequest{
		ExternalID: req.ExternalID,
		ClientID:   req.ClientID,
		Amount:     amount,
		Currency:   req.Currency,
		WalletAddr: req.WalletAddr,
	})
	if errors.Is(err, domain.ErrAlreadyExists) {
		metrics.WebhookRequestsTotal.WithLabelValues("duplicate").Inc()
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "already_registered"})
		return
	}
	if err != nil {
		metrics.WebhookRequestsTotal.WithLabelValues("error").Inc()
		h.logger.Error("register transfer", slog.Any("error", err))
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	metrics.WebhookRequestsTotal.WithLabelValues("accepted").Inc()
	h.writeJSON(w, http.StatusAccepted, map[string]string{
		"transfer_id": t.ID,
		"status":      string(t.Status),
	})
}

func (h *Handler) handleGetTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := h.transfers.GetByID(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		h.writeError(w, http.StatusNotFound, "transfer not found")
		return
	}
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.writeJSON(w, http.StatusOK, t)
}

func (h *Handler) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	o, err := h.orders.GetByID(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		h.writeError(w, http.StatusNotFound, "order not found")
		return
	}
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.writeJSON(w, http.StatusOK, o)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) verifyHMAC(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func (h *Handler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, map[string]string{"error": msg})
}
