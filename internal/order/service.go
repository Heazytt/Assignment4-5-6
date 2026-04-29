package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sre-project/microservices/internal/pkg/jwt"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	"github.com/sre-project/microservices/internal/pkg/rabbitmq"
	productpb "github.com/sre-project/microservices/proto/productpb"
	userpb "github.com/sre-project/microservices/proto/userpb"
)

const OrderEventsQueue = "order.events"

type Order struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"user_id"`
	Total     float64     `json:"total"`
	Status    string      `json:"status"`
	Items     []OrderItem `json:"items"`
	CreatedAt time.Time   `json:"created_at"`
}

type OrderItem struct {
	ProductID int64   `json:"product_id"`
	Quantity  int32   `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, o *Order) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx,
		`INSERT INTO orders (user_id, total, status) VALUES ($1, $2, $3)
		 RETURNING id, created_at`, o.UserID, o.Total, o.Status).Scan(&o.ID, &o.CreatedAt)
	if err != nil {
		return err
	}
	for _, it := range o.Items {
		_, err = tx.Exec(ctx,
			`INSERT INTO order_items (order_id, product_id, quantity, unit_price)
			 VALUES ($1,$2,$3,$4)`, o.ID, it.ProductID, it.Quantity, it.UnitPrice)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) ListByUser(ctx context.Context, userID int64) ([]Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, total, status, created_at FROM orders WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.Total, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (*Order, error) {
	o := &Order{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, total, status, created_at FROM orders WHERE id=$1`, id).
		Scan(&o.ID, &o.UserID, &o.Total, &o.Status, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("not found")
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT product_id, quantity, unit_price FROM order_items WHERE order_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ProductID, &it.Quantity, &it.UnitPrice); err != nil {
			return nil, err
		}
		o.Items = append(o.Items, it)
	}
	return o, rows.Err()
}

// Handler.
type Handler struct {
	repo       *Repository
	jwt        *jwt.Manager
	user       userpb.UserServiceClient
	product    productpb.ProductServiceClient
	rabbit     *rabbitmq.Client
	metrics    *metrics.Registry
	log        *slog.Logger
}

func NewHandler(repo *Repository, j *jwt.Manager, u userpb.UserServiceClient, p productpb.ProductServiceClient,
	rb *rabbitmq.Client, m *metrics.Registry, log *slog.Logger) *Handler {
	return &Handler{repo: repo, jwt: j, user: u, product: p, rabbit: rb, metrics: m, log: log}
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /orders", h.create)
	mux.HandleFunc("GET /orders", h.list)
	mux.HandleFunc("GET /orders/{id}", h.get)
}

type createReq struct {
	Items []struct {
		ProductID int64 `json:"product_id"`
		Quantity  int32 `json:"quantity"`
	} `json:"items"`
}

func (h *Handler) authenticate(r *http.Request) (*jwt.Claims, bool) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		return nil, false
	}
	c, err := h.jwt.Verify(tok)
	if err != nil {
		return nil, false
	}
	return c, true
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(req.Items) == 0 {
		http.Error(w, "no items", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Validate user exists via gRPC.
	if _, err := h.user.GetUser(ctx, &userpb.GetUserRequest{Id: claims.UserID}); err != nil {
		h.metrics.ExternalCalls.WithLabelValues("user-service", "error").Inc()
		h.log.Error("user lookup failed", "err", err)
		http.Error(w, "user lookup failed", http.StatusBadGateway)
		return
	}
	h.metrics.ExternalCalls.WithLabelValues("user-service", "ok").Inc()

	// Reserve stock for each item via Product gRPC.
	order := &Order{UserID: claims.UserID, Status: "created"}
	for _, it := range req.Items {
		if it.Quantity <= 0 {
			http.Error(w, "quantity must be positive", http.StatusBadRequest)
			return
		}
		resp, err := h.product.ReserveStock(ctx, &productpb.ReserveStockRequest{
			ProductId: it.ProductID, Quantity: it.Quantity,
		})
		if err != nil {
			h.metrics.ExternalCalls.WithLabelValues("product-service", "error").Inc()
			http.Error(w, "product reserve failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		h.metrics.ExternalCalls.WithLabelValues("product-service", "ok").Inc()
		if !resp.GetOk() {
			http.Error(w, "cannot reserve product "+strconv.FormatInt(it.ProductID, 10)+": "+resp.GetReason(), http.StatusConflict)
			return
		}
		order.Items = append(order.Items, OrderItem{
			ProductID: it.ProductID, Quantity: it.Quantity, UnitPrice: resp.GetUnitPrice(),
		})
		order.Total += resp.GetUnitPrice() * float64(it.Quantity)
	}

	if err := h.repo.Create(ctx, order); err != nil {
		h.log.Error("save order failed", "err", err)
		h.metrics.BusinessOps.WithLabelValues("create_order", "db_error").Inc()
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	// Publish event to RabbitMQ.
	evt, _ := json.Marshal(map[string]any{
		"event":      "order.created",
		"order_id":   order.ID,
		"user_id":    order.UserID,
		"total":      order.Total,
		"created_at": order.CreatedAt,
	})
	if err := h.rabbit.Publish(ctx, OrderEventsQueue, evt); err != nil {
		h.metrics.ExternalCalls.WithLabelValues("rabbitmq", "error").Inc()
		h.log.Warn("publish order event failed", "err", err)
		// non-fatal
	} else {
		h.metrics.ExternalCalls.WithLabelValues("rabbitmq", "ok").Inc()
	}
	h.metrics.BusinessOps.WithLabelValues("create_order", "ok").Inc()
	writeJSON(w, http.StatusCreated, order)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.repo.ListByUser(r.Context(), claims.UserID)
	if err != nil {
		h.log.Error("list orders", "err", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []Order{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	o, err := h.repo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if o.UserID != claims.UserID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Shutdown cleans up resources.
func (h *Handler) Shutdown() error {
	h.rabbit.Close()
	return nil
}

// Service-level error wrapper for clearer log lines used in incident.
func WrapDBError(host string, err error) error {
	return fmt.Errorf("order-service db (host=%s): %w", host, err)
}
