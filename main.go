package echo_limiter

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/shareed2k/go_limiter"
)

const (
	SlidingWindowAlgorithm = go_limiter.SlidingWindowAlgorithm
	GCRAAlgorithm          = go_limiter.GCRAAlgorithm
	DefaultKeyPrefix       = "echo_limiter"
	defaultMessage         = "Too many requests, please try again later."
	defaultStatusCode      = http.StatusTooManyRequests
)

var (
	DefaultConfig = Config{
		Skipper:    middleware.DefaultSkipper,
		Max:        10,
		Burst:      10,
		StatusCode: defaultStatusCode,
		Message:    defaultMessage,
		Prefix:     DefaultKeyPrefix,
		Algorithm:  SlidingWindowAlgorithm,
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
		// Default: sliding window
		Algorithm uint

		// Prefix
		// Default:
		Prefix string

		// SkipOnError
		// Default: false
		SkipOnError bool

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

		// ErrHandler is called when a error happen inside go_limiiter lib
		// Default: func(c echo.Context) {
		//   return ctx.String(defaultStatusCode, defaultMessage)
		// }
		ErrHandler func(error, echo.Context) error
	}
)

func New(rediser *redis.Client) echo.MiddlewareFunc {
	config := DefaultConfig
	config.Rediser = rediser
	return NewWithConfig(config)
}

func NewWithConfig(config Config) echo.MiddlewareFunc {
	if config.Rediser == nil {
		panic(errors.New("redis client is missing"))
	}

	if config.Skipper == nil {
		config.Skipper = DefaultConfig.Skipper
	}

	if config.Max == 0 {
		config.Max = DefaultConfig.Max
	}

	if config.Burst == 0 {
		config.Burst = DefaultConfig.Burst
	}

	if config.StatusCode == 0 {
		config.StatusCode = DefaultConfig.StatusCode
	}

	if config.Message == "" {
		config.Message = DefaultConfig.Message
	}

	if config.Algorithm == 0 {
		config.Algorithm = DefaultConfig.Algorithm
	}

	if config.Prefix == "" {
		config.Prefix = DefaultConfig.Prefix
	}

	if config.Period == 0 {
		config.Period = DefaultConfig.Period
	}

	if config.Key == nil {
		config.Key = DefaultConfig.Key
	}

	if config.Handler == nil {
		config.Handler = func(ctx echo.Context) error {
			return ctx.String(config.StatusCode, config.Message)
		}
	}

	if config.ErrHandler == nil {
		config.ErrHandler = func(err error, ctx echo.Context) error {
			return echo.NewHTTPError(http.StatusInternalServerError, err)
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

				if config.SkipOnError {
					return next(ctx)
				}

				return config.ErrHandler(err, ctx)
			}

			res := ctx.Response()

			// Check if hits exceed the max
			if !result.Allowed {
				// Return response with Retry-After header
				// https://tools.ietf.org/html/rfc6584
				res.Header().Set("Retry-After", strconv.FormatInt(time.Now().Add(result.RetryAfter).Unix(), 10))

				// Call Handler func
				return config.Handler(ctx)
			}

			// We can continue, update RateLimit headers
			res.Header().Set("X-RateLimit-Limit", strconv.Itoa(config.Max))
			res.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			res.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(result.ResetAfter).Unix(), 10))

			return next(ctx)
		}
	}
}
