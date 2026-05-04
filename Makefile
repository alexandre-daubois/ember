.DEFAULT_GOAL := help

BINARY    := ember
CMD       := ./cmd/ember
LDFLAGS   := -s -w

GO_TEST_FLAGS := -race -shuffle=on -count=1

.PHONY: build test test-nocolor lint coverage bench check integration fuzz clean help

build: ## Build the binary
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) $(CMD)

test: ## Run tests with race detector, shuffle and no cache
	go test ./... $(GO_TEST_FLAGS)

test-nocolor: ## Run UI tests under NO_COLOR=1
	NO_COLOR=1 go test ./internal/ui/... $(GO_TEST_FLAGS)

lint: ## Run golangci-lint
	golangci-lint run

bench: ## Run benchmarks
	go test -bench=. -benchmem ./internal/app/

integration: ## Run integration tests (requires running Caddy)
	go test -tags integration $(GO_TEST_FLAGS) -v ./internal/app/

fuzz: ## Run all fuzz targets for 30s each
	./scripts/run_fuzz.sh 30s

check: lint test test-nocolor ## Run lint + tests + NO_COLOR variant

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.txt integration-coverage.txt

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
