module github.com/arda-labs/arda/apps/auth-gateway

go 1.26

require (
	github.com/arda-labs/arda/libs/go/arda-auth v0.0.0
	github.com/google/uuid v1.6.0
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.21.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)

replace github.com/arda-labs/arda/libs/go/arda-auth => ../../libs/go/arda-auth
