package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/yourname/hedge-service/internal/domain"
	"github.com/yourname/hedge-service/internal/metrics"
	"github.com/yourname/hedge-service/internal/platform"
)

type NotificationWorker struct {
	notifications domain.NotificationRepository
	sender        platform.NotificationSender
	logger        *slog.Logger
	interval      time.Duration
}

func NewNotificationWorker(
	notifications domain.NotificationRepository,
	sender platform.NotificationSender,
	logger *slog.Logger,
) *NotificationWorker {
	return &NotificationWorker{
		notifications: notifications,
		sender:        sender,
		logger:        logger,
		interval:      1 * time.Second,
	}
}

func (w *NotificationWorker) Run(ctx context.Context) {
	w.logger.Info("notification worker started")
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("notification worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *NotificationWorker) processBatch(ctx context.Context) {
	for {
		n, err := w.notifications.ClaimPending(ctx)
		if errors.Is(err, domain.ErrNotFound) {
			return
		}
		if err != nil {
			w.logger.Error("claim pending notification", slog.Any("error", err))
			return
		}
		if err := w.processOne(ctx, n); err != nil {
			w.logger.Error("send notification",
				slog.String("notification_id", n.ID),
				slog.Any("error", err),
			)
		}
	}
}

func (w *NotificationWorker) processOne(ctx context.Context, n *domain.OutboxNotification) error {
	var p struct {
		WalletAddr string `json:"wallet_addr"`
	}
	json.Unmarshal([]byte(n.Payload), &p) //nolint:errcheck
	recipient := n.ClientID

	err := w.sender.Send(ctx, string(n.Channel), recipient, n.Payload)
	if err != nil {
		n.RetryCount++
		status := domain.NotificationStatusPending
		if n.RetryCount >= domain.MaxNotificationRetries {
			status = domain.NotificationStatusFailed
			metrics.NotificationsTotal.WithLabelValues("failed", string(n.Channel)).Inc()
			w.logger.Error("notification permanently failed",
				slog.String("notification_id", n.ID),
				slog.String("client_id", n.ClientID),
			)
		}
		return w.notifications.UpdateStatus(ctx, n.ID, status, n.RetryCount, nil)
	}

	now := time.Now().UTC()
	metrics.NotificationsTotal.WithLabelValues("sent", string(n.Channel)).Inc()

	if err := w.notifications.UpdateStatus(ctx, n.ID, domain.NotificationStatusSent, n.RetryCount, &now); err != nil {
		w.logger.Warn("update notification status", slog.Any("error", err))
	}

	w.logger.Info("notification delivered",
		slog.String("notification_id", n.ID),
		slog.String("client_id", n.ClientID),
		slog.String("channel", string(n.Channel)),
	)
	return nil
}
