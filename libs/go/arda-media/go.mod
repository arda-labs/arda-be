module github.com/arda-labs/arda/libs/go/arda-media

go 1.26.3

require github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../arda-grpc
