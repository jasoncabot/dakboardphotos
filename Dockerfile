FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY *.go *.html ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/server .

# Volumes are provided at runtime; create mount points so the intent is clear.
VOLUME ["/photos", "/state"]

EXPOSE 8080
ENV PHOTOS_DIR=/photos \
    STATE_FILE=/state/shuffle.json \
    LISTEN_ADDR=:8080

CMD ["./server"]
