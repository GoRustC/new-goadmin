package e2e

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/mods/rbac/schema"
	"github.com/GoRustC/new-goadmin/pkg/crypto/hash"
)

func unique() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }

func menuID(t *testing.T, code string) string {
	t.Helper()
	var m schema.Menu
	if err := injector.DB.Where("code = ?", code).First(&m).Error; err != nil {
		t.Fatalf("find menu %q: %v", code, err)
	}
	return m.ID
}

func createRole(t *testing.T, root, suffix string, menuIDs ...string) string {
	t.Helper()
	menus := []map[string]string{}
	for _, id := range menuIDs {
		menus = append(menus, map[string]string{"menu_id": id})
	}
	r := call(t, http.MethodPost, "/api/v1/roles", root, map[string]any{
		"code": "r" + suffix, "name": "role " + suffix, "status": "enabled", "menus": menus,
	})
	expectStatus(t, "create role", r, http.StatusOK)
	id := dataField(t, r, "id")
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/roles/"+id, root, nil) })
	return id
}

func userForm(username, password, status string, roleIDs ...string) map[string]any {
	roles := []map[string]string{}
	for _, id := range roleIDs {
		roles = append(roles, map[string]string{"role_id": id})
	}
	form := map[string]any{"username": username, "name": "name " + username, "status": status, "roles": roles}
	if password != "" {
		form["password"] = hash.MD5String(password)
	}
	return form
}

func createUser(t *testing.T, root, username, password string, roleIDs ...string) string {
	t.Helper()
	r := call(t, http.MethodPost, "/api/v1/users", root, userForm(username, password, "activated", roleIDs...))
	expectStatus(t, "create user "+username, r, http.StatusOK)
	id := dataField(t, r, "id")
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/users/"+id, root, nil) })
	return id
}

func list(t *testing.T, r response) []map[string]any {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal(r.Data, &items); err != nil {
		t.Fatalf("decode list: %v (%s)", err, r.Data)
	}
	return items
}

func TestCaptchaEndpoints(t *testing.T) {
	id, _ := newCaptcha(t)

	res, err := http.Get(server.URL + "/api/v1/captcha/image?id=" + id)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(body, []byte("\x89PNG")) {
		t.Fatalf("captcha image: status %d, type %q, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(body))
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("captcha image should not be cached, Cache-Control=%q", cc)
	}

	reload := call(t, http.MethodGet, "/api/v1/captcha/image?reload=1&id="+id, "", nil)
	if reload.status != http.StatusOK {
		t.Fatalf("captcha reload: HTTP %d", reload.status)
	}
	res, err = http.Get(server.URL + "/api/v1/captcha/image?id=nope")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("unknown captcha id: status %d, type %q, want 404 JSON", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestErrorContract(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)

	nf := call(t, http.MethodGet, "/api/v1/does-not-exist", root, nil)
	expectStatus(t, "unknown route", nf, http.StatusNotFound)
	if nf.Success || nf.Error == nil || nf.Error.ID == "" {
		t.Errorf("errors must use the {success:false,error:{id,...}} envelope, got %+v", nf)
	}

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/roles", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer "+root)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed JSON: got HTTP %d, want 400", res.StatusCode)
	}

	expectStatus(t, "missing required fields", call(t, http.MethodPost, "/api/v1/roles", root, map[string]any{}), http.StatusBadRequest)
	expectStatus(t, "page size over the maximum", call(t, http.MethodGet, "/api/v1/users?current=1&pageSize=101", root, nil), http.StatusBadRequest)
	expectStatus(t, "unknown user id", call(t, http.MethodGet, "/api/v1/users/nope", root, nil), http.StatusNotFound)
	expectStatus(t, "unknown role id", call(t, http.MethodGet, "/api/v1/roles/nope", root, nil), http.StatusNotFound)
	expectStatus(t, "unknown menu id", call(t, http.MethodGet, "/api/v1/menus/nope", root, nil), http.StatusNotFound)
	expectStatus(t, "garbage token", call(t, http.MethodGet, "/api/v1/current/user", "garbage", nil), http.StatusUnauthorized)
}

func TestRequestBodyLimits(t *testing.T) {
	limit := config.C.Middleware.CopyBody.MaxContentLen

	big := bytes.Repeat([]byte("a"), int(limit)+1)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/login", bytes.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: got HTTP %d, want 413", res.StatusCode)
	}

	// A gzip bomb: tiny on the wire, larger than the limit once decompressed.
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(make([]byte, limit*2))
	_ = w.Close()
	if int64(gz.Len()) >= limit {
		t.Fatalf("test setup: compressed payload is %d bytes, not smaller than the %d limit", gz.Len(), limit)
	}
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/login", bytes.NewReader(gz.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("gzip bomb (%d bytes compressed): got HTTP %d, want 413", gz.Len(), res.StatusCode)
	}
}

func TestCurrentUserEndpoints(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	suffix := unique()
	roleID := createRole(t, root, suffix, menuID(t, "user"))
	username := "cur" + suffix
	createUser(t, root, username, "Old-Pass-1", roleID)
	token := mustLogin(t, username, "Old-Pass-1")

	// Root is a config account: it cannot be edited through the API.
	expectStatus(t, "root cannot update profile", call(t, http.MethodPut, "/api/v1/current/user", root, map[string]any{"name": "x"}), http.StatusBadRequest)
	expectStatus(t, "root cannot change password", call(t, http.MethodPut, "/api/v1/current/password", root, map[string]any{
		"old_password": hash.MD5String(rootPassword), "new_password": hash.MD5String("x"),
	}), http.StatusBadRequest)

	// Menus: root sees the whole tree, the user only the granted page and its parent.
	rootMenus := list(t, call(t, http.MethodGet, "/api/v1/current/menus", root, nil))
	userMenus := list(t, call(t, http.MethodGet, "/api/v1/current/menus", token, nil))
	if len(rootMenus) < 2 {
		t.Fatalf("root menu tree has %d top-level entries", len(rootMenus))
	}
	if len(userMenus) != 1 || userMenus[0]["code"] != "system" {
		t.Fatalf("user menu tree should be just the system parent, got %v", userMenus)
	}
	children, _ := userMenus[0]["children"].([]any)
	if len(children) != 1 || children[0].(map[string]any)["code"] != "user" {
		t.Fatalf("user should see only the user page under system, got %v", children)
	}

	// Profile update.
	upd := call(t, http.MethodPut, "/api/v1/current/user", token, map[string]any{"name": "New Name", "email": "a@example.com"})
	expectStatus(t, "update profile", upd, http.StatusOK)
	if got := dataField(t, call(t, http.MethodGet, "/api/v1/current/user", token, nil), "name"); got != "New Name" {
		t.Errorf("profile name after update: %q", got)
	}

	// Token refresh issues a working token and retires the one it replaced.
	ref := call(t, http.MethodPost, "/api/v1/current/refresh-token", token, nil)
	expectStatus(t, "refresh token", ref, http.StatusOK)
	newToken := dataField(t, ref, "access_token")
	expectStatus(t, "refreshed token works", call(t, http.MethodGet, "/api/v1/current/user", newToken, nil), http.StatusOK)
	expectStatus(t, "token replaced by refresh", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusUnauthorized)
	if a, b := jwtAuthTime(t, token), jwtAuthTime(t, newToken); a == 0 || a != b {
		t.Fatalf("refresh must keep the session start: auth_time %v -> %v", a, b)
	}
	expectStatus(t, "root can refresh", call(t, http.MethodPost, "/api/v1/current/refresh-token", root, nil), http.StatusOK)

	// Password change signs out every session, including the current one.
	other := mustLogin(t, username, "Old-Pass-1")
	expectStatus(t, "wrong old password", call(t, http.MethodPut, "/api/v1/current/password", newToken, map[string]any{
		"old_password": hash.MD5String("wrong"), "new_password": hash.MD5String("New-Pass-2"),
	}), http.StatusBadRequest)
	expectStatus(t, "change password", call(t, http.MethodPut, "/api/v1/current/password", newToken, map[string]any{
		"old_password": hash.MD5String("Old-Pass-1"), "new_password": hash.MD5String("New-Pass-2"),
	}), http.StatusOK)
	expectStatus(t, "current session after password change", call(t, http.MethodGet, "/api/v1/current/user", newToken, nil), http.StatusUnauthorized)
	expectStatus(t, "other session after password change", call(t, http.MethodGet, "/api/v1/current/user", other, nil), http.StatusUnauthorized)
	expectStatus(t, "old password rejected", login(t, username, "Old-Pass-1"), http.StatusBadRequest)
	fresh := mustLogin(t, username, "New-Pass-2")
	expectStatus(t, "login right after the change works", call(t, http.MethodGet, "/api/v1/current/user", fresh, nil), http.StatusOK)
}

func TestUserManagement(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	suffix := unique()
	roleID := createRole(t, root, suffix, menuID(t, "user"))
	username := "um" + suffix
	userID := createUser(t, root, username, "Pass-1", roleID)

	// No default password is configured in the production sample.
	expectStatus(t, "create without password", call(t, http.MethodPost, "/api/v1/users", root, userForm("np"+suffix, "", "activated")), http.StatusBadRequest)
	expectStatus(t, "reset without default password", call(t, http.MethodPatch, "/api/v1/users/"+userID+"/reset-pwd", root, nil), http.StatusBadRequest)
	expectStatus(t, "duplicate username", call(t, http.MethodPost, "/api/v1/users", root, userForm(username, "x", "activated")), http.StatusConflict)

	// List, filter and pagination.
	all := call(t, http.MethodGet, "/api/v1/users?current=1&pageSize=0", root, nil)
	expectStatus(t, "list users", all, http.StatusOK)
	if n := len(list(t, all)); n == 0 || n > 10 {
		t.Errorf("pageSize=0 should fall back to 10 rows, got %d", n)
	}
	found := list(t, call(t, http.MethodGet, "/api/v1/users?current=1&pageSize=10&username="+username, root, nil))
	if len(found) != 1 || found[0]["id"] != userID {
		t.Fatalf("filter by username: %v", found)
	}
	if _, leaked := found[0]["password"]; leaked {
		t.Fatal("user list exposes the password hash")
	}
	got := call(t, http.MethodGet, "/api/v1/users/"+userID, root, nil)
	expectStatus(t, "get user", got, http.StatusOK)
	if strings.Contains(string(got.Data), "$2a$") {
		t.Fatal("user detail exposes the password hash")
	}

	// Freezing a user ends their session and blocks login.
	token := mustLogin(t, username, "Pass-1")
	expectStatus(t, "active user", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusOK)
	expectStatus(t, "freeze user", call(t, http.MethodPut, "/api/v1/users/"+userID, root, userForm(username, "", "freezed", roleID)), http.StatusOK)
	expectStatus(t, "frozen user's token", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusUnauthorized)
	expectStatus(t, "frozen user login", login(t, username, "Pass-1"), http.StatusBadRequest)

	// Updating without a password keeps the old one.
	expectStatus(t, "unfreeze user", call(t, http.MethodPut, "/api/v1/users/"+userID, root, userForm(username, "", "activated", roleID)), http.StatusOK)
	token = mustLogin(t, username, "Pass-1")

	// Deleting a user ends their session.
	expectStatus(t, "delete user", call(t, http.MethodDelete, "/api/v1/users/"+userID, root, nil), http.StatusOK)
	expectStatus(t, "deleted user's token", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusUnauthorized)
	expectStatus(t, "deleted user is gone", call(t, http.MethodGet, "/api/v1/users/"+userID, root, nil), http.StatusNotFound)
}

func TestRoleManagement(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	suffix := unique()
	userMenu, loggerMenu := menuID(t, "user"), menuID(t, "logger")
	roleID := createRole(t, root, suffix, userMenu)

	expectStatus(t, "duplicate role code", call(t, http.MethodPost, "/api/v1/roles", root, map[string]any{
		"code": "r" + suffix, "name": "dup", "status": "enabled",
	}), http.StatusConflict)

	got := call(t, http.MethodGet, "/api/v1/roles/"+roleID, root, nil)
	expectStatus(t, "get role", got, http.StatusOK)
	var role struct {
		Menus []struct {
			MenuID string `json:"menu_id"`
		} `json:"menus"`
	}
	_ = json.Unmarshal(got.Data, &role)
	if len(role.Menus) != 1 || role.Menus[0].MenuID != userMenu {
		t.Fatalf("role menus: %+v", role.Menus)
	}
	if found := list(t, call(t, http.MethodGet, "/api/v1/roles?current=1&pageSize=10&name=role+"+suffix, root, nil)); len(found) != 1 {
		t.Fatalf("filter roles by name: %v", found)
	}

	username := "rm" + suffix
	createUser(t, root, username, "Pass-1", roleID)
	token := mustLogin(t, username, "Pass-1")
	eventually(t, "granted users page", http.MethodGet, "/api/v1/users", token, http.StatusOK)
	expectStatus(t, "loggers not granted yet", call(t, http.MethodGet, "/api/v1/loggers", token, nil), http.StatusForbidden)

	// Adding a menu to the role grants its APIs without a restart.
	expectStatus(t, "add logger menu", call(t, http.MethodPut, "/api/v1/roles/"+roleID, root, map[string]any{
		"code": "r" + suffix, "name": "role " + suffix, "status": "enabled",
		"menus": []map[string]string{{"menu_id": userMenu}, {"menu_id": loggerMenu}},
	}), http.StatusOK)
	eventually(t, "loggers granted", http.MethodGet, "/api/v1/loggers", token, http.StatusOK)

	// Disabling the role revokes everything.
	expectStatus(t, "disable role", call(t, http.MethodPut, "/api/v1/roles/"+roleID, root, map[string]any{
		"code": "r" + suffix, "name": "role " + suffix, "status": "disabled",
		"menus": []map[string]string{{"menu_id": userMenu}, {"menu_id": loggerMenu}},
	}), http.StatusOK)
	eventually(t, "disabled role", http.MethodGet, "/api/v1/users", token, http.StatusForbidden)

	expectStatus(t, "delete role", call(t, http.MethodDelete, "/api/v1/roles/"+roleID, root, nil), http.StatusOK)
	expectStatus(t, "deleted role is gone", call(t, http.MethodGet, "/api/v1/roles/"+roleID, root, nil), http.StatusNotFound)
}

func TestMenusAndLoggers(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)

	tree := list(t, call(t, http.MethodGet, "/api/v1/menus?includeResources=true", root, nil))
	var userPage map[string]any
	var walk func([]any)
	walk = func(items []any) {
		for _, it := range items {
			m := it.(map[string]any)
			if m["code"] == "user" && m["type"] == "page" {
				userPage = m
			}
			if c, ok := m["children"].([]any); ok {
				walk(c)
			}
		}
	}
	asAny := make([]any, len(tree))
	for i := range tree {
		asAny[i] = tree[i]
	}
	walk(asAny)
	if userPage == nil {
		t.Fatal("menu tree has no user page")
	}
	if res, _ := userPage["resources"].([]any); len(res) == 0 {
		t.Fatal("includeResources=true returned no resources for the user page")
	}

	id := userPage["id"].(string)
	expectStatus(t, "get menu", call(t, http.MethodGet, "/api/v1/menus/"+id, root, nil), http.StatusOK)

	// The production sample sets DenyOperateMenu: menus come from menu.json only.
	expectStatus(t, "create menu denied", call(t, http.MethodPost, "/api/v1/menus", root, map[string]any{
		"code": "x" + unique(), "name": "x", "type": "page", "status": "enabled",
	}), http.StatusBadRequest)
	expectStatus(t, "delete menu denied", call(t, http.MethodDelete, "/api/v1/menus/"+id, root, nil), http.StatusBadRequest)

	logs := call(t, http.MethodGet, "/api/v1/loggers?current=1&pageSize=5&level=info", root, nil)
	expectStatus(t, "query loggers", logs, http.StatusOK)
	if n := len(list(t, logs)); n > 5 {
		t.Errorf("logger page size 5 returned %d rows", n)
	}
	expectStatus(t, "query loggers by user and time", call(t, http.MethodGet,
		"/api/v1/loggers?current=1&pageSize=5&userName=admin&startTime=2020-01-01+00:00:00&endTime=2099-01-01+00:00:00", root, nil), http.StatusOK)
}

// The captcha library's default store deadlocked when one captcha was
// verified by many requests at once, after which no one could log in.
func TestConcurrentCaptchaVerification(t *testing.T) {
	const rounds, concurrency = 200, 32
	for round := range rounds {
		id, _ := newCaptcha(t)
		body := map[string]string{"username": "x", "password": "x", "captcha_id": id, "captcha_code": "0000"}
		done := make(chan struct{})
		for range concurrency {
			go func() {
				defer func() { done <- struct{}{} }()
				b, _ := json.Marshal(body)
				res, err := http.Post(server.URL+"/api/v1/login", "application/json", bytes.NewReader(b))
				if err == nil {
					res.Body.Close()
				}
			}()
		}
		for range concurrency {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("round %d: login requests hung", round)
			}
		}
	}

	ch := make(chan response, 1)
	go func() { ch <- call(t, http.MethodGet, "/api/v1/captcha/id", "", nil) }()
	select {
	case r := <-ch:
		expectStatus(t, "captcha after concurrent verification", r, http.StatusOK)
	case <-time.After(10 * time.Second):
		t.Fatal("captcha endpoint hung after concurrent verification")
	}
	mustLogin(t, "admin", rootPassword)
}

// childMenuID finds a button such as "add" under a page such as "user"; button
// codes repeat across pages.
func childMenuID(t *testing.T, parentCode, code string) string {
	t.Helper()
	parent := menuID(t, parentCode)
	var m schema.Menu
	if err := injector.DB.Where("code = ? AND parent_id = ?", code, parent).First(&m).Error; err != nil {
		t.Fatalf("find menu %s.%s: %v", parentCode, code, err)
	}
	return m.ID
}

func roleForm(code string, menuIDs ...string) map[string]any {
	menus := []map[string]string{}
	for _, id := range menuIDs {
		menus = append(menus, map[string]string{"menu_id": id})
	}
	return map[string]any{"code": code, "name": "role " + code, "status": "enabled", "menus": menus}
}

// A user or role administrator must not be able to grant, or take over,
// privileges they do not hold. Before the guard, editing users or roles was
// equivalent to root.
func TestPrivilegeCeiling(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()

	userPage, rolePage, loggerPage := menuID(t, "user"), menuID(t, "role"), menuID(t, "logger")
	adminMenus := []string{
		userPage, childMenuID(t, "user", "add"), childMenuID(t, "user", "edit"), childMenuID(t, "user", "delete"),
		rolePage, childMenuID(t, "role", "add"), childMenuID(t, "role", "edit"), childMenuID(t, "role", "delete"),
	}
	adminRole := createRole(t, root, "adm"+s, adminMenus...)
	lowRole := createRole(t, root, "low"+s, userPage)
	highRole := createRole(t, root, "high"+s, userPage, loggerPage)

	adminID := createUser(t, root, "adm"+s, "Pass-1", adminRole)
	lowID := createUser(t, root, "low"+s, "Pass-1", lowRole)
	highID := createUser(t, root, "high"+s, "Pass-1", highRole)

	admin := mustLogin(t, "adm"+s, "Pass-1")
	eventually(t, "admin can manage users", http.MethodGet, "/api/v1/users", admin, http.StatusOK)
	eventually(t, "admin can manage roles", http.MethodGet, "/api/v1/roles", admin, http.StatusOK)

	denied := func(what string, r response) {
		t.Helper()
		expectStatus(t, what, r, http.StatusForbidden)
		if r.Error == nil || r.Error.ID != "com.rbac.privilege-escalation" {
			t.Fatalf("%s: expected a privilege escalation error, got %+v", what, r.Error)
		}
	}

	// Users.
	denied("create user with a more privileged role",
		call(t, http.MethodPost, "/api/v1/users", admin, userForm("x1"+s, "Pass-1", "activated", highRole)))
	created := call(t, http.MethodPost, "/api/v1/users", admin, userForm("x2"+s, "Pass-1", "activated", lowRole))
	expectStatus(t, "create user with a lesser role", created, http.StatusOK)
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/users/"+dataField(t, created, "id"), root, nil) })

	denied("take over a more privileged user by setting its password",
		call(t, http.MethodPut, "/api/v1/users/"+highID, admin, userForm("high"+s, "Hijacked-1", "activated", highRole)))
	// menu.json grants the reset-pwd API to no menu, so casbin already denies
	// it to every non-root user before the guard runs.
	expectStatus(t, "reset a more privileged user's password",
		call(t, http.MethodPatch, "/api/v1/users/"+highID+"/reset-pwd", admin, nil), http.StatusForbidden)
	denied("delete a more privileged user", call(t, http.MethodDelete, "/api/v1/users/"+highID, admin, nil))
	denied("give a lesser user a more privileged role",
		call(t, http.MethodPut, "/api/v1/users/"+lowID, admin, userForm("low"+s, "", "activated", lowRole, highRole)))
	denied("change own account through user management",
		call(t, http.MethodPut, "/api/v1/users/"+adminID, admin, userForm("adm"+s, "", "activated", adminRole, highRole)))
	expectStatus(t, "manage a lesser user", call(t, http.MethodPut, "/api/v1/users/"+lowID, admin,
		userForm("low"+s, "", "freezed", lowRole)), http.StatusOK)

	// The victim's password was not changed.
	mustLogin(t, "high"+s, "Pass-1")

	// Roles.
	denied("create a role with menus the admin lacks",
		call(t, http.MethodPost, "/api/v1/roles", admin, roleForm("x3"+s, userPage, loggerPage)))
	lesser := call(t, http.MethodPost, "/api/v1/roles", admin, roleForm("x4"+s, userPage))
	expectStatus(t, "create a role with menus the admin holds", lesser, http.StatusOK)
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/roles/"+dataField(t, lesser, "id"), root, nil) })

	denied("add a missing menu to the admin's own role",
		call(t, http.MethodPut, "/api/v1/roles/"+adminRole, admin, roleForm("adm"+s, append(adminMenus, loggerPage)...)))
	denied("change a more privileged role",
		call(t, http.MethodPut, "/api/v1/roles/"+highRole, admin, roleForm("high"+s, userPage)))
	denied("delete a more privileged role", call(t, http.MethodDelete, "/api/v1/roles/"+highRole, admin, nil))

	// The admin still cannot read logs: nothing above granted it.
	expectStatus(t, "admin still lacks loggers", call(t, http.MethodGet, "/api/v1/loggers", admin, nil), http.StatusForbidden)
}

func TestSecurityHeadersAndReadiness(t *testing.T) {
	r, err := http.Get(server.URL + "/api/v1/captcha/id")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	for k, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("API response %s = %q, want %q", k, got, want)
		}
	}

	// Pages outside /api (for example a hosted frontend) must not get the
	// API's restrictive CSP.
	h, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	h.Body.Close()
	if h.Header.Get("Content-Security-Policy") != "" || h.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("non-API response headers: CSP=%q nosniff=%q", h.Header.Get("Content-Security-Policy"), h.Header.Get("X-Content-Type-Options"))
	}

	expectStatus(t, "readiness", call(t, http.MethodGet, "/health/ready", "", nil), http.StatusOK)
}

func TestLoginLockAndEnumeration(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()
	roleID := createRole(t, root, "lk"+s, menuID(t, "user"))
	username := "lk" + s
	createUser(t, root, username, "Right-1", roleID)

	max := config.C.General.LoginLock.MaxFailures
	for range max {
		expectStatus(t, "wrong password", login(t, username, "wrong"), http.StatusBadRequest)
	}
	locked := login(t, username, "Right-1")
	expectStatus(t, "locked even with the right password", locked, http.StatusTooManyRequests)
	if locked.Error == nil || locked.Error.ID != "com.login.locked" {
		t.Fatalf("expected a lock error, got %+v", locked.Error)
	}

	// Names that do not exist lock the same way, so a lock reveals nothing.
	ghost := "ghost" + s
	for range max {
		r := login(t, ghost, "x")
		expectStatus(t, "unknown user", r, http.StatusBadRequest)
		if r.Error == nil || r.Error.ID != "com.invalid.username-or-password" {
			t.Fatalf("unknown user should get the generic error, got %+v", r.Error)
		}
	}
	expectStatus(t, "unknown name locks too", login(t, ghost, "x"), http.StatusTooManyRequests)

	// Other accounts are unaffected.
	mustLogin(t, "admin", rootPassword)

	// A frozen account answers a wrong password exactly like any other.
	frozen := "fz" + s
	frozenID := createUser(t, root, frozen, "Right-1", roleID)
	expectStatus(t, "freeze", call(t, http.MethodPut, "/api/v1/users/"+frozenID, root, userForm(frozen, "", "freezed", roleID)), http.StatusOK)
	r := login(t, frozen, "wrong")
	if r.Error == nil || r.Error.ID != "com.invalid.username-or-password" {
		t.Fatalf("frozen account with a wrong password must look like any failure, got %+v", r.Error)
	}
}

func TestAdminPasswordChangeSignsUserOut(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()
	roleID := createRole(t, root, "pw"+s, menuID(t, "user"))
	username := "pw" + s
	userID := createUser(t, root, username, "Pass-1", roleID)
	token := mustLogin(t, username, "Pass-1")

	expectStatus(t, "admin sets a new password", call(t, http.MethodPut, "/api/v1/users/"+userID, root,
		userForm(username, "Pass-2", "activated", roleID)), http.StatusOK)
	expectStatus(t, "old session after admin password change", call(t, http.MethodGet, "/api/v1/current/user", token, nil), http.StatusUnauthorized)
	mustLogin(t, username, "Pass-2")
}

// jwtAuthTime reads the auth_time claim without verifying the token.
func jwtAuthTime(t *testing.T, token string) float64 {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		AuthTime float64 `json:"auth_time"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	return claims.AuthTime
}

// withMenuWrites lifts the production sample's DenyOperateMenu for one test.
func withMenuWrites(t *testing.T) {
	t.Helper()
	orig := config.C.General.DenyOperateMenu
	config.C.General.DenyOperateMenu = false
	t.Cleanup(func() { config.C.General.DenyOperateMenu = orig })
}

func menuForm(code, parentID string) map[string]any {
	return map[string]any{"code": code, "name": "menu " + code, "type": "page", "status": "enabled", "parent_id": parentID}
}

func TestMenuCodePathAndCycles(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)

	// ?code=system.user must resolve "user" under "system"; it used to look
	// for "user" at the top level and answer 404.
	r := call(t, http.MethodGet, "/api/v1/menus?code=system.user", root, nil)
	expectStatus(t, "query by code path", r, http.StatusOK)
	// The result is a tree rooted at the matched menus' ancestors.
	var codes []string
	var walk func(items []any, path string)
	walk = func(items []any, path string) {
		for _, it := range items {
			m := it.(map[string]any)
			p := path + "/" + fmt.Sprint(m["code"])
			codes = append(codes, p)
			if c, ok := m["children"].([]any); ok {
				walk(c, p)
			}
		}
	}
	var tree []any
	_ = json.Unmarshal(r.Data, &tree)
	walk(tree, "")
	if !slices.Contains(codes, "/system/user/add") {
		t.Fatalf("system.user should return the user page's buttons, got %v", codes)
	}
	if slices.Contains(codes, "/system/role/add") {
		t.Fatalf("system.user must not return other pages' buttons, got %v", codes)
	}
	expectStatus(t, "unknown code path", call(t, http.MethodGet, "/api/v1/menus?code=system.nope", root, nil), http.StatusNotFound)

	withMenuWrites(t)
	s := unique()
	a := call(t, http.MethodPost, "/api/v1/menus", root, menuForm("a"+s, ""))
	expectStatus(t, "create menu A", a, http.StatusOK)
	aID := dataField(t, a, "id")
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/menus/"+aID, root, nil) })
	b := call(t, http.MethodPost, "/api/v1/menus", root, menuForm("b"+s, aID))
	expectStatus(t, "create menu B under A", b, http.StatusOK)
	bID := dataField(t, b, "id")
	c := call(t, http.MethodPost, "/api/v1/menus", root, menuForm("c"+s, bID))
	expectStatus(t, "create menu C under B", c, http.StatusOK)
	cID := dataField(t, c, "id")

	expectStatus(t, "A under itself", call(t, http.MethodPut, "/api/v1/menus/"+aID, root, menuForm("a"+s, aID)), http.StatusBadRequest)
	expectStatus(t, "A under its child", call(t, http.MethodPut, "/api/v1/menus/"+aID, root, menuForm("a"+s, bID)), http.StatusBadRequest)
	expectStatus(t, "A under its grandchild", call(t, http.MethodPut, "/api/v1/menus/"+aID, root, menuForm("a"+s, cID)), http.StatusBadRequest)

	// A legal move still works: C up to the top level.
	expectStatus(t, "move C to the top level", call(t, http.MethodPut, "/api/v1/menus/"+cID, root, menuForm("c"+s, "")), http.StatusOK)
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/menus/"+cID, root, nil) })
}

// Searches behave the same on every database: case-insensitive (PostgreSQL's
// LIKE is not), with % and _ in the input matched literally.
func TestSearchIsPortable(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()
	roleID := createRole(t, root, "Srch"+s, menuID(t, "user"))
	mixed := "MixedCase" + s
	createUser(t, root, mixed, "Pass-1", roleID)
	createUser(t, root, "a_b"+s, "Pass-1", roleID)
	createUser(t, root, "axb"+s, "Pass-1", roleID)

	usernames := func(query string) []string {
		t.Helper()
		r := call(t, http.MethodGet, "/api/v1/users?current=1&pageSize=50&username="+url.QueryEscape(query), root, nil)
		expectStatus(t, "search "+query, r, http.StatusOK)
		var names []string
		for _, u := range list(t, r) {
			names = append(names, fmt.Sprint(u["username"]))
		}
		return names
	}

	for _, q := range []string{strings.ToLower(mixed), strings.ToUpper(mixed), "mixedcase" + s[:6]} {
		if got := usernames(q); !slices.Contains(got, mixed) {
			t.Errorf("search %q should find %q regardless of case, got %v", q, mixed, got)
		}
	}
	if got := usernames("a_b" + s); len(got) != 1 || got[0] != "a_b"+s {
		t.Errorf("_ must match literally: search a_b%s got %v", s, got)
	}
	if got := usernames("%" + s); len(got) != 0 {
		t.Errorf("%% must match literally: got %v", got)
	}

	roles := list(t, call(t, http.MethodGet, "/api/v1/roles?current=1&pageSize=50&name="+url.QueryEscape("ROLE SRCH"+s), root, nil))
	if len(roles) != 1 {
		t.Errorf("role name search should ignore case, got %d roles", len(roles))
	}
}

// Only root may set or reset another user's password, whatever menu.json
// grants: the check lives in the business layer, not in casbin.
func TestPasswordResetIsRootOnly(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()
	userPage := menuID(t, "user")

	// Grant the reset API through the user page, as a misconfigured menu.json
	// would, so casbin lets the request through to the business layer.
	res := schema.MenuResource{ID: "rp" + s[len(s)-10:], MenuID: userPage, Method: http.MethodPatch, Path: "/api/v1/users/{id}/reset-pwd"}
	if err := injector.DB.Create(&res).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { injector.DB.Delete(&schema.MenuResource{}, "id = ?", res.ID) })

	adminRole := createRole(t, root, "rpa"+s, userPage, childMenuID(t, "user", "edit"))
	lowRole := createRole(t, root, "rpl"+s, userPage)
	createUser(t, root, "rpa"+s, "Pass-1", adminRole)
	targetID := createUser(t, root, "rpl"+s, "Pass-1", lowRole)
	admin := mustLogin(t, "rpa"+s, "Pass-1")
	eventually(t, "admin can edit users", http.MethodGet, "/api/v1/users", admin, http.StatusOK)

	rootOnly := func(what string, r response) {
		t.Helper()
		expectStatus(t, what, r, http.StatusForbidden)
		if r.Error == nil || r.Error.ID != "com.rbac.root-only" {
			t.Fatalf("%s: expected com.rbac.root-only, got %+v", what, r.Error)
		}
	}

	// Wait until casbin has loaded the extra grant, then the business layer
	// must still refuse.
	deadline := time.Now().Add(15 * time.Second)
	var r response
	for time.Now().Before(deadline) {
		r = call(t, http.MethodPatch, "/api/v1/users/"+targetID+"/reset-pwd", admin, nil)
		if r.Error != nil && r.Error.ID != "com.casbin.denied" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	rootOnly("admin resets a password despite a casbin grant", r)

	rootOnly("admin sets a password while editing a user",
		call(t, http.MethodPut, "/api/v1/users/"+targetID, admin, userForm("rpl"+s, "New-Pass-9", "activated", lowRole)))
	expectStatus(t, "admin edits a user without touching the password",
		call(t, http.MethodPut, "/api/v1/users/"+targetID, admin, userForm("rpl"+s, "", "activated", lowRole)), http.StatusOK)
	mustLogin(t, "rpl"+s, "Pass-1")

	// Root may.
	expectStatus(t, "root sets a password", call(t, http.MethodPut, "/api/v1/users/"+targetID, root,
		userForm("rpl"+s, "New-Pass-9", "activated", lowRole)), http.StatusOK)
	mustLogin(t, "rpl"+s, "New-Pass-9")
}

// Single-instance design: role changes rebuild the policy as soon as they
// commit, so they apply to the very next request, with no polling delay.
func TestPermissionChangesApplyImmediately(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()
	userPage, loggerPage := menuID(t, "user"), menuID(t, "logger")
	roleID := createRole(t, root, "imm"+s, userPage)
	createUser(t, root, "imm"+s, "Pass-1", roleID)
	token := mustLogin(t, "imm"+s, "Pass-1")

	expectStatus(t, "granted at once", call(t, http.MethodGet, "/api/v1/users", token, nil), http.StatusOK)
	expectStatus(t, "not yet granted", call(t, http.MethodGet, "/api/v1/loggers", token, nil), http.StatusForbidden)

	expectStatus(t, "grant loggers", call(t, http.MethodPut, "/api/v1/roles/"+roleID, root, roleForm("imm"+s, userPage, loggerPage)), http.StatusOK)
	expectStatus(t, "grant applies to the next request", call(t, http.MethodGet, "/api/v1/loggers", token, nil), http.StatusOK)

	expectStatus(t, "revoke everything", call(t, http.MethodPut, "/api/v1/roles/"+roleID, root, roleForm("imm"+s)), http.StatusOK)
	expectStatus(t, "revoke applies to the next request", call(t, http.MethodGet, "/api/v1/users", token, nil), http.StatusForbidden)
}

// Menu resources become casbin policy lines. A path with a line break or a
// comma could add rules or break policy parsing.
func TestMenuResourceInjectionIsRejected(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	withMenuWrites(t)
	s := unique()

	form := menuForm("inj"+s, "")
	form["resources"] = []map[string]string{{"method": "GET", "path": "/x\np, anyrole, /api/v1/*, DELETE"}}
	expectStatus(t, "injection through the API", call(t, http.MethodPost, "/api/v1/menus", root, form), http.StatusBadRequest)

	// A malicious row written straight to the database is skipped when the
	// policy is built: the role keeps its legitimate grant and gains nothing.
	userPage := menuID(t, "user")
	bad := schema.MenuResource{ID: "inj" + s[len(s)-10:], MenuID: userPage, Method: "GET", Path: "/x\np, %s, /api/v1/loggers, GET"}
	roleID := createRole(t, root, "inj"+s, userPage)
	bad.Path = strings.Replace(bad.Path, "%s", roleID, 1)
	if err := injector.DB.Create(&bad).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { injector.DB.Delete(&schema.MenuResource{}, "id = ?", bad.ID) })
	if err := injector.M.RBAC.Casbinx.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	createUser(t, root, "inj"+s, "Pass-1", roleID)
	token := mustLogin(t, "inj"+s, "Pass-1")
	expectStatus(t, "legitimate grant still works", call(t, http.MethodGet, "/api/v1/users", token, nil), http.StatusOK)
	expectStatus(t, "injected rule has no effect", call(t, http.MethodGet, "/api/v1/loggers", token, nil), http.StatusForbidden)
}

// Menu create, read, update, list and delete, with menu writes enabled.
func TestMenuCRUD(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	withMenuWrites(t)
	s := unique()

	form := map[string]any{
		"code": "crud" + s, "name": "Menu management", "description": "Menu management",
		"sequence": 9, "type": "page", "path": "/system/crud" + s, "properties": `{"icon":"menu"}`,
		"status":    "enabled",
		"resources": []map[string]string{{"method": "GET", "path": "/api/v1/crud" + s}},
	}
	created := call(t, http.MethodPost, "/api/v1/menus", root, form)
	expectStatus(t, "create menu", created, http.StatusOK)
	id := dataField(t, created, "id")
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/menus/"+id, root, nil) })

	var menu map[string]any
	_ = json.Unmarshal(created.Data, &menu)
	for _, k := range []string{"code", "name", "description", "type", "path", "properties", "status"} {
		if fmt.Sprint(menu[k]) != fmt.Sprint(form[k]) {
			t.Errorf("created menu %s = %v, want %v", k, menu[k], form[k])
		}
	}
	if fmt.Sprint(menu["sequence"]) != "9" {
		t.Errorf("created menu sequence = %v, want 9", menu["sequence"])
	}

	found := false
	for _, m := range list(t, call(t, http.MethodGet, "/api/v1/menus", root, nil)) {
		if m["id"] == id {
			found = true
		}
	}
	if !found {
		t.Fatal("created menu missing from the list")
	}

	form["name"], form["status"] = "Menu management 1", "disabled"
	expectStatus(t, "update menu", call(t, http.MethodPut, "/api/v1/menus/"+id, root, form), http.StatusOK)
	got := call(t, http.MethodGet, "/api/v1/menus/"+id, root, nil)
	expectStatus(t, "get menu", got, http.StatusOK)
	if dataField(t, got, "name") != "Menu management 1" || dataField(t, got, "status") != "disabled" {
		t.Fatalf("update not applied: %s", got.Data)
	}

	expectStatus(t, "delete menu", call(t, http.MethodDelete, "/api/v1/menus/"+id, root, nil), http.StatusOK)
	expectStatus(t, "deleted menu is gone", call(t, http.MethodGet, "/api/v1/menus/"+id, root, nil), http.StatusNotFound)
}

// Concurrent creates of one username used to pass the "already exists" check
// together and insert duplicates; the unique index now lets exactly one win.
func TestConcurrentDuplicateCreates(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	s := unique()
	roleID := createRole(t, root, "dup"+s, menuID(t, "user"))

	const n = 10
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			codes <- call(t, http.MethodPost, "/api/v1/users", root, userForm("dup"+s, "Pass-1", "activated", roleID)).status
		})
	}
	wg.Wait()
	close(codes)
	created, conflicts := 0, 0
	for c := range codes {
		switch c {
		case http.StatusOK:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	if created != 1 || conflicts != n-1 {
		t.Fatalf("created=%d conflicts=%d, want 1 and %d", created, conflicts, n-1)
	}
	var count int64
	injector.DB.Model(new(schema.User)).Where("username = ?", "dup"+s).Count(&count)
	if count != 1 {
		t.Fatalf("%d users named dup%s in the database, want 1", count, s)
	}
	var u schema.User
	injector.DB.Where("username = ?", "dup"+s).First(&u)
	t.Cleanup(func() { call(t, http.MethodDelete, "/api/v1/users/"+u.ID, root, nil) })
}

// Time bounds used to apply only when both were given, as raw strings: a
// single bound was silently ignored and invalid values answered 200.
func TestLoggerTimeRangeValidation(t *testing.T) {
	root := mustLogin(t, "admin", rootPassword)
	q := func(params string) int {
		return call(t, http.MethodGet, "/api/v1/loggers?current=1&pageSize=5&"+params, root, nil).status
	}
	for _, bad := range []string{"startTime=yesterday", "endTime=2026-13-01", "startTime=2026-10-02&endTime=2026-10-01"} {
		if code := q(bad); code != http.StatusBadRequest {
			t.Errorf("%s: got HTTP %d, want 400", bad, code)
		}
	}
	for _, good := range []string{"startTime=2020-01-01", "endTime=2099-01-01+00%3A00%3A00", "startTime=2020-01-01T00%3A00%3A00Z&endTime=2099-01-01"} {
		if code := q(good); code != http.StatusOK {
			t.Errorf("%s: got HTTP %d, want 200", good, code)
		}
	}
}
