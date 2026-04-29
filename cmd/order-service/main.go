package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sre-project/microservices/internal/order"
	"github.com/sre-project/microservices/internal/pkg/config"
	"github.com/sre-project/microservices/internal/pkg/db"
	"github.com/sre-project/microservices/internal/pkg/jwt"
	"github.com/sre-project/microservices/internal/pkg/logger"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	"github.com/sre-project/microservices/internal/pkg/rabbitmq"
	productpb "github.com/sre-project/microservices/proto/productpb"
	userpb "github.com/sre-project/microservices/proto/userpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	config.Common
	DB           config.Database
	Rabbit       config.RabbitMQ
	UserGRPC     string `envconfig:"USER_GRPC_ADDR" default:"user-service:9000"`
	ProductGRPC  string `envconfig:"PRODUCT_GRPC_ADDR" default:"product-service:9000"`
}

func main() {
	var cfg Config
	if err := config.Process("", &cfg); err != nil {
		panic(err)
	}
	log := logger.New("order-service", cfg.LogLevel)
	log.Info("starting", "db_host", cfg.DB.Host, "rabbit_url", cfg.Rabbit.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// >>> THIS IS WHERE THE INCIDENT SURFACES.
	// If DB_HOST is misconfigured (e.g., "postgres-typo"), pgxpool will fail
	// to connect on startup — the container will exit-restart loop and Prometheus
	// will record service_up=0. See reports/incident_report.md for details.
	pool, err := db.New(ctx, cfg.DB.DSN())
	if err != nil {
		log.Error("FATAL: order-service cannot connect to database",
			"err", err, "db_host", cfg.DB.Host, "db_port", cfg.DB.Port,
			"hint", "check DB_HOST env var; misconfiguration causes hard startup failure")
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("connected to database", "host", cfg.DB.Host)

	rb, err := rabbitmq.Connect(cfg.Rabbit.URL)
	if err != nil {
		log.Error("rabbitmq connect failed", "err", err)
		os.Exit(1)
	}
	defer rb.Close()
	if err := rb.DeclareQueue(order.OrderEventsQueue); err != nil {
		log.Error("declare queue failed", "err", err)
		os.Exit(1)
	}

	userConn, err := grpc.NewClient(cfg.UserGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("user grpc connect failed", "err", err)
		os.Exit(1)
	}
	defer userConn.Close()

	productConn, err := grpc.NewClient(cfg.ProductGRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("product grpc connect failed", "err", err)
		os.Exit(1)
	}
	defer productConn.Close()

	m := metrics.New("order-service")
	repo := order.NewRepository(pool)
	jm := jwt.NewManager(cfg.JWTSecret, 24*time.Hour)

	h := order.NewHandler(
		repo, jm,
		userpb.NewUserServiceClient(userConn),
		productpb.NewProductServiceClient(productConn),
		rb, m, log,
	)

	mux := http.NewServeMux()
	h.Routes(mux)
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		// Real health check - verify DB still reachable.
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, `{"status":"unhealthy","reason":"db unreachable"}`, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: m.Middleware(mux), ReadHeaderTimeout: 10 * time.Second}
	m.ServiceUp.Set(1)

	go func() {
		log.Info("order-service HTTP listening", "port", cfg.HTTPPort)
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
