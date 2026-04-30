.DEFAULT_GOAL := help

.PHONY: up down traffic ember help

up: ## Start two Caddy instances (blue on :2019, green on :2020)
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed."; exit 1; }
	docker compose up -d
	@echo ""
	@echo "  Blue  Caddy admin : http://localhost:2019  | site: http://localhost:8081"
	@echo "  Green Caddy admin : http://localhost:2020  | site: http://localhost:8082"
	@echo ""
	@echo "  Ember example   : ember --daemon --expose :9191 \\"
	@echo "                          --addr blue=http://localhost:2019 \\"
	@echo "                          --addr green=http://localhost:2020"

down: ## Stop and remove the multi-Caddy stack
	docker compose down --remove-orphans

traffic: ## Generate light traffic against both instances
	@for i in $$(seq 1 50); do \
		curl -s http://localhost:8081/ > /dev/null & \
		curl -s http://localhost:8082/ > /dev/null & \
		curl -s http://localhost:8081/health > /dev/null & \
		curl -s http://localhost:8082/health > /dev/null & \
		sleep 0.05; \
	done; wait
	@echo "  done"

ember: ## Run Ember in daemon mode against both Caddy instances
	go run ../../cmd/ember --daemon --expose :9191 \
		--addr blue=http://localhost:2019 \
		--addr green=http://localhost:2020

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
