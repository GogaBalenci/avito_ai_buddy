package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

// HeaderEscalation tells the client that the second «Не то» in a row asks
// for a draft question to the mentor (11 K-A7): the client sends
// intent:ask_mentor. The body stays 204 (the contract of /api/feedback).
const HeaderEscalation = "X-Buddy-Escalation"

// GetKbDoc returns a document for the source page (11 §2.7, p. 6).
func (s *Server) GetKbDoc(w http.ResponseWriter, r *http.Request, slug string) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	var out struct {
		Doc api.KbDoc `json:"doc"`
	}
	err := s.Tools.Call(ctx, "knowledge", "get_doc", map[string]any{"actor": a, "slug": slug}, &out)
	if err != nil {
		te, _ := mcpclient.AsToolError(err)
		switch {
		case te != nil && te.Code == "PERMISSION_DENIED": // K-A2: the title without the body
			_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": "knowledge.get_doc", "object": slug, "role": a.Role}})
			writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Документ доступен только наставникам", map[string]any{"title": te.Details["title"]})
		case te != nil && te.Code == "NOT_FOUND": // K-A3, K-A11
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Документ не найден", nil)
		default:
			writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "База знаний сейчас не отвечает", map[string]any{"retryable": true})
		}
		return
	}
	if out.Doc.ConflictsWith == nil {
		out.Doc.ConflictsWith = []string{}
	}
	from := r.URL.Query().Get("from")
	if from != "reply" && from != "step" {
		from = "direct"
	}
	_ = s.Events.Publish(ctx, events.Event{Name: "kb_doc_opened", Payload: map[string]any{"slug": slug, "from": from}})
	writeJSON(w, http.StatusOK, out.Doc)
}

// ownReply loads a saved reply of the user (404 for a foreign one).
func (s *Server) ownReply(ctx context.Context, userID string, id uuid.UUID) (*api.AgentReply, error) {
	var body []byte
	err := s.Pool.QueryRow(ctx, `SELECT body FROM app.agent_replies WHERE reply_id = $1 AND user_id = $2`, id, userID).Scan(&body)
	if err != nil {
		return nil, err
	}
	var rep api.AgentReply
	if err := json.Unmarshal(body, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

// PostFeedback stores «Помогло» / «Не то» (11 §2.7, p. 7).
func (s *Server) PostFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	var body api.FeedbackRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	id := body.ReplyId
	if _, err := s.ownReply(ctx, a.UserID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Ответ не найден", nil)
			return
		}
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	value := string(body.Value)
	var prev *string
	_ = s.Pool.QueryRow(ctx, `SELECT value FROM app.feedback WHERE reply_id = $1`, id).Scan(&prev)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO app.feedback (reply_id, user_id, value, created_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (reply_id) DO UPDATE SET value = EXCLUDED.value, created_at = EXCLUDED.created_at`, id, a.UserID, value, s.Clock.Now()); err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	if prev != nil && *prev == value { // the same value again: nothing new
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = s.Events.Publish(ctx, events.Event{Name: "feedback", Payload: map[string]any{"reply_id": id.String(), "value": value}})
	key := "ctx:fbdown:" + a.UserID
	if value == "up" {
		s.Redis.Del(ctx, key)
	} else if n, err := s.Redis.Incr(ctx, key).Result(); err == nil {
		s.Redis.Expire(ctx, key, time.Hour)
		if n >= 2 { // two «Не то» in a row (K-A7)
			s.Redis.Del(ctx, key)
			_ = s.Events.Publish(ctx, events.Event{Name: "escalated", Payload: map[string]any{"reason": "negative_feedback"}})
			w.Header().Set(HeaderEscalation, "negative_feedback")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// PostClientEvent records an event that happens only on the client; the
// other fields come from the saved reply and the session.
func (s *Server) PostClientEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	var body api.ClientEventRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	switch body.Name {
	case api.ClientEventRequestNameNextStepClicked:
		if body.StepId == nil || *body.StepId == "" {
			writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Нужен step_id", nil)
			return
		}
		_ = s.Events.Publish(ctx, events.Event{Name: "next_step_clicked", Payload: map[string]any{"step_id": *body.StepId}})
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if body.ReplyId == nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Нужен reply_id", nil)
		return
	}
	rep, err := s.ownReply(ctx, a.UserID, *body.ReplyId)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Ответ не найден", nil)
		return
	}
	switch body.Name {
	case api.ClientEventRequestNameEscalationCopied:
		reason := ""
		if rep.Escalation != nil {
			reason = string(rep.Escalation.Reason)
		}
		_ = s.Events.Publish(ctx, events.Event{Name: "escalation_copied", Payload: map[string]any{"reason": reason}})
	case api.ClientEventRequestNameDocConflictReported:
		if ok, _ := s.Redis.SetNX(ctx, "ev:conflict:"+body.ReplyId.String(), "1", 30*24*time.Hour).Result(); ok {
			slugs := []string{}
			for _, src := range rep.Sources {
				slugs = append(slugs, src.Slug)
			}
			_ = s.Events.Publish(ctx, events.Event{Name: "doc_conflict_reported", Payload: map[string]any{"slugs": slugs}})
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
