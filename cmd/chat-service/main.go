package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sre-project/microservices/internal/chat"
	"github.com/sre-project/microservices/internal/pkg/config"
	"github.com/sre-project/microservices/internal/pkg/db"
	"github.com/sre-project/microservices/internal/pkg/jwt"
	"github.com/sre-project/microservices/internal/pkg/logger"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	"github.com/sre-project/microservices/internal/pkg/rabbitmq"
)

type Config struct {
	config.Common
	DB     config.Database
	Rabbit config.RabbitMQ
}

func main() {
	var cfg Config
	if err := config.Process("", &cfg); err != nil {
		panic(err)
	}
	log := logger.New("chat-service", cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.New(ctx, cfg.DB.DSN())
	if err != nil {
		log.Error("db connection failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rb, err := rabbitmq.Connect(cfg.Rabbit.URL)
	if err != nil {
		log.Error("rabbitmq connect failed", "err", err)
		os.Exit(1)
	}
	defer rb.Close()

	m := metrics.New("chat-service")
	repo := chat.NewRepository(pool)
	jm := jwt.NewManager(cfg.JWTSecret, 24*time.Hour)
	inbox := chat.NewInbox()

	if err := chat.Consume(ctx, rb, inbox, log); err != nil {
		log.Error("start consumer failed", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	chat.NewHandler(repo, jm, rb, inbox, m, log).Routes(mux)
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: m.Middleware(mux), ReadHeaderTimeout: 10 * time.Second}
	m.ServiceUp.Set(1)

	go func() {
		log.Info("chat-service HTTP listening", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http error", "err", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sCancel()
	_ = srv.Shutdown(shutdownCtx)
}
