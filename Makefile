# day2day — worklog CLI build targets
#
# Native build (repo-root ./worklog, gitignored):
#   make build          # or: ./scripts/build.sh
#
# Cross-compile artifacts (gitignored bin/):
#   make linux-amd64 linux-arm64 darwin-amd64 darwin-arm64
#
# Auto-detect host → native ./worklog:
#   make build-local    # uses scripts/detect-platform.sh

BINARY   ?= worklog
MAIN     := ./cmd/worklog
GO       ?= go
BIN_DIR  := bin
LDFLAGS  ?=

# Platforms supported by scripts/detect-platform.sh
PLATFORM_TARGETS := linux-amd64 linux-arm64 darwin-amd64 darwin-arm64

.PHONY: help build build-local clean test vet
.PHONY: $(PLATFORM_TARGETS)

.DEFAULT_GOAL := help

help: ## List targets
	@grep -E '^[a-zA-Z0-9_.-]+:.*##' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build build-local: ## Build ./worklog for this machine (auto-detect OS/arch)
	@$(MAKE) $(shell ./scripts/detect-platform.sh) OUTPUT=./$(BINARY)

$(PLATFORM_TARGETS): ## Cross-compile or native build for $@
	@target='$@'; \
	os=$${target%-*}; \
	arch=$${target#*-}; \
	out='$(or $(OUTPUT),$(BIN_DIR)/$(BINARY)-$@)'; \
	mkdir -p "$$(dirname "$$out")"; \
	echo "GOOS=$$os GOARCH=$$arch -> $$out"; \
	GOOS=$$os GOARCH=$$arch $(GO) build -ldflags "$(LDFLAGS)" -o "$$out" $(MAIN)

test: ## Run tests
	$(GO) test ./...

vet: ## Run go vet
	$(GO) vet ./...

clean: ## Remove ./worklog and bin/ artifacts
	rm -f ./$(BINARY)
	rm -rf $(BIN_DIR)
