module github.com/arda-labs/arda/libs/go/arda-grpc

go 1.27.2

require (
	github.com/arda-labs/arda/libs/go/arda-businessdate v0.0.0
	github.com/arda-labs/arda/libs/go/arda-proto v0.0.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.11
)

replace github.com/arda-labs/arda/libs/go/arda-proto => ../arda-proto

replace github.com/arda-labs/arda/libs/go/arda-businessdate => ../arda-businessdate

require (
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

exclude google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013

exclude google.golang.org/genproto v0.0.0-20200513103714-09dca8ec2884
