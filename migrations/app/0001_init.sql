-- Schema app (owner: api). 12-architecture.md §8.2.
CREATE SCHEMA IF NOT EXISTS app;

CREATE TABLE app.meta (key text PRIMARY KEY, value text NOT NULL);       -- seeded_at

CREATE TABLE app.employees (
  user_id      text PRIMARY KEY CHECK (user_id ~ '^u-[a-z0-9-]{1,40}$'),
  display_name text NOT NULL,                                            -- синтетическое
  role         text NOT NULL CHECK (role IN ('intern', 'mentor')),
  track_id     text,
  start_date   date,                                                     -- today − DEMO_*_DAYS_AGO при сиде
  mentor_id    text REFERENCES app.employees (user_id),
  CHECK ((role = 'intern') = (track_id IS NOT NULL AND start_date IS NOT NULL AND mentor_id IS NOT NULL))
);
CREATE INDEX employees_mentor ON app.employees (mentor_id);

CREATE TABLE app.feature_flags (                                         -- FlagProvider (флаги приложения)
  name           text PRIMARY KEY,                                       -- llm_enabled, llm_rephrase
  value          text NOT NULL,
  allowed_values text[] NOT NULL,
  description    text NOT NULL,
  updated_at     timestamptz NOT NULL,
  CHECK (value = ANY (allowed_values))
);

CREATE TABLE app.chat_messages (
  id                uuid PRIMARY KEY,
  user_id           text NOT NULL REFERENCES app.employees,
  client_message_id uuid NOT NULL,
  kind              text NOT NULL CHECK (kind IN ('text', 'payload')),
  text_masked       text CHECK (char_length(text_masked) <= 1000),        -- после pii.Mask
  payload           text,
  button_label      text,                                                 -- что показать в истории вместо payload
  created_at        timestamptz NOT NULL,
  UNIQUE (user_id, client_message_id),                                    -- вторая линия защиты от дублей
  CHECK ((kind = 'text') = (text_masked IS NOT NULL))
);
CREATE INDEX chat_messages_user_ts ON app.chat_messages (user_id, created_at DESC);

CREATE TABLE app.agent_replies (
  reply_id   uuid PRIMARY KEY,
  message_id uuid NOT NULL UNIQUE REFERENCES app.chat_messages (id) ON DELETE CASCADE,
  user_id    text NOT NULL REFERENCES app.employees,
  intent     text NOT NULL,
  mode       text NOT NULL CHECK (mode IN ('demo', 'llm', 'fallback')),
  body       jsonb NOT NULL,                                              -- AgentReply как отдан клиенту
  created_at timestamptz NOT NULL
);

CREATE TABLE app.feedback (
  reply_id   uuid PRIMARY KEY REFERENCES app.agent_replies ON DELETE CASCADE,
  user_id    text NOT NULL REFERENCES app.employees,
  value      text NOT NULL CHECK (value IN ('up', 'down')),
  created_at timestamptz NOT NULL
);

CREATE TABLE app.action_log (                                             -- журнал действий
  id          bigserial PRIMARY KEY,
  action_id   uuid NOT NULL,
  user_id     text NOT NULL REFERENCES app.employees,
  kind        text NOT NULL CHECK (kind IN ('time_off', 'access', 'access_decision', 'step_status')),
  result      text NOT NULL CHECK (result IN ('completed', 'failed')),
  external_id text,                                                       -- ABS-…, ACC-…, step_id
  error_code  text,
  request_id  text NOT NULL,
  ts          timestamptz NOT NULL
);
CREATE UNIQUE INDEX action_log_completed_once ON app.action_log (action_id) WHERE result = 'completed';

CREATE TABLE app.time_off_requests (                                      -- копия ответа сервиса отсутствий
  external_id  text PRIMARY KEY CHECK (external_id ~ '^ABS-[0-9]{6}$'),
  user_id      text NOT NULL REFERENCES app.employees,
  date_from    date NOT NULL,
  date_to      date NOT NULL,
  working_days smallint NOT NULL CHECK (working_days BETWEEN 1 AND 5),
  status       text NOT NULL CHECK (status IN ('registered', 'cancelled')),
  action_id    uuid UNIQUE,                                               -- NULL у сида
  created_at   timestamptz NOT NULL,
  synced_at    timestamptz NOT NULL,
  CHECK (date_to >= date_from)
);
CREATE INDEX time_off_user_to ON app.time_off_requests (user_id, date_to);

CREATE TABLE app.access_requests (                                        -- копия + связь «заявка ↔ шаг» (наша)
  request_id      text PRIMARY KEY CHECK (request_id ~ '^ACC-[0-9]{6}$'),
  user_id         text NOT NULL REFERENCES app.employees,
  resource_id     text NOT NULL,
  resource_title  text NOT NULL,
  step_id         text,                                                   -- NULL: ресурс не из пути
  status          text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  policy          text NOT NULL CHECK (policy IN ('auto', 'mentor', 'timer')),
  access_level    text NOT NULL CHECK (access_level IN ('read', 'write', 'admin')),
  approver        text,
  justification   text CHECK (char_length(justification) <= 200),
  auto_approve_at timestamptz,
  decided_by      text CHECK (decided_by IN ('auto', 'timer', 'mentor')),
  reject_reason   text CHECK (reject_reason IN ('not_needed', 'other_resource', 'discuss_1on1')),
  action_id       uuid UNIQUE,
  created_at      timestamptz NOT NULL,
  synced_at       timestamptz NOT NULL
);
CREATE INDEX access_requests_user_status ON app.access_requests (user_id, status);

CREATE TABLE app.path_period_stats (                                      -- для метрики «неделя 1 вовремя» (вместо path_steps_cache из 11 §1.14)
  user_id    text NOT NULL REFERENCES app.employees,
  period     text NOT NULL,
  done       integer NOT NULL,
  total      integer NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (user_id, period)
);

CREATE TABLE app.events (                                                 -- EventPublisher; без текста и ПДн (AG-20)
  event_id       uuid PRIMARY KEY,
  ts             timestamptz NOT NULL,
  name           text NOT NULL,
  request_id     text,
  user_id        text,                                                    -- без FK: события переживают сброс демо
  role           text,
  session_id     text,
  mode           text,
  prompt_version text,
  payload        jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX events_name_ts ON app.events (name, ts);
CREATE INDEX events_user_ts ON app.events (user_id, ts);
CREATE UNIQUE INDEX events_doc_conflict_once ON app.events ((payload->>'reply_id'))
  WHERE name = 'doc_conflict_reported';                                   -- AC-K23: ровно один раз
