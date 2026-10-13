package gormx

import (
	"strings"
	"testing"

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
