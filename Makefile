APP_NAME := media-mcp
CMD_PATH := ./cmd/$(APP_NAME)
BUILD_DIR := build

# Detect the target OS at build time so Windows builds get a .exe suffix
# (launchers/MCP clients resolve .exe; a bare name would risk a stale binary).
TARGET_OS := $(shell go env GOOS)
ifeq ($(TARGET_OS),windows)
BIN := $(APP_NAME).exe
else
BIN := $(APP_NAME)
endif

# Version stamped into serverInfo: tag name on a tagged HEAD (v0.3.0),
# "<tag>-<n>-g<hash>" a few commits past a tag, bare short hash with no tag,
# "-dirty" appended when the tree has uncommitted changes.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION) -X media-mcp/internal/mcp.version=$(VERSION)

.PHONY: build run clean windows linux darwin test

build:
	@echo ">> Building $(APP_NAME) ($(TARGET_OS)) version=$(VERSION)..."
	@go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BIN) $(CMD_PATH)
	@echo ">> Done: $(BUILD_DIR)/$(BIN)"

run:
	@go run $(CMD_PATH)

test:
	@go test -v ./...

clean:
	@rm -rf $(BUILD_DIR)
	@echo "Cleaned."

# Cross-compile targets
windows:
	@echo ">> Building for Windows version=$(VERSION)..."
	@GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe $(CMD_PATH)

linux:
	@echo ">> Building for Linux version=$(VERSION)..."
	@GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 $(CMD_PATH)

darwin:
	@echo ">> Building for macOS version=$(VERSION)..."
	@GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-darwin-amd64 $(CMD_PATH)

all: clean build windows linux darwin
	@echo ">> All platforms built in $(BUILD_DIR)/"
