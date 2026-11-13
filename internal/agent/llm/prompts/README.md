# Промпты LLM (версионируются, AG-22)

| Файл | Версия | Где описан |
|---|---|---|
| `intent.system.txt` + `intent.user.tmpl` | `intent-v1` | `docs/09-spec-agent.md` §4 |
| `rephrase.system.txt` | `rephrase-v2` | `docs/09-spec-agent.md` §7, §17.2 |

При изменении текста поднимите версию в `internal/agent/llm/llm.go`: она пишется в события (`prompt_version`).
