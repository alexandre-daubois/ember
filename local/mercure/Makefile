.DEFAULT_GOAL := help

.PHONY: up down traffic start help

up: ## Start the standalone Mercure hub (no HTTP metrics: Ember's hub-only mode)
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed. Please install it."; exit 1; }
	docker compose up -d
	@echo ""
	@echo "  Mercure hub : http://localhost:8082/.well-known/mercure"
	@echo "  Caddy admin : http://localhost:2020"

down: ## Clean docker compose in the local folder
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed. Please install it."; exit 1; }
	docker compose down --remove-orphans

traffic: ## Subscribe and publish against the local hub (SUBSCRIBERS=N, DURATION=S)
	@./load.sh

start: ## Run Ember against the local hub (log listener reachable from Docker)
	EMBER_LOG_LISTEN=host.docker.internal:9211 go run ../../cmd/ember --addr http://localhost:2020

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
