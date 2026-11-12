# AI-buddy «Авиитик Витя» — контекст проекта для OpenSpec

ИИ-наставник для стажёров Авито (кейс Hack&Change 2026). Показывает путь адаптации и следующий шаг, отвечает по синтетической базе знаний со ссылкой на источник, оформляет отгул и заявки на доступ через MCP-имитации корпоративных сервисов — только после явного подтверждения.

## Стек
- Backend: Go 1.26, PostgreSQL 18, Redis 8; сервисы `api` и `mcp-mocks` (MCP Streamable HTTP).
- Frontend: React 19, TypeScript, TanStack Router/Query, Zustand, Zod; nginx.
- Запуск: `docker compose up --build --wait`; LLM необязательна (`LLM_PROVIDER=demo`).

## Где что лежит
- Полные спецификации (источник правды): `docs/09-spec-agent.md`, `docs/10-spec-s3-time-off.md`, `docs/11-spec-s1-s2-s4.md`.
- Изменения: `openspec/changes/add-*` — proposal, design, tasks и требования как `## ADDED Requirements`. Реализованные изменения заархивированы в `openspec/changes/archive/`, их требования — в `openspec/specs/<capability>/spec.md` (12.11).
- После реализации `openspec archive <change>` переносит требования в `openspec/specs/<capability>/spec.md`.

## Соглашения
- Требования нумеруются как в спецификациях (`AG-xx`, `TO-xx`, `MP-xx`, `KQ-xx`, `AR-xx`), сценарии — как критерии приёмки (`AC-xx`). Номера используются в тестах.
- Все данные синтетические; инструкции и сервисы — тестовые, не регламенты Авито.
- Проверка: `openspec validate --all --strict` (CI).
