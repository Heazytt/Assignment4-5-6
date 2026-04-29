package product

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	pb "github.com/sre-project/microservices/proto/productpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Product struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Price       float64 `json:"price"`
	Stock       int32   `json:"stock"`
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) List(ctx context.Context) ([]Product, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, description, price, stock FROM products ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (*Product, error) {
	p := &Product{}
	err := r.pool.QueryRow(ctx, `SELECT id, name, description, price, stock FROM products WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("not found")
	}
	return p, err
}

func (r *Repository) Create(ctx context.Context, p Product) (*Product, error) {
	err := r.pool.QueryRow(ctx,
		`INSERT INTO products (name, description, price, stock) VALUES ($1,$2,$3,$4)
		 RETURNING id`, p.Name, p.Description, p.Price, p.Stock).Scan(&p.ID)
	return &p, err
}

// ReserveStock atomically decrements stock if enough is available.
// Returns the unit price on success.
func (r *Repository) ReserveStock(ctx context.Context, id int64, qty int32) (float64, bool, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, false, "", err
	}
	defer tx.Rollback(ctx)
	var price float64
	var stock int32
	err = tx.QueryRow(ctx, `SELECT price, stock FROM products WHERE id=$1 FOR UPDATE`, id).Scan(&price, &stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, "product not found", nil
	}
	if err != nil {
		return 0, false, "", err
	}
	if stock < qty {
		return price, false, "insufficient stock", nil
	}
	_, err = tx.Exec(ctx, `UPDATE products SET stock = stock - $1 WHERE id = $2`, qty, id)
	if err != nil {
		return 0, false, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, "", err
	}
	return price, true, "", nil
}

// HTTP handler.
type Handler struct {
	repo    *Repository
	metrics *metrics.Registry
	log     *slog.Logger
}

func NewHandler(repo *Repository, m *metrics.Registry, log *slog.Logger) *Handler {
	return &Handler{repo: repo, metrics: m, log: log}
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /products", h.list)
	mux.HandleFunc("GET /products/{id}", h.get)
	mux.HandleFunc("POST /products", h.create)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.repo.List(r.Context())
	if err != nil {
		h.log.Error("list products", "err", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []Product{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	p, err := h.repo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var p Product
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if p.Name == "" || p.Price <= 0 {
		http.Error(w, "name and positive price required", http.StatusBadRequest)
		return
	}
	out, err := h.repo.Create(r.Context(), p)
	if err != nil {
		h.log.Error("create product", "err", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	h.metrics.BusinessOps.WithLabelValues("create_product", "ok").Inc()
	writeJSON(w, http.StatusCreated, out)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// gRPC server.
type GRPCServer struct {
	pb.UnimplementedProductServiceServer
	repo    *Repository
	metrics *metrics.Registry
	log     *slog.Logger
}

func NewGRPCServer(repo *Repository, m *metrics.Registry, log *slog.Logger) *GRPCServer {
	return &GRPCServer{repo: repo, metrics: m, log: log}
}

func (s *GRPCServer) GetProduct(ctx context.Context, req *pb.GetProductRequest) (*pb.ProductResponse, error) {
	p, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &pb.ProductResponse{
		Id: p.ID, Name: p.Name, Description: p.Description, Price: p.Price, Stock: p.Stock,
	}, nil
}

func (s *GRPCServer) ReserveStock(ctx context.Context, req *pb.ReserveStockRequest) (*pb.ReserveStockResponse, error) {
	price, ok, reason, err := s.repo.ReserveStock(ctx, req.GetProductId(), req.GetQuantity())
	if err != nil {
		s.metrics.BusinessOps.WithLabelValues("reserve_stock", "error").Inc()
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !ok {
		s.metrics.BusinessOps.WithLabelValues("reserve_stock", "rejected").Inc()
		return &pb.ReserveStockResponse{Ok: false, Reason: reason}, nil
	}
	s.metrics.BusinessOps.WithLabelValues("reserve_stock", "ok").Inc()
	return &pb.ReserveStockResponse{Ok: true, UnitPrice: price}, nil
}
