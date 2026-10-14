package util

import (
	stderrors "errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/GoRustC/new-goadmin/pkg/encoding/json"
	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/logging"
)

// Get access token from header or query parameter
func GetToken(c *gin.Context) string {
	var token string
	auth := c.GetHeader("Authorization")
	prefix := "Bearer "

	if auth != "" && strings.HasPrefix(auth, prefix) {
		token = auth[len(prefix):]
	} else {
		token = auth
	}

	if token == "" {
		token = c.Query("accessToken")
	}

	return token
}

// Get body data from context
func GetBodyData(c *gin.Context) []byte {
	if v, ok := c.Get(ReqBodyKey); ok {
		if b, ok := v.([]byte); ok {
			return b
		}
	}
	return nil
}

// Parse body json data to struct
func ParseJSON(c *gin.Context, obj any) error {
	if err := c.ShouldBindJSON(obj); err != nil {
		return errors.BadRequest("", "Failed to parse json: %s", err.Error())
	}
	return nil
}

// Parse query parameter to struct
func ParseQuery(c *gin.Context, obj any) error {
	if err := c.ShouldBindQuery(obj); err != nil {
		return errors.BadRequest("", "Failed to parse query: %s", err.Error())
	}
	return nil
}

// Parse body form data to struct
func ParseForm(c *gin.Context, obj any) error {
	if err := c.ShouldBindWith(obj, binding.Form); err != nil {
		return errors.BadRequest("", "Failed to parse form: %s", err.Error())
	}
	return nil
}

// Response json data with status code
// internalErrorBody is sent when a response cannot be encoded.
var internalErrorBody = []byte(`{"success":false,"error":{"id":"internal_server_error","code":500,"detail":"Internal Server Error","status":"Internal Server Error"}}`)

func ResJSON(c *gin.Context, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		// A value that cannot be encoded is a programming error: log which
		// request hit it and answer a plain 500 instead of panicking.
		logging.Context(c.Request.Context()).Error("Failed to encode response",
			zap.Error(err), zap.String("path", c.Request.URL.Path))
		status, buf = http.StatusInternalServerError, internalErrorBody
	}

	c.Data(status, "application/json; charset=utf-8", buf)
	c.Abort()
}

func ResSuccess(c *gin.Context, v any) {
	ResJSON(c, http.StatusOK, ResponseResult{
		Success: true,
		Data:    v,
	})
}

func ResOK(c *gin.Context) {
	ResJSON(c, http.StatusOK, ResponseResult{
		Success: true,
	})
}

func ResPage(c *gin.Context, v any, pr *PaginationResult) {
	var total int64
	if pr != nil {
		total = pr.Total
	}

	// Render a missing list as [] rather than null.
	if isNilValue(v) {
		v = make([]any, 0)
	}

	ResJSON(c, http.StatusOK, ResponseResult{
		Success: true,
		Data:    v,
		Total:   total,
	})
}

func ResError(c *gin.Context, err error, status ...int) {
	var ierr *errors.Error
	if stderrors.Is(err, gorm.ErrDuplicatedKey) {
		// A unique constraint caught a concurrent duplicate that passed the
		// application's "already exists" check.
		err = errors.Conflict("", "Resource already exists")
	}
	if e, ok := errors.AsError(err); ok {
		// Work on a copy: errors are often package-level values shared by
		// concurrent requests, such as middleware.ErrCasbinDenied.
		cp := *e
		ierr = &cp
	} else {
		ierr = &errors.Error{
			ID:     errors.DefaultInternalServerErrorID,
			Code:   http.StatusInternalServerError,
			Status: http.StatusText(http.StatusInternalServerError),
		}
	}

	code := int(ierr.Code)
	if len(status) > 0 {
		code = status[0]
	}

	if code >= 500 {
		ctx := c.Request.Context()
		ctx = logging.NewTag(ctx, logging.TagKeySystem)
		ctx = logging.NewStack(ctx, fmt.Sprintf("%+v", err))
		logging.Context(ctx).Error("Internal server error", zap.Error(err))
		ierr.Detail = http.StatusText(http.StatusInternalServerError)
	}

	ierr.Code = int32(code) //nolint:gosec // HTTP status codes fit in int32.
	ResJSON(c, code, ResponseResult{Error: ierr})
}

// isNilValue reports whether v is nil or a nil pointer, slice, map or
// interface. reflect.Value.IsNil panics for other kinds, so check first.
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return true
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Map:
		return rv.IsNil()
	}
	return false
}
