BINARY = sandbox-mcp
CMD_DIR = .

# Versioning: get the version from the latest git tag
# バージョン管理: 最新のgitタグからバージョンを取得
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags="-X 'main.version=$(VERSION)'"

.PHONY: build test test-version install register unregister clean

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY) $(CMD_DIR)

test:
	go test ./...

# Build integration test: verify ldflags version injection
# ビルド統合テスト: ldflags によるバージョン注入の検証
test-version:
	@TEST_BIN=$$(mktemp) && \
	TEST_VER="0.0.0-test" && \
	CGO_ENABLED=0 go build -ldflags="-X 'main.version=$$TEST_VER'" -o "$$TEST_BIN" $(CMD_DIR) && \
	GOT=$$($$TEST_BIN version) && \
	rm -f "$$TEST_BIN" && \
	if [ "$$GOT" = "sandbox-mcp $$TEST_VER" ]; then \
		echo "PASS: $$GOT"; \
	else \
		echo "FAIL: got='$$GOT', want='sandbox-mcp $$TEST_VER'"; exit 1; \
	fi

install:
	go install $(LDFLAGS) $(CMD_DIR)

# Register to all available CLIs (Claude, Gemini)
# Uses the installed binary name (found via PATH)
register: register-claude register-gemini

register-claude:
	@if ! command -v claude >/dev/null 2>&1; then \
		echo "[Claude] CLI not installed, skipping"; \
	elif ! command -v $(BINARY) >/dev/null 2>&1; then \
		echo "[Claude] $(BINARY) not found in PATH. Run: make install"; \
	else \
		claude mcp add $(BINARY) $(BINARY) && \
		echo "[Claude] $(BINARY) registered"; \
	fi

register-gemini:
	@if ! command -v gemini >/dev/null 2>&1; then \
		echo "[Gemini] CLI not installed, skipping"; \
	elif ! command -v $(BINARY) >/dev/null 2>&1; then \
		echo "[Gemini] $(BINARY) not found in PATH. Run: make install"; \
	else \
		gemini mcp add $(BINARY) $(BINARY) && \
		echo "[Gemini] $(BINARY) registered" || \
		echo "[Gemini] skipped (not configured)"; \
	fi

# Unregister from all available CLIs
unregister: unregister-claude unregister-gemini

unregister-claude:
	@if ! command -v claude >/dev/null 2>&1; then \
		echo "[Claude] CLI not installed, skipping"; \
	else \
		claude mcp remove $(BINARY) 2>/dev/null && \
		echo "[Claude] $(BINARY) removed" || \
		echo "[Claude] $(BINARY) was not registered"; \
	fi

unregister-gemini:
	@if ! command -v gemini >/dev/null 2>&1; then \
		echo "[Gemini] CLI not installed, skipping"; \
	else \
		gemini mcp remove $(BINARY) 2>/dev/null && \
		echo "[Gemini] $(BINARY) removed" || \
		echo "[Gemini] $(BINARY) was not registered"; \
	fi

clean:
	rm -f $(BINARY)
