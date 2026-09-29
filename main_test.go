package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestLoadEnvSkipsTheTemplateWhenTheTokenIsInTheEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DISCORD_BOT_TOKEN", "from-compose")

	if err := loadEnv(); err != nil {
		t.Errorf("got %v, want nil because the token already came from the environment", err)
	}
	if _, err := os.Stat(".env"); !os.IsNotExist(err) {
		t.Error("a template .env was written although the token is set")
	}
}

func TestLoadEnvWritesTheTemplateWithoutAToken(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DISCORD_BOT_TOKEN", "")

	if err := loadEnv(); err == nil {
		t.Error("got nil, want the please-configure error")
	}
	content, err := os.ReadFile(".env")
	if err != nil {
		t.Fatalf("got %v, want the template .env written", err)
	}
	if !slices.Contains(strings.Split(string(content), "\n"), "DISCORD_BOT_TOKEN=your_bot_token_here") {
		t.Errorf("got %q, want the token placeholder in the template", content)
	}
}

func TestLoadEnvReadsAnExistingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DEBUG_MODE", "unset")
	os.Unsetenv("DEBUG_MODE")
	if err := os.WriteFile(".env", []byte("DEBUG_MODE=true\n"), 0644); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	if err := loadEnv(); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	if os.Getenv("DEBUG_MODE") != "true" {
		t.Errorf("got DEBUG_MODE=%q, want the value from .env", os.Getenv("DEBUG_MODE"))
	}
}
