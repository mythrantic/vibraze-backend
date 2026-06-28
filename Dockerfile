# Production backend image builds from source.
# Uses pure-Go SQLite (modernc.org/sqlite), so CGO is not required.
FROM golang:1.21-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /app/raga-backend .

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/raga-backend .
RUN mkdir -p /music /data

EXPOSE 3000

ENTRYPOINT ["/app/raga-backend"]
