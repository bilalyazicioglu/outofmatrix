// Command server is the outofmatrix media cloud backend: a self-hosted
// Google Photos + Spotify replacement in a single Go binary.
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

	"outofmatrix/internal/config"
	deliveryhttp "outofmatrix/internal/delivery/http"
	"outofmatrix/internal/delivery/ws"
	"outofmatrix/internal/repository/postgres"
	"outofmatrix/internal/usecase"
	"outofmatrix/internal/worker"
	"outofmatrix/migrations"
	"outofmatrix/pkg/ffmpeg"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.JWTSecretGenerated {
		log.Warn("JWT_SECRET not set; generated a random secret — sessions will not survive restarts")
	}

	// Root context: cancelled on SIGINT/SIGTERM, which cascades into the
	// HTTP server, the worker pool and any running FFmpeg subprocess.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- Infrastructure -----------------------------------------------------
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("connected to postgres")

	if err := postgres.Migrate(ctx, pool, migrations.Files, log); err != nil {
		return err
	}

	mediaRepo := postgres.NewMediaRepository(pool)
	userRepo := postgres.NewUserRepository(pool)
	collectionRepo := postgres.NewCollectionRepository(pool)
	uploadRepo := postgres.NewUploadRepository(pool)

	prober := ffmpeg.NewProber(cfg.FFprobePath)
	transcoder := ffmpeg.NewTranscoder(cfg.FFmpegPath, log)

	// --- Background pipeline + real-time events ------------------------------
	jobPool := worker.New(cfg.MaxWorkers, cfg.JobQueueSize, cfg.ProcessTimeout, log)
	hub := ws.NewHub(log)

	// --- Usecases -------------------------------------------------------------
	authUC := usecase.NewAuthUsecase(userRepo, cfg.JWTSecret, cfg.TokenTTL)
	mediaUC := usecase.NewMediaUsecase(usecase.MediaConfig{
		Repo:              mediaRepo,
		Prober:            prober,
		Transcoder:        transcoder,
		Dispatcher:        jobPool,
		Notifier:          hub,
		StoragePath:       cfg.StoragePath,
		HLSSegmentSeconds: cfg.HLSSegmentSeconds,
		HWAccel:           ffmpeg.HWAccel(cfg.HWAccel),
		Log:               log,
	})
	uploadUC := usecase.NewUploadUsecase(uploadRepo, mediaUC, cfg.MaxUploadBytes, cfg.UploadTTL, log)
	collectionUC := usecase.NewCollectionUsecase(collectionRepo, mediaRepo)

	if err := mediaUC.EnsureStorageDirs(); err != nil {
		return err
	}

	jobPool.Start(ctx, func(jobCtx context.Context, job worker.Job) error {
		return mediaUC.Process(jobCtx, job.MediaID)
	})

	// Re-queue anything that was pending or mid-processing at last shutdown.
	if queued, err := mediaUC.RecoverUnfinished(ctx); err != nil {
		log.Warn("job recovery failed", "error", err)
	} else if queued > 0 {
		log.Info("recovered unfinished media jobs", "count", queued)
	}

	// Janitor: reclaim abandoned chunked-upload staging files.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			if removed, err := uploadUC.CleanupExpired(ctx); err != nil {
				if ctx.Err() == nil {
					log.Warn("upload janitor failed", "error", err)
				}
			} else if removed > 0 {
				log.Info("upload janitor reclaimed expired sessions", "count", removed)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	// --- HTTP server ----------------------------------------------------------
	router := deliveryhttp.NewRouter(deliveryhttp.RouterDeps{
		Log:            log,
		Auth:           authUC,
		Media:          mediaUC,
		Uploads:        uploadUC,
		Collections:    collectionUC,
		Hub:            hub,
		MaxUploadBytes: cfg.MaxUploadBytes,
		WebDir:         cfg.WebDir,
		Health: func() error {
			pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			return pool.Ping(pingCtx)
		},
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
		// Large uploads, long-running streams and WebSockets rule out global
		// read/write timeouts; the header timeout still stops slowloris.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", server.Addr, "hwaccel", cfg.HWAccel, "workers", cfg.MaxWorkers)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	// Graceful stop: finish in-flight requests, then drain the job queue.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown incomplete", "error", err)
	}
	jobPool.Shutdown()

	log.Info("bye")
	return nil
}
