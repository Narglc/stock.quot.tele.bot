package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInitConfig_EnvOverride(t *testing.T) {
	p := writeTempConfig(t, "telegram:\n  token: \"\"\nredis:\n  url: \"\"\nmcp:\n  target_chat_id: 0\n")
	t.Setenv("TOKEN", "env-token")
	t.Setenv("RDB_URL", "redis://x:1")
	t.Setenv("MCP_TARGET_CHAT_ID", "555")

	cfg, ok := InitConfig(p)
	if !ok {
		t.Fatal("InitConfig failed")
	}
	if cfg.Telegram.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", cfg.Telegram.Token)
	}
	if cfg.Redis.URL != "redis://x:1" {
		t.Errorf("URL = %q, want redis://x:1", cfg.Redis.URL)
	}
	if cfg.MCP.TargetChatID != 555 {
		t.Errorf("TargetChatID = %d, want 555", cfg.MCP.TargetChatID)
	}
}

func TestInitConfig_AddrDefault(t *testing.T) {
	p := writeTempConfig(t, "telegram:\n  token: \"t\"\nredis:\n  url: \"u\"\n")
	cfg, ok := InitConfig(p)
	if !ok {
		t.Fatal("InitConfig failed")
	}
	if cfg.MCP.Addr != ":8081" {
		t.Errorf("Addr = %q, want :8081", cfg.MCP.Addr)
	}
}
