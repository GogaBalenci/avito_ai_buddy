# Архитектура AI-buddy («Авиитик Витя»)

Версия 1.0 · 11.11.2026 · статус: готово к C1
Источники: `02-decisions.md` v3.8, `09-spec-agent.md` v1.2, `10-spec-s3-time-off.md` v1.2, `11-spec-s1-s2-s4.md` v1.1, `17-qa-patch.md` v1.0 (до переноса — источник правды по своим пунктам), `16-qa-webinar.md`.
Здесь окончательно зафиксированы структура репозитория, компоненты, OpenAPI, контракты MCP, схема БД и ключи Redis, а также имена `MCP_*_URL` и эндпоинтов чата. Если имена в `09`–`11` расходятся с этим файлом, прав этот файл (так сказано в самих спецификациях). Список правок к ним — §16, переносится промтом 3.0. Матрица прав — `13-permissions.md` (v1.0).

---

## 1. Решения коротко

Обозначения критериев: **Д** — допуск, **В** — внедряемость (30%), **UX** — пользовательский опыт (25%), **Р** — работоспособность (20%), **Т** — техническая реализация (15%), **Б** — безопасность (5%).

| # | Решение | Почему | Критерий |
|---|---|---|---|
| 1 | Monorepo: Go-модуль в корне (`cmd/`, `internal/`), фронт в `web/`, контракты в `contracts/`, данные в `content/` | Эксперт видит всё в одном клоне; один `docker compose up` | Д, Т |
| 2 | Два Go-бинарника: `api` и `mcp-mocks`; граница между ними проверяет линтер (depguard) | Имитации отделены от прикладного кода: по Q&A их код в порог покрытия не входит, а замена имитации на реальный сервис не трогает `api` | В, Т |
| 3 | Оркестратор знает сервисы только через `ToolClient` + `MCP_*_URL` и проверяет ответы по **нашим** контрактам из `contracts/` | Замена имитации = смена адреса; расхождение реального сервиса с контрактом даёт понятную ошибку, а не неверный ответ стажёру | В, Р |
| 4 | MCP: официальный Go SDK (`github.com/modelcontextprotocol/go-sdk`, v1.x), Streamable HTTP, без сессий (`Stateless`) | Стандартный протокол, к которому у Авито есть MCP (Q&A); без сессий нечего терять при рестарте | В |
| 5 | Один PostgreSQL: схема `app` у `api`, схемы `mock_*` у `mcp-mocks`; `api` в `mock_*` не ходит (архитектурный тест) | Один контейнер БД вместо пяти; граница сервисов видна в SQL | Д, В |
| 6 | Флаги имитаций (`mock_absences_fault`, `access_mentor_approval`) хранит `mcp-mocks`; демо-панель меняет их через `/internal/demo/flags` | Флаг имитации — свойство имитации; в проде его нет, `api` о нём не знает | В, Б |
| 7 | OpenAPI 3.0.3 → `oapi-codegen` (strict server, chi) + валидатор запросов по той же спецификации; фронт — `openapi-typescript` + Zod с `satisfies` | Одна спецификация задаёт и код, и проверку тел (`additionalProperties: false` → 400). OpenAPI 3.1 генератор поддерживает не полностью | Т, Б |
| 8 | `web` — nginx со статикой и прокси `/api` → один origin; проверка `Origin` = same-origin по `Host` | Cookie и CSRF работают и на `localhost`, и на `127.0.0.1` — эксперт не получит 403 на первом же POST | Д, Б |
| 9 | Наружу публикуется один порт (`WEB_PORT`, по умолчанию 8380); Grafana — `GRAFANA_PORT` только в профиле | Меньше конфликтов портов на машине эксперта | Д |
| 10 | Префикс реестра образов `REGISTRY_PREFIX` (по умолчанию `docker.io/`, запасной `mirror.gcr.io/`) | Docker Hub может быть недоступен из России — переключение одной строкой в `.env` | Д |
| 11 | Тесты и покрытие запускаются теми же командами в CI и у эксперта: `docker compose --profile test run --rm test-go` / `test-web` | Воспроизводимые отчёты без локального Go и Node; PostgreSQL из compose, без testcontainers | Т |
| 12 | Порог Go — по пакетам приложения без `internal/mcpmocks`, `cmd/mcp-mocks`, сгенерированного кода; отчёт по имитациям — отдельный, справочный | Прямой ответ эксперта на Q&A | Т |
| 13 | Данные (`content/`), миграции (`migrations/`) и контракты (`contracts/`) встраиваются в бинарники (`go:embed`) | Не нужны тома и пути, одинаково на Windows, Linux и macOS | Д |
| 14 | Сиды — при первом старте; сброс — кнопкой; даты относительно сегодняшнего дня | Повторный `docker compose up` не стирает то, что эксперт уже сделал; проверка через неделю работает | Р, UX |
| 15 | Новые эндпоинты сверх спецификаций: `/api/auth/*`, `/api/me`, `/api/client-events` | Вход по тестовой учётке и события «Скопировать», «Сообщить о расхождении» нужны сценариям, а в `09`–`11` им не назначены пути | Р |

---

## 2. Структура репозитория

```
avito-ai-buddy/
├── README.md                      # запуск, учётки, проверка S1–S4, тесты, LLM
├── LICENSE                        # MIT
├── .env.example                   # §4.3, без секретов
├── .gitattributes                 # * text=auto eol=lf
├── .gitignore                     # .env, coverage/, web/coverage/, node_modules/
├── .golangci.yml                  # линтеры + depguard (границы, §2.1)
├── Makefile                       # только удобство: up, down, test, smoke, gen
├── docker-compose.yml             # §4
├── compose.dev.yaml               # локально: публикует postgres/redis на 127.0.0.1
├── go.mod / go.sum                # модуль github.com/<owner>/avito-ai-buddy
│
├── api/
│   ├── openapi.yaml               # §6 — единственный источник HTTP-контракта
│   └── oapi-codegen.yaml          # strict-server + chi + models
├── contracts/                     # JSON Schema MCP-инструментов (§7)
│   ├── embed.go                   # package contracts: go:embed, Bundle(), Validator()
│   ├── common/{actor,error}.v1.json
│   ├── checklist/…  knowledge/…  absences/…  access/…
├── content/                       # данные корпоративных сервисов-имитаций (§8.6)
│   ├── embed.go                   # package content: go:embed
│   ├── tracks/{track.schema.json,common.yaml,backend.yaml,frontend.yaml}
│   ├── kb/*.md                    # 29 документов (26 + 3 из патча 17)
│   ├── access/catalog.yaml        # 7 ресурсов с access_level/approver
│   └── seeds/employees.yaml       # 3 тестовые учётки, связь наставник–стажёр
├── migrations/
│   ├── embed.go                   # package migrations
│   ├── app/0001_init.sql …        # схема app (применяет api)
│   └── mocks/0001_init.sql …      # схемы mock_* (применяет mcp-mocks)
│
├── cmd/
│   ├── api/main.go                # ≤ 20 строк: config → app.Run
│   └── mcp-mocks/main.go          # ≤ 20 строк: config → mcpmocks.Run
├── internal/
│   ├── app/                       # сборка зависимостей api, HTTP-сервер, graceful shutdown
│   ├── config/                    # env → Config, проверки, LLM misconfigured → demo
│   ├── gen/api/                   # сгенерировано oapi-codegen (вне порога)
│   ├── httpapi/                   # реализация StrictServerInterface, маппинг ошибок
│   ├── auth/                      # тестовый SSO, сессии в Redis, Origin, rate-limit
│   │   └── sysactor/              # конструктор актора system; импортирует только access/sync (depguard)
│   ├── agent/
│   │   ├── intent/  rules/        # IntentResult, правила demo, rules/intents.ru.yaml
│   │   ├── llm/                   # LLMClient: openai_compatible, breaker, health
│   │   ├── router/                # намерение → инструменты (09 §5)
│   │   ├── reply/                 # шаблоны, ReplyGuard, AgentReply
│   │   ├── guard/                 # роль × инструмент × объект (слой 2)
│   │   ├── pii/                   # маскирование
│   │   └── prompts/*.ru.tmpl      # go:embed, prompt_version
│   ├── actions/                   # черновики (Redis), confirm/cancel, модель 10 §8.1
│   ├── path/                      # день, этап, сортировка, «Что дальше», прогресс
│   ├── access/sync/               # заявка → шаг, событие ровно один раз
│   ├── timeoff/rules/             # общий: API + mcp-mocks (в пороге)
│   ├── checklist/fsm/             # общий: API + mcp-mocks (в пороге)
│   ├── tracks/                    # общий загрузчик треков + кросс-тест ссылок (в пороге)
│   ├── kb/                        # общий загрузчик базы знаний (в пороге)
│   ├── employees/                 # общий загрузчик seeds/employees.yaml (в пороге)
│   ├── mcpclient/                 # ToolClient: MCP-клиент, outputSchema, таймаут, _meta
│   ├── events/                    # EventPublisher → app.events
│   ├── flags/                     # FlagProvider → app.feature_flags
│   ├── store/                     # pgx-репозитории схемы app
│   ├── demo/                      # сиды app, сброс (вызывает mcp-mocks admin)
│   ├── clock/                     # Clock (Europe/Moscow), фейк для тестов
│   ├── testkit/                   # pgtest (БД на пакет из шаблона), miniredis, фейки
│   └── mcpmocks/                  # ВСЁ, что относится к имитациям (вне порога)
│       ├── server/                # сборка 4 MCP-endpoint + /internal/demo + /healthz
│       ├── checklist/  knowledge/  absences/  access/
│       ├── catalog/               # загрузчик access/catalog.yaml
│       └── admin/                 # reset, flags (mock_admin.flags)
├── test/smoke/                    # go test -tags smoke: основной сценарий через API
│
├── web/
│   ├── package.json  vite.config.ts  vitest.config.ts  tsconfig.json  eslint.config.js
│   ├── nginx.conf                 # статика, прокси /api и /healthz, CSP
│   └── src/
│       ├── main.tsx  routeTree.gen.ts (вне порога)
│       ├── app/                   # providers, queryClient, router
│       ├── routes/                # __root, login, index (чат + «Мой путь»), requests, kb.$slug,
│       │                          # mentor.interns, mentor.interns.$userId, mentor.inbox
│       ├── features/{auth,chat,path,time-off,access,kb,mentor,demo}/
│       │                          # api.ts · schema.ts (Zod) · hooks · store.ts · components/
│       ├── shared/
│       │   ├── api/client.ts  api/schema.gen.ts (openapi-typescript, вне порога)
│       │   ├── ui/                # токены (2 слоя), Button, Fieldset, Toast, Chip, Skeleton
│       │   └── mascot/            # 5 SVG-состояний Вити
│       └── test/                  # MSW-хендлеры, фикстуры
│
├── deploy/
│   ├── docker/{api,mcp-mocks,web,test-go}.Dockerfile
│   └── grafana/provisioning/{datasources,dashboards}/ + dashboards/buddy.json
├── scripts/                       # POSIX sh: test-go.sh, coverage-check.sh, gen.sh
├── docs/                          # 01–17 (копии из Project knowledge), CJM, скриншоты
├── openspec/                      # specs/*/spec.md, changes/*/{proposal,design,tasks}.md
└── .github/workflows/ci.yml       # §12
```

### 2.1 Правила границ (проверяются автоматически)
| Правило | Как проверяется | Зачем |
|---|---|---|
| Пакеты вне `internal/mcpmocks` и `cmd/mcp-mocks` не импортируют `internal/mcpmocks/...` | `depguard` в `.golangci.yml` | Приложение не зависит от имитаций — их можно выбросить |
| `internal/auth/sysactor` (служебная роль `system`) импортирует только `internal/access/sync` | `depguard` | Роль `system` не может появиться из пользовательского запроса (`13` §2) |
| `internal/mcpmocks` не импортирует `internal/agent`, `internal/actions`, `internal/httpapi`, `internal/store` | `depguard` | Имитация не знает об агенте, как и реальный сервис |
| Код приложения не обращается к схемам `mock_*` | Тест `TestAppDoesNotTouchMockSchemas` (поиск `mock_` в `.go` и `.sql` вне `internal/mcpmocks` и `migrations/mocks`) | Граница сервисов видна в SQL |
| Компоненты фронта не используют `dangerouslySetInnerHTML` | eslint `react/no-danger: error` | AG-26 |
| Сгенерированный код совпадает со спецификацией | CI: `scripts/gen.sh && git diff --exit-code` | OpenAPI и код не расходятся |

**Почему общие пакеты лежат вне `mcpmocks`.** `timeoff/rules`, `checklist/fsm`, `tracks`, `kb`, `employees` — прикладная логика и проверка контента. Её используют и `api`, и CI, поэтому по решению `02` она входит в порог. `catalog` нужен только имитации и лежит внутри `mcpmocks`.

---

## 3. Компоненты и взаимодействие

### 3.1 Схема компонентов

```mermaid
flowchart LR
    U([Браузер эксперта]) -->|"http://localhost:WEB_PORT"| WEB
    subgraph net["docker compose · внутренняя сеть"]
        WEB["web · nginx<br/>SPA: React, TanStack, Zustand, Zod<br/>прокси /api и /healthz · CSP"]
        subgraph API["api · Go"]
            MW["middleware<br/>request_id · сессия · Origin · rate-limit"]
            H["httpapi<br/>валидатор OpenAPI → strict handlers"]
            AG["agent<br/>rules · llm · router · reply · guard · pii"]
            AC["actions<br/>черновики, confirm"]
            PT["path<br/>этап, «Что дальше»"]
            SY["access/sync"]
            TC["ToolClient<br/>MCP · outputSchema · таймаут"]
            PR["EventPublisher · FlagProvider<br/>EmployeeProvider · Clock"]
        end
        subgraph MK["mcp-mocks · Go · вне порога покрытия"]
            CL["/mcp/checklist"]
            KN["/mcp/knowledge"]
            AB["/mcp/absences"]
            AX["/mcp/access"]
            AD["/internal/demo<br/>reset · flags"]
        end
        PG[("PostgreSQL<br/>app · mock_*")]
        RD[("Redis<br/>сессии · черновики · дубли · лимиты")]
        GF["Grafana<br/>profile observability"]
    end
    LLM[("OpenAI-совместимый LLM<br/>необязательно")]
    WEB --> MW --> H
    H --> AG & AC & PT
    AG --> TC
    AC --> TC
    PT --> SY --> TC
    AG -.->|"LLM_PROVIDER=openai_compatible"| LLM
    TC -->|"MCP_*_URL"| CL & KN & AB & AX
    H -->|"MCP_MOCKS_ADMIN_URL (только демо)"| AD
    MW --> RD
    AC --> RD
    PR --> PG
    CL & KN & AB & AX & AD --> PG
    GF -->|"SQL к app.events"| PG
```

### 3.2 Ответственность компонентов
| Компонент | Отвечает за | Не отвечает за | Замена в проде |
|---|---|---|---|
| `web` | Экраны, состояния, оптимистичные апдейты, Zod-проверка ответов и форм | Права, даты «сегодня», выбор ответа | Встраивается в платформу агентом или виджетом |
| `api/middleware` | `X-Request-Id`, сессия → `actor`, same-origin `Origin` на POST, лимиты 20/мин | Бизнес-правила | Корпоративный SSO вместо тестового входа |
| `api/agent` | Распознавание (правила + LLM), маршрут, guard (слой 2), ответ, ReplyGuard, ПДн | Хранение заявок | Промпты и маршруты переносятся в платформу агентов |
| `api/actions` | Черновики, состояния, повторная проверка при `confirm`, локальные копии заявок | Решение о заявке | — |
| `api/path` | День, этап, порядок шагов, «Что дальше», прогресс | Хранение статусов шагов | — (логика остаётся у нас при замене чек-листа) |
| `api/access/sync` | Статус заявки → статус шага, событие о решении ровно один раз | Решение о заявке | Вебхук сервиса доступов (roadmap) |
| `ToolClient` | MCP-вызов, `_meta.request_id`, `MCP_TIMEOUT_MS`, проверка `outputSchema`, маппинг ошибок | Права | Не меняется |
| `mcp-mocks` | Контракт, права (слой 3), идемпотентность, детерминированные правила, сиды | UI, агент, LLM | Реальные MCP-серверы по тем же контрактам |
| PostgreSQL | Схема `app`: пользователи, копии заявок, диалоги, журнал, события, флаги. Схемы `mock_*`: данные имитаций | — | Корпоративный PostgreSQL |
| Redis | Сессии, черновики, дубли `client_message_id`, контекст диалога, лимиты, дедупликация событий | Долговременные данные | Корпоративный Redis |
| Grafana | Дашборд метрик пользы по `app.events` | — | Корпоративная Grafana, источник — Kafka/ClickHouse |

### 3.3 Подтверждение заявки через все слои
Покажем самый важный путь: три независимые проверки прав и сквозной `request_id`.

```mermaid
sequenceDiagram
    autonumber
    participant UI as web
    participant MW as api: middleware
    participant ACT as api: actions
    participant G as api: guard + rules
    participant TC as ToolClient
    participant M as mcp-mocks /mcp/absences
    participant PG as PostgreSQL
    participant R as Redis
    UI->>MW: POST /api/actions/{id}/confirm (cookie, Origin)
    MW->>MW: слой 1: сессия → actor, same-origin, лимит, X-Request-Id
    MW->>ACT: confirm(actor, id)
    ACT->>R: action:{id}: свой? active? не истёк? → state=confirming (Lua)
    ACT->>G: слой 2: роль × инструмент; timeoff/rules.Validate(today)
    ACT->>TC: absences.create_request(actor, idempotency_key=id, params_hash), _meta.request_id
    TC->>M: tools/call (таймаут MCP_TIMEOUT_MS)
    M->>M: слой 3: схема входа, guard, ключ идемпотентности, rules, advisory lock
    M->>PG: INSERT mock_absences.requests (уникальный idempotency_key)
    M-->>TC: structuredContent
    TC->>TC: проверка по contracts/absences/create_request.output.v1.json
    ACT->>PG: одна транзакция: app.time_off_requests + app.action_log + app.events
    ACT->>R: state=confirmed + result (TTL 24 ч)
    ACT-->>UI: 200 {result: RequestView, reply: AgentReply}
```

Сбой на шагах 6–9 → черновик возвращается в `active`, ответ 503 `retryable: true`. Повтор с тем же `action_id` не создаёт дубликат: имитация вернёт ту же заявку с `idempotent_replay: true`, а `ON CONFLICT DO NOTHING` в копии не даст второй строки.

### 3.4 Что делает `api` при старте
1. Читает и проверяет конфигурацию. При `openai_compatible` без `LLM_BASE_URL` или `LLM_MODEL` пишет предупреждение и работает как `demo` (AG-24) — это не ошибка старта.
2. Применяет миграции `migrations/app` (goose, таблица версий `app.goose_db_version`).
3. Если в `app.meta` нет `seeded_at`, применяет сиды (`internal/demo`).
4. Загружает `intents.ru.yaml`, промпты и контракты; ошибка — сервис не стартует (это ловит CI).
5. `/healthz` зелёный, если отвечают PostgreSQL и Redis. Недоступность `mcp-mocks` здоровье `api` не роняет: чат и экраны отвечают понятной ошибкой с «Повторить».

`mcp-mocks` при старте: миграции `migrations/mocks` → загрузка `content/` (треки, база знаний, каталог; ошибка → не стартует, P-A15) → сиды, если их нет → `/healthz`.

---

## 4. Запуск и окружение

### 4.1 Сервисы `docker-compose.yml`
| Сервис | Образ / сборка | Порт внутри | Наружу | Healthcheck | Зависит от |
|---|---|---|---|---|---|
| `postgres` | `${REGISTRY_PREFIX}library/postgres` (alpine, multi-arch), том `pgdata` | 5432 | нет (`compose.dev.yaml`: `127.0.0.1:55432`) | `pg_isready` | — |
| `redis` | `${REGISTRY_PREFIX}library/redis` (alpine), без сохранения на диск | 6379 | нет (`compose.dev.yaml`: `127.0.0.1:56379`) | `redis-cli ping` | — |
| `mcp-mocks` | `deploy/docker/mcp-mocks.Dockerfile` | 8090 | **нет** (вызывает только `api`) | `GET /healthz` | postgres |
| `api` | `deploy/docker/api.Dockerfile` | 8080 | нет (через `web`) | `GET /healthz` | postgres, redis, mcp-mocks (`service_healthy`) |
| `web` | `deploy/docker/web.Dockerfile` (сборка Vite → nginx alpine) | 80 | `${WEB_PORT:-8380}` | `GET /` | api |
| `grafana` | `${REGISTRY_PREFIX}grafana/grafana-oss`, профиль `observability` | 3000 | `${GRAFANA_PORT:-8381}` | `GET /api/health` | postgres |
| `test-go` | `deploy/docker/test-go.Dockerfile` (golang, Debian — нужен `-race`), профиль `test`, том `./coverage:/out` | — | — | — | postgres |
| `test-web` | node alpine, профиль `test`, том `./web/coverage` | — | — | — | — |
| `smoke` | тот же образ, что `test-go`, профиль `test`; `go test -tags smoke ./test/smoke`, `BASE_URL=http://web` | — | — | — | web |

Версии образов фиксирует каркас (C0) и выносит в таблицу в README; здесь только требования: базовые образы multi-arch (amd64 и arm64), сборки многостадийные, Go-бинарники статические (`CGO_ENABLED=0`) в distroless/alpine.

### 4.2 Команды (их же выполняет CI)
```bash
docker compose up --build --wait                               # запуск
docker compose --profile test run --rm test-go                 # Go: тесты + coverage/go-app.*, coverage/go-mocks.*
docker compose --profile test run --rm test-web                # фронт: тесты + web/coverage/*
docker compose --profile test run --rm smoke                   # основной сценарий через API
docker compose --profile observability up -d grafana           # дашборд: http://localhost:8381
```

### 4.3 Переменные окружения (`.env.example`, имена окончательные)
```dotenv
# --- Запуск ---
WEB_PORT=8380                       # единственный опубликованный порт приложения (не 5000/7000 — AirPlay на macOS)
GRAFANA_PORT=8381                   # только профиль observability
REGISTRY_PREFIX=docker.io/          # если Docker Hub недоступен: mirror.gcr.io/
TZ=Europe/Moscow                    # «сегодня» считает только сервер

# --- Хранилища (внутренняя сеть compose) ---
POSTGRES_USER=buddy
POSTGRES_PASSWORD=buddy_local_only  # локальный стенд; в проде — секрет из хранилища
POSTGRES_DB=buddy
DATABASE_URL=postgres://buddy:buddy_local_only@postgres:5432/buddy?sslmode=disable
REDIS_URL=redis://redis:6379/0

# --- Корпоративные сервисы (MCP). Замена имитации на реальный сервис = смена адреса ---
MCP_CHECKLIST_URL=http://mcp-mocks:8090/mcp/checklist
MCP_KNOWLEDGE_URL=http://mcp-mocks:8090/mcp/knowledge
MCP_ABSENCES_URL=http://mcp-mocks:8090/mcp/absences
MCP_ACCESS_URL=http://mcp-mocks:8090/mcp/access
MCP_TIMEOUT_MS=3000
MCP_MOCKS_ADMIN_URL=http://mcp-mocks:8090/internal   # только демо: сброс и флаги имитаций

# --- LLM (необязательно; без ключа всё работает в demo) ---
LLM_PROVIDER=demo                   # demo | openai_compatible
LLM_BASE_URL=
LLM_API_KEY=
LLM_MODEL=
LLM_EXTRA_HEADERS=
LLM_TIMEOUT_SECONDS=10

# --- Демо ---
DEMO_TOOLS_ENABLED=true             # /api/demo/* и /internal/demo/*; в проде false
DEMO_INTERN_BE_DAYS_AGO=3
DEMO_INTERN_FE_DAYS_AGO=10
ACCESS_TIMER_MINUTES=2
PATH_POLL_INTERVAL_MS=10000

# --- Поиск и сессии ---
KB_R_HIGH=0.1                       # подбираются 13.11 (KM-13)
KB_R_LOW=0.03
SESSION_TTL_HOURS=12
COOKIE_SECURE=false                 # true за HTTPS
ALLOWED_ORIGINS=                    # необязательно: доп. origin через запятую; по умолчанию same-origin
LOG_LEVEL=info
```

Кто что читает: `api` — всё, кроме `ACCESS_TIMER_MINUTES`; `mcp-mocks` — `DATABASE_URL`, `TZ`, `ACCESS_TIMER_MINUTES`, `DEMO_TOOLS_ENABLED`, `LOG_LEVEL`. Секретов в репозитории нет; `.env` в `.gitignore`; ключ LLM не попадает в логи и в `/api/llm/health`.

### 4.4 Зафиксированные имена (закрывает открытый вопрос `02`)
| Что | Имя |
|---|---|
| Адреса сервисов | `MCP_CHECKLIST_URL`, `MCP_KNOWLEDGE_URL`, `MCP_ABSENCES_URL`, `MCP_ACCESS_URL` |
| Таймаут MCP | `MCP_TIMEOUT_MS` (по умолчанию 3000) |
| Демо-админка имитаций | `MCP_MOCKS_ADMIN_URL` |
| Пути MCP-endpoint | `/mcp/checklist`, `/mcp/knowledge`, `/mcp/absences`, `/mcp/access` |
| Отправка сообщения в чат | `POST /api/chat/messages` |
| История чата | `GET /api/chat/history` |
| Имя инструмента в коде и событиях | `<endpoint>.<tool>`, например `absences.create_request` |

---

## 5. Backend

### 5.1 Интерфейсы — точки замены
```go
// internal/mcpclient
type ToolClient interface {
    // endpoint: checklist | knowledge | absences | access; out проверяется по outputSchema из contracts/
    Call(ctx context.Context, endpoint, tool string, in any, out any) error // ошибки: *ToolError{Code, Retryable, Details}
}

// internal/events
type EventPublisher interface{ Publish(ctx context.Context, e Event) error } // реализация: app.events; в проде — Kafka

// internal/flags
type FlagProvider interface{ Get(ctx context.Context, name string) (string, error) } // читается на каждом запросе

// internal/employees
type EmployeeProvider interface {
    Get(ctx context.Context, userID string) (Employee, error)        // трек, старт, наставник
    InternsOf(ctx context.Context, mentorID string) ([]Employee, error)
}

// internal/clock
type Clock interface{ Now() time.Time } // Europe/Moscow; в SQL время передаётся параметром $now, не now()

// internal/agent/llm
type LLMClient interface {
    Complete(ctx context.Context, req ChatRequest) (ChatResponse, error) // openai_compatible; в тестах всегда заглушка
}
```
У каждого интерфейса есть фейк в `internal/testkit`. Это и делает тесты оркестратора быстрыми, а замену сервисов — локальной.

### 5.2 Цепочка обработки HTTP-запроса
`recover` → `request_id` (из `X-Request-Id` или новый UUID; в ответ и в `context`) → `slog`-логгер с `request_id` → `session` (cookie `buddy_session` → Redis → `actor`; публичные маршруты: `/healthz`, `/api/config`, `/api/llm/health`, `/api/auth/accounts`, `/api/auth/login`) → `origin` (на всех POST: `Origin` = `scheme://Host` запроса или из `ALLOWED_ORIGINS`, иначе 403 `BAD_ORIGIN`) → `ratelimit` (Redis, окно 60 с: 20 сообщений, 20 действий — `POST /api/actions`, `confirm`, `cancel`, статусы шагов, решения наставника) → **валидатор OpenAPI** (kin-openapi: схема тела, параметров, `additionalProperties: false` → 400) → strict handler.

Правило «ровно одно из `text`/`payload`» и грамматика payload проверяются в обработчике (OpenAPI 3.0 + генератор плохо переносят `oneOf` без свойств) — это AG-25, покрыто тестами.

### 5.3 Ошибки
Единый формат `Error` (§6). Маппинг ошибок `ToolClient`:
| MCP | HTTP | `code` |
|---|---|---|
| `VALIDATION_FAILED`, `OVERLAP` | 422 | `VALIDATION_FAILED` + `errors[]` |
| `PERMISSION_DENIED` | 403 | `PERMISSION_DENIED` |
| `NOT_FOUND` | 404 | `NOT_FOUND` |
| `DUPLICATE_ACTIVE`, `ALREADY_DECIDED`, `STATUS_CONFLICT`, `TRANSITION_NOT_ALLOWED`, `IDEMPOTENCY_CONFLICT` | 409 | тот же |
| `UNAVAILABLE`, `INTERNAL`, таймаут, ответ не по `outputSchema` | 503 | `UNAVAILABLE` / `TIMEOUT` / `INTERNAL`, `retryable: true` |

Для чата часть этих ошибок превращается в ответ агента с кодом 200 (например, `oops` + `retry` при недоступном чек-листе) — по таблицам `09` §13 и `10`/`11`.

### 5.4 Библиотеки (версии фиксируются в `go.mod` на C0)
| Задача | Библиотека | Почему |
|---|---|---|
| HTTP-роутер и генерация | `go-chi/chi`, `oapi-codegen` v2 (strict-server), `kin-openapi` (валидатор) | Сервер из спецификации; валидатор по тому же файлу |
| MCP сервер и клиент | `github.com/modelcontextprotocol/go-sdk` v1.x | Официальный SDK |
| JSON Schema | `github.com/santhosh-tekuri/jsonschema` (draft 2020-12) | `$ref` между файлами, `if/then/else` из наших контрактов |
| PostgreSQL | `jackc/pgx/v5` | Стандарт для Go |
| Миграции | `pressly/goose/v3` (embed, отдельная таблица версий на схему) | Две независимые цепочки миграций в одной БД |
| Redis | `redis/go-redis/v9`; в тестах `alicebob/miniredis/v2` | Тесты черновиков без контейнера |
| YAML | `goccy/go-yaml` или `gopkg.in/yaml.v3` | Треки, каталог, словарь |
| Стемминг | Snowball для русского (`09` §6.1; лицензию проверить при подключении) | Правила demo |
| Логи | `log/slog` (JSON) | Стандартная библиотека |

---

## 6. OpenAPI

Файл `api/openapi.yaml`. OpenAPI **3.0.3** — потому что `oapi-codegen` поддерживает 3.1 не полностью. Ниже — полная спецификация; описания сценарного поведения — ссылками на `09`–`11`.

```yaml
openapi: 3.0.3
info:
  title: AI-buddy API («Авиитик Витя»)
  version: 1.0.0
  description: |
    HTTP API веб-интерфейса AI-buddy. Все данные синтетические, инструкции тестовые.
    Сессия — cookie buddy_session (HttpOnly, SameSite=Lax). Изменяющие запросы — только POST
    с проверкой Origin (same-origin). Каждый ответ содержит X-Request-Id.
    Лишние поля в телах запросов отклоняются (400).
servers:
  - url: /
security:
  - session: []
tags:
  - {name: system}
  - {name: auth}
  - {name: chat, description: "09 §2, §9"}
  - {name: actions, description: "Черновики и подтверждение, 10 §8.1"}
  - {name: path, description: "S1, 11 раздел 1"}
  - {name: time-off, description: "S3, 10"}
  - {name: requests}
  - {name: access, description: "S4, 11 раздел 3"}
  - {name: kb, description: "S2, 11 раздел 2"}
  - {name: mentor}
  - {name: demo, description: "Только при DEMO_TOOLS_ENABLED=true, иначе 404"}

paths:
  /healthz:
    get:
      tags: [system]
      operationId: getHealthz
      security: []
      responses:
        '200': {description: "PostgreSQL и Redis доступны", content: {application/json: {schema: {$ref: '#/components/schemas/Health'}}}}
        '503': {description: "Зависимость недоступна", content: {application/json: {schema: {$ref: '#/components/schemas/Health'}}}}
  /api/config:
    get:
      tags: [system]
      operationId: getConfig
      security: []
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/AppConfig'}}}}
  /api/llm/health:
    get:
      tags: [system]
      operationId: getLlmHealth
      security: []
      description: Провайдер, модель и статус. Ключ, заголовки и полный адрес не возвращаются. Пробный вызов не чаще раза в 30 с.
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/LlmHealth'}}}}

  /api/auth/accounts:
    get:
      tags: [auth]
      operationId: listTestAccounts
      security: []
      description: Тестовые учётки для экрана входа в один клик (имитация SSO).
      responses:
        '200':
          description: OK
          content: {application/json: {schema: {type: object, required: [items], properties: {items: {type: array, items: {$ref: '#/components/schemas/TestAccount'}}}}}}
  /api/auth/login:
    post:
      tags: [auth]
      operationId: login
      security: []
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/LoginRequest'}}}
      responses:
        '200':
          description: Сессия создана (Set-Cookie buddy_session)
          content: {application/json: {schema: {$ref: '#/components/schemas/Me'}}}
        '400': {$ref: '#/components/responses/BadRequest'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
  /api/auth/logout:
    post:
      tags: [auth]
      operationId: logout
      responses:
        '204': {description: "Сессия удалена"}
        '401': {$ref: '#/components/responses/Unauthorized'}
  /api/me:
    get:
      tags: [auth]
      operationId: getMe
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/Me'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}

  /api/chat/messages:
    post:
      tags: [chat]
      operationId: postChatMessage
      description: |
        Ровно одно из text / payload (иначе 400). Payload — кнопки: intent:, step:, resource:, clarify: (09 §2.1).
        Повтор client_message_id в течение 10 мин возвращает сохранённый ответ без повторной обработки.
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/ChatMessageRequest'}}}
      responses:
        '200': {description: "Ответ бадди", content: {application/json: {schema: {$ref: '#/components/schemas/AgentReply'}}}}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '429': {$ref: '#/components/responses/TooManyRequests'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/chat/history:
    get:
      tags: [chat]
      operationId: getChatHistory
      parameters:
        - {name: limit, in: query, schema: {type: integer, minimum: 1, maximum: 100, default: 50}}
      responses:
        '200': {description: "Последние сообщения (замаскированные) и сохранённые ответы", content: {application/json: {schema: {$ref: '#/components/schemas/HistoryPage'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}

  /api/actions:
    post:
      tags: [actions]
      operationId: createAction
      description: Проверка параметров и создание черновика (Redis, 15 мин). Один активный черновик отгула на пользователя и один черновик доступа на пару «пользователь — ресурс».
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/ActionCreateRequest'}}}
      responses:
        '201': {description: "Черновик создан", content: {application/json: {schema: {$ref: '#/components/schemas/ActionCreated'}}}}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '409': {$ref: '#/components/responses/Conflict'}
        '422': {$ref: '#/components/responses/Unprocessable'}
        '429': {$ref: '#/components/responses/TooManyRequests'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/actions/{action_id}:
    get:
      tags: [actions]
      operationId: getAction
      parameters: [{$ref: '#/components/parameters/ActionId'}]
      responses:
        '200': {description: "Состояние черновика для карточек в истории чата", content: {application/json: {schema: {$ref: '#/components/schemas/ActionView'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
  /api/actions/{action_id}/confirm:
    post:
      tags: [actions]
      operationId: confirmAction
      description: Единственный путь записи во внешний сервис (AG-05). Повтор confirm подтверждённого черновика → 200 с тем же результатом.
      parameters: [{$ref: '#/components/parameters/ActionId'}]
      responses:
        '200': {description: "Заявка создана", content: {application/json: {schema: {$ref: '#/components/schemas/ConfirmResponse'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '409': {$ref: '#/components/responses/Conflict'}
        '410': {$ref: '#/components/responses/Gone'}
        '422': {$ref: '#/components/responses/Unprocessable'}
        '429': {$ref: '#/components/responses/TooManyRequests'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/actions/{action_id}/cancel:
    post:
      tags: [actions]
      operationId: cancelAction
      parameters: [{$ref: '#/components/parameters/ActionId'}]
      responses:
        '200': {description: "Черновик отменён", content: {application/json: {schema: {$ref: '#/components/schemas/ActionView'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
        '410': {$ref: '#/components/responses/Gone'}
        '429': {$ref: '#/components/responses/TooManyRequests'}

  /api/path:
    get:
      tags: [path]
      operationId: getPath
      description: Сначала синхронизирует заявки S4 (11 §3.6), затем считает этап, «Что дальше», прогресс.
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/PathView'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/path/steps/{step_id}:
    get:
      tags: [path]
      operationId: getPathStep
      parameters: [{$ref: '#/components/parameters/StepId'}]
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/PathStep'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/path/steps/{step_id}/status:
    post:
      tags: [path]
      operationId: setPathStepStatus
      description: Причина выводится сервером (done → user_mark_done; in_progress из done → user_reopen; из not_started → user_start). Шаг с заявкой вручную не закрывается (409).
      parameters: [{$ref: '#/components/parameters/StepId'}]
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/StepStatusUpdate'}}}
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/StepStatusResult'}}}}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '409': {$ref: '#/components/responses/Conflict'}
        '429': {$ref: '#/components/responses/TooManyRequests'}
        '503': {$ref: '#/components/responses/Unavailable'}

  /api/time-off/config:
    get:
      tags: [time-off]
      operationId: getTimeOffConfig
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/TimeOffConfig'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}

  /api/requests:
    get:
      tags: [requests]
      operationId: listRequests
      description: История заявок из локальной копии (отгулы и доступы), created_at по убыванию. Наставнику — только employee_id своего стажёра, иначе 404.
      parameters:
        - {name: kind, in: query, schema: {$ref: '#/components/schemas/RequestKind'}}
        - {name: scope, in: query, schema: {type: string, enum: [upcoming, all], default: all}}
        - {name: employee_id, in: query, schema: {$ref: '#/components/schemas/UserId'}}
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/RequestList'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}

  /api/access/resources:
    get:
      tags: [access]
      operationId: listAccessResources
      description: Ресурсы, которые роль может запросить (запрещённые не перечисляются).
      responses:
        '200':
          description: OK
          content: {application/json: {schema: {type: object, required: [items], properties: {items: {type: array, items: {$ref: '#/components/schemas/AccessResourceView'}}}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '503': {$ref: '#/components/responses/Unavailable'}

  /api/kb/{slug}:
    get:
      tags: [kb]
      operationId: getKbDoc
      parameters:
        - {name: slug, in: path, required: true, schema: {type: string, pattern: '^[a-z0-9-]{2,60}$'}}
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/KbDoc'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/feedback:
    post:
      tags: [kb]
      operationId: postFeedback
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/FeedbackRequest'}}}
      responses:
        '204': {description: "Принято (повтор перезаписывает оценку)"}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
  /api/client-events:
    post:
      tags: [system]
      operationId: postClientEvent
      description: |
        События, которые происходят только на клиенте («Скопировать», «Сообщить о расхождении», клик «Что дальше»).
        Имя — из белого списка; остальные поля сервер берёт из сохранённого ответа и сессии.
        doc_conflict_reported записывается не больше одного раза на reply_id.
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/ClientEventRequest'}}}
      responses:
        '204': {description: "Принято"}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}

  /api/mentor/interns:
    get:
      tags: [mentor]
      operationId: listMentorInterns
      responses:
        '200': {description: "Свои стажёры", content: {application/json: {schema: {$ref: '#/components/schemas/MentorInternList'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
  /api/mentor/interns/{user_id}/path:
    get:
      tags: [mentor]
      operationId: getMentorInternPath
      parameters:
        - {name: user_id, in: path, required: true, schema: {$ref: '#/components/schemas/UserId'}}
      responses:
        '200': {description: "Путь стажёра, read_only=true", content: {application/json: {schema: {$ref: '#/components/schemas/PathView'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/mentor/access-requests:
    get:
      tags: [mentor]
      operationId: listMentorAccessRequests
      parameters:
        - {name: status, in: query, schema: {type: string, enum: [pending, approved, rejected], default: pending}}
      responses:
        '200': {description: "Входящие заявки своих стажёров", content: {application/json: {schema: {$ref: '#/components/schemas/RequestList'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/mentor/access-requests/{request_id}/decision:
    post:
      tags: [mentor]
      operationId: decideAccessRequest
      parameters:
        - {name: request_id, in: path, required: true, schema: {type: string, pattern: '^ACC-\d{6}$'}}
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/DecisionRequest'}}}
      responses:
        '200': {description: "Решение принято (повтор с тем же ключом — тот же ответ)", content: {application/json: {schema: {$ref: '#/components/schemas/RequestView'}}}}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '403': {$ref: '#/components/responses/Forbidden'}
        '404': {$ref: '#/components/responses/NotFound'}
        '409': {$ref: '#/components/responses/Conflict'}
        '429': {$ref: '#/components/responses/TooManyRequests'}
        '503': {$ref: '#/components/responses/Unavailable'}

  /api/demo/reset:
    post:
      tags: [demo]
      operationId: resetDemo
      description: Сначала сброс имитаций (MCP_MOCKS_ADMIN_URL), затем схемы app и ключей Redis (кроме сессий). События не удаляются.
      responses:
        '204': {description: "Демо-данные возвращены к сидам"}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
        '503': {$ref: '#/components/responses/Unavailable'}
  /api/demo/flags:
    get:
      tags: [demo]
      operationId: listDemoFlags
      responses:
        '200': {description: "Флаги из белого списка", content: {application/json: {schema: {$ref: '#/components/schemas/DemoFlagList'}}}}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
    post:
      tags: [demo]
      operationId: setDemoFlag
      requestBody:
        required: true
        content: {application/json: {schema: {$ref: '#/components/schemas/DemoFlagUpdate'}}}
      responses:
        '200': {description: "OK", content: {application/json: {schema: {$ref: '#/components/schemas/DemoFlag'}}}}
        '400': {$ref: '#/components/responses/BadRequest'}
        '401': {$ref: '#/components/responses/Unauthorized'}
        '404': {$ref: '#/components/responses/NotFound'}
        '503': {$ref: '#/components/responses/Unavailable'}

components:
  securitySchemes:
    session: {type: apiKey, in: cookie, name: buddy_session}

  parameters:
    ActionId: {name: action_id, in: path, required: true, schema: {type: string, format: uuid}}
    StepId: {name: step_id, in: path, required: true, schema: {type: string, pattern: '^[a-z]{2}-[a-z0-9.-]{1,20}$'}}

  responses:
    BadRequest: {description: "Неверный запрос", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    Unauthorized: {description: "Нет сессии или истекла", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    Forbidden: {description: "Нет прав или неверный Origin", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    NotFound: {description: "Не найдено или чужое (существование не раскрывается)", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    Conflict: {description: "Конфликт состояния", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    Gone: {description: "Черновик истёк, отменён или заменён", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    Unprocessable: {description: "Ошибки проверки по полям", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    TooManyRequests: {description: "Больше 20 сообщений или действий в минуту", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}
    Unavailable: {description: "Сервис недоступен, можно повторить", content: {application/json: {schema: {$ref: '#/components/schemas/Error'}}}}

  schemas:
    UserId: {type: string, pattern: '^u-[a-z0-9-]{1,40}$'}
    Role: {type: string, enum: [intern, mentor]}
    MascotState: {type: string, enum: [hello, done, oops, ask_mentor]}
    StepStatus: {type: string, enum: [not_started, in_progress, waiting, done]}
    PeriodId: {type: string, enum: [week1, week2, month1, month2, month3, after3]}
    RequestKind: {type: string, enum: [time_off, access]}
    AccessLevel: {type: string, enum: [read, write, admin]}
    Policy: {type: string, enum: [auto, mentor, timer]}
    IsoDate: {type: string, format: date}

    Error:
      type: object
      required: [code, message]
      properties:
        code:
          type: string
          description: >-
            VALIDATION_FAILED, BAD_REQUEST, UNAUTHORIZED, BAD_ORIGIN, PERMISSION_DENIED, NOT_FOUND,
            ACTION_EXPIRED, ACTION_CANCELLED, ACTION_REPLACED, STATUS_CONFLICT, TRANSITION_NOT_ALLOWED,
            DUPLICATE_ACTIVE, ALREADY_DECIDED, IDEMPOTENCY_CONFLICT, RATE_LIMITED, UNAVAILABLE, TIMEOUT, INTERNAL
        message: {type: string, description: "Текст для пользователя"}
        retryable: {type: boolean}
        errors: {type: array, items: {$ref: '#/components/schemas/FieldError'}}
        current_status: {$ref: '#/components/schemas/StepStatus'}
        existing: {$ref: '#/components/schemas/RequestView'}
        request: {$ref: '#/components/schemas/RequestView'}
        title: {type: string, description: "Заголовок закрытого документа (403 на /api/kb, только при disclose_title)"}
        reply: {$ref: '#/components/schemas/AgentReply'}
    FieldError:
      type: object
      required: [field, code, message]
      properties:
        field: {type: string, enum: [date_from, date_to, range, resource_id, text, payload]}
        code: {type: string, description: "REQUIRED, INVALID_DATE, DATE_IN_PAST, TOO_FAR, WEEKEND_DATE, RANGE_REVERSED, TOO_LONG, OVERLAP и др."}
        message: {type: string}
        params: {type: object, additionalProperties: true}

    Health:
      type: object
      required: [status, postgres, redis]
      properties:
        status: {type: string, enum: [ok, degraded]}
        postgres: {type: boolean}
        redis: {type: boolean}
    AppConfig:
      type: object
      required: [poll_interval_ms, demo_tools_enabled, timezone, llm_provider]
      properties:
        poll_interval_ms: {type: integer}
        demo_tools_enabled: {type: boolean}
        timezone: {type: string, example: Europe/Moscow}
        llm_provider: {type: string, enum: [demo, openai_compatible]}
    LlmHealth:
      type: object
      required: [provider, status, checked_at]
      properties:
        provider: {type: string, enum: [demo, openai_compatible]}
        model: {type: string, nullable: true}
        status: {type: string, enum: [ok, degraded, auth_error, misconfigured, circuit_open, disabled]}
        last_error_code: {type: string, nullable: true}
        checked_at: {type: string, format: date-time}

    TrackRef:
      type: object
      required: [id, title, synthetic]
      properties:
        id: {type: string}
        title: {type: string}
        synthetic: {type: boolean}
        disclaimer: {type: string, nullable: true}
    Me:
      type: object
      required: [user_id, display_name, role]
      properties:
        user_id: {$ref: '#/components/schemas/UserId'}
        display_name: {type: string}
        role: {$ref: '#/components/schemas/Role'}
        track: {allOf: [{$ref: '#/components/schemas/TrackRef'}], nullable: true}
    TestAccount:
      type: object
      required: [user_id, display_name, role, description]
      properties:
        user_id: {$ref: '#/components/schemas/UserId'}
        display_name: {type: string}
        role: {$ref: '#/components/schemas/Role'}
        description: {type: string, example: "Backend, неделя 1 — основной сценарий"}
    LoginRequest:
      type: object
      additionalProperties: false
      required: [user_id]
      properties:
        user_id: {$ref: '#/components/schemas/UserId'}

    ChatMessageRequest:
      type: object
      additionalProperties: false
      required: [client_message_id]
      description: Ровно одно из text / payload (проверяется в обработчике, AG-25).
      properties:
        client_message_id: {type: string, format: uuid}
        text: {type: string, minLength: 1, maxLength: 1000}
        payload: {type: string, pattern: '^(intent|step|resource|clarify):[a-z0-9._-]{1,64}$'}
    AgentReply:
      type: object
      required: [reply_id, client_message_id, mode, intent, mascot_state, text, sources, actions, notices]
      properties:
        reply_id: {type: string, format: uuid}
        client_message_id: {type: string, format: uuid}
        mode: {type: string, enum: [demo, llm, fallback]}
        intent: {type: string, enum: [help, my_path, step_explain, step_done, kb_question, time_off, access_request, my_requests, ask_mentor, out_of_scope, unknown]}
        mascot_state: {$ref: '#/components/schemas/MascotState'}
        text: {type: string, description: "Только текст; маркеры [n] ссылаются на sources (AG-26)"}
        sources: {type: array, items: {$ref: '#/components/schemas/Source'}}
        actions: {type: array, items: {$ref: '#/components/schemas/ReplyAction'}}
        pending_action: {allOf: [{$ref: '#/components/schemas/PendingAction'}], nullable: true}
        escalation: {allOf: [{$ref: '#/components/schemas/Escalation'}], nullable: true}
        notices:
          type: array
          items: {type: string, enum: [simplified_mode, llm_auth_error, llm_misconfigured, synthetic_track, pii_masked, restricted_doc_exists, doc_conflict, overlap_unchecked]}
    Source:
      type: object
      required: [n, slug, title, url, label, source_level]
      properties:
        n: {type: integer, minimum: 1}
        slug: {type: string}
        title: {type: string}
        url: {type: string, pattern: '^/kb/[a-z0-9-]+$'}
        label: {type: string, enum: [test_instruction, test_description]}
        source_level: {type: string, enum: [playbook, case, public, team, none]}
        dated: {type: string, nullable: true}
    ReplyAction:
      type: object
      required: [type, label]
      properties:
        type: {type: string, enum: [quick_reply, open_form, confirm, cancel, edit, mark_done, request_access, copy, link, retry, feedback]}
        label: {type: string}
        payload: {type: string, description: "Для quick_reply — по грамматике 09 §2.1"}
        value: {type: string, enum: [up, down], description: "Для feedback"}
        action_id: {type: string, format: uuid, description: "Для confirm, cancel, edit, retry"}
        step_id: {type: string, description: "Для mark_done"}
        form: {type: string, enum: [time_off]}
        prefill: {allOf: [{$ref: '#/components/schemas/DateRange'}], nullable: true}
        existing: {type: array, maxItems: 3, items: {$ref: '#/components/schemas/TimeOffBrief'}}
        text: {type: string, description: "Для copy — текст в буфер обмена"}
        copy_event: {type: string, enum: [escalation_copied, doc_conflict_reported], description: "Какое событие отправить в /api/client-events после копирования"}
        url: {type: string, description: "Для link — путь внутри приложения"}
    DateRange:
      type: object
      required: [date_from, date_to]
      properties:
        date_from: {$ref: '#/components/schemas/IsoDate'}
        date_to: {$ref: '#/components/schemas/IsoDate'}
    TimeOffBrief:
      type: object
      required: [request_id, date_from, date_to]
      properties:
        request_id: {type: string, pattern: '^ABS-\d{6}$'}
        date_from: {$ref: '#/components/schemas/IsoDate'}
        date_to: {$ref: '#/components/schemas/IsoDate'}
    PendingAction:
      type: object
      required: [action_id, kind, state, expires_at, summary]
      properties:
        action_id: {type: string, format: uuid}
        kind: {$ref: '#/components/schemas/RequestKind'}
        state: {type: string, enum: [active, confirming, confirmed, cancelled, replaced, expired]}
        expires_at: {type: string, format: date-time}
        summary: {$ref: '#/components/schemas/ActionSummary'}
    ActionSummary:
      type: object
      description: time_off — даты и рабочие дни; access — ресурс, уровень, согласующий, правило, обоснование.
      properties:
        date_from: {$ref: '#/components/schemas/IsoDate'}
        date_to: {$ref: '#/components/schemas/IsoDate'}
        working_days: {type: integer, minimum: 1, maximum: 5}
        resource_id: {type: string}
        title: {type: string}
        access_level: {$ref: '#/components/schemas/AccessLevel'}
        approver: {type: string, nullable: true}
        effective_policy: {$ref: '#/components/schemas/Policy'}
        timer_minutes: {type: integer, nullable: true}
        step_id: {type: string, nullable: true}
        justification: {type: string, maxLength: 200}
    Escalation:
      type: object
      required: [reason, draft]
      properties:
        reason: {type: string, enum: [explicit, no_answer, no_source_term, permission_denied, not_understood, negative_feedback, mentor_topic]}
        draft: {type: string, maxLength: 1500}
    HistoryItem:
      type: object
      required: [id, ts, author]
      properties:
        id: {type: string, format: uuid}
        ts: {type: string, format: date-time}
        author: {type: string, enum: [user, bot]}
        text: {type: string, nullable: true, description: "Замаскированный текст пользователя"}
        button_label: {type: string, nullable: true, description: "Подпись нажатой кнопки вместо payload"}
        reply: {allOf: [{$ref: '#/components/schemas/AgentReply'}], nullable: true}
    HistoryPage:
      type: object
      required: [items]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/HistoryItem'}}

    ActionCreateRequest:
      type: object
      additionalProperties: false
      required: [kind, params]
      properties:
        kind: {$ref: '#/components/schemas/RequestKind'}
        params:
          type: object
          additionalProperties: false
          description: time_off — date_from, date_to; access — resource_id. step_id и employee_id не принимаются (сервер определяет сам).
          properties:
            date_from: {type: string, maxLength: 10}
            date_to: {type: string, maxLength: 10}
            resource_id: {type: string, pattern: '^[a-z][a-z0-9-]{1,40}$'}
    ActionCreated:
      type: object
      required: [pending_action, notices]
      properties:
        pending_action: {$ref: '#/components/schemas/PendingAction'}
        notices: {type: array, items: {type: string, enum: [overlap_unchecked]}}
    ActionView:
      type: object
      required: [action_id, kind, state, expires_at, summary]
      properties:
        action_id: {type: string, format: uuid}
        kind: {$ref: '#/components/schemas/RequestKind'}
        state: {type: string, enum: [active, confirming, confirmed, cancelled, replaced, expired], description: "expired вычисляется из active и expires_at"}
        expires_at: {type: string, format: date-time}
        summary: {$ref: '#/components/schemas/ActionSummary'}
        result: {allOf: [{$ref: '#/components/schemas/RequestView'}], nullable: true}
    ConfirmResponse:
      type: object
      required: [result, reply]
      properties:
        result: {$ref: '#/components/schemas/RequestView'}
        reply: {$ref: '#/components/schemas/AgentReply'}

    RequestView:
      type: object
      description: Единый вид заявки (отгул или доступ) для карточек, истории, «Моих заявок» и «Входящих».
      required: [kind, request_id, status, created_at]
      properties:
        kind: {$ref: '#/components/schemas/RequestKind'}
        request_id: {type: string, pattern: '^(ABS|ACC)-\d{6}$'}
        status: {type: string, enum: [registered, cancelled, pending, approved, rejected]}
        created_at: {type: string, format: date-time}
        date_from: {$ref: '#/components/schemas/IsoDate'}
        date_to: {$ref: '#/components/schemas/IsoDate'}
        working_days: {type: integer}
        resource_id: {type: string}
        resource_title: {type: string}
        access_level: {$ref: '#/components/schemas/AccessLevel'}
        approver: {type: string, nullable: true}
        justification: {type: string, nullable: true}
        policy: {$ref: '#/components/schemas/Policy'}
        step_id: {type: string, nullable: true}
        step_title: {type: string, nullable: true}
        auto_approve_at: {type: string, format: date-time, nullable: true}
        decided_by: {type: string, enum: [auto, timer, mentor], nullable: true}
        reject_reason: {type: string, enum: [not_needed, other_resource, discuss_1on1], nullable: true}
        employee:
          type: object
          nullable: true
          description: Только в ответах наставнику
          required: [user_id, display_name]
          properties:
            user_id: {$ref: '#/components/schemas/UserId'}
            display_name: {type: string}
    RequestList:
      type: object
      required: [items, total]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/RequestView'}}
        total: {type: integer}
        stale: {type: boolean, description: "Сервис недоступен — показаны данные локальной копии"}

    PathView:
      type: object
      required: [employee, today, start_date, day, state, current_period, periods, steps, next_step, waiting, progress, read_only, has_pending_requests, stale_requests]
      properties:
        employee:
          type: object
          required: [user_id, display_name, track]
          properties:
            user_id: {$ref: '#/components/schemas/UserId'}
            display_name: {type: string}
            track: {$ref: '#/components/schemas/TrackRef'}
        today: {$ref: '#/components/schemas/IsoDate'}
        start_date: {$ref: '#/components/schemas/IsoDate'}
        day: {type: integer}
        state: {type: string, enum: [ready, pre_start, only_waiting, finished]}
        current_period: {$ref: '#/components/schemas/PeriodId'}
        periods: {type: array, items: {$ref: '#/components/schemas/Period'}}
        steps: {type: array, items: {$ref: '#/components/schemas/PathStep'}}
        next_step: {allOf: [{$ref: '#/components/schemas/NextStep'}], nullable: true}
        waiting: {type: array, items: {type: string}}
        progress: {$ref: '#/components/schemas/Progress'}
        read_only: {type: boolean}
        has_pending_requests: {type: boolean}
        stale_requests: {type: boolean}
    Period:
      type: object
      required: [id, title, from_day, state, done, total]
      properties:
        id: {$ref: '#/components/schemas/PeriodId'}
        title: {type: string}
        from_day: {type: integer}
        to_day: {type: integer, nullable: true}
        state: {type: string, enum: [past, current, future]}
        done: {type: integer}
        total: {type: integer}
    PathStep:
      type: object
      required: [step_id, period, order, title, type, help, status, synthetic]
      properties:
        step_id: {type: string}
        checklist_ref: {type: string, nullable: true}
        period: {$ref: '#/components/schemas/PeriodId'}
        order: {type: integer}
        title: {type: string}
        type: {type: string, enum: [learn, setup, access, meet, tell, org]}
        help:
          type: object
          required: [kind]
          properties:
            kind: {type: string, enum: [explain, access_request, ask_mentor, track_only]}
            kb_doc: {type: string}
            resource: {type: string}
        status: {$ref: '#/components/schemas/StepStatus'}
        synthetic: {type: boolean}
        request:
          type: object
          nullable: true
          required: [request_id, status]
          properties:
            request_id: {type: string, pattern: '^ACC-\d{6}$'}
            status: {type: string, enum: [pending, approved, rejected]}
            auto_approve_at: {type: string, format: date-time, nullable: true}
            reject_reason: {type: string, nullable: true}
    NextStep:
      type: object
      required: [step_id, reason]
      properties:
        step_id: {type: string}
        reason: {type: string, enum: [overdue, current, ahead]}
    Progress:
      type: object
      required: [period_done, period_total, overall_done, overall_total]
      properties:
        period_done: {type: integer}
        period_total: {type: integer}
        overall_done: {type: integer}
        overall_total: {type: integer}
    StepStatusUpdate:
      type: object
      additionalProperties: false
      required: [status, expected_status, idempotency_key]
      properties:
        status: {type: string, enum: [in_progress, done]}
        expected_status: {$ref: '#/components/schemas/StepStatus'}
        idempotency_key: {type: string, format: uuid}
    StepStatusResult:
      type: object
      required: [step, changed, path_summary]
      properties:
        step: {$ref: '#/components/schemas/PathStep'}
        changed: {type: boolean}
        path_summary:
          type: object
          required: [state, next_step, waiting, progress]
          properties:
            state: {type: string, enum: [ready, pre_start, only_waiting, finished]}
            next_step: {allOf: [{$ref: '#/components/schemas/NextStep'}], nullable: true}
            waiting: {type: array, items: {type: string}}
            progress: {$ref: '#/components/schemas/Progress'}

    TimeOffConfig:
      type: object
      required: [today, max_date, max_working_days, horizon_days, weekend_days]
      properties:
        today: {$ref: '#/components/schemas/IsoDate'}
        max_date: {$ref: '#/components/schemas/IsoDate'}
        max_working_days: {type: integer, example: 5}
        horizon_days: {type: integer, example: 90}
        weekend_days: {type: array, items: {type: integer, minimum: 1, maximum: 7}, example: [6, 7]}

    AccessResourceView:
      type: object
      required: [resource_id, title, kind, description, access_level, effective_policy]
      properties:
        resource_id: {type: string}
        title: {type: string}
        kind: {type: string, enum: [channel, space, repo, tool, database]}
        description: {type: string}
        access_level: {$ref: '#/components/schemas/AccessLevel'}
        approver: {type: string, nullable: true, description: "Подпись согласующего (имитация)"}
        effective_policy: {$ref: '#/components/schemas/Policy'}
        timer_minutes: {type: integer, nullable: true}

    KbDoc:
      type: object
      required: [slug, title, kind, summary, body_md, label, source_level, url, updated_at, conflicts_with]
      properties:
        slug: {type: string}
        title: {type: string}
        kind: {type: string, enum: [process, term, tool, guide, org]}
        summary: {type: string}
        body_md: {type: string, description: "Рендерится через санитайзер, без сырого HTML"}
        label: {type: string, enum: [test_instruction, test_description]}
        source_level: {type: string, enum: [playbook, case, public, team, none]}
        dated: {type: string, nullable: true}
        url: {type: string}
        updated_at: {type: string, format: date-time}
        conflicts_with: {type: array, items: {type: string}}
    FeedbackRequest:
      type: object
      additionalProperties: false
      required: [reply_id, value]
      properties:
        reply_id: {type: string, format: uuid}
        value: {type: string, enum: [up, down]}
    ClientEventRequest:
      type: object
      additionalProperties: false
      required: [name]
      properties:
        name: {type: string, enum: [escalation_copied, doc_conflict_reported, next_step_clicked]}
        reply_id: {type: string, format: uuid, description: "Обязателен для escalation_copied и doc_conflict_reported"}
        step_id: {type: string, description: "Обязателен для next_step_clicked"}

    MentorIntern:
      type: object
      required: [user_id, display_name, track, current_period, day, progress, pending_access_requests, upcoming_time_off]
      properties:
        user_id: {$ref: '#/components/schemas/UserId'}
        display_name: {type: string}
        track: {$ref: '#/components/schemas/TrackRef'}
        current_period: {$ref: '#/components/schemas/PeriodId'}
        day: {type: integer}
        progress: {$ref: '#/components/schemas/Progress'}
        pending_access_requests: {type: integer}
        upcoming_time_off: {type: array, maxItems: 2, items: {$ref: '#/components/schemas/TimeOffBrief'}}
    MentorInternList:
      type: object
      required: [items]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/MentorIntern'}}
    DecisionRequest:
      type: object
      additionalProperties: false
      required: [decision, idempotency_key]
      properties:
        decision: {type: string, enum: [approve, reject]}
        reject_reason: {type: string, enum: [not_needed, other_resource, discuss_1on1], description: "Обязательна при reject и запрещена при approve"}
        idempotency_key: {type: string, format: uuid}

    DemoFlag:
      type: object
      required: [name, value, allowed_values]
      properties:
        name: {type: string}
        value: {type: string}
        allowed_values: {type: array, items: {type: string}}
    DemoFlagList:
      type: object
      required: [items]
      properties:
        items: {type: array, items: {$ref: '#/components/schemas/DemoFlag'}}
    DemoFlagUpdate:
      type: object
      additionalProperties: false
      required: [name, value]
      properties:
        name: {type: string, enum: [mock_absences_fault, access_mentor_approval]}
        value: {type: string, description: "mock_absences_fault: off | unavailable (| slow, если успели); access_mentor_approval: on | off"}
```

**Итого 29 операций.** Порядок реализации — по чекпоинтам `07`: к C3 — system, auth, chat, actions, path, time-off, requests, demo/reset; к C4 — kb, feedback, client-events, access, mentor/interns; под чертой отсечения — `mentor/access-requests*`. Обработчик, который ещё не реализован, отвечает 501 и в README не упоминается.

---

## 7. Контракты MCP

### 7.1 Транспорт и соглашения
- **Транспорт:** MCP Streamable HTTP, один бинарник `mcp-mocks`, четыре сервера на путях `/mcp/{checklist,knowledge,absences,access}` (порт 8090, наружу не публикуется). `Stateless: true` — без сессий, рестарт ничего не ломает.
- **Схемы:** у каждого инструмента объявлены `inputSchema` и `outputSchema` — это файлы из `contracts/`, собранные в один документ (`contracts.Bundle`: относительные `$ref` переносятся в `$defs`). Тест `tools/list` = файлы (M-14, C-11, KM-12, XM-14) — по одному на endpoint.
- **Успех:** `structuredContent` по `outputSchema` + тот же JSON в `content[0].text`. **Ошибка:** `isError: true` + `structuredContent` по `common/error.v1.json`.
- **`_meta`:** `{ "request_id": "<uuid>" }` в каждом `tools/call`. Имитация пишет его в свои логи — путь сообщения виден от браузера до сервиса.
- **Аннотации инструментов** (подсказки MCP): чтение — `readOnlyHint: true`; запись — `idempotentHint: true` (идемпотентность по ключу); `access.list_requests` — `readOnlyHint: false, idempotentHint: true` (честно: применяет таймер).
- **Проверка на стороне клиента:** `ToolClient` проверяет ответ по **нашему** контракту из `contracts/`, а не по тому, что прислал сервер в `tools/list`. Реальный сервис, разошедшийся с контрактом, даёт `INTERNAL` → 503, а не неверный ответ стажёру (A33, X-A26).
- **Доверие к `actor`:** имитация доверяет `actor`, потому что доступна только из внутренней сети compose. В проде — сервисный токен или mTLS на транспорте (строка в карте интеграций), контракт не меняется.
- **Время:** имитации берут «сейчас» из `Clock` и передают его в SQL параметром — таймер S4 тестируется без ожидания.

### 7.2 Каталог инструментов
| Инструмент | Тип | Роли (`actor.role`) | Идемпотентность | Побочные эффекты | Ошибки | Спецификация |
|---|---|---|---|---|---|---|
| `checklist.get_path` | чтение | intern — свой; mentor — свои стажёры (`employee_id` обязателен) | — | нет | `PERMISSION_DENIED`, `NOT_FOUND` | `11` §1.11.2 |
| `checklist.get_step` | чтение | как `get_path` | — | нет | `NOT_FOUND`, `PERMISSION_DENIED` | `11` §1.11.3, §7.3 ниже |
| `checklist.set_step_status` | запись | intern — `user_*`; system — `access_*` | `idempotency_key` + `expected_status` | одна строка `mock_checklist.progress` | `VALIDATION_FAILED`, `PERMISSION_DENIED`, `NOT_FOUND`, `IDEMPOTENCY_CONFLICT`, `STATUS_CONFLICT`, `TRANSITION_NOT_ALLOWED` | `11` §1.11.4 |
| `knowledge.search` | чтение | intern, mentor; права — условие SQL | — | нет | `VALIDATION_FAILED`, `PERMISSION_DENIED` (system) | `11` §2.10.1 + `conflicts_with` |
| `knowledge.get_doc` | чтение | intern, mentor | — | нет | `NOT_FOUND`, `PERMISSION_DENIED` (`details.title` при `disclose_title`) | `11` §2.10.2 + `conflicts_with` |
| `absences.create_request` | запись | intern | `idempotency_key` (уникальный индекс) + `params_hash` | одна строка `mock_absences.requests` | `VALIDATION_FAILED`, `OVERLAP`, `PERMISSION_DENIED`, `IDEMPOTENCY_CONFLICT`, `UNAVAILABLE` | `10` §11.2 |
| `absences.list_requests` | чтение | intern — свои; mentor — свои стажёры | — | нет | `PERMISSION_DENIED` | `10` §11.3 |
| `access.list_resources` | чтение | intern, mentor, system | — | нет | — | `11` §3.10.2, §7.3 ниже |
| `access.create_request` | запись | intern, ресурс с `allowed_roles ∋ intern` | `idempotency_key` + `params_hash`; частичный уникальный индекс активной заявки | одна строка `mock_access.requests` | `VALIDATION_FAILED`, `PERMISSION_DENIED` (`ROLE_NOT_ALLOWED`), `NOT_FOUND`, `IDEMPOTENCY_CONFLICT`, `DUPLICATE_ACTIVE` | `11` §3.10.3 + `justification` |
| `access.list_requests` | чтение + таймер | intern — свои; mentor — свои стажёры; system (`employee_id` обязателен) | повтор ничего не меняет | `pending → approved` по `created_at + N` | `PERMISSION_DENIED`, `VALIDATION_FAILED` | `11` §3.10.4 |
| `access.decide_request` | запись | mentor, заявки своих стажёров в `pending` | `idempotency_key` (`mock_access.decisions`) | одна заявка | `NOT_FOUND`, `PERMISSION_DENIED`, `IDEMPOTENCY_CONFLICT`, `ALREADY_DECIDED` | `11` §3.10.5 |

Не в MCP (служебное, только при `DEMO_TOOLS_ENABLED=true`): `POST /internal/demo/reset`, `GET|POST /internal/demo/flags`, `GET /healthz`. Это не контракт корпоративного сервиса, и в проде их нет.

### 7.3 Файлы контрактов
```
contracts/
  common/actor.v1.json  common/error.v1.json                         # 10 §11.1, 11 §0.2 (роль system, 11 кодов ошибок)
  absences/request.v1.json  create_request.{input,output}.v1.json  list_requests.{input,output}.v1.json   # 10 §11 без изменений
  checklist/step.v1.json  get_path.{input,output}.v1.json  get_step.{input,output}.v1.json  set_step_status.{input,output}.v1.json
  knowledge/doc-frontmatter.v1.json  search.{input,output}.v1.json  get_doc.{input,output}.v1.json
  access/resource.v1.json  request.v1.json  list_resources.{input,output}.v1.json  create_request.{input,output}.v1.json
         list_requests.{input,output}.v1.json  decide_request.{input,output}.v1.json
```
Ниже полностью — схемы, которые меняет патч `17` или которые в `11` описаны только словами. Остальные переносятся из `10`/`11` как есть.

#### `access/resource.v1.json` (патч `17` §1.13: `access_level`, `approver`)
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "access/resource.v1.json",
  "title": "AccessResource",
  "type": "object",
  "additionalProperties": false,
  "required": ["resource_id", "title", "kind", "description", "policy", "effective_policy",
               "access_level", "approver", "allowed"],
  "properties": {
    "resource_id":      { "type": "string", "pattern": "^[a-z][a-z0-9-]{1,40}$" },
    "title":            { "type": "string", "maxLength": 120 },
    "kind":             { "type": "string", "enum": ["channel", "space", "repo", "tool", "database"] },
    "description":      { "type": "string", "maxLength": 400 },
    "policy":           { "type": "string", "enum": ["auto", "mentor", "timer"] },
    "effective_policy": { "type": "string", "enum": ["auto", "mentor", "timer"],
                          "description": "С учётом флага access_mentor_approval" },
    "timer_minutes":    { "type": ["integer", "null"], "minimum": 1 },
    "access_level":     { "type": "string", "enum": ["read", "write", "admin"] },
    "approver":         { "type": ["string", "null"], "maxLength": 80,
                          "description": "Подпись согласующего (имитация). Решение принимает правило policy" },
    "allowed":          { "type": "boolean" },
    "deny_reason_code": { "type": ["string", "null"], "enum": ["ROLE_NOT_ALLOWED", null] },
    "deny_explanation": { "type": ["string", "null"], "maxLength": 300 },
    "aliases":          { "type": "array", "items": { "type": "string" } }
  }
}
```

#### `access/request.v1.json` (патч `17` §1.14: `access_level`, `justification` — только чтение)
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "access/request.v1.json",
  "title": "AccessRequest",
  "type": "object",
  "additionalProperties": false,
  "required": ["request_id", "employee_id", "resource_id", "status", "policy", "access_level",
               "justification", "created_at"],
  "properties": {
    "request_id":         { "type": "string", "pattern": "^ACC-\\d{6}$" },
    "employee_id":        { "type": "string", "pattern": "^u-[a-z0-9-]{1,40}$" },
    "resource_id":        { "type": "string" },
    "status":             { "type": "string", "enum": ["pending", "approved", "rejected"] },
    "policy":             { "type": "string", "enum": ["auto", "mentor", "timer"],
                            "description": "Действующее правило с учётом флага на момент чтения" },
    "access_level":       { "type": "string", "enum": ["read", "write", "admin"],
                            "description": "Копия уровня ресурса на момент создания" },
    "justification":      { "type": ["string", "null"], "maxLength": 200 },
    "created_at":         { "type": "string", "format": "date-time" },
    "auto_approve_at":    { "type": ["string", "null"], "format": "date-time" },
    "decided_at":         { "type": ["string", "null"], "format": "date-time" },
    "decided_by":         { "type": ["string", "null"], "enum": ["auto", "timer", "mentor", null] },
    "decided_by_user_id": { "type": ["string", "null"] },
    "reject_reason":      { "type": ["string", "null"], "enum": ["not_needed", "other_resource", "discuss_1on1", null] }
  }
}
```

#### `access/create_request.input.v1.json` (патч `17` §1.14: `justification`)
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "access/create_request.input.v1.json",
  "type": "object",
  "additionalProperties": false,
  "required": ["actor", "idempotency_key", "params_hash", "resource_id"],
  "properties": {
    "actor":           { "$ref": "../common/actor.v1.json" },
    "idempotency_key": { "type": "string", "format": "uuid", "description": "action_id черновика" },
    "params_hash":     { "type": "string", "pattern": "^[a-f0-9]{64}$",
                         "description": "SHA-256 канонического JSON {resource_id}; justification не входит" },
    "resource_id":     { "type": "string", "pattern": "^[a-z][a-z0-9-]{1,40}$" },
    "justification":   { "type": "string", "maxLength": 200,
                         "description": "Формирует сервер бадди из шага; свободный ввод пользователя не допускается" }
  }
}
```
Как формируется `justification` (`internal/actions`): шаг найден по `help.resource` → «Шаг {{checklist_ref}} чек-листа адаптации: {{title}}» (у шага без `checklist_ref` — «Шаг «{{title}}» трека {{track}}»); шага нет → «Запрос из чата бадди». Пользовательский текст сюда не попадает никогда — ПДн не собираются.

#### `access/list_resources.{input,output}.v1.json`
```json
{ "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "access/list_resources.input.v1.json",
  "type": "object", "additionalProperties": false, "required": ["actor"],
  "properties": {
    "actor":          { "$ref": "../common/actor.v1.json" },
    "include_denied": { "type": "boolean", "default": true }
  } }
```
```json
{ "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "access/list_resources.output.v1.json",
  "type": "object", "additionalProperties": false, "required": ["items", "total"],
  "properties": {
    "items": { "type": "array", "items": { "$ref": "resource.v1.json" } },
    "total": { "type": "integer", "minimum": 0 }
  } }
```

#### `checklist/get_step.{input,output}.v1.json`
```json
{ "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "checklist/get_step.input.v1.json",
  "type": "object", "additionalProperties": false, "required": ["actor", "step_id"],
  "properties": {
    "actor":       { "$ref": "../common/actor.v1.json" },
    "step_id":     { "type": "string", "pattern": "^[a-z]{2}-[a-z0-9.-]{1,20}$" },
    "employee_id": { "type": "string", "pattern": "^u-[a-z0-9-]{1,40}$" }
  } }
```
```json
{ "$schema": "https://json-schema.org/draft/2020-12/schema", "$id": "checklist/get_step.output.v1.json",
  "type": "object", "additionalProperties": false, "required": ["step"],
  "properties": { "step": { "$ref": "step.v1.json" } } }
```

#### `knowledge/doc-frontmatter.v1.json` (патч `17` §1.2, §1.7: `conflicts_with`)
```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "knowledge/doc-frontmatter.v1.json",
  "title": "KnowledgeDocFrontMatter",
  "type": "object",
  "additionalProperties": false,
  "required": ["slug", "title", "kind", "source_level", "allowed_roles", "summary"],
  "properties": {
    "slug":           { "type": "string", "pattern": "^[a-z0-9-]{2,60}$" },
    "title":          { "type": "string", "maxLength": 160 },
    "kind":           { "type": "string", "enum": ["process", "term", "tool", "guide", "org"] },
    "source_level":   { "type": "string", "enum": ["playbook", "case", "public", "team", "none"] },
    "dated":          { "type": ["string", "null"] },
    "allowed_roles":  { "type": "array", "minItems": 1, "uniqueItems": true,
                        "items": { "enum": ["intern", "mentor"] } },
    "disclose_title": { "type": "boolean", "default": false },
    "tracks":         { "type": "array", "items": { "type": "string" }, "default": [] },
    "periods":        { "type": "array", "items": { "enum": ["week1", "week2", "month1", "month2", "month3", "after3"] }, "default": [] },
    "tags":           { "type": "array", "items": { "type": "string" }, "default": [] },
    "conflicts_with": { "type": "array", "items": { "type": "string", "pattern": "^[a-z0-9-]{2,60}$" },
                        "uniqueItems": true, "default": [],
                        "description": "Документы, которые расходятся с этим. Ссылка взаимная, allowed_roles одинаковые (правило 8 загрузчика)" },
    "test_only":      { "type": "boolean", "default": false },
    "summary":        { "type": "string", "minLength": 1, "maxLength": 300 }
  }
}
```

#### `knowledge/search.output.v1.json` и `get_doc.output.v1.json` — добавка
В `items[].properties` (search) и `doc.properties` (get_doc) добавляется необязательное поле; остальное — как в `11` §2.10.1–2.10.2:
```json
"conflicts_with": { "type": "array", "items": { "type": "string" } }
```
Оркестратор (дополнение `17` §1.4): если ответ строится по правилам 3–4 и у источника непустой `conflicts_with`, он вызывает `knowledge.get_doc` для конфликтующего документа (права проверяются как обычно), добавляет его вторым источником, `notices: [doc_conflict]`, действие `copy` с `copy_event: doc_conflict_reported`.

### 7.4 Как заменить имитацию реальным сервисом
1. Поднять MCP-сервер (или тонкий адаптер к существующему API сервиса), который отдаёт инструменты с теми же именами и схемами из `contracts/`.
2. Указать его адрес в `MCP_<SERVICE>_URL`. Код агента, промпты, фронт и тесты поведения не меняются.
3. Статусы реального сервиса (например, согласование отгула руководителем и HR BP) адаптер переводит в статусы контракта.
4. Проверка совместимости — тот же контрактный тест `tools/list` и проверка `outputSchema` в `ToolClient`.

---

## 8. PostgreSQL

### 8.1 Схемы и владельцы
| Схема | Владелец (кто пишет) | Миграции | Что внутри |
|---|---|---|---|
| `app` | `api` | `migrations/app` | Сотрудники, флаги, диалоги, ответы, оценки, журнал действий, копии заявок, статистика этапов, события |
| `mock_checklist` | `mcp-mocks` | `migrations/mocks` | Треки и шаги из YAML, прогресс, ключи идемпотентности |
| `mock_knowledge` | `mcp-mocks` | `migrations/mocks` | Документы базы знаний и полнотекстовый индекс |
| `mock_absences` | `mcp-mocks` | `migrations/mocks` | Заявки на отгул |
| `mock_access` | `mcp-mocks` | `migrations/mocks` | Каталог ресурсов, заявки, решения |
| `mock_admin` | `mcp-mocks` | `migrations/mocks` | Флаги имитаций, отметка о сидах |

Один пользователь БД на стенде. В проде у каждого сервиса своя БД — это видно уже сейчас по границе схем и тесту `TestAppDoesNotTouchMockSchemas`.

### 8.2 Схема `app`
```sql
-- migrations/app/0001_init.sql
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
```
Метрика «неделя 1 вовремя» из `11` §1.14 переписывается на `app.path_period_stats` (её обновляет `GET /api/path`).

### 8.3 Схемы имитаций
```sql
-- migrations/mocks/0001_init.sql
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
```

**Почему `tsv` — обычная колонка, а не `GENERATED`.** В выражении индекса участвует `array_to_string(tags)`, а эта функция не помечена как `IMMUTABLE`, поэтому генерируемая колонка на ней не создаётся. Документы пишет только загрузчик, так что колонку проще посчитать в его `INSERT`: `setweight(to_tsvector('russian', $title), 'A') || setweight(to_tsvector('russian', $tags_text), 'B') || setweight(to_tsvector('russian', $body), 'C')`.

### 8.4 Сиды
| Что | Где | Значения |
|---|---|---|
| Сотрудники | `content/seeds/employees.yaml` → `app.employees` (+ `start_date`) и в память `mcp-mocks` (без дат) | `u-intern-be` (backend, `today − 3`), `u-intern-fe` (frontend, `today − 10`), `u-mentor`; оба стажёра у `u-mentor` |
| Флаги приложения | `app.feature_flags` | `llm_enabled = on`, `llm_rephrase = on` |
| Флаги имитаций | `mock_admin.flags` | `mock_absences_fault = off` (`off,unavailable` + `slow`, если успели), `access_mentor_approval = on` |
| Прогресс S1 | `mock_checklist.progress` | `11` §1.13 |
| Отгул S3 | `mock_absences.requests` + `app.time_off_requests` | `ABS-000101`, пн–вт через 3 недели (`10` §12.4) |
| Доступ S4 | `mock_access.requests` + `app.access_requests` | `ACC-000101`, `space-cluster`, `pending`, `created_at = now − 1 день`, `step_id = cm-1.10`, `justification = «Шаг 1.10 чек-листа адаптации: …»` |

Имитациям даты старта не нужны: этап считает `internal/path` из `app.employees`. Поэтому рестарт `mcp-mocks` в другой день ничего не сдвигает.

### 8.5 Сброс демо (`POST /api/demo/reset`)
1. `api` → `POST {MCP_MOCKS_ADMIN_URL}/demo/reset`. Имитация в одной транзакции: `TRUNCATE` заявок, решений, прогресса, ключей идемпотентности; `setval` последовательностей на 101; перезагрузка `content/`; сиды; флаги по умолчанию. Ошибка → 503, схема `app` не тронута.
2. `api` в одной транзакции: `TRUNCATE` `chat_messages` (каскадом ответы и оценки), `action_log`, `time_off_requests`, `access_requests`, `path_period_stats`; upsert сотрудников с новыми датами; сиды копий заявок.
3. Redis: удалить по `SCAN MATCH` `action:*`, `active_action:*`, `msg:*`, `ctx:*`, `dedupe:*`. Сессии остаются — эксперт не разлогинивается.
4. События не удаляются: дашборд сохраняет историю.

### 8.6 Где лежат данные контента
| Данные | Путь | Кто загружает | Проверки при старте |
|---|---|---|---|
| Треки | `content/tracks/*.yaml` | `mcp-mocks` (`internal/tracks`) | 8 правил `11` §1.12 |
| База знаний | `content/kb/*.md` | `mcp-mocks` (`internal/kb`) | 9 правил (`11` §2.10.3 + правило 8 патча `17`) |
| Каталог доступов | `content/access/catalog.yaml` | `mcp-mocks` (`mcpmocks/catalog`) | `11` §3.10.6 + `access_level`, `approver` |
| Сотрудники | `content/seeds/employees.yaml` | `api` и `mcp-mocks` (`internal/employees`) | трек существует, наставник существует |
| Словарь demo | `internal/agent/rules/intents.ru.yaml` | `api` | схема словаря, уникальность фраз |

Загрузчики принимают `fs.FS`: в бинарнике — встроенный `content`, в тестах — `os.DirFS(t.TempDir())` (AC-P16 «новый трек без кода»). Кросс-тест «трек → документ → ресурс» — `internal/tracks/crossref_test.go`.

---

## 9. Redis

| Ключ | Значение | TTL | Кто пишет |
|---|---|---|---|
| `sess:{sid}` | `{user_id, role, created_at}` | `SESSION_TTL_HOURS`, продлевается | `auth` |
| `msg:{user_id}:{client_message_id}` | `reply_id` + сериализованный `AgentReply` | 10 мин | чат (повтор → прежний ответ) |
| `msglock:{user_id}:{client_message_id}` | `SET NX` на время обработки | 30 с | чат (параллельный повтор ждёт и получает тот же ответ) |
| `action:{action_id}` | черновик `10` §8.1 (`state`, `expires_at`, `result`) | 24 ч | `actions` |
| `active_action:{user_id}:time_off` | `action_id` | 24 ч | `actions` |
| `active_action:{user_id}:access:{resource_id}` | `action_id` | 24 ч | `actions` |
| `ctx:lastq:{user_id}` | `last_bot_question` (варианты уточнения) | 10 мин | агент |
| `ctx:unknown:{user_id}`, `ctx:neg:{user_id}` | счётчики «не понял подряд», «Не то подряд» | 30 мин | агент |
| `rl:{user_id}:{msg\|act}:{epoch_min}` | счётчик | 60 с | `ratelimit` |
| `dedupe:{event}:{user_id}` | маркер (`path_viewed`, `mentor_inbox_viewed`) | 10 мин | `events` |

Состояние circuit breaker LLM и кэш `/api/llm/health` (30 с) — в памяти процесса: экземпляр `api` один. Если `api` станет несколько экземпляров, это переедет в Redis — строка в roadmap. Переход черновика `active → confirming` — Lua-скрипт (атомарно и в Redis, и в miniredis).

---

## 10. Frontend

### 10.1 Слои
- **Маршруты** (TanStack Router, файловые): `/login`, `/` (чат + панель «Мой путь»; у наставника — переход на `/mentor/interns`), `/requests`, `/kb/$slug`, `/mentor/interns`, `/mentor/interns/$userId`, `/mentor/inbox`. Гард маршрута по `['me']`: 401 → `/login` с тостом «Сессия истекла — войди снова».
- **Данные** (TanStack Query): ключи `['me']`, `['config']`, `['path']`, `['requests', filters]`, `['chat', 'history']`, `['action', id]`, `['timeOffConfig']`, `['kb', slug]`, `['mentor', 'interns']`, `['mentor', 'inbox']`, `['demo', 'flags']`. Опрос `['path']` и `['mentor','inbox']` — только при `has_pending_requests` и только в активной вкладке (`11` §3.6).
- **Сторы Zustand** — только клиентское состояние: композер чата и запрос в полёте (`client_message_id` для повтора), форма отгула (`useTimeOffForm`, состояния `10` §9.2), очередь тостов, состояние маскота.
- **Zod** — каждый ответ API разбирается схемой фичи; схема связана с типами из OpenAPI: `export const PathViewSchema = z.object({...}) satisfies z.ZodType<components['schemas']['PathView']>`. Если поле в OpenAPI изменилось, а Zod — нет, падает `tsc`. Неожиданный формат ответа → состояние `error`, а не белый экран.
- **API-клиент** (`shared/api/client.ts`): `fetch` с `credentials: 'same-origin'`, единый разбор `Error`, `X-Request-Id` из ответа — в тексте ошибки «код для поддержки», `AbortController` для «Отменить» после 5 с ожидания.
- **Компоненты тонкие**: логика — в хуках, сторах, схемах. Это и даёт покрытие фронта без тяжёлых UI-тестов.

### 10.2 Безопасность и UX-обязательства
- Текст ответа бадди — только текст; маркеры `[n]` → ссылки на `sources`. Markdown документов — `react-markdown` без `rehype-raw` (сырой HTML не рендерится) + `urlTransform`, пропускающий только `http`, `https` и относительные ссылки. В eslint запрещён `dangerouslySetInnerHTML`.
- `nginx.conf`: `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; object-src 'none'; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`. Внешних CDN нет.
- Дата «сегодня» — только из `GET /api/time-off/config` и `PathView.today`; арифметика дат — `Date.UTC`/`getUTCDay`. Тест с `TZ=America/Los_Angeles`.
- Токены дизайна в два слоя (палитра → семантика) в CSS-переменных; светлая тема обязательна, тёмная — п. 0 черты отсечения.
- Состояния у каждого экрана: скелетон, пусто, ошибка + «Повторить», «stale»; маскот — 5 состояний, `thinking` ставит только клиент.

---

## 11. Тесты и покрытие

### 11.1 Слои тестов
| Слой | Что | Инфраструктура | Где |
|---|---|---|---|
| Чистые функции | `timeoff/rules` (векторы V-01…V-21 из `docs/specs/s3/validation-cases.json`), `checklist/fsm`, `path`, правила demo, `pii`, `ReplyGuard` | нет | рядом с кодом |
| Загрузчики и контент | треки, база знаний, каталог, словарь, кросс-ссылки | `fs.FS` | `internal/tracks`, `internal/kb`, `internal/mcpmocks/catalog` |
| Оркестратор и API | таблицы T-01…T-38, A-*, PA-*, KA-*, XA-* | `httptest`, фейковые `ToolClient`, `LLMClient`, `Clock`; miniredis; PostgreSQL для `app` | `internal/httpapi`, `internal/agent/...` |
| Интеграция с имитациями | реальный `mcp-mocks` в процессе (`httptest.Server`) + реальный `ToolClient` | PostgreSQL из compose | `internal/httpapi/integration_test.go` |
| Имитации | M-*, C-*, KM-*, XM-*, гонки (10 горутин) | PostgreSQL из compose | `internal/mcpmocks/...` (отдельный отчёт) |
| Контракты | `tools/list` = `contracts/`; все схемы компилируются; OpenAPI валиден | нет / PostgreSQL | `contracts/`, `internal/mcpmocks/server` |
| Фронт | хуки, сторы, Zod-схемы, API-клиент, ключевые экраны | Vitest + Testing Library + MSW, jsdom | `web/src/**` |
| Smoke | основной сценарий S1 + S3 (+ S4 после C4) через HTTP | весь стек | `test/smoke` |

LLM в тестах — всегда заглушка `LLMClient`; адаптер `openai_compatible` проверяется на фейковом HTTP-сервере. Изоляция БД: `internal/testkit/pgtest` создаёт для каждого пакета свою базу из шаблона с применёнными миграциями (`CREATE DATABASE … TEMPLATE`) — пакеты идут параллельно и не мешают друг другу. Отдельной инфраструктуры для гонок нет: PostgreSQL из compose (решение `02` v3.8).

### 11.2 Команды и отчёты
`scripts/test-go.sh` (запускается в сервисе `test-go`):
```sh
#!/bin/sh
set -eu
OUT=${OUT:-/out}
APP_PKGS=$(go list ./... | grep -v -e '/internal/mcpmocks' -e '/cmd/mcp-mocks' -e '/internal/gen/' -e '/test/smoke')
MOCK_PKGS=$(go list ./internal/mcpmocks/... ./cmd/mcp-mocks/...)
COVERPKG=$(echo "$APP_PKGS" | paste -sd, -)

# 1) Порог: код приложения, включая общие пакеты (rules, fsm, tracks, kb, employees)
go test -race -count=1 -coverpkg="$COVERPKG" -coverprofile="$OUT/go-app.out" $APP_PKGS
go tool cover -func="$OUT/go-app.out" > "$OUT/go-app.txt"
go tool cover -html="$OUT/go-app.out" -o "$OUT/go-app.html"

# 2) Справочно: имитации (вне порога — Q&A 10.11)
go test -race -count=1 -coverprofile="$OUT/go-mocks.out" $MOCK_PKGS
go tool cover -func="$OUT/go-mocks.out" > "$OUT/go-mocks.txt"
go tool cover -html="$OUT/go-mocks.out" -o "$OUT/go-mocks.html"

scripts/coverage-check.sh "$OUT/go-app.txt" 80     # жёсткий порог CI; цель команды — 85
```
Фронт — `vitest run --coverage` (провайдер v8), отчёты `lcov`, `html`, `json-summary` в `web/coverage/`; пороги в `vitest.config.ts`: `lines`, `statements`, `functions`, `branches` — по 80.

| Отчёт | Файл | Порог | Что не входит (перечислено в README со ссылкой на Q&A) |
|---|---|---|---|
| Go, приложение | `coverage/go-app.{txt,html,out}` | ≥ 80 (CI), цель 85 | `internal/mcpmocks`, `cmd/mcp-mocks`, `internal/gen/api` (oapi-codegen), `test/smoke` |
| Go, имитации (справочно) | `coverage/go-mocks.{txt,html,out}` | нет | — |
| Фронт | `web/coverage/{lcov.info,index.html,coverage-summary.json}` | 4 метрики ≥ 80 | `src/routeTree.gen.ts`, `src/shared/api/schema.gen.ts`, `*.d.ts` |

`cmd/api/main.go` в порог входит, поэтому он ≤ 20 строк, а сборка зависимостей вынесена в `internal/app` и покрыта интеграционными тестами.

---

## 12. CI (GitHub Actions, `ubuntu-latest`, amd64)

| Job | Шаги | Падает, если |
|---|---|---|
| `lint` | `golangci-lint` (с depguard), `go vet`; `npm ci && npm run lint && tsc --noEmit`; `scripts/gen.sh && git diff --exit-code`; `openspec validate --strict` | нарушены границы, сгенерированный код разошёлся со спецификацией, спецификации невалидны |
| `test` | `docker compose up -d --wait postgres redis` → `docker compose --profile test run --rm test-go` → `test-web`; артефакт `coverage/` + `web/coverage/`; итоги покрытия в `$GITHUB_STEP_SUMMARY` | тест упал, покрытие < 80 в любом отчёте приложения |
| `e2e` | `docker compose up --build --wait` → `docker compose --profile test run --rm smoke`; при ошибке — `docker compose logs` в артефакт | основной сценарий не проходит, не стартует сборка amd64 |

В CI выполняются **те же команды, что в README**: зелёный CI доказывает, что инструкция работает. arm64 проверяется локально на Mac (`02`).

---

## 13. Наблюдаемость

- **Логи:** `slog` JSON в stdout у `api` и `mcp-mocks`; поля `request_id`, `user_id` (синтетический), `intent`, `mode`, `tool`, `duration_ms`, `error_code`. Без текста сообщений, дат отсутствия и ключей (AG-12, `10` §14).
- **Сквозной `request_id`:** `X-Request-Id` → `context` → `_meta` MCP-вызова → логи имитации → `app.events.request_id`. Один запрос можно проследить от браузера до сервиса.
- **События:** `EventPublisher` → `app.events`; состав — `09` §12, `10` §13, `11` §1.14, §2.12, §3.13, плюс `doc_conflict_reported` (`17`). `reply_sent` несёт задержки по этапам (`llm_ms`, `mcp_ms`).
- **Grafana** (`--profile observability`): источник PostgreSQL, дашборд `deploy/grafana/dashboards/buddy.json`, провижининг из файлов. Панели: воронка S3; доля «неделя 1 вовремя» (`app.path_period_stats`); вопросы без наставника и эскалации по причинам; оценки; ожидание доступа по `decided_by`; расхождения инструкций по документам; p50/p95 `reply_sent` по режимам; доля откатов LLM; отказы по правам. Без профиля всё работает; скриншоты дашборда — в README и на слайде.

---

## 14. Безопасность: где что проверяется
Подробная матрица «роль × инструмент × документ» и тест-кейсы отказов — в `13-permissions.md`. Здесь — слои.

| Слой | Что проверяет | Где в коде |
|---|---|---|
| 1. HTTP | Сессия → `actor` (роль и личность только отсюда), same-origin `Origin`, лимиты, схема тела (`additionalProperties: false`), грамматика payload | `internal/auth`, валидатор OpenAPI, `internal/httpapi` |
| 2. Оркестратор | Роль × инструмент × объект перед каждым MCP-вызовом; маршруты стажёра для наставника; повторная проверка при `confirm` | `internal/agent/guard`, `internal/actions` |
| 3. Имитация | Повторная проверка роли и связи «наставник–стажёр», переходы FSM, идемпотентность | `internal/mcpmocks/*` |
| Поиск | `allowed_roles @> ARRAY[role]` в `WHERE`; закрытые — только `slug, title` при `disclose_title`; `conflicts_with` — только между документами с одинаковыми правами (правило 8) | `internal/mcpmocks/knowledge`, `internal/kb` |
| Данные | Только синтетика; маскирование ПДн до БД, логов и LLM; нет полей «причина», «сумма»; обоснование доступа формирует сервер | `internal/agent/pii`, контракты |
| Фронт | Текст как текст, санитайзер markdown, CSP | `web/` |
| Секреты | Только env; `.env` не в git; ключ LLM не в логах и не в `/api/llm/health` | `internal/config` |

---

## 15. Отклонённые варианты
| Вариант | Почему нет |
|---|---|
| Отдельный контейнер на каждую имитацию | +3 контейнера и healthcheck'а при той же заменяемости: адрес endpoint и так в env |
| Testcontainers для гонок | Отдельная инфраструктура; PostgreSQL из compose проверяет то же (решение `02` v3.8) |
| OpenAPI 3.1 | `oapi-codegen` поддерживает его не полностью; выигрыш для эксперта нулевой |
| `GENERATED`-колонки FTS | `array_to_string` не `IMMUTABLE` — миграция падает (§8.3) |
| Флаги имитаций в `app.feature_flags` | `api` знал бы о флагах, которых в проде нет; имитация читала бы чужую схему |
| Отдельные пользователи БД на схему | Нужен init-скрипт контейнера — лишний риск запуска; граница проверяется тестом, в проде — отдельные БД |
| Vite dev-сервер в compose | Медленнее, нестабильнее и не похоже на прод; nginx со статикой даёт один origin и CSP |
| Публиковать порты `api` и `mcp-mocks` | Больше конфликтов портов; `mcp-mocks` доверяет `actor` только потому, что недоступен снаружи |
| SSE для ответов | Roadmap (`02`); ответ в demo < 300 мс |

---

## 16. Правки к другим документам (промт 3.0, вместе с патчем `17`)
1. **`09`, `10`, `11`:** пути `docs/contracts/…` → `contracts/…`; `tracks/` → `content/tracks/`; `kb/` → `content/kb/`; `access/catalog.yaml` → `content/access/catalog.yaml`.
2. **`10` §10, `11` §3.12:** ответ `confirm` — `{result: RequestView, reply}`; `RequestView` — единый вид заявки (§6). Поля `10` (`request_id`, `status`, `date_from`, `date_to`, `working_days`) в нём есть.
3. **`10` §11.6, `11` §0.3:** флаги имитаций хранятся в `mock_admin.flags` (`FlagProvider` имитации); `POST /api/demo/flags` проксирует в `MCP_MOCKS_ADMIN_URL/demo/flags`. Белый список не меняется.
4. **`10` §14, §19, `11` §5:** вопрос «testcontainers или compose» закрыт — PostgreSQL из compose.
5. **`11` §1.14:** `path_steps_cache` → таблица `app.path_period_stats`; SQL метрики — по ней.
6. **`09` §8.2, `11` §2.6.5, `17` §1.4:** события `escalation_copied` и `doc_conflict_reported` отправляются через `POST /api/client-events`; `next_step_clicked` (`11` §1.14) — тоже.
7. **`10` §8.1, `11` §0.5:** `GET /api/actions/{id}` отдаёт вычисляемое состояние `expired` (черновик `active` после `expires_at`).
8. **`09` §2.1:** вход по тестовой учётке — `GET /api/auth/accounts`, `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/me`.
9. **`11` §1.11.3:** схемы `get_step` — §7.3 этого файла.

---

## 17. Риски и открытые вопросы
| Риск | Влияние | Что делаем |
|---|---|---|
| 29 операций OpenAPI при одном разработчике | Не успеть всё к C4 | Генерация сразу, реализация по чекпоинтам (§6); нереализованное → 501 и не упоминается в README |
| API Go SDK MCP меняется между минорными версиями | Сборка ломается | Версия фиксируется в `go.mod` на C1; контрактные тесты ловят расхождения |
| Same-origin `Origin` за чужим прокси | 403 на POST у эксперта | Сравниваем с `Host` от nginx; запасной путь — `ALLOWED_ORIGINS`; тест на `localhost` и `127.0.0.1` в smoke |
| `-race` удлиняет тесты | Медленный CI | Если > 5 мин — `-race` только для пакетов с горутинами (`actions`, `access/sync`, `mcpmocks`) |
| miniredis расходится с Redis в Lua | Ложнозелёные тесты черновиков | Smoke на реальном Redis проходит `confirm` дважды |
| `REGISTRY_PREFIX` не охватывает `FROM` в Dockerfile | Сборка без Docker Hub падает | `ARG REGISTRY_PREFIX` в каждом Dockerfile, `build.args` в compose; проверить на C0 |

Открытые вопросы:
- Версии Go, Node, PostgreSQL, Redis — зафиксировать в README на C0 (если каркас уже сделан — сверить с §4.1).
- Порты: если C0 уже выбрал другие значения, правы `.env.example` и README — обновить §4.3 при переносе в `docs/`.

---

*Конец `12-architecture.md`.*
