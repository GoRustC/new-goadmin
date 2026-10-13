package gormx

import (
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	sdmysql "github.com/go-sql-driver/mysql"
)

// createDatabaseWithMySQL connects without a database name; every other DSN
// setting must survive. It used to rebuild the DSN as user:pass@tcp(addr)/.
func TestServerDSNKeepsSettings(t *testing.T) {
	for _, dsn := range []string{
		"u:p@unix(/var/run/mysqld/mysqld.sock)/app?parseTime=true",
		"u:p@tcp(db:3306)/app?tls=true&timeout=5s",
	} {
		cfg, err := sdmysql.ParseDSN(dsn)
		if err != nil {
			t.Fatal(err)
		}
		server := cfg.Clone()
		server.DBName = ""
		got := server.FormatDSN()
		parsed, err := sdmysql.ParseDSN(got)
		if err != nil {
			t.Fatalf("%s: %v", got, err)
		}
		if parsed.Net != cfg.Net || parsed.Addr != cfg.Addr || parsed.TLSConfig != cfg.TLSConfig || parsed.Timeout != cfg.Timeout || parsed.DBName != "" {
			t.Errorf("%s -> %s lost settings", dsn, got)
		}
		if cfg.DBName != "app" || !strings.Contains(dsn, "/app") {
			t.Errorf("original config was modified: %+v", cfg)
		}
	}
}

func TestSQLiteDSN(t *testing.T) {
	tests := map[string]string{
		"data/app.db":                  "data/app.db?_pragma=busy_timeout(5000)&_txlock=immediate",
		"data/app.db?cache=shared":     "data/app.db?cache=shared&_pragma=busy_timeout(5000)&_txlock=immediate",
		"x.db?_pragma=busy_timeout(1)": "x.db?_pragma=busy_timeout(1)&_txlock=immediate",
		"x.db?_txlock=deferred":        "x.db?_txlock=deferred&_pragma=busy_timeout(5000)",
	}
	for in, want := range tests {
		if got := sqliteDSN(in); got != want {
			t.Errorf("sqliteDSN(%q) = %q, want %q", in, got, want)
		}
	}
}

// Outside debug mode, SQL errors used to be discarded entirely.
func TestSQLErrorsAreLoggedOutsideDebug(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	orig := zap.L()
	zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(func() { zap.ReplaceGlobals(orig) })

	db, err := New(Config{DBType: "sqlite3", DSN: filepath.Join(t.TempDir(), "x.db"), MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Exec("SELECT * FROM no_such_table").Error
	if logs.FilterMessageSnippet("no_such_table").Len() == 0 {
		t.Fatalf("SQL error was not logged; got %d entries", logs.Len())
	}

	// A connection used by a log sink must not log its own failures.
	quiet, err := New(Config{DBType: "sqlite3", DSN: filepath.Join(t.TempDir(), "y.db"), MaxOpenConns: 1, DisableSQLLog: true})
	if err != nil {
		t.Fatal(err)
	}
	before := logs.Len()
	_ = quiet.Exec("SELECT * FROM no_such_table").Error
	if logs.Len() != before {
		t.Fatal("DisableSQLLog connection logged a SQL error")
	}
}
