# Frontend build stage
FROM node:20-alpine AS frontend-builder

WORKDIR /app/web

# Copy package files
COPY web/package*.json ./

# Install dependencies
RUN npm ci

# Copy frontend source
COPY web/ ./

# Build frontend
RUN npm run build

# Backend build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Copy built frontend from frontend-builder
COPY --from=frontend-builder /app/web/dist ./web/dist

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o ccproxy ./cmd/server

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -g '' ccproxy

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/ccproxy .

# Copy default config
COPY --from=builder /app/config.yaml ./config.yaml.example

# Create data directory
RUN mkdir -p /app/data && chown -R ccproxy:ccproxy /app

# Switch to non-root user
USER ccproxy

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Environment variables with defaults
ENV CCPROXY_SERVER_PORT=8080 \
    CCPROXY_SERVER_HOST=0.0.0.0 \
    CCPROXY_STORAGE_DB_PATH=/app/data/ccproxy.db

# Run the binary
ENTRYPOINT ["./ccproxy"]
