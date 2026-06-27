.PHONY: build run test docker lint

build:
	go build -o bin/notification-service ./cmd/notification-service

run:
	go run ./cmd/notification-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t notification-service:latest .
