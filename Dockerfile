# Build stage
FROM golang:1.22-alpine as builder

WORKDIR /app

# Copy go mod and sum files
COPY hubtotea/go.mod hubtotea/go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the Docker container
COPY hubtotea/ .

# Build the Go app
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -o hubtotea .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the pre-built binary from the previous stage
COPY --from=builder /app/hubtotea .

# COPY the entrypoint script that will load certificates and run the binary
COPY docker/entrypoint.sh .

ENTRYPOINT ["/app/entrypoint.sh"]

# Run the executable
CMD ["./hubtotea"]