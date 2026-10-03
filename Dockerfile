FROM --platform=$BUILDPLATFORM golang:1.25-trixie AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o noraegaori .

FROM debian:trixie-slim

ARG TARGETARCH

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg libopus0 procps \
    && if [ "$TARGETARCH" = "386" ]; then apt-get install -y --no-install-recommends python3; fi \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /build/noraegaori .
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN mkdir -p /app/config /app/data /app/lib /app/locales

ENV DEBUG_MODE=false

RUN useradd --uid 1000 --user-group --create-home botuser && \
    chown -R botuser:botuser /app

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD pgrep noraegaori || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["./noraegaori"]
