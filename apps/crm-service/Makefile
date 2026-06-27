.PHONY: build run test docker lint

build:
	go build -o bin/crm-service ./cmd/crm-service

run:
	go run ./cmd/crm-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t crm-service:latest .
