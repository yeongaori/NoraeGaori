# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o noraedev ./cmd/bot

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ffmpeg \
    python3 \
    py3-pip \
    ca-certificates \
    sqlite \
    && pip3 install --no-cache-dir yt-dlp

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/noraedev .

# Create directories
RUN mkdir -p /app/config /app/data

# Set environment
ENV DEBUG_MODE=false

# Run as non-root user
RUN adduser -D -u 1000 botuser && \
    chown -R botuser:botuser /app
USER botuser

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD pgrep noraedev || exit 1

# Run the bot
CMD ["./noraedev"]
