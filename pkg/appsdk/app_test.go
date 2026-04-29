package appsdk

import (
	"charm.land/fantasy"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xiehqing/hiagent/internal/message"
	"testing"
	"time"
)

func TestAppRun(t *testing.T) {
	var opts = []Option{
		WithDatabaseDriver("mysql"),
		WithDatabaseDSN("root:zorkdata.8888@tcp(192.168.12.34:3306)/crush_dev?charset=utf8mb4&parseTime=True&loc=Local"),
		WithWorkDir("C:\\projectData\\biddata\\ceshi\\bid\\extract"),
		WithSkipPermissionRequests(true),
		WithDebug(false),
		WithSelectedProvider("deepseek"),
		WithSelectedModel("deepseek-reasoner"),
	}
	app, err := New(context.Background(), opts...)
	if err != nil {
		t.Error(err)
		return
	}
	res, err := app.SubmitMessage(context.Background(), "浣犲ソ", "asdasda", false)
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(res.Response.Content)
}

type RunResponse struct {
	result *fantasy.AgentResult
	err    error
}

func TestNew(t *testing.T) {
	var opts = []Option{
		WithDatabaseDriver("mysql"),
		WithDatabaseDSN("root:zorkdata.8888@tcp(192.168.12.34:3306)/crush_dev?charset=utf8mb4&parseTime=True&loc=Local"),
		WithWorkDir("C:\\projectData\\biddata\\ceshi\\bid\\extract"),
		WithSkipPermissionRequests(true),
		WithDebug(false),
		WithSelectedProvider("deepseek"),
		WithSelectedModel("deepseek-reasoner"),
	}
	app, err := New(context.Background(), opts...)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer app.Shutdown()

	done := make(chan RunResponse, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sessionID := "appsdk-test-new"
	prompt := "浣犲ソ"

	go func(ctx context.Context, sessionID, prompt string) {
		result, err := app.SubmitMessage(ctx, prompt, sessionID, false)
		if err != nil {
			done <- RunResponse{
				err: fmt.Errorf("failed to start agent processing stream: %w", err),
			}
			return
		}
		done <- RunResponse{
			result: result,
		}
	}(ctx, sessionID, prompt)

	messageEvents := app.SubscribeMessage(ctx)
	streamFinished := false

	for {
		select {
		case res := <-done:
			if res.err != nil {
				if streamFinished && errors.Is(res.err, context.Canceled) {
					return
				}
				fmt.Println(res.err)
				return
			}
			fmt.Println(res.result)
			return
		case event := <-messageEvents:
			msg := event.Payload
			if msg.SessionID != sessionID || msg.Role != message.Assistant {
				continue
			}

			s, _ := json.Marshal(msg)
			fmt.Println(string(s))
			if msg.IsFinished() {
				streamFinished = true
				cancel()
			}
		case <-ctx.Done():
			if !streamFinished {
				fmt.Println("ctx done")
			}
			return
		}
	}
}

func TestApi(t *testing.T) {
	var opts = []Option{
		WithDatabaseDriver("mysql"),
		WithDatabaseDSN("root:zorkdata.8888@tcp(192.168.12.34:3306)/crush_dev?charset=utf8mb4&parseTime=True&loc=Local"),
		WithWorkDir("C:\\projectData\\biddata\\ceshi\\bid\\extract"),
		WithSkipPermissionRequests(true),
		WithDebug(false),
		WithSelectedProvider("deepseek"),
		WithSelectedModel("deepseek-reasoner"),
	}
	app, err := New(context.Background(), opts...)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer app.Shutdown()
	providers := app.Providers()
	bytes, err := json.Marshal(providers)
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(string(bytes))
}
