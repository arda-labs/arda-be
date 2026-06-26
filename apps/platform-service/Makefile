.PHONY: build run test docker lint

build:
	go build -o bin/platform-service ./cmd/platform-service

run:
	go run ./cmd/platform-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t platform-service:latest .

