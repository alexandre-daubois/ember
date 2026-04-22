.DEFAULT_GOAL := help

.PHONY: local clean help

up: ## Start docker compose via build & up in the local folder
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed. Please install it."; exit 1; }
	docker compose build && docker compose up -d
	@echo ""
	@echo "  FrankenPHP  : http://localhost:8080"
	@echo "  Caddy admin : http://localhost:2019"

down: ## Clean docker compose in the local folder
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed. Please install it."; exit 1; }
	docker compose down --remove-orphans

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
