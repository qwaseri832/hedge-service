package domain

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// --- Errors ---

var (
	ErrNotFound          = errors.New("not found")
	ErrAlreadyExists     = errors.New("already exists")
	ErrInvalidAmount     = errors.New("amount must be positive")
	ErrInvalidCurrency   = errors.New("unsupported currency")
	ErrMaxRetriesReached = errors.New("max retries reached")
)

// --- Transfer ---

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "pending"
	TransferStatusProcessed TransferStatus = "processed"
	TransferStatusFailed    TransferStatus = "failed"
)

// Transfer — входящий перевод от клиента.
type Transfer struct {
	ID          string
	ExternalID  string // идемпотентный ключ от платёжной системы
	ClientID    string
	AmountFiat  decimal.Decimal // сумма в фиате (USD/RUB)
	Currency    string          // "USD", "RUB"
	WalletAddr  string          // крипто-кошелёк клиента для отправки
	Status      TransferStatus
	RetryCount  int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewTransfer(externalID, clientID string, amount decimal.Decimal, currency, walletAddr string) (*Transfer, error) {
	if amount.IsNegative() || amount.IsZero() {
		return nil, ErrInvalidAmount
	}
	if currency != "USD" && currency != "RUB" {
		return nil, ErrInvalidCurrency
	}
	now := time.Now().UTC()
	return &Transfer{
		ExternalID: externalID,
		ClientID:   clientID,
		AmountFiat: amount,
		Currency:   currency,
		WalletAddr: walletAddr,
		Status:     TransferStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

const MaxTransferRetries = 3

func (t *Transfer) CanRetry() bool {
	return t.RetryCount < MaxTransferRetries
}

// --- Order ---

type OrderStatus string

const (
	OrderStatusPending         OrderStatus = "pending"
	OrderStatusPlaced          OrderStatus = "placed"           // отправлен на биржу
	OrderStatusPartiallyFilled OrderStatus = "partially_filled" // частичное исполнение
	OrderStatusFilled          OrderStatus = "filled"           // полностью исполнен
	OrderStatusFailed          OrderStatus = "failed"
	OrderStatusCancelled       OrderStatus = "cancelled"
)

// Order — ордер на покупку крипты на бирже.
type Order struct {
	ID              string
	TransferID      string
	ClientOrderID   string          // наш идемпотентный ключ для биржи
	ExchangeOrderID string          // ID от биржи (после размещения)
	Symbol          string          // "BTCUSDT"
	AmountFiat      decimal.Decimal // сколько потратить
	AmountCrypto    decimal.Decimal // сколько купили (заполняется после исполнения)
	Price           decimal.Decimal // цена исполнения
	Status          OrderStatus
	RetryCount      int
	FailReason      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const MaxOrderRetries = 3

func (o *Order) CanRetry() bool {
	return o.RetryCount < MaxOrderRetries
}

// --- Notification (Outbox) ---

type NotificationStatus string

const (
	NotificationStatusPending   NotificationStatus = "pending"
	NotificationStatusSent      NotificationStatus = "sent"
	NotificationStatusFailed    NotificationStatus = "failed"
)

type NotificationChannel string

const (
	NotificationChannelEmail   NotificationChannel = "email"
	NotificationChannelWebhook NotificationChannel = "webhook"
)

// OutboxNotification — событие в Outbox таблице.
// Создаётся атомарно с ордером в одной транзакции.
type OutboxNotification struct {
	ID         string
	TransferID string
	OrderID    string
	ClientID   string
	Channel    NotificationChannel
	Payload    string // JSON с текстом уведомления
	Status     NotificationStatus
	RetryCount int
	SentAt     *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const MaxNotificationRetries = 5
