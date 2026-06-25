package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/yourname/hedge-service/internal/domain"
)

func TestNewTransfer_Valid(t *testing.T) {
	amount := decimal.NewFromFloat(1000.50)
	tr, err := domain.NewTransfer("ext-001", "client-1", amount, "USD", "bc1qxy2k...")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tr.Status != domain.TransferStatusPending {
		t.Errorf("expected pending, got %s", tr.Status)
	}
	if !tr.AmountFiat.Equal(amount) {
		t.Errorf("amount mismatch: %s != %s", tr.AmountFiat, amount)
	}
}

func TestNewTransfer_ZeroAmount(t *testing.T) {
	_, err := domain.NewTransfer("ext-002", "client-1", decimal.Zero, "USD", "bc1q...")
	if err != domain.ErrInvalidAmount {
		t.Errorf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestNewTransfer_NegativeAmount(t *testing.T) {
	_, err := domain.NewTransfer("ext-003", "client-1", decimal.NewFromFloat(-100), "USD", "bc1q...")
	if err != domain.ErrInvalidAmount {
		t.Errorf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestNewTransfer_InvalidCurrency(t *testing.T) {
	_, err := domain.NewTransfer("ext-004", "client-1", decimal.NewFromFloat(100), "EUR", "bc1q...")
	if err != domain.ErrInvalidCurrency {
		t.Errorf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestTransfer_CanRetry(t *testing.T) {
	tr, _ := domain.NewTransfer("ext-005", "client-1", decimal.NewFromFloat(100), "USD", "bc1q...")
	tr.ID = "test-id"

	if !tr.CanRetry() {
		t.Error("fresh transfer should be retryable")
	}
	tr.RetryCount = domain.MaxTransferRetries
	if tr.CanRetry() {
		t.Error("transfer at max retries should not be retryable")
	}
}

func TestOrder_CanRetry(t *testing.T) {
	o := &domain.Order{RetryCount: 0}
	if !o.CanRetry() {
		t.Error("fresh order should be retryable")
	}
	o.RetryCount = domain.MaxOrderRetries
	if o.CanRetry() {
		t.Error("order at max retries should not be retryable")
	}
}

func TestDecimalPrecision(t *testing.T) {
	// Финансовая точность — критично не терять копейки
	a := decimal.NewFromFloat(0.1)
	b := decimal.NewFromFloat(0.2)
	sum := a.Add(b)

	expected := decimal.NewFromFloat(0.3)
	if !sum.Equal(expected) {
		t.Errorf("decimal precision failed: %s != %s", sum, expected)
	}

	// float64 бы провалил этот тест
	if 0.1+0.2 == 0.3 {
		t.Error("float64 should not pass this — if it does, something is wrong with the test")
	}
}
