package middleware

import (
	"bytes"
	"compress/gzip"
	stderrors "errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// CopyBodyConfig configures CopyBodyWithConfig. Bodies larger than
// MaxContentLen bytes, after gzip decompression, are refused with 413.
type CopyBodyConfig struct {
	AllowedPathPrefixes []string
	SkippedPathPrefixes []string
	MaxContentLen       int64
}

// DefaultCopyBodyConfig allows bodies of up to 32MB.
var DefaultCopyBodyConfig = CopyBodyConfig{
	MaxContentLen: 32 << 20, // 32MB
}

// CopyBody reads the request body into memory with DefaultCopyBodyConfig, so
// it can be read again by handlers and stored by the operation log. A gzip
// body is decompressed.
func CopyBody() gin.HandlerFunc {
	return CopyBodyWithConfig(DefaultCopyBodyConfig)
}

// CopyBodyWithConfig is CopyBody with the given configuration.
func CopyBodyWithConfig(config CopyBodyConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AllowedPathPrefixes(c, config.AllowedPathPrefixes...) ||
			SkippedPathPrefixes(c, config.SkippedPathPrefixes...) ||
			c.Request.Body == nil {
			c.Next()
			return
		}

		var (
			requestBody []byte
			err         error
		)
		safe := http.MaxBytesReader(c.Writer, c.Request.Body, config.MaxContentLen)
		if strings.EqualFold(c.GetHeader("Content-Encoding"), "gzip") {
			zr, zerr := gzip.NewReader(safe)
			if zerr != nil {
				// Reading on as plain data would hand the handler a body missing
				// the bytes the gzip header check already consumed.
				util.ResError(c, errors.BadRequest("", "Invalid gzip request body"))
				return
			}
			// MaxBytesReader only bounds the compressed stream; bound the
			// decompressed output too, otherwise a small gzip bomb can
			// allocate unbounded memory before authentication runs.
			requestBody, err = readAllLimited(zr, config.MaxContentLen)
			_ = zr.Close()
			// The body handed on is decompressed.
			c.Request.Header.Del("Content-Encoding")
		} else {
			requestBody, err = io.ReadAll(safe)
		}

		if err != nil {
			var tooLarge *http.MaxBytesError
			if stderrors.As(err, &tooLarge) || stderrors.Is(err, errBodyTooLarge) {
				util.ResError(c, errors.RequestEntityTooLarge("", "Request body too large, limit %d byte", config.MaxContentLen))
			} else {
				// Corrupt gzip data, a client that disconnected mid-upload, ...
				util.ResError(c, errors.BadRequest("", "Failed to read request body"))
			}
			return
		}

		_ = c.Request.Body.Close()
		bf := bytes.NewBuffer(requestBody)
		c.Request.Body = io.NopCloser(bf)
		c.Set(util.ReqBodyKey, requestBody)
		c.Next()
	}
}

var errBodyTooLarge = stderrors.New("request body exceeds the size limit")

// readAllLimited reads at most limit bytes from r and fails with
// errBodyTooLarge if r holds more.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errBodyTooLarge
	}
	return b, nil
}
