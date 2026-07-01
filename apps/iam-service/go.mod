module github.com/arda-labs/arda/apps/iam-service

go 1.26

require (
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0
	github.com/arda-labs/arda/libs/go/arda-media v0.0.0
	github.com/casbin/casbin/v3 v3.10.0
	github.com/lib/pq v1.12.3
	github.com/pressly/goose/v3 v3.27.1
	github.com/redis/go-redis/v9 v9.21.0
	golang.org/x/crypto v0.53.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/arda-labs/arda/libs/go/arda-postgres v0.0.0
	github.com/bmatcuk/doublestar/v4 v4.6.1 // indirect
	github.com/casbin/govaluate v1.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-media => ../../libs/go/arda-media

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres
