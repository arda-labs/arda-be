module github.com/arda-labs/arda/apps/notification-service

go 1.26

require (
	github.com/arda-labs/arda/libs/go/arda-auth v0.0.0
	github.com/arda-labs/arda/libs/go/arda-events v0.0.0
	github.com/lib/pq v1.12.3
	github.com/nats-io/nats.go v1.52.0
	github.com/pressly/goose/v3 v3.27.1
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/arda-labs/arda/libs/go/arda-auth => ../../libs/go/arda-auth

replace github.com/arda-labs/arda/libs/go/arda-events => ../../libs/go/arda-events

require (
	github.com/arda-labs/arda/libs/go/arda-postgres v0.0.0
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres
