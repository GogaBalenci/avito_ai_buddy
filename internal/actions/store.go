// Package actions holds drafts of requests and their confirmation (10 §8.1,
// 09 §8.1): the orchestrator never writes to a corporate service from a chat
// message; it creates a draft, and only POST /api/actions/{id}/confirm calls
// the service — idempotently by action_id.
package actions

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/GogaBalenci/avito_ai_buddy/internal/clock"
)

// Draft states. "expired" is computed from active + expires_at, never stored.
const (
	StateActive     = "active"
	StateConfirming = "confirming"
	StateConfirmed  = "confirmed"
	StateCancelled  = "cancelled"
	StateReplaced   = "replaced"
	StateExpired    = "expired"
)

// Lifetimes: a draft lives 15 min (expires_at), the Redis key 24 h, so an
// expired draft (410) differs from a missing one (404).
const (
	DraftLife = 15 * time.Minute
	KeyTTL    = 24 * time.Hour
)

// Draft is action:{action_id}.
type Draft struct {
	ActionID   string            `json:"action_id"`
	UserID     string            `json:"user_id"`
	Kind       string            `json:"kind"` // time_off | access
	Params     map[string]string `json:"params"`
	ParamsHash string            `json:"params_hash"`
	Summary    map[string]any    `json:"summary"`
	Notices    []string          `json:"notices,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	ExpiresAt  time.Time         `json:"expires_at"`
	State      string            `json:"-"`
	Result     json.RawMessage   `json:"-"`
}

// Errors.
var (
	ErrNotFound = errors.New("draft not found")
)

// Store keeps drafts in Redis as a hash: data, state, user_id, expires_at, result.
type Store struct {
	Redis *redis.Client
	Clock clock.Clock
}

func key(id string) string { return "action:" + id }

// ActiveKey is the pointer to the active draft (one per user for time off,
// one per user and resource for access).
func ActiveKey(userID, kind, resource string) string {
	if kind == "access" {
		return "active_action:" + userID + ":access:" + resource
	}
	return "active_action:" + userID + ":time_off"
}

// Save stores a new draft and replaces the previous active one (A21).
// It returns the id of the replaced draft, if any.
func (s *Store) Save(ctx context.Context, d Draft) (string, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	ptr := ActiveKey(d.UserID, d.Kind, d.Params["resource_id"])
	prev, err := s.Redis.Get(ctx, ptr).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", err
	}
	pipe := s.Redis.TxPipeline()
	pipe.HSet(ctx, key(d.ActionID), "data", data, "state", StateActive, "user_id", d.UserID,
		"expires_at", strconv.FormatInt(d.ExpiresAt.UnixMilli(), 10))
	pipe.Expire(ctx, key(d.ActionID), KeyTTL)
	pipe.Set(ctx, ptr, d.ActionID, KeyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
	}
	replaced := ""
	if prev != "" && prev != d.ActionID {
		ok, err := replaceScript.Run(ctx, s.Redis, []string{key(prev)}).Bool()
		if err == nil && ok {
			replaced = prev
		}
	}
	return replaced, nil
}

var replaceScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'state') == 'active' then
  redis.call('HSET', KEYS[1], 'state', 'replaced')
  return 1
end
return 0`)

// Get loads a draft of the user; a foreign or missing one is ErrNotFound.
func (s *Store) Get(ctx context.Context, id, userID string) (Draft, error) {
	vals, err := s.Redis.HGetAll(ctx, key(id)).Result()
	if err != nil {
		return Draft{}, err
	}
	if len(vals) == 0 || vals["user_id"] != userID {
		return Draft{}, ErrNotFound
	}
	var d Draft
	if err := json.Unmarshal([]byte(vals["data"]), &d); err != nil {
		return Draft{}, err
	}
	d.State = vals["state"]
	if r := vals["result"]; r != "" {
		d.Result = json.RawMessage(r)
	}
	return d, nil
}

// ViewState is the state shown to clients: active after expires_at is expired.
func (s *Store) ViewState(d Draft) string {
	if d.State == StateActive && s.Clock.Now().After(d.ExpiresAt) {
		return StateExpired
	}
	return d.State
}

// begin atomically moves active → confirming (if own and not expired) and
// returns the state found: ok | expired | not_found | <state>.
var beginScript = redis.NewScript(`
local uid = redis.call('HGET', KEYS[1], 'user_id')
if not uid or uid ~= ARGV[1] then return 'not_found' end
local st = redis.call('HGET', KEYS[1], 'state')
if st == 'active' then
  if tonumber(ARGV[2]) > tonumber(redis.call('HGET', KEYS[1], 'expires_at')) then return 'expired' end
  redis.call('HSET', KEYS[1], 'state', 'confirming')
  return 'ok'
end
return st`)

// Begin starts a confirmation.
func (s *Store) Begin(ctx context.Context, id, userID string) (string, error) {
	return beginScript.Run(ctx, s.Redis, []string{key(id)}, userID, s.Clock.Now().UnixMilli()).Text()
}

// Finish stores the result: confirmed for 24 h (a repeated confirm returns it).
func (s *Store) Finish(ctx context.Context, id string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.Redis.HSet(ctx, key(id), "state", StateConfirmed, "result", b).Err()
}

// Revert returns a confirming draft to active (after 422 or 503).
func (s *Store) Revert(ctx context.Context, id string) error {
	return s.Redis.HSet(ctx, key(id), "state", StateActive).Err()
}

var cancelScript = redis.NewScript(`
local uid = redis.call('HGET', KEYS[1], 'user_id')
if not uid or uid ~= ARGV[1] then return 'not_found' end
local st = redis.call('HGET', KEYS[1], 'state')
if st == 'active' then
  if tonumber(ARGV[2]) > tonumber(redis.call('HGET', KEYS[1], 'expires_at')) then return 'expired' end
  redis.call('HSET', KEYS[1], 'state', 'cancelled')
  return 'ok'
end
return st`)

// Cancel moves an active draft to cancelled.
func (s *Store) Cancel(ctx context.Context, id, userID string) (string, error) {
	return cancelScript.Run(ctx, s.Redis, []string{key(id)}, userID, s.Clock.Now().UnixMilli()).Text()
}

// State reads the current state (for waiting on a parallel confirm).
func (s *Store) State(ctx context.Context, id string) string {
	st, _ := s.Redis.HGet(ctx, key(id), "state").Result()
	return st
}

// ActiveDraft returns the id of the user's active time-off draft, if any.
func (s *Store) ActiveDraft(ctx context.Context, userID string) string {
	id, _ := s.Redis.Get(ctx, ActiveKey(userID, "time_off", "")).Result()
	if id == "" || s.State(ctx, id) != StateActive {
		return ""
	}
	return id
}
