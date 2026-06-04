.PHONY: all build run clean test deps install help local

ifeq ($(OS),Windows_NT)
    NULL := nul
    EXE := .exe
    BLANK := echo.
    CGO := 0
else
    NULL := /dev/null
    EXE :=
    BLANK := echo
    CGO := 1
endif

# Binary name
BINARY_NAME=noraegaori$(EXE)
BINARY_PATH=./$(BINARY_NAME)

# Build flags
BUILD_FLAGS=-ldflags="-s -w"

# Docker image tag: master → release, anything else → dev
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>$(NULL))
ifeq ($(GIT_BRANCH),master)
    DOCKER_TAG := release
else
    DOCKER_TAG := dev
endif
DOCKER_IMAGE := $(BINARY_NAME):$(DOCKER_TAG)

all: build

## build: Build the bot (uses libopus at runtime if present, WASM otherwise)
build: export CGO_ENABLED := $(CGO)
build: deps
	@echo Building $(BINARY_NAME)...
	@go build $(BUILD_FLAGS) -o $(BINARY_NAME) ./cmd/bot
	@echo Build complete: $(BINARY_PATH)

## run: Build and run the application
run: build
	@echo Starting $(BINARY_NAME)...
	@$(BINARY_PATH)

## dev: Run in development mode with debug logging
dev:
	@echo Running in development mode...
	@DEBUG_MODE=true go run ./cmd/bot

## clean: Remove build artifacts
clean:
	@echo Cleaning build artifacts...
	@rm -f $(BINARY_NAME)
	@rm -rf data/
	@echo Clean complete

## deps: Download dependencies
deps:
	@echo Downloading dependencies...
	@go mod download
	@go mod tidy
	@echo Dependencies ready

## test: Run tests
test:
	@echo Running tests...
	@go test -v ./...

## install: Install required system dependencies
install:
	@echo Installing system dependencies...
	@echo Checking for FFmpeg...
	@which ffmpeg > /dev/null || (echo "\e[91mFFmpeg not found. Install with: sudo apt install ffmpeg\e[0m" && exit 1)
	@echo FFmpeg found
	@echo Checking for yt-dlp...
	@which yt-dlp > /dev/null || (echo "\033[33myt-dlp not found. Install with: pip install yt-dlp\e[0m" && exit 0)
	@echo yt-dlp found
	@echo All system dependencies ready

## setup: First-time setup (install deps + build)
setup: install deps build
	@$(BLANK)
	@echo Setup complete!
	@$(BLANK)
	@echo Next steps:
	@echo 1. Configure .env file with your Discord bot token
	@echo 2. Edit config/config.json for bot settings
	@echo 3. Add admin user IDs to config/admins.json
	@echo 4. Run: make run

## local: Build using local discordgo-fork
local:
	@echo Building $(BINARY_NAME) with local discordgo-fork...
	@cp go.mod go.mod.bak; cp go.sum go.sum.bak; \
		go mod edit -replace github.com/bwmarrin/discordgo=/home/yeongaori/discordgo-fork; \
		CGO_ENABLED=1 go build $(BUILD_FLAGS) -o $(BINARY_NAME) ./cmd/bot; \
		RC=$$?; mv go.mod.bak go.mod; mv go.sum.bak go.sum; exit $$RC
	@echo "Build complete (local fork): $(BINARY_PATH)"

## docker-build: Build Docker image (tag derives from current branch)
docker-build:
	@echo Building Docker image: $(DOCKER_IMAGE)
	@docker build -t $(DOCKER_IMAGE) .
	@echo Docker image built: $(DOCKER_IMAGE)

## docker-run: Run in Docker container
docker-run:
	@echo Running in Docker: $(DOCKER_IMAGE)
	@docker run --rm -it \
		-v $(PWD)/config:/app/config \
		-v $(PWD)/data:/app/data \
		-v $(PWD)/.env:/app/.env \
		$(DOCKER_IMAGE)

## lint: Run linter
lint:
	@echo Running linter...
	@which golangci-lint > /dev/null || (echo "\e[91mgolangci-lint not installed\e[0m" && exit 1)
	@golangci-lint run

## format: Format code
format:
	@echo Formatting code...
	@go fmt ./...
	@echo Code formatted

## help: Show this help message
help:
	@echo NoraeGaori - Makefile Commands
	@$(BLANK)
	@echo "Usage: make [command]"
	@$(BLANK)
	@echo Commands:
	@sed -n 's/^##//p' Makefile | column -t -s ':' | sed -e 's/^/ /'
	@$(BLANK)
	@echo Environment Variables:
	@echo "  DISCORD_BOT_TOKEN  Discord bot token (required)"
	@echo "  DEBUG_MODE         Enable debug logging (optional)"
	@$(BLANK)
	@echo First time setup:
	@echo "  make setup"
	@$(BLANK)
	@echo For more information, see README.md or BUILD_GUIDE.md
