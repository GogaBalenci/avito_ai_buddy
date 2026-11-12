## 1. Вход и правила demo (к C3)
- [x] 1.1 `POST /api/chat/messages`: ровно одно из `text`/`payload`, грамматика payload, `client_message_id` (Redis, 10 мин), лимиты, `GET /api/chat/history`
- [x] 1.2 `internal/agent/pii`: маскирование до БД, логов и модели; тесты на `1.13`, `ABS-000123`, даты
- [x] 1.3 `intents.ru.yaml` + загрузчик со стеммингом; алгоритм §6.3 (отрицание, несколько намерений, защитные темы первыми)
- [x] 1.4 Маршруты `help`, `unknown`, `my_path`, `step_explain`, `step_done`, `time_off`; ответы наставнику на сценарии стажёра
- [x] 1.5 Тесты T-01…T-14, T-21, T-23…T-26, T-30, T-33, T-35; таблицы перефразировок и ложных срабатываний

## 2. Подтверждение и черновики (к C3, общие с add-time-off)
- [x] 2.1 Модель черновика `10` §8.1: `POST /api/actions`, `GET /api/actions/{id}`, `confirm`, `cancel`; `expired` как вычисляемое состояние
- [x] 2.2 «Да» текстом не подтверждает; тесты §14.1-4, §14.1-5, §14.1-15, §14.1-16

## 3. База знаний, доступы, эскалация (к C4)
- [x] 3.1 Маршруты `kb_question` (порядок §6.5), `access_request`, `my_requests`, `ask_mentor`
- [x] 3.2 Эскалация: черновик вопроса, `copy_event`, `POST /api/client-events`
- [x] 3.3 Тесты T-15…T-20, T-22, T-27…T-29, T-31, T-32, T-34, T-39

## 4. LLM (13.11)
- [x] 4.1 `LLMClient` `openai_compatible`: `json_schema` или `json_object`, таймаут, circuit breaker, флаги `llm_enabled`/`llm_rephrase`
- [x] 4.2 Промпты `intent-v1`, `rephrase-v1` (`go:embed`), `prompt_version` в событиях
- [x] 4.3 `ReplyGuard` (7 правил), `GET /api/llm/health`; конфигурация без `LLM_MODEL` и 401 → demo с плашкой
- [x] 4.4 Тесты T-36…T-38, адаптер на фейковом HTTP-сервере, §14.1-2, -3, -10, -13, -14, -17

## 5. Фронт
- [x] 5.1 Чат: состояния, `thinking` на клиенте, отмена после 5 с, текст без HTML, маркеры `[n]`, плашки по `label`, индикатор режима
- [x] 5.2 Тест XSS (§14.1-18)
