API_DIR=backend
WORKER_DIR=worker
FRONTEND_DIR=frontend

.PHONY: api
api:
	cd $(API_DIR) && go run ./cmd/api

.PHONY: worker
worker:
	cd $(WORKER_DIR) && uv run python -m app.main

.PHONY: frontend
frontend:
	cd $(FRONTEND_DIR) && bun run dev

.PHONY: dev
dev:
	./scripts/dev.sh

.PHONY: tunnel
tunnel:
	./scripts/tunnel.sh
