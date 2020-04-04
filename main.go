package echo_limiter

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/imdario/mergo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/shareed2k/go_limiter"
)

const (
	SimpleAlgorithm   = "simple"
	GCRAAlgorithm     = "gcra"
	DefaultKeyPrefix  = "echo_limiter"
	defaultMessage    = "Too many requests, please try again later."
	defaultStatusCode = http.StatusTooManyRequests
)

var (
	DefaultConfig = Config{
		Skipper:    middleware.DefaultSkipper,
		Max:        10,
		Burst:      10,
		StatusCode: defaultStatusCode,
		Message:    defaultMessage,
		Prefix:     DefaultKeyPrefix,
		Algorithm:  SimpleAlgorithm,
		Period:     time.Minute,
		Key: func(ctx echo.Context) string {
			return ctx.RealIP()
		},
	}
)

type (
	Config struct {
		Skipper middleware.Skipper

		// Rediser
		Rediser *redis.Client

		// Max number of recent connections
		// Default: 10
		Max int

		// Burst
		Burst int

		// StatusCode
		// Default: 429 Too Many Requests
		StatusCode int

		// Message
		// default: "Too many requests, please try again later."
		Message string

		// Algorithm
		// Default: simple
		Algorithm string

		// Prefix
		// Default:
		Prefix string

		// Period
		Period time.Duration

		// Key allows to use a custom handler to create custom keys
		// Default: func(echo.Context) string {
		//   return ctx.RealIP()
		// }
		Key func(echo.Context) string

		// Handler is called when a request hits the limit
		// Default: func(c echo.Context) {
		//   return ctx.String(defaultStatusCode, defaultMessage)
		// }
		Handler func(echo.Context) error
	}
)

func New(rediser *redis.Client) echo.MiddlewareFunc {
	config := DefaultConfig
	config.Rediser = rediser
	return NewWithConfig(config)
}

func NewWithConfig(config Config) echo.MiddlewareFunc {
	if err := mergo.Merge(&config, DefaultConfig); err != nil {
		panic(err)
	}

	if config.Rediser == nil {
		panic(errors.New("redis client is missing"))
	}

	if config.Handler == nil {
		config.Handler = func(ctx echo.Context) error {
			return ctx.String(config.StatusCode, config.Message)
		}
	}

	limiter := go_limiter.NewLimiter(config.Rediser)
	limit := &go_limiter.Limit{
		Period:    config.Period,
		Algorithm: config.Algorithm,
		Rate:      int64(config.Max),
		Burst:     int64(config.Burst),
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if config.Skipper(ctx) {
				return next(ctx)
			}

			result, err := limiter.Allow(config.Key(ctx), limit)
			if err != nil {
				ctx.Logger().Error(err)

				return next(ctx)
			}

			res := ctx.Response()

			// Check if hits exceed the max
			if !result.Allowed {
				// Call Handler func
				err := config.Handler(ctx)

				// Return response with Retry-After header
				// https://tools.ietf.org/html/rfc6584
				res.Header().Set("Retry-After", strconv.FormatInt(time.Now().Add(result.RetryAfter).Unix(), 10))

				return err
			}

			// We can continue, update RateLimit headers
			res.Header().Set("X-RateLimit-Limit", strconv.Itoa(config.Max))
			res.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			res.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(result.ResetAfter).Unix(), 10))

			return next(ctx)
		}
	}
}
