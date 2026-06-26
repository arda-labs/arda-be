version: '3'

tasks:
  build:
    cmds:
      - go build -o bin/{{.ServiceName}} ./cmd/{{.ServiceName}}

  run:
    cmds:
      - go run ./cmd/{{.ServiceName}}

  test:
    cmds:
      - go test ./...

  lint:
    cmds:
      - go vet ./...

  docker:
    cmds:
      - docker build -t {{.ServiceName}}:latest .
