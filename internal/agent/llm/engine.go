package llm

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
	"github.com/GogaBalenci/avito_ai_buddy/internal/config"
	"github.com/GogaBalenci/avito_ai_buddy/internal/flags"
)

// Health statuses (09 §13.2).
const (
	StatusOK            = "ok"
	StatusDegraded      = "degraded"
	StatusAuthError     = "auth_error"
	StatusMisconfigured = "misconfigured"
	StatusCircuitOpen   = "circuit_open"
	StatusDisabled      = "disabled"
)

// ProviderOpenAI is the value of LLM_PROVIDER for the adapter.
const ProviderOpenAI = "openai_compatible"

// probeTTL: the health probe calls the model at most once per 30 s.
const probeTTL = 30 * time.Second

// Engine combines the client, the breaker and the runtime flags.
type Engine struct {
	Cfg     config.LLMConfig
	Client  *Client
	Breaker *Breaker
	Flags   flags.Provider
	Clock   clock.Clock

	mu        sync.Mutex
	probedAt  time.Time
	probeCode string
}

// New builds the engine from the configuration.
func New(cfg config.LLMConfig, fl flags.Provider, clk clock.Clock) *Engine {
	return &Engine{Cfg: cfg, Flags: fl, Clock: clk, Breaker: &Breaker{Now: clk.Now},
		Client: &Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, Headers: cfg.ExtraHeaders,
			Timeout: cfg.Timeout, HTTP: &http.Client{}}}
}

// Requested reports that the operator asked for a model (LLM_PROVIDER).
func (e *Engine) Requested() bool { return e != nil && e.Cfg.Requested == ProviderOpenAI }

// Misconfigured names the missing variable (AG-24), "" if none.
func (e *Engine) Misconfigured() string {
	if !e.Requested() {
		return ""
	}
	return e.Cfg.Misconfigured
}

func (e *Engine) flag(ctx context.Context, name string) bool {
	if e.Flags == nil {
		return true
	}
	return flags.On(ctx, e.Flags, name)
}

// Enabled: configured and the kill switch llm_enabled is on.
func (e *Engine) Enabled(ctx context.Context) bool {
	return e.Requested() && e.Misconfigured() == "" && e.flag(ctx, "llm_enabled")
}

// RephraseOn: step 2 is allowed by the flag llm_rephrase.
func (e *Engine) RephraseOn(ctx context.Context) bool {
	return e.Enabled(ctx) && e.flag(ctx, "llm_rephrase")
}

func (e *Engine) record(err error) {
	if err == nil {
		e.Breaker.Success()
		return
	}
	e.Breaker.Failure(ReasonOf(err))
}

// Intent runs step 1 through the breaker.
func (e *Engine) Intent(ctx context.Context, ic IntentContext) (IntentResult, error) {
	if !e.Breaker.Allow() {
		return IntentResult{}, &Error{Reason: ReasonCircuitOpen}
	}
	r, err := e.Client.Intent(ctx, ic)
	e.record(err)
	return r, err
}

// Rephrase runs step 2 through the breaker.
func (e *Engine) Rephrase(ctx context.Context, in RephraseInput) (string, error) {
	if !e.Breaker.Allow() {
		return "", &Error{Reason: ReasonCircuitOpen}
	}
	t, err := e.Client.Rephrase(ctx, in)
	e.record(err)
	return t, err
}

// AuthBlocked reports a rejected key: the breaker is open for 10 minutes.
func (e *Engine) AuthBlocked() bool {
	_, auth, _ := e.Breaker.State()
	return auth
}

// Status is the state for GET /api/llm/health (09 §13.2) and the last error code.
func (e *Engine) Status(ctx context.Context) (string, string) {
	switch {
	case !e.Requested() || !e.flag(ctx, "llm_enabled"):
		return StatusDisabled, ""
	case e.Misconfigured() != "":
		return StatusMisconfigured, "missing_" + e.Misconfigured()
	}
	if open, auth, last := e.Breaker.State(); auth {
		return StatusAuthError, ReasonAuth
	} else if open {
		return StatusCircuitOpen, last
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.probedAt.IsZero() || e.Clock.Now().Sub(e.probedAt) >= probeTTL {
		_, err := e.Client.chat(ctx, "Ответь словом ok.", "ok", 0, 1, nil)
		e.probedAt, e.probeCode = e.Clock.Now(), ""
		if err != nil {
			e.probeCode = ReasonOf(err)
			e.Breaker.Failure(e.probeCode)
		}
	}
	switch e.probeCode {
	case "":
		return StatusOK, ""
	case ReasonAuth:
		return StatusAuthError, ReasonAuth
	}
	return StatusDegraded, e.probeCode
}
