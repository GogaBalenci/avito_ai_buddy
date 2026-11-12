# Change: add-agent — общее поведение агента «Авиитик Витя»

## Why
Стажёру нужен один собеседник, который понимает свободный текст и кнопки, отвечает коротко со ссылкой на источник и ничего не делает во внешних сервисах без явного подтверждения. Ключа платной LLM у команды нет, поэтому все сценарии обязаны работать в режиме `demo` на правилах, а модель — только улучшение. Права, ПДн и тестовые пометки должен обеспечивать код, а не промпт.

## What Changes
- Конвейер сообщения: вход `text` или `payload` (`intent:`, `step:`, `resource:`, `clarify:`), `client_message_id` против дублей, история, вход по тестовой учётке (capability `agent-core`).
- Распознавание намерений: правила `demo` из YAML с защитой от ложных срабатываний, схема `IntentResult`, промпт `intent-v1`, приоритет защитных тем над моделью (`agent-intents`).
- Ответы: шаблоны, формат `AgentReply`, переформулировка `rephrase-v1` с проверкой `ReplyGuard`, состояния маскота, вывод без исполнения HTML (`agent-replies`).
- Ограничения: подтверждение только кнопкой, эскалация к наставнику черновиком, права в SQL поиска, маскирование ПДн, без финансов, защита от prompt injection (`agent-guardrails`).

## Impact
- Affected specs: agent-core, agent-intents, agent-replies, agent-guardrails (новые).
- Affected code: `internal/agent/{intent,rules,llm,router,reply,guard,pii,prompts}`, `internal/actions`, `internal/httpapi`, `internal/auth`, `web/src/features/chat`.
- Источник: `docs/09-spec-agent.md` v1.3 (AG-01…AG-26, §14).
