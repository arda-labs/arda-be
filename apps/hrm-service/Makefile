.PHONY: build run test docker lint

build:
	go build -o bin/hrm-service ./cmd/hrm-service

run:
	go run ./cmd/hrm-service

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t hrm-service:latest .
