module github.com/arda-labs/arda/apps/loan-service

go 1.27.1

require (
	github.com/arda-labs/arda/libs/go/arda-businessdate v0.0.0
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0
	github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0
	github.com/arda-labs/arda/libs/go/arda-http v0.0.0
	github.com/arda-labs/arda/libs/go/arda-money v0.0.0
	github.com/arda-labs/arda/libs/go/arda-params v0.0.0
	github.com/arda-labs/arda/libs/go/arda-postgres v0.0.0
	github.com/arda-labs/arda/libs/go/arda-proto v0.0.0
	github.com/arda-labs/arda/libs/go/arda-time v0.0.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.27.1
	github.com/shopspring/decimal v1.4.0
	google.golang.org/grpc v1.84.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../../libs/go/arda-grpc

replace github.com/arda-labs/arda/libs/go/arda-proto => ../../libs/go/arda-proto

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-money => ../../libs/go/arda-money

replace github.com/arda-labs/arda/libs/go/arda-params => ../../libs/go/arda-params

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres

replace github.com/arda-labs/arda/libs/go/arda-time => ../../libs/go/arda-time

replace github.com/arda-labs/arda/libs/go/arda-businessdate => ../../libs/go/arda-businessdate
