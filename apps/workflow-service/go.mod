module github.com/arda-labs/arda/apps/workflow-service

go 1.26

require (
	github.com/camunda/zeebe/clients/go/v8 v8.5.5
	github.com/lib/pq v1.10.9
	github.com/pressly/goose/v3 v3.21.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/arda-labs/arda/libs/go/arda-postgres v0.0.0
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-ozzo/ozzo-validation/v4 v4.3.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/sethvargo/go-retry v0.2.4 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/oauth2 v0.18.0 // indirect
	golang.org/x/sync v0.7.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
	google.golang.org/grpc v1.62.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres
