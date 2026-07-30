# agentsurface
#
# Every target here is a short command you could type yourself. Read them
# before you run them; that is the habit this whole project is asking for.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BINARY  := agentsurface
PKG     := ./cmd/agentsurface
BIN_DIR := bin
VERSION ?= dev

# -trimpath and CGO_ENABLED=0 are not decoration. They are two of the three
# things that make the build reproducible, the third being a pinned Go version.
# See docs/VERIFY.md.
GO_BUILD_FLAGS := -trimpath
LDFLAGS        := -X main.version=$(VERSION)

.PHONY: help
help: ## Show this help
	@echo "agentsurface"
	@echo
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-10s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "  Before pushing, run: make check"

.PHONY: build
build: ## Build ./bin/agentsurface
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build $(GO_BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)
	@echo "built $(BIN_DIR)/$(BINARY)"

.PHONY: install
install: ## Install agentsurface into GOBIN
	CGO_ENABLED=0 go install $(GO_BUILD_FLAGS) -ldflags '$(LDFLAGS)' $(PKG)

.PHONY: test
test: ## Run the tests with the race detector
	CGO_ENABLED=1 go test -race -count=1 ./...

.PHONY: cover
cover: ## Run the tests and write coverage.out
	go test -count=1 -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

.PHONY: fmt
fmt: ## Format the tree with gofmt
	gofmt -w -s .
	@echo "formatted"

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Check formatting and run go vet, changing nothing
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt clean. Run 'make fmt'." >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi; \
	echo "gofmt clean"
	go vet ./...

.PHONY: verify
verify: ## Prove the binary cannot make network calls, exactly as CI does
	@bash .github/scripts/no-network.sh
	@go test -count=1 ./internal/networkguard/...

.PHONY: check
check: lint test verify ## Everything CI runs. Run this before you push.
	@echo
	@echo "lint, tests and the no-network check all passed"

.PHONY: snapshot
snapshot: ## Build the release artefacts locally without publishing (needs goreleaser)
	goreleaser release --snapshot --clean --skip=publish,announce,sign

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN_DIR) dist coverage.out coverage.html
	@echo "cleaned"
