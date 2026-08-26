module github.com/arda-labs/arda/apps/ai-service

go 1.26

require (
	github.com/arda-labs/arda/libs/go/arda-crypto v0.0.0
	github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0
	github.com/arda-labs/arda/libs/go/arda-http v0.0.0
	github.com/lib/pq v1.10.9
	github.com/pressly/goose/v3 v3.21.1
)

replace github.com/arda-labs/arda/libs/go/arda-crypto => ../../libs/go/arda-crypto

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../../libs/go/arda-grpc

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

require (
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0 // indirect
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/dop251/goja v0.0.0-20260822123354-58e940e0d230 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.2.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
