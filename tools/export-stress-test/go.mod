module github.com/arda-labs/arda/tools/export-stress-test

go 1.27.1

require github.com/arda-labs/arda/libs/go/arda-export v0.0.0

require (
	github.com/arda-labs/arda/libs/go/arda-time v0.0.0-00010101000000-000000000000 // indirect
	github.com/richardlehane/mscfb v1.0.7 // indirect
	github.com/richardlehane/msoleps v1.0.6 // indirect
	github.com/tiendc/go-deepcopy v1.7.2 // indirect
	github.com/xuri/efp v0.0.1 // indirect
	github.com/xuri/excelize/v2 v2.11.0 // indirect
	github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/text v0.42.0 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-crypto => ../../libs/go/arda-crypto

replace github.com/arda-labs/arda/libs/go/arda-doc => ../../libs/go/arda-doc

replace github.com/arda-labs/arda/libs/go/arda-errors => ../../libs/go/arda-errors

replace github.com/arda-labs/arda/libs/go/arda-export => ../../libs/go/arda-export

replace github.com/arda-labs/arda/libs/go/arda-http => ../../libs/go/arda-http

replace github.com/arda-labs/arda/libs/go/arda-money => ../../libs/go/arda-money

replace github.com/arda-labs/arda/libs/go/arda-postgres => ../../libs/go/arda-postgres

replace github.com/arda-labs/arda/libs/go/arda-time => ../../libs/go/arda-time
