# Makefile — только для удобства. У каждой цели есть эквивалент через docker compose
# (на Windows make обычно нет — используйте команды из комментариев или README).

.PHONY: up down test smoke

up: ## docker compose up --build --wait
	docker compose up --build --wait

down: ## docker compose down
	docker compose down

test: ## docker compose --profile test run --rm test-go && docker compose --profile test run --rm test-web
	docker compose --profile test run --rm test-go
	docker compose --profile test run --rm test-web

smoke: ## docker compose --profile test run --rm smoke
	docker compose --profile test run --rm smoke
