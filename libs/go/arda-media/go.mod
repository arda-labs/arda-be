module github.com/arda-labs/arda/libs/go/arda-media

go 1.27.1

require github.com/arda-labs/arda/libs/go/arda-grpc v0.0.0

require (
	github.com/arda-labs/arda/libs/go/arda-proto v0.0.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-grpc => ../arda-grpc

replace github.com/arda-labs/arda/libs/go/arda-proto => ../arda-proto

replace github.com/arda-labs/arda/libs/go/arda-time => ../arda-time
