# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Port configuration aligns with Kong CLI defaults
ARG PORT=8080

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
ARG BUILD_ID=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.buildID=${BUILD_ID}" \
    -o gradebot \
    ./main.go

# Runtime stage
FROM alpine:3

# Re-declare port for runtime stage and set as environment variable
ARG PORT=8080
ENV PORT=${PORT}

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/gradebot .

# Create non-root user
RUN addgroup -g 1000 gradebot && \
    adduser -D -u 1000 -G gradebot gradebot && \
    chown -R gradebot:gradebot /app

USER gradebot

EXPOSE ${PORT}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider "http://localhost:${PORT}/" || exit 1

# Run the application
CMD ["./gradebot"]
