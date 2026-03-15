package service

import (
	"github.com/boyter/pincer/common"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewEnvironmentPreviewUsesPreviewPaths(t *testing.T) {
	t.Setenv("PREVIEW_SAMPLE_DATA", "true")
	t.Setenv("ACTIVITY_FILE_PATH", "")
	t.Setenv("BOTS_FILE_PATH", "")

	env := common.NewEnvironment()

	if !env.PreviewSampleData {
		t.Fatal("expected preview sample data to be enabled")
	}
	if env.ActivityFilePath != "activity.preview.json" {
		t.Errorf("expected preview activity path, got %s", env.ActivityFilePath)
	}
	if env.BotsFilePath != "bots.preview.json" {
		t.Errorf("expected preview bots path, got %s", env.BotsFilePath)
	}
}

func TestNewEnvironmentNormalUsesDefaultPaths(t *testing.T) {
	t.Setenv("PREVIEW_SAMPLE_DATA", "false")
	t.Setenv("ACTIVITY_FILE_PATH", "")
	t.Setenv("BOTS_FILE_PATH", "")

	env := common.NewEnvironment()

	if env.PreviewSampleData {
		t.Fatal("expected preview sample data to be disabled")
	}
	if env.ActivityFilePath != "activity.json" {
		t.Errorf("expected default activity path, got %s", env.ActivityFilePath)
	}
	if env.BotsFilePath != "bots.json" {
		t.Errorf("expected default bots path, got %s", env.BotsFilePath)
	}
}

func TestBootstrapPreviewDataSeedsOnlyWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	env := &common.Environment{
		BaseUrl:           "http://localhost:8001/",
		MaxPostLength:     500,
		PreviewSampleData: true,
		ActivityFilePath:  filepath.Join(dir, "activity.preview.json"),
		BotsFilePath:      filepath.Join(dir, "bots.preview.json"),
	}

	ser, err := NewService(env)
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	ser.BootstrapPreviewData()

	if len(ser.LocalActivity) == 0 {
		t.Fatal("expected preview data to create activity")
	}
	if len(ser.RegisteredBots) == 0 {
		t.Fatal("expected preview data to create bots")
	}

	activityBytes, err := os.ReadFile(env.ActivityFilePath)
	if err != nil {
		t.Fatalf("expected preview activity file to exist: %v", err)
	}
	if !strings.Contains(string(activityBytes), "local_activity") {
		t.Errorf("expected preview activity file contents, got %s", string(activityBytes))
	}
}

func TestBootstrapPreviewDataDoesNotReseedWhenDataExists(t *testing.T) {
	dir := t.TempDir()
	env := &common.Environment{
		BaseUrl:           "http://localhost:8001/",
		MaxPostLength:     500,
		PreviewSampleData: true,
		ActivityFilePath:  filepath.Join(dir, "activity.preview.json"),
		BotsFilePath:      filepath.Join(dir, "bots.preview.json"),
	}

	ser, err := NewService(env)
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	ser.RegisteredBots["existing"] = &RegisteredBot{Username: "existing", RegisteredAt: 123}
	ser.LocalActivity = append(ser.LocalActivity, ActivityObject{Username: "existing", Content: "hello", UnixTimestamp: 123})

	ser.BootstrapPreviewData()

	if len(ser.RegisteredBots) != 1 {
		t.Errorf("expected existing bot to remain the only bot, got %d", len(ser.RegisteredBots))
	}
	if len(ser.LocalActivity) != 1 {
		t.Errorf("expected existing activity to remain untouched, got %d", len(ser.LocalActivity))
	}
}
