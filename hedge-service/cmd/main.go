package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourname/hedge-service/config"
	"github.com/yourname/hedge-service/internal/handler"
	"github.com/yourname/hedge-service/internal/platform"
	"github.com/yourname/hedge-service/internal/repository"
	"github.com/yourname/hedge-service/internal/usecase"
	"github.com/yourname/hedge-service/internal/worker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", slog.Any("error", err))
		os.Exit(1)
	}

	// --- PostgreSQL ---
	ctx := context.Background()
	dbpool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to postgres", slog.Any("error", err))
		os.Exit(1)
	}
	defer dbpool.Close()

	if err := dbpool.Ping(ctx); err != nil {
		logger.Error("postgres ping", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("connected to postgres")

	// --- Репозитории ---
	transferRepo := repository.NewTransferRepo(dbpool)
	orderRepo := repository.NewOrderRepo(dbpool)
	notificationRepo := repository.NewNotificationRepo(dbpool)

	// --- Платформенные зависимости ---
	exchange := platform.NewMockExchangeClient(cfg.ExchangeFailRate)
	notificationSender := platform.NewMockNotificationSender(logger)

	// --- UseCase ---
	hedgeUC := usecase.NewHedgeUseCase(transferRepo, orderRepo, exchange, logger)

	// --- HTTP хендлер ---
	h := handler.New(hedgeUC, transferRepo, orderRepo, logger, cfg.WebhookSecret)
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      h.Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Graceful shutdown контекст ---
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// --- Воркеры ---
	transferWorker := worker.NewTransferWorker(hedgeUC, transferRepo, logger)
	notificationWorker := worker.NewNotificationWorker(notificationRepo, notificationSender, logger)

	for i := 0; i < cfg.TransferWorkerCount; i++ {
		go transferWorker.Run(runCtx)
	}
	for i := 0; i < cfg.NotificationWorkerCount; i++ {
		go notificationWorker.Run(runCtx)
	}

	// --- HTTP сервер ---
	go func() {
		logger.Info("http server started", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", slog.Any("error", err))
		}
	}()

	// --- Ждём сигнала ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down...")
	cancel() // останавливаем воркеры

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", slog.Any("error", err))
	}

	logger.Info("stopped")
}
