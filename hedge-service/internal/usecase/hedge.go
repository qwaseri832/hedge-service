package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/yourname/hedge-service/internal/domain"
	"github.com/yourname/hedge-service/internal/metrics"
	"github.com/yourname/hedge-service/internal/platform"
)

// HedgeUseCase — принимает перевод и выставляет хедж-ордер на бирже.
type HedgeUseCase struct {
	transfers domain.TransferRepository
	orders    domain.OrderRepository
	exchange  platform.ExchangeClient
	logger    *slog.Logger
	symbol    string
}

func NewHedgeUseCase(
	transfers domain.TransferRepository,
	orders domain.OrderRepository,
	exchange platform.ExchangeClient,
	logger *slog.Logger,
) *HedgeUseCase {
	return &HedgeUseCase{
		transfers: transfers,
		orders:    orders,
		exchange:  exchange,
		logger:    logger,
		symbol:    "BTCUSDT",
	}
}

// RegisterTransfer — принимает входящий перевод.
// Идемпотентен: повторный вызов с тем же externalID вернёт ErrAlreadyExists.
func (uc *HedgeUseCase) RegisterTransfer(ctx context.Context, req RegisterTransferRequest) (*domain.Transfer, error) {
	t, err := domain.NewTransfer(
		req.ExternalID,
		req.ClientID,
		req.Amount,
		req.Currency,
		req.WalletAddr,
	)
	if err != nil {
		metrics.TransfersTotal.WithLabelValues("invalid").Inc()
		return nil, fmt.Errorf("validate transfer: %w", err)
	}
	t.ID = uuid.New().String()

	if err := uc.transfers.Create(ctx, t); err != nil {
		if err == domain.ErrAlreadyExists {
			metrics.TransfersTotal.WithLabelValues("duplicate").Inc()
		}
		return nil, fmt.Errorf("save transfer: %w", err)
	}

	metrics.TransfersTotal.WithLabelValues("registered").Inc()
	metrics.PendingTransfers.Inc()

	uc.logger.Info("transfer registered",
		slog.String("transfer_id", t.ID),
		slog.String("external_id", t.ExternalID),
		slog.String("amount", t.AmountFiat.String()),
		slog.String("currency", t.Currency),
	)
	return t, nil
}

// ProcessTransfer — выставляет ордер на биржу для одного перевода.
func (uc *HedgeUseCase) ProcessTransfer(ctx context.Context, t *domain.Transfer) error {
	log := uc.logger.With(slog.String("transfer_id", t.ID))

	amountUSD, err := uc.toUSD(ctx, t.AmountFiat, t.Currency)
	if err != nil {
		return fmt.Errorf("convert to usd: %w", err)
	}

	clientOrderID := fmt.Sprintf("hedge-%s", t.ID)

	existingOrder, err := uc.orders.GetByClientOrderID(ctx, clientOrderID)
	if err == nil {
		log.Info("order already exists, skipping", slog.String("order_id", existingOrder.ID))
		metrics.PendingTransfers.Dec()
		return uc.transfers.UpdateStatus(ctx, t.ID, domain.TransferStatusProcessed, t.RetryCount)
	}

	// Замеряем время выставления ордера
	start := time.Now()
	result, err := uc.exchange.PlaceMarketOrder(ctx, clientOrderID, uc.symbol, amountUSD)
	orderDuration := time.Since(start)

	if err != nil {
		t.RetryCount++
		metrics.OrdersTotal.WithLabelValues("failed", uc.symbol).Inc()
		log.Warn("exchange order failed",
			slog.Int("retry_count", t.RetryCount),
			slog.Any("error", err),
		)
		if !t.CanRetry() {
			metrics.TransfersTotal.WithLabelValues("failed").Inc()
			metrics.PendingTransfers.Dec()
			uc.transfers.UpdateStatus(ctx, t.ID, domain.TransferStatusFailed, t.RetryCount) //nolint:errcheck
			return fmt.Errorf("max retries reached: %w", err)
		}
		uc.transfers.UpdateStatus(ctx, t.ID, domain.TransferStatusPending, t.RetryCount) //nolint:errcheck
		return fmt.Errorf("place order: %w", err)
	}

	metrics.OrderExecutionSeconds.Observe(orderDuration.Seconds())

	now := time.Now().UTC()
	order := &domain.Order{
		ID:              uuid.New().String(),
		TransferID:      t.ID,
		ClientOrderID:   clientOrderID,
		ExchangeOrderID: result.ExchangeOrderID,
		Symbol:          result.Symbol,
		AmountFiat:      amountUSD,
		AmountCrypto:    result.AmountCrypto,
		Price:           result.Price,
		Status:          domain.OrderStatusPlaced,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if result.Status == "filled" {
		order.Status = domain.OrderStatusFilled
	}

	notification := uc.buildNotification(t, order)

	if err := uc.orders.CreateWithNotification(ctx, order, notification); err != nil {
		return fmt.Errorf("save order+notification: %w", err)
	}

	metrics.OrdersTotal.WithLabelValues(string(order.Status), order.Symbol).Inc()
	metrics.TransfersTotal.WithLabelValues("processed").Inc()
	metrics.PendingTransfers.Dec()

	if err := uc.transfers.UpdateStatus(ctx, t.ID, domain.TransferStatusProcessed, t.RetryCount); err != nil {
		log.Warn("failed to mark transfer processed", slog.Any("error", err))
	}

	log.Info("hedge order placed",
		slog.String("order_id", order.ID),
		slog.String("exchange_order_id", order.ExchangeOrderID),
		slog.String("amount_crypto", order.AmountCrypto.String()),
		slog.String("price", order.Price.String()),
		slog.Duration("exchange_latency", orderDuration),
	)
	return nil
}

func (uc *HedgeUseCase) toUSD(ctx context.Context, amount decimal.Decimal, currency string) (decimal.Decimal, error) {
	if currency == "USD" {
		return amount, nil
	}
	price, err := uc.exchange.GetPrice(ctx, "USDTRUB")
	if err != nil {
		return decimal.Zero, fmt.Errorf("get USDTRUB price: %w", err)
	}
	if price.IsZero() {
		return decimal.Zero, fmt.Errorf("got zero USDTRUB price")
	}
	return amount.Div(price).Round(2), nil
}

func (uc *HedgeUseCase) buildNotification(t *domain.Transfer, o *domain.Order) *domain.OutboxNotification {
	type payload struct {
		Message      string `json:"message"`
		OrderID      string `json:"order_id"`
		AmountCrypto string `json:"amount_crypto"`
		Symbol       string `json:"symbol"`
		WalletAddr   string `json:"wallet_addr"`
	}

	p := payload{
		Message:      "Ваша заявка принята. Криптовалюта будет отправлена на ваш кошелёк в течение 10 минут.",
		OrderID:      o.ID,
		AmountCrypto: o.AmountCrypto.String(),
		Symbol:       o.Symbol,
		WalletAddr:   t.WalletAddr,
	}

	payloadBytes, _ := json.Marshal(p)
	now := time.Now().UTC()

	return &domain.OutboxNotification{
		ID:         uuid.New().String(),
		TransferID: t.ID,
		OrderID:    o.ID,
		ClientID:   t.ClientID,
		Channel:    domain.NotificationChannelEmail,
		Payload:    string(payloadBytes),
		Status:     domain.NotificationStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

type RegisterTransferRequest struct {
	ExternalID string
	ClientID   string
	Amount     decimal.Decimal
	Currency   string
	WalletAddr string
}
