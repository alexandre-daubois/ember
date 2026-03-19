.DEFAULT_GOAL := help

BINARY    := ember
CMD       := ./cmd/ember
LDFLAGS   := -s -w

.PHONY: build test lint coverage bench check snapshot docker clean help

build: ## Build the binary
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)

test: ## Run tests with race detector
	go test ./... -race

lint: ## Run golangci-lint
	golangci-lint run

bench: ## Run benchmarks
	go test -bench=. -benchmem ./internal/app/

check: lint test ## Run lint + test (same as CI)

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.txt

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
