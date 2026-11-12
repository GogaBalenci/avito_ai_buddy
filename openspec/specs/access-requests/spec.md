# access-requests Specification

## Purpose
S4 «Запросы доступов и в каналы» (`11` раздел 3): каталог ресурсов с уровнем доступа и согласующим, правила auto/mentor/timer, черновик и подтверждение заявки, синхронизация статуса заявки с шагом пути, решение наставника во «Входящих», отказ для запрещённых ресурсов.

## Requirements

### Requirement: AR-01 Каталог и права на ресурсы
Система SHALL позволять стажёру запрашивать только ресурсы, где allowed_roles содержит intern;
запрос запрещённого ресурса SHALL отклоняться с объяснением без создания черновика и заявки,
а права SHALL проверяться в API, оркестраторе и mcp-mocks независимо.

#### Scenario: AC-A01 Запись в продовую БД
- **GIVEN** роль intern
- **WHEN** стажёр пишет «дай доступ на запись в продовую базу данных»
- **THEN** pending_action = null, заявка не создана, mascot_state = ask_mentor,
  текст объясняет, что роли стажёра это не положено, и предлагает обсудить с наставником
- **AND** события permission_denied{object: prod-db-write} и escalated{reason: permission_denied}

#### Scenario: AC-A02 Обход оркестратора
- **WHEN** в mcp-mocks вызывается create_request(prod-db-write) с actor.role = intern
- **THEN** ошибка PERMISSION_DENIED с details.reason = ROLE_NOT_ALLOWED

#### Scenario: AC-A03 Ресурс не распознан
- **WHEN** стажёр пишет «нужен доступ»
- **THEN** бадди предлагает список разрешённых ресурсов быстрыми ответами с payload "resource:<id>",
  prod-db-write в списке нет

#### Scenario: AC-A21 Payload ресурса
- **WHEN** стажёр отправляет payload "resource:prod-db-write"
- **THEN** ответ 403, черновика нет, событие permission_denied{object: prod-db-write}
- **AND WHEN** стажёр отправляет payload "resource:nope"
- **THEN** ответ 404 «Такого ресурса в тестовом каталоге нет»

#### Scenario: AC-A19 Наставник в чате
- **GIVEN** роль mentor
- **WHEN** наставник пишет «добавь меня в tech-dep»
- **THEN** черновика нет, ответ «Это сценарий для стажёров. Входящие заявки — на экране „Входящие“»
  со ссылкой, событие permission_denied{tool: "route:access_request", role: mentor}

### Requirement: AR-02 Черновик и явное подтверждение
Заявка SHALL создаваться только по POST /api/actions/{action_id}/confirm от владельца активного черновика;
черновик SHALL жить по модели 10 §8.1 (404 — чужой или несуществующий, 410 — истёкший, отменённый
или заменённый, повтор confirm подтверждённого — тот же результат); карточка черновика SHALL показывать
ресурс, уровень доступа и правило обработки; текст «да» SHALL NOT подтверждать.

#### Scenario: AC-A04 Заявка из шага с одобрением сразу — два нажатия
- **GIVEN** u-intern-be, шаг cm-1.7 not_started
- **WHEN** стажёр нажимает «Запросить доступ» (payload "step:cm-1.7")
- **THEN** шаг cm-1.7 = in_progress, в ответе pending_action на team-channels с подсказкой «Одобряется сразу»
- **AND WHEN** стажёр нажимает «Подтвердить»
- **THEN** заявка ACC-NNNNNN в статусе approved с decided_by = auto,
  шаг cm-1.7 = done, next_step = be-1.11, mascot_state = done

#### Scenario: AC-A05 Заявка из чата
- **WHEN** стажёр пишет «добавь меня в tech-dep»
- **THEN** черновик на channel-tech-dep с подсказкой «одобрится автоматически примерно через 2 мин»

#### Scenario: AC-A22 Заменённый черновик
- **GIVEN** активный черновик A на channel-tech-dep
- **WHEN** стажёр создаёт черновик B на тот же ресурс, затем подтверждает A
- **THEN** ответ 410 «заменён», заявка не создана
- **AND** черновик на другой ресурс черновик A не заменяет

### Requirement: AR-03 Правила имитации
Сервис доступов SHALL обрабатывать заявку по правилу ресурса: auto — approved сразу; mentor — pending
до решения наставника; timer — approved при первом чтении не раньше created_at + ACCESS_TIMER_MINUTES,
без фоновых задач; при флаге access_mentor_approval = off правило mentor SHALL работать как timer.

#### Scenario: AC-A06 Таймер по created_at
- **GIVEN** заявка на channel-tech-dep создана в 12:00:00
- **WHEN** заявки читаются в 12:01:59
- **THEN** статус pending, шаг be-1.15 = waiting
- **AND WHEN** заявки читаются в 12:02:00
- **THEN** статус approved с decided_by = timer, шаг be-1.15 = done, событие access_request_decided один раз

#### Scenario: AC-A07 Ждёт наставника
- **GIVEN** сид ACC-000101 (space-cluster, policy mentor), создана сутки назад
- **WHEN** стажёр открывает «Мой путь»
- **THEN** заявка pending, шаг cm-1.10 = waiting, плашка «Ждём ответа»

#### Scenario: AC-A08 Режим без экрана наставника
- **GIVEN** флаг access_mentor_approval = off
- **WHEN** стажёр открывает «Мой путь»
- **THEN** ACC-000101 = approved с decided_by = timer, шаг cm-1.10 = done

### Requirement: AR-04 Идемпотентность и дубликаты
Один action_id SHALL давать не больше одной заявки; при активной заявке (pending или approved)
на тот же ресурс новая заявка SHALL NOT создаваться, а пользователь SHALL видеть статус существующей.

#### Scenario: AC-A09 Двойное подтверждение
- **WHEN** confirm вызван дважды (или 10 раз параллельно)
- **THEN** создана одна заявка, все ответы — 200 с одним request_id, action_completed один раз

#### Scenario: AC-A10 Повторный запрос
- **GIVEN** ACC-000101 pending
- **WHEN** стажёр снова запрашивает space-cluster
- **THEN** новая заявка не создана, ответ «Заявка №ACC-000101 уже ждёт наставника»

### Requirement: AR-05 Решение наставника
Наставник SHALL видеть входящие заявки только своих стажёров и SHALL одобрять или отклонять их
кнопками; отклонение SHALL требовать причину из фиксированного списка; повторное решение SHALL
возвращать ALREADY_DECIDED, а повтор с тем же ключом — тот же результат.

#### Scenario: AC-A11 Одобрение
- **GIVEN** u-mentor и ACC-000101 pending
- **WHEN** наставник нажимает «Одобрить»
- **THEN** заявка approved с decided_by = mentor, карточка уходит из «Входящих»
- **AND** при следующем опросе у u-intern-be шаг cm-1.10 = done и тост «Доступ к … одобрен»

#### Scenario: AC-A12 Отклонение
- **WHEN** наставник отклоняет заявку с причиной not_needed
- **THEN** у стажёра шаг возвращается в in_progress, чип «Отклонено: Пока не нужен для задач»,
  доступна кнопка «Запросить снова», и новая заявка на тот же ресурс создаётся

#### Scenario: AC-A13 Гонка с таймером
- **GIVEN** заявка timer, наступило created_at + N, заявки ещё не читались
- **WHEN** наставник нажимает «Одобрить»
- **THEN** ответ ALREADY_DECIDED с decided_by = timer, событие access_request_decided одно

#### Scenario: AC-A14 Чужие и запрещённые решения
- **WHEN** стажёр вызывает POST /api/mentor/access-requests/ACC-000101/decision
- **THEN** ответ 403
- **AND WHEN** наставник решает заявку не своего стажёра (фикстура)
- **THEN** ответ 404

### Requirement: AR-06 Статус без перезагрузки
Статус заявки SHALL возвращаться в шаг «Моего пути» через синхронизацию при чтении;
интерфейс стажёра SHALL опрашивать сервер с интервалом PATH_POLL_INTERVAL_MS, пока есть ожидающие
заявки, и SHALL давать кнопку «Обновить»; событие о решении SHALL публиковаться ровно один раз.

#### Scenario: AC-A15 Стажёр узнаёт об одобрении
- **GIVEN** вкладка стажёра открыта, ACC-000101 pending
- **WHEN** наставник одобряет заявку
- **THEN** не позже чем через PATH_POLL_INTERVAL_MS + 1 с шаг cm-1.10 у стажёра = done без перезагрузки

#### Scenario: AC-A16 Опрос выключается
- **GIVEN** у стажёра нет pending-заявок
- **THEN** фоновых запросов GET /api/path нет

### Requirement: AR-07 Сбои сервиса доступов
При недоступности сервиса при подтверждении система SHALL показывать понятную ошибку с «Повторить»
без дубликатов; при недоступности во время опроса SHALL показывать данные из локальной копии
с пометкой, что статусы не обновились.

#### Scenario: AC-A17 Сервис недоступен при подтверждении
- **GIVEN** фейковый сервис доступов отвечает UNAVAILABLE
- **WHEN** стажёр подтверждает черновик
- **THEN** ответ 503 retryable = true, mascot_state = oops, текст «заявка не создана»;
  повтор после восстановления создаёт одну заявку с тем же action_id

#### Scenario: AC-A18 Сервис недоступен при опросе
- **WHEN** GET /api/path при недоступном сервисе доступов
- **THEN** ответ 200 из локальной копии с stale_requests = true

### Requirement: AR-08 Шаг и ресурс заявки определяет сервер
Шаг заявки SHALL определяться сервером по help.resource шага из пути пользователя, а не передаваться
клиентом; алиас ресурса без глагола доступа SHALL NOT создавать черновик и SHALL давать только
быстрый ответ «Запросить доступ».

#### Scenario: AC-A20 Термин — не заявка
- **WHEN** стажёр пишет «что такое tech-dep»
- **THEN** pending_action = null, ответ по term-tech-dep (S2),
  в actions есть быстрый ответ «Запросить доступ: Канал Tech-dep» с payload "resource:channel-tech-dep"

#### Scenario: AC-A23 Ресурс не из пути
- **GIVEN** u-intern-be (трек backend)
- **WHEN** стажёр пишет «дай доступ к фигме» и подтверждает черновик
- **THEN** заявка на figma-library создана со step_id = null, видна в «Моих заявках»,
  статусы шагов не меняются
