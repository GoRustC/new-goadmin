package e2e

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LyricTian/gin-admin/v10/internal/config"
	"github.com/LyricTian/gin-admin/v10/internal/mods/rbac/schema"
	"github.com/LyricTian/gin-admin/v10/pkg/crypto/hash"
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

	// Token refresh issues a working token.
	ref := call(t, http.MethodPost, "/api/v1/current/refresh-token", token, nil)
	expectStatus(t, "refresh token", ref, http.StatusOK)
	newToken := dataField(t, ref, "access_token")
	expectStatus(t, "refreshed token works", call(t, http.MethodGet, "/api/v1/current/user", newToken, nil), http.StatusOK)

	// Password change.
	expectStatus(t, "wrong old password", call(t, http.MethodPut, "/api/v1/current/password", token, map[string]any{
		"old_password": hash.MD5String("wrong"), "new_password": hash.MD5String("New-Pass-2"),
	}), http.StatusBadRequest)
	expectStatus(t, "change password", call(t, http.MethodPut, "/api/v1/current/password", token, map[string]any{
		"old_password": hash.MD5String("Old-Pass-1"), "new_password": hash.MD5String("New-Pass-2"),
	}), http.StatusOK)
	expectStatus(t, "old password rejected", login(t, username, "Old-Pass-1"), http.StatusBadRequest)
	mustLogin(t, username, "New-Pass-2")
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
	expectStatus(t, "duplicate username", call(t, http.MethodPost, "/api/v1/users", root, userForm(username, "x", "activated")), http.StatusBadRequest)

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
	}), http.StatusBadRequest)

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
	for round := 0; round < rounds; round++ {
		id, _ := newCaptcha(t)
		body := map[string]string{"username": "x", "password": "x", "captcha_id": id, "captcha_code": "0000"}
		done := make(chan struct{})
		for i := 0; i < concurrency; i++ {
			go func() {
				defer func() { done <- struct{}{} }()
				b, _ := json.Marshal(body)
				res, err := http.Post(server.URL+"/api/v1/login", "application/json", bytes.NewReader(b))
				if err == nil {
					res.Body.Close()
				}
			}()
		}
		for i := 0; i < concurrency; i++ {
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
