// Package gormx opens gorm databases (MySQL, PostgreSQL, SQLite) with
// connection pooling and SQL logging through zap.
package gormx

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	sdmysql "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// Config configures New. MaxLifetime and MaxIdleTime are in seconds.
type Config struct {
	Debug        bool
	PrepareStmt  bool
	DBType       string // mysql/postgres/sqlite3
	DSN          string
	MaxLifetime  int
	MaxIdleTime  int
	MaxOpenConns int
	MaxIdleConns int
	TablePrefix  string
	// SlowThreshold logs queries slower than this as warnings; 0 uses 200ms.
	SlowThreshold time.Duration
	// DisableSQLLog turns off SQL logging. Set it for a connection used by a
	// log sink, where logging a failed write would produce another write.
	DisableSQLLog bool
}

// newSQLLogger logs every statement in debug mode and, otherwise, SQL errors
// and slow queries as warnings through zap. Before, non-debug mode discarded
// both, leaving nothing to investigate database problems with.
func newSQLLogger(cfg Config) logger.Interface {
	if cfg.DisableSQLLog {
		return logger.Discard
	}
	if cfg.Debug {
		return logger.Default
	}
	slow := cfg.SlowThreshold
	if slow <= 0 {
		slow = 200 * time.Millisecond
	}
	return logger.New(zapWriter{}, logger.Config{
		SlowThreshold:             slow,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
}

type zapWriter struct{}

func (zapWriter) Printf(format string, args ...any) {
	zap.L().Warn(fmt.Sprintf(format, args...), zap.String("tag", "sql"))
}

// ensureSQLiteDir creates the directory of a SQLite database file.
func ensureSQLiteDir(dsn string) error {
	path, _, _ := strings.Cut(strings.TrimPrefix(dsn, "file:"), "?")
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create sqlite directory %s: %w", dir, err)
	}
	return nil
}

// New opens the database described by cfg. For MySQL it first creates the
// database if it does not exist; for SQLite it creates the file's directory.
func New(cfg Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch strings.ToLower(cfg.DBType) {
	case "mysql":
		if err := createDatabaseWithMySQL(cfg.DSN); err != nil {
			return nil, err
		}
		dialector = mysql.Open(cfg.DSN)
	case "postgres":
		dialector = postgres.Open(cfg.DSN)
	case "sqlite3":
		if err := ensureSQLiteDir(cfg.DSN); err != nil {
			return nil, err
		}
		dialector = sqlite.Open(sqliteDSN(cfg.DSN))
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.DBType)
	}

	ormCfg := &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix:   cfg.TablePrefix,
			SingularTable: true,
		},
		Logger:      newSQLLogger(cfg),
		PrepareStmt: cfg.PrepareStmt,
		// Map driver errors such as unique violations to gorm.ErrDuplicatedKey,
		// so callers can tell a conflict from a failure on every database.
		TranslateError: true,
	}

	db, err := gorm.Open(dialector, ormCfg)
	if err != nil {
		return nil, err
	}

	if cfg.Debug {
		db = db.Debug()
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.MaxLifetime) * time.Second)
	sqlDB.SetConnMaxIdleTime(time.Duration(cfg.MaxIdleTime) * time.Second)

	return db, nil
}

// sqliteBusyTimeoutMs is how long a connection waits for a lock held by
// another connection before failing with SQLITE_BUSY.
const sqliteBusyTimeoutMs = 5000

// sqliteDSN makes concurrent writers wait instead of failing. SQLite allows
// one writer at a time, and the database log hook writes to the same file
// through its own connection. Two settings are needed:
//   - busy_timeout: wait for a lock instead of failing immediately;
//   - _txlock=immediate: take the write lock at BEGIN. A deferred transaction
//     that reads first and writes later fails at once when another connection
//     holds the write lock, because SQLite skips the busy handler there to
//     avoid deadlocks.
//
// Settings already present in the DSN are kept.
func sqliteDSN(dsn string) string {
	var params []string
	if !strings.Contains(dsn, "busy_timeout") {
		params = append(params, fmt.Sprintf("_pragma=busy_timeout(%d)", sqliteBusyTimeoutMs))
	}
	if !strings.Contains(dsn, "_txlock") {
		params = append(params, "_txlock=immediate")
	}
	if len(params) == 0 {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + strings.Join(params, "&")
}

func createDatabaseWithMySQL(dsn string) error {
	cfg, err := sdmysql.ParseDSN(dsn)
	if err != nil {
		return err
	}

	// Connect without selecting the database, keeping every other setting of
	// the original DSN: network (tcp or unix socket), TLS, timeouts and so on.
	dbName := cfg.DBName
	server := cfg.Clone()
	server.DBName = ""
	db, err := sql.Open("mysql", server.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()

	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` DEFAULT CHARACTER SET = `utf8mb4`;", strings.ReplaceAll(dbName, "`", "``"))
	_, err = db.Exec(query)
	return err
}
