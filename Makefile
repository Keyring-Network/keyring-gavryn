.PHONY: up down logs ps dev smoke coverage docs-drift

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f --tail=100

ps:
	docker compose ps

dev:
	./scripts/dev.sh

smoke:
	./scripts/smoke.sh

coverage:
	./scripts/coverage-all.sh

docs-drift:
	./scripts/check-docs-drift.sh
