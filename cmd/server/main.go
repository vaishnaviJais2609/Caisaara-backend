package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/here-arjun-1/Caisaara-backend/internal/auth"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/database"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/email"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/validation"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/worker"
	"github.com/here-arjun-1/Caisaara-backend/internal/community"
	"github.com/here-arjun-1/Caisaara-backend/internal/config"
	"github.com/here-arjun-1/Caisaara-backend/internal/player"
	"github.com/here-arjun-1/Caisaara-backend/internal/router"
	"github.com/here-arjun-1/Caisaara-backend/migrations"
	"github.com/hibiken/asynq"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := migrations.Run(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	conn, err := database.ConnectDB(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}
	defer conn.Close()

	redisClient, err := database.ConnectRedis(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("redis connection failed: %w", err)
	}
	defer func() { _ = redisClient.Close() }()

	asynqRedisOpt := asynq.RedisClientOpt{Addr: cfg.RedisURL}

	taskDistributor := asynq.NewClient(asynqRedisOpt)
	defer func() { _ = taskDistributor.Close() }()

	if err := validation.Register(); err != nil {
		return fmt.Errorf("register validators: %w", err)
	}

	authModule := auth.NewModule(conn, redisClient, taskDistributor, cfg.JWTSecret)
	playerModule := player.NewModule(conn)
	communityModule := community.NewModule(conn)

	r, err := router.New(cfg.JWTSecret, authModule, playerModule, communityModule)
	if err != nil {
		return fmt.Errorf("setup router: %w", err)
	}

	asynqServer := worker.StartEmailServer(asynqRedisOpt, email.NewSender(cfg.SMTP))
	worker.StartSessionCleanup(authModule.SessionRepository)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("server started", "port", cfg.Port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	if asynqServer != nil {
		asynqServer.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	slog.Info("server exited")
	return nil
}
