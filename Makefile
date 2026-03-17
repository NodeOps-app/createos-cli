.PHONY: build clean install run test lint lint-fix fmt deps help

# Binary name
BINARY_NAME=createos
# Build directory
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build the project
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) -v

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe

# Install the binary to $GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GOCMD) install

# Run the application
run:
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) -v
	./$(BUILD_DIR)/$(BINARY_NAME)

# Run tests
test:
	$(GOTEST) -v ./...

# Clean build files
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Run linter and auto-fix issues
lint-fix:
	@echo "Running linter with auto-fix..."
	golangci-lint run --fix ./...

# Show help
help:
	@echo "Available targets:"
	@echo "  build      - Build the binary"
	@echo "  build-all  - Build for multiple platforms"
	@echo "  install    - Install the binary to GOPATH/bin"
	@echo "  run        - Build and run the application"
	@echo "  test       - Run tests"
	@echo "  clean      - Remove build artifacts"
	@echo "  deps       - Download and tidy dependencies"
	@echo "  fmt        - Format code"
	@echo "  lint       - Run linter"
	@echo "  lint-fix   - Run linter and auto-fix issues"
	@echo "  help       - Show this help message"
