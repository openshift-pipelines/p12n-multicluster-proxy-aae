# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Build the application
RUN go build -o proxy-aae ./cmd/proxy-server/main.go

# Final stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /app/proxy-aae .

# Use nonroot user
USER 65532:65532

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/proxy-aae"]
