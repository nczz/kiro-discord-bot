# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o bot .

# Runtime stage
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl bash unzip \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL https://cli.kiro.dev/install | bash
ENV PATH="/root/.local/bin:${PATH}"
WORKDIR /app
COPY --from=builder /app/bot .
VOLUME ["/data", "/projects"]
CMD ["./bot"]
