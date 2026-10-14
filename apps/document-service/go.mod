module github.com/arda-labs/arda/apps/document-service

go 1.27.2

require (
	golang.org/x/text v0.42.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-time => ../../libs/go/arda-time
