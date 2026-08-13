package main

import (
	"context"
	"log"

	"github.com/vultisig/card-backend/internal/config"
	"github.com/vultisig/card-backend/internal/db"
	"github.com/vultisig/card-backend/internal/reap"
	"github.com/vultisig/card-backend/internal/service"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		log.Fatal("config: JWT_SECRET is required")
	}
	if cfg.ReapAPIKey == "" {
		log.Fatal("config: REAP_API_KEY is required")
	}

	ctx := context.Background()

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
	srv := NewServer(pool, authService, userService, accountService)

	log.Printf("card-backend starting on port %s", cfg.Port)
	log.Fatal(srv.Start(":" + cfg.Port))
}
