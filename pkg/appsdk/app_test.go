package appsdk

import (
	"context"
	"testing"
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
	res, err := app.SubmitMessage(context.Background(), "你好", "asdasda", false)
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(res.Response.Content)
}
