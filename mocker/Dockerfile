# Build stage for multi-architecture support (amd64 and arm64)
FROM golang:1.26-alpine@sha256:f23e8b227fb4493eabe03bede4d5a32d04092da71962f1fb79b5f7d1e6c2a17f AS builder

# Build arguments for multi-platform support (e.g. arm64 and amd64)
ARG TARGETARCH 
ARG TARGETOS=linux

# Install git (required for some Go modules)
RUN apk add --no-cache git

WORKDIR /build

# Copy go mod files
COPY go.mod ./
COPY go.sum* ./
RUN go mod download

# Copy source code
COPY main.go .

# Build the binary for the target platform
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -installsuffix cgo -o mocker main.go

# Runtime stage
FROM alpine:latest@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

# Build argument for platform (needed for COPY --platform)
ARG TARGETARCH

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/mocker .

# Expose default port
EXPOSE 8000

# Run the mocker server
# Default to binding to 0.0.0.0 to accept connections from outside the container
CMD ["./mocker", "-host", "0.0.0.0", "-port", "8000"]

