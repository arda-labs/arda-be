.PHONY: build run test docker lint

build:
	go build -o bin/statistical-service ./cmd/statistical-service

run:
	go run ./cmd/statistical-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t statistical-service:latest .
