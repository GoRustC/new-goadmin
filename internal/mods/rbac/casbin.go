package rbac

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LyricTian/gin-admin/v10/internal/config"
	"github.com/LyricTian/gin-admin/v10/internal/mods/rbac/dal"
	"github.com/LyricTian/gin-admin/v10/internal/mods/rbac/schema"
	"github.com/LyricTian/gin-admin/v10/pkg/cachex"
	"github.com/LyricTian/gin-admin/v10/pkg/logging"
	"github.com/LyricTian/gin-admin/v10/pkg/util"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
	"go.uber.org/zap"
)

// Load rbac permissions to casbin
type Casbinx struct {
	Config          *config.Config
	enforcer        *atomic.Value `wire:"-"`
	ticker          *time.Ticker  `wire:"-"`
	Cache           cachex.Cacher
	MenuDAL         *dal.Menu
	MenuResourceDAL *dal.MenuResource
	RoleDAL         *dal.Role
}

func (a *Casbinx) GetEnforcer() *casbin.Enforcer {
	if v := a.enforcer.Load(); v != nil {
		return v.(*casbin.Enforcer)
	}
	return nil
}

type policyQueueItem struct {
	RoleID    string
	Resources schema.MenuResources
}

func (a *Casbinx) Load(ctx context.Context) error {
	if a.Config.Middleware.Casbin.Disable {
		return nil
	}

	if a.Config.Storage.Cache.Type == "memory" && !a.Config.IsDebug() {
		logging.Context(ctx).Warn("Casbin policy reloads are signalled through Storage.Cache; " +
			"with the memory cache, other instances never see role or menu changes. " +
			"Use the redis cache when running more than one instance.")
	}

	// Read the reload signal before loading, so a change made while loading
	// shows up as a new value on the next tick.
	signal, err := a.reloadSignal(ctx)
	if err != nil {
		return err
	}

	a.enforcer = new(atomic.Value)
	if err := a.load(ctx); err != nil {
		return err
	}

	// Create the ticker before starting the goroutine so Release never races with it.
	a.ticker = time.NewTicker(time.Duration(a.Config.Middleware.Casbin.AutoLoadInterval) * time.Second)
	go a.autoLoad(ctx, signal)
	return nil
}

// reloadSignal returns the value role and menu changes write to the cache.
// Every change writes a new unique ID, so any difference means "reload".
func (a *Casbinx) reloadSignal(ctx context.Context) (string, error) {
	val, _, err := a.Cache.Get(ctx, config.CacheNSForRole, config.CacheKeyForSyncToCasbin)
	return val, err
}

func (a *Casbinx) load(ctx context.Context) error {
	start := time.Now()
	roleResult, err := a.RoleDAL.Query(ctx, schema.RoleQueryParam{
		Status: schema.RoleStatusEnabled,
	}, schema.RoleQueryOptions{
		QueryOptions: util.QueryOptions{SelectFields: []string{"id"}},
	})
	if err != nil {
		return err
	}

	var resCount int32
	queue := make(chan *policyQueueItem, len(roleResult.Data))
	threadNum := max(a.Config.Middleware.Casbin.LoadThread, 1)
	lock := new(sync.Mutex)
	buf := new(bytes.Buffer)

	wg := new(sync.WaitGroup)
	wg.Add(threadNum)
	for i := 0; i < threadNum; i++ {
		go func() {
			defer wg.Done()
			ibuf := new(bytes.Buffer)
			for item := range queue {
				for _, res := range item.Resources {
					// No trailing whitespace: the string adapter does not trim
					// lines, so "GET " would never match a request method.
					_, _ = fmt.Fprintf(ibuf, "p, %s, %s, %s\n", item.RoleID, res.Path, res.Method)
				}
			}
			lock.Lock()
			_, _ = buf.Write(ibuf.Bytes())
			lock.Unlock()
		}()
	}

	var loadErr error
	for _, item := range roleResult.Data {
		resources, err := a.queryRoleResources(ctx, item.ID)
		if err != nil {
			// Abort the whole load: installing a partial policy would deny the
			// role's users until the next change. Keep the current enforcer and
			// let the next tick retry.
			loadErr = fmt.Errorf("query resources of role %s: %w", item.ID, err)
			break
		}
		atomic.AddInt32(&resCount, int32(len(resources)))
		queue <- &policyQueueItem{
			RoleID:    item.ID,
			Resources: resources,
		}
	}
	close(queue)
	wg.Wait()
	if loadErr != nil {
		return loadErr
	}

	// Always swap in a fresh enforcer, even with no policies: keeping the old
	// one would continue to allow permissions that were just revoked.
	// Policies are loaded from memory, so nothing is written to the work dir.
	modelFile := filepath.Join(a.Config.General.WorkDir, a.Config.Middleware.Casbin.ModelFile)
	m, err := model.NewModelFromFile(modelFile)
	if err != nil {
		logging.Context(ctx).Error("Failed to load casbin model", zap.Error(err))
		return err
	}
	var e *casbin.Enforcer
	if buf.Len() > 0 {
		e, err = casbin.NewEnforcer(m, stringadapter.NewAdapter(buf.String()))
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
	a.enforcer.Store(e)

	logging.Context(ctx).Info("Casbin load policy",
		zap.Duration("cost", time.Since(start)),
		zap.Int("roles", len(roleResult.Data)),
		zap.Int32("resources", resCount),
		zap.Int("bytes", buf.Len()),
	)
	return nil
}

func (a *Casbinx) queryRoleResources(ctx context.Context, roleID string) (schema.MenuResources, error) {
	menuResult, err := a.MenuDAL.Query(ctx, schema.MenuQueryParam{
		RoleID: roleID,
		Status: schema.MenuStatusEnabled,
	}, schema.MenuQueryOptions{
		QueryOptions: util.QueryOptions{
			SelectFields: []string{"id", "parent_id", "parent_path"},
		},
	})
	if err != nil {
		return nil, err
	} else if len(menuResult.Data) == 0 {
		return nil, nil
	}

	menuIDs := make([]string, 0, len(menuResult.Data))
	menuIDMapper := make(map[string]struct{})
	for _, item := range menuResult.Data {
		if _, ok := menuIDMapper[item.ID]; ok {
			continue
		}
		menuIDs = append(menuIDs, item.ID)
		menuIDMapper[item.ID] = struct{}{}
		if pp := item.ParentPath; pp != "" {
			for _, pid := range strings.Split(pp, util.TreePathDelimiter) {
				if pid == "" {
					continue
				}
				if _, ok := menuIDMapper[pid]; ok {
					continue
				}
				menuIDs = append(menuIDs, pid)
				menuIDMapper[pid] = struct{}{}
			}
		}
	}

	menuResourceResult, err := a.MenuResourceDAL.Query(ctx, schema.MenuResourceQueryParam{
		MenuIDs: menuIDs,
	})
	if err != nil {
		return nil, err
	}

	return menuResourceResult.Data, nil
}

// autoLoad reloads policies whenever the reload signal differs from the one
// last handled. Comparing for inequality rather than ordering means changes
// made within the same second, or on hosts with skewed clocks, are not lost.
func (a *Casbinx) autoLoad(ctx context.Context, handled string) {
	for range a.ticker.C {
		signal, err := a.reloadSignal(ctx)
		if err != nil {
			logging.Context(ctx).Error("Failed to get cache", zap.Error(err), zap.String("key", config.CacheKeyForSyncToCasbin))
			continue
		}
		if signal == handled {
			continue
		}
		if err := a.load(ctx); err != nil {
			logging.Context(ctx).Error("Failed to load casbin policy", zap.Error(err))
			continue // keep the old value so the next tick retries
		}
		handled = signal
	}
}

func (a *Casbinx) Release(ctx context.Context) error {
	if a.ticker != nil {
		a.ticker.Stop()
	}
	return nil
}
