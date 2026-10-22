# Developer tasks for weread-exporter. `make help` lists them.

BINARY := weread-exporter
MODULE := github.com/jtsang4/weread-exporter-go
CMD    := ./cmd/weread-exporter

# Build information, injected into internal/version (the same fields
# .goreleaser.yaml sets for the release binaries).
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.Date=$(DATE) \
	-X $(MODULE)/internal/version.BuiltBy=make

# The platforms of a release (keep in sync with .goreleaser.yaml).
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

GOLANGCI_LINT ?= golangci-lint
GORELEASER    ?= goreleaser

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this list
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build bin/weread-exporter for this machine
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

.PHONY: install
install: ## Install weread-exporter into $GOBIN (or $GOPATH/bin)
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" $(CMD)

.PHONY: run
run: ## Run the program: make run ARGS="info -b 34336681"
	go run $(CMD) $(ARGS)

.PHONY: test
test: ## Run all tests (the browser tests are skipped without Chrome)
	go test ./...

.PHONY: test-short
test-short: ## Run the tests that need no browser
	go test -short ./...

.PHONY: test-race
test-race: ## Run all tests with the race detector
	go test -race ./...

.PHONY: cover
cover: ## Run the tests with coverage: coverage.txt and a summary
	go test -race -covermode=atomic -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -n 1

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .

.PHONY: fmt-check
fmt-check: ## Fail if the code is not formatted
	@unformatted="$$(gofmt -s -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt -s would change:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum need tidying
	@cp go.mod go.mod.orig && cp go.sum go.sum.orig; \
	go mod tidy; \
	status=0; \
	cmp -s go.mod go.mod.orig && cmp -s go.sum go.sum.orig || { echo "go.mod/go.sum are not tidy: run 'make tidy'"; status=1; }; \
	mv go.mod.orig go.mod; mv go.sum.orig go.sum; \
	exit $$status

.PHONY: notices
notices: ## Regenerate THIRD_PARTY_NOTICES.md (after the dependencies changed)
	sh scripts/notices.sh

.PHONY: notices-check
notices-check: ## Fail if THIRD_PARTY_NOTICES.md is out of date
	@tmp="$$(mktemp)"; \
	sh scripts/notices.sh "$$tmp" 2>/dev/null || { rm -f "$$tmp"; exit 1; }; \
	if cmp -s "$$tmp" THIRD_PARTY_NOTICES.md; then rm -f "$$tmp"; else \
		rm -f "$$tmp"; echo "THIRD_PARTY_NOTICES.md is out of date: run 'make notices'"; exit 1; \
	fi

.PHONY: completions
completions: ## Write the shell completion scripts into completions/
	sh scripts/completions.sh

.PHONY: check
check: fmt-check tidy-check vet lint notices-check test-race ## Everything CI checks: format, tidy, vet, lint, notices, tests with -race

.PHONY: cross
cross: ## Cross-compile for every release platform into dist/
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		out="dist/$(BINARY)_$${os}_$${arch}$$ext"; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" $(CMD) || exit 1; \
	done

.PHONY: snapshot
snapshot: ## Build a release locally without publishing (needs goreleaser)
	$(GORELEASER) release --snapshot --clean

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml (needs goreleaser)
	$(GORELEASER) check

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist completions coverage.txt coverage.html
