package ardaredis

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Connect initializes a Redis client. If the URL has sentinel scheme (redis-sentinel:// or redis+sentinel://),
// it will initialize a FailoverClient (Sentinel). Otherwise it initializes a standard client.
func Connect(ctx context.Context, redisURL string) (*redis.Client, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("redis url is empty")
	}

	// Example Sentinel URL format:
	// redis-sentinel://[:password]@host1:26379,host2:26379/mymaster
	if strings.HasPrefix(redisURL, "redis-sentinel://") || strings.HasPrefix(redisURL, "redis+sentinel://") {
		cleanURL := strings.TrimPrefix(redisURL, "redis-sentinel://")
		cleanURL = strings.TrimPrefix(cleanURL, "redis+sentinel://")

		// Parse authentication and hosts
		var auth, hostPart, masterName string
		
		// Extract master name at the end
		lastSlash := strings.LastIndex(cleanURL, "/")
		if lastSlash == -1 {
			return nil, fmt.Errorf("invalid sentinel url format: missing master name")
		}
		masterName = cleanURL[lastSlash+1:]
		remaining := cleanURL[:lastSlash]

		// Extract auth part if present
		atSign := strings.LastIndex(remaining, "@")
		var password string
		var username string
		if atSign != -1 {
			auth = remaining[:atSign]
			hostPart = remaining[atSign+1:]

			authParts := strings.SplitN(auth, ":", 2)
			if len(authParts) == 2 {
				username = authParts[0]
				password = authParts[1]
			} else {
				password = authParts[0]
			}
		} else {
			hostPart = remaining
		}

		sentinelAddrs := strings.Split(hostPart, ",")

		rdb := redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       masterName,
			SentinelAddrs:    sentinelAddrs,
			Username:         username,
			Password:         password,
			SentinelPassword: password,
		})

		if err := rdb.Ping(ctx).Err(); err != nil {
			return nil, fmt.Errorf("ping sentinel failed: %w", err)
		}
		return rdb, nil
	}

	// Otherwise, use standard parse URL
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse standard redis url: %w", err)
	}

	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping standard redis failed: %w", err)
	}
	return rdb, nil
}
