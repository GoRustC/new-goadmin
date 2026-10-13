package test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoRustC/new-goadmin/internal/config"
	"github.com/GoRustC/new-goadmin/internal/wirex"
	"github.com/gavv/httpexpect/v2"
	"github.com/gin-gonic/gin"
)

const (
	baseAPI = "/api/v1"
)

var (
	app *gin.Engine
)

func init() {
	config.MustLoad("")

	// Use a temp dir as the work dir so the database and the generated casbin
	// policy file never land in the repo. Casbin resolves both its model and
	// policy files relative to WorkDir, so the model file is copied in.
	workDir, err := os.MkdirTemp("", "ginadmin-test-")
	if err != nil {
		panic(err)
	}
	model, err := os.ReadFile(filepath.Join("..", "configs", "rbac_model.conf"))
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "rbac_model.conf"), model, 0o600); err != nil {
		panic(err)
	}

	config.C.General.WorkDir = workDir
	config.C.Storage.DB.DSN = filepath.Join(workDir, "ginadmin.db")
	config.C.Storage.DB.AutoMigrate = true // tables must exist before casbin loads roles
	config.C.Middleware.Auth.SigningKey = "test-signing-key-0123456789abcdef"

	ctx := context.Background()
	injector, _, err := wirex.BuildInjector(ctx)
	if err != nil {
		panic(err)
	}

	if err := injector.M.Init(ctx); err != nil {
		panic(err)
	}

	app = gin.New()
	err = injector.M.RegisterRouters(ctx, app)
	if err != nil {
		panic(err)
	}
}

func tester(t *testing.T) *httpexpect.Expect {
	return httpexpect.WithConfig(httpexpect.Config{
		Client: &http.Client{
			Transport: httpexpect.NewBinder(app),
			Jar:       httpexpect.NewCookieJar(),
		},
		Reporter: httpexpect.NewAssertReporter(t),
		Printers: []httpexpect.Printer{
			httpexpect.NewDebugPrinter(t, true),
		},
	})
}
