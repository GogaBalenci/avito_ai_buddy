## ADDED Requirements

### Requirement: MP-01 Этап по дню от старта
Система SHALL вычислять день как (today − start_date) + 1 по серверной дате Europe/Moscow
и текущий этап по границам периодов из common.yaml; при day < 1 SHALL показывать состояние pre_start.

#### Scenario: AC-P01 Демо-стажёр Backend на неделе 1
- **GIVEN** u-intern-be с start_date = 2026-11-07
- **WHEN** он открывает «Мой путь»
- **THEN** current_period = week1, day = 4, в заголовке «Неделя 1 — Вход в команду · день 4 из 7»

#### Scenario: AC-P02 Граница этапов
- **GIVEN** start_date = 2026-11-03 (day = 8)
- **WHEN** открыт «Мой путь»
- **THEN** current_period = week2, а незакрытые шаги week1 показаны как «Хвост этапа «Неделя 1»»

#### Scenario: AC-P03 Старт в будущем
- **GIVEN** start_date = 2026-11-12
- **WHEN** открыт «Мой путь»
- **THEN** state = pre_start, текст «Стажировка начнётся 12.11», next_step.reason = ahead

### Requirement: MP-02 Только шаги стажёра в правильном порядке
Система SHALL показывать стажёру и наставнику на экране стажёра только шаги owner=intern
из общего трека и трека направления, отсортированные по order, затем common раньше направления,
затем step_id.

#### Scenario: AC-P04 Шаги наставника скрыты
- **WHEN** u-intern-be открывает «Мой путь»
- **THEN** в steps нет cm-1.1…cm-1.5, на неделе 1 ровно 12 шагов

#### Scenario: AC-P05 Ничья по order
- **GIVEN** u-intern-fe и шаги cm-1.6 и fe-w1-10 с order = 10
- **WHEN** открыт этап week1
- **THEN** cm-1.6 стоит раньше fe-w1-10

### Requirement: MP-03 Статусы и переходы
Статус шага SHALL принимать значения not_started, in_progress, waiting, done и меняться только
по таблице переходов; шаг с help.kind=access_request SHALL NOT закрываться пользователем вручную;
статус SHALL NOT меняться из свободного текста в чате.

#### Scenario: AC-P06 Основная кнопка переводит в работу
- **GIVEN** шаг be-1.12 в статусе not_started
- **WHEN** стажёр нажимает «Разобраться»
- **THEN** клиент отправляет {client_message_id: <новый uuid>, payload: "step:be-1.12"}
- **AND** статус in_progress, в чате объяснение со ссылкой и кнопкой «Отметить пройденным»,
  в истории чата нажатие показано подписью кнопки

#### Scenario: AC-P07 Шаг с заявкой нельзя закрыть вручную
- **WHEN** POST /api/path/steps/cm-1.10/status {status: done}
- **THEN** ответ 409 TRANSITION_NOT_ALLOWED, статус остаётся waiting

#### Scenario: AC-P08 Текст не меняет статус
- **WHEN** стажёр пишет «я изучил авито в цифрах, шаг 1.12»
- **THEN** статус be-1.12 не меняется, в ответе карточка шага с кнопкой «Отметить пройденным»

#### Scenario: AC-P23 Шаг не из пути
- **WHEN** стажёр отправляет payload "step:cm-1.1" (шаг наставника) или "step:fe-w1-10" (шаг другого трека)
- **THEN** ответ 404 «Такого шага в твоём пути нет», статусы не меняются

#### Scenario: AC-P24 Кнопка по готовому и ожидающему шагу
- **WHEN** стажёр отправляет payload "step:cm-1.6" (done)
- **THEN** ответ «Шаг уже отмечен» с кнопкой «Вернуть в работу», статус done
- **AND WHEN** стажёр отправляет payload "step:cm-1.10" (waiting)
- **THEN** ответ показывает статус заявки ACC-000101, новый черновик не создан

#### Scenario: AC-P25 Повтор нажатия
- **GIVEN** ответ на {client_message_id: X, payload: "step:be-1.12"} уже получен
- **WHEN** клиент повторно отправляет X
- **THEN** возвращается тот же reply_id, событие step_started записано один раз

### Requirement: MP-04 Один следующий шаг
Система SHALL показывать ровно один следующий шаг по алгоритму: хвосты прошлых этапов,
затем текущий этап, затем ближайший будущий с пометкой «Можно начать заранее»; шаги в статусе
waiting SHALL NOT становиться следующим шагом и SHALL показываться плашкой «Ждём ответа».

#### Scenario: AC-P09 Первый экран демо Backend
- **GIVEN** сиды демо
- **WHEN** u-intern-be открывает «Мой путь»
- **THEN** next_step = cm-1.7 (reason = current), waiting = [cm-1.10], прогресс этапа «2 из 12»

#### Scenario: AC-P10 Хвост прошлого этапа
- **GIVEN** сиды демо
- **WHEN** u-intern-fe открывает «Мой путь»
- **THEN** next_step = fe-w1-50 с reason = overdue и подписью «Хвост этапа «Неделя 1»»

#### Scenario: AC-P11 Этап закрыт, остались ожидающие
- **GIVEN** все шаги week1 done, кроме cm-1.10 (waiting)
- **WHEN** открыт «Мой путь»
- **THEN** next_step — первый шаг week2 с reason = ahead, плашка «Ждём ответа» по cm-1.10

#### Scenario: AC-P12 Всё пройдено
- **GIVEN** все видимые шаги done
- **THEN** state = finished, next_step = null, Витя в состоянии done

### Requirement: MP-05 Отметить пройденным
Кнопка «Отметить пройденным» SHALL сохранять статус done идемпотентно по idempotency_key
с проверкой expected_status, SHALL обновлять интерфейс оптимистично и откатывать изменение
при ошибке; готовый шаг (кроме шагов с заявкой) SHALL возвращаться в работу.

#### Scenario: AC-P13 Отметка и пересчёт
- **GIVEN** be-1.11 в статусе in_progress
- **WHEN** стажёр нажимает «Отметить пройденным»
- **THEN** ответ 200 с path_summary, прогресс «3 из 12», событие step_done{source: user}
- **AND** повторный запрос с тем же idempotency_key возвращает тот же результат без второго события

#### Scenario: AC-P14 Конфликт вкладок
- **GIVEN** be-1.11 уже done в другой вкладке
- **WHEN** POST status done с expected_status = in_progress
- **THEN** ответ 409 STATUS_CONFLICT с current_status = done, UI перечитывает путь

#### Scenario: AC-P15 Отмена отметки
- **WHEN** стажёр нажимает «Отменить» в тосте в течение 5 с
- **THEN** шаг снова in_progress, событие step_reopened

### Requirement: MP-06 Направление — это данные
Путь SHALL строиться из content/tracks/*.yaml по формату schema_version 1; загрузчик SHALL проверять
правила 1–8 и при нарушении SHALL останавливать старт с сообщением, где указаны файл, шаг и правило;
новый трек SHALL появляться в API без изменения кода.

#### Scenario: AC-P16 Новый трек без кода
- **GIVEN** во временной папке content/tracks/ добавлен qa.yaml и сотрудник с track = qa
- **WHEN** загрузчик перезапущен
- **THEN** GET /api/path этого сотрудника возвращает шаги common + qa

#### Scenario: AC-P17 Тестовый трек помечен
- **WHEN** u-intern-fe открывает «Мой путь»
- **THEN** видна плашка с disclaimer трека, у шагов fe-* чип «тестовый»

#### Scenario: AC-P18 Невалидный трек
- **GIVEN** в YAML шаг ссылается на несуществующий период
- **WHEN** загрузчик запущен
- **THEN** ошибка содержит имя файла, id шага и номер правила; сервис не стартует

### Requirement: MP-07 Демо относительно сегодняшнего дня
Сиды SHALL задавать даты старта относительно текущего дня (DEMO_INTERN_BE_DAYS_AGO,
DEMO_INTERN_FE_DAYS_AGO); «Сбросить демо-данные» SHALL возвращать прогресс, заявки,
черновики и флаги к сидам.

#### Scenario: AC-P19 Эксперт проверяет через неделю
- **GIVEN** сиды применены 2026-11-17 через «Сбросить демо-данные»
- **WHEN** u-intern-be открывает «Мой путь»
- **THEN** day = 4, current_period = week1, next_step = cm-1.7

### Requirement: MP-08 Наставник видит прогресс только для чтения
Наставник SHALL видеть путь только своих стажёров и SHALL NOT менять статусы шагов.

#### Scenario: AC-P20 Экран «Мои стажёры»
- **WHEN** u-mentor открывает «Мои стажёры»
- **THEN** видны u-intern-be и u-intern-fe с этапом и прогрессом; путь открывается с read_only = true
  и без кнопок действий

#### Scenario: AC-P21 Запись от наставника
- **WHEN** u-mentor отправляет POST /api/path/steps/cm-1.6/status
- **THEN** ответ 403, событие permission_denied

#### Scenario: AC-P26 Наставник в чате
- **WHEN** u-mentor пишет «что мне делать дальше?»
- **THEN** ответ «Это сценарий для стажёров…» со ссылкой «Мои стажёры»,
  событие permission_denied{tool: "route:my_path", role: mentor}

#### Scenario: AC-P27 Ближайший отгул у наставника
- **GIVEN** сиды демо (у u-intern-be заявка ABS-000101)
- **WHEN** u-mentor открывает «Мои стажёры»
- **THEN** у u-intern-be есть upcoming_time_off с ABS-000101 и pending_access_requests = 1

### Requirement: MP-09 Ошибки и состояния экрана
Панель SHALL иметь состояния loading, ready, pre_start, only_waiting, finished, error и stale;
сбой сервиса чек-листа SHALL давать понятную ошибку с повтором и SHALL NOT блокировать чат.

#### Scenario: AC-P22 Сервис чек-листа недоступен
- **GIVEN** checklist не отвечает дольше MCP_TIMEOUT_MS
- **WHEN** открыт «Мой путь»
- **THEN** панель в состоянии error с кнопкой «Повторить», чат принимает сообщения
