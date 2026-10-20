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

	"github.com/here-arjun-1/Caisaara-backend/internal/analysis"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/database"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/email"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/validation"
	"github.com/here-arjun-1/Caisaara-backend/internal/auth/worker"
	"github.com/here-arjun-1/Caisaara-backend/internal/bot"
	"github.com/here-arjun-1/Caisaara-backend/internal/chat"
	"github.com/here-arjun-1/Caisaara-backend/internal/club"
	"github.com/here-arjun-1/Caisaara-backend/internal/community"
	"github.com/here-arjun-1/Caisaara-backend/internal/config"
	"github.com/here-arjun-1/Caisaara-backend/internal/friends"
	"github.com/here-arjun-1/Caisaara-backend/internal/game"
	"github.com/here-arjun-1/Caisaara-backend/internal/invitation"
	"github.com/here-arjun-1/Caisaara-backend/internal/matchmaking"
	"github.com/here-arjun-1/Caisaara-backend/internal/nearby"
	"github.com/here-arjun-1/Caisaara-backend/internal/player"
	"github.com/here-arjun-1/Caisaara-backend/internal/puzzle"
	"github.com/here-arjun-1/Caisaara-backend/internal/rating"
	"github.com/here-arjun-1/Caisaara-backend/internal/router"
	"github.com/here-arjun-1/Caisaara-backend/internal/storage"
	"github.com/here-arjun-1/Caisaara-backend/internal/tilt"
	"github.com/here-arjun-1/Caisaara-backend/internal/tournament"
	"github.com/here-arjun-1/Caisaara-backend/internal/websocket"
	"github.com/here-arjun-1/Caisaara-backend/migrations"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

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

	avatarStorage, err := storage.NewS3Storage(context.Background(), cfg.S3)
	if err != nil {
		return fmt.Errorf("setup s3 storage: %w", err)
	}

	authModule := auth.NewModule(conn, redisClient, taskDistributor, cfg.JWTSecret)
	playerModule := player.NewModule(conn, avatarStorage)
	communityModule := community.NewModule(conn)

	gameRepository := game.NewRepository(conn)
	gameService := game.NewService(gameRepository, redisClient)
	gameHandler := game.NewHandler(gameService)

	analysisRepo := analysis.NewRepository(conn)
	analysisEngine := analysis.NewAnalysisEngine(cfg.StockfishPath)
	defer analysisEngine.Close()
	analysisService := analysis.NewService(analysisRepo, gameRepository, taskDistributor)
	analysisHandler := analysis.NewHandler(analysisService)
	analysisProcessor := analysis.NewAnalysisProcessor(analysisEngine, analysisRepo, gameRepository)

	botRepository := bot.NewRepository(conn)
	botEngine := bot.NewEngine(cfg.StockfishPath)
	defer botEngine.Close()
	botService := bot.NewService(botRepository, botEngine)
	botHandler := bot.NewHandler(botService)

	inviteRepo := invitation.NewRedisInviteRepository(redisClient)
	invitationService := invitation.NewService(
		inviteRepo,
		gameRepository,
		authModule.UserRepository,
	)
	invitationHandler := invitation.NewHandler(invitationService)

	tiltRepository := tilt.NewRepository(conn)
	tiltService := tilt.NewService(tiltRepository)
	tiltHandler := tilt.NewHandler(tiltService)

	ratingRepository := rating.NewRepository(conn)
	ratingService := rating.NewService(ratingRepository)

	queueRepo := matchmaking.NewRedisQueueRepository(redisClient)
	matchmakingService := matchmaking.NewService(queueRepo, ratingRepository, tiltService)
	matchmakingHandler := matchmaking.NewHandler(matchmakingService)
	matcher := matchmaking.NewMatcher(queueRepo, gameRepository)

	chatRepo := chat.NewRepository(conn)
	chatLimiter := chat.NewRedisRateLimiter(redisClient, 10, time.Minute)
	chatService := chat.NewService(chatRepo, gameRepository, chatLimiter)

	clubRepository := club.NewRepository(conn)
	clubService := club.NewService(clubRepository, chatLimiter)

	wsHub := websocket.NewHub()
	clubHandler := club.NewHandler(clubService, wsHub)
	wsHandler := websocket.NewHandler(wsHub, gameService, chatService)
	wsHandler.SetInviteFinder(invitationService)
	wsHandler.SetWatchInviteService(invitationService)
	wsHandler.SetUserRepo(authModule.UserRepository)
	authModule.SetDisconnector(wsHub)
	clubHandler.SetUserRepo(authModule.UserRepository)
	botHandler.SetUserRepo(authModule.UserRepository)
	botHandler.SetHub(wsHub)
	chatHandler := chat.NewHandler(chatService, wsHandler.BroadcastChatMessage)

	tournamentRepo := tournament.NewRepository(conn)
	tournamentService := tournament.NewService(tournamentRepo, gameRepository, wsHandler)
	tournamentHandler := tournament.NewHandler(tournamentService)
	wsHandler.SetTournamentFinder(tournamentService)

	nearbyRepository := nearby.NewRepository(conn)
	nearbyService := nearby.NewService(nearbyRepository)
	nearbyHandler := nearby.NewHandler(nearbyService)

	friendsRepository := friends.NewRepository(conn)
	friendsService := friends.NewService(friendsRepository)
	friendsHandler := friends.NewHandler(friendsService)

	puzzleRepo := puzzle.NewRepository(conn, redisClient)
	puzzleService := puzzle.NewService(puzzleRepo)
	puzzleHandler := puzzle.NewHandler(puzzleService)

	gameService.SetGameCompletionListener(func(ctx context.Context, gameID string, result string) {
		if err := ratingService.UpdateRatingsAfterGame(ctx, gameID); err != nil {
			slog.ErrorContext(ctx, "update ratings failed", "game_id", gameID, "error", err)
		}
		_ = tournamentService.OnGameCompleted(ctx, gameID, result)
		_, _ = analysisService.TriggerAnalysis(ctx, gameID, 0)
	})

	gameService.SetGameStateUpdatedListener(func(ctx context.Context, g *game.Game) {
		wsHandler.BroadcastGameState(g)
	})

	r, err := router.New(
		cfg.JWTSecret,
		authModule,
		playerModule,
		communityModule,
		invitationHandler,
		gameHandler,
		matchmakingHandler,
		wsHandler,
		botHandler,
		clubHandler,
		chatHandler,
		tiltHandler,
		analysisHandler,
		nearbyHandler,
		friendsHandler,
		puzzleHandler,
		tournamentHandler,
	)
	if err != nil {
		return fmt.Errorf("setup router: %w", err)
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	asynqServer := worker.StartEmailServer(asynqRedisOpt, email.NewSender(cfg.SMTP))
	analysisServer := analysis.StartAnalysisServer(asynqRedisOpt, analysisProcessor, 2)
	worker.StartSessionCleanup(appCtx, authModule.SessionRepository, 1*time.Hour)
	matcher.Start(appCtx)
	tournament.StartTournamentCleanup(appCtx, tournamentRepo, 24*time.Hour)
	gameService.StartTimeoutChecker(appCtx, 1*time.Second)
	wsHandler.StartActiveAuthChecker(appCtx, 5*time.Second)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	metricsSrv := &http.Server{
		Addr:              ":9100",
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server failed", "error", err)
		}
	}()

	slog.Info("server started", "port", cfg.Port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	appCancel()

	if asynqServer != nil {
		asynqServer.Stop()
	}
	if analysisServer != nil {
		analysisServer.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = metricsSrv.Shutdown(ctx)

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	slog.Info("server exited")
	return nil
}
