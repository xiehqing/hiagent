package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
	"github.com/xiehqing/hiagent/internal/config"
)

var pragmas = map[string]string{
	"foreign_keys":  "ON",
	"journal_mode":  "WAL",
	"page_size":     "4096",
	"cache_size":    "-8000",
	"synchronous":   "NORMAL",
	"secure_delete": "ON",
	"busy_timeout":  "30000",
}

//go:embed migrations/*.sql
var FS embed.FS

//go:embed migrations_mysql/*.sql
var mysqlFS embed.FS

func init() {
	goose.SetBaseFS(FS)

	if testing.Testing() {
		goose.SetLogger(goose.NopLogger())
	}
}

// Connect opens a SQLite database connection and runs migrations.
func Connect(ctx context.Context, dataDir string) (*sql.DB, error) {
	if dataDir == "" {
		return nil, fmt.Errorf("data.dir is not set")
	}
	dbPath := filepath.Join(dataDir, "crush.db")

	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}

	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := goose.SetDialect("sqlite3"); err != nil {
		slog.Error("Failed to set dialect", "error", err)
		return nil, fmt.Errorf("failed to set dialect: %w", err)
	}

	goose.SetBaseFS(FS)
	if err := goose.Up(db, "migrations"); err != nil {
		slog.Error("Failed to apply migrations", "error", err)
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return db, nil
}

// ConnectWithConfig opens the configured database backend.
//
// When no database configuration is provided, SQLite remains the default.
func ConnectWithConfig(ctx context.Context, cfg *config.Config) (*sql.DB, error) {
	if cfg == nil || cfg.Options == nil || cfg.Options.Database == nil || cfg.Options.Database.Driver == "" || cfg.Options.Database.Driver == "sqlite" {
		if cfg == nil || cfg.Options == nil {
			return nil, fmt.Errorf("config options are not set")
		}
		return Connect(ctx, cfg.Options.DataDirectory)
	}

	switch cfg.Options.Database.Driver {
	case "mysql":
		return ConnectMySQL(ctx, cfg.Options.Database.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.Options.Database.Driver)
	}
}

// ConnectMySQL opens a MySQL database connection and runs MySQL migrations.
func ConnectMySQL(ctx context.Context, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("mysql dsn is not set")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to mysql database: %w", err)
	}

	goose.SetBaseFS(mysqlFS)
	if err := goose.SetDialect("mysql"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set mysql dialect: %w", err)
	}
	if err := goose.Up(db, "migrations_mysql"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply mysql migrations: %w", err)
	}

	return db, nil
}
