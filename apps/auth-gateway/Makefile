.PHONY: build run test docker lint

build:
	go build -o bin/auth-gateway ./cmd/auth-gateway

run:
	go run ./cmd/auth-gateway

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t auth-gateway:latest .
