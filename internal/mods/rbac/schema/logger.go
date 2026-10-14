// Package schema defines the models, query parameters and request forms of the
// rbac module.
package schema

import (
	"time"

	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// Logger management
type Logger struct {
	ID        string    `gorm:"size:20;primaryKey;" json:"id"`           // Unique ID
	Level     string    `gorm:"size:20;index;" json:"level"`             // Log level
	TraceID   string    `gorm:"size:64;index;" json:"trace_id"`          // Trace ID
	UserID    string    `gorm:"size:20;index;" json:"user_id"`           // User ID
	Tag       string    `gorm:"size:32;index;" json:"tag"`               // Log tag
	Message   string    `gorm:"size:1024;" json:"message"`               // Log message
	Stack     string    `gorm:"type:text;" json:"stack"`                 // Error stack
	Data      string    `gorm:"type:text;" json:"data"`                  // Log data
	CreatedAt time.Time `gorm:"index;" json:"created_at"`                // Create time
	LoginName string    `json:"login_name" gorm:"<-:false;-:migration;"` // From User.Username
	UserName  string    `json:"user_name" gorm:"<-:false;-:migration;"`  // From User.Name
}

// Defining the query parameters for the `Logger` struct.
type LoggerQueryParam struct {
	util.PaginationParam
	Level        string `form:"level"`     // Log level
	TraceID      string `form:"traceID"`   // Trace ID
	LikeUserName string `form:"userName"`  // User Name
	Tag          string `form:"tag"`       // Log tag
	LikeMessage  string `form:"message"`   // Log message
	StartTime    string `form:"startTime"` // Start time, "2006-01-02 15:04:05", "2006-01-02" or RFC 3339
	EndTime      string `form:"endTime"`   // End time, same formats

	// Parsed from StartTime and EndTime by ParseTimeRange.
	StartAt *time.Time `form:"-"`
	EndAt   *time.Time `form:"-"`
}

// timeLayouts are the accepted StartTime and EndTime formats. Values without
// a zone are read in the server's local time zone.
var timeLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"}

func parseQueryTime(field, v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return &t, nil
		}
	}
	return nil, errors.BadRequest("", "Invalid %s %q, use \"2006-01-02 15:04:05\", \"2006-01-02\" or RFC 3339", field, v)
}

// ParseTimeRange validates StartTime and EndTime and fills StartAt and EndAt.
// Either bound may be omitted.
func (a *LoggerQueryParam) ParseTimeRange() error {
	start, err := parseQueryTime("startTime", a.StartTime)
	if err != nil {
		return err
	}
	end, err := parseQueryTime("endTime", a.EndTime)
	if err != nil {
		return err
	}
	if start != nil && end != nil && start.After(*end) {
		return errors.BadRequest("", "startTime must not be after endTime")
	}
	a.StartAt, a.EndAt = start, end
	return nil
}

// Defining the query options for the `Logger` struct.
type LoggerQueryOptions struct {
	util.QueryOptions
}

// Defining the query result for the `Logger` struct.
type LoggerQueryResult struct {
	Data       Loggers
	PageResult *util.PaginationResult
}

// Defining the slice of `Logger` struct.
type Loggers []*Logger
