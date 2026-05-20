# Stage 1: Build
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Кэширование зависимостей
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники
COPY . .
RUN go mod tidy

# Собираем все бинарники
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/server   ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/migrate  ./cmd/migrate
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/web      ./cmd/web

# Stage 2: Runtime

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Бинарники
COPY --from=builder /bin/server  /app/server
COPY --from=builder /bin/migrate /app/migrate
COPY --from=builder /bin/web     /app/web

# Миграции и seed
COPY migrations/ /app/migrations/

# Скрипт запуска
COPY docker-entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 50051

ENTRYPOINT ["/app/entrypoint.sh"]
