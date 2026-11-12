# Design: add-my-path

Полное описание — `docs/11-spec-s1-s2-s4.md` v1.2, раздел 1 (§1.5–§1.15) и общий §0.

## Фикстуры сценариев (§0.1)
`today = 2026-11-10` (вторник), 12:00 Europe/Moscow, `LLM_PROVIDER=demo`; `u-intern-be` (backend, старт 3 дня назад, `week1`), `u-intern-fe` (frontend, тестовый трек, старт 10 дней назад, `week2`), оба у `u-mentor`.

## Модель (§1.5)
- `day = (today − start_date) + 1`; периоды из `common.yaml`; `day < 1` → `pre_start`.
- Путь = `common` + трек направления; видны только шаги `owner: intern`; сортировка `order` → common раньше направления → `step_id`.
- Статусы `not_started | in_progress | waiting | done`; переходы `user_*` (стажёр) и `access_*` (служебная роль `system`, только с `request_ref`); повтор перехода в текущий статус — no-op.
- «Что дальше»: overdue → current → ahead; `waiting` не становится следующим шагом.

## Почему логика в `internal/path`, а не в имитации (§0.4)
При замене `checklist` на реальный сервис чек-листа этапы и «Что дальше» не теряются: имитация хранит только треки и прогресс.

## MCP `checklist` (§1.11)
`get_path`, `get_step` — чтение; `set_step_status` — идемпотентно по ключу, оптимистичная блокировка `expected_status`, `fsm.Check`. Схемы — `contracts/checklist/*` (`get_step` — `12` §7.3).

## Данные и сиды (§1.12–§1.13)
Треки — `content/tracks/*.yaml`, 8 правил загрузчика, fail fast. Сиды относительно сегодняшнего дня: первый экран Backend — «Что дальше» = `cm-1.7`, ждём `cm-1.10`, «2 из 12».

## Метрика «неделя 1 вовремя» (§1.14)
По событиям `step_done{on_time}` и таблице `app.path_period_stats`, которую обновляет `GET /api/path`.

## Риски
- Первый экран зависит от S4 (`cm-1.7` — шаг с заявкой): минимальный S4 до C3 или временный сид `cm-1.7 = done`.
