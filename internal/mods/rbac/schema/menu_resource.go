package schema

import (
	"regexp"
	"time"

	"github.com/GoRustC/new-goadmin/pkg/errors"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// Menu resource management for RBAC
type MenuResource struct {
	ID        string    `json:"id" gorm:"size:20;primarykey"` // Unique ID
	MenuID    string    `json:"menu_id" gorm:"size:20;index"` // From Menu.ID
	Method    string    `json:"method" gorm:"size:20;"`       // HTTP method
	Path      string    `json:"path" gorm:"size:255;"`        // API request path (e.g. /api/v1/users/:id)
	CreatedAt time.Time `json:"created_at" gorm:"index;"`     // Create time
	UpdatedAt time.Time `json:"updated_at" gorm:"index;"`     // Update time
}

// Defining the query parameters for the `MenuResource` struct.
type MenuResourceQueryParam struct {
	util.PaginationParam
	MenuID  string   `form:"-"` // From Menu.ID
	MenuIDs []string `form:"-"` // From Menu.ID
}

// Defining the query options for the `MenuResource` struct.
type MenuResourceQueryOptions struct {
	util.QueryOptions
}

// Defining the query result for the `MenuResource` struct.
type MenuResourceQueryResult struct {
	Data       MenuResources
	PageResult *util.PaginationResult
}

// Defining the slice of `MenuResource` struct.
type MenuResources []*MenuResource

// Defining the data structure for creating a `MenuResource` struct.
type MenuResourceForm struct {
}

// A validation function for the `MenuResourceForm` struct.
func (a *MenuResourceForm) Validate() error {
	return nil
}

func (a *MenuResourceForm) FillTo(menuResource *MenuResource) error {
	return nil
}

// Methods a menu resource may use.
var allowedResourceMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true,
}

// resourcePathPattern allows route paths with :param, {param} and *
// wildcards. Commas, quotes, whitespace and line breaks are rejected: the
// casbin policy is assembled as "p, role, path, method" lines, so they could
// inject extra rules or break parsing.
var resourcePathPattern = regexp.MustCompile(`^/[A-Za-z0-9/_\-.:{}*~]*$`)

// Validate checks the method and path before they become a policy rule.
func (a *MenuResource) Validate() error {
	if !allowedResourceMethods[a.Method] {
		return errors.BadRequest("", "Invalid resource method %q", a.Method)
	}
	if len(a.Path) > 255 || !resourcePathPattern.MatchString(a.Path) {
		return errors.BadRequest("", "Invalid resource path %q", a.Path)
	}
	return nil
}
