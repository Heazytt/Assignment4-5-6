package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sre-project/microservices/internal/pkg/config"
	"github.com/sre-project/microservices/internal/pkg/db"
	"github.com/sre-project/microservices/internal/pkg/logger"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	"github.com/sre-project/microservices/internal/user"
	pb "github.com/sre-project/microservices/proto/userpb"
	"google.golang.org/grpc"
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
	log := logger.New("user-service", cfg.LogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.New(ctx, cfg.DB.DSN())
	if err != nil {
		log.Error("db connection failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("connected to database", "host", cfg.DB.Host)

	m := metrics.New("user-service")
	repo := user.NewRepository(pool)

	// gRPC server
	grpcSrv := grpc.NewServer()
	pb.RegisterUserServiceServer(grpcSrv, user.NewGRPCServer(repo, log, m))

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Error("grpc listen failed", "err", err)
		os.Exit(1)
	}
	go func() {
		log.Info("user-service gRPC listening", "port", cfg.GRPCPort)
		if err := grpcSrv.Serve(lis); err != nil {
			log.Error("grpc serve error", "err", err)
		}
	}()

	// HTTP for metrics + health
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: m.Middleware(mux), ReadHeaderTimeout: 10 * time.Second}
	m.ServiceUp.Set(1)

	go func() {
		log.Info("user-service HTTP listening", "port", cfg.HTTPPort)
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
	grpcSrv.GracefulStop()
}
