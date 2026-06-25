package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/yourname/hedge-service/internal/domain"
	"github.com/yourname/hedge-service/internal/usecase"
)

// TransferWorker — фоновый воркер, обрабатывает pending переводы.
// Использует SKIP LOCKED — можно запустить несколько горутин без конфликтов.
type TransferWorker struct {
	uc        *usecase.HedgeUseCase
	transfers domain.TransferRepository
	logger    *slog.Logger
	interval  time.Duration
}

func NewTransferWorker(
	uc *usecase.HedgeUseCase,
	transfers domain.TransferRepository,
	logger *slog.Logger,
) *TransferWorker {
	return &TransferWorker{
		uc:        uc,
		transfers: transfers,
		logger:    logger,
		interval:  2 * time.Second,
	}
}

func (w *TransferWorker) Run(ctx context.Context) {
	w.logger.Info("transfer worker started")
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("transfer worker stopped")
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *TransferWorker) processBatch(ctx context.Context) {
	// Обрабатываем по одному — SKIP LOCKED сам обеспечивает параллелизм
	for {
		t, err := w.transfers.ClaimPending(ctx)
		if errors.Is(err, domain.ErrNotFound) {
			return // нет pending переводов
		}
		if err != nil {
			w.logger.Error("claim pending transfer", slog.Any("error", err))
			return
		}

		if err := w.uc.ProcessTransfer(ctx, t); err != nil {
			w.logger.Error("process transfer",
				slog.String("transfer_id", t.ID),
				slog.Any("error", err),
			)
		}
	}
}
