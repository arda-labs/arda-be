module github.com/arda-labs/arda/apps/crm-service

go 1.26

require (
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0
	github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0
	github.com/arda-labs/arda/libs/go/arda-http v0.0.0
	github.com/arda-labs/arda/libs/go/arda-proto v0.0.0
	github.com/lib/pq v1.10.9
	github.com/pressly/goose/v3 v3.21.1
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../../libs/go/arda-grpc

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-proto => ../../libs/go/arda-proto

exclude google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013

exclude google.golang.org/genproto v0.0.0-20200513103714-09dca8ec2884

require (
	github.com/arda-labs/arda/libs/go/arda-postgres v0.0.0
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres
