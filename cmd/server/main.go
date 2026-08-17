package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vultisig/card-backend/internal/config"
	"github.com/vultisig/card-backend/internal/db"
	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/service"
)

// Above the REAP client's request timeout, so an in-flight proxy call can finish.
const shutdownTimeout = 20 * time.Second

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatal("config: JWT_SECRET is required")
	}
	if cfg.ReapAPIKey == "" {
		log.Fatal("config: REAP_API_KEY is required")
	}
	if cfg.ReapWebhookSecret == "" {
		log.Fatal("config: REAP_WEBHOOK_SECRET is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Fatalf("db migrate: %v", err)
	}

	authService := service.NewAuthService(cfg.JWTSecret, pool)
	reapClient := reap.NewClient(cfg.ReapEnv, cfg.ReapAPIKey)
	userService := service.NewUserService(pool, reapClient)
	accountService := service.NewAccountService(pool, reapClient)
	cardService := service.NewCardService(pool, reapClient)
	cardShipmentService := service.NewCardShipmentService(pool, reapClient)
	cardDesignService := service.NewCardDesignService(reapClient)
	cardTransactionService := service.NewCardTransactionService(pool, reapClient)
	activityService := service.NewActivityService(pool, reapClient)
	fraudAlertService := service.NewFraudAlertService(pool, reapClient)
	simulationService := service.NewSimulationService(pool, reapClient)
	webhookService := service.NewWebhookService(pool, cfg.ReapWebhookSecret)
	srv := NewServer(pool, authService, userService, accountService, cardService, cardShipmentService, cardDesignService, cardTransactionService, activityService, fraudAlertService, simulationService, webhookService, cfg.ReapEnv)

	log.Printf("card-backend starting on port %s", cfg.Port)

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Start(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// Let in-flight requests finish: killing one mid-flight can leave a card or
	// account created in REAP with no local ownership row (issue #21).
	select {
	case err := <-serverErr:
		if err != nil {
			log.Fatalf("server: %v", err)
		}
	case <-ctx.Done():
		stop()
		log.Print("card-backend shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		if err := <-serverErr; err != nil {
			log.Printf("server: %v", err)
		}
	}
}
