package platform

import (
	"context"
	"log/slog"
)

// NotificationSender — интерфейс отправки уведомлений клиенту.
type NotificationSender interface {
	Send(ctx context.Context, channel, recipient, payload string) error
}

// MockNotificationSender — логирует в stdout, в продакшене заменяется на SMTP/webhook.
type MockNotificationSender struct {
	logger *slog.Logger
}

func NewMockNotificationSender(logger *slog.Logger) NotificationSender {
	return &MockNotificationSender{logger: logger}
}

func (s *MockNotificationSender) Send(_ context.Context, channel, recipient, payload string) error {
	s.logger.Info("📨 notification sent",
		slog.String("channel", channel),
		slog.String("recipient", recipient),
		slog.String("payload", payload),
	)
	// В продакшене здесь:
	// - email: SMTP через gomail или AWS SES
	// - webhook: HTTP POST на URL клиента с HMAC подписью
	return nil
}
