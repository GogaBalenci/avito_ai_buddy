package httpapi

import (
	"errors"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/GogaBalenci/avito_ai_buddy/internal/actions"
	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
	"github.com/GogaBalenci/avito_ai_buddy/internal/reqctx"
)

func writeActionError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *actions.Error
	if !errors.As(err, &ae) {
		writeError(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "Сервис временно недоступен", map[string]any{"retryable": true})
		return
	}
	extra := map[string]any{}
	if ae.Retryable {
		extra["retryable"] = true
	}
	if len(ae.Fields) > 0 {
		extra["errors"] = ae.Fields
	}
	writeError(w, r, ae.Status, ae.Code, ae.Message, extra)
}

// pendingAction converts a draft to PendingAction / ActionView.
func actionView(st *actions.Store, d actions.Draft) api.ActionView {
	v := api.ActionView{ActionId: openapi_types.UUID(mustUUID(d.ActionID)), Kind: api.RequestKind(d.Kind),
		State: api.ActionViewState(st.ViewState(d)), ExpiresAt: d.ExpiresAt, Summary: summaryJSON(d)}
	if len(d.Result) > 0 {
		var c actions.Confirmed
		if decodeJSON(d.Result, &c) == nil {
			v.Result = &c.Result
		}
	}
	return v
}

// CreateAction validates params and stores a draft (10 §10).
func (s *Server) CreateAction(w http.ResponseWriter, r *http.Request) {
	a, _ := reqctx.Actor(r.Context())
	if a.Role != "intern" {
		writeError(w, r, http.StatusForbidden, "PERMISSION_DENIED", "Оформлять заявки может только стажёр", nil)
		return
	}
	if !s.limit(w, r, "act") {
		return
	}
	var body api.ActionCreateRequest
	if err := decodeBody(r, &body); err != nil {
		writeError(w, r, http.StatusBadRequest, "BAD_REQUEST", "Неверный запрос", nil)
		return
	}
	var d actions.Draft
	var err error
	switch body.Kind {
	case api.RequestKindTimeOff:
		d, err = s.Actions.CreateTimeOff(r.Context(), a, str(body.Params.DateFrom), str(body.Params.DateTo))
	default:
		writeError(w, r, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Заявки на доступ появятся позже", nil)
		return
	}
	if err != nil {
		writeActionError(w, r, err)
		return
	}
	notices := []api.ActionCreatedNotices{}
	for _, n := range d.Notices {
		notices = append(notices, api.ActionCreatedNotices(n))
	}
	writeJSON(w, http.StatusCreated, api.ActionCreated{PendingAction: pendingJSON(s.Actions.Store, d), Notices: notices})
}

// GetAction returns the state of a draft for history cards (A-36).
func (s *Server) GetAction(w http.ResponseWriter, r *http.Request, id api.ActionId) {
	a, _ := reqctx.Actor(r.Context())
	d, err := s.Actions.Get(r.Context(), a, id.String())
	if err != nil {
		writeActionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, actionView(s.Actions.Store, d))
}

// ConfirmAction confirms a draft: the only write path (AG-05).
func (s *Server) ConfirmAction(w http.ResponseWriter, r *http.Request, id api.ActionId) {
	a, _ := reqctx.Actor(r.Context())
	if !s.limit(w, r, "act") {
		return
	}
	res, err := s.Actions.Confirm(r.Context(), a, id.String())
	if err != nil {
		writeActionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, api.ConfirmResponse{Result: res.Result, Reply: *res.Reply})
}

// CancelAction cancels a draft (A19).
func (s *Server) CancelAction(w http.ResponseWriter, r *http.Request, id api.ActionId) {
	a, _ := reqctx.Actor(r.Context())
	if !s.limit(w, r, "act") {
		return
	}
	d, err := s.Actions.Cancel(r.Context(), a, id.String())
	if err != nil {
		writeActionError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, actionView(s.Actions.Store, d))
}
