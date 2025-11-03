.PHONY: build run test clean docker-build docker-run docker-stop help

# Variables
BINARY_NAME=log-shipper
DOCKER_IMAGE=log-shipper:latest
LOG_FILE=./logs/app.log
# LOG_FILE=../logstreamhive/logs/service.log 
SERVER_HOST=localhost
SERVER_PORT=9000

help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

build: ## Build the Go binary
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) .
	@echo "Build complete!"

run: build ## Run the log shipper locally (basic mode)
	@echo "Running log shipper in basic mode..."
	@./$(BINARY_NAME) --log-file $(LOG_FILE) --server-host $(SERVER_HOST) --server-port $(SERVER_PORT) --type basic

run-resilient: build ## Run the log shipper locally (resilient mode)
	@echo "Running log shipper in resilient mode..."
	@./$(BINARY_NAME) --log-file $(LOG_FILE) --server-host $(SERVER_HOST) --server-port $(SERVER_PORT) --type resilient

run-enhanced: build ## Run the log shipper locally (enhanced mode)
	@echo "Running log shipper in enhanced mode..."
	@./$(BINARY_NAME) --log-file $(LOG_FILE) --server-host $(SERVER_HOST) --server-port $(SERVER_PORT) --type enhanced

run-batch: build ## Run the log shipper in batch mode
	@echo "Running log shipper in batch mode..."
	@./$(BINARY_NAME) --log-file $(LOG_FILE) --server-host $(SERVER_HOST) --server-port $(SERVER_PORT) --batch --type enhanced

test: ## Run tests
	@echo "Running tests..."
	@go test -v ./...

clean: ## Clean build artifacts
	@echo "Cleaning up..."
	@rm -f $(BINARY_NAME)
	@rm -f undelivered_logs.json
	@echo "Clean complete!"

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .
	@echo "Docker image built successfully!"

docker-run: docker-build ## Run log shipper in Docker with docker-compose
	@echo "Starting log shipper with docker-compose..."
	@docker-compose up -d
	@echo "Log shipper started! Use 'docker-compose logs -f log-shipper' to view logs"

docker-stop: ## Stop Docker containers
	@echo "Stopping Docker containers..."
	@docker-compose down
	@echo "Containers stopped!"

docker-logs: ## View Docker logs
	@docker-compose logs -f log-shipper

docker-shell: ## Get a shell in the running container
	@docker exec -it log-shipper sh

init: ## Initialize project directories
	@echo "Creating project directories..."
	@mkdir -p logs data
	@echo "Directories created!"

deps: ## Download Go dependencies
	@echo "Downloading dependencies..."
	@go mod download
	@echo "Dependencies downloaded!"

fmt: ## Format Go code
	@echo "Formatting code..."
	@go fmt ./...
	@echo "Code formatted!"

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet complete!"

mod-tidy: ## Tidy go.mod
	@echo "Tidying go.mod..."
	@go mod tidy
	@echo "go.mod tidied!"

install: build ## Install the binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@go install
	@echo "Installation complete!"