module github.com/arda-labs/arda/apps/ai-service

go 1.27.1

replace github.com/arda-labs/arda/libs/go/arda-crypto => ../../libs/go/arda-crypto

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../../libs/go/arda-grpc

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

require (
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20250317173921-a4b03ec1a45e // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

require (
	github.com/arda-labs/arda/libs/go/arda-crypto v0.0.0
	github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0
	github.com/arda-labs/arda/libs/go/arda-http v0.0.0
	github.com/arda-labs/arda/libs/go/arda-postgres v0.0.0
	github.com/arda-labs/arda/libs/go/arda-time v0.0.0-00010101000000-000000000000
	github.com/dop251/goja v0.0.0-20260822123354-58e940e0d230
	github.com/jackc/pgx/v5 v5.10.0
	github.com/nats-io/nats.go v1.52.0
	github.com/pressly/goose/v3 v3.27.1
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres

replace github.com/arda-labs/arda/libs/go/arda-time => ../../libs/go/arda-time
