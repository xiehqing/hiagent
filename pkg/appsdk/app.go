package appsdk

import (
	"charm.land/fantasy"
	"context"
	"database/sql"
	"fmt"
	"github.com/pkg/errors"
	"github.com/xiehqing/hiagent/internal/agent"
	"github.com/xiehqing/hiagent/internal/app"
	"github.com/xiehqing/hiagent/internal/config"
	"github.com/xiehqing/hiagent/internal/db"
	"github.com/xiehqing/hiagent/internal/message"
	"github.com/xiehqing/hiagent/internal/pubsub"
	"github.com/xiehqing/hiagent/internal/session"
	"log/slog"
)

type App struct {
	AppInstance *app.App
}

// setDatabaseOptions sets the database options in the config.
func handleDatabaseConnection(ctx context.Context, cfg *config.Config, dc *DatabaseConfig) (*sql.DB, error) {
	if cfg.Options == nil {
		cfg.Options = &config.Options{}
	}
	cfg.Options.Database = &config.DatabaseOptions{
		Driver: string(dc.Driver),
		DSN:    dc.DSN,
	}
	conn, err := db.ConnectWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("sdk.New: failed to connect to database: %w", err)
	}
	return conn, nil
}

func New(ctx context.Context, opts ...Option) (*App, error) {
	o := &Options{
		cfg: AppConfig{
			SkipPermissionRequests:    true,
			Debug:                     false,
			DisableProviderAutoUpdate: true,
		},
	}
	for _, opt := range opts {
		opt(o)
	}
	if o.cfg.WorkDir == "" {
		return nil, fmt.Errorf("sdk.New: WorkDir is required (use sdk.WithWorkDir)")
	}
	if o.cfg.Database.Driver == "" {
		o.cfg.Database.Driver = DatabaseDriverSqlite
		if o.cfg.DataDir == "" {
			return nil, fmt.Errorf("sdk.New: DataDir is required for sqlite (use sdk.WithDataDir)")
		}
	}
	if o.cfg.Database.Driver == DatabaseDriverMysql {
		if o.cfg.Database.DSN == "" {
			return nil, fmt.Errorf("sdk.New: DSN is required for mysql (use sdk.WithDatabaseDSN)")
		}
	}

	cfg, err := config.Init(o.cfg.WorkDir, o.cfg.DataDir, o.cfg.Debug)
	if err != nil {
		return nil, fmt.Errorf("sdk.New: failed to initialize config: %w", err)
	}
	cfg.Overrides().SkipPermissionRequests = o.cfg.SkipPermissionRequests
	cfg.Config().Options.DisableProviderAutoUpdate = o.cfg.DisableProviderAutoUpdate
	if o.cfg.Database.Driver == DatabaseDriverSqlite {
		if err = createDotCrushDir(cfg.Config().Options.DataDirectory); err != nil {
			return nil, fmt.Errorf("sdk.New: failed to create data directory: %w", err)
		}
	}
	conn, err := handleDatabaseConnection(ctx, cfg.Config(), &o.cfg.Database)
	if err != nil {
		return nil, err
	}
	if o.cfg.SelectedModel != "" && o.cfg.SelectedProvider != "" {
		err = cfg.SetRuntimePreferredModel(o.cfg.SelectedProvider, o.cfg.SelectedModel)
		if err != nil {
			return nil, errors.WithMessage(err, "sdk.New: failed to set runtime preferred model")
		}
	}
	app, err := app.NewWithSystemPrompt(ctx, conn, cfg, o.cfg.AdditionalSystemPrompt)
	if err != nil {
		return nil, fmt.Errorf("sdk.New: failed to create app workspace: %w", err)
	}
	return &App{AppInstance: app}, nil
}

func (a *App) SubmitMessage(ctx context.Context, prompt string, continueSessionID string, useLast bool) (*fantasy.AgentResult, error) {
	if a.AppInstance.AgentCoordinator == nil {
		return nil, fmt.Errorf("sdk.SubmitMessage: agent coordinator is nil")
	}
	session, err := a.resolveSession(ctx, continueSessionID, useLast)

	if err != nil {
		return nil, fmt.Errorf("sdk.SubmitMessage: failed to create session for sdk mode: %w", err)
	}

	if continueSessionID != "" || useLast {
		slog.Info("sdk.SubmitMessage: continuing session for sdk run", "session_id", session.ID)
	} else {
		slog.Info("sdk.SubmitMessage: created session for sdk run", "session_id", session.ID)
	}
	return a.AppInstance.AgentCoordinator.Run(ctx, session.ID, prompt)
}

func (a *App) resolveSession(ctx context.Context, continueSessionID string, useLast bool) (session.Session, error) {
	switch {
	case continueSessionID != "":
		if a.AppInstance.Sessions.IsAgentToolSession(continueSessionID) {
			return session.Session{}, fmt.Errorf("cannot continue an agent tool session: %s", continueSessionID)
		}
		sess, err := a.AppInstance.Sessions.Get(ctx, continueSessionID)
		if err != nil {
			return session.Session{}, fmt.Errorf("session not found: %s", continueSessionID)
		}
		if sess.ParentSessionID != "" {
			return session.Session{}, fmt.Errorf("cannot continue a child session: %s", continueSessionID)
		}
		return sess, nil

	case useLast:
		sess, err := a.AppInstance.Sessions.GetLast(ctx)
		if err != nil {
			return session.Session{}, fmt.Errorf("no sessions found to continue")
		}
		return sess, nil

	default:
		return a.AppInstance.Sessions.Create(ctx, agent.DefaultSessionName)
	}
}

// SubscribeMessage subscribes to the message channel.
func (a *App) SubscribeMessage(ctx context.Context) <-chan pubsub.Event[message.Message] {
	return a.AppInstance.Messages.Subscribe(ctx)
}
