module github.com/arda-labs/arda/apps/deposit-service

go 1.27.1

require (
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0
	github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0
	github.com/arda-labs/arda/libs/go/arda-http v0.0.0
	github.com/arda-labs/arda/libs/go/arda-money v0.0.0-20260907112815-1343cfb7e777
	github.com/arda-labs/arda/libs/go/arda-proto v0.0.0
	github.com/arda-labs/arda/libs/go/arda-time v0.0.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/pressly/goose/v3 v3.27.1
	github.com/shopspring/decimal v1.4.0
	google.golang.org/grpc v1.81.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../../libs/go/arda-grpc

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-proto => ../../libs/go/arda-proto

exclude google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013

exclude google.golang.org/genproto v0.0.0-20200513103714-09dca8ec2884

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres

replace github.com/arda-labs/arda/libs/go/arda-money => ../../libs/go/arda-money

replace github.com/arda-labs/arda/libs/go/arda-time => ../../libs/go/arda-time
