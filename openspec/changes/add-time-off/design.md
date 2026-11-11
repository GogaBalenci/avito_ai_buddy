# Design: add-time-off

Полное описание — `docs/10-spec-s3-time-off.md` v1.3 (§5–§12, §14).

## Фикстуры сценариев (§15)
Если не сказано иное: `today = 2026-11-10` (вторник), 12:00 Europe/Moscow, `LLM_PROVIDER=demo`, пользователь `u-intern-be`, наставник `u-mentor`, сид `ABS-000101` на `2026-11-16..2026-11-17`.

## Поток (§5)
Намерение → тестовый порядок `[1]` + `open_form` (с `existing` из `list_requests`) → Zod-подсказка → `POST /api/actions` (автомат S3, 201 или 422) → карточка-черновик → `confirm` (повторный guard и валидация) → `absences.create_request` с `idempotency_key = action_id` → копия в `time_off_requests`, `action_log`, события → «Готово» + «Скопировать сообщение наставнику».

## Правила дат (§6)
`REQUIRED → INVALID_DATE → DATE_IN_PAST → TOO_FAR → WEEKEND_DATE`, затем `RANGE_REVERSED → TOO_LONG`, затем `OVERLAP` (только сервер). До 5 рабочих дней, начало и конец — рабочие дни, горизонт 90 дней, праздники не учитываются. «Сегодня» — только на сервере.

## Черновик (§8.1)
Ключ Redis `action:{id}` живёт 24 ч, срок черновика — `expires_at` (15 мин). Состояния `active → confirming → confirmed | cancelled | replaced`; `expired` вычисляется. 404 — чужой или несуществующий, 410 — истёк, отменён или заменён, повтор `confirm` подтверждённого — тот же результат.

## MCP `absences` (§11)
`create_request`: схема → guard (только `intern`) → ключ идемпотентности → флаг сбоя → `rules.Validate` → advisory lock + `OVERLAP` → вставка; уникальный индекс `idempotency_key` закрывает гонку. Ответ проверяется `ToolClient` по `outputSchema`. Флаг `mock_absences_fault` хранится в `mock_admin.flags`, `slow` — последним.

## Хранение (§12)
Копия результата в `app.time_off_requests`; блок «Мои заявки», `/requests` (`RequestList`), `upcoming_time_off` у наставника.

## Риски
- Гонки двух `confirm` и двух черновиков — тесты против PostgreSQL из compose.
- Праздники не учитываются — записано в тестовом порядке и README.
