# ============================================================
# ccrouter — unified build
# ============================================================
BINARY   := ccrouter
WEB_DIR  := web
DIST_DIR := internal/server/web/dist
GO_FILES := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: all build web clean test lint run dev

# Default: build everything
all: build

# ── Full production build ─────────────────────────────────
build: web
	go build -ldflags="-s -w" -o $(BINARY) ./cmd/ccrouter

# ── Frontend only ─────────────────────────────────────────
web: $(WEB_DIR)/node_modules
	cd $(WEB_DIR) && npm run build

$(WEB_DIR)/node_modules: $(WEB_DIR)/package-lock.json
	cd $(WEB_DIR) && npm ci
	@touch $(WEB_DIR)/node_modules  # update mtime

# ── Go only (assumes dist already exists) ─────────────────
go-build:
	go build -ldflags="-s -w" -o $(BINARY) ./cmd/ccrouter

# ── Development: Go backend + Vite HMR proxy ──────────────
dev:
	@echo "Start Go backend:  make run"
	@echo "Start Vite HMR:    cd web && npm run dev"

run:
	go run ./cmd/ccrouter

# ── Tests ─────────────────────────────────────────────────
test:
	go test ./...

test-web: $(WEB_DIR)/node_modules
	cd $(WEB_DIR) && npm run lint

# ── Clean ─────────────────────────────────────────────────
clean:
	rm -f $(BINARY)
	rm -rf $(DIST_DIR)
