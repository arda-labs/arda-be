.PHONY: build run test docker lint

build:
	go build -o bin/deposit-service ./cmd/deposit-service

run:
	go run ./cmd/deposit-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t deposit-service:latest .
