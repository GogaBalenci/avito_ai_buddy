package httpapi

import (
	"context"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actor"
	"github.com/GogaBalenci/avito_ai_buddy/internal/agent/guard"
	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/employees"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
	"github.com/GogaBalenci/avito_ai_buddy/internal/timeoff/rules"
)

// GetTimeOffConfig gives the form "today" and limits from the server (10 §6.3).
func (s *Server) GetTimeOffConfig(w http.ResponseWriter, _ *http.Request) {
	today := clock.Today(s.Clock)
	writeJSON(w, http.StatusOK, api.TimeOffConfig{Today: date(today), MaxDate: date(rules.MaxDate(today)),
		MaxWorkingDays: rules.MaxWorkingDays, HorizonDays: rules.HorizonDays, WeekendDays: []int{6, 7}})
}

// ListRequests is the history of requests from the local copy (10 §12.3).
func (s *Server) ListRequests(w http.ResponseWriter, r *http.Request, params api.ListRequestsParams) {
	ctx := r.Context()
	a, _ := reqctx.Actor(ctx)
	target := a.UserID
	var emp *employees.Employee
	switch a.Role {
	case actor.Intern:
		if params.EmployeeId != nil && *params.EmployeeId != a.UserID {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
			return
		}
	case actor.Mentor:
		if params.EmployeeId == nil {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
			return
		}
		if err := s.Guard.Authorize(ctx, a, guard.AbsencesList, guard.Object{EmployeeID: *params.EmployeeId}); err != nil {
			writeError(w, r, http.StatusNotFound, "NOT_FOUND", "Не найдено", nil)
			return
		}
		target = *params.EmployeeId
		if e, err := s.Employees.Get(ctx, target); err == nil {
			emp = &e
		}
	}
	stale := false
	if s.Sync != nil && (params.Kind == nil || *params.Kind == api.RequestKindAccess) {
		stale = s.Sync(ctx, target) != nil
	}
	upcoming := params.Scope != nil && *params.Scope == api.Upcoming
	items, err := s.requests(ctx, target, params.Kind, upcoming)
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	if emp != nil {
		for i := range items {
			items[i].Employee = &struct {
				DisplayName string     `json:"display_name"`
				UserId      api.UserId `json:"user_id"`
			}{DisplayName: emp.DisplayName, UserId: emp.UserID}
		}
	}
	list := api.RequestList{Items: items, Total: len(items)}
	if stale {
		list.Stale = &stale
	}
	writeJSON(w, http.StatusOK, list)
}

// requests reads the local copies of time-off and access requests.
func (s *Server) requests(ctx context.Context, userID string, kind *api.RequestKind, upcoming bool) ([]api.RequestView, error) {
	items := []api.RequestView{}
	today := clock.Today(s.Clock)
	if kind == nil || *kind == api.RequestKindTimeOff {
		rows, err := s.Pool.Query(ctx, `SELECT external_id, status, created_at, date_from, date_to, working_days FROM app.time_off_requests
			WHERE user_id = $1 AND (NOT $2 OR (date_to >= $3 AND status = 'registered')) ORDER BY created_at DESC`, userID, upcoming, today)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v api.RequestView
			var from, to time.Time
			var wd int
			if err := rows.Scan(&v.RequestId, &v.Status, &v.CreatedAt, &from, &to, &wd); err != nil {
				rows.Close()
				return nil, err
			}
			v.Kind = api.RequestKindTimeOff
			v.DateFrom, v.DateTo, v.WorkingDays = &openapi_types.Date{Time: from}, &openapi_types.Date{Time: to}, &wd
			items = append(items, v)
		}
		rows.Close()
	}
	if kind == nil || *kind == api.RequestKindAccess {
		rows, err := s.Pool.Query(ctx, `SELECT request_id, status, created_at, resource_id, resource_title, access_level, approver,
			justification, policy, step_id, auto_approve_at, decided_by, reject_reason FROM app.access_requests
			WHERE user_id = $1 AND (NOT $2 OR status = 'pending') ORDER BY created_at DESC`, userID, upcoming)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v api.RequestView
			var res, title, level, policy string
			var by, reason *string
			if err := rows.Scan(&v.RequestId, &v.Status, &v.CreatedAt, &res, &title, &level, &v.Approver, &v.Justification,
				&policy, &v.StepId, &v.AutoApproveAt, &by, &reason); err != nil {
				rows.Close()
				return nil, err
			}
			al, pol := api.AccessLevel(level), api.Policy(policy)
			v.Kind, v.ResourceId, v.ResourceTitle, v.AccessLevel, v.Policy = api.RequestKindAccess, &res, &title, &al, &pol
			if by != nil {
				d := api.RequestViewDecidedBy(*by)
				v.DecidedBy = &d
			}
			if reason != nil {
				rr := api.RequestViewRejectReason(*reason)
				v.RejectReason = &rr
			}
			items = append(items, v)
		}
		rows.Close()
	}
	sortByCreated(items)
	return items, nil
}

func sortByCreated(xs []api.RequestView) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].CreatedAt.After(xs[j-1].CreatedAt); j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}
