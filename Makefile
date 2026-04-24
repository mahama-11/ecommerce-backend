APP_NAME=ecommerce-service
CONFIG?=config.local

.PHONY: tidy test run build

tidy:
	go mod tidy

test:
	go test ./...

run:
	go run ./cmd/server -config $(CONFIG)

build:
	go build -o bin/$(APP_NAME) ./cmd/server
