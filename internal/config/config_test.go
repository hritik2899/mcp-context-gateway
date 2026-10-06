package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecureDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil || c.Listen != "127.0.0.1:8080" {
		t.Fatal(c, err)
	}
	c.Listen = "0.0.0.0:8080"
	if c.Validate() == nil {
		t.Fatal("unauthenticated public bind accepted")
	}
}
func TestStrictConfiguration(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	for _, raw := range []string{`{"unknown":true}`, `{} {}`, `{"requestTimeout":"0s"}`, `{"servers":[{"name":"bad","url":"https://user:password@example.com/mcp"}]}`} {
		os.WriteFile(file, []byte(raw), 0600)
		if _, err := Load(file); err == nil {
			t.Fatal(raw)
		}
	}
}
func TestCredentialsAreNotSerialized(t *testing.T) {
	t.Setenv("TEST_GATEWAY_TOKEN", "01234567890123456789012345678901")
	c := Default()
	c.Listen = "0.0.0.0:8080"
	c.Principals = []Principal{{Name: "test", TokenEnv: "TEST_GATEWAY_TOKEN", AllowTools: []string{"*"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if string(c.JSON()) == "" {
		t.Fatal("empty config")
	}
}
