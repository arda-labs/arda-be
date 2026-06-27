.PHONY: build run test docker lint

build:
	go build -o bin/workflow-service ./cmd/workflow-service

run:
	go run ./cmd/workflow-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t workflow-service:latest .
