// Package e2e drives the real HTTP pipeline (every middleware, casbin, JWT,
// Redis token store) against a real database and Redis.
//
// By default it runs against a temporary SQLite database with in-process
// caches, so "go test ./..." covers authentication and permissions. Set all
// of these to run against a real database and Redis instead; the checks that
// need Redis only run then:
//
//	E2E_DB_TYPE     mysql or postgres
//	E2E_DB_DSN      DSN of a database the test may write to
//	E2E_REDIS_ADDR  Redis address, e.g. 127.0.0.1:6379
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LyricTian/captcha"
	"github.com/LyricTian/captcha/store"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/GoRustC/new-goadmin/internal/bootstrap"
	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/internal/wirex"
	"github.com/GoRustC/new-goadmin/pkg/cachex"
	"github.com/GoRustC/new-goadmin/pkg/captchax"
	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
	"github.com/GoRustC/new-goadmin/pkg/jwtx"
	"github.com/GoRustC/new-goadmin/pkg/middleware"
)

const rootPassword = "Test-Root-123"

var (
	withRedis bool
	server    *httptest.Server
	injector  *wirex.Injector
	captchas  *recordingStore
	// metricsRegistry receives the request metrics of the handler under test.
	metricsRegistry = prometheus.NewRegistry()
)

// recordingStore keeps a copy of each captcha's digits so the test can answer
// the captcha the way a human reading the image would.
type recordingStore struct {
	store.Store
	mu     sync.Mutex
	digits map[string]string
}

func (s *recordingStore) Set(id string, digits []byte) {
	code := make([]byte, len(digits))
	for i, d := range digits {
		code[i] = '0' + d
	}
	s.mu.Lock()
	s.digits[id] = string(code)
	s.mu.Unlock()
	s.Store.Set(id, digits)
}

func (s *recordingStore) answer(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.digits[id]
}

func TestMain(m *testing.M) {
	dbType, dsn, redisAddr := os.Getenv("E2E_DB_TYPE"), os.Getenv("E2E_DB_DSN"), os.Getenv("E2E_REDIS_ADDR")
	withRedis = dbType != "" && dsn != "" && redisAddr != ""
	// Removed explicitly before os.Exit, which skips deferred calls.
	var tempDir string
	if !withRedis {
		dir, err := os.MkdirTemp("", "ginadmin-e2e-")
		if err != nil {
			panic(err)
		}
		tempDir = dir
		dbType, dsn, redisAddr = "sqlite3", filepath.Join(dir, "e2e.db"), "unused:6379"
	}

	// Load the production sample exactly as a deployment would.
	env := map[string]string{
		"GINADMIN_DB_TYPE":       dbType,
		"GINADMIN_DB_DSN":        dsn,
		"GINADMIN_REDIS_ADDR":    redisAddr,
		"GINADMIN_JWT_KEY":       strings.Repeat("e2e-signing-key-", 4),
		"GINADMIN_ROOT_PASSWORD": mustBcrypt(hash.MD5String(rootPassword)),
	}
	for k, v := range env {
		os.Setenv(k, v)
	}
	config.MustLoad("../../configs", "prod")
	config.C.General.WorkDir = "../../configs"
	// The suite sends thousands of requests from one address; rate limiting
	// is covered by its own tests in pkg/middleware.
	config.C.Middleware.RateLimiter.Enable = false
	if !withRedis {
		config.C.Storage.Cache.Type = "memory"
		config.C.Middleware.Auth.Store.Type = "memory"
		config.C.Util.Captcha.CacheType = "memory"
	}
	config.C.PreLoad()
	if err := config.C.Validate(); err != nil {
		panic(err)
	}

	ctx := context.Background()
	inj, cleanup, err := wirex.BuildInjector(ctx)
	if err != nil {
		panic(err)
	}
	if err := inj.M.Init(ctx); err != nil {
		panic(err)
	}

	// Init installed the configured captcha store; wrap the same kind of store
	// so the test can read answers while still exercising it.
	var inner store.Store = captchax.NewMemoryStore(captcha.Expiration)
	if c := config.C.Util.Captcha; c.CacheType == "redis" {
		inner = captchax.NewRedisStore(captchax.RedisConfig{
			Addr: c.Redis.Addr, Username: c.Redis.Username, Password: c.Redis.Password,
			DB: c.Redis.DB, KeyPrefix: config.C.Storage.Cache.KeyPrefix + c.Redis.KeyPrefix, // as the module does
		}, captcha.Expiration)
	}
	captchas = &recordingStore{Store: inner, digits: map[string]string{}}
	captcha.SetCustomStore(captchas)
	handler, err := bootstrap.NewHTTPHandler(ctx, inj, metricsRegistry)
	if err != nil {
		panic(err)
	}
	injector = inj
	server = httptest.NewServer(handler)

	code := m.Run()

	server.Close()
	_ = inj.M.Release(ctx)
	cleanup()
	if tempDir != "" {
		_ = os.RemoveAll(tempDir)
	}
	os.Exit(code)
}

type response struct {
	status  int
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		ID     string `json:"id"`
		Detail string `json:"detail"`
	} `json:"error"`
}

func call(t *testing.T, method, path, token string, body any) response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var r response
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &r)
	r.status = res.StatusCode
	return r
}

func expectStatus(t *testing.T, what string, r response, want int) {
	t.Helper()
	if r.status != want {
		detail := ""
		if r.Error != nil {
			detail = r.Error.ID + ": " + r.Error.Detail
		}
		t.Fatalf("%s: got HTTP %d (%s), want %d", what, r.status, detail, want)
	}
}

func dataField(t *testing.T, r response, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Data, &m); err != nil {
		t.Fatalf("decode data: %v (%s)", err, r.Data)
	}
	v, _ := m[field].(string)
	return v
}

func newCaptcha(t *testing.T) (id, code string) {
	t.Helper()
	r := call(t, http.MethodGet, "/api/v1/captcha/id", "", nil)
	expectStatus(t, "get captcha id", r, http.StatusOK)
	id = dataField(t, r, "captcha_id")
	return id, captchas.answer(id)
}

func login(t *testing.T, username, password string) response {
	t.Helper()
	id, code := newCaptcha(t)
	return call(t, http.MethodPost, "/api/v1/login", "", map[string]string{
		"username":     username,
		"password":     hash.MD5String(password),
		"captcha_id":   id,
		"captcha_code": code,
	})
}

func mustLogin(t *testing.T, username, password string) string {
	t.Helper()
	r := login(t, username, password)
	expectStatus(t, "login as "+username, r, http.StatusOK)
	token := dataField(t, r, "access_token")
	if token == "" {
		t.Fatalf("login as %s returned no token", username)
	}
	return token
}

// eventually polls until the request returns want, because casbin picks up
// role changes on its reload interval rather than immediately.
func eventually(t *testing.T, what, method, path, token string, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last response
	for time.Now().Before(deadline) {
		last = call(t, method, path, token, nil)
		if last.status == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	expectStatus(t, what, last, want)
}

func TestLoginAndPermissions(t *testing.T) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Authentication basics.
	expectStatus(t, "no token", call(t, http.MethodGet, "/api/v1/users", "", nil), http.StatusUnauthorized)
	expectStatus(t, "wrong root password", login(t, "admin", "wrong"), http.StatusBadRequest)

	id, code := newCaptcha(t)
	badCaptcha := call(t, http.MethodPost, "/api/v1/login", "", map[string]string{
		"username": "admin", "password": hash.MD5String(rootPassword), "captcha_id": id, "captcha_code": code + "9",
	})
	expectStatus(t, "wrong captcha", badCaptcha, http.StatusBadRequest)
	reuse := call(t, http.MethodPost, "/api/v1/login", "", map[string]string{
		"username": "admin", "password": hash.MD5String(rootPassword), "captcha_id": id, "captcha_code": code,
	})
	expectStatus(t, "captcha cannot be reused after a failed attempt", reuse, http.StatusBadRequest)

	root := mustLogin(t, "admin", rootPassword)
	me := call(t, http.MethodGet, "/api/v1/current/user", root, nil)
	expectStatus(t, "root current user", me, http.StatusOK)
	if got := dataField(t, me, "username"); got != "admin" {
		t.Fatalf("root username: got %q", got)
	}
	expectStatus(t, "root bypasses casbin", call(t, http.MethodGet, "/api/v1/loggers", root, nil), http.StatusOK)

	// Grant a role the "user" page only.
	var userMenu schema.Menu
	if err := injector.DB.Where("code = ?", "user").First(&userMenu).Error; err != nil {
		t.Fatalf("find user menu: %v", err)
	}
	role := call(t, http.MethodPost, "/api/v1/roles", root, map[string]any{
		"code": "e2e-" + suffix, "name": "e2e " + suffix, "status": "enabled",
		"menus": []map[string]string{{"menu_id": userMenu.ID}},
	})
	expectStatus(t, "create role", role, http.StatusOK)
	roleID := dataField(t, role, "id")

	username := "e2e" + suffix
	user := call(t, http.MethodPost, "/api/v1/users", root, map[string]any{
		"username": username, "name": "e2e user", "password": hash.MD5String("User-Pass-1"),
		"status": "activated", "roles": []map[string]string{{"role_id": roleID}},
	})
	expectStatus(t, "create user", user, http.StatusOK)
	userID := dataField(t, user, "id")
	t.Cleanup(func() {
		call(t, http.MethodDelete, "/api/v1/users/"+userID, root, nil)
		call(t, http.MethodDelete, "/api/v1/roles/"+roleID, root, nil)
	})

	expectStatus(t, "new user wrong password", login(t, username, "wrong"), http.StatusBadRequest)
	token := mustLogin(t, username, "User-Pass-1")

	// Granted by the "user" page.
	eventually(t, "granted: list users", http.MethodGet, "/api/v1/users", token, http.StatusOK)
	expectStatus(t, "granted: get user", call(t, http.MethodGet, "/api/v1/users/"+userID, token, nil), http.StatusOK)
	expectStatus(t, "granted: list roles", call(t, http.MethodGet, "/api/v1/roles", token, nil), http.StatusOK)
	expectStatus(t, "current user needs no grant", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusOK)

	// Not granted: the "add" button and other pages.
	expectStatus(t, "denied: create user", call(t, http.MethodPost, "/api/v1/users", token, map[string]any{
		"username": "x" + suffix, "name": "x", "status": "activated", "roles": []map[string]string{},
	}), http.StatusForbidden)
	expectStatus(t, "denied: loggers", call(t, http.MethodGet, "/api/v1/loggers", token, nil), http.StatusForbidden)
	expectStatus(t, "denied: menus", call(t, http.MethodGet, "/api/v1/menus", token, nil), http.StatusForbidden)

	// Revoking the role's only menu must take effect without a restart.
	upd := call(t, http.MethodPut, "/api/v1/roles/"+roleID, root, map[string]any{
		"code": "e2e-" + suffix, "name": "e2e " + suffix, "status": "enabled", "menus": []map[string]string{},
	})
	expectStatus(t, "revoke role menus", upd, http.StatusOK)
	eventually(t, "revoked: list users", http.MethodGet, "/api/v1/users", token, http.StatusForbidden)

	// Logout puts the token on the Redis blacklist.
	expectStatus(t, "logout", call(t, http.MethodPost, "/api/v1/current/logout", token, nil), http.StatusOK)
	expectStatus(t, "token rejected after logout", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusUnauthorized)

	// A second instance sharing Redis must reject it too.
	if !withRedis {
		t.Log("skipping the cross-instance logout check: no Redis configured")
		return
	}
	other := jwtx.New(jwtx.NewStoreWithCache(cachex.NewRedisCache(cachex.RedisConfig{
		Addr: config.C.Middleware.Auth.Store.Redis.Addr,
		DB:   config.C.Middleware.Auth.Store.Redis.DB,
	}, cachex.WithDelimiter(config.C.Middleware.Auth.Store.Delimiter),
		cachex.WithKeyPrefix(config.C.Storage.Cache.KeyPrefix))),
		jwtx.SetSigningKey(config.C.Middleware.Auth.SigningKey, ""))
	if _, err := other.ParseSubject(context.Background(), token); !errors.Is(err, jwtx.ErrInvalidToken) {
		t.Fatalf("another instance accepted a logged-out token: err=%v", err)
	}
	_ = other.Release(context.Background())
}

func TestRateLimiterRedisStore(t *testing.T) {
	if !withRedis {
		t.Skip("no Redis configured")
	}
	ctx := context.Background()
	s := middleware.NewRateLimiterRedisStore(middleware.RateLimiterRedisConfig{
		Addr:      config.C.Storage.Cache.Redis.Addr,
		DB:        config.C.Middleware.RateLimiter.Store.Redis.DB,
		KeyPrefix: config.C.Storage.Cache.KeyPrefix,
	})
	key := fmt.Sprintf("e2e-ip-%d", time.Now().UnixNano())

	// 5 requests per 10 seconds: MaxRequests < Period used to compute a rate
	// of 0 and deny everything.
	for i := 1; i <= 5; i++ {
		ok, err := s.Allow(ctx, key, 10*time.Second, 5)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if ok, _ := s.Allow(ctx, key, 10*time.Second, 5); ok {
		t.Fatal("6th request in the period should be denied")
	}
}

// mustBcrypt hashes a root password the way "ginadmin hash-password" does;
// production refuses a plain MD5 root password.
func mustBcrypt(md5 string) string {
	h, err := hash.GeneratePassword(md5)
	if err != nil {
		panic(err)
	}
	return h
}
