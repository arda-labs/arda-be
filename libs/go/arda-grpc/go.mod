module github.com/arda-labs/arda/libs/go/arda-grpc

go 1.26

require (
	github.com/arda-labs/arda/libs/go/arda-proto v0.0.0
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
)

replace github.com/arda-labs/arda/libs/go/arda-proto => ../arda-proto

require (
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
)

exclude google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013

exclude google.golang.org/genproto v0.0.0-20200513103714-09dca8ec2884
