## echo_limiter is middleware for echo framework 

echo_limiter using [redis](https://github.com/go-redis/redis) as store for rate limit with two algorithms for choosing sliding window, gcra [leaky bucket](https://en.wikipedia.org/wiki/Leaky_bucket)

### Install
```
go get github.com/shareed2k/echo_limiter
```
### Example
```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/labstack/echo/v4"
	limiter "github.com/shareed2k/echo_limiter"
)

func main() {
	e := echo.New()

	option, err := redis.ParseURL("redis://127.0.0.1:6379/0")
	if err != nil {
		log.Fatal(err)
	}
	client := redis.NewClient(option)
	_ = client.FlushDB().Err()

	// 3 requests per 10 seconds max
	cfg := limiter.Config{
		Rediser:   client,
		Max:       3,
		Burst:     3,
		Period:    10 * time.Second,
		Algorithm: limiter.GCRAAlgorithm,
	}

	e.Use(limiter.NewWithConfig(cfg))

	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Hello, World!")
	})
	e.Logger.Fatal(e.Start(":3000"))
}
```
### Test
```curl
curl http://localhost:3000
...
< HTTP/1.1 200 OK
< Date: Fri, 03 Apr 2020 13:02:02 GMT
< Content-Type: text/plain; charset=utf-8
< Content-Length: 8
< X-Ratelimit-Limit: 3
< X-Ratelimit-Remaining: 2
< X-Ratelimit-Reset: 1585918925
...

curl http://localhost:3000
curl http://localhost:3000
curl http://localhost:3000

...
< HTTP/1.1 429 Too Many Requests
< Date: Fri, 03 Apr 2020 13:02:29 GMT
< Content-Type: text/plain; charset=utf-8
< Content-Length: 42
< Retry-After: 1585918951
...
```
