package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUserNotFound = errors.New("user not found")
var ErrEmailTaken = errors.New("email already registered")

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, email, name, hash string) (*User, error) {
	const q = `INSERT INTO users (email, name, password_hash)
	           VALUES ($1, $2, $3)
	           RETURNING id, email, name, password_hash, created_at`
	u := &User{}
	err := r.pool.QueryRow(ctx, q, email, name, hash).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		// 23505 = unique_violation
		if pgErr := pgErrCode(err); pgErr == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return u, nil
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (*User, error) {
	const q = `SELECT id, email, name, password_hash, created_at
	           FROM users WHERE email = $1`
	u := &User{}
	err := r.pool.QueryRow(ctx, q, email).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

func (r *Repository) FindByID(ctx context.Context, id int64) (*User, error) {
	const q = `SELECT id, email, name, password_hash, created_at
	           FROM users WHERE id = $1`
	u := &User{}
	err := r.pool.QueryRow(ctx, q, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	return u, err
}

// pgErrCode extracts the PostgreSQL error code if the error is a *pgconn.PgError.
func pgErrCode(err error) string {
	type pgErr interface{ SQLState() string }
	var p pgErr
	if errors.As(err, &p) {
		return p.SQLState()
	}
	return ""
}
