-- Schemas mock_* (owner: mcp-mocks). 12-architecture.md §8.3.
CREATE SCHEMA IF NOT EXISTS mock_admin;
CREATE TABLE mock_admin.meta  (key text PRIMARY KEY, value text NOT NULL);
CREATE TABLE mock_admin.flags (                                           -- FlagProvider имитаций
  name           text PRIMARY KEY,                                        -- mock_absences_fault, access_mentor_approval
  value          text NOT NULL,
  allowed_values text[] NOT NULL,
  CHECK (value = ANY (allowed_values))
);

-- checklist
CREATE SCHEMA IF NOT EXISTS mock_checklist;
CREATE TABLE mock_checklist.tracks  (track_id text PRIMARY KEY, title text NOT NULL, synthetic boolean NOT NULL, disclaimer text);
CREATE TABLE mock_checklist.periods (id text PRIMARY KEY, title text NOT NULL, from_day int NOT NULL, to_day int, sort smallint NOT NULL);
CREATE TABLE mock_checklist.steps (
  step_id       text PRIMARY KEY,
  track_id      text NOT NULL REFERENCES mock_checklist.tracks,
  period        text NOT NULL REFERENCES mock_checklist.periods,
  "order"       int  NOT NULL,
  title         text NOT NULL,
  type          text NOT NULL,
  owner         text NOT NULL CHECK (owner IN ('intern', 'mentor', 'lead', 'curator')),
  help          jsonb NOT NULL,                                           -- {kind, kb_doc?, resource?, source_level?}
  checklist_ref text,
  synthetic     boolean NOT NULL
);
CREATE TABLE mock_checklist.progress (
  employee_id   text NOT NULL,
  step_id       text NOT NULL REFERENCES mock_checklist.steps,
  status        text NOT NULL CHECK (status IN ('not_started', 'in_progress', 'waiting', 'done')),
  status_source text NOT NULL CHECK (status_source IN ('user', 'system', 'seed')),
  request_ref   text,
  updated_at    timestamptz NOT NULL,
  PRIMARY KEY (employee_id, step_id)
);
CREATE TABLE mock_checklist.idempotency (
  idempotency_key uuid PRIMARY KEY,
  employee_id     text NOT NULL,
  step_id         text NOT NULL,
  status          text NOT NULL,
  reason          text NOT NULL,
  result          jsonb NOT NULL,
  created_at      timestamptz NOT NULL
);

-- knowledge
CREATE SCHEMA IF NOT EXISTS mock_knowledge;
CREATE TABLE mock_knowledge.docs (
  slug           text PRIMARY KEY,
  title          text NOT NULL,
  kind           text NOT NULL,
  source_level   text NOT NULL,
  dated          text,
  allowed_roles  text[] NOT NULL,
  disclose_title boolean NOT NULL DEFAULT false,
  tracks         text[] NOT NULL DEFAULT '{}',
  periods        text[] NOT NULL DEFAULT '{}',
  tags           text[] NOT NULL DEFAULT '{}',
  conflicts_with text[] NOT NULL DEFAULT '{}',
  test_only      boolean NOT NULL DEFAULT false,
  summary        text NOT NULL CHECK (char_length(summary) <= 300),
  body_md        text NOT NULL,
  tsv            tsvector NOT NULL,     -- A: title, B: tags, C: body; считает загрузчик при INSERT
  title_tsv      tsvector NOT NULL,     -- title + tags, для заголовков закрытых документов
  updated_at     timestamptz NOT NULL
);
CREATE INDEX docs_tsv       ON mock_knowledge.docs USING gin (tsv);
CREATE INDEX docs_title_tsv ON mock_knowledge.docs USING gin (title_tsv);

-- absences
CREATE SCHEMA IF NOT EXISTS mock_absences;
CREATE SEQUENCE mock_absences.request_seq START 102;
CREATE TABLE mock_absences.requests (
  request_id      text PRIMARY KEY DEFAULT 'ABS-' || lpad(nextval('mock_absences.request_seq')::text, 6, '0'),
  employee_id     text NOT NULL,
  type            text NOT NULL DEFAULT 'time_off' CHECK (type = 'time_off'),
  status          text NOT NULL CHECK (status IN ('registered', 'cancelled')),
  date_from       date NOT NULL,
  date_to         date NOT NULL,
  working_days    smallint NOT NULL CHECK (working_days BETWEEN 1 AND 5),
  idempotency_key uuid NOT NULL UNIQUE,                                  -- гонка одного ключа (M-04)
  params_hash     char(64) NOT NULL,
  created_at      timestamptz NOT NULL,
  CHECK (date_to >= date_from)
);
CREATE INDEX absences_employee ON mock_absences.requests (employee_id, date_to);

-- access
CREATE SCHEMA IF NOT EXISTS mock_access;
CREATE SEQUENCE mock_access.request_seq START 102;
CREATE TABLE mock_access.resources (
  resource_id      text PRIMARY KEY,
  title            text NOT NULL,
  kind             text NOT NULL,
  description      text NOT NULL,
  policy           text NOT NULL CHECK (policy IN ('auto', 'mentor', 'timer')),
  access_level     text NOT NULL CHECK (access_level IN ('read', 'write', 'admin')),
  approver         text,
  allowed_roles    text[] NOT NULL,
  deny_explanation text,
  aliases          text[] NOT NULL,
  sort             smallint NOT NULL,
  CHECK (cardinality(allowed_roles) > 0 OR deny_explanation IS NOT NULL),
  CHECK (cardinality(allowed_roles) = 0 OR approver IS NOT NULL)          -- патч 17 §1.16
);
CREATE TABLE mock_access.requests (
  request_id         text PRIMARY KEY DEFAULT 'ACC-' || lpad(nextval('mock_access.request_seq')::text, 6, '0'),
  employee_id        text NOT NULL,
  resource_id        text NOT NULL REFERENCES mock_access.resources,
  status             text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
  policy             text NOT NULL CHECK (policy IN ('auto', 'mentor', 'timer')),   -- базовое правило ресурса
  access_level       text NOT NULL,
  justification      text CHECK (char_length(justification) <= 200),
  idempotency_key    uuid NOT NULL UNIQUE,
  params_hash        char(64) NOT NULL,
  created_at         timestamptz NOT NULL,
  auto_approve_at    timestamptz,
  decided_at         timestamptz,
  decided_by         text CHECK (decided_by IN ('auto', 'timer', 'mentor')),
  decided_by_user_id text,
  reject_reason      text CHECK (reject_reason IN ('not_needed', 'other_resource', 'discuss_1on1')),
  CHECK ((status = 'rejected') = (reject_reason IS NOT NULL))
);
CREATE UNIQUE INDEX access_one_active ON mock_access.requests (employee_id, resource_id)
  WHERE status IN ('pending', 'approved');                               -- вторая линия против дублей (XM-05)
CREATE TABLE mock_access.decisions (
  idempotency_key uuid PRIMARY KEY,
  request_id      text NOT NULL REFERENCES mock_access.requests,
  decision        text NOT NULL CHECK (decision IN ('approve', 'reject')),
  reject_reason   text,
  actor_user_id   text NOT NULL,
  created_at      timestamptz NOT NULL
);
