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

.PHONY: build run clean windows linux darwin test

build:
	@echo ">> Building $(APP_NAME) ($(TARGET_OS))..."
	@go build -o $(BUILD_DIR)/$(BIN) $(CMD_PATH)
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
	@echo ">> Building for Windows..."
	@GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe $(CMD_PATH)

linux:
	@echo ">> Building for Linux..."
	@GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 $(CMD_PATH)

darwin:
	@echo ">> Building for macOS..."
	@GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(APP_NAME)-darwin-amd64 $(CMD_PATH)

all: clean build windows linux darwin
	@echo ">> All platforms built in $(BUILD_DIR)/"
