# Change: add-vitya-character — характер «Авиитика Вити» и две доработки по проверке LLM

## Why
Маскот задаёт тон продукта (критерий «пользовательский опыт», 25%), но сейчас Витя говорит канцеляритом («Не понял тебя. Я умею вот это:»), не поддерживает простую болтовню и выглядит статично. Проверка 13.11 на реальном ключе Yandex AI Studio нашла два неверных ответа: подмена роли через сообщение распознана моделью как `my_requests` (показан список заявок), а «скинь телефон моего наставника» ушло в `kb_question` вместо отказа `personal_data`. Оба случая безопасны, но выглядят как ошибка бадди.

## What Changes
- Голос Вити в шаблонах «не понял» и болтовни, детерминированный выбор варианта по `client_message_id` (capability `agent-character`, AG-27).
- Раздел о голосе в промпте переформулировки `rephrase-v2`; `ReplyGuard` без изменений (AG-28).
- Болтовня — тема `small_talk:*` намерения `help`, низший приоритет, шаблон + возврат к делу, без модели (AG-29).
- Защитная тема `role_change` в правилах, важнее модели; R3 из `13` (`agent-guardrails`, AG-30).
- `personal_data` ловит «телефон/почта/контакты/телеграм наставника/коллеги/руководителя» парой слов (`agent-guardrails`, AG-31).
- Фронт: приветствие пустого чата по данным пути (AG-32), CSS-анимации маскота с `prefers-reduced-motion` (AG-33), тост-праздник при закрытии этапа (AG-34).

## Impact
- Affected specs: agent-character (новая), agent-guardrails (добавлены требования).
- Affected code: `internal/agent/rules` (`intents.ru.yaml`, пары слов, болтовня), `internal/agent/router` (`voice.go`, маршруты), `internal/agent/llm/prompts` (`intent.system.txt` → `intent-v2`, `rephrase.system.txt` → `rephrase-v2`, `intent-result.v1.json`: `topic` + `role_change`), `web/src/features/chat/ChatPane.tsx`, `web/src/shared/mascot`, `web/src/features/path`.
- Контракты API (OpenAPI, Zod) не меняются. Тест-векторы дат, матрица прав и smoke — без изменений.
- Источник: `docs/09-spec-agent.md` v1.6 (AG-27…AG-34, §17), `docs/13-permissions.md` v1.2 (R3).
