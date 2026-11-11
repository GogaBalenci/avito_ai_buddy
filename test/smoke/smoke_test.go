//go:build smoke

package smoke

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	api "github.com/GogaBalenci/avito_ai_buddy/internal/gen/api"
)

func baseURL() string {
	if u := os.Getenv("BASE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8380"
}

type session struct {
	t *testing.T
	c *http.Client
}

func newSession(t *testing.T) *session {
	jar, _ := cookiejar.New(nil)
	return &session{t: t, c: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
}

// do sends a request and returns status and body; v (if not nil) receives JSON.
func (s *session) do(method, path string, body any, v any) (int, string) {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, baseURL()+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("Origin", baseURL())
	}
	start := time.Now()
	resp, err := s.c.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if d := time.Since(start); d > 2*time.Second {
		s.t.Errorf("%s %s took %v (local operations must be fast)", method, path, d)
	}
	if v != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(raw, v); err != nil {
			s.t.Fatalf("%s %s: decode %v: %s", method, path, err, raw)
		}
	}
	return resp.StatusCode, string(raw)
}

func (s *session) must(code int, method, path string, body any, v any) string {
	s.t.Helper()
	got, raw := s.do(method, path, body, v)
	if got != code {
		s.t.Fatalf("%s %s: status %d, want %d: %s", method, path, got, code, raw)
	}
	return raw
}

func (s *session) login(userID string) {
	s.t.Helper()
	s.must(http.StatusOK, "POST", "/api/auth/login", map[string]any{"user_id": userID}, nil)
}

func (s *session) chat(id, field, value string) api.AgentReply {
	s.t.Helper()
	var r api.AgentReply
	s.must(http.StatusOK, "POST", "/api/chat/messages", map[string]any{"client_message_id": id, field: value}, &r)
	return r
}

func action(r api.AgentReply, typ api.ReplyActionType) *api.ReplyAction {
	for i := range r.Actions {
		if r.Actions[i].Type == typ {
			return &r.Actions[i]
		}
	}
	return nil
}

func TestMainScenario(t *testing.T) {
	s := newSession(t)
	s.must(http.StatusOK, "GET", "/healthz", nil, nil)
	s.login("u-intern-be")
	s.must(http.StatusNoContent, "POST", "/api/demo/reset", nil, nil)

	t.Run("S1 my path", func(t *testing.T) {
		s.t = t
		var p api.PathView
		s.must(http.StatusOK, "GET", "/api/path", nil, &p)
		// cm-1.7 is seeded done until S4 (access requests) is connected, so the next step is be-1.11.
		if p.CurrentPeriod != "week1" || p.NextStep == nil || p.NextStep.StepId != "be-1.11" || len(p.Waiting) != 1 || p.Waiting[0] != "cm-1.10" {
			t.Fatalf("path: period %s next %+v waiting %v", p.CurrentPeriod, p.NextStep, p.Waiting)
		}
		r := s.chat(uuid.NewString(), "payload", "step:be-1.12")
		if r.Intent != "step_explain" || action(r, api.MarkDone) == nil || len(r.Sources) == 0 {
			t.Fatalf("step button: %+v", r)
		}
		var st api.PathStep
		s.must(http.StatusOK, "GET", "/api/path/steps/be-1.12", nil, &st)
		if st.Status != "in_progress" {
			t.Fatalf("be-1.12 = %s", st.Status)
		}
		s.must(http.StatusNotFound, "POST", "/api/chat/messages", map[string]any{"client_message_id": uuid.NewString(), "payload": "step:cm-1.1"}, nil)

		key := uuid.NewString()
		body := map[string]any{"status": "done", "expected_status": "in_progress", "idempotency_key": key}
		var res, again api.StepStatusResult
		s.must(http.StatusOK, "POST", "/api/path/steps/be-1.12/status", body, &res)
		s.must(http.StatusOK, "POST", "/api/path/steps/be-1.12/status", body, &again)
		if !res.Changed || res.PathSummary.Progress.PeriodDone != 4 || again.Step.Status != "done" {
			t.Fatalf("mark done: %+v / %+v", res, again)
		}
		s.must(http.StatusConflict, "POST", "/api/path/steps/cm-1.10/status",
			map[string]any{"status": "done", "expected_status": "waiting", "idempotency_key": uuid.NewString()}, nil)

		r = s.chat(uuid.NewString(), "text", "что мне делать дальше?")
		if r.Intent != "my_path" || !strings.Contains(r.Text, "Дальше — «Изучи принципы разработки Авито»") {
			t.Fatalf("my_path: %+v", r)
		}
	})

	t.Run("S3 time off", func(t *testing.T) {
		s.t = t
		id := uuid.NewString()
		r := s.chat(id, "text", "хочу отгул")
		if action(r, api.OpenForm) == nil {
			t.Fatalf("open_form: %+v", r)
		}
		if again := s.chat(id, "text", "хочу отгул"); again.ReplyId != r.ReplyId {
			t.Fatal("a repeated client_message_id must return the same reply")
		}
		var cfg api.TimeOffConfig
		s.must(http.StatusOK, "GET", "/api/time-off/config", nil, &cfg)
		var before api.RequestList
		s.must(http.StatusOK, "GET", "/api/requests?kind=time_off&scope=upcoming", nil, &before)
		day := freeWorkingDay(cfg.Today.Time.AddDate(0, 0, 7), before.Items)

		past := cfg.Today.Time.AddDate(0, 0, -1).Format("2006-01-02")
		raw := s.must(http.StatusUnprocessableEntity, "POST", "/api/actions",
			map[string]any{"kind": "time_off", "params": map[string]any{"date_from": past, "date_to": past}}, nil)
		if !strings.Contains(raw, "DATE_IN_PAST") {
			t.Fatalf("past date: %s", raw)
		}
		var created api.ActionCreated
		s.must(http.StatusCreated, "POST", "/api/actions",
			map[string]any{"kind": "time_off", "params": map[string]any{"date_from": day, "date_to": day}}, &created)
		confirm := "/api/actions/" + created.PendingAction.ActionId.String() + "/confirm"
		var ok1, ok2 api.ConfirmResponse
		s.must(http.StatusOK, "POST", confirm, nil, &ok1)
		s.must(http.StatusOK, "POST", confirm, nil, &ok2)
		if !strings.HasPrefix(ok1.Result.RequestId, "ABS-") || ok2.Result.RequestId != ok1.Result.RequestId {
			t.Fatalf("confirm: %+v / %+v", ok1.Result, ok2.Result)
		}
		var after api.RequestList
		s.must(http.StatusOK, "GET", "/api/requests?kind=time_off&scope=upcoming", nil, &after)
		if len(after.Items) != len(before.Items)+1 || len(after.Items) < 2 {
			t.Fatalf("requests: before %d after %d", len(before.Items), len(after.Items))
		}
		r = s.chat(uuid.NewString(), "text", "у меня температура, хочу отгул")
		if action(r, api.OpenForm) != nil || r.Intent != "out_of_scope" {
			t.Fatalf("sick leave must not open the form: %+v", r)
		}
	})

	t.Run("mentor", func(t *testing.T) {
		m := newSession(t)
		m.login("u-mentor")
		m.must(http.StatusForbidden, "POST", "/api/actions",
			map[string]any{"kind": "time_off", "params": map[string]any{"date_from": "2026-12-01", "date_to": "2026-12-01"}}, nil)
		var list api.MentorInternList
		m.must(http.StatusOK, "GET", "/api/mentor/interns", nil, &list)
		found := false
		for _, it := range list.Items {
			if it.UserId == "u-intern-be" && len(it.UpcomingTimeOff) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("mentor must see the intern's upcoming time off: %+v", list.Items)
		}
		r := m.chat(uuid.NewString(), "text", "что мне делать дальше?")
		if !strings.Contains(r.Text, "сценарий для стажёров") {
			t.Fatalf("mentor on an intern scenario: %+v", r)
		}
	})
}

// freeWorkingDay returns the first working day from d that no request covers.
func freeWorkingDay(d time.Time, busy []api.RequestView) string {
	for {
		wd := d.Weekday()
		free := wd != time.Saturday && wd != time.Sunday
		for _, b := range busy {
			if b.DateFrom != nil && b.DateTo != nil && !d.Before(b.DateFrom.Time) && !d.After(b.DateTo.Time) {
				free = false
			}
		}
		if free {
			return d.Format("2006-01-02")
		}
		d = d.AddDate(0, 0, 1)
	}
}
