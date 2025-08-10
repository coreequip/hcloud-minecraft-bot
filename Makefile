.PHONY: help build run test clean docker-build docker-run lint

# Default target
help:
	@echo "Available targets:"
	@echo "  build        - Build the binary"
	@echo "  run          - Run the application"
	@echo "  test         - Run tests"
	@echo "  lint         - Run linters"
	@echo "  clean        - Clean build artifacts"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run Docker container"

# Build the binary
build:
	go build -ldflags="-w -s" -o hcloud-minecraft-bot .

# Run the application
run:
	go run .

# Run tests
test:
	go test -v -race -coverprofile=coverage.out ./...

# Run linters
lint:
	@if command -v golangci-lint >/dev/null; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Clean build artifacts
clean:
	rm -f hcloud-minecraft-bot
	rm -f coverage.out

# Build Docker image
docker-build:
	docker build -t hcloud-minecraft-bot:local .

# Run Docker container
docker-run:
	docker run --rm \
		--env-file .env \
		--name hcloud-minecraft-bot \
		hcloud-minecraft-bot:local