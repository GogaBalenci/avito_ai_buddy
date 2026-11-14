## 1. S1 — к C3 (12.11 18:00); п. 8–9 — после C3
- [x] 1.1 `internal/tracks` (загрузчик, правила 1–8) + `common.yaml`, `backend.yaml` из CSV; тесты L-01…L-04.
- [x] 1.2 `internal/checklist/fsm` + тест P-07.
- [x] 1.3 `mcp-mocks/checklist`: `get_path`, `get_step`, `set_step_status`; тесты C-01…C-11.
- [x] 1.4 `internal/path`: день, этап, сортировка, «Что дальше», прогресс; тесты P-01…P-06.
- [x] 1.5 API: `GET /api/path`, `POST …/status`, `GET /api/config`; payload `step:` во входе чата (общий парсер `09` §2.1); маршруты `my_path`, `step_explain`, `step_done` в оркестраторе; тесты PA-01…PA-14, PA-17.
- [x] 1.6 Фронт: панель «Мой путь», карточка «Что дальше», `useMarkStep`, состояния; тесты PF-01…PF-10.
- [x] 1.7 Сиды S1 (относительные даты) + сброс; smoke-шаги §1.17.
- [x] 1.8 Экран «Мои стажёры» (только чтение) — после C3, **не режется** (решение команды: черта отсечения).
- [x] 1.9 `frontend.yaml` — п. 3 черты отсечения (≈0,5 ч).
