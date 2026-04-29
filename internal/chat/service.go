package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sre-project/microservices/internal/pkg/jwt"
	"github.com/sre-project/microservices/internal/pkg/metrics"
	"github.com/sre-project/microservices/internal/pkg/rabbitmq"
)

const ChatQueue = "chat.messages"

type Message struct {
	ID         int64     `json:"id"`
	SenderID   int64     `json:"sender_id"`
	ReceiverID int64     `json:"receiver_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Save(ctx context.Context, m *Message) error {
	return r.pool.QueryRow(ctx,
		`INSERT INTO chat_messages (sender_id, receiver_id, content)
		 VALUES ($1,$2,$3) RETURNING id, created_at`,
		m.SenderID, m.ReceiverID, m.Content).Scan(&m.ID, &m.CreatedAt)
}

func (r *Repository) Conversation(ctx context.Context, userA, userB int64) ([]Message, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, sender_id, receiver_id, content, created_at
		 FROM chat_messages
		 WHERE (sender_id=$1 AND receiver_id=$2) OR (sender_id=$2 AND receiver_id=$1)
		 ORDER BY id ASC LIMIT 100`, userA, userB)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SenderID, &m.ReceiverID, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Inbox keeps recent messages per user in memory so polling the inbox
// endpoint returns near-realtime data delivered through RabbitMQ.
type Inbox struct {
	mu sync.Mutex
	m  map[int64][]Message
}

func NewInbox() *Inbox { return &Inbox{m: make(map[int64][]Message)} }

func (i *Inbox) Push(msg Message) {
	i.mu.Lock()
	defer i.mu.Unlock()
	cur := i.m[msg.ReceiverID]
	if len(cur) >= 50 {
		cur = cur[1:]
	}
	i.m[msg.ReceiverID] = append(cur, msg)
}

func (i *Inbox) Drain(userID int64) []Message {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := i.m[userID]
	delete(i.m, userID)
	return out
}

// Handler exposes the HTTP API.
type Handler struct {
	repo    *Repository
	jwt     *jwt.Manager
	rabbit  *rabbitmq.Client
	inbox   *Inbox
	metrics *metrics.Registry
	log     *slog.Logger
}

func NewHandler(repo *Repository, jm *jwt.Manager, rb *rabbitmq.Client, inbox *Inbox,
	m *metrics.Registry, log *slog.Logger) *Handler {
	return &Handler{repo: repo, jwt: jm, rabbit: rb, inbox: inbox, metrics: m, log: log}
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /chat/send", h.send)
	mux.HandleFunc("GET /chat/inbox", h.inboxHandler)
	mux.HandleFunc("GET /chat/with/{userId}", h.conversation)
}

type sendReq struct {
	ReceiverID int64  `json:"receiver_id"`
	Content    string `json:"content"`
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req sendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.ReceiverID == 0 || strings.TrimSpace(req.Content) == "" {
		http.Error(w, "receiver_id and content required", http.StatusBadRequest)
		return
	}
	msg := Message{SenderID: claims.UserID, ReceiverID: req.ReceiverID, Content: req.Content}
	if err := h.repo.Save(r.Context(), &msg); err != nil {
		h.log.Error("save chat message", "err", err)
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	body, _ := json.Marshal(msg)
	if err := h.rabbit.Publish(r.Context(), ChatQueue, body); err != nil {
		h.metrics.ExternalCalls.WithLabelValues("rabbitmq", "error").Inc()
		h.log.Warn("publish chat message failed", "err", err)
	} else {
		h.metrics.ExternalCalls.WithLabelValues("rabbitmq", "ok").Inc()
	}
	h.metrics.BusinessOps.WithLabelValues("send_message", "ok").Inc()
	writeJSON(w, http.StatusCreated, msg)
}

func (h *Handler) inboxHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	msgs := h.inbox.Drain(claims.UserID)
	if msgs == nil {
		msgs = []Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (h *Handler) conversation(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authenticate(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	other, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	msgs, err := h.repo.Conversation(r.Context(), claims.UserID, other)
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []Message{}
	}
	writeJSON(w, http.StatusOK, msgs)
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

// Consume reads messages from RabbitMQ and pushes them into the inbox.
// In a real system this would push via WebSocket; here we keep a simple
// polling inbox so the demo works without WS plumbing.
func Consume(ctx context.Context, rb *rabbitmq.Client, inbox *Inbox, log *slog.Logger) error {
	if err := rb.DeclareQueue(ChatQueue); err != nil {
		return err
	}
	deliveries, err := rb.Consume(ChatQueue, "chat-service")
	if err != nil {
		return err
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					return
				}
				var m Message
				if err := json.Unmarshal(d.Body, &m); err != nil {
					log.Warn("bad chat message", "err", err)
					continue
				}
				inbox.Push(m)
			}
		}
	}()
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
