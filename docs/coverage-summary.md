# Сводка покрытия

Последний прогон: **14.11.2026**, код коммита `5d12e24`, на Mac (arm64) в Docker, той же командой, что в CI и в README:

```bash
docker compose --profile test run --build --rm test-go    # Go: go vet, тесты с -race, два отчёта
docker compose --profile test run --rm test-web           # фронт: eslint, tsc, Vitest с покрытием
```

| Отчёт | Порог | Результат |
|---|---|---|
| Go — приложение (всё, кроме имитаций и сгенерированного кода) | 80% statements, цель 85% | **91,1%** (4 305 statements) |
| Go — имитации `mcp-mocks` (справочно, вне порога) | — | 70,5% |
| Фронт `web/src` — 197 тестов | 80% по каждой метрике | lines **98,5%** · statements **97,5%** · functions **95,2%** · branches **91,9%** |

Полные отчёты появляются после команд выше: `coverage/go-app.html`, `coverage/go-app.txt`, `web/coverage/index.html`. В CI сводка выводится в Summary задачи `test`, отчёты — в артефакте `coverage`. Почему имитации вне порога — [README, «Тесты и покрытие»](../README.md#тесты-и-покрытие).

## Go — приложение по пакетам

| Пакет | Statements | Покрыто |
|---|---:|---:|
| `cmd/api` | 5 | 0.0% |
| `content` | 6 | 100.0% |
| `contracts` | 105 | 83.8% |
| `internal/access` | 28 | 89.3% |
| `internal/access/sync` | 80 | 91.2% |
| `internal/actions` | 369 | 87.0% |
| `internal/actor` | 2 | 50.0% |
| `internal/agent/guard` | 51 | 94.1% |
| `internal/agent/llm` | 217 | 95.9% |
| `internal/agent/pii` | 8 | 87.5% |
| `internal/agent/reply` | 18 | 88.9% |
| `internal/agent/router` | 850 | 93.4% |
| `internal/agent/rules` | 272 | 98.9% |
| `internal/app` | 79 | 84.8% |
| `internal/auth` | 58 | 89.7% |
| `internal/auth/sysactor` | 1 | 100.0% |
| `internal/checklist/fsm` | 26 | 100.0% |
| `internal/clock` | 12 | 100.0% |
| `internal/config` | 118 | 99.2% |
| `internal/demo` | 45 | 82.2% |
| `internal/employees` | 52 | 98.1% |
| `internal/events` | 31 | 100.0% |
| `internal/flags` | 11 | 100.0% |
| `internal/health` | 104 | 98.1% |
| `internal/httpapi` | 936 | 86.2% |
| `internal/httpx` | 35 | 91.4% |
| `internal/kb` | 100 | 90.0% |
| `internal/mcpclient` | 114 | 93.0% |
| `internal/migrate` | 39 | 82.1% |
| `internal/path` | 150 | 96.0% |
| `internal/reqctx` | 9 | 100.0% |
| `internal/store` | 34 | 88.2% |
| `internal/testkit` | 28 | 96.4% |
| `internal/testkit/pgtest` | 75 | 81.3% |
| `internal/timeoff/rules` | 70 | 100.0% |
| `internal/tracks` | 151 | 88.7% |
| `internal/yamlx` | 12 | 100.0% |
| `migrations` | 4 | 100.0% |

`cmd/*` — только точки входа `main` (чтение конфигурации и запуск), их проверяет smoke-тест на поднятом стенде, а не модульные тесты.
