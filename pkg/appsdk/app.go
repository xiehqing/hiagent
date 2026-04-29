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
	"github.com/xiehqing/hiagent/internal/history"
	"github.com/xiehqing/hiagent/internal/message"
	"github.com/xiehqing/hiagent/internal/pubsub"
	"github.com/xiehqing/hiagent/internal/session"
	"log/slog"
	"os"
)

type App struct {
	AppInstance *app.App
}

// setDatabaseOptions sets the database options in the config.
func handleDatabaseConnection(ctx context.Context, dataDir string, dc *DatabaseConfig) (*sql.DB, error) {
	conn, err := db.ConnectWithOption(ctx, string(dc.Driver), dataDir, dc.DSN)
	if err != nil {
		return nil, fmt.Errorf("sdk.New: failed to connect to database: %w", err)
	}
	return conn, nil
}

func applyRuntimeDatabaseOverride(store *config.ConfigStore, dc *DatabaseConfig) {
	if store == nil || dc == nil {
		return
	}
	store.Overrides().Database = &config.DatabaseOptions{
		Driver: string(dc.Driver),
		DSN:    dc.DSN,
	}
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
	o.cfg.DataDir = config.DefaultDataDir(o.cfg.WorkDir, o.cfg.DataDir)
	conn, err := handleDatabaseConnection(ctx, o.cfg.DataDir, &o.cfg.Database)
	if err != nil {
		return nil, err
	}
	cfg, err := config.InitNew(o.cfg.WorkDir, o.cfg.DataDir, conn, o.cfg.Debug)
	if err != nil {
		return nil, fmt.Errorf("sdk.New: failed to initialize config: %w", err)
	}
	cfg.Overrides().SkipPermissionRequests = o.cfg.SkipPermissionRequests
	cfg.Config().Options.DisableProviderAutoUpdate = o.cfg.DisableProviderAutoUpdate
	applyRuntimeDatabaseOverride(cfg, &o.cfg.Database)
	if o.cfg.Database.Driver == DatabaseDriverSqlite {
		if err = createDotCrushDir(cfg.Config().Options.DataDirectory); err != nil {
			return nil, fmt.Errorf("sdk.New: failed to create data directory: %w", err)
		}
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
			return session.Session{}, fmt.Errorf("sdk.resolveSession: cannot continue an agent tool session: %s", continueSessionID)
		}
		sess, err := a.AppInstance.Sessions.Get(ctx, continueSessionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				slog.Info("sdk.resolveSession: session not found, creating new session", "session_id", continueSessionID)
				return a.AppInstance.Sessions.CreateSession(ctx, agent.DefaultSessionName, continueSessionID)
			}
			return session.Session{}, fmt.Errorf("sdk.resolveSession: session not found: %s", continueSessionID)
		}
		if sess.ParentSessionID != "" {
			return session.Session{}, fmt.Errorf("sdk.resolveSession: cannot continue a child session: %s", continueSessionID)
		}
		return sess, nil

	case useLast:
		sess, err := a.AppInstance.Sessions.GetLast(ctx)
		if err != nil {
			return session.Session{}, fmt.Errorf("sdk.resolveSession: no sessions found to continue")
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

// SessionFiles 获取会话文件
func (a *App) SessionFiles(ctx context.Context, sessionID string) ([]history.File, error) {
	files, err := a.AppInstance.History.ListLatestSessionFiles(ctx, sessionID)
	if err != nil {
		slog.Error("sdk.SessionFiles: failed to list session files", "session_id", sessionID, "err", err)
		return nil, fmt.Errorf("sdk.SessionFiles: failed to list session files: %w", err)
	}
	for i := 0; i < len(files); i++ {
		file := files[i]
		if file.Content == "" && file.Path != "" {
			cd, _ := os.ReadFile(file.Path)
			files[i].Content = string(cd)
		}
	}
	return files, nil
}

// SessionReadFiles 获取会话读取的文件
func (a *App) SessionReadFiles(ctx context.Context, sessionID string) ([]string, error) {
	return a.AppInstance.FileTracker.ListReadFiles(ctx, sessionID)
}

// DeleteSession 删除会话
func (a *App) DeleteSession(ctx context.Context, sessionID string) error {
	return a.AppInstance.Sessions.Delete(ctx, sessionID)
}

func (a *App) Sessions(ctx context.Context) ([]session.Session, error) {
	return a.AppInstance.Sessions.List(ctx)
}

func (a *App) Session(ctx context.Context, sessionID string) (session.Session, error) {
	return a.AppInstance.Sessions.Get(ctx, sessionID)
}

func (a *App) SessionByIDs(ctx context.Context, sessionIDs []string) ([]session.Session, error) {
	return a.AppInstance.Sessions.ListByIDs(ctx, sessionIDs)
}

// SessionMessages 获取会话消息
func (a *App) SessionMessages(ctx context.Context, sessionID string) ([]DataMessage, error) {
	messages, err := a.AppInstance.Messages.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sdk.Sessions: failed to list messages: %w", err)
	}
	files, err := a.SessionFiles(ctx, sessionID)
	if err != nil {
		return nil, errors.WithMessage(err, "sdk.Sessions: failed to list session files")
	}
	mergeMessages := a.mergeMessages(messages)
	messageList := make([]DataMessage, 0)
	for i, msg := range mergeMessages {
		dm := DataMessage{
			ID:               msg.ID,
			Role:             msg.Role,
			SessionID:        msg.SessionID,
			Parts:            msg.Parts,
			Model:            msg.Model,
			Provider:         msg.Provider,
			CreatedAt:        msg.CreatedAt,
			UpdatedAt:        msg.UpdatedAt,
			IsSummaryMessage: msg.IsSummaryMessage,
		}
		if i == len(mergeMessages)-1 {
			dm.Files = files
		}
		messageList = append(messageList, dm)
	}
	return messageList, nil
}

func (a *App) mergeMessages(messages []message.Message) []message.Message {
	var handleMessages = make([]message.Message, 0)
	currentMsgRole := ""
	for _, msg := range messages {
		msgRole := ""
		if msg.Role == message.User {
			msgRole = "user"
		} else {
			msgRole = "assistant"
		}
		if currentMsgRole == "" || currentMsgRole != msgRole {
			handleMessages = append(handleMessages, msg)
			currentMsgRole = msgRole
		} else {
			handleMessages[len(handleMessages)-1].Parts = append(handleMessages[len(handleMessages)-1].Parts, msg.Parts...)
		}
	}
	return handleMessages
}

// Shutdown shuts down the app.
func (a *App) Shutdown() {
	a.AppInstance.Shutdown()
}

// Providers 获取提供商
func (a *App) Providers() ([]config.ProviderItem, error) {
	providers, err := a.AppInstance.Store().Providers()
	if err != nil {
		return nil, fmt.Errorf("sdk.Providers: failed to get providers: %w", err)
	}
	return providers, nil
}
