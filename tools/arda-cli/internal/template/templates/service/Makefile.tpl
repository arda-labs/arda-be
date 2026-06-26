.PHONY: build run test docker lint

build:
	go build -o bin/{{.ServiceName}} ./cmd/{{.ServiceName}}

run:
	go run ./cmd/{{.ServiceName}}

test:
	go test ./...

lint:
	go vet ./...

docker:
	docker build -t {{.ServiceName}}:latest .
