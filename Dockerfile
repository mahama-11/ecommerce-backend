FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/ecommerce-service ./cmd/server

FROM alpine:3.19
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk add --no-cache ca-certificates tzdata wget
RUN addgroup -g 1000 -S appuser && adduser -u 1000 -S appuser -G appuser
WORKDIR /app
COPY --from=builder /out/ecommerce-service ./ecommerce-service
COPY config.*.yaml ./
RUN chown -R appuser:appuser /app
USER appuser
ENV ECOMMERCE_PORT=8296
ENV ECOMMERCE_CONFIG_FILE=config.prod
EXPOSE 8296
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget --no-verbose --tries=1 --spider http://localhost:${ECOMMERCE_PORT}/healthz || exit 1
CMD ["./ecommerce-service", "-config", "config.prod"]
