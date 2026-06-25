package domain

import (
	"context"
	"time"
)

// TransferRepository — работа с переводами.
type TransferRepository interface {
	// Create сохраняет перевод. Возвращает ErrAlreadyExists если external_id уже есть.
	Create(ctx context.Context, t *Transfer) error
	GetByID(ctx context.Context, id string) (*Transfer, error)
	GetByExternalID(ctx context.Context, externalID string) (*Transfer, error)
	// ClaimPending берёт один pending перевод для обработки (SKIP LOCKED).
	ClaimPending(ctx context.Context) (*Transfer, error)
	UpdateStatus(ctx context.Context, id string, status TransferStatus, retryCount int) error
}

// OrderRepository — работа с ордерами.
type OrderRepository interface {
	// CreateWithNotification создаёт ордер и outbox-уведомление в одной транзакции.
	CreateWithNotification(ctx context.Context, o *Order, n *OutboxNotification) error
	GetByID(ctx context.Context, id string) (*Order, error)
	GetByClientOrderID(ctx context.Context, clientOrderID string) (*Order, error)
	// ClaimPlaced берёт один placed ордер для проверки статуса на бирже.
	ClaimPlaced(ctx context.Context) (*Order, error)
	Update(ctx context.Context, o *Order) error
}

// NotificationRepository — работа с outbox уведомлениями.
type NotificationRepository interface {
	// ClaimPending берёт одно pending уведомление (SKIP LOCKED).
	ClaimPending(ctx context.Context) (*OutboxNotification, error)
	UpdateStatus(ctx context.Context, id string, status NotificationStatus, retryCount int, sentAt *time.Time) error
}
