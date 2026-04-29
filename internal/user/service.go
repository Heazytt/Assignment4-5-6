package user

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	pb "github.com/sre-project/microservices/proto/userpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type User struct {
	ID    int64
	Email string
	Name  string
}

func (r *Repository) GetByID(ctx context.Context, id int64) (*User, error) {
	const q = `SELECT id, email, name FROM users WHERE id = $1`
	u := &User{}
	err := r.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	return u, err
}

// GRPCServer implements the user.UserService gRPC service.
type GRPCServer struct {
	pb.UnimplementedUserServiceServer
	repo    *Repository
	log     *slog.Logger
	metrics *metrics.Registry
}

func NewGRPCServer(repo *Repository, log *slog.Logger, m *metrics.Registry) *GRPCServer {
	return &GRPCServer{repo: repo, log: log, metrics: m}
}

func (s *GRPCServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.UserResponse, error) {
	u, err := s.repo.GetByID(ctx, req.GetId())
	if err != nil {
		s.metrics.BusinessOps.WithLabelValues("get_user", "error").Inc()
		return nil, err
	}
	s.metrics.BusinessOps.WithLabelValues("get_user", "ok").Inc()
	return &pb.UserResponse{Id: u.ID, Email: u.Email, Name: u.Name}, nil
}
