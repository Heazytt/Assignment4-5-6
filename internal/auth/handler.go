package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sre-project/microservices/internal/pkg/jwt"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	repo    *Repository
	jwt     *jwt.Manager
	metrics *metrics.Registry
	log     *slog.Logger
}

func NewHandler(repo *Repository, jwt *jwt.Manager, m *metrics.Registry, log *slog.Logger) *Handler {
	return &Handler{repo: repo, jwt: jwt, metrics: m, log: log}
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", h.register)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("GET /auth/me", h.me)
	mux.HandleFunc("GET /auth/verify", h.verify)
}

type registerReq struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type tokenResp struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "email, name and password are required")
		return
	}
	if len(req.Password) < 6 {
		writeErr(w, http.StatusBadRequest, "password must be >= 6 chars")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.log.Error("bcrypt failed", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	ctx, cancel := withTimeout(r.Context())
	defer cancel()

	u, err := h.repo.Create(ctx, strings.ToLower(req.Email), req.Name, string(hash))
	if err != nil {
		if errors.Is(err, ErrEmailTaken) {
			h.metrics.BusinessOps.WithLabelValues("register", "conflict").Inc()
			writeErr(w, http.StatusConflict, "email already registered")
			return
		}
		h.log.Error("create user failed", "err", err)
		h.metrics.BusinessOps.WithLabelValues("register", "error").Inc()
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	token, err := h.jwt.Issue(u.ID, u.Email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	h.metrics.BusinessOps.WithLabelValues("register", "ok").Inc()
	writeJSON(w, http.StatusCreated, tokenResp{Token: token, User: u})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, cancel := withTimeout(r.Context())
	defer cancel()

	u, err := h.repo.FindByEmail(ctx, strings.ToLower(req.Email))
	if err != nil {
		h.metrics.BusinessOps.WithLabelValues("login", "not_found").Inc()
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		h.metrics.BusinessOps.WithLabelValues("login", "bad_password").Inc()
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := h.jwt.Issue(u.ID, u.Email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	h.metrics.BusinessOps.WithLabelValues("login", "ok").Inc()
	writeJSON(w, http.StatusOK, tokenResp{Token: token, User: u})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx, cancel := withTimeout(r.Context())
	defer cancel()
	u, err := h.repo.FindByID(ctx, claims.UserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// verify - used by other services to check token validity.
func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"valid":   true,
		"user_id": claims.UserID,
		"email":   claims.Email,
	})
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 5*time.Second)
}
