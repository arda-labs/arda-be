module github.com/arda-labs/arda/apps/document-service

go 1.27.2

require (
	github.com/arda-labs/arda/libs/go/arda-errors v0.0.0
	github.com/arda-labs/arda/libs/go/arda-http v0.0.0-00010101000000-000000000000
	golang.org/x/text v0.42.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/arda-labs/arda/libs/go/arda-time v0.0.0 // indirect

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-time => ../../libs/go/arda-time
