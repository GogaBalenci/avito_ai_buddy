-- Роль Grafana: только чтение событий и статистики этапов (13: минимальные права).
-- Выполняется сервисом grafana-init профиля observability; идемпотентно.
SELECT format('CREATE ROLE grafana_reader LOGIN PASSWORD %L', :'pw')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'grafana_reader') \gexec
SELECT format('ALTER ROLE grafana_reader WITH LOGIN PASSWORD %L', :'pw') \gexec
GRANT CONNECT ON DATABASE :"db" TO grafana_reader;
GRANT USAGE ON SCHEMA app TO grafana_reader;
GRANT SELECT ON app.events, app.path_period_stats TO grafana_reader;
-- никаких других таблиц: сообщения чата, заявки и сотрудники Grafana не видит
