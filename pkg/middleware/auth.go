// Package middleware provides the gin middlewares of the HTTP server.
package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// AuthConfig configures AuthWithConfig. ParseUserID authenticates the request
// and returns the user ID; requests for which Skipper returns true, or whose
// path is skipped or not allowed, pass through unauthenticated.
type AuthConfig struct {
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
	RootID              string
	Skipper             func(c *gin.Context) bool
	ParseUserID         func(c *gin.Context) (string, error)
}

// AuthWithConfig returns a middleware that authenticates requests and stores
// the user ID in the request context. A user whose ID is RootID is marked as
// root. Authentication errors are sent as the response.
func AuthWithConfig(config AuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) ||
			(config.Skipper != nil && config.Skipper(c)) {
			c.Next()
			return
		}

		userID, err := config.ParseUserID(c)
		if err != nil {
			util.ResError(c, err)
			return
		}

		ctx := util.NewUserID(c.Request.Context(), userID)
		ctx = logging.NewUserID(ctx, userID)
		if userID == config.RootID {
			ctx = util.NewIsRootUser(ctx)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
