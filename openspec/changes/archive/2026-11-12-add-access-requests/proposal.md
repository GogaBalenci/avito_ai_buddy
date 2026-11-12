# Change: add-access-requests — S4 «Запросы доступов и в каналы»

## Why
Шаги «попроси кого-то» (каналы, пространство кластера, Tech-dep) — ровно те оргдействия, которые кейс просит снять с наставника. Второе действие через корпоративный сервис с возвратом статуса в «Мой путь», правдоподобная имитация согласования и отказ по роли в коде инструмента. Без минимального S4 первый экран упирается в нерабочую кнопку `cm-1.7`.

## What Changes
- Каталог ресурсов (`content/access/catalog.yaml`) с `access_level`, подписью `approver`, правилами `auto | mentor | timer`; `prod-db-write` не положен никому.
- Черновик и подтверждение по общей модели `10` §8.1; обоснование заявки подставляет сервер из шага.
- MCP `access`: `list_resources`, `create_request`, `list_requests` (таймер при чтении), `decide_request`.
- Синхронизация «заявка → шаг» (`internal/access/sync`), опрос без перезагрузки; экран «Входящие» наставника.

## Impact
- Affected specs: access-requests (новая).
- Affected code: `internal/access/sync`, `internal/mcpmocks/{access,catalog}`, `contracts/access/*`, `internal/actions`, `web/src/features/access`, `web/src/features/mentor`.
- Источник: `docs/11-spec-s1-s2-s4.md` v1.2, раздел 3.
