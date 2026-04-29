package appsdk

import (
	"fmt"
	"github.com/xiehqing/hiagent/internal/history"
	"github.com/xiehqing/hiagent/internal/message"
	"os"
	"path/filepath"
)

type DatabaseDriver string

const (
	DatabaseDriverSqlite DatabaseDriver = "sqlite"
	DatabaseDriverMysql  DatabaseDriver = "mysql"
)

type AppConfig struct {
	WorkDir                   string         `json:"workDir"`
	DataDir                   string         `json:"dataDir"`
	Database                  DatabaseConfig `json:"database"`
	SkipPermissionRequests    bool           `json:"skipPermissionRequests"`
	DisableProviderAutoUpdate bool           `json:"disableProviderAutoUpdate"`
	AdditionalSystemPrompt    string         `json:"additionalSystemPrompt"`
	Debug                     bool           `json:"debug"`
	SelectedProvider          string         `json:"selectedProvider"`
	SelectedModel             string         `json:"selectedModel"`
}

type DatabaseConfig struct {
	Driver DatabaseDriver `json:"driver"`
	DSN    string         `json:"dsn"`
}

type Options struct {
	cfg AppConfig
}

// Option is a functional option for Engine creation.
type Option func(*Options)

func WithSkipPermissionRequests(skipPermissionRequests bool) Option {
	return func(o *Options) {
		o.cfg.SkipPermissionRequests = skipPermissionRequests
	}
}

func WithDebug(debug bool) Option {
	return func(o *Options) {
		o.cfg.Debug = debug
	}
}

func WithDatabaseDriver(driver DatabaseDriver) Option {
	return func(o *Options) {
		o.cfg.Database.Driver = driver
	}
}

func WithDatabaseDSN(dsn string) Option {
	return func(o *Options) {
		o.cfg.Database.DSN = dsn
	}
}

func WithWorkDir(workDir string) Option {
	return func(o *Options) {
		o.cfg.WorkDir = workDir
	}
}

func WithDataDir(dataDir string) Option {
	return func(o *Options) {
		o.cfg.DataDir = dataDir
	}
}

func WithSelectedProvider(selectedProvider string) Option {
	return func(o *Options) {
		o.cfg.SelectedProvider = selectedProvider
	}
}

func WithSelectedModel(selectedModel string) Option {
	return func(o *Options) {
		o.cfg.SelectedModel = selectedModel
	}
}

func WithDisableProviderAutoUpdate(disableProviderAutoUpdate bool) Option {
	return func(o *Options) {
		o.cfg.DisableProviderAutoUpdate = disableProviderAutoUpdate
	}
}

func WithAdditionalSystemPrompt(additionalSystemPrompt string) Option {
	return func(o *Options) {
		o.cfg.AdditionalSystemPrompt = additionalSystemPrompt
	}
}

// createDotCrushDir creates the .crush directory in th
func createDotCrushDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %q %w", dir, err)
	}

	gitIgnorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitIgnorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitIgnorePath, []byte("*\n"), 0o644); err != nil {
			return fmt.Errorf("failed to create .gitignore file: %q %w", gitIgnorePath, err)
		}
	}
	return nil
}

// DataMessage 数据消息
type DataMessage struct {
	ID               string                `json:"id"`
	Role             message.MessageRole   `json:"role"`
	SessionID        string                `json:"session_id"`
	Parts            []message.ContentPart `json:"parts"`
	Model            string                `json:"model"`
	Provider         string                `json:"provider"`
	CreatedAt        int64                 `json:"createdAt"`
	UpdatedAt        int64                 `json:"updatedAt"`
	IsSummaryMessage bool                  `json:"is_summary_message"`
	Files            []history.File        `json:"files,omitempty"`
}
