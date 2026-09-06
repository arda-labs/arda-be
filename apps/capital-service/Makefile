.PHONY: build run test docker lint

build:
	go build -o bin/capital-service ./cmd/capital-service

run:
	go run ./cmd/capital-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t capital-service:latest .
