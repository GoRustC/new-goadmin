package rbac

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"go.uber.org/zap"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/dal"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/logging"
	"github.com/GoRustC/new-goadmin/pkg/util"
)

// Casbinx builds the casbin policy from roles, menus and menu resources.
//
// The service runs as a single instance, so role and menu changes reload the
// policy in-process right after they commit (see biz.PolicyReloader); there
// is no polling and no cross-instance signal.
type Casbinx struct {
	enforcer        atomic.Pointer[casbin.Enforcer] `wire:"-"`
	reloadMu        sync.Mutex                      `wire:"-"`
	Config          *config.Config
	MenuDAL         *dal.Menu
	MenuResourceDAL *dal.MenuResource
	RoleDAL         *dal.Role
	RoleMenuDAL     *dal.RoleMenu
}

// GetEnforcer returns the current enforcer, or nil before the first load.
// The casbin middleware denies every request while it is nil.
func (a *Casbinx) GetEnforcer() *casbin.Enforcer {
	return a.enforcer.Load()
}

// Load builds the initial policy.
func (a *Casbinx) Load(ctx context.Context) error {
	if a.Config.Middleware.Casbin.Disable {
		return nil
	}
	return a.Reload(ctx)
}

// Reload rebuilds the policy from the database and swaps it in. Concurrent
// calls run one at a time; on error the current policy stays in place.
func (a *Casbinx) Reload(ctx context.Context) error {
	if a.Config.Middleware.Casbin.Disable {
		return nil
	}
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()

	start := time.Now()
	policy, roles, rules, err := a.buildPolicy(ctx)
	if err != nil {
		logging.Context(ctx).Error("Failed to build casbin policy", zap.Error(err))
		return err
	}

	modelFile := filepath.Join(a.Config.General.WorkDir, a.Config.Middleware.Casbin.ModelFile)
	m, err := model.NewModelFromFile(modelFile)
	if err != nil {
		logging.Context(ctx).Error("Failed to load casbin model", zap.Error(err))
		return err
	}
	var e *casbin.Enforcer
	if policy != "" {
		e, err = casbin.NewEnforcer(m, stringadapter.NewAdapter(policy))
	} else {
		// The string adapter rejects empty input; a model-only enforcer has no
		// policies and therefore denies every request.
		e, err = casbin.NewEnforcer(m)
	}
	if err != nil {
		logging.Context(ctx).Error("Failed to create casbin enforcer", zap.Error(err))
		return err
	}
	e.EnableLog(a.Config.IsDebug())
	// Always swap in the new enforcer, even with no policies: keeping the old
	// one would continue to allow permissions that were just revoked.
	a.enforcer.Store(e)

	logging.Context(ctx).Info("Casbin load policy",
		zap.Duration("cost", time.Since(start)),
		zap.Int("roles", roles),
		zap.Int("rules", rules),
	)
	return nil
}

// buildPolicy returns the policy as "p, role, path, method" lines, with a
// fixed number of queries regardless of how many roles exist. A role grants
// the resources of each enabled menu assigned to it and of that menu's
// ancestors.
func (a *Casbinx) buildPolicy(ctx context.Context) (policy string, roles, rules int, err error) {
	roleResult, err := a.RoleDAL.Query(ctx, schema.RoleQueryParam{
		Status: schema.RoleStatusEnabled,
	}, schema.RoleQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"id"}},
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("query roles: %w", err)
	}
	if len(roleResult.Data) == 0 {
		return "", 0, 0, nil
	}

	roleMenuResult, err := a.RoleMenuDAL.Query(ctx, schema.RoleMenuQueryParam{}, schema.RoleMenuQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"role_id", "menu_id"}},
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("query role menus: %w", err)
	}
	menuResult, err := a.MenuDAL.Query(ctx, schema.MenuQueryParam{}, schema.MenuQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"id", "parent_path", "status"}},
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("query menus: %w", err)
	}
	resourceResult, err := a.MenuResourceDAL.Query(ctx, schema.MenuResourceQueryParam{}, schema.MenuResourceQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"menu_id", "method", "path"}},
	})
	if err != nil {
		return "", 0, 0, fmt.Errorf("query menu resources: %w", err)
	}

	menus := make(map[string]*schema.Menu, len(menuResult.Data))
	for _, m := range menuResult.Data {
		menus[m.ID] = m
	}
	resources := make(map[string]schema.MenuResources)
	for _, r := range resourceResult.Data {
		resources[r.MenuID] = append(resources[r.MenuID], r)
	}
	menusByRole := make(map[string][]string)
	for _, rm := range roleMenuResult.Data {
		menusByRole[rm.RoleID] = append(menusByRole[rm.RoleID], rm.MenuID)
	}

	var b strings.Builder
	for _, role := range roleResult.Data {
		// Granted menus that are enabled, plus their ancestors.
		granted := make(map[string]struct{})
		for _, menuID := range menusByRole[role.ID] {
			m, ok := menus[menuID]
			if !ok || m.Status != schema.MenuStatusEnabled {
				continue
			}
			granted[m.ID] = struct{}{}
			for pid := range strings.SplitSeq(m.ParentPath, util.TreePathDelimiter) {
				if pid != "" {
					granted[pid] = struct{}{}
				}
			}
		}

		seen := make(map[string]struct{})
		var lines []string
		for menuID := range granted {
			for _, r := range resources[menuID] {
				// Second line of defense for rows that bypassed the API,
				// e.g. written to the database directly.
				if err := r.Validate(); err != nil {
					logging.Context(ctx).Warn("Skipping invalid menu resource",
						zap.String("menu_id", r.MenuID), zap.String("method", r.Method), zap.String("path", r.Path))
					continue
				}
				// No trailing whitespace: the string adapter does not trim
				// lines, so "GET " would never match a request method.
				line := fmt.Sprintf("p, %s, %s, %s", role.ID, r.Path, r.Method)
				if _, dup := seen[line]; !dup {
					seen[line] = struct{}{}
					lines = append(lines, line)
				}
			}
		}
		sort.Strings(lines) // deterministic output for logs and tests
		for _, line := range lines {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		rules += len(lines)
	}
	return b.String(), len(roleResult.Data), rules, nil
}

func (a *Casbinx) Release(ctx context.Context) error {
	return nil
}
