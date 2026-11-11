package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/pii"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/router"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// Repeated client_message_id within this window returns the saved reply (09 §2.1).
const msgTTL = 10 * time.Minute

const msgPending = "pending"

func msgKey(userID string, id uuid.UUID) string { return "msg:" + userID + ":" + id.String() }

// PostChatMessage implements POST /api/chat/messages (09 §2).
func (s *Server) PostChatMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	var body api.ChatMessageRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	hasText, hasPayload := body.Text != nil, body.Payload != nil
	if hasText == hasPayload {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Нужно ровно одно из полей text и payload", nil)
		return
	}
	cmid := body.ClientMessageId
	key := msgKey(a.UserID, cmid)

	// A repeat returns the saved reply without processing, events or history.
	if rep, ok := s.savedReply(ctx, a.UserID, key, cmid); ok {
		writeJSON(w, http.StatusOK, rep)
		return
	}
	if !s.limit(w, r, "msg") {
		return
	}
	set, err := s.Redis.SetNX(ctx, key, msgPending, msgTTL).Result()
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	if !set { // a parallel repeat: wait for the first one
		if rep, ok := s.savedReply(ctx, a.UserID, key, cmid); ok {
			writeJSON(w, http.StatusOK, rep)
			return
		}
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Ответ ещё готовится — повтори через секунду", map[string]any{"retryable": true})
		return
	}

	in := router.Input{ClientMessageID: cmid}
	if hasText {
		in.Text, in.Masked = pii.Mask(*body.Text)
	} else {
		in.Payload = *body.Payload
	}
	out, err := s.Agent.Handle(ctx, a, in)
	if err != nil {
		s.Redis.Del(context.WithoutCancel(ctx), key)
		var re *router.Error
		if errors.As(err, &re) {
			writeError(w, r, re.Status, re.Code, re.Message, nil)
			return
		}
		s.Log.Error("chat", "err", err, "request_id", reqctx.RequestID(ctx))
		writeError(w, r, http.StatusInternalServerError, "INTERNAL", "Что-то пошло не так", nil)
		return
	}
	rep := out.Reply
	if err := s.saveExchange(ctx, a, in, out); err != nil {
		if saved, ok := s.fromDB(ctx, a.UserID, cmid); ok { // UNIQUE: the second line of defense
			rep = saved
		} else {
			s.Redis.Del(context.WithoutCancel(ctx), key)
			s.Log.Error("chat save", "err", err, "request_id", reqctx.RequestID(ctx))
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "Что-то пошло не так", nil)
			return
		}
	}
	b, _ := json.Marshal(rep)
	s.Redis.Set(context.WithoutCancel(ctx), key, b, msgTTL)
	writeJSON(w, http.StatusOK, rep)
}

// savedReply returns the reply of a processed message, waiting a little for
// one in flight. ok=false means "not processed yet and nobody is processing".
func (s *Server) savedReply(ctx context.Context, userID, key string, cmid uuid.UUID) (*api.AgentReply, bool) {
	deadline := time.Now().Add(s.chatWait()) // wall time: the business clock may be frozen in tests
	for {
		v, err := s.Redis.Get(ctx, key).Result()
		switch {
		case errors.Is(err, redis.Nil):
			return s.fromDB(ctx, userID, cmid)
		case err != nil:
			return nil, false
		case v != msgPending:
			var rep api.AgentReply
			if json.Unmarshal([]byte(v), &rep) == nil {
				return &rep, true
			}
			return nil, false
		}
		if !time.Now().Before(deadline) {
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *Server) chatWait() time.Duration {
	if s.ChatWait > 0 {
		return s.ChatWait
	}
	return 10 * time.Second
}

// fromDB finds a reply saved earlier for (user, client_message_id).
func (s *Server) fromDB(ctx context.Context, userID string, cmid uuid.UUID) (*api.AgentReply, bool) {
	var body []byte
	err := s.Pool.QueryRow(ctx, `SELECT r.body FROM app.agent_replies r JOIN app.chat_messages m ON m.id = r.message_id
		WHERE m.user_id = $1 AND m.client_message_id = $2`, userID, cmid).Scan(&body)
	if err != nil {
		return nil, false
	}
	var rep api.AgentReply
	if json.Unmarshal(body, &rep) != nil {
		return nil, false
	}
	return &rep, true
}

// saveExchange stores the masked message and the reply in one transaction.
func (s *Server) saveExchange(ctx context.Context, a actor.Actor, in router.Input, out router.Output) error {
	body, err := json.Marshal(out.Reply)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		// Strictly increasing per user, so the history order is stable even
		// when two messages share a timestamp.
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT GREATEST($2::timestamptz, (SELECT max(created_at) + interval '1 microsecond'
			FROM app.chat_messages WHERE user_id = $1))`, a.UserID, s.Clock.Now()).Scan(&now); err != nil {
			return err
		}
		msgID := uuid.New()
		var text, payload, label *string
		kind := "text"
		if in.Payload != "" {
			kind, payload, label = "payload", &in.Payload, &out.ButtonLabel
		} else {
			text = &in.Text
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.chat_messages (id, user_id, client_message_id, kind, text_masked, payload, button_label, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, msgID, a.UserID, in.ClientMessageID, kind, text, payload, label, now); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO app.agent_replies (reply_id, message_id, user_id, intent, mode, body, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, out.Reply.ReplyId, msgID, a.UserID, string(out.Reply.Intent),
			string(out.Reply.Mode), body, now)
		return err
	})
}

// GetChatHistory implements GET /api/chat/history: masked messages and saved
// replies, oldest first, without calling the agent again (09 §2.1).
func (s *Server) GetChatHistory(w http.ResponseWriter, r *http.Request, params api.GetChatHistoryParams) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}
	rows, err := s.Pool.Query(ctx, `SELECT m.id, m.created_at, m.text_masked, m.button_label, r.reply_id, r.created_at, r.body
		FROM app.chat_messages m LEFT JOIN app.agent_replies r ON r.message_id = m.id
		WHERE m.user_id = $1 ORDER BY m.created_at DESC, m.id DESC LIMIT $2`, a.UserID, limit)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	defer rows.Close()
	items := []api.HistoryItem{}
	for rows.Next() {
		var (
			id          uuid.UUID
			ts          time.Time
			text, label *string
			replyID     *uuid.UUID
			replyTS     *time.Time
			body        []byte
		)
		if err := rows.Scan(&id, &ts, &text, &label, &replyID, &replyTS, &body); err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL", "Что-то пошло не так", nil)
			return
		}
		if replyID != nil {
			var rep api.AgentReply
			if json.Unmarshal(body, &rep) == nil {
				items = append(items, api.HistoryItem{Id: *replyID, Ts: *replyTS, Author: api.Bot, Reply: &rep})
			}
		}
		items = append(items, api.HistoryItem{Id: id, Ts: ts, Author: api.User, Text: text, ButtonLabel: label})
	}
	slices.Reverse(items)
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	writeJSON(w, http.StatusOK, api.HistoryPage{Items: items})
}
