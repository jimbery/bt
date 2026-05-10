package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvFile_setsUnsetKeys(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Unsetenv("DOTENV_TEST_ALPHA")
		_ = os.Unsetenv("DOTENV_TEST_BETA")
	})
	_ = os.Unsetenv("DOTENV_TEST_ALPHA")
	if err := os.Setenv("DOTENV_TEST_BETA", "from-process"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	content := "DOTENV_TEST_ALPHA=from-file\n# comment\nDOTENV_TEST_BETA=ignored\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadDotEnvFile(p); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DOTENV_TEST_ALPHA"); got != "from-file" {
		t.Fatalf("alpha: got %q", got)
	}
	if got := os.Getenv("DOTENV_TEST_BETA"); got != "from-process" {
		t.Fatalf("beta should not be overridden: got %q", got)
	}
}

func TestLoadDotEnvFile_missingIsNoOp(t *testing.T) {
	if err := LoadDotEnvFile(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatal(err)
	}
}
