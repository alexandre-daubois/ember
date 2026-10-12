.DEFAULT_GOAL := help

.PHONY: certs up down traffic start help

certs: ## Generate in ./certs a CA, a certificate for the daemon on localhost and one for the TUI
	@command -v openssl >/dev/null 2>&1 || { echo >&2 "openssl is required"; exit 1; }
	mkdir -p certs
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 30 \
		-subj "/CN=Ember demo CA" -addext "basicConstraints=critical,CA:TRUE" \
		-keyout certs/ca-key.pem -out certs/ca.pem
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=localhost" \
		-keyout certs/server-key.pem -out certs/server.csr
	printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' > certs/server.ext
	openssl x509 -req -days 30 -in certs/server.csr -CA certs/ca.pem -CAkey certs/ca-key.pem \
		-CAcreateserial -extfile certs/server.ext -out certs/server.pem
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=demo-client" \
		-keyout certs/client-key.pem -out certs/client.csr
	printf 'extendedKeyUsage=clientAuth\n' > certs/client.ext
	openssl x509 -req -days 30 -in certs/client.csr -CA certs/ca.pem -CAkey certs/ca-key.pem \
		-CAcreateserial -extfile certs/client.ext -out certs/client.pem

up: ## Start FrankenPHP and the Ember daemon (run `make certs` first)
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed. Please install it."; exit 1; }
	@test -f certs/server.pem || { echo >&2 "Run 'make certs' first."; exit 1; }
	docker compose build && docker compose up -d
	@echo ""
	@echo "  FrankenPHP (Normal)  : http://localhost:8080"
	@echo "  FrankenPHP (Workers) : http://localhost:8081"
	@echo "  Ember daemon         : https://localhost:9191 (admin API not published)"

down: ## Clean docker compose in the local folder
	@command -v docker >/dev/null 2>&1 || { echo >&2 "Docker is not installed. Please install it."; exit 1; }
	docker compose down --remove-orphans

traffic: ## Generate traffic against the local FrankenPHP (WORKERS=N, DURATION=S)
	@../frankenphp/load.sh

start: ## Run the Ember TUI against the daemon, without access to the admin API
	go run ../../cmd/ember --remote https://localhost:9191 --ca-cert certs/ca.pem \
		--client-cert certs/client.pem --client-key certs/client-key.pem

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
