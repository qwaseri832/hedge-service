package integration_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	_ "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/yourname/hedge-service/internal/domain"
	"github.com/yourname/hedge-service/internal/platform"
	"github.com/yourname/hedge-service/internal/repository"
	"github.com/yourname/hedge-service/internal/usecase"
)

// setupDB поднимает реальный Postgres в Docker и накатывает миграции.
func setupDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("hedge_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.WithInitScripts("../../migrations/001_init.up.sql"),
		tcpostgres.BasicWaitStrategies(),
		// Дополнительно ждём готовности
		tcpostgres.WithWaitStrategyAndDeadline(
			30*time.Second,
			wait.ForLog("database system is ready to accept connections"),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}

	t.Cleanup(func() { pool.Close() })

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	return pool
}

// TestRegisterTransfer_Idempotency проверяет что два одинаковых запроса
// создают только один перевод (idempotency key).
func TestRegisterTransfer_Idempotency(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	transferRepo := repository.NewTransferRepo(pool)
	orderRepo := repository.NewOrderRepo(pool)
	exchange := platform.NewMockExchangeClient(0) // без ошибок
	uc := usecase.NewHedgeUseCase(transferRepo, orderRepo, exchange, logger)

	req := usecase.RegisterTransferRequest{
		ExternalID: "pay-idempotency-test",
		ClientID:   "client-1",
		Amount:     decimal.NewFromFloat(100),
		Currency:   "USD",
		WalletAddr: "bc1qtest",
	}

	// Первый вызов — должен создать перевод
	t1, err := uc.RegisterTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	if t1.ID == "" {
		t.Error("expected non-empty transfer ID")
	}

	// Второй вызов с тем же external_id — должен вернуть ErrAlreadyExists
	_, err = uc.RegisterTransfer(ctx, req)
	if err == nil {
		t.Fatal("expected error on duplicate, got nil")
	}
	if !isAlreadyExists(err) {
		t.Fatalf("expected ErrAlreadyExists, got: %v", err)
	}

	// В БД должна быть только одна запись
	fetched, err := transferRepo.GetByExternalID(ctx, req.ExternalID)
	if err != nil {
		t.Fatalf("get by external id: %v", err)
	}
	if fetched.ID != t1.ID {
		t.Errorf("IDs differ: %s != %s", fetched.ID, t1.ID)
	}
}

// TestProcessTransfer_FullFlow проверяет полный флоу:
// перевод → ордер → outbox уведомление.
func TestProcessTransfer_FullFlow(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	transferRepo := repository.NewTransferRepo(pool)
	orderRepo := repository.NewOrderRepo(pool)
	notificationRepo := repository.NewNotificationRepo(pool)
	exchange := platform.NewMockExchangeClient(0) // без ошибок
	uc := usecase.NewHedgeUseCase(transferRepo, orderRepo, exchange, logger)

	// Регистрируем перевод
	t1, err := uc.RegisterTransfer(ctx, usecase.RegisterTransferRequest{
		ExternalID: "pay-fullflow-test",
		ClientID:   "client-2",
		Amount:     decimal.NewFromFloat(500),
		Currency:   "USD",
		WalletAddr: "bc1qfullflow",
	})
	if err != nil {
		t.Fatalf("register transfer: %v", err)
	}

	// Берём из очереди и обрабатываем
	pending, err := transferRepo.ClaimPending(ctx)
	if err != nil {
		t.Fatalf("claim pending: %v", err)
	}
	if pending.ID != t1.ID {
		t.Errorf("claimed wrong transfer: %s != %s", pending.ID, t1.ID)
	}

	if err := uc.ProcessTransfer(ctx, pending); err != nil {
		t.Fatalf("process transfer: %v", err)
	}

	// Перевод должен стать processed
	updated, err := transferRepo.GetByID(ctx, t1.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if updated.Status != domain.TransferStatusProcessed {
		t.Errorf("expected processed, got %s", updated.Status)
	}

	// Ордер должен быть создан
	order, err := orderRepo.GetByClientOrderID(ctx, "hedge-"+t1.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.AmountCrypto.IsZero() {
		t.Error("expected non-zero crypto amount")
	}
	if order.Price.IsZero() {
		t.Error("expected non-zero price")
	}

	// Outbox уведомление должно быть pending
	notification, err := notificationRepo.ClaimPending(ctx)
	if err != nil {
		t.Fatalf("claim notification: %v", err)
	}
	if notification.TransferID != t1.ID {
		t.Errorf("wrong transfer_id in notification: %s", notification.TransferID)
	}
	if notification.Status != domain.NotificationStatusPending {
		t.Errorf("expected pending notification, got %s", notification.Status)
	}
}

// TestProcessTransfer_RetryOnExchangeError проверяет retry логику.
func TestProcessTransfer_RetryOnExchangeError(t *testing.T) {
	pool := setupDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	transferRepo := repository.NewTransferRepo(pool)
	orderRepo := repository.NewOrderRepo(pool)
	// Биржа всегда падает
	exchange := platform.NewMockExchangeClient(1.0)
	uc := usecase.NewHedgeUseCase(transferRepo, orderRepo, exchange, logger)

	t1, err := uc.RegisterTransfer(ctx, usecase.RegisterTransferRequest{
		ExternalID: "pay-retry-test",
		ClientID:   "client-3",
		Amount:     decimal.NewFromFloat(100),
		Currency:   "USD",
		WalletAddr: "bc1qretry",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Исчерпываем все попытки
	for i := 0; i < domain.MaxTransferRetries; i++ {
		pending, err := transferRepo.ClaimPending(ctx)
		if err != nil {
			t.Fatalf("claim attempt %d: %v", i, err)
		}
		uc.ProcessTransfer(ctx, pending) //nolint:errcheck
	}

	// После MaxRetries перевод должен быть failed
	final, err := transferRepo.GetByID(ctx, t1.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if final.Status != domain.TransferStatusFailed {
		t.Errorf("expected failed after max retries, got %s", final.Status)
	}
	if final.RetryCount != domain.MaxTransferRetries {
		t.Errorf("expected retry_count=%d, got %d", domain.MaxTransferRetries, final.RetryCount)
	}
}

func isAlreadyExists(err error) bool {
	return err != nil && (err == domain.ErrAlreadyExists ||
		containsStr(err.Error(), "already exists"))
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && findStr(s, substr))
}

func findStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
