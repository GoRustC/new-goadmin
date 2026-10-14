package middleware

import (
	"github.com/casbin/casbin/v2"
	"github.com/gin-gonic/gin"

	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// ErrCasbinDenied is sent when no subject of the request is allowed.
var ErrCasbinDenied = errors.Forbidden("com.casbin.denied", "Permission denied")

// CasbinConfig configures CasbinWithConfig. GetSubjects returns the casbin
// subjects (role IDs) of the request's user.
type CasbinConfig struct {
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
	Skipper             func(c *gin.Context) bool
	GetEnforcer         func(c *gin.Context) *casbin.Enforcer
	GetSubjects         func(c *gin.Context) []string
}

// CasbinWithConfig returns a middleware that allows a request if any of its
// subjects may access the path with the method, and answers 403 otherwise.
func CasbinWithConfig(config CasbinConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) ||
			(config.Skipper != nil && config.Skipper(c)) {
			c.Next()
			return
		}

		enforcer := config.GetEnforcer(c)
		if enforcer == nil {
			util.ResError(c, ErrCasbinDenied)
			return
		}

		for _, sub := range config.GetSubjects(c) {
			if b, err := enforcer.Enforce(sub, c.Request.URL.Path, c.Request.Method); err != nil {
				util.ResError(c, err)
				return
			} else if b {
				c.Next()
				return
			}
		}
		util.ResError(c, ErrCasbinDenied)
	}
}
