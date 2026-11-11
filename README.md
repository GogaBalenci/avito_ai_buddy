# AI-buddy «Авиитик Витя» — ИИ-наставник для стажёров Авито

Кейс Авито на хакатоне Hack&Change 2026. Бадди показывает стажёру, где он находится в адаптации и что делать дальше, отвечает на вопросы по базе знаний со ссылкой на источник и оформляет заявки через корпоративные сервисы (MCP-имитации) только после явного подтверждения.

> **Статус: бэкенд основного сценария (C3).** Стенд поднимается одной командой. Через API работают S1 «Мой путь» и S3 «Отгул» в режиме `demo` (без ключа LLM): чат с распознаванием по правилам, черновик и явное подтверждение заявки. Проверка — `smoke` ниже. Интерфейс, S2 и S4 появятся на следующих этапах, и этот README будет дополнен пошаговой проверкой каждого сценария. Все данные синтетические.

## Быстрый старт

Нужен только Docker (Docker Desktop на macOS и Windows или Docker Engine на Linux) с Compose v2.20+. Go и Node.js ставить не нужно.

```bash
docker compose up --build --wait
```

Откройте <http://localhost:8380>. На странице видно, что web → api → PostgreSQL и Redis работают.

| | |
|---|---|
| Первая сборка | ≈ 1 мин: сборка без кэша 18 с + старт до зелёных healthchecks 42 с, включая скачивание `postgres` и `redis` (MacBook Air M-series, arm64, Docker 29.4). Если базовые образы `golang` и `node` ещё не скачаны, добавьте время на загрузку ≈ 400 МБ. |
| Повторный запуск | секунды (образы уже собраны) |
| Остановить | `docker compose down` (данные БД сохраняются в томе) |
| Остановить и стереть данные | `docker compose down -v` |

Файл `.env` создавать не обязательно: значения по умолчанию совпадают с [`.env.example`](.env.example). Чтобы что-то поменять, скопируйте `.env.example` в `.env` и отредактируйте.

### Если Docker Hub недоступен

Все базовые образы берутся из Docker Hub. Если он недоступен (например, из сети в России), используйте зеркало Google:

```bash
REGISTRY_PREFIX=mirror.gcr.io/ docker compose up --build --wait
```

или один раз пропишите `REGISTRY_PREFIX=mirror.gcr.io/` в `.env`. Префикс применяется ко всем образам: и в `docker-compose.yml`, и в каждом `FROM` в Dockerfile.

### Если порт 8380 занят

```bash
WEB_PORT=8480 docker compose up --build --wait
```

Наружу публикуется только `WEB_PORT`. Порты 5000 и 7000 не используются, потому что на macOS их занимает AirPlay. PostgreSQL, Redis, api и mcp-mocks доступны только внутри сети compose.

## Версии

| Компонент | Версия | Образ |
|---|---|---|
| Go | 1.26 | `golang:1.26-alpine` (сборка), `golang:1.26-bookworm` (тесты, `-race`) |
| Node.js | 24 LTS | `node:24-alpine` (сборка фронта) |
| PostgreSQL | 18 | `postgres:18-alpine` |
| Redis | 8 | `redis:8-alpine` (без сохранения на диск) |
| nginx | 1.28 | `nginx:1.28-alpine` (статика и прокси `/api`) |
| Runtime Go-сервисов | Alpine 3.22 | `alpine:3.22`, статические бинарники, пользователь без root |
| Фронт | React 19, Vite 8, TypeScript 5.9 | зафиксированы в `web/package-lock.json` |

Все базовые образы multi-arch (amd64 и arm64): стенд одинаково собирается на Mac с Apple Silicon, Linux и Windows (WSL2).

## Сервисы

| Сервис | Назначение | Healthcheck | Наружу |
|---|---|---|---|
| `web` | SPA (React + TypeScript) за nginx, проксирует `/api` и `/healthz` в `api` | `GET /` | `WEB_PORT` (8380) |
| `api` | HTTP API бадди (Go) | `GET /healthz`: проверяет PostgreSQL и Redis | нет |
| `mcp-mocks` | Имитации корпоративных сервисов: чек-лист, база знаний, отсутствия, доступы (MCP) | `GET /healthz` | нет |
| `postgres` | Схема `app` у api, схемы `mock_*` у имитаций | `pg_isready` | нет |
| `redis` | Сессии, черновики, лимиты | `redis-cli ping` | нет |

Сервисы стартуют по цепочке `depends_on: condition: service_healthy`: postgres и redis → mcp-mocks → api → web. Команда `--wait` возвращает управление, только когда все сервисы здоровы.

## Тесты

```bash
docker compose --profile test run --rm test-go    # Go: тесты (-race) и отчёты покрытия в coverage/
docker compose --profile test run --rm test-web   # фронт (пока заглушка)
docker compose --profile test run --rm smoke      # основной сценарий S1 + S3 через API (сбрасывает демо-данные)
```

Отчёты Go: `coverage/go-app.html` (код приложения, по нему считается порог) и `coverage/go-mocks.html` (имитации, справочно). CI в [GitHub Actions](.github/workflows/ci.yml) выполняет те же команды.

Для тех, у кого есть `make`: `make up`, `make down`, `make test`, `make smoke`. Это обёртки над командами выше.

## Структура

```
cmd/            точки входа: api, mcp-mocks
internal/       код приложения; internal/mcpmocks — имитации (границу проверяет depguard)
web/            фронт (Vite + React + TypeScript), nginx.conf
deploy/docker/  многостадийные Dockerfile
scripts/        тесты, smoke, проверка каркаса
```

Целевая структура и архитектура описаны в `docs/` (будут добавлены).

## Лицензия

[MIT](LICENSE)
