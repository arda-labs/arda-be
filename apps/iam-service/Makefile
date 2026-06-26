.PHONY: build run test docker lint

build:
	go build -o bin/iam-service ./cmd/iam-service

run:
	go run ./cmd/iam-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t iam-service:latest .
