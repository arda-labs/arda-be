.PHONY: build run test docker lint

build:
	go build -o bin/loan-service ./cmd/loan-service

run:
	go run ./cmd/loan-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t loan-service:latest .
