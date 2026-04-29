package config

import (
	"cmp"
	"database/sql"
	"fmt"
	"github.com/xiehqing/hiagent/internal/csync"
	"github.com/xiehqing/hiagent/internal/env"
	"github.com/xiehqing/hiagent/internal/fsext"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

func InitNew(workingDir string, dataDir string, db *sql.DB, debug bool) (*ConfigStore, error) {
	store, err := LoadNew(workingDir, dataDir, db, debug)
	if err != nil {
		return nil, err
	}
	return store, nil
}

// LoadNew loads the configuration from the default paths and returns a
// ConfigStore that owns both the pure-data Config and all runtime state.
func LoadNew(workingDir, dataDir string, db *sql.DB, debug bool) (*ConfigStore, error) {
	configPaths := lookupConfigs(workingDir)
	cfg, loadedPaths, err := loadFromConfigPaths(configPaths)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from paths %v: %w", configPaths, err)
	}
	cfg.setNewDefaults(workingDir)

	cfg.setDataDir(workingDir, dataDir)
	store := &ConfigStore{
		conn:           db,
		config:         cfg,
		workingDir:     workingDir,
		globalDataPath: GlobalConfigData(),
		workspacePath:  filepath.Join(cfg.Options.DataDirectory, fmt.Sprintf("%s.json", appName)),
		loadedPaths:    loadedPaths,
	}

	if debug {
		cfg.Options.Debug = true
	}
	// Load workspace config last so it has highest priority.
	if wsData, err := os.ReadFile(store.workspacePath); err == nil && len(wsData) > 0 {
		merged, mergeErr := loadFromBytes(append([][]byte{mustMarshalConfig(cfg)}, wsData))
		if mergeErr == nil {
			// Preserve defaults that setDefaults already applied.
			dataDir := cfg.Options.DataDirectory
			*cfg = *merged
			cfg.setDefaults(workingDir, dataDir)
			store.config = cfg
			store.loadedPaths = append(store.loadedPaths, store.workspacePath)
		}
	}

	// Validate hooks after all config merging is complete so workspace
	// hooks also get their matcher regexes compiled.
	if err := cfg.ValidateHooks(); err != nil {
		return nil, fmt.Errorf("invalid hook configuration: %w", err)
	}

	if !isInsideWorktree() {
		const depth = 2
		const items = 100
		slog.Warn("No git repository detected in working directory, will limit file walk operations", "depth", depth, "items", items)
		assignIfNil(&cfg.Tools.Ls.MaxDepth, depth)
		assignIfNil(&cfg.Tools.Ls.MaxItems, items)
		assignIfNil(&cfg.Options.TUI.Completions.MaxDepth, depth)
		assignIfNil(&cfg.Options.TUI.Completions.MaxItems, items)
	}

	if isAppleTerminal() {
		slog.Warn("Detected Apple Terminal, enabling transparent mode")
		assignIfNil(&cfg.Options.TUI.Transparent, true)
	}

	// Load known providers, this loads the config from catwalk
	providers, err := AllKnownProviders(cfg, db)
	if err != nil {
		return nil, err
	}
	store.knownProviders = providers

	env := env.New()
	// Configure providers
	valueResolver := NewShellVariableResolver(env)
	store.resolver = valueResolver

	// Disable auto-reload during initial load to prevent nested calls from
	// config-modifying operations inside configureProviders.
	store.autoReloadDisabled = true
	defer func() { store.autoReloadDisabled = false }()

	if err := cfg.configureProviders(store, env, valueResolver, store.knownProviders); err != nil {
		return nil, fmt.Errorf("failed to configure providers: %w", err)
	}

	if !cfg.IsConfigured() {
		slog.Warn("No providers configured")
		return store, nil
	}

	if err := configureSelectedModels(store, store.knownProviders, true); err != nil {
		return nil, fmt.Errorf("failed to configure selected models: %w", err)
	}
	store.SetupAgents()

	// Capture initial staleness snapshot
	store.captureStalenessSnapshot(loadedPaths)

	return store, nil
}

func DefaultDataDir(workingDir, dataDir string) string {
	var d string
	if dataDir != "" {
		d = dataDir
	}
	if path, ok := fsext.LookupClosest(workingDir, defaultDataDirectory); ok {
		d = path
	} else {
		d = filepath.Join(workingDir, defaultDataDirectory)
	}
	if d != "" {
		os.MkdirAll(d, 0755)
	}
	return d
}

// setDataDir 设置数据目录
func (c *Config) setDataDir(workingDir, dataDir string) {
	if dataDir != "" {
		c.Options.DataDirectory = dataDir
	} else if c.Options.DataDirectory == "" {
		if path, ok := fsext.LookupClosest(workingDir, defaultDataDirectory); ok {
			c.Options.DataDirectory = path
		} else {
			c.Options.DataDirectory = filepath.Join(workingDir, defaultDataDirectory)
		}
	}
}

func (c *Config) GetGlobalSkillPaths() []string {
	return GlobalSkillsDirs()
}

func (c *Config) GetProjectSkillPaths(workingDir string) []string {
	return ProjectSkillsDir(workingDir)
}

func (c *Config) setNewDefaults(workingDir string) {
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}

	if c.Providers == nil {
		c.Providers = csync.NewMap[string, ProviderConfig]()
	}
	if c.Models == nil {
		c.Models = make(map[SelectedModelType]SelectedModel)
	}
	if c.RecentModels == nil {
		c.RecentModels = make(map[SelectedModelType][]SelectedModel)
	}
	if c.MCP == nil {
		c.MCP = make(map[string]MCPConfig)
	}
	if c.LSP == nil {
		c.LSP = make(map[string]LSPConfig)
	}

	// Apply defaults to LSP configurations
	c.applyLSPDefaults()

	// Add the default context paths if they are not already present
	c.Options.ContextPaths = append(defaultContextPaths, c.Options.ContextPaths...)
	slices.Sort(c.Options.ContextPaths)
	c.Options.ContextPaths = slices.Compact(c.Options.ContextPaths)

	// Add the default skills directories if not already present.
	for _, dir := range GlobalSkillsDirs() {
		if !slices.Contains(c.Options.SkillsPaths, dir) {
			c.Options.SkillsPaths = append(c.Options.SkillsPaths, dir)
		}
	}

	// Project specific skills dirs.
	c.Options.SkillsPaths = append(c.Options.SkillsPaths, ProjectSkillsDir(workingDir)...)

	if str, ok := os.LookupEnv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE"); ok {
		c.Options.DisableProviderAutoUpdate, _ = strconv.ParseBool(str)
	}

	if str, ok := os.LookupEnv("CRUSH_DISABLE_DEFAULT_PROVIDERS"); ok {
		c.Options.DisableDefaultProviders, _ = strconv.ParseBool(str)
	}

	if c.Options.Attribution == nil {
		c.Options.Attribution = &Attribution{
			TrailerStyle:  TrailerStyleAssistedBy,
			GeneratedWith: true,
		}
	} else if c.Options.Attribution.TrailerStyle == "" {
		// Migrate deprecated co_authored_by or apply default
		if c.Options.Attribution.CoAuthoredBy != nil {
			if *c.Options.Attribution.CoAuthoredBy {
				c.Options.Attribution.TrailerStyle = TrailerStyleCoAuthoredBy
			} else {
				c.Options.Attribution.TrailerStyle = TrailerStyleNone
			}
		} else {
			c.Options.Attribution.TrailerStyle = TrailerStyleAssistedBy
		}
	}
	c.Options.InitializeAs = cmp.Or(c.Options.InitializeAs, defaultInitializeAs)
}
