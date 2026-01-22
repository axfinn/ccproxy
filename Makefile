.PHONY: build run clean test docker-build docker-run docker-stop docker-logs docker-push build-web dev-web

# Variables
APP_NAME := ccproxy
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
DOCKER_IMAGE := $(APP_NAME)
DOCKER_TAG := $(VERSION)

# Go build flags
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

# Build the web frontend
build-web:
	cd web && npm install && npm run build

# Run web frontend in development mode
dev-web:
	cd web && npm run dev

# Build the binary (includes frontend if dist exists)
build: build-web
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(APP_NAME) ./cmd/server

# Build Go only (without rebuilding frontend)
build-go:
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(APP_NAME) ./cmd/server

# Run locally
run: build
	./$(APP_NAME)

# Clean build artifacts
clean:
	rm -f $(APP_NAME)
	rm -f *.db
	rm -rf web/dist web/node_modules

# Run tests
test:
	go test -v ./...

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy

# Docker commands
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) -t $(DOCKER_IMAGE):latest .

docker-run:
	docker-compose up -d

docker-stop:
	docker-compose down

docker-logs:
	docker-compose logs -f

docker-restart:
	docker-compose restart

docker-shell:
	docker-compose exec ccproxy /bin/sh

# Push to registry (customize DOCKER_REGISTRY as needed)
DOCKER_REGISTRY ?= docker.io
docker-push: docker-build
	docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	docker tag $(DOCKER_IMAGE):latest $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest

# Generate token (for development)
generate-token:
	@echo "Generating token..."
	@curl -s -X POST http://localhost:8080/api/token/generate \
		-H "X-Admin-Key: $${CCPROXY_ADMIN_KEY}" \
		-H "Content-Type: application/json" \
		-d '{"name": "dev-token", "expires_in": "720h"}' | jq .

# Health check
health:
	@curl -s http://localhost:8080/health | jq .

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the binary with frontend"
	@echo "  build-go      - Build the binary without rebuilding frontend"
	@echo "  build-web     - Build the web frontend only"
	@echo "  dev-web       - Run web frontend in development mode"
	@echo "  run           - Build and run locally"
	@echo "  clean         - Clean build artifacts"
	@echo "  test          - Run tests"
	@echo "  fmt           - Format code"
	@echo "  lint          - Lint code"
	@echo "  deps          - Download dependencies"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Start with docker-compose"
	@echo "  docker-stop   - Stop docker-compose"
	@echo "  docker-logs   - View docker-compose logs"
	@echo "  docker-restart- Restart docker-compose"
	@echo "  docker-shell  - Open shell in container"
	@echo "  docker-push   - Push image to registry"
	@echo "  generate-token- Generate a dev token"
	@echo "  health        - Check server health"
