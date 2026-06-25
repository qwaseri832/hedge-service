package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/yourname/hedge-service/internal/domain"
)

// --- Transfer Repository ---

type transferRepo struct {
	db *pgxpool.Pool
}

func NewTransferRepo(db *pgxpool.Pool) domain.TransferRepository {
	return &transferRepo{db: db}
}

func (r *transferRepo) Create(ctx context.Context, t *domain.Transfer) error {
	query := `
		INSERT INTO transfers
			(id, external_id, client_id, amount_fiat, currency, wallet_addr, status, retry_count, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (external_id) DO NOTHING
	`
	res, err := r.db.Exec(ctx, query,
		t.ID, t.ExternalID, t.ClientID,
		t.AmountFiat.String(), t.Currency, t.WalletAddr,
		t.Status, t.RetryCount, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert transfer: %w", err)
	}
	if res.RowsAffected() == 0 {
		return domain.ErrAlreadyExists
	}
	return nil
}

func (r *transferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	query := `
		SELECT id, external_id, client_id, amount_fiat, currency, wallet_addr,
		       status, retry_count, created_at, updated_at
		FROM transfers WHERE id = $1
	`
	row := r.db.QueryRow(ctx, query, id)
	return scanTransfer(row)
}

func (r *transferRepo) GetByExternalID(ctx context.Context, externalID string) (*domain.Transfer, error) {
	query := `
		SELECT id, external_id, client_id, amount_fiat, currency, wallet_addr,
		       status, retry_count, created_at, updated_at
		FROM transfers WHERE external_id = $1
	`
	row := r.db.QueryRow(ctx, query, externalID)
	return scanTransfer(row)
}

// ClaimPending использует SKIP LOCKED — несколько воркеров не возьмут одну запись.
func (r *transferRepo) ClaimPending(ctx context.Context) (*domain.Transfer, error) {
	query := `
		SELECT id, external_id, client_id, amount_fiat, currency, wallet_addr,
		       status, retry_count, created_at, updated_at
		FROM transfers
		WHERE status = 'pending'
		  AND retry_count < $1
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`
	// Оборачиваем в транзакцию — FOR UPDATE требует активной транзакции.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	row := tx.QueryRow(ctx, query, domain.MaxTransferRetries)
	t, err := scanTransfer(row)
	if err != nil {
		return nil, err
	}

	// Помечаем что взяли в обработку — чтобы не было двойного claim
	_, err = tx.Exec(ctx,
		`UPDATE transfers SET status = 'processing', updated_at = $1 WHERE id = $2`,
		time.Now().UTC(), t.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("mark processing: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return t, nil
}

func (r *transferRepo) UpdateStatus(ctx context.Context, id string, status domain.TransferStatus, retryCount int) error {
	_, err := r.db.Exec(ctx,
		`UPDATE transfers SET status = $1, retry_count = $2, updated_at = $3 WHERE id = $4`,
		status, retryCount, time.Now().UTC(), id,
	)
	return err
}

func scanTransfer(row pgx.Row) (*domain.Transfer, error) {
	var t domain.Transfer
	var amountStr string
	err := row.Scan(
		&t.ID, &t.ExternalID, &t.ClientID,
		&amountStr, &t.Currency, &t.WalletAddr,
		&t.Status, &t.RetryCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	t.AmountFiat, err = decimal.NewFromString(amountStr)
	if err != nil {
		return nil, fmt.Errorf("parse amount: %w", err)
	}
	return &t, nil
}

// --- Order Repository ---

type orderRepo struct {
	db *pgxpool.Pool
}

func NewOrderRepo(db *pgxpool.Pool) domain.OrderRepository {
	return &orderRepo{db: db}
}

// CreateWithNotification — сердце Outbox Pattern.
// Ордер и уведомление создаются в одной транзакции атомарно.
func (r *orderRepo) CreateWithNotification(ctx context.Context, o *domain.Order, n *domain.OutboxNotification) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	_, err = tx.Exec(ctx, `
		INSERT INTO orders
			(id, transfer_id, client_order_id, exchange_order_id, symbol,
			 amount_fiat, amount_crypto, price, status, retry_count, fail_reason, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		o.ID, o.TransferID, o.ClientOrderID, o.ExchangeOrderID, o.Symbol,
		o.AmountFiat.String(), o.AmountCrypto.String(), o.Price.String(),
		o.Status, o.RetryCount, o.FailReason, o.CreatedAt, o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_notifications
			(id, transfer_id, order_id, client_id, channel, payload, status, retry_count, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		n.ID, n.TransferID, n.OrderID, n.ClientID, n.Channel,
		n.Payload, n.Status, n.RetryCount, n.CreatedAt, n.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert outbox notification: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *orderRepo) GetByID(ctx context.Context, id string) (*domain.Order, error) {
	query := `
		SELECT id, transfer_id, client_order_id, exchange_order_id, symbol,
		       amount_fiat, amount_crypto, price, status, retry_count, fail_reason, created_at, updated_at
		FROM orders WHERE id = $1
	`
	row := r.db.QueryRow(ctx, query, id)
	return scanOrder(row)
}

func (r *orderRepo) GetByClientOrderID(ctx context.Context, clientOrderID string) (*domain.Order, error) {
	query := `
		SELECT id, transfer_id, client_order_id, exchange_order_id, symbol,
		       amount_fiat, amount_crypto, price, status, retry_count, fail_reason, created_at, updated_at
		FROM orders WHERE client_order_id = $1
	`
	row := r.db.QueryRow(ctx, query, clientOrderID)
	return scanOrder(row)
}

func (r *orderRepo) ClaimPlaced(ctx context.Context) (*domain.Order, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	query := `
		SELECT id, transfer_id, client_order_id, exchange_order_id, symbol,
		       amount_fiat, amount_crypto, price, status, retry_count, fail_reason, created_at, updated_at
		FROM orders
		WHERE status IN ('placed', 'partially_filled')
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`
	row := tx.QueryRow(ctx, query)
	o, err := scanOrder(row)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return o, nil
}

func (r *orderRepo) Update(ctx context.Context, o *domain.Order) error {
	o.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(ctx, `
		UPDATE orders
		SET exchange_order_id = $1,
		    amount_crypto      = $2,
		    price              = $3,
		    status             = $4,
		    retry_count        = $5,
		    fail_reason        = $6,
		    updated_at         = $7
		WHERE id = $8
	`,
		o.ExchangeOrderID, o.AmountCrypto.String(), o.Price.String(),
		o.Status, o.RetryCount, o.FailReason, o.UpdatedAt, o.ID,
	)
	return err
}

func scanOrder(row pgx.Row) (*domain.Order, error) {
	var o domain.Order
	var amountFiatStr, amountCryptoStr, priceStr string
	err := row.Scan(
		&o.ID, &o.TransferID, &o.ClientOrderID, &o.ExchangeOrderID, &o.Symbol,
		&amountFiatStr, &amountCryptoStr, &priceStr,
		&o.Status, &o.RetryCount, &o.FailReason, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan order: %w", err)
	}
	if o.AmountFiat, err = decimal.NewFromString(amountFiatStr); err != nil {
		return nil, fmt.Errorf("parse amount_fiat: %w", err)
	}
	if o.AmountCrypto, err = decimal.NewFromString(amountCryptoStr); err != nil {
		return nil, fmt.Errorf("parse amount_crypto: %w", err)
	}
	if o.Price, err = decimal.NewFromString(priceStr); err != nil {
		return nil, fmt.Errorf("parse price: %w", err)
	}
	return &o, nil
}

// --- Notification Repository ---

type notificationRepo struct {
	db *pgxpool.Pool
}

func NewNotificationRepo(db *pgxpool.Pool) domain.NotificationRepository {
	return &notificationRepo{db: db}
}

func (r *notificationRepo) ClaimPending(ctx context.Context) (*domain.OutboxNotification, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	query := `
		SELECT id, transfer_id, order_id, client_id, channel, payload,
		       status, retry_count, sent_at, created_at, updated_at
		FROM outbox_notifications
		WHERE status = 'pending'
		  AND retry_count < $1
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`
	row := tx.QueryRow(ctx, query, domain.MaxNotificationRetries)

	var n domain.OutboxNotification
	err = row.Scan(
		&n.ID, &n.TransferID, &n.OrderID, &n.ClientID, &n.Channel,
		&n.Payload, &n.Status, &n.RetryCount, &n.SentAt, &n.CreatedAt, &n.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan notification: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &n, nil
}

func (r *notificationRepo) UpdateStatus(ctx context.Context, id string, status domain.NotificationStatus, retryCount int, sentAt *time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE outbox_notifications SET status = $1, retry_count = $2, sent_at = $3, updated_at = $4 WHERE id = $5`,
		status, retryCount, sentAt, time.Now().UTC(), id,
	)
	return err
}
