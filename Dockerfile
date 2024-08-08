# Build stage
FROM golang:1.22-alpine as builder

WORKDIR /app
RUN apk --no-cache add git
COPY hubtotea/go.mod hubtotea/go.sum ./
RUN go mod download
RUN --mount=target=. \
  mkdir -p /build && \
  cd hubtotea && \
  CGO_ENABLED=0 \
  go build -ldflags "-X main.Version=$(git describe --tags --always)" -a -installsuffix cgo \
  -o /build/hubtotea \
  .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the pre-built binary from the previous stage
COPY --from=builder /build/hubtotea .

# COPY the entrypoint script that will load certificates and run the binary
COPY docker/entrypoint.sh .

ENTRYPOINT ["/app/entrypoint.sh"]

# Run the executable
CMD ["./hubtotea"]