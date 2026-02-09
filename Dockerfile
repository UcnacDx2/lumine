# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY *.go ./

# Build the application for Linux x64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o lumine .

# Runtime stage
FROM alpine:latest

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/lumine .

# Copy default config
COPY config.json .

# Expose SOCKS5 and HTTP proxy ports (default ports from config.json)
EXPOSE 1080 1225

# Run the application
CMD ["./lumine"]
