package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sre-project/microservices/internal/auth"
	"github.com/sre-project/microservices/internal/pkg/config"
	"github.com/sre-project/microservices/internal/pkg/db"
	"github.com/sre-project/microservices/internal/pkg/jwt"
	"github.com/sre-project/microservices/internal/pkg/logger"
	"github.com/sre-project/microservices/internal/pkg/metrics"
)

type Config struct {
	config.Common
	DB config.Database
}

func main() {
	var cfg Config
	if err := config.Process("", &cfg); err != nil {
		panic(err)
	}
	log := logger.New("auth-service", cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.New(ctx, cfg.DB.DSN())
	if err != nil {
		log.Error("db connection failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("connected to database", "host", cfg.DB.Host)

	m := metrics.New("auth-service")
	jm := jwt.NewManager(cfg.JWTSecret, 24*time.Hour)
	repo := auth.NewRepository(pool)
	h := auth.NewHandler(repo, jm, m, log)

	mux := http.NewServeMux()
	h.Routes(mux)
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           m.Middleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	m.ServiceUp.Set(1)

	go func() {
		log.Info("auth-service listening", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "err", err)
			cancel()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-stop:
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = slog.Default()
}
