.PHONY: build run test docker lint

build:
	go build -o bin/mdm-service ./cmd/mdm-service

run:
	go run ./cmd/mdm-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t mdm-service:latest .
