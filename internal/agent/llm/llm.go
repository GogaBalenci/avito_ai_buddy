// Package llm is the optional model adapter (09 §3–4, §7, §13): an
// OpenAI-compatible chat API without function calling. Step 1 returns an
// IntentResult JSON, step 2 rephrases a template answer. Every failure falls
// back to the demo rules; a circuit breaker keeps a broken model out of the
// way (AG-16, AG-24).
package llm

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Prompt versions (AG-22): bump when a prompt file changes.
const (
	IntentVersion   = "intent-v2"
	RephraseVersion = "rephrase-v2"
)

//go:embed prompts/*
var promptFS embed.FS

var (
	intentSystem   = mustRead("prompts/intent.system.txt")
	rephraseSystem = mustRead("prompts/rephrase.system.txt")
	intentUser     = template.Must(template.New("intent").Parse(mustRead("prompts/intent.user.tmpl")))
	intentSchema   = mustSchema()
)

func mustRead(name string) string {
	b, err := promptFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func mustSchema() *jsonschema.Schema {
	raw, err := promptFS.ReadFile("prompts/intent-result.v1.json")
	if err != nil {
		panic(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		panic(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("intent-result.v1.json", doc); err != nil {
		panic(err)
	}
	return c.MustCompile("intent-result.v1.json")
}

// IntentSchemaJSON is the schema sent in response_format.
func IntentSchemaJSON() json.RawMessage {
	b, _ := promptFS.ReadFile("prompts/intent-result.v1.json")
	return b
}

// Failure reasons (event llm_fallback.reason).
const (
	ReasonTimeout     = "timeout"
	ReasonHTTP        = "http_error"
	ReasonAuth        = "auth_error"
	ReasonInvalidJSON = "invalid_json"
	ReasonCircuitOpen = "circuit_open"
)

// Error is a model failure.
type Error struct {
	Reason string
	Status int
}

func (e *Error) Error() string { return fmt.Sprintf("llm %s (status %d)", e.Reason, e.Status) }

// ReasonOf returns the failure reason of an error.
func ReasonOf(err error) string {
	var le *Error
	if errors.As(err, &le) {
		return le.Reason
	}
	return ReasonHTTP
}

// Step is a visible step for the prompt context.
type Step struct{ Ref, Title string }

// Resource is an allowed access resource for the prompt context.
type Resource struct{ ID, Title string }

// IntentContext fills intent.user.tmpl (09 §4.2): no names or e-mails.
type IntentContext struct {
	Today, Weekday, Role, Track, PeriodTitle string
	Steps                                    []Step
	Resources                                []Resource
	LastBotQuestion                          string
	MaskedText                               string
}

// Params of IntentResult.
type Params struct {
	Query        string `json:"query,omitempty"`
	StepRef      string `json:"step_ref,omitempty"`
	ResourceHint string `json:"resource_hint,omitempty"`
	DateFrom     string `json:"date_from,omitempty"`
	DateTo       string `json:"date_to,omitempty"`
	Topic        string `json:"topic,omitempty"`
}

// IntentResult is intent-result.v1.json.
type IntentResult struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
	Params     Params  `json:"params"`
}

// Source of a rephrase input.
type Source struct {
	N     int    `json:"n"`
	Title string `json:"title"`
}

// RephraseInput is the draft given to the model (09 §7.1).
type RephraseInput struct {
	Intent       string   `json:"intent"`
	UserQuestion string   `json:"user_question"`
	DraftText    string   `json:"draft_text"`
	Facts        []string `json:"facts"`
	Sources      []Source `json:"sources"`
	MustKeep     []string `json:"must_keep"`
	ActionState  string   `json:"action_state"`
	MaxChars     int      `json:"max_chars"`
}

// Client calls an OpenAI-compatible /chat/completions.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
	Timeout time.Duration
	HTTP    *http.Client
	// jsonObject: the provider rejected json_schema once, use json_object.
	jsonObject atomic.Bool
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	MaxTokens      int            `json:"max_tokens"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chat returns the content of the first choice.
func (c *Client) chat(ctx context.Context, system, user string, temperature float64, maxTokens int, schema json.RawMessage) (string, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		sys := system
		if schema != nil && c.jsonObject.Load() { // json_object: the schema goes into the prompt
			sys += "\nСхема ответа (JSON Schema):\n" + string(schema)
		}
		req := chatRequest{Model: c.Model, Temperature: temperature, MaxTokens: maxTokens,
			Messages: []chatMessage{{Role: "system", Content: sys}, {Role: "user", Content: user}}}
		switch {
		case schema != nil && !c.jsonObject.Load():
			req.ResponseFormat = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "IntentResult", "schema": schema, "strict": false}}
		default:
			req.ResponseFormat = map[string]any{"type": "json_object"}
		}
		body, _ := json.Marshal(req)
		hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", &Error{Reason: ReasonHTTP}
		}
		hr.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			hr.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		for k, v := range c.Headers {
			hr.Header.Set(k, v)
		}
		resp, err := c.httpClient().Do(hr)
		if err != nil {
			if ctx.Err() != nil {
				return "", &Error{Reason: ReasonTimeout}
			}
			return "", &Error{Reason: ReasonHTTP}
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			return "", &Error{Reason: ReasonAuth, Status: resp.StatusCode}
		case resp.StatusCode == http.StatusBadRequest && schema != nil && !c.jsonObject.Load():
			c.jsonObject.Store(true) // json_schema is not supported: json_object + the schema in the prompt
			continue
		case resp.StatusCode >= 300:
			return "", &Error{Reason: ReasonHTTP, Status: resp.StatusCode}
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
			return "", &Error{Reason: ReasonInvalidJSON, Status: resp.StatusCode}
		}
		return out.Choices[0].Message.Content, nil
	}
	return "", &Error{Reason: ReasonHTTP, Status: http.StatusBadRequest}
}

// stripFence removes a ```json fence some models add.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

// Intent runs step 1 (09 §4): temperature 0, 200 tokens, schema check.
func (c *Client) Intent(ctx context.Context, ic IntentContext) (IntentResult, error) {
	var user bytes.Buffer
	if err := intentUser.Execute(&user, ic); err != nil {
		return IntentResult{}, &Error{Reason: ReasonInvalidJSON}
	}
	content, err := c.chat(ctx, intentSystem, user.String(), 0, 200, IntentSchemaJSON())
	if err != nil {
		return IntentResult{}, err
	}
	return ParseIntent(content)
}

// ParseIntent checks the model answer against intent-result.v1.json.
func ParseIntent(content string) (IntentResult, error) {
	content = stripFence(content)
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(content))
	if err != nil {
		return IntentResult{}, &Error{Reason: ReasonInvalidJSON}
	}
	if err := intentSchema.Validate(doc); err != nil {
		return IntentResult{}, &Error{Reason: ReasonInvalidJSON}
	}
	var r IntentResult
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		return IntentResult{}, &Error{Reason: ReasonInvalidJSON}
	}
	return r, nil
}

// Rephrase runs step 2 (09 §7): temperature 0.3, 300 tokens, {"text": …}.
func (c *Client) Rephrase(ctx context.Context, in RephraseInput) (string, error) {
	b, _ := json.Marshal(in)
	content, err := c.chat(ctx, rephraseSystem, string(b), 0.3, 300, nil)
	if err != nil {
		return "", err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stripFence(content)), &out); err != nil {
		return "", &Error{Reason: ReasonInvalidJSON}
	}
	text, ok := out["text"].(string)
	if !ok || len(out) != 1 {
		return "", &Error{Reason: ReasonInvalidJSON}
	}
	return text, nil
}

// Breaker: 3 failures in a row open it for 60 s; an auth error opens it
// for 10 min at once (09 §13.1).
type Breaker struct {
	Now func() time.Time

	mu        sync.Mutex
	fails     int
	openUntil time.Time
	lastCode  string
	auth      bool
}

// Allow reports whether the model may be called now.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.now().Before(b.openUntil)
}

func (b *Breaker) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Success closes the breaker.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fails, b.lastCode, b.auth = 0, "", false
}

// Failure counts a failure.
func (b *Breaker) Failure(reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastCode = reason
	if reason == ReasonAuth {
		b.auth = true
		b.openUntil = b.now().Add(10 * time.Minute)
		return
	}
	b.fails++
	if b.fails >= 3 {
		b.openUntil = b.now().Add(60 * time.Second)
		b.fails = 0
	}
}

// State returns open/auth flags and the last error code.
func (b *Breaker) State() (open, auth bool, last string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.now().Before(b.openUntil), b.auth && b.now().Before(b.openUntil), b.lastCode
}
