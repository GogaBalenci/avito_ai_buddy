package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/checklist/fsm"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	"github.com/GogaBalenci/avito_ai_buddy/internal/events"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/mcpclient"
	"github.com/GogaBalenci/avito_ai_buddy/internal/path"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

func date(t time.Time) openapi_types.Date { return openapi_types.Date{Time: t} }

// pathStepJSON converts a step to PathStep.
func pathStepJSON(s path.Step, req *path.Request) api.PathStep {
	ps := api.PathStep{StepId: s.ID, Period: api.PeriodId(s.Period), Order: s.Order, Title: s.Title,
		Type: api.PathStepType(s.Type), Status: api.StepStatus(s.Status), Synthetic: s.Synthetic}
	if s.ChecklistRef != "" {
		ref := s.ChecklistRef
		ps.ChecklistRef = &ref
	}
	ps.Help.Kind = api.PathStepHelpKind(s.HelpKind)
	if s.KBDoc != "" {
		d := s.KBDoc
		ps.Help.KbDoc = &d
	}
	if s.Resource != "" {
		r := s.Resource
		ps.Help.Resource = &r
	}
	if req != nil {
		ps.Request = &struct {
			AutoApproveAt *time.Time                `json:"auto_approve_at,omitempty"`
			RejectReason  *string                   `json:"reject_reason,omitempty"`
			RequestId     string                    `json:"request_id"`
			Status        api.PathStepRequestStatus `json:"status"`
		}{AutoApproveAt: req.AutoApproveAt, RejectReason: req.RejectReason, RequestId: req.RequestID, Status: api.PathStepRequestStatus(req.Status)}
	}
	return ps
}

func nextJSON(n *path.Next) *api.NextStep {
	if n == nil {
		return nil
	}
	return &api.NextStep{StepId: n.StepID, Reason: api.NextStepReason(n.Reason)}
}

func progressJSON(p path.Progress) api.Progress {
	return api.Progress{PeriodDone: p.PeriodDone, PeriodTotal: p.PeriodTotal, OverallDone: p.OverallDone, OverallTotal: p.OverallTotal}
}

// pathViewJSON converts a loaded path to PathView.
func pathViewJSON(l *path.Loaded, readOnly, staleRequests bool) api.PathView {
	v := api.PathView{Today: date(l.Today), StartDate: date(l.Start), Day: l.Day, State: api.PathViewState(l.State),
		CurrentPeriod: api.PeriodId(l.CurrentPeriod), NextStep: nextJSON(l.Next), Waiting: append([]string{}, l.Waiting...),
		Progress: progressJSON(l.Progress), ReadOnly: readOnly, StaleRequests: staleRequests}
	v.Employee.UserId = l.Employee.UserID
	v.Employee.DisplayName = l.Employee.DisplayName
	v.Employee.Track = api.TrackRef{Id: l.Track.ID, Title: l.Track.Title, Synthetic: l.Track.Synthetic, Disclaimer: l.Track.Disclaimer}
	v.Periods = []api.Period{}
	for _, p := range l.Periods {
		v.Periods = append(v.Periods, api.Period{Id: api.PeriodId(p.ID), Title: p.Title, FromDay: p.FromDay, ToDay: p.ToDay,
			State: api.PeriodState(p.State), Done: p.Done, Total: p.Total})
	}
	v.Steps = []api.PathStep{}
	for _, s := range l.Steps {
		var req *path.Request
		if r, ok := l.Requests[s.ID]; ok {
			req = &r
		}
		v.Steps = append(v.Steps, pathStepJSON(s, req))
	}
	for _, r := range l.Requests {
		if r.Status == "pending" {
			v.HasPendingRequests = true
		}
	}
	return v
}

// writePathError maps errors of path loading.
func writePathError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, guard.ErrNotFound), errors.Is(err, employees.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
	case errors.Is(err, guard.ErrDenied):
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Недостаточно прав", nil)
	default:
		if _, ok := mcpclient.AsToolError(err); ok {
			writeToolError(w, r, err, "Не получилось загрузить твой путь — попробуй ещё раз")
			return
		}
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Не получилось загрузить твой путь — попробуй ещё раз", map[string]any{"retryable": true})
	}
}

// GetPath is S1: the intern's own path (11 §1.10).
func (s *Server) GetPath(w http.ResponseWriter, r *http.Request) {
	a, _ := reqctx.Actor(r.Context())
	if a.Role != actor.Intern {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Это экран стажёра. Прогресс стажёров — на экране «Мои стажёры»", nil)
		return
	}
	stale := false
	if s.Sync != nil {
		if err := s.Sync(r.Context(), a.UserID); err != nil {
			stale = true // eventual consistency: the next read retries (11 §3.6)
		}
	}
	l, err := s.Path.Load(r.Context(), a, "")
	if err != nil {
		writePathError(w, r, err)
		return
	}
	s.afterPathViewed(r.Context(), a, l)
	writeJSON(w, http.StatusOK, pathViewJSON(l, false, stale))
}

// afterPathViewed updates app.path_period_stats and publishes path_viewed
// (at most once per 10 min) and period_started (first view of a period).
func (s *Server) afterPathViewed(ctx context.Context, a actor.Actor, l *path.Loaded) {
	now := s.Clock.Now()
	for _, p := range l.Periods {
		_, _ = s.Pool.Exec(ctx, `INSERT INTO app.path_period_stats (user_id, period, done, total, updated_at) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (user_id, period) DO UPDATE SET done = EXCLUDED.done, total = EXCLUDED.total, updated_at = EXCLUDED.updated_at`,
			a.UserID, p.ID, p.Done, p.Total, now)
	}
	if ok, _ := s.Redis.SetNX(ctx, "dedupe:period:"+a.UserID+":"+l.CurrentPeriod, "1", 0).Result(); ok {
		_ = s.Events.Publish(ctx, events.Event{Name: "period_started", Payload: map[string]any{"period": l.CurrentPeriod}})
	}
	if ok, _ := s.Redis.SetNX(ctx, "dedupe:path_viewed:"+a.UserID, "1", 10*time.Minute).Result(); ok {
		p := map[string]any{"period": l.CurrentPeriod, "day": l.Day, "period_done": l.Progress.PeriodDone, "period_total": l.Progress.PeriodTotal}
		if l.Next != nil {
			p["next_step_id"], p["next_reason"] = l.Next.StepID, l.Next.Reason
		}
		_ = s.Events.Publish(ctx, events.Event{Name: "path_viewed", Payload: p})
	}
}

// GetPathStep returns one step of the intern's path.
func (s *Server) GetPathStep(w http.ResponseWriter, r *http.Request, stepID api.StepId) {
	a, _ := reqctx.Actor(r.Context())
	if a.Role != actor.Intern {
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Такого шага в твоём пути нет", nil)
		return
	}
	st, err := s.Path.GetStep(r.Context(), a, stepID)
	if err != nil {
		writeToolError(w, r, err, notFoundStep(err))
		return
	}
	reqs, _ := s.Path.Requests(r.Context(), a.UserID)
	var req *path.Request
	if x, ok := reqs[st.ID]; ok {
		req = &x
	}
	writeJSON(w, http.StatusOK, pathStepJSON(st, req))
}

func notFoundStep(err error) string {
	if mcpclient.IsCode(err, "NOT_FOUND") {
		return "Такого шага в твоём пути нет"
	}
	return ""
}

// SetPathStepStatus is «Отметить пройденным» / «Вернуть в работу» (11 §1.10).
func (s *Server) SetPathStepStatus(w http.ResponseWriter, r *http.Request, stepID api.StepId) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	if a.Role != actor.Intern {
		_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": "checklist.set_step_status", "object": stepID}})
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Наставник видит прогресс только для чтения", nil)
		return
	}
	if !s.limit(w, r, "act") {
		return
	}
	var body api.StepStatusUpdate
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	st, err := s.Path.GetStep(ctx, a, stepID)
	if err != nil {
		writeToolError(w, r, err, notFoundStep(err))
		return
	}
	reason := fsm.UserReasonFor(string(body.ExpectedStatus), string(body.Status))
	if err := s.Guard.Authorize(ctx, a, guard.PathUpdate, guard.Object{Reason: reason, HelpKind: st.HelpKind}); err != nil {
		writeError(w, r, http.StatusConflict, "TRANSITION_NOT_ALLOWED", "Этот шаг закроется сам, когда заявку одобрят", map[string]any{"current_status": st.Status})
		return
	}
	updated, changed, replay, err := s.Path.SetStatus(ctx, a, stepID, string(body.Status), string(body.ExpectedStatus), reason, body.IdempotencyKey.String())
	if err != nil {
		te, _ := mcpclient.AsToolError(err)
		switch {
		case te != nil && te.Code == "STATUS_CONFLICT":
			writeError(w, r, http.StatusConflict, "STATUS_CONFLICT", "Шаг уже обновлён", map[string]any{"current_status": te.Details["current_status"]})
		case te != nil && te.Code == "TRANSITION_NOT_ALLOWED":
			writeError(w, r, http.StatusConflict, "TRANSITION_NOT_ALLOWED", "Так сменить статус шага нельзя", map[string]any{"current_status": te.Details["current_status"]})
		default:
			writeToolError(w, r, err, notFoundStep(err))
		}
		return
	}
	l, err := s.Path.Load(ctx, a, "")
	if err != nil {
		writePathError(w, r, err)
		return
	}
	if changed && !replay {
		s.stepEvents(ctx, l, updated, reason)
	}
	reqs := l.Requests
	var req *path.Request
	if x, ok := reqs[updated.ID]; ok {
		req = &x
	}
	res := api.StepStatusResult{Step: pathStepJSON(updated, req), Changed: changed}
	res.PathSummary.State = api.StepStatusResultPathSummaryState(l.State)
	res.PathSummary.NextStep = nextJSON(l.Next)
	res.PathSummary.Waiting = append([]string{}, l.Waiting...)
	res.PathSummary.Progress = progressJSON(l.Progress)
	writeJSON(w, http.StatusOK, res)
}

// stepEvents publishes step_started / step_done / step_reopened (11 §1.14).
func (s *Server) stepEvents(ctx context.Context, l *path.Loaded, st path.Step, reason string) {
	switch reason {
	case fsm.UserStart:
		_ = s.Events.Publish(ctx, events.Event{Name: "step_started", Payload: map[string]any{"step_id": st.ID, "period": st.Period}})
	case fsm.UserReopen:
		_ = s.Events.Publish(ctx, events.Event{Name: "step_reopened", Payload: map[string]any{"step_id": st.ID}})
	case fsm.UserMarkDone:
		onTime := true
		for _, p := range l.Periods {
			if p.ID == st.Period && p.ToDay != nil {
				onTime = l.Day <= *p.ToDay
			}
		}
		_ = s.Events.Publish(ctx, events.Event{Name: "step_done", Payload: map[string]any{"step_id": st.ID, "period": st.Period,
			"source": "user", "day": l.Day, "on_time": onTime}})
	}
}

// ListMentorInterns is «Мои стажёры» (11 §1.10, 10 §12.2).
func (s *Server) ListMentorInterns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	if a.Role != actor.Mentor {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Экран доступен наставникам", nil)
		return
	}
	interns, err := s.Employees.InternsOf(ctx, a.UserID)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	if s.Sync != nil {
		for _, e := range interns {
			_ = s.Sync(ctx, e.UserID)
		}
	}
	items := []api.MentorIntern{}
	today := clock.Today(s.Clock)
	for _, e := range interns {
		l, err := s.Path.Load(ctx, a, e.UserID)
		if err != nil {
			writePathError(w, r, err)
			return
		}
		mi := api.MentorIntern{UserId: e.UserID, DisplayName: e.DisplayName, CurrentPeriod: api.PeriodId(l.CurrentPeriod), Day: l.Day,
			Progress: progressJSON(l.Progress), UpcomingTimeOff: []api.TimeOffBrief{},
			Track: api.TrackRef{Id: l.Track.ID, Title: l.Track.Title, Synthetic: l.Track.Synthetic, Disclaimer: l.Track.Disclaimer}}
		_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM app.access_requests WHERE user_id = $1 AND status = 'pending'`, e.UserID).Scan(&mi.PendingAccessRequests)
		rows, err := s.Pool.Query(ctx, `SELECT external_id, date_from, date_to FROM app.time_off_requests
			WHERE user_id = $1 AND status = 'registered' AND date_to >= $2 ORDER BY date_from LIMIT 2`, e.UserID, today)
		if err == nil {
			for rows.Next() {
				var b api.TimeOffBrief
				var from, to time.Time
				if rows.Scan(&b.RequestId, &from, &to) == nil {
					b.DateFrom, b.DateTo = date(from), date(to)
					mi.UpcomingTimeOff = append(mi.UpcomingTimeOff, b)
				}
			}
			rows.Close()
		}
		items = append(items, mi)
	}
	writeJSON(w, http.StatusOK, api.MentorInternList{Items: items})
}

// GetMentorInternPath is the read-only path of a mentee (AC-P20).
func (s *Server) GetMentorInternPath(w http.ResponseWriter, r *http.Request, userID api.UserId) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	if a.Role != actor.Mentor {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Экран доступен наставникам", nil)
		return
	}
	if err := s.Guard.Authorize(ctx, a, guard.MentorDashboard, guard.Object{EmployeeID: userID}); err != nil {
		_ = s.Events.Publish(ctx, events.Event{Name: "permission_denied", Payload: map[string]any{"tool": "checklist.get_path", "object": userID}})
		writeGuardError(w, r, err, "Недостаточно прав")
		return
	}
	if s.Sync != nil {
		_ = s.Sync(ctx, userID)
	}
	l, err := s.Path.Load(ctx, a, userID)
	if err != nil {
		writePathError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pathViewJSON(l, true, false))
}
